package agent

import (
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

// deviceMountSet bundles the bind mounts and device mappings produced by
// deviceMountsFor for a given workload's declared device access. The
// platform-specific implementations live in executor_devices_unix.go and
// executor_devices_windows.go.
type deviceMountSet struct {
	mounts         []mount.Mount
	deviceMappings []container.DeviceMapping
}
