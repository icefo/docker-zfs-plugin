package zfsdriver

import (
	"errors"
	"os/exec"
	"strings"
)

func getZfsDatasetNameFromMountpoint(mountpoint string) (result string, err error) {
	out, err := exec.Command("zfs", "list", "-r", "-H", "-o", "name,mountpoint", "-t", "filesystem").Output()
	if err != nil {
		return "", errors.New("could not list ZFS datasets")
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 2 {
			name, mount := fields[0], fields[1]
			if mount == mountpoint {
				return name, nil
			}
		}
	}
	return "", nil
}
