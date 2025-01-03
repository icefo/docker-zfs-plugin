# docker-volume-zfs-plugin
Docker volume plugin for creating persistent volumes as a dedicated zfs dataset.

## Installation
```
$ mkdir /mnt/icefo-docker-zfs-volumes
$ zfs create -o compression=on -o mountpoint=/mnt/icefo-docker-zfs-volumes dataset/name
# Compression is not mandatory but almost free in compute and can make you save a lot of space
# dataset/name will be the default parent dataset for the volumes
$ cd in source folder
$ make && make enable
```


## Usage
> Note that created volumes will always have a mountpoint under `/mnt/icefo-docker-zfs-volumes/volumes/`

```
$ docker volume create -d icefo/docker-volume-zfs-plugin:2.2 testVolume
testVolume
$ docker volume ls
DRIVER                               VOLUME NAME
icefo/docker-volume-zfs-plugin:2.2   asdad_db
local                                localTestvolume
icefo/docker-volume-zfs-plugin:2.2   testVolume
```

ZFS attributes can be passed in as driver options in the `docker volume create` command:
```
$ docker volume create -d icefo/docker-volume-zfs-plugin:2.2 -o compression=on -o dedup=on testVolume
testVolume
```
I don't advise to use the dedup option, but it's possible. The mountpoint option is forbidden as it's
easier for the driver to mount everything under the same path.

If you want to use a different root dataset than the default one:
```
$ docker volume create -d icefo/docker-volume-zfs-plugin:2.2 -o driver_zfsRootDataset="zpool-docker/test1234" testVolume23
testVolume23
```
Will create the dataset `zpool-docker/test1234/testVolume23` mounted under /mnt/icefo-docker-zfs-volumes/volumes/testVolume23 


### Docker compose
The plugin can be used in docker compose similar to other volume plugins:
```
volumes:
   data:
      driver: icefo/docker-volume-zfs-plugin:2.2
      driver_opts:
         compression: on
```
Since a default root dataset must be available, the default docker-compose name can be used


## Breaking API changes
I make no guarantees about the compatibility with previous versions of this plugin
