//go:build !windows

package agent

import (
	"github.com/docker/docker/api/types/mount"
)

// cupsSocketHostPath is the canonical CUPS socket path on Linux distributions
// (Alpine, Debian, Ubuntu, Fedora). Bind-mounting this into a print workload
// container gives it access to the host's CUPS daemon for traditional printing.
const cupsSocketHostPath = "/var/run/cups/cups.sock"

// deviceMountsFor returns the bind mounts and device mappings required to
// satisfy the requested device-access exceptions. On Unix:
//   - cups_socket: bind-mount the host CUPS Unix socket into the container
//   - usb_printer: not yet wired — needs PrinterInfo.ConnectionPath threaded
//     through ContainerSpec. Tracked for B4 (print job confirmation flow).
func deviceMountsFor(access []DeviceAccess) deviceMountSet {
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
			// TODO(B4): wire PrinterInfo.ConnectionPath through ContainerSpec
		}
	}
	return set
}
