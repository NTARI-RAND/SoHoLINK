//go:build darwin

package wizard

import (
	"fmt"
	"os/exec"
	"strings"
)

// detectDriveTypeImpl detects drive type on macOS.
func detectDriveTypeImpl() string {
	// diskutil info returns "Solid State: Yes" for SSDs
	cmd := exec.Command("diskutil", "info", "/")
	output, err := cmd.Output()
	if err != nil {
		return "Unknown"
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Solid State:") {
			if strings.Contains(line, "Yes") {
				// Distinguish NVMe from SATA SSD
				cmd2 := exec.Command("system_profiler", "SPNVMeDataType")
				out2, err := cmd2.Output()
				if err == nil && len(out2) > 100 {
					return "NVMe"
				}
				return "SSD"
			}
			return "HDD"
		}
	}
	return "Unknown"
}

// detectGPUImpl detects GPU on macOS using system_profiler.
func detectGPUImpl() *GPUInfo {
	cmd := exec.Command("system_profiler", "SPDisplaysDataType", "-json")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	outputStr := string(output)

	// Quick heuristic parse — look for chipset / vendor info
	gpu := &GPUInfo{}

	if strings.Contains(outputStr, "NVIDIA") {
		gpu.Vendor = "NVIDIA"
		gpu.Model = extractMacGPUModel(outputStr, "NVIDIA")
	} else if strings.Contains(outputStr, "AMD") || strings.Contains(outputStr, "Radeon") {
		gpu.Vendor = "AMD"
		gpu.Model = extractMacGPUModel(outputStr, "AMD")
	} else if strings.Contains(outputStr, "Apple M") {
		// Apple Silicon integrated GPU — report but mark as integrated
		gpu.Vendor = "Apple"
		gpu.Model = "Apple Silicon GPU"
	} else {
		return nil
	}

	return gpu
}

// extractMacGPUModel extracts GPU model name from system_profiler JSON output.
func extractMacGPUModel(output, vendor string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Chipset Model") || strings.Contains(line, "sppci_model") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				model := strings.Trim(parts[1], ` "`)
				return strings.TrimSpace(model)
			}
		}
	}
	return vendor + " GPU"
}

// detectHypervisorImpl detects hypervisor availability on macOS.
// macOS supports the Virtualization.framework (Apple Silicon & Intel).
func detectHypervisorImpl() (HypervisorInfo, error) {
	info := HypervisorInfo{
		Type:      "virtualization-framework",
		Installed: false,
		Enabled:   false,
		Features:  []string{},
	}

	// Check if Virtualization.framework is usable by probing the hvf entitlement.
	// On Intel Macs, check for Hypervisor.framework via sysctl.
	cmd := exec.Command("sysctl", "kern.hv_support")
	output, err := cmd.Output()
	if err == nil && strings.Contains(string(output), "1") {
		info.Installed = true
		info.Enabled = true
		info.Features = append(info.Features, "hypervisor-framework")
	}

	// Additional check: look for Rosetta / Apple Virtualization on Apple Silicon
	cmd2 := exec.Command("sysctl", "hw.optional.arm64")
	out2, err2 := cmd2.Output()
	if err2 == nil && strings.Contains(string(out2), "1") {
		info.Features = append(info.Features, "apple-virtualization-framework")
		info.Features = append(info.Features, "nested-virtualization")
		info.Installed = true
		info.Enabled = true
	}

	// Try to get macOS version for framework availability note
	cmd3 := exec.Command("sw_vers", "-productVersion")
	out3, err3 := cmd3.Output()
	if err3 == nil {
		info.Version = strings.TrimSpace(string(out3))
	}

	return info, nil
}

// detectFirewallEnabledImpl detects Application Firewall status on macOS.
func detectFirewallEnabledImpl() bool {
	// /usr/libexec/ApplicationFirewall/socketfilterfw reports firewall state
	cmd := exec.Command("/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "enabled")
}

// detectVirtualizationEnabledImpl checks if hardware virtualisation is
// accessible on macOS (Hypervisor.framework entitlement check).
func detectVirtualizationEnabledImpl() bool {
	// kern.hv_support = 1 means Hypervisor.framework is available
	cmd := exec.Command("sysctl", "kern.hv_support")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "1")
}

// GetElectricityRate returns 0; the user inputs electricity rate manually.
func GetElectricityRate() float64 {
	return 0.0
}

// MeasurePowerDraw attempts to read power via powermetrics on macOS.
// Requires root (sudo). Returns an error if unavailable.
func MeasurePowerDraw() (idle, load float64, err error) {
	// powermetrics requires root — use estimation fallback in cost_calculator.go
	cmd := exec.Command("powermetrics", "--samplers", "cpu_power", "-n", "1", "-i", "1000")
	output, errCmd := cmd.Output()
	if errCmd != nil {
		return 0, 0, fmt.Errorf("powermetrics unavailable (root required): %w", errCmd)
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CPU Power:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				var watts float64
				_, scanErr := fmt.Sscanf(parts[2], "%f", &watts)
				if scanErr == nil {
					return watts * 0.3, watts, nil // approximate idle = 30% of load
				}
			}
		}
	}

	return 0, 0, fmt.Errorf("could not parse powermetrics output")
}
