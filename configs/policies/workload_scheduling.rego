package soholink.scheduling

# Workload scheduling policies for FedScheduler placement decisions.
# Providers define rules that determine whether a job can be placed on their node.
#
# Example usage in scheduler:
#   input := &AuthzInput{
#     WorkloadID: "job-123",
#     EstimatedDuration: 3600,  # seconds
#     Node: &NodeState{
#       NodeDID: "did:key:z6Mki...",
#       GPUTemperature: 75.5,
#       GPULoad: "high",
#     },
#   }
#   result := policyEngine.EvaluateWorkloadScheduling(ctx, input)

# Default: allow placement unless explicitly denied
default allow_placement = true

# Deny reasons for debugging
deny_reasons contains reason if {
    not allow_placement
    reason := "placement_denied_by_policy"
}

# ── Thermal Budget Protection ──────────────────────────────────────
# Providers can set thermal thresholds to protect hardware from degradation.
# This prevents long-running jobs on nodes that are already hot.

# Example thermal policy (providers can override with custom thresholds):
# - Reject jobs > 2 hours if GPU is already at 75°C or higher
# - Reject jobs > 4 hours if GPU is at 70°C or higher
# - Reject any job if GPU is > 85°C (thermal throttling zone)

# Deny placement if GPU is in thermal throttling zone (>85°C) for any job
allow_placement = false if {
    input.node.is_gpu_node == true
    input.node.gpu_temperature > 85.0
}

deny_reasons contains reason if {
    input.node.is_gpu_node == true
    input.node.gpu_temperature > 85.0
    reason := "gpu_thermal_throttling_zone"
}

# Deny long-running jobs on warm nodes
allow_placement = false if {
    input.node.is_gpu_node == true
    input.node.gpu_temperature > 75.0
    input.estimated_duration_seconds > 7200  # > 2 hours
    input.node.gpu_load != "idle"
}

deny_reasons contains reason if {
    input.node.is_gpu_node == true
    input.node.gpu_temperature > 75.0
    input.estimated_duration_seconds > 7200
    input.node.gpu_load != "idle"
    reason := "gpu_thermal_protect_long_job_warm_node"
}

# Deny medium-long jobs on moderately warm nodes
allow_placement = false if {
    input.node.is_gpu_node == true
    input.node.gpu_temperature > 70.0
    input.estimated_duration_seconds > 14400  # > 4 hours
    input.node.gpu_load != "idle"
}

deny_reasons contains reason if {
    input.node.is_gpu_node == true
    input.node.gpu_temperature > 70.0
    input.estimated_duration_seconds > 14400
    input.node.gpu_load != "idle"
    reason := "gpu_thermal_protect_extended_job_moderate_temp"
}

# ── Resource Availability ──────────────────────────────────────────
# Deny if node doesn't have enough CPU or memory

allow_placement = false if {
    input.required_cpu > 0
    input.node.available_cpu < input.required_cpu
}

deny_reasons contains reason if {
    input.required_cpu > 0
    input.node.available_cpu < input.required_cpu
    reason := "insufficient_cpu"
}

allow_placement = false if {
    input.required_memory_mb > 0
    input.node.available_memory_mb < input.required_memory_mb
}

deny_reasons contains reason if {
    input.required_memory_mb > 0
    input.node.available_memory_mb < input.required_memory_mb
    reason := "insufficient_memory"
}

# ── Load-based Scheduling ──────────────────────────────────────────
# Prefer routing to less-loaded nodes when possible.
# This is informational for the scheduler's bandit, not a hard denial.

# Helper to compute load score (lower is better)
load_score = input.node.current_job_count * 10 + input.node.gpu_temperature

# Prefer placing on idle/low-load nodes for short jobs
scheduling_preference if {
    input.estimated_duration_seconds < 1800  # < 30 minutes
    input.node.gpu_load == "idle"
}
