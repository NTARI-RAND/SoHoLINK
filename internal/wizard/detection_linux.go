//go:build linux

package wizard

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// detectDriveTypeImpl detects drive type on Linux.
func detectDriveTypeImpl() string {
	// Try to check if drive is rotational
	// /sys/block/sda/queue/rotational: 0 = SSD, 1 = HDD
	data, err := os.ReadFile("/sys/block/sda/queue/rotational")
	if err != nil {
		return "Unknown"
	}

	rotational := strings.TrimSpace(string(data))
	if rotational == "0" {
		// Check if NVMe
		if _, err := os.Stat("/dev/nvme0n1"); err == nil {
			return "NVMe"
		}
		return "SSD"
	}

	return "HDD"
}

// detectGPUImpl detects GPU on Linux.
func detectGPUImpl() *GPUInfo {
	// Try lspci to find GPU
	cmd := exec.Command("lspci")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		lineLower := strings.ToLower(line)

		// Look for VGA or 3D controller
		if !strings.Contains(lineLower, "vga") && !strings.Contains(lineLower, "3d controller") {
			continue
		}

		// Skip integrated Intel graphics
		if strings.Contains(lineLower, "intel") && strings.Contains(lineLower, "integrated") {
			continue
		}

		gpu := &GPUInfo{}

		// Parse vendor and model
		if strings.Contains(lineLower, "nvidia") {
			gpu.Vendor = "NVIDIA"
			gpu.Model = extractGPUModel(line, "NVIDIA")
		} else if strings.Contains(lineLower, "amd") || strings.Contains(lineLower, "radeon") {
			gpu.Vendor = "AMD"
			gpu.Model = extractGPUModel(line, "AMD")
		}

		if gpu.Vendor != "" {
			// Try to get driver version
			if gpu.Vendor == "NVIDIA" {
				gpu.DriverVersion = getNVIDIADriverVersion()
			}
			return gpu
		}
	}

	return nil
}

// extractGPUModel extracts GPU model from lspci line.
func extractGPUModel(line, vendor string) string {
	// Example: "01:00.0 VGA compatible controller: NVIDIA Corporation GA102 [GeForce RTX 3090]"
	parts := strings.SplitN(line, ":", 3)
	if len(parts) < 3 {
		return "Unknown"
	}

	model := strings.TrimSpace(parts[2])
	// Remove vendor name if present
	model = strings.TrimPrefix(model, vendor+" Corporation ")
	model = strings.TrimPrefix(model, vendor+" ")

	return model
}

// getNVIDIADriverVersion gets NVIDIA driver version.
func getNVIDIADriverVersion() string {
	cmd := exec.Command("nvidia-smi", "--query-gpu=driver_version", "--format=csv,noheader")
	output, err := cmd.Output()
	if err != nil {
		return "Unknown"
	}
	return strings.TrimSpace(string(output))
}

// detectHypervisorImpl detects KVM on Linux.
func detectHypervisorImpl() (HypervisorInfo, error) {
	info := HypervisorInfo{
		Type:      "kvm",
		Installed: false,
		Enabled:   false,
		Features:  []string{},
	}

	// Check if KVM module is loaded
	cmd := exec.Command("lsmod")
	output, err := cmd.Output()
	if err == nil {
		if strings.Contains(string(output), "kvm") {
			info.Installed = true
			info.Enabled = true
		}
	}

	// Check if /dev/kvm exists
	if _, err := os.Stat("/dev/kvm"); err == nil {
		info.Installed = true
		info.Enabled = true
	}

	// Get KVM version from kernel
	if info.Installed {
		data, err := os.ReadFile("/sys/module/kvm/version")
		if err == nil {
			info.Version = strings.TrimSpace(string(data))
		}

		// Check for features
		info.Features = []string{"virtio", "vhost"}

		// Check for nested virtualization
		if data, err := os.ReadFile("/sys/module/kvm_intel/parameters/nested"); err == nil {
			if strings.TrimSpace(string(data)) == "Y" {
				info.Features = append(info.Features, "nested_virtualization")
			}
		}
		if data, err := os.ReadFile("/sys/module/kvm_amd/parameters/nested"); err == nil {
			if strings.TrimSpace(string(data)) == "1" {
				info.Features = append(info.Features, "nested_virtualization")
			}
		}
	}

	return info, nil
}

// detectFirewallEnabledImpl detects firewall status on Linux.
func detectFirewallEnabledImpl() bool {
	// Check iptables
	cmd := exec.Command("iptables", "-L", "-n")
	output, err := cmd.Output()
	if err == nil {
		// If iptables has any rules, firewall is considered enabled
		lines := strings.Split(string(output), "\n")
		if len(lines) > 8 { // More than just headers
			return true
		}
	}

	// Check ufw
	cmd = exec.Command("ufw", "status")
	output, err = cmd.Output()
	if err == nil {
		if strings.Contains(string(output), "Status: active") {
			return true
		}
	}

	// Check firewalld
	cmd = exec.Command("firewall-cmd", "--state")
	output, err = cmd.Output()
	if err == nil {
		if strings.Contains(string(output), "running") {
			return true
		}
	}

	return false
}

// detectVirtualizationEnabledImpl detects if virtualization is enabled on Linux.
func detectVirtualizationEnabledImpl() bool {
	// Check if /dev/kvm exists and is accessible
	if _, err := os.Stat("/dev/kvm"); err == nil {
		// Try to open it
		f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
		if err == nil {
			f.Close()
			return true
		}
	}

	// Check CPU flags
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}

	cpuinfo := string(data)
	// Intel VT-x
	if strings.Contains(cpuinfo, "vmx") {
		return true
	}

	// AMD-V
	if strings.Contains(cpuinfo, "svm") {
		return true
	}

	return false
}

// GetElectricityRate attempts to get electricity rate based on location.
func GetElectricityRate() float64 {
	// Future: Use IP geolocation + public utility data
	return 0.0
}

// MeasurePowerDraw attempts to measure system power draw on Linux.
func MeasurePowerDraw() (idle, load float64, err error) {
	// Try Intel RAPL (Running Average Power Limit)
	raplPath := "/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj"

	if _, err := os.Stat(raplPath); err == nil {
		// RAPL is available
		// Read energy counter before and after a delay
		data1, err := os.ReadFile(raplPath)
		if err != nil {
			return 0, 0, err
		}

		// Validate the counter is readable before returning the "needs sampling" error.
		if _, err := strconv.ParseUint(strings.TrimSpace(string(data1)), 10, 64); err != nil {
			return 0, 0, err
		}

		// RAPL gives a one-shot counter; meaningful power draw requires two
		// readings separated by a time interval. Return an error so the caller
		// falls back to the estimation path.
		return 0, 0, fmt.Errorf("RAPL requires continuous sampling")
	}

	// Try sensors command
	cmd := exec.Command("sensors")
	output, err := cmd.Output()
	if err == nil {
		// Parse sensors output for power readings
		// This varies by hardware
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "power") || strings.Contains(line, "Power") {
				// Found power reading, but parsing is complex
				// Return error to use estimation
				return 0, 0, fmt.Errorf("sensors available but parsing not implemented")
			}
		}
	}

	return 0, 0, fmt.Errorf("no power measurement method available")
}
