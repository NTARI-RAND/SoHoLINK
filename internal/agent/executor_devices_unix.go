//go:build !windows

package agent

import (
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

// cupsSocketHostPath is the canonical CUPS socket path on Linux distributions
// (Alpine, Debian, Ubuntu, Fedora). Bind-mounting this into a print workload
// container gives it access to the host's CUPS daemon for traditional printing.
const cupsSocketHostPath = "/var/run/cups/cups.sock"

// deviceMountsFor returns the bind mounts and device mappings required to
// satisfy the requested device-access exceptions. On Unix:
//   - cups_socket: bind-mount the host CUPS Unix socket into the container
//   - usb_printer: map connectionPath into the container at the same path
//     with rwm cgroup permissions. connectionPath comes from
//     ContainerSpec.ConnectionPath, which the agent populates from local
//     PrinterInfo at job-poll time. Empty connectionPath produces no
//     mapping — defensive against allowlist entries that declare
//     usb_printer access for non-print workloads.
func deviceMountsFor(access []DeviceAccess, connectionPath string) deviceMountSet {
	var set deviceMountSet
	for _, da := range access {
		switch da {
		case DeviceCUPSSocket:
			set.mounts = append(set.mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: cupsSocketHostPath,
				Target: cupsSocketHostPath,
			})
		case DeviceUSBPrinter:
			if connectionPath == "" {
				continue
			}
			set.deviceMappings = append(set.deviceMappings, container.DeviceMapping{
				PathOnHost:        connectionPath,
				PathInContainer:   connectionPath,
				CgroupPermissions: "rwm",
			})
		}
	}
	return set
}
