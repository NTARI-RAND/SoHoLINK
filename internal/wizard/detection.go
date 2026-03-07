package wizard

import (
	"fmt"
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// DetectSystemCapabilities performs comprehensive system detection.
func DetectSystemCapabilities() (*SystemCapabilities, error) {
	caps := &SystemCapabilities{}

	var err error

	// Detect OS
	caps.OS, err = detectOS()
	if err != nil {
		return nil, fmt.Errorf("failed to detect OS: %w", err)
	}

	// Detect CPU
	caps.CPU, err = detectCPU()
	if err != nil {
		return nil, fmt.Errorf("failed to detect CPU: %w", err)
	}

	// Detect Memory
	caps.Memory, err = detectMemory()
	if err != nil {
		return nil, fmt.Errorf("failed to detect memory: %w", err)
	}

	// Detect Storage
	caps.Storage, err = detectStorage()
	if err != nil {
		return nil, fmt.Errorf("failed to detect storage: %w", err)
	}

	// Detect GPU (optional, may fail)
	caps.GPU = detectGPU()

	// Detect Hypervisor
	caps.Hypervisor, err = detectHypervisor()
	if err != nil {
		return nil, fmt.Errorf("failed to detect hypervisor: %w", err)
	}

	// Detect Network
	caps.Network, err = detectNetwork()
	if err != nil {
		return nil, fmt.Errorf("failed to detect network: %w", err)
	}

	// Detect Virtualization
	caps.Virtualization, err = detectVirtualization()
	if err != nil {
		return nil, fmt.Errorf("failed to detect virtualization: %w", err)
	}

	return caps, nil
}

// detectOS detects operating system information.
func detectOS() (OSInfo, error) {
	info, err := host.Info()
	if err != nil {
		return OSInfo{}, err
	}

	return OSInfo{
		Platform:     info.OS,
		Distribution: fmt.Sprintf("%s %s", info.Platform, info.PlatformVersion),
		Version:      info.PlatformVersion,
		Architecture: runtime.GOARCH,
		Kernel:       info.KernelVersion,
	}, nil
}

// detectCPU detects CPU information.
func detectCPU() (CPUInfo, error) {
	cpuInfo, err := cpu.Info()
	if err != nil {
		return CPUInfo{}, err
	}

	if len(cpuInfo) == 0 {
		return CPUInfo{}, fmt.Errorf("no CPU information available")
	}

	// Get logical CPU count
	logicalCores := runtime.NumCPU()

	// Get physical cores (may not be available on all platforms)
	physicalCores, _ := cpu.Counts(false)
	if physicalCores == 0 {
		physicalCores = logicalCores
	}

	info := CPUInfo{
		Model:    cpuInfo[0].ModelName,
		Vendor:   cpuInfo[0].VendorID,
		Cores:    physicalCores,
		Threads:  logicalCores,
		FrequencyMHz: cpuInfo[0].Mhz,
	}

	// Detect virtualization technology
	info.VirtualizationTech = detectCPUVirtualizationTech(cpuInfo[0].VendorID, cpuInfo[0].Flags)

	return info, nil
}

// detectCPUVirtualizationTech detects CPU virtualization technology from flags.
func detectCPUVirtualizationTech(vendor string, flags []string) string {
	flagSet := make(map[string]bool)
	for _, flag := range flags {
		flagSet[flag] = true
	}

	// Intel VT-x
	if flagSet["vmx"] {
		return "VT-x (Intel)"
	}

	// AMD-V
	if flagSet["svm"] {
		return "AMD-V (AMD)"
	}

	// ARM
	if vendor == "ARM" && runtime.GOARCH == "arm64" {
		return "ARM Virtualization"
	}

	return "Unknown/Not Supported"
}

// detectMemory detects memory information.
func detectMemory() (MemoryInfo, error) {
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return MemoryInfo{}, err
	}

	return MemoryInfo{
		TotalGB:     float64(vmem.Total) / (1024 * 1024 * 1024),
		AvailableGB: float64(vmem.Available) / (1024 * 1024 * 1024),
		UsedGB:      int(vmem.Used / (1024 * 1024 * 1024)), // #nosec G115 -- disk size in GB never exceeds MaxInt on any supported platform
		UsedPercent: vmem.UsedPercent,
	}, nil
}

// detectStorage detects storage information.
func detectStorage() (StorageInfo, error) {
	// Detect primary partition
	var mountPoint string
	switch runtime.GOOS {
	case "windows":
		mountPoint = "C:\\"
	case "darwin":
		mountPoint = "/"
	default: // linux
		mountPoint = "/"
	}

	usage, err := disk.Usage(mountPoint)
	if err != nil {
		return StorageInfo{}, err
	}

	return StorageInfo{
		TotalGB:     float64(usage.Total) / (1024 * 1024 * 1024),
		AvailableGB: float64(usage.Free) / (1024 * 1024 * 1024),
		UsedGB:      int(usage.Used / (1024 * 1024 * 1024)), // #nosec G115 -- disk size in GB never exceeds MaxInt on any supported platform
		UsedPercent: usage.UsedPercent,
		Filesystem:  usage.Fstype,
		MountPoint:  mountPoint,
		DriveType:   detectDriveType(), // Platform-specific
	}, nil
}

// detectDriveType attempts to detect drive type (SSD/HDD/NVMe).
// Implementation is platform-specific in detection_*.go files.
func detectDriveType() string {
	// Implemented in platform-specific files
	return detectDriveTypeImpl()
}

// detectGPU attempts to detect GPU information.
// Returns nil if no GPU detected or detection fails.
func detectGPU() *GPUInfo {
	// Platform-specific implementation
	return detectGPUImpl()
}

// detectHypervisor detects hypervisor availability and status.
func detectHypervisor() (HypervisorInfo, error) {
	// Platform-specific implementation
	return detectHypervisorImpl()
}

// detectNetwork detects network configuration.
func detectNetwork() (NetworkInfo, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return NetworkInfo{}, err
	}

	netInfo := NetworkInfo{
		Interfaces: make([]NetworkInterface, 0, len(interfaces)),
	}

	maxSpeed := 0
	for _, iface := range interfaces {
		if iface.Name == "lo" || iface.Name == "Loopback" {
			continue // Skip loopback
		}

		netIface := NetworkInterface{
			Name:        iface.Name,
			MACAddress:  iface.HardwareAddr,
			IPAddresses: make([]string, len(iface.Addrs)),
			IsUp:        len(iface.Flags) > 0, // Check if flags exist
			IsLoopback:  false,
		}

		for i, addr := range iface.Addrs {
			netIface.IPAddresses[i] = addr.Addr
		}

		netInfo.Interfaces = append(netInfo.Interfaces, netIface)

		// Track max speed
		// Note: gopsutil doesn't provide speed, we estimate
		if netIface.IsUp && !netIface.IsLoopback {
			maxSpeed = 1000 // Assume 1 Gbps for active interfaces
		}
	}

	netInfo.BandwidthMbps = maxSpeed

	// Detect firewall status (platform-specific)
	netInfo.FirewallEnabled = detectFirewallEnabled()

	return netInfo, nil
}

// detectVirtualization detects virtualization support.
func detectVirtualization() (VirtualizationInfo, error) {
	cpuInfo, err := cpu.Info()
	if err != nil {
		return VirtualizationInfo{}, err
	}

	if len(cpuInfo) == 0 {
		return VirtualizationInfo{
			Supported: false,
			Enabled:   false,
		}, nil
	}

	flags := cpuInfo[0].Flags
	flagSet := make(map[string]bool)
	for _, flag := range flags {
		flagSet[flag] = true
	}

	// Check for virtualization support
	supported := flagSet["vmx"] || flagSet["svm"]
	tech := detectCPUVirtualizationTech(cpuInfo[0].VendorID, flags)

	// Check if enabled (requires platform-specific detection)
	enabled := detectVirtualizationEnabled()

	return VirtualizationInfo{
		Supported:     supported,
		Enabled:       enabled,
		Technology:    tech,
		NestedSupport: flagSet["ept"] || flagSet["npt"], // Intel EPT or AMD NPT
	}, nil
}

// CalculateAvailableResources calculates allocatable resources.
// Strategy: Reserve 50% for host system, allocate 50% to marketplace.
func (s *SystemCapabilities) CalculateAvailableResources() *ResourceAllocation {
	alloc := &ResourceAllocation{
		TotalCPUCores:  s.CPU.Cores,
		TotalMemoryGB:  int(s.Memory.TotalGB),
		TotalStorageGB: int(s.Storage.TotalGB),
	}

	// CPU: Reserve 50% for host
	alloc.ReservedCores = s.CPU.Cores / 2
	alloc.AllocatableCores = s.CPU.Cores - alloc.ReservedCores

	// Memory: Reserve 50% for host
	alloc.ReservedMemoryGB = int(s.Memory.TotalGB / 2)
	alloc.AllocatableMemoryGB = int(s.Memory.TotalGB) - alloc.ReservedMemoryGB

	// Storage: Reserve at least 200GB for host, allocate rest
	minReserved := 200
	if s.Storage.TotalGB < 400 {
		// If less than 400GB total, reserve 50%
		alloc.ReservedStorageGB = int(s.Storage.TotalGB / 2)
	} else {
		alloc.ReservedStorageGB = minReserved
	}
	alloc.AllocatableStorageGB = int(s.Storage.TotalGB) - alloc.ReservedStorageGB

	// Calculate max VMs
	// Limited by CPU (assume 4 cores per VM) or Memory (assume 4GB per VM)
	maxVMsByCPU := alloc.AllocatableCores / 4
	maxVMsByMemory := alloc.AllocatableMemoryGB / 4

	if maxVMsByCPU < maxVMsByMemory {
		alloc.MaxVMs = maxVMsByCPU
	} else {
		alloc.MaxVMs = maxVMsByMemory
	}

	// Cap at reasonable maximum
	if alloc.MaxVMs > 20 {
		alloc.MaxVMs = 20
	}

	// GPU
	alloc.HasGPU = s.GPU != nil
	alloc.GPUAllocatable = false // Conservative default

	return alloc
}

// ValidateProviderCapability checks if system can be a provider.
func (s *SystemCapabilities) ValidateProviderCapability() error {
	// Check virtualization
	if !s.Virtualization.Supported {
		return fmt.Errorf("CPU does not support virtualization")
	}

	if !s.Virtualization.Enabled {
		return fmt.Errorf("virtualization is supported but not enabled in BIOS")
	}

	// Check hypervisor
	if !s.Hypervisor.Installed {
		return fmt.Errorf("no hypervisor installed (need Hyper-V, KVM, or similar)")
	}

	// Check minimum resources
	if s.CPU.Cores < 4 {
		return fmt.Errorf("minimum 4 CPU cores required (found %d)", s.CPU.Cores)
	}

	if s.Memory.TotalGB < 8 {
		return fmt.Errorf("minimum 8 GB RAM required (found %.1f GB)", s.Memory.TotalGB)
	}

	if s.Storage.AvailableGB < 100 {
		return fmt.Errorf("minimum 100 GB free storage required (found %.1f GB)", s.Storage.AvailableGB)
	}

	return nil
}

// Platform-specific functions (implemented in detection_windows.go, detection_linux.go, etc.)

// detectFirewallEnabled detects if firewall is enabled.
func detectFirewallEnabled() bool {
	return detectFirewallEnabledImpl()
}

// detectVirtualizationEnabled detects if virtualization is enabled.
func detectVirtualizationEnabled() bool {
	return detectVirtualizationEnabledImpl()
}
