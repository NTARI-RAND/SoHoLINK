# SoHoLINK Development Roadmap

**Last Updated:** 2026-03-24
**Status:** Sprint 3 – Orchestration Enhancements
**Base Version:** 0.1.2 (Network isolation & security foundation)

---

## Context: Architectural Guidance Papers

### Paper 1: "From Voltage to Vision: How Chips Decode Reality"

Educational primer on hardware fundamentals relevant to understanding SoHoLINK's heterogeneous node pool:

- **CPU fundamentals:** Instruction pipeline, ALU, clock synchronization
- **GPU architecture:** Massive parallelism vs. sequential CPU reasoning; shader cores; VRAM as a schedulable dimension
- **Thermal behavior:** Heat dissipation under sustained load; throttling mechanics
- **Data paths:** How voltage patterns become actionable information at scale

**Application to SoHoLINK:** Foundational knowledge for intelligent GPU profiling, thermal budgeting, and capability negotiation in the FedScheduler.

---

### Paper 2: "Improving the Nervous System: CPU/GPU Network Orchestration and What SoHoLINK Can Teach Us"

Directly addresses seven improvement vectors for SoHoLINK's scheduler and federation model. **Aligned with current architecture; no rebuild required.** Each improvement extends existing subsystems.

---

## Current Development Status (Sprint 2 Completion)

### ✓ In Progress (Active Branches)

1. **Network Isolation & Security** (`firewall.go`, `workload_executor.go`)
   - `PortManager` — platform-specific firewall rules (iptables/Linux, netsh/Windows)
   - `WorkloadExecutor` — network namespace isolation (CLONE_NEWNET/Linux, Hyper-V/Windows)
   - Per-workload port and network isolation

2. **Default Authorization Policy** (`default.rego`)
   - Base OPA rules for AAA
   - Authentication context propagation

3. **FedScheduler Integration** (`scheduler.go`, `scheduler_api.go`)
   - Wiring isolation constraints into placement decisions
   - Compatibility with existing LinUCB bandit

---

## Sprint 3: Orchestration Enhancements

The following six improvements extend the FedScheduler and node coordination layer. **They build on what exists; no architectural rewrite needed.**

### 1. GPU Profiling & Schedulable Dimensions

**Current:** Marketplace listing only. Scheduler treats all GPU nodes as interchangeable.

**Goal:** Extend node heartbeat to advertise GPU capabilities; make VRAM, compute capability, temperature, and PCIe bandwidth schedulable dimensions.

**Implementation:**
- Add `gpu_profile` struct to node heartbeat:
  ```go
  type GPUProfile struct {
    VRAMFree      int64  // MB
    ComputeCapability string // "8.6", "9.0", etc.
    Temperature   float32 // °C
    PCIeBandwidth int64  // MB/s
  }
  ```
- Extend `FedScheduler.MatchNode()` to filter by GPU requirements
- Update API docs: `GET /api/peers` now includes GPU profiles

**Effort:** ~2 days | **Priority:** 1 (foundation for capability negotiation)

---

### 2. Thermal Budget Enforcement via OPA Policy

**Current:** OPA policies exist but don't consider thermal state.

**Goal:** Let providers set thermal thresholds; scheduler auto-rejects long-running jobs on hot nodes.

**Implementation:**
- Add thermal budget fields to OPA input context:
  ```rego
  input.node.gpu_temp = 78.5
  input.job.estimated_duration = "2h30m"
  input.job.expected_gpu_load = "high"
  ```
- Example provider policy:
  ```rego
  allow_gpu_job if {
    input.job.estimated_duration < 1h
    input.node.gpu_temp < 72
  }
  ```
- Scheduler checks OPA before placement; if policy denies, request is routed elsewhere

**Effort:** ~1 day | **Priority:** 2 (protects member hardware)

---

### 3. Richer Scheduling Signals for LinUCB Bandit

**Current:** Binary settle/fail signal. Bandit learns "is node X reliable?" only.

**Goal:** Feed multi-dimensional telemetry to bandit: actual vs. estimated duration, thermal state at start, network jitter, job type (inference vs. batch vs. encoding).

**Implementation:**
- `TelemetryRecorder` (exists) writes JSONL to disk; extend fields:
  ```json
  {
    "node_id": "...",
    "job_type": "gpu_inference",
    "duration_actual_ms": 4500,
    "duration_estimated_ms": 5000,
    "gpu_temp_start": 62.3,
    "gpu_temp_peak": 71.8,
    "network_jitter_ms": 12.4,
    "htlc_settled": true,
    "timestamp": "2026-03-24T..."
  }
  ```
- Extend bandit's `Reward()` function to weight multiple signals:
  - `settlement_score` (existing binary, 0 or 1)
  - `accuracy_score` (actual vs. estimated duration delta)
  - `thermal_score` (peak temp relative to node's limit)
  - `reliability_score` (jitter, no network timeouts)
- Compute UCB scores using weighted combination

**Effort:** ~3 days | **Priority:** 3 (improves placement quality)

---

### 4. Pre-Dispatch Capability Negotiation

**Current:** Fixed Wasm sandbox. No negotiation about execution environment.

**Goal:** Job submissions declare requirements (wasm, cuda:8.6+, python3.11, network restrictions); nodes advertise capabilities; mismatch detected before dispatch.

**Implementation:**
- Extend job submission schema:
  ```json
  {
    "runtime": "wasm",
    "gpu_required": { "compute_capability": "8.6+", "vram_mb": 8192 },
    "accelerators": ["cuda", "cudnn"],
    "python_version": "3.11",
    "network_policy": "outbound_denied"
  }
  ```
- Extend node profile with capability set (returned in heartbeat and `GET /api/peers`)
- `FedScheduler.CanRunJob(job, node)` checks all requirements before placement
- If mismatch, return error with list of qualified nodes

**Effort:** ~2.5 days | **Priority:** 4 (prevents silent failures)

---

### 5. Two-Tier Federation Topology

**Current:** Flat peer mesh. Every node talks to every other; coordination traffic grows quadratically.

**Goal:** Local clusters with elected coordinators. Nodes on same subnet form cluster; cluster exports aggregate capacity to global NTARI mesh.

**Implementation:**
- **Local cluster formation:**
  - Multicast heartbeat stays within subnet (TTL=1 for local scope)
  - Nodes on same subnet auto-form cluster if latency < 50ms
  - Cluster elects a coordinator (highest uptime wins; tiebreak by lowest node ID)

- **Coordinator responsibilities:**
  - Local scheduling within cluster (lower latency)
  - Aggregate surplus capacity report to global mesh: "Cluster-Branch-A: 40 CPU-h, 2 GPU-h available"
  - Relay inbound global job requests to cluster members

- **Global mesh:**
  - Nodes keep heartbeat contact with ~5–10 random other clusters (gossip)
  - BGP-like capacity aggregation: each cluster is an "autonomous system"
  - Job requests route to clusters with capacity, then within clusters to nodes

**Effort:** ~1 week | **Priority:** 5 (enables larger networks)

---

### 6. Network-Wide Reputation Ledger

**Current:** Per-node UCB scores. Payment data (HTLC settlements) flows through but isn't aggregated into network reputation.

**Goal:** Aggregate payment telemetry into a distributed reputation ledger (signed, Merkle-chained). High-reputation nodes unlock higher rates and priority scheduling.

**Implementation:**
- **Reputation entry (signed by node):**
  ```json
  {
    "node_id": "...",
    "period": "2026-03-20T00:00Z",
    "jobs_completed": 847,
    "jobs_failed": 3,
    "settlement_rate": 0.9964,
    "avg_accuracy": 0.98,
    "reputation_score": 9850,
    "signature": "ed25519(...)"
  }
  ```
- **Ledger chain:** Each period's entry includes hash of previous entry (tamper-evident)
- **Coordinator publishes:** `GET /api/reputation/ledger` returns full history
- **Scheduler uses for:**
  - Dynamic pricing: `base_rate_sats * (1 + reputation_bonus_percent)`
  - Scheduling priority: high-reputation nodes get first pick of jobs
  - Requester discovery: `GET /api/providers?min_reputation=9500` filters by ledger score

**Effort:** ~1 week | **Priority:** 6 (monetizes reliability; aligns incentives)

---

## Implementation Schedule

| Sprint | Focus | Duration | Blockers |
|--------|-------|----------|----------|
| 3.1 | Items 1–2: GPU profiling + thermal budgets | 3 days | None |
| 3.2 | Item 3: LinUCB signal enrichment | 3 days | Telemetry pipeline (exists) |
| 3.3 | Item 4: Capability negotiation | 2.5 days | Job schema versioning |
| 3.4 | Item 5: Two-tier topology | 7 days | Cluster election logic |
| 3.5 | Item 6: Reputation ledger | 7 days | Merkle chain storage |

**Total:** ~4 weeks | **Go-live:** Mid-April 2026

---

## Success Criteria

- ✓ GPU workloads reach target nodes without false negatives
- ✓ No thermal throttling complaints from providers
- ✓ Scheduler accuracy improves 15–20% (measured by actual vs. estimated duration)
- ✓ Network scales to 50+ clusters without topology saturation
- ✓ High-reputation providers see ≥10% rate premium reflected in ledger

---

## Out of Scope (Future Sprints)

- Mobile app (Android TV, iOS) — separate workstream
- Advanced bandit features (contextual clustering by job type)
- Zero-knowledge proofs for node attestation
- Raft-based consensus for coordinator failover (v0.3+)
