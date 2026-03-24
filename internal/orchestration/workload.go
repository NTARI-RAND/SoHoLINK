// Package orchestration implements the FedScheduler — a Kubernetes-inspired
// elastic orchestrator that distributes workloads across federated SOHO nodes.
package orchestration

import (
	"time"
)

// Workload represents a user-submitted unit of work that the FedScheduler
// places onto one or more federation nodes.
type Workload struct {
	WorkloadID  string
	Name        string
	OwnerDID    string
	Type        string // "container", "vm", "function", "service"

	Spec        WorkloadSpec
	Constraints PlacementConstraints

	Replicas    int
	AutoScale   *AutoScalePolicy

	Status       string // "pending", "scheduling", "running", "scaling", "failed", "stopped"
	DesiredState string // "running", "stopped"

	HealthCheck HealthCheckConfig

	SLA *SLARef // Optional SLA contract reference

	CreatedAt time.Time
	UpdatedAt time.Time
}

// SLARef links a workload to an SLA contract.
type SLARef struct {
	ContractID string
}

// WorkloadSpec defines the resource and runtime requirements.
type WorkloadSpec struct {
	// Compute
	CPUCores    float64 // 0.5 = 500m, 2.0 = 2 cores
	MemoryMB    int64
	GPURequired bool
	GPUModel    string // "RTX 4090", "A100", etc.

	// Storage
	DiskGB      int64
	StorageType string // "ephemeral", "persistent"

	// Network
	NetworkMbps int
	PublicIP    bool
	Ports       []PortMapping

	// Image / code
	Image      string
	Entrypoint []string
	Environment map[string]string

	// Execution environment requirements (for capability negotiation)
	RuntimeRequired    string   // "wasm", "container", "vm" (required runtime)
	GPUComputeMin      string   // e.g. "8.6" (minimum CUDA compute capability)
	GPUMemoryMinMB     int64    // Minimum GPU VRAM required in MB
	AcceleratorsNeeded []string // e.g. ["cuda", "cudnn", "vulkan"]
	PythonVersion      string   // e.g. "3.11", "3.10" (if applicable)
	NetworkPolicy      string   // "outbound_allowed", "outbound_denied", "restricted"

	// Runtime
	Timeout    time.Duration
	MaxRetries int
}

// PortMapping maps a container port to a host protocol.
type PortMapping struct {
	ContainerPort int
	HostPort      int
	Protocol      string // "tcp", "udp"
}

// PlacementConstraints control where workloads are scheduled.
type PlacementConstraints struct {
	// Geographic
	Regions        []string
	ExcludeRegions []string
	MaxLatencyMs   int

	// Resource quality
	MinProviderScore   int
	PreferredProviders []string
	ExcludedProviders  []string

	// Cost
	MaxCostPerHour int64 // Cents

	// Affinity / anti-affinity
	Affinity     *Affinity
	AntiAffinity *Affinity
}

// Affinity expresses co-location or separation preferences.
type Affinity struct {
	WorkloadLabels map[string]string
	Strength       string // "required", "preferred"
}

// AutoScalePolicy governs horizontal scaling of workload replicas.
type AutoScalePolicy struct {
	Enabled     bool
	MinReplicas int
	MaxReplicas int

	// Triggers
	TargetCPU     float64 // Scale when avg CPU > this (0.0–1.0)
	TargetMemory  float64
	TargetLatency int // Scale when p95 latency > Xms

	// Cool-down
	ScaleUpCooldown   time.Duration
	ScaleDownCooldown time.Duration
}

// HealthCheckConfig defines how the scheduler monitors replica health.
type HealthCheckConfig struct {
	Type             string // "http", "tcp", "exec"
	Endpoint         string // "/health" or port number
	IntervalSeconds  int
	TimeoutSeconds   int
	FailureThreshold int
}

// WorkloadState tracks runtime state for an active workload.
//
// W1 — duplicate field note: WorkloadID, Status, DesiredReplicas, CreatedAt,
// and UpdatedAt are top-level convenience aliases that mirror the embedded
// Workload pointer's fields.  When Workload != nil (always the case for
// actively-scheduled workloads) the authoritative values live on Workload;
// the top-level fields are a snapshot populated at construction time and may
// diverge if Workload is mutated after WorkloadState is created.
// New code should read WorkloadState.Workload.* directly to avoid stale data.
type WorkloadState struct {
	// Convenience fields (mirror Workload fields for direct access).
	// See W1 doc note above — prefer WorkloadState.Workload.* for live values.
	WorkloadID      string
	Status          string
	DesiredReplicas int
	CreatedAt       time.Time
	UpdatedAt       time.Time

	Workload   *Workload
	Placements []Placement
	Health     HealthStatus
	Metrics    WorkloadMetrics

	// Mobile segmentation fields — populated when the workload is split into
	// short segments for execution on battery-powered mobile nodes.
	// CheckpointData is the latest serialised execution checkpoint; the
	// scheduler stores it so that a replacement desktop node can resume from
	// the last completed segment if the mobile node disappears mid-task.
	CheckpointData []byte `json:"checkpoint_data,omitempty"`

	// SegmentIndex is the index of the segment currently in-flight (0-based).
	SegmentIndex int `json:"segment_index,omitempty"`

	// SegmentCount is the total number of segments the task was split into.
	// 0 means the task has not been segmented (desktop-only execution).
	SegmentCount int `json:"segment_count,omitempty"`
}

// Placement records where a single replica is running.
type Placement struct {
	PlacementID     string
	WorkloadID      string
	ReplicaNum      int
	NodeDID         string
	NodeAddress     string
	Status          string // "pending", "running", "failed"
	Performance     float64
	StartedAt       time.Time
	LastHealthCheck time.Time
}

// HealthStatus is the aggregate health of a workload.
type HealthStatus struct {
	Status  string // "healthy", "degraded", "unhealthy"
	Details string
}

// WorkloadMetrics holds aggregated runtime metrics.
type WorkloadMetrics struct {
	AvgCPUPercent    float64
	AvgMemoryPercent float64
	P95LatencyMs     int
	RequestsPerSec   float64
}
