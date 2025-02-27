package main

import (
	zfsdriver "docker-volume-zfs-plugin/zfs"
	"log/slog"
	"os"
	"strconv"

	"github.com/coreos/go-systemd/activation"
	"github.com/docker/go-plugins-helpers/volume"
)

func main() {
	lvl := new(slog.LevelVar)
	lvl.Set(slog.LevelInfo)
	fh, err := os.OpenFile("/mnt/icefo-docker-zfs-volumes/my_app.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		println("Failed to create file handler:", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewTextHandler(fh, &slog.HandlerOptions{
		Level: lvl,
	}))

	debug := os.Getenv("DEBUG")
	if ok, _ := strconv.ParseBool(debug); ok {
		lvl.Set(slog.LevelDebug)
	}

	d, err := zfsdriver.NewZfsDriver(logger)
	if err != nil {
		panic(err)
	}

	h := volume.NewHandler(d)

	listeners, _ := activation.Listeners() // wtf coreos, this function never returns errors
	if len(listeners) == 0 {
		logger.Debug("launching volume handler.")
		h.ServeUnix("icefo-zfs-vol", 0)
	} else if len(listeners) == 1 {
		l := listeners[0]
		logger.Debug("launching volume handler", "listener", l.Addr().String())
		h.Serve(l)
	} else {
		logger.Warn("driver does not support multiple sockets")
	}
}
