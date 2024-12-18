package zfsdriver

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/clinta/go-zfs"
	"github.com/docker/go-plugins-helpers/volume"
)

// ZfsDriver implements the plugin helpers volume.Driver interface for zfs
type ZfsDriver struct {
	volume.Driver

	//The volumes the plugin is tracking, use empty struct as value to mimic a set
	volumes map[string]struct{}
	log     *slog.Logger
}

// Where to save the stored volumes/metadata
const (
	statePath = "/mnt/zfs-v2-state.json"
	//This is some top-tier garbage code, but the v2 plugin infrastructure always re-scopes any returned mount paths for the
	//container to where they mount the filesystem. Since we actually return host paths via ZFS, however, we need to somehow
	//escape this system back to the root namespace. They try to provide a way to do this via the "propagatedmount" infra,
	//where they replace the specified container path with a base path on the host, but that base is where _they_ decide to
	//put it, deep in the docker plugin paths where they mount the filesystem, and it includes a variable path token that we
	//can't get access to here. To get around this, we propagate the same length of path as they would mount us under (just
	//without the variable hash), and then then peel back the path with repeated ".." so we get to the "real" path from root.
	//This variable should be prepended to any mount path that we return out of the plugin to ensure we make all parties
	//"agree" where things are stored.
	propagateBase = "/var/lib/docker/plugins/pluginHash/propagated-mount/../../../../../.."
	//To get a root-relative path that we can have access to in the container, we store things under the usual docker volume
	//path in the host filesystem and mount that path in.
	volumeBase = "/var/lib/docker/volumes/"
)

// NewZfsDriver returns the plugin driver object
func NewZfsDriver(logger *slog.Logger) (*ZfsDriver, error) {
	zd := &ZfsDriver{
		volumes: map[string]struct{}{},
		log:     logger,
	}
	zd.log.Info("Creating ZFS Driver")

	//Load any datasets that we had saved to persistent storage
	err := zd.loadDatasetState()
	if err != nil {
		return nil, err
	}

	return zd, nil
}

func (zd *ZfsDriver) loadDatasetState() error {
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			zd.log.Debug("No initial state found")
		} else {
			return err
		}
	} else {
		if err := json.Unmarshal(data, &zd.volumes); err != nil {
			return err
		}
	}
	return nil
}

func (zd *ZfsDriver) saveDatasetState() {
	data, err := json.Marshal(zd.volumes)
	if err != nil {
		zd.log.Error("Cannot save dataset state", slog.Any("err", err), "Volumes", zd.volumes)
		return
	}

	if err := os.WriteFile(statePath, data, 0644); err != nil {
		zd.log.Error("Cannot write state path file", slog.Any("err", err), "StatePath", statePath)
	}
}

// Create creates a new zfs dataset for a volume
func (zd *ZfsDriver) Create(req *volume.CreateRequest) error {
	zd.log.Debug("Create", "Request", req)

	if zfs.DatasetExists(req.Name) {
		return errors.New("volume already exists")
	}

	//We unfortunately have to somewhat ignore the mountpath that the user specifies as we're stuck inside a container and
	//can't access all of the host filesystem that ZFS mounts things relative to. We explicitly mount the volumeBase path into
	//the container so that we can mount our volumes there with a consistent filepath between the host and the container. Thus
	//we need to prepend this path to all mountpaths we pass to ZFS itself when it creates the datasets and sets the host
	//mountpoints. This is needed to ensure that when ZFS on the host re-mounts the dataset (e.g. on boot) it does so in the
	//right place. To try and play nice we at least use the user-specified mountpath as a sub-path if one was provided.
	if req.Options == nil {
		req.Options = make(map[string]string)
	}
	mountpoint, explicit := req.Options["mountpoint"]
	if !explicit {
		//If we have no explicit mountpoint the default ZFS behaviour is to use the dataset name, so mimic that here.
		mountpoint = req.Name
	}
	req.Options["mountpoint"] = volumeBase + strings.Trim(mountpoint, "/")

	_, err := zfs.CreateDatasetRecursive(req.Name, req.Options)
	if err != nil {
		return err
	}

	zd.volumes[req.Name] = struct{}{}

	zd.saveDatasetState()

	return err
}

// List returns a list of zfs volumes on this host
func (zd *ZfsDriver) List() (*volume.ListResponse, error) {
	zd.log.Debug("List")
	var vols []*volume.Volume

	for dsn := range zd.volumes {
		vol, err := zd.getVolume(dsn)
		if err != nil {
			zd.log.Error("Failed to get dataset info", slog.Any("err", err), "DatasetName", dsn)
			continue
		}
		vols = append(vols, vol)
	}

	return &volume.ListResponse{Volumes: vols}, nil
}

// Get returns the volume.Volume{} object for the requested volume
// nolint: dupl
func (zd *ZfsDriver) Get(req *volume.GetRequest) (*volume.GetResponse, error) {
	zd.log.Debug("Get", "Request", req)

	v, err := zd.getVolume(req.Name)
	if err != nil {
		return nil, err
	}

	return &volume.GetResponse{Volume: v}, nil
}

func (zd *ZfsDriver) scopeMount(mountpath string) string {
	//We just naively join them with string append rather than invoking filepath.join as that will collapse our ".." hack to
	//get out to properly mount relative to the root filesystem.
	return propagateBase + mountpath
}

func (zd *ZfsDriver) getVolume(name string) (*volume.Volume, error) {
	ds, err := zfs.GetDataset(name)
	if err != nil {
		return nil, err
	}

	mp, err := ds.GetMountpoint()
	if err != nil {
		return nil, err
	}

	//Need to scope the host path for the container before returning to docker
	mp = zd.scopeMount(mp)

	ts, err := ds.GetCreation()
	if err != nil {
		zd.log.Error("Failed to get creation property from zfs dataset", slog.Any("err", err), "Volume name", name)
		return &volume.Volume{Name: name, Mountpoint: mp}, nil
	}

	return &volume.Volume{Name: name, Mountpoint: mp, CreatedAt: ts.Format(time.RFC3339)}, nil
}

func (zd *ZfsDriver) getMP(name string) (string, error) {
	ds, err := zfs.GetDataset(name)
	if err != nil {
		return "", err
	}

	mp, err := ds.GetMountpoint()
	if err != nil {
		return "", err
	}

	//Need to scope the host path for the container before returning to docker
	mp = zd.scopeMount(mp)

	return mp, nil
}

// Remove destroys a zfs dataset for a volume
func (zd *ZfsDriver) Remove(req *volume.RemoveRequest) error {
	zd.log.Debug("Remove", "Request", req)

	ds, err := zfs.GetDataset(req.Name)
	if err != nil {
		return err
	}

	err = ds.Destroy()
	if err != nil {
		return err
	}

	delete(zd.volumes, req.Name)

	zd.saveDatasetState()

	return nil
}

// Path returns the mountpoint of a volume
// nolint: dupl
func (zd *ZfsDriver) Path(req *volume.PathRequest) (*volume.PathResponse, error) {
	zd.log.Debug("Path", "Request", req)

	mp, err := zd.getMP(req.Name)
	if err != nil {
		return nil, err
	}

	return &volume.PathResponse{Mountpoint: mp}, nil
}

// Mount returns the mountpoint of the zfs volume
// nolint: dupl
func (zd *ZfsDriver) Mount(req *volume.MountRequest) (*volume.MountResponse, error) {
	zd.log.Debug("Mount", "Request", req)

	mp, err := zd.getMP(req.Name)
	if err != nil {
		return nil, err
	}

	return &volume.MountResponse{Mountpoint: mp}, nil
}

// Unmount does nothing because a zfs dataset need not be unmounted
func (zd *ZfsDriver) Unmount(req *volume.UnmountRequest) error {
	zd.log.Debug("Unmount", "Request", req)

	return nil
}

// Capabilities sets the scope to local as this is a local only driver
func (zd *ZfsDriver) Capabilities() *volume.CapabilitiesResponse {
	zd.log.Debug("Capabilities")
	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: "local"}}
}
