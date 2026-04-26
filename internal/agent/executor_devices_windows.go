//go:build windows

package agent

// deviceMountsFor on Windows returns no device mounts. The Linux container
// runtime (Docker Desktop's WSL2 VM) cannot bind-mount Windows printer
// resources — the Windows print spooler does not expose a Unix socket and
// USB devices are not directly available to the Linux container.
//
// Windows print support is tracked as sub-phase B8 (Windows-native print
// agent) and requires a separate execution model from the containerized
// path used for compute and storage workloads. Until B8 lands, Windows
// agents cannot accept print workloads — enforced upstream by the opt-out
// gate, not here. This stub returns empty without error so the executor's
// shape stays uniform across platforms.
func deviceMountsFor(_ []DeviceAccess) deviceMountSet {
	return deviceMountSet{}
}
