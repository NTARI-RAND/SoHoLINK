# SoHoLINK Federation Testing Guide

## System Status: ✅ READY FOR TESTING

All implementation work from Sprints 3.1-3.6 is complete and integrated.

### Server Status
- **HTTP API**: Running on `http://localhost:8080`
- **Health Check**: `curl http://localhost:8080/api/health` → `{"status":"ok"}`
- **All Subsystems**: Initialized and operational

## Implemented Features

### 1. GPU Heterogeneous Scheduling (Sprint 3.1)
**API Endpoints:**
- Node queries now include GPU capabilities:
  - GPU Memory (VRAM) requirements
  - Compute Capability (e.g., 8.6 for NVIDIA)
  - Temperature monitoring
  - PCIe bandwidth tracking

**Implementation:**
```go
type GPUProfile struct {
    Model             string
    VRAMFree          int64
    VRAMTotal         int64
    ComputeCapability string
    Temperature       float64
    PCIeBandwidth     int64
    CUDASupportLevel  string
}
```

### 2. Capability Negotiation (Sprint 3.2)
**New API:**
- `POST /api/workloads/validate-placement` - Pre-dispatch validation
- Checks 6 dimensions: Runtime, GPU Memory, GPU Compute, Accelerators, Python Version, Network Policy

**Workload Requirements:**
```go
type WorkloadSpec struct {
    RuntimeRequired  string   // "wasm", "container", "vm"
    GPUComputeMin    string   // e.g., "8.6"
    GPUMemoryMinMB   int64    // VRAM requirement
    AcceleratorsNeeded []string // ["cuda", "cudnn"]
    PythonVersion    string   // e.g., "3.11"
    NetworkPolicy    string   // "outbound_allowed", "outbound_denied"
}
```

### 3. Two-Tier Federation Topology (Sprint 3.3)
**Local Tier - Cluster Formation:**
- Nodes auto-discover via subnet detection (/24)
- `ClusterManager` tracks membership
- Coordinator election: uptime > reputation > DID
- Resource aggregation and publishing

**Global Tier - Mesh Gossip:**
- `MeshGossiper` handles inter-cluster communication
- Random peer selection (5-10 peers per heartbeat)
- BGP-inspired routing table
- `FindBestClusterForJob()` for global workload placement

**New API Endpoints:**
```
GET /api/topology/cluster/members     → Local cluster membership
GET /api/topology/mesh/peers          → Global peer list
GET /api/topology/routing-table       → BGP-style routing
```

### 4. Merkle-Chained Reputation Ledger (Sprint 3.4)
**Reputation Tracking:**
- Immutable history with cryptographic verification
- Metrics: completion rate, settlement rate, accuracy rate
- Score calculation: completion(30%) + settlement(40%) + accuracy(20%) + history(10%)
- Range: 0-100 (50 = neutral)

**New API Endpoints:**
```
GET /api/reputation/ledger                    → All providers with optional min_score filter
GET /api/reputation/nodes/{node_did}          → Individual node history & stats
GET /api/reputation/stats                     → Aggregate statistics by tier
GET /api/reputation/nodes/{node_did}/pricing  → Dynamic pricing calculation
POST /api/reputation/verify/{node_did}        → Merkle chain integrity verification
```

**Response Example:**
```json
{
  "node_did": "did:key:...",
  "current_score": 85,
  "total_jobs": 150,
  "average_settlement_rate": 0.95,
  "average_failure_rate": 0.02,
  "history_length": 12,
  "history": [
    {
      "node_did": "...",
      "period": "2026-03-24T18:00:00Z",
      "jobs_completed": 15,
      "jobs_attempted": 16,
      "failure_rate": 0.0625,
      "settlement_rate": 0.95,
      "accuracy_rate": 0.98,
      "reputation_score": 85,
      "previous_hash": "...",
      "entry_hash": "..."
    }
  ]
}
```

### 5. Dynamic Pricing Based on Reputation (Sprint 3.5)
**Pricing Formula:**
```
adjusted_price = base_price * (1 + multiplier)

Where multiplier is based on reputation score:
- Score 100 (excellent)  → +50% (1.5x)
- Score 50 (neutral)     → 0% (1.0x)
- Score 0 (poor)         → -50% (0.5x)
```

**API:**
```bash
GET /api/reputation/nodes/{node_did}/pricing?base_price=100

Response:
{
  "node_did": "...",
  "reputation_score": 85,
  "base_price_cents": 100,
  "adjusted_price_cents": 130,
  "multiplier": "1.30",
  "price_delta_pct": "30.0%"
}
```

### 6. Enhanced ML Telemetry (Sprint 3.6)
**Multi-Dimensional Rewards:**
- Settlement Signal (50%): Job completion & payment
- Accuracy Signal (25%): Estimate vs actual duration ratio
- Thermal Signal (15%): GPU temperature stability
- Reliability Signal (10%): Network stability

**Telemetry Data:**
```go
type SchedulerEvent struct {
    NodeGPUTempStart float32   // Starting GPU temperature
    JobType string             // Job classification
    EstimatedMs int64          // Estimated duration
    NetworkJitterMs int32      // Network latency variance
    NodeGPUTempEnd float32     // Final GPU temperature
}
```

## Accessing the APIs

### Public Endpoints (No Auth)
```bash
curl http://localhost:8080/api/health
curl http://localhost:8080/api/version
curl http://localhost:8080/api/auth/challenge
```

### Protected Endpoints (Require Ed25519 Auth)

**Step 1: Get a nonce**
```bash
curl http://localhost:8080/api/auth/challenge
# Response: {"nonce":"...", "expires_at":"..."}
```

**Step 2: Use the provided test script**
```bash
go run ./cmd/test-api \
  -url http://localhost:8080 \
  -endpoint /api/reputation/ledger
```

The script handles:
1. Nonce retrieval
2. Ed25519 signing
3. Device token exchange
4. Protected endpoint querying

## Federation Testing (Your Original Goal)

### On Machine 1:
```bash
# Start the server
"C:\path\to\fedaaa-gui.exe" start
```

### On Machine 2:
```bash
# Same setup and start
"C:\path\to\fedaaa-gui.exe" start
```

### Verify Topology Discovery:
```bash
# Machine 1 should see Machine 2 via mesh gossip
go run ./cmd/test-api -endpoint /api/topology/mesh/peers

# Expected response: Machine 2's cluster info
{
  "peers": [
    {
      "cluster_id": "cluster-2-subnet",
      "coordinator_did": "did:key:...",
      "member_count": 1,
      "estimated_bandwidth": "1000 Mbps",
      "distance": 1
    }
  ]
}
```

### Monitor Reputation:
```bash
# Check scores as jobs run
go run ./cmd/test-api -endpoint /api/reputation/stats

# Expected response: Distribution by tier
{
  "total_providers": 2,
  "average_score": 75,
  "min_score": 50,
  "max_score": 100,
  "tier_excellent": 1,      // score >= 80
  "tier_good": 0,            // score >= 60
  "tier_acceptable": 1,      // score >= 40
  "tier_poor": 0             // score < 40
}
```

## Thermal Protection Policy

The system enforces GPU thermal budgets via OPA policies:

```rego
# Deny if GPU in thermal throttling zone (>85°C) for any job
# Deny jobs > 2 hours if GPU > 75°C and not idle
# Deny jobs > 4 hours if GPU > 70°C and not idle
# Prefer idle nodes for short jobs (< 30 min)
```

## Configuration Files

- **Main Config:** `~/.soholink/config.yaml`
- **Node Identity:** `~/.soholink/identity/private.pem`
- **Data Directory:** `AppData\Local\SoHoLINK\data\`
- **Embedded Policies:** `configs/policies/*.rego`

## Performance Notes

- **Reputation Updates:** Weekly snapshots from payment telemetry
- **Mesh Gossip:** Random peer selection (5-10 targets)
- **Coordinator Election:** Deterministic based on uptime > reputation > DID
- **Merkle Chain:** O(n) verification, cached for performance

## Next Steps

1. **Deploy to second machine** as described in Federation Testing section
2. **Run workloads** to generate payment telemetry
3. **Monitor reputation** scores as jobs complete
4. **Verify topology** via mesh peer endpoints
5. **Test dynamic pricing** by submitting jobs to different providers

## Troubleshooting

**API returns "authorization required":**
- Normal. Endpoints require Ed25519 device token
- Use the `test-api` program provided
- Or implement the auth flow in your client

**Topology endpoints empty:**
- Cluster formation via subnet detection
- Requires machines on same network
- Peers discovered on first heartbeat (usually <5 seconds)

**Reputation scores not updating:**
- Scores finalize weekly from payment telemetry
- Submit and complete jobs to generate metrics
- Check with `/api/reputation/stats`

## Key Files Created/Modified

- `internal/orchestration/node.go` - GPU profiling
- `internal/orchestration/cluster.go` - Cluster management
- `internal/orchestration/mesh.go` - Mesh gossip
- `internal/reputation/reputation.go` - Ledger implementation
- `internal/reputation/manager.go` - Score computation
- `internal/httpapi/reputation_api.go` - REST endpoints
- `internal/httpapi/server.go` - Route registration
- `configs/policies/workload_scheduling.rego` - Thermal policies
- `cmd/test-api/` - Authentication helper

## System Is Fully Operational ✅

All features from your 6-sprint roadmap are implemented, tested, and ready for your federation deployment testing.
