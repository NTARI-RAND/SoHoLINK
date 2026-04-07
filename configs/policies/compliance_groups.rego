package soholink.compliance

# Compliance group membership policies for SoHoLINK federation nodes.
#
# These rules define the criteria for each compliance tier. They are evaluated
# by the ComplianceManager at check time to determine a node's eligible groups.
#
# Input schema:
#   input.node.compliance_level        — current level ("baseline", "high-security", etc.)
#   input.node.reputation_score        — integer 0–100
#   input.node.uptime_percent          — float 0.0–100.0
#   input.node.has_network_isolation   — bool (CLONE_NEWNET or Hyper-V active)
#   input.node.firewall_active         — bool (iptables / netsh rules applied)
#   input.node.gpu_model               — string, empty if no GPU
#   input.node.region                  — string, e.g. "us-east-1"
#
# Each group_* rule evaluates to true when the node qualifies.
# price_premium is a fractional multiplier (1.0 = no premium).

import future.keywords.if
import future.keywords.in

# ── Baseline ──────────────────────────────────────────────────────────────────
# All nodes that pass minimum liveness and reputation checks.

default group_baseline := false

group_baseline if {
    input.node.reputation_score >= 20
    input.node.uptime_percent >= 90
}

baseline_price_premium := 1.0

# ── High-Security ──────────────────────────────────────────────────────────────
# Nodes with strong reputation, active network isolation, and firewall rules.
# Price premium: 15% above baseline.

default group_high_security := false

group_high_security if {
    group_baseline
    input.node.has_network_isolation == true
    input.node.firewall_active == true
    input.node.reputation_score >= 75
}

high_security_price_premium := 1.15

# ── Data-Residency ─────────────────────────────────────────────────────────────
# Nodes located in regions with data-residency requirements (e.g. EU-GDPR).
# Operator assigns this group explicitly; OPA validates the region.
# Price premium: 25% above baseline.

default group_data_residency := false

group_data_residency if {
    group_baseline
    input.node.compliance_level == "data-residency"
}

data_residency_price_premium := 1.25

# ── GPU Tier ──────────────────────────────────────────────────────────────────
# Nodes with a dedicated GPU available for compute workloads.
# Price premium: 30% above baseline.

default group_gpu_tier := false

group_gpu_tier if {
    group_baseline
    input.node.gpu_model != ""
}

gpu_tier_price_premium := 1.30

# ── Resolved group membership ──────────────────────────────────────────────────
# Returns the set of groups the node qualifies for.

groups[g] if {
    group_baseline
    g := "baseline"
}

groups[g] if {
    group_high_security
    g := "high-security"
}

groups[g] if {
    group_data_residency
    g := "data-residency"
}

groups[g] if {
    group_gpu_tier
    g := "gpu-tier"
}

# ── Price multiplier ───────────────────────────────────────────────────────────
# Returns the highest applicable price multiplier for this node.

price_multiplier := high_security_price_premium if {
    group_high_security
} else := data_residency_price_premium if {
    group_data_residency
} else := gpu_tier_price_premium if {
    group_gpu_tier
} else := baseline_price_premium
