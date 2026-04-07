package orchestration

import (
	"time"
)

// GPUProfile contains detailed GPU hardware information for scheduling decisions.
type GPUProfile struct {
	Model              string  // e.g. "RTX 4090", "A100"
	VRAMFree           int64   // MB
	VRAMTotal          int64   // MB
	ComputeCapability  string  // e.g. "8.6", "9.0" for NVIDIA
	Temperature        float32 // Celsius
	PCIeBandwidth      int64   // MB/s (theoretical)
	CUDASupportLevel   string  // "cuda:8.6", "cuda:9.0", etc.
}

// CapabilitySet describes the execution environments and features a node supports.
// Used for pre-dispatch capability negotiation to prevent silent job failures.
type CapabilitySet struct {
	// Runtime environments
	RuntimesSupported []string // "wasm", "container", "vm", "unikernel"

	// Accelerators / libraries
	AcceleratorsSupported []string // "cuda", "cudnn", "openmpi", "vulkan", etc.

	// Language runtimes
	PythonVersions []string // ["3.11", "3.10", "3.9"]
	NodeVersions   []string // ["18.0", "20.0"]
	JavaVersions   []string // ["11", "17", "21"]

	// Network policies
	NetworkIsolation string // "none", "restricted", "isolated"
	OutboundAllowed  bool   // Can job access external networks?

	// Storage
	PersistentStorageSupported bool
	StorageQuotaMB             int64

	// Attestation
	AttestationLevel string // "none", "basic", "enhanced"
	LastCheckedAt    time.Time
}

// Node represents a federation node that can accept workloads.
type Node struct {
	DID     string
	Address string
	Region  string

	// Capacity
	TotalCPU          float64
	AvailableCPU      float64
	TotalMemoryMB     int64
	AvailableMemoryMB int64
	TotalDiskGB       int64
	AvailableDiskGB   int64

	// GPU
	HasGPU     bool
	GPUProfile *GPUProfile // Detailed GPU profiling (nil if no GPU)

	// Execution environment capabilities
	Capabilities *CapabilitySet // Node's supported runtimes, accelerators, etc.

	// Network
	BandwidthMbps int
	LatencyMs     int // Measured from central SOHO

	// Pricing
	PricePerCPUHour int64 // Cents
	PricePerGBMonth int64

	// Reputation
	ReputationScore int // 0-100
	UptimePercent   float64
	FailureRate     float64 // Fraction of failed jobs

	// Status
	Status        string // "online", "busy", "offline"
	LastHeartbeat time.Time

	// Compliance (Phase 2)
	ComplianceLevel string // "baseline", "high-security", "data-residency", "gpu-tier"
	ComplianceGroup string // e.g. "US-East-Secure", "EU-GDPR"
	SLATier         string // "best-effort", "standard", "premium"
}

// NodeQuery describes filter criteria for node discovery.
type NodeQuery struct {
	MinCPU               float64
	MinMemory            int64
	MinDisk              int64
	GPURequired          bool
	GPUModel             string
	MinGPUVRAM           int64  // Minimum GPU VRAM in MB (0 = no requirement)
	MinComputeCapability string // e.g. "8.6" for NVIDIA (empty = no requirement)
	Regions              []string
	MinReputation        int
	MaxCostPerHour       int64

	// Compliance filters (Phase 2)
	ComplianceGroup string // filter to nodes in a specific compliance group
	SLATier         string // filter to nodes with a minimum SLA tier
}

// NodeCapacity is a snapshot of a node's available resources.
type NodeCapacity struct {
	NodeDID       string
	AvailableCPU  float64
	AvailableMem  int64
	AvailableDisk int64
	ActiveJobs    int
}

// DeployRequest is sent to a node's worker API to deploy a workload replica.
type DeployRequest struct {
	PlacementID string
	WorkloadID  string
	Spec        WorkloadSpec
	HealthCheck HealthCheckConfig
}
