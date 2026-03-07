package wizard

import "time"

// SystemCapabilities represents detected system hardware and software.
type SystemCapabilities struct {
	OS             OSInfo
	CPU            CPUInfo
	Memory         MemoryInfo
	Storage        StorageInfo
	GPU            *GPUInfo // Optional
	Hypervisor     HypervisorInfo
	Network        NetworkInfo
	Virtualization VirtualizationInfo
}

// OSInfo contains operating system details.
type OSInfo struct {
	Platform     string // "windows", "linux", "darwin"
	Distribution string // "Windows 11 Pro", "Ubuntu 22.04", etc.
	Version      string
	Architecture string // "amd64", "arm64"
	Kernel       string
}

// CPUInfo contains CPU details.
type CPUInfo struct {
	Model              string
	Vendor             string
	Cores              int
	Threads            int
	FrequencyMHz       float64
	VirtualizationTech string // "VT-x", "AMD-V", etc.
}

// MemoryInfo contains RAM details.
type MemoryInfo struct {
	TotalGB     float64
	AvailableGB float64
	UsedGB      int
	UsedPercent float64
}

// StorageInfo contains storage details.
type StorageInfo struct {
	TotalGB      float64
	AvailableGB  float64
	UsedGB       int
	UsedPercent  float64
	Filesystem   string
	MountPoint   string
	DriveType    string // "SSD", "HDD", "NVMe"
}

// GPUInfo contains GPU details (if present).
type GPUInfo struct {
	Model             string
	Vendor            string
	VRAMGb            int
	ComputeCapability string
	DriverVersion     string
}

// HypervisorInfo contains hypervisor details.
type HypervisorInfo struct {
	Type      string // "hyper-v", "kvm", "none"
	Version   string
	Installed bool
	Enabled   bool
	Features  []string
}

// NetworkInfo contains network details.
type NetworkInfo struct {
	Interfaces      []NetworkInterface
	BandwidthMbps   int
	PublicIP        string
	PrivateIP       string
	FirewallEnabled bool
}

// NetworkInterface represents a network interface.
type NetworkInterface struct {
	Name         string
	MACAddress   string
	IPAddresses  []string
	IsUp         bool
	IsLoopback   bool
	SpeedMbps    int
}

// VirtualizationInfo contains virtualization support details.
type VirtualizationInfo struct {
	Supported     bool
	Enabled       bool
	Technology    string // "VT-x", "AMD-V", "Apple Virtualization"
	NestedSupport bool
}

// ResourceAllocation represents allocatable resources for the marketplace.
type ResourceAllocation struct {
	TotalCPUCores      int
	AllocatableCores   int
	ReservedCores      int

	TotalMemoryGB      int
	AllocatableMemoryGB int
	ReservedMemoryGB   int

	TotalStorageGB      int
	AllocatableStorageGB int
	ReservedStorageGB   int

	MaxVMs             int
	HasGPU             bool
	GPUAllocatable     bool
}

// CostProfile represents user's operating costs.
type CostProfile struct {
	// Electricity
	ElectricityRatePerKWh float64
	BasePowerWatts        float64
	LoadPowerWatts        float64
	PowerCostPerHour      float64

	// Cooling
	HasExtraCooling    bool
	CoolingCostPerHour float64

	// Depreciation
	HardwareCost          float64
	HardwareLifespanYears float64
	DepreciationPerHour   float64

	// Total
	TotalCostPerHour float64
}

// PricingConfig represents pricing configuration.
type PricingConfig struct {
	PerVMPerHour      float64
	Currency          string
	ProfitMarginPercent float64
	PriceMode         string // "competitive", "premium", "cost-recovery", "custom"
}

// MarketRates represents current marketplace pricing.
type MarketRates struct {
	Min       float64
	P25       float64
	Median    float64
	P75       float64
	Max       float64
	Count     int
	Timestamp time.Time
}

// AWSComparison compares pricing to AWS equivalent.
type AWSComparison struct {
	InstanceType   string
	AWSPrice       float64
	YourPrice      float64
	SavingsPercent float64
}

// WizardConfig is the complete configuration from wizard.
type WizardConfig struct {
	Mode             string // "provider", "consumer", "both"
	Resources        ResourceAllocation
	Pricing          PricingConfig
	CostProfile      CostProfile
	NetworkMode      string // "public", "private"
	AutoAccept       bool
	Policies         PolicyConfig

	// Generated paths
	ConfigPath       string
	DependencyReport string
	IdentityPath     string
}

// PolicyConfig represents resource allocation policies.
type PolicyConfig struct {
	MaxVMsPerCustomer      int
	MaxCPUCoresPerVM       int
	MaxMemoryPerVMGB       int
	MaxStoragePerVMGB      int
	MinContractLeadTime    string
	MaxContractDuration    string
	RequireSignatures      bool
	RateLimitingEnabled    bool
}

// DependencyReport documents system configuration.
type DependencyReport struct {
	Timestamp    time.Time
	Platform     OSInfo
	Hardware     HardwareInfo
	Hypervisor   HypervisorInfo
	Network      NetworkInfo
	Dependencies []Dependency
	CostProfile  CostProfile
	Pricing      PricingConfig
}

// HardwareInfo aggregates all hardware details.
type HardwareInfo struct {
	CPU     CPUInfo
	Memory  MemoryInfo
	Storage StorageInfo
	GPU     *GPUInfo
}

// Dependency represents a system dependency.
type Dependency struct {
	Name     string
	Type     string // "required", "optional"
	Status   string // "installed", "missing", "not_applicable"
	Version  string
	Platform string // Platform requirement, e.g., "linux_only"
}

// WizardStep represents a step in the wizard.
type WizardStep int

const (
	StepWelcome WizardStep = iota
	StepDetection
	StepCostMeasurement
	StepDepreciation
	StepPricing
	StepIdentity
	StepNetwork
	StepDependencies
	StepPolicies
	StepReview
	StepComplete
)

// String returns the step name.
func (s WizardStep) String() string {
	names := []string{
		"Welcome",
		"Detection",
		"Cost Measurement",
		"Depreciation",
		"Pricing",
		"Identity",
		"Network",
		"Dependencies",
		"Policies",
		"Review",
		"Complete",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return "Unknown"
}

// Progress returns step number and total steps.
func (s WizardStep) Progress() (current, total int) {
	return int(s) + 1, int(StepComplete) + 1
}
