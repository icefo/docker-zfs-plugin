package main

import (
	zfsdriver "docker-zfs-plugin/zfs"
	"log/slog"
	"os"
	"strconv"

	"github.com/coreos/go-systemd/activation"
	"github.com/docker/go-plugins-helpers/volume"
)

func main() {
	lvl := new(slog.LevelVar)
	lvl.Set(slog.LevelInfo)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
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
	if len(listeners) > 1 {
		logger.Warn("driver does not support multiple sockets")
	}
	if len(listeners) == 0 {
		logger.Debug("launching volume handler.")
		h.ServeUnix("zfs-v2", 0)
	} else {
		l := listeners[0]
		logger.Debug("launching volume handler", "listener", l.Addr().String())
		h.Serve(l)
	}
}
