package agent

import (
	"context"
	"fmt"
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// HardwareProfile holds the agent-detected capabilities of the host machine.
// BandwidthMbps is not detectable via gopsutil; it is set from the provider's
// resource profile configuration.
type HardwareProfile struct {
	CPUCores      int
	RAMMB         int64
	GPUPresent    bool
	GPUModel      string
	StorageGB     int64
	BandwidthMbps int
	Platform      string // runtime.GOOS: linux, windows, darwin, android
	Arch          string // runtime.GOARCH: amd64, arm64, arm, etc.
}

// Detect samples the host hardware and returns a populated HardwareProfile.
// All errors from gopsutil are surfaced; callers should decide whether to
// retry or degrade gracefully.
//
// GPU detection: gopsutil v3 does not expose GPU data in any package.
// host.InfoWithContext returns OS/platform metadata only (hostname, uptime,
// virtualisation) — no GPU fields. GPUPresent and GPUModel are always false/""
// from this function. Platform-specific GPU probing (nvidia-smi, /dev/dri,
// Android GPU APIs) will be added in a later agent phase.
func Detect(ctx context.Context) (HardwareProfile, error) {
	p := HardwareProfile{
		Platform: runtime.GOOS,
		Arch:     runtime.GOARCH,
	}

	// CPU — sum Cores across all physical sockets.
	// On some platforms (Windows) InfoStat.Cores carries logical processors;
	// if the sum is still zero fall back to cpu.CountsWithContext.
	cpuInfos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return HardwareProfile{}, fmt.Errorf("detect hardware: cpu info: %w", err)
	}
	var totalCores int32
	for _, info := range cpuInfos {
		totalCores += info.Cores
	}
	if totalCores == 0 {
		n, err := cpu.CountsWithContext(ctx, false) // false = physical cores
		if err != nil {
			return HardwareProfile{}, fmt.Errorf("detect hardware: cpu count fallback: %w", err)
		}
		totalCores = int32(n)
	}
	p.CPUCores = int(totalCores)

	// RAM — convert bytes to MB.
	vmStat, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return HardwareProfile{}, fmt.Errorf("detect hardware: memory: %w", err)
	}
	p.RAMMB = int64(vmStat.Total / (1024 * 1024))

	// Storage — total bytes on the root filesystem, converted to GB.
	// Use C:\ on Windows; Linux/macOS production nodes use /.
	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = `C:\`
	}
	diskStat, err := disk.UsageWithContext(ctx, diskPath)
	if err != nil {
		return HardwareProfile{}, fmt.Errorf("detect hardware: disk: %w", err)
	}
	p.StorageGB = int64(diskStat.Total / (1024 * 1024 * 1024))

	// GPU: unavailable via gopsutil — see function doc above.
	p.GPUPresent = false
	p.GPUModel = ""

	return p, nil
}

// HasChanged reports whether any field of the hardware profile has changed.
// All fields are comparable types so struct equality is used directly.
func HasChanged(old, new HardwareProfile) bool {
	return old != new
}
