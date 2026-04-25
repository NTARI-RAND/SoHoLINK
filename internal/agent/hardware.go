package agent

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sort"

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
	Platform      string        // runtime.GOOS: linux, windows, darwin, android
	Arch          string        // runtime.GOARCH: amd64, arm64, arm, etc.
	Printers      []PrinterInfo // traditional + 3D printers detected by DetectPrinters
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

	// Printers — detection failure is non-fatal. A machine with no printers
	// or where lpstat / WMI is unavailable still reports a complete
	// HardwareProfile. The orchestrator simply sees an empty Printers slice
	// and routes no print jobs to the node.
	printers, err := DetectPrinters()
	if err != nil {
		slog.Warn("printer detection partial failure", "err", err)
	}
	p.Printers = printers

	return p, nil
}

// HasChanged reports whether any field of the hardware profile has changed.
// Slice fields prevent direct struct equality, so each field is compared
// explicitly. Printers are compared with stable ordering via printersEqual
// so detection-order jitter does not produce false-positive change events.
func HasChanged(old, new HardwareProfile) bool {
	if old.CPUCores != new.CPUCores ||
		old.RAMMB != new.RAMMB ||
		old.GPUPresent != new.GPUPresent ||
		old.GPUModel != new.GPUModel ||
		old.StorageGB != new.StorageGB ||
		old.BandwidthMbps != new.BandwidthMbps ||
		old.Platform != new.Platform ||
		old.Arch != new.Arch {
		return true
	}
	return !printersEqual(old.Printers, new.Printers)
}

// printersEqual returns true if both slices contain the same set of
// PrinterInfo entries regardless of order. Comparison is by stable sort
// on ID followed by element-by-element equality.
func printersEqual(a, b []PrinterInfo) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	aCopy := make([]PrinterInfo, len(a))
	bCopy := make([]PrinterInfo, len(b))
	copy(aCopy, a)
	copy(bCopy, b)
	sort.Slice(aCopy, func(i, j int) bool { return aCopy[i].ID < aCopy[j].ID })
	sort.Slice(bCopy, func(i, j int) bool { return bCopy[i].ID < bCopy[j].ID })
	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}
	return true
}
