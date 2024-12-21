# docker-volume-zfs-plugin
Docker volume plugin for creating persistent volumes as a dedicated zfs dataset.

## Installation

```
$ docker plugin install TrilliumIT/zfs

# or to use the name 'zfs' when creating volumes
$ docker plugin install TrilliumIT/zfs --alias zfs

# or to change where plugin state is stored
$ docker plugin install TrilliumIT/zfs state.source=<any_folder>
```

## Usage

> Note that created datasets will always have a mountpoint under `/var/lib/docker/volumes/`
>
> Any manually-specified mountpoints for datasets will be relative to that root path.

```
$ docker volume create -d TrilliumIT/zfs tank/docker-volumes/data
tank/docker-volumes/1
$ docker volume ls
DRIVER              VOLUME NAME
local               2d75de358a70ba469ac968ee852efd4234b9118b7722ee26a1c5a90dcaea6751
local               842a765a9bb11e234642c933b3dfc702dee32b73e0cf7305239436a145b89017
local               9d72c664cbd20512d4e3d5bb9b39ed11e4a632c386447461d48ed84731e44034
local               be9632386a2d396d438c9707e261f86fd9f5e72a7319417901d84041c8f14a4d
local               e1496dfe4fa27b39121e4383d1b16a0a7510f0de89f05b336aab3c0deb4dda0e
TrilliumIT/zfs      tank/docker-volumes/data
```

ZFS attributes can be passed in as driver options in the `docker volume create` command:

```
$ docker volume create -d TrilliumIT/zfs -o compression=lz4 -o dedup=on tank/docker-volumes/data2
tank/docker-volume/data2
```

You don't need to use specific root datasets, and can use any pool on the system:

```
$ docker volume create -d TrilliumIT/zfs tank2/docker-data
tank2/docker-data
```

### docker compose

The plugin can be used in docker compose similar to other volume plugins:
```
volumes:
   data:
      name: tank/docker-volumes/data3
      driver: TrilliumIT/zfs
      driver_opts:
         compression: on
```

## Breaking API changes

### Version 2
The driver has been refactored to integrate into the docker-managed plugin v2 system. For the manually installed `zfs` plugin, please use the v1.x.x release/branch.

> Any preexisting volumes from the manually installed plugin _will not_ be controlled by the managed plugin.

The two versions of the plugin can operate in parallel if you ensure that you don't ever create volumes with the managed plugin in a root dataset used by the manually installed plugin. To migrate a given dataset to the new infrastructure you will need to duplicate the data from the existing volume to a new volume created with the managed plugin before changing your application to use the new volume.

### Version 1
The driver was refactored to allow multiple pools and fully qualified dataset names. The master branch has removed all legacy naming options and now fully qualified dataset names are required.

If you still have not converted to fully qualified names, please use the latest release in the v0.4.x line until you can switch to non-legacy volume names.
