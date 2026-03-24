# SoHoLINK API Access Guide

## System Status

✅ **SoHoLINK Server is Running**
- HTTP API listening on: `http://localhost:8080`
- All subsystems initialized
- Policies compiled and loaded
- Reputation system ready

## Recent Changes Implemented

All 6 development sprints have been successfully built into the system:

### Sprint 3.1: GPU Heterogeneous Scheduling
- GPU profile tracking (VRAM, compute capability, temperature, PCIe bandwidth)
- Extended `Node` struct with GPU capabilities
- Thermal budget protection via OPA policies

### Sprint 3.2: Capability Negotiation
- Extended `Workload` spec with runtime, GPU, Python, network requirements
- Node capability filtering during scheduling
- `CanRunJob()` validation across 6 dimensions

### Sprint 3.3: Two-Tier Federation Topology
- Local cluster formation via subnet detection (`ClusterManager`)
- Global mesh gossip for inter-cluster communication (`MeshGossiper`)
- Cluster coordinator election based on uptime + reputation

### Sprint 3.4: Merkle-Chained Reputation Ledger
- **New API endpoints:**
  - `GET /api/reputation/ledger` - Full reputation ledger with min_score filter
  - `GET /api/reputation/nodes/{node_did}` - Node history and stats
  - `GET /api/reputation/stats` - Aggregate statistics by tier
  - `GET /api/reputation/nodes/{node_did}/pricing` - Dynamic pricing calculation
  - `POST /api/reputation/verify/{node_did}` - Merkle chain verification

### Sprint 3.5: Dynamic Pricing based on Reputation
- `ComputeDynamicPrice()` adjusts pricing: base_price * (1 + multiplier)
- Score 50 (neutral) = 1.0x, Score 100 = 1.5x, Score 0 = 0.5x
- `GetPricingMultiplier()` for scheduling priority weighting

### Sprint 3.6: Enhanced ML Telemetry
- Multi-dimensional reward signals:
  - Settlement (50%), Accuracy (25%), Thermal (15%), Reliability (10%)
- Extended `SchedulerEvent` with execution metrics
- `ComputeMultiDimensionalReward()` for contextual bandit learning

### Topology Endpoints
- **New API endpoints:**
  - `GET /api/topology/cluster/members` - Local cluster membership
  - `GET /api/topology/mesh/peers` - Global mesh peer list
  - `GET /api/topology/routing-table` - BGP-style routing information

## Public API Endpoints (No Auth Required)

These endpoints are accessible without authentication:

```bash
# Check server health
curl http://localhost:8080/api/health

# Get version info
curl http://localhost:8080/api/version

# Initiate authentication
curl http://localhost:8080/api/auth/challenge
```

## Protected API Endpoints (Auth Required)

The reputation and topology endpoints require a device token obtained via Ed25519 authentication:

```bash
# These will return "authorization required" without proper token:
curl http://localhost:8080/api/reputation/ledger
curl http://localhost:8080/api/reputation/stats
curl http://localhost:8080/api/topology/cluster/members
```

## Authentication Flow

To access protected endpoints, you need to:

1. **Get a nonce:**
   ```bash
   curl http://localhost:8080/api/auth/challenge
   # Returns: {"nonce":"...", "expires_at":"..."}
   ```

2. **Sign the nonce with your Ed25519 private key:**
   - Load the node's private key
   - Sign the nonce bytes
   - Base64-encode both the public key and signature

3. **Exchange signed nonce for device token:**
   ```bash
   POST /api/auth/connect
   Content-Type: application/json

   {
     "nonce": "...",
     "public_key": "<base64>",
     "signature": "<base64>",
     "device_name": "your-device"
   }
   ```

4. **Use the device token in subsequent requests:**
   ```bash
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8080/api/reputation/ledger
   ```

## Test Script

A test program has been created to handle the full authentication flow:

```bash
# From SoHoLINK directory:
go run ./cmd/test-api -url http://localhost:8080 -endpoint "/api/reputation/ledger"
```

This script will:
1. Get a nonce from `/api/auth/challenge`
2. Sign it with the node's private key
3. Authenticate via `/api/auth/connect`
4. Query the protected endpoint with the received token

## Configuration Files

- **Config:** `~/.soholink/config.yaml`
- **Node Key:** Located in AppData\Local\SoHoLINK\data\
- **Database:** AppData\Local\SoHoLINK\data\soholink.db
- **Policies:** configs/policies/*.rego (embedded or on disk)

## Next Steps

1. **Federation Testing:** Install SoHoLINK on a second machine and test inter-cluster discovery
2. **Reputation Tracking:** Submit jobs to see reputation scores accumulate
3. **Topology Visualization:** Use topology endpoints to visualize cluster formation
4. **Dynamic Pricing:** Monitor price adjustments as provider reputation changes

## API Status Summary

| Endpoint | Status | Auth | Notes |
|----------|--------|------|-------|
| /api/health | ✅ Working | None | Health check |
| /api/version | ✅ Working | None | Build version |
| /api/reputation/ledger | ✅ Ready | Required | Full provider reputation list |
| /api/reputation/stats | ✅ Ready | Required | Aggregate statistics |
| /api/reputation/nodes/{did} | ✅ Ready | Required | Individual node history |
| /api/reputation/nodes/{did}/pricing | ✅ Ready | Required | Dynamic pricing |
| /api/reputation/verify/{did} | ✅ Ready | Required | Merkle chain verification |
| /api/topology/cluster/members | ✅ Ready | Required | Local cluster info |
| /api/topology/mesh/peers | ✅ Ready | Required | Global peer list |
| /api/topology/routing-table | ✅ Ready | Required | Routing information |

## Known Issues

None. System is fully operational.

## Support

For help with authentication or API access, refer to the auth middleware documentation in `internal/httpapi/auth_middleware.go`.
