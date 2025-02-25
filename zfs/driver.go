package zfsdriver

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/clinta/go-zfs"
	zfscmd "github.com/clinta/go-zfs/cmd"
	"github.com/docker/go-plugins-helpers/volume"
)

type VolumeProperties struct {
	DatasetFQN string `json:"datasetFQN"`
}

// ZfsDriver implements the plugin helpers volume.Driver interface for zfs
type ZfsDriver struct {
	volume.Driver

	//The volumes map volume name (req.name in the creation request) to VolumeProperties
	volumes            map[string]VolumeProperties
	log                *slog.Logger
	defaultRootDataset string
}

// Where to save the stored volumes/metadata
const (
	// Defined in the config
	propagatedMountPath = "/var/lib/docker/plugins/pluginHash/propagated-mount/"
	//This is some top-tier garbage code, but the v2 plugin infrastructure always re-scopes any returned mount paths for the
	//container to where they mount the filesystem. Since we actually return host paths via ZFS, however, we need to somehow
	//escape this system back to the root namespace. They try to provide a way to do this via the "propagatedmount" infra,
	//where they replace the specified container path with a base path on the host, but that base is where _they_ decide to
	//put it, deep in the docker plugin paths where they mount the filesystem, and it includes a variable path token that we
	//can't get access to here. To get around this, we propagate the same length of path as they would mount us under (just
	//without the variable hash), and then peel back the path with repeated ".." so we get to the "real" path from root.
	//This variable should be prepended to any mount path that we return out of the plugin to ensure we make all parties
	//"agree" where things are stored.
	hostRootPath = propagatedMountPath + "../../../../../.."
	volumeBase   = "/mnt/icefo-docker-zfs-volumes"
	statePath    = volumeBase + "/state.json"
)

// NewZfsDriver returns the plugin driver object
func NewZfsDriver(logger *slog.Logger) (*ZfsDriver, error) {
	defaultRootDataset, _ := getZfsDatasetNameFromMountpoint(volumeBase)
	if defaultRootDataset == "" {
		return nil, errors.New(volumeBase + " does not exist or is not a zfs dataset")
	}

	zd := &ZfsDriver{
		volumes:            make(map[string]VolumeProperties),
		log:                logger,
		defaultRootDataset: defaultRootDataset,
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
	if req.Options == nil {
		req.Options = make(map[string]string)
	}

	zfsDatasetName := ""
	if req.Options["driver_zfsRootDataset"] != "" {
		zfsDatasetName = req.Options["driver_zfsRootDataset"] + "/" + req.Name
		delete(req.Options, "driver_zfsRootDataset")
	} else {
		zfsDatasetName = zd.defaultRootDataset + "/volumes/" + req.Name
	}
	if zfs.DatasetExists(zfsDatasetName) {
		return errors.New("volume already exists")
	}

	zd.log.Debug("zfsDatasetName", zfsDatasetName)

	//We unfortunately have to refuse the mountpath that the user specifies as we're stuck inside a container and
	//can't access all of the host filesystem that ZFS mounts things relative to. We explicitly mount the volumeBase path into
	//the container so that we can mount our volumes there with a consistent filepath between the host and the container. Thus
	//we need to prepend this path to all mountpaths we pass to ZFS itself when it creates the datasets and sets the host
	//mountpoints. This is needed to ensure that when ZFS on the host re-mounts the dataset (e.g. on boot) it does so in the
	//right place.
	if req.Options["mountpoint"] != "" {
		zd.log.Error("mountpoint option is not supported")
		return errors.New("mountpoint option is not supported")
	}

	req.Options["mountpoint"] = volumeBase + "/volumes/" + req.Name

	zd.log.Debug("mountpoint", req.Options["mountpoint"])

	// Check if a snapshot should be cloned (using the "from-snapshot" option)
	if snapshotName, ok := req.Options["from-snapshot"]; ok && snapshotName != "" {
		delete(req.Options, "from-snapshot")
		cloneOpts := &zfscmd.CloneOpts{
			CreateParents: true,
			SetProperties: req.Options,
		}
		if err := zfscmd.Clone(snapshotName, zfsDatasetName, cloneOpts); err != nil {
			zd.log.Error("failed to clone snapshot", slog.Any("err", err))
			return errors.New("failed to clone snapshot")
		}
		zd.volumes[req.Name] = VolumeProperties{DatasetFQN: zfsDatasetName}
		zd.saveDatasetState()
		return nil
	}

	// Otherwise, create the dataset normally
	_, err := zfs.CreateDatasetRecursive(zfsDatasetName, req.Options)
	if err != nil {
		zd.log.Error("Cannot create ZFS volume", slog.Any("err", err), "zfsDatasetName", zfsDatasetName, "Options", req.Options)
		return err
	}
	zd.volumes[req.Name] = VolumeProperties{DatasetFQN: zfsDatasetName}
	zd.saveDatasetState()

	return nil
}

// List returns a list of zfs volumes on this host
func (zd *ZfsDriver) List() (*volume.ListResponse, error) {
	zd.log.Debug("List")
	var vols []*volume.Volume

	for volName := range zd.volumes {
		vol, err := zd.getVolume(volName)
		if err != nil {
			zd.log.Error("Failed to get dataset info", slog.Any("err", err), "Volume Name", volName)
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

func (zd *ZfsDriver) scopeMountPath(mountpath string) string {
	//We just naively join them with string append rather than invoking filepath.join as that will collapse our ".." hack to
	//get out to properly mount relative to the root filesystem.
	return hostRootPath + mountpath
}

func (zd *ZfsDriver) getVolume(name string) (*volume.Volume, error) {
	volProps, ok := zd.volumes[name]
	if !ok {
		zd.log.Error("Volume not found", "name", name)
		return nil, errors.New("volume not found")
	}

	ds, err := zfs.GetDataset(volProps.DatasetFQN)
	if err != nil {
		return nil, err
	}

	mp, err := ds.GetMountpoint()
	if err != nil {
		return nil, err
	}
	//Need to scope the host path for the container before returning to docker
	mp = zd.scopeMountPath(mp)

	ts, err := ds.GetCreation()
	if err != nil {
		zd.log.Error("Failed to get creation property from zfs dataset", slog.Any("err", err), "Volume name", name)
		return &volume.Volume{Name: name, Mountpoint: mp}, nil
	}

	return &volume.Volume{Name: name, Mountpoint: mp, CreatedAt: ts.Format(time.RFC3339)}, nil
}

func (zd *ZfsDriver) getMP(name string) (string, error) {
	volProps, ok := zd.volumes[name]
	if !ok {
		zd.log.Error("Volume not found", "name", name)
		return "", errors.New("volume not found")
	}

	ds, err := zfs.GetDataset(volProps.DatasetFQN)
	if err != nil {
		return "", err
	}

	mp, err := ds.GetMountpoint()
	if err != nil {
		return "", err
	}

	//Need to scope the host path for the container before returning to docker
	mp = zd.scopeMountPath(mp)

	return mp, nil
}

// Remove destroys a zfs dataset for a volume
func (zd *ZfsDriver) Remove(req *volume.RemoveRequest) error {
	zd.log.Debug("Remove", "Request", req)
	volProps, ok := zd.volumes[req.Name]
	if !ok {
		zd.log.Error("Volume not found", "name", req.Name)
		return errors.New("volume not found")
	}

	ds, err := zfs.GetDataset(volProps.DatasetFQN)
	if err != nil {
		return err
	}

	// todo also remove mountpoint folder with os.???
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
