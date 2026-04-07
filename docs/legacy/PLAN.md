# SoHoLINK: Gap Analysis (Legacy Task Tracker)

> **This file is the original gap-analysis document.** For current forward planning see [`ROADMAP.md`](ROADMAP.md).
> For the complete change history see [`CHANGELOG.md`](CHANGELOG.md).
>
> Status key: ✅ COMPLETED · 🔨 IN PROGRESS · ⬜ PLANNED · ❌ DEFERRED

## Guiding Principles

- Each task is scoped to a single PR-sized unit of work
- Tasks are ordered by dependency (earlier tasks unblock later ones)
- "Done" means: code compiles, tests pass, behavior matches spec
- Stubs that the spec marks as future phases are NOT gaps — only items the spec marks COMPLETED or architecturally required are gaps

---

## GAP 1: License Mismatch (AGPL-3.0 vs Apache-2.0)

**Spec says:** AGPL-3.0 throughout (Sections 1.2, 14.3, 16)
**Code says:** README.md states Apache-2.0

### Task 1.1: Resolve license with stakeholders ✅ COMPLETED

**Decision:** AGPL-3.0 (matches spec, supports federation sovereignty)

- ✅ License decision finalized
- ⏳ LICENSE file to be added by project owner
- ⏳ SPDX headers to be added by project owner

**Effort:** 1-2 hours (will be completed by project owner)

---

## GAP 2: GUI Installer ✅ COMPLETED (2026-03-01)

**Spec says:** Fyne GUI framework, wizard screens (welcome, mode selection, SaaS config, progress, completion), cross-platform
**Code says:** `internal/gui/dashboard/dashboard.go` has data structures only. No Fyne import. No rendering code. Fyne not in go.mod.

### Task 2.1: Add Fyne dependency and build-tag infrastructure ✅ COMPLETED

- ✅ Added `fyne.io/fyne/v2` to go.mod
- ✅ Added `//go:build gui` tag to `internal/gui/dashboard/dashboard.go`
- ✅ Created `internal/gui/dashboard/stub.go` for non-GUI builds
- ✅ Created `cmd/fedaaa-gui/main.go` entry point (separate binary)
- ✅ Updated Makefile with `build-gui`, `build-gui-windows`, `build-gui-linux`, `build-gui-macos` targets

**Effort:** 2 hours (completed)

### Task 2.2: Implement installer wizard screens

- `welcome.go` — Welcome screen with logo, description, Continue button
- `license.go` — License display with Accept/Decline
- `mode.go` — SaaS Client vs Standalone radio selection
- `saas_config.go` — Central URL, username, credential fields, Test Connection button
- `standalone_config.go` — Identity, storage, network configuration panels
- `progress.go` — Progress bar calling `internal/cli/install` logic
- `completion.go` — Success screen with dashboard shortcut, Open Dashboard button

Each screen is a Fyne `container.NewVBox(...)` with navigation buttons calling the existing `cli.Install()` logic underneath.

**Effort:** 16-20 hours

### Task 2.3: Implement dashboard GUI screens

- Wire `GetCapacityView()`, `GetRevenueStats()`, `GetRentalView()`, `GetAlertView()` to real store queries
- Replace stub returns with actual SQLite queries
- Create Fyne tab container with Revenue, Status, Reputation, Settings tabs matching spec Section 11.2

**Effort:** 12-16 hours

### Task 2.4: Write GUI tests

- Test installer wizard flow (mock store)
- Test dashboard data binding
- Verify build-tag isolation (non-gui build still compiles without Fyne)

**Effort:** 4-6 hours

---

## GAP 3: Cross-Platform Packaging ✅ COMPLETED (2026-03-01)

**Spec says:** WiX MSI (Windows), PKG (macOS), DEB/RPM/AppImage (Linux), GitHub Actions CI/CD
**Code says:** Makefile builds a single binary. No packaging. No CI/CD.

### Task 3.1: Create GitHub Actions CI/CD pipeline

- `.github/workflows/build.yml`:
  - Matrix: linux/amd64, linux/arm64, windows/amd64, darwin/amd64, darwin/arm64
  - Steps: checkout, setup-go, test, build, upload artifacts
- `.github/workflows/test.yml`:
  - Run `make test` on push/PR
  - Upload coverage report

**Effort:** 4-6 hours

### Task 3.2: Windows MSI packaging (WiX)

- Create `packaging/windows/` directory
- `Product.wxs` WiX XML defining:
  - Install directory (`C:\Program Files\SoHoLINK\`)
  - Service registration (optional)
  - Start menu shortcut
  - Source code bundle directory
- Add `build-msi` Makefile target
- GitHub Actions step to build MSI using `wixtoolset/wix` action

**Effort:** 8-10 hours

### Task 3.3: macOS PKG packaging

- Create `packaging/macos/` directory
- `Distribution.xml` and `component.plist`
- Post-install script to register launchd service
- Add `build-pkg` Makefile target
- Code-signing placeholder (requires Apple Developer cert)

**Effort:** 6-8 hours

### Task 3.4: Linux DEB/RPM/AppImage packaging

- Create `packaging/linux/` directory
- Use `nfpm` (Go-native packager) for DEB and RPM:
  - `nfpm.yaml` config specifying binary, config files, systemd unit
- Create `fedaaa.service` systemd unit file
- AppImage: Use `appimagetool` with desktop entry
- Add `build-deb`, `build-rpm`, `build-appimage` Makefile targets

**Effort:** 6-8 hours

### Task 3.5: Source code bundling in packages

- All packages include `/opt/soholink/source/` (Linux), `C:\Program Files\SoHoLINK\source\` (Windows)
- Makefile target `bundle-source` creates tarball of repo (excluding .git, vendor)
- Packages embed this tarball

**Effort:** 2-3 hours

---

## GAP 4: P2P Mesh Networking ⬜ PLANNED (v0.6.0)

**Spec says:** mDNS discovery, gossip protocol, blockchain consensus when central offline, anti-eclipse protection
**Code says:** `thinclient/p2p.go` has structure but `handlePeerConnection()` just closes, `collectVotes()` auto-approves, `writeBlockToCentral()` is empty

### Task 4.1: Implement peer connection handler

In `handlePeerConnection()`:
- Read DID authentication handshake from peer (send/verify Ed25519 challenge)
- Exchange peer capability announcements (CPU, memory, storage, reputation)
- Register authenticated peer in local peer table
- Start bidirectional heartbeat goroutine

**Effort:** 6-8 hours

### Task 4.2: Implement block voting protocol

In `collectVotes()`:
- Serialize proposed block to peers via TCP
- Each peer validates block contents (Merkle proof, signatures)
- Peers respond with signed vote (approve/reject + reason)
- Collect votes with timeout (30s)
- Return actual vote results instead of hardcoded approval

**Effort:** 6-8 hours

### Task 4.3: Implement central sync

In `writeBlockToCentral()`:
- HTTP POST to central SOHO `/api/blocks` endpoint
- Send block data + Merkle proof + peer signatures
- Handle conflict resolution (central has newer state)
- Retry with exponential backoff on failure

**Effort:** 4-6 hours

### Task 4.4: Add real mDNS discovery

- Replace store-based peer lookup with actual mDNS using `github.com/hashicorp/mdns`
- Register service `_soholink._tcp` with node DID and capabilities
- Discover peers on local network
- Fall back to store-based lookup when mDNS unavailable

**Effort:** 4-6 hours

### Task 4.5: P2P integration tests

- Test: two nodes discover each other via mDNS
- Test: central goes down, nodes form mesh, continue operating
- Test: central returns, nodes sync accumulated blocks
- Test: malicious peer rejected during DID handshake

**Effort:** 6-8 hours

---

## GAP 5: Dashboard Data ✅ ALREADY IMPLEMENTED + TESTS ADDED

**Status:** The PLAN incorrectly identified this as a gap. The code is **fully implemented** with complete database queries.

**Code Reality:** `internal/store/central.go` lines 699-868 contain all required methods:
- `GetTotalRevenue()` - Total revenue across all time
- `GetRevenueSince(since)` - Revenue since timestamp
- `GetRevenueByType(resourceType)` - Revenue by resource type
- `GetPendingPayout()` - Unsettled revenue
- `GetRecentRevenue(limit)` - Recent revenue entries with details
- `GetActiveRentals()` - Active resource transactions
- `GetRecentAlerts(limit)` - Recent rating alerts

### Task 5.1: Add comprehensive tests ✅ COMPLETED

Created `internal/store/central_test.go` with:
- ✅ Revenue totals and aggregation tests
- ✅ Revenue time-based filtering tests
- ✅ Pending payout calculation tests
- ✅ Revenue by resource type tests
- ✅ Active rentals filtering tests
- ✅ Recent alerts ordering tests

**Effort:** 3 hours (actual)

---

## GAP 6: Container Isolation ⬜ PLANNED (v0.5.0)

**Spec says:** Linux namespaces (CLONE_NEW*), cgroups, seccomp, AppArmor profiles
**Code says:** `sandbox.go` references `sandbox_linux.go` which does not exist. Falls back to basic `exec.Command()`.

### Task 6.1: Create sandbox_linux.go with namespace isolation

File: `internal/compute/sandbox_linux.go` with `//go:build linux`

Implement `executeLinux()`:
- Set `SysProcAttr.Cloneflags`: CLONE_NEWUSER, CLONE_NEWNS, CLONE_NEWPID, CLONE_NEWNET, CLONE_NEWUTS, CLONE_NEWIPC
- UID/GID mappings to nobody (65534)
- Resource limits via Rlimit: CPU seconds, address space, file size, NOFILE (64), NPROC (16)
- Mount private propagation for mount namespace

**Effort:** 8-10 hours

### Task 6.2: Add seccomp filtering

- Create `internal/compute/seccomp_linux.go`
- Define syscall whitelist (read, write, open, close, mmap, etc.)
- Block dangerous syscalls (ptrace, mount, reboot, etc.)
- Apply via `libseccomp-golang` or raw BPF program
- Add `golang.org/x/sys` dependency (already present)

**Effort:** 6-8 hours

### Task 6.3: Add AppArmor profile

- Create `configs/apparmor/soholink-sandbox` profile
- Deny network, deny /proc, /sys, /dev writes
- Allow specific executables (python3, node, bash)
- Read-only system libraries
- Read-write work directory only
- Load profile at sandbox startup if AppArmor available

**Effort:** 4-6 hours

### Task 6.4: Add cgroup v2 resource limits

- Create `internal/compute/cgroup_linux.go`
- Create transient cgroup for each sandbox job
- Set CPU quota, memory max, IO limits
- Clean up cgroup on job completion
- Graceful fallback if cgroups unavailable

**Effort:** 6-8 hours

### Task 6.5: Sandbox isolation tests

- Test: sandboxed process cannot see host PIDs
- Test: sandboxed process cannot access network
- Test: CPU/memory limits enforced
- Test: seccomp blocks ptrace
- Test: fallback works on non-Linux

**Effort:** 6-8 hours

---

## GAP 7: Hypervisor Backends ⬜ PLANNED (v0.8.0)

**Spec says:** KVM/QEMU with AMD SEV, Hyper-V with VBS, actual VM lifecycle
**Code says:** `kvm.go` and `hyperv.go` simulate with goroutine + sleep, never exec QEMU or PowerShell

### Task 7.1: Implement KVM backend

In `internal/compute/kvm.go`:
- `CreateVM()`: Build and exec `qemu-system-x86_64` command with proper flags
- Parse QEMU process PID, track in VM state
- `StartVM()`: Send QMP `cont` command
- `StopVM()`: Send QMP `quit` command
- `DestroyVM()`: Kill process, remove disk images
- `Snapshot()`/`Restore()`: QMP `savevm`/`loadvm`
- AMD SEV support when available (detect via `/sys/module/kvm_amd/parameters/sev`)
- QMP monitor socket for VM management

**Effort:** 16-20 hours

### Task 7.2: Implement Hyper-V backend

In `internal/compute/hyperv.go`:
- `CreateVM()`: Execute PowerShell `New-VM`, `Set-VMProcessor`, `New-VHD`
- `StartVM()`: `Start-VM`
- `StopVM()`: `Stop-VM`
- `DestroyVM()`: `Remove-VM -Force`
- `Snapshot()`: `Checkpoint-VM`
- `Restore()`: `Restore-VMCheckpoint`
- Security: `Set-VMFirmware -EnableSecureBoot On`, `Enable-VMTPM`
- Parse PowerShell JSON output for state queries

**Effort:** 12-16 hours

### Task 7.3: Hypervisor integration tests

- Test KVM: create, start, stop, destroy VM (requires KVM-enabled host)
- Test Hyper-V: same lifecycle (requires Windows with Hyper-V)
- Test security defaults applied (SecureBoot, TPM, encryption)
- Test snapshot/restore cycle
- Skip tests when hypervisor unavailable

**Effort:** 8-10 hours

---

## GAP 8: Blockchain Submission ✅ ALREADY IMPLEMENTED + TESTS ADDED

**Status:** The PLAN incorrectly identified this as a gap. The code is **fully implemented** with complete blockchain anchoring.

**Code Reality:**
- `internal/blockchain/local.go`: Complete local blockchain implementation (lines 1-292)
- `internal/app/app.go` lines 410-422: Merkle batcher wired to blockchain via callback
- Automatic anchoring on batch creation

### Task 8.1: Define blockchain interface

Create `internal/blockchain/chain.go`:
```go
type Chain interface {
    SubmitBatch(ctx context.Context, root []byte, metadata BatchMetadata) (txHash string, blockHeight uint64, err error)
    VerifyBatch(ctx context.Context, txHash string, expectedRoot []byte) (bool, error)
    GetLatestCheckpoint(ctx context.Context) (*Checkpoint, error)
}
```

**Effort:** 2-3 hours

### Task 8.2: Implement local/custom chain

Create `internal/blockchain/local.go`:
- Append-only block file (one block per batch)
- Block = {header, merkle_root, prev_hash, timestamp, signature}
- Verification: chain of prev_hash links
- Store block height and tx hash in `blockchain_batch` table
- This is the "Custom Chain (Default)" from spec Section 4.2

**Effort:** 10-14 hours

### Task 8.3: Wire Merkle batcher to blockchain

In `internal/merkle/batch.go`:
- After `BuildBatch()` creates batch metadata, call `chain.SubmitBatch()`
- Store returned `txHash` and `blockHeight` in batch record
- Update `lbtas_scores.last_anchor_block` and `last_anchor_hash`

**Effort:** 4-6 hours

### Task 8.4: Optional Ethereum/Polygon integration

Create `internal/blockchain/ethereum.go`:
- Use `go-ethereum` client library
- Submit Merkle root to a simple anchor contract
- Verify proofs against on-chain root
- Configuration: RPC URL, contract address, private key
- This is optional and can be deferred

**Effort:** 12-16 hours (optional — can be Phase 2)

### Task 8.5: Blockchain tests

- Test local chain: submit batch, verify, chain integrity
- Test Merkle proof against on-chain root
- Test batch metadata persistence

**Effort:** 4-6 hours

---

## GAP 9: Payment Processor Implementations ✅ COMPLETED (2026-03-01)

**Spec says:** Stripe (2.9% + $0.30), Bitcoin Lightning (<0.1%), Federation Tokens (0%)
**Code says:** `stripe.go` and `lightning.go` return "not yet implemented" for all methods. `fedtoken.go` has partial CreateCharge only.

### Task 9.1: Implement Stripe processor

In `internal/payment/stripe.go`:
- Add `github.com/stripe/stripe-go/v81` dependency
- `CreateCharge()`: Create Stripe PaymentIntent
- `ConfirmCharge()`: Confirm PaymentIntent
- `RefundCharge()`: Create refund
- `GetChargeStatus()`: Retrieve PaymentIntent status
- `ListCharges()`: List PaymentIntents with filters
- Handle webhooks for async status updates
- Test with Stripe test keys

**Effort:** 10-14 hours

### Task 9.2: Implement Lightning processor

In `internal/payment/lightning.go`:
- Connect to LND via gRPC (`lnrpc`)
- `CreateCharge()`: Create invoice (`lnrpc.AddInvoice`)
- `ConfirmCharge()`: Check invoice settlement status
- `RefundCharge()`: Send payment to refund address
- `GetChargeStatus()`: Lookup invoice
- `ListCharges()`: List invoices with filters
- Requires LND node running (or mock for tests)

**Effort:** 10-14 hours

### Task 9.3: Complete Federation Token processor

In `internal/payment/fedtoken.go`:
- `ConfirmCharge()`: Verify token transfer on local chain
- `RefundCharge()`: Reverse token transfer
- `GetChargeStatus()`: Query local chain
- `ListCharges()`: Query local ledger

**Effort:** 4-6 hours

### Task 9.4: Complete Barter processor

In `internal/payment/barter.go`:
- Implement remaining stub methods (2 methods)
- `GetChargeStatus()` and `ListCharges()` from credit ledger

**Effort:** 2-3 hours

### Task 9.5: Payment integration tests

- Test Stripe with test API keys
- Test Lightning with mock LND
- Test federation token round-trip
- Test barter credit tracking
- Test offline settlement queue retry behavior

**Effort:** 6-8 hours

---

## GAP 10: Managed Service Provisioning ⬜ PLANNED (v0.9.0)

**Spec says:** Create actual PostgreSQL, S3, RabbitMQ instances on federation nodes
**Code says:** `Provision()` creates metadata records but doesn't deploy anything. Health checks return true. Metrics return zero.

### Task 10.1: Implement PostgreSQL provisioner

In `internal/services/postgres.go`:
- `Provision()`: Pull Docker image, start container with volume mount
- Configure streaming replication for HA plans
- Generate real connection strings
- `Deprovision()`: Stop and remove container + volumes
- `HealthCheck()`: `pg_isready` via exec
- `GetMetrics()`: Query `pg_stat_database`

**Effort:** 12-16 hours

### Task 10.2: Implement Object Store provisioner (MinIO)

In `internal/services/objectstore.go`:
- `Provision()`: Start MinIO container, create bucket, create service account
- `Deprovision()`: Remove container + data
- `HealthCheck()`: MinIO health endpoint
- `GetMetrics()`: MinIO Prometheus metrics

**Effort:** 10-14 hours

### Task 10.3: Implement Message Queue provisioner (RabbitMQ)

In `internal/services/queue.go`:
- `Provision()`: Start RabbitMQ container, create vhost, create user
- `Deprovision()`: Remove container
- `HealthCheck()`: RabbitMQ management API health
- `GetMetrics()`: Queue depth, message rate, consumer count

**Effort:** 10-14 hours

### Task 10.4: Service provisioning tests

- Test PostgreSQL: provision, connect, query, deprovision
- Test MinIO: provision, upload object, download, deprovision
- Test RabbitMQ: provision, publish, consume, deprovision
- All tests use Docker (skip if Docker unavailable)

**Effort:** 8-10 hours

---

## GAP 11: SLA Credit Computation ✅ ALREADY IMPLEMENTED + TESTS ADDED

**Status:** The PLAN incorrectly identified this as a gap. The code is **fully implemented** with tiered credit logic.

**Code Reality:** `sla/monitor.go` lines 181-212 contain complete tiered credit computation:
- `computeCredit()`: Tiered uptime credits (5-100% depending on tier)
- `computeLatencyCredit()`: Proportional latency credits with overage calculation
- `contract.go` lines 82-141: Four tier definitions (Basic, Standard, Premium, Enterprise)

### Task 11.1: Add comprehensive tests ✅ COMPLETED

Created `internal/sla/monitor_test.go` with:
- ✅ 20+ test cases for tiered credit computation
- ✅ All tier boundaries tested (Basic, Standard, Premium, Enterprise)
- ✅ Latency credit calculation tests
- ✅ Violation detection tests
- ✅ Range matching tests
- ✅ Severity classification tests

**Effort:** 3 hours (actual)

---

## GAP 12: CDN Health Checks ✅ ALREADY IMPLEMENTED + TESTS ADDED

**Status:** The PLAN incorrectly identified this as a gap. The code is **fully implemented** with active health probing.

**Code Reality:** `cdn/router.go` lines 156-192 contain complete active health probing:
- `probeEdgeNode()`: TCP connection to measure RTT (lines 163-168)
- Query `/cdn/status` HTTP endpoint for load (lines 171-189)
- Health check loop every 10 seconds (lines 119-154)
- Updates health, latency, and load metrics

### Task 12.1: Add comprehensive tests ✅ COMPLETED

Created `internal/cdn/router_test.go` with:
- ✅ Geographic routing tests (proximity-based selection)
- ✅ Health-based filtering (unhealthy nodes excluded)
- ✅ Load balancing tests
- ✅ TCP probe success/failure tests
- ✅ HTTP `/cdn/status` endpoint parsing tests
- ✅ Haversine distance calculation tests
- ✅ Route scoring tests
- ✅ Integration test with health check loop

**Effort:** 3 hours (actual)

---

## GAP 13: AGPL Compliance Infrastructure ⬜ PLANNED (v1.0.0)

**Spec says:** `/source` HTTP endpoint, source bundled in installer, NOTICE.txt
**Code says:** No `/source` endpoint, no source bundling, no NOTICE.txt

*Only required if AGPL-3.0 is chosen in Gap 1.*

### Task 13.1: Add /source HTTP endpoint

In `internal/httpapi/server.go`:
- Add `GET /source` route
- Serve pre-built source tarball from embedded or disk path
- Include `Content-Disposition: attachment; filename=soholink-source.tar.gz`
- Add link in dashboard footer (when GUI exists)

**Effort:** 2-3 hours

### Task 13.2: Create NOTICE.txt generator

- Script that reads go.mod and generates NOTICE.txt with all dependencies and their licenses
- Include in build pipeline
- Embed in binary or distribute with package

**Effort:** 2-3 hours

### Task 13.3: Add AGPL headers to source files

- Script to prepend AGPL-3.0 header comment to all `.go` files
- Run as part of CI to enforce

**Effort:** 1-2 hours

---

## GAP 14: Auto-Update System ⬜ PLANNED (v1.0.0)

**Spec says:** Auto-update for thin clients, signature verification
**Code says:** No update mechanism exists

### Task 14.1: Implement update checker

Create `internal/update/checker.go`:
- Check GitHub Releases API (or custom endpoint) for latest version
- Compare semantic versions
- Download binary + signature
- Verify Ed25519 signature of binary
- CLI command: `fedaaa update check`

**Effort:** 6-8 hours

### Task 14.2: Implement update applier

Create `internal/update/applier.go`:
- Replace current binary with new version (atomic rename)
- Backup old binary
- Rollback on failure
- CLI commands: `fedaaa update download`, `fedaaa update apply`
- SaaS mode: central SOHO pushes updates to thin clients

**Effort:** 6-8 hours

### Task 14.3: Update tests

- Test version comparison
- Test signature verification (valid/invalid/tampered)
- Test binary replacement and rollback

**Effort:** 3-4 hours

---

## GAP 15: Orchestration — Actual Node Deployment 🔨 IN PROGRESS (partial — HTTP API exists; scheduler still records DB placements only)

**Spec says:** FedScheduler deploys workloads to nodes via node API
**Code says:** `scheduleWorkload()` creates Placement DB records but never calls any node API

### Task 15.1: Define node agent API

Create `internal/httpapi/node_agent.go`:
- `POST /api/worker/deploy` — deploy container/VM to this node
- `DELETE /api/worker/{id}` — terminate workload
- `GET /api/worker/{id}/status` — health/metrics
- `GET /api/node/capacity` — available resources

**Effort:** 6-8 hours

### Task 15.2: Implement deployment calls in scheduler

In `internal/orchestration/scheduler.go`:
- After placement scoring, HTTP POST to each selected node's `/api/worker/deploy`
- Handle deployment failures (try next candidate)
- Update placement record with actual deployment status
- Wire health monitor to poll `/api/worker/{id}/status`

**Effort:** 8-10 hours

### Task 15.3: Orchestration integration tests

- Test: submit workload → scheduler places on nodes → nodes deploy
- Test: node failure → auto-heal to different node
- Test: auto-scale up/down based on CPU
- Test: anti-affinity constraint enforcement

**Effort:** 6-8 hours

---

## GAP 16: HTTP API Completeness 🔨 IN PROGRESS (core endpoints implemented; governance + storage + full resource discovery pending)

**Spec says:** Full REST API for workloads, services, storage, revenue, governance
**Code says:** Only health, LBTAS score, LBTAS rating, and resource discovery (stub) endpoints exist

### Task 16.1: Workload management endpoints

- `POST /api/workloads` — submit workload (calls FedScheduler)
- `GET /api/workloads` — list workloads
- `GET /api/workloads/{id}` — get workload state
- `PUT /api/workloads/{id}/scale` — manual scale
- `DELETE /api/workloads/{id}` — terminate workload

**Effort:** 6-8 hours

### Task 16.2: Service management endpoints

- `POST /api/services` — provision managed service
- `GET /api/services` — list instances
- `GET /api/services/{id}` — instance details + connection string
- `DELETE /api/services/{id}` — deprovision

**Effort:** 4-6 hours

### Task 16.3: Storage endpoints

- `POST /api/storage/buckets` — create bucket
- `PUT/GET/DELETE /api/storage/objects/{bucket}/{key}` — S3-like operations
- Or: configure MinIO to serve S3 API directly

**Effort:** 6-8 hours

### Task 16.4: Revenue and governance endpoints

- `GET /api/revenue/balance` — current balance
- `GET /api/revenue/history` — transaction history
- `POST /api/governance/proposals` — create proposal
- `POST /api/governance/vote` — cast vote
- `GET /api/governance/proposals` — list active proposals

**Effort:** 6-8 hours

### Task 16.5: Resource discovery implementation

- Replace stub in `/api/resources/discover` with real node query
- Return available nodes matching criteria (CPU, memory, region, reputation)

**Effort:** 2-3 hours

---

## GAP 17: Spec Document Accuracy Updates ✅ COMPLETED (2026-03-02)

**The spec must be updated to reflect reality.** These changes are documentation-only.

### Task 17.1: Update Phase 1 & 2 completion status

- Phase 1: Mark GUI installer and packaging as IN PROGRESS, not COMPLETED
- Phase 1: Mark CLI installer and error handling as COMPLETED (accurate)
- Phase 2: Mark Central SOHO as PARTIAL (framework exists, not feature-complete)
- Phase 2: Mark P2P mesh as IN PROGRESS

### Task 17.2: Correct code references

- Verify all line-number references in spec match actual code
- Update `internal/app/app.go` line references for nonce invariant
- Update `internal/cli/install.go` line references for error handling

### Task 17.3: Add current-state section

- Add "Current Implementation Status" section documenting what's actually built
- Separate from roadmap (aspirational) vs status (factual)

**Effort:** 2-3 hours total

---

## Execution Order (Dependency-Aware)

### Wave 1: Foundations (no dependencies, can be parallel)
- GAP 1: License decision (blocks GAP 13)
- GAP 6: sandbox_linux.go (independent)
- GAP 8.1-8.3: Blockchain local chain + wiring (independent)
- GAP 11: SLA credit computation (independent)
- GAP 12: CDN health probes (independent)
- GAP 17: Spec accuracy updates (independent)

### Wave 2: Core Infrastructure (depends on Wave 1)
- GAP 4: P2P mesh implementation (depends on nothing)
- GAP 5: Dashboard data queries (depends on nothing)
- GAP 9: Payment processors (depends on nothing)
- GAP 14: Auto-update system (depends on nothing)

### Wave 3: Platform Services (depends on Wave 2)
- GAP 7: Hypervisor backends (depends on GAP 6 for sandbox integration)
- GAP 10: Managed service provisioning (depends on GAP 15 for node deployment)
- GAP 15: Orchestration node deployment (depends on GAP 16.1 for API)

### Wave 4: User-Facing (depends on Waves 1-3)
- GAP 2: GUI installer (depends on GAPs 5, 14 for dashboard data and update flow)
- GAP 3: Cross-platform packaging (depends on GAP 2 for GUI binary)
- GAP 13: AGPL compliance (depends on GAP 1 decision, GAP 16 for /source endpoint)
- GAP 16: HTTP API completeness (depends on GAPs 10, 15 for service/orchestration)

---

## Effort Summary

| Gap | Description | Estimated Hours |
|-----|-------------|----------------|
| 1   | License decision | 1-2 |
| 2   | GUI installer + dashboard | 34-46 |
| 3   | Cross-platform packaging + CI/CD | 26-35 |
| 4   | P2P mesh networking | 26-36 |
| 5   | Dashboard data wiring | 11-16 |
| 6   | Container isolation (Linux) | 30-40 |
| 7   | Hypervisor backends (KVM/Hyper-V) | 36-46 |
| 8   | Blockchain submission | 32-45 |
| 9   | Payment processors | 32-45 |
| 10  | Managed service provisioning | 40-54 |
| 11  | SLA credit computation | 7-10 |
| 12  | CDN health probes | 6-9 |
| 13  | AGPL compliance (conditional) | 5-8 |
| 14  | Auto-update system | 15-20 |
| 15  | Orchestration deployment | 20-26 |
| 16  | HTTP API completeness | 24-33 |
| 17  | Spec accuracy | 2-3 |

**Total: ~350-475 hours** (~9-12 developer-weeks at 40 hrs/week)

### Quick Wins (< 1 day each):
- GAP 1: License decision
- GAP 11: SLA credit calculation
- GAP 12: CDN health probes
- GAP 17: Spec document corrections

### Medium Effort (1-2 weeks each):
- GAP 4: P2P mesh
- GAP 5: Dashboard data
- GAP 9: Payment processors
- GAP 14: Auto-update
- GAP 15: Orchestration deployment
- GAP 16: HTTP API

### Large Effort (2-4 weeks each):
- GAP 2: GUI installer ✅ DONE
- GAP 3: Packaging + CI/CD ✅ DONE
- GAP 6: Container isolation (⬜ v0.5.0)
- GAP 7: Hypervisor backends (⬜ v0.8.0)
- GAP 8: Blockchain ✅ DONE
- GAP 10: Managed services (⬜ v0.9.0)

---

## Mobile Integration ✅ COMPLETED (2026-03-02)

All Go-side prerequisites for mobile participation are implemented. Mobile device participation is tracked in [`docs/MOBILE_INTEGRATION.md`](docs/MOBILE_INTEGRATION.md) and [`ROADMAP.md`](ROADMAP.md). The remaining work is native app development (Kotlin/Swift) in `mobile/`.

### Pre-Mobile Go Prerequisites ✅ All Completed (2026-03-02)

| Task | File | Description | Status |
|---|---|---|---|
| M1 | `internal/orchestration/mobile.go` | `NodeClass` type + constants; `NodeConstraints` struct; wire protocol types | ✅ |
| M2 | `internal/httpapi/mobilehub.go` | WebSocket hub (`/ws/nodes`); 30s ping; 90s heartbeat timeout | ✅ |
| M3 | `internal/httpapi/server.go` | `POST /api/v1/nodes/mobile/register`; `GET /api/v1/nodes/mobile` | ✅ |
| M4 | `internal/orchestration/scheduler.go` | `ScheduleMobile()`; `assignWithReplication()`; `PreemptMobileWorkload()` | ✅ |
| M5 | `internal/orchestration/workload.go` | `CheckpointData []byte`, `SegmentIndex int`, `SegmentCount int` | ✅ |
| M6 | `configs/policies/resource_sharing.rego` | `task_replication_factor`; `mobile_eligible_task`; `allow_mobile_task` | ✅ |
| M7 | `internal/wasm/executor.go` | `Executor` interface; `StubExecutor`; `WithTimeout`; `TaskManifest` | ✅ |
| M8 | `internal/payment/htlc.go` | `CreateHoldInvoice`; `SettleHoldInvoice`; `CancelHoldInvoice` | ✅ |
| M9 | `internal/notification/apns.go` | `APNSNotifier` with JWT auto-refresh; `SendJobRequest`, `SendPaymentReceived`, `SendNodeOffline` | ✅ |

---

## ML-Driven Scheduling ✅ Phase 0+1 COMPLETED (2026-03-02)

Contextual bandit scheduling and telemetry infrastructure are complete. For full research background see [`docs/research/ML_LOAD_BALANCING.md`](docs/research/ML_LOAD_BALANCING.md). Future phases tracked in [`ROADMAP.md`](ROADMAP.md) v0.6.0.

### ML Go Infrastructure ✅ Completed (2026-03-02)

| Task | File | Description | Status |
|---|---|---|---|
| ML-0a | `internal/ml/features.go` | Dimension constants (`ContextDim=20`); `clamp()` helper | ✅ |
| ML-0b | `internal/ml/telemetry.go` | `TelemetryRecorder` JSONL writer; `SchedulerEvent`; `RewardFor()`; `EventBuilder` | ✅ |
| ML-1a | `internal/ml/bandit.go` | `LinUCBBandit`: disjoint LinUCB; Gauss-Jordan inverse; `Select`/`Update`/`RemoveArm` | ✅ |
| ML-1b | `internal/orchestration/mlfeatures.go` | `NodeFeatures`, `TaskFeatures`, `SystemFeatures`, `BuildContext`, `SystemState` | ✅ |
| ML-1c | `internal/orchestration/scheduler.go` | `SetMLBandit()`, `SetTelemetryRecorder()`, bandit-based `ScheduleMobile()`, `RecordMobileOutcome()` | ✅ |
| ML-R | `docs/research/ML_LOAD_BALANCING.md` | 755-line ML scheduling research report | ✅ |

### ML Future Phases ⬜ (v0.6.0 Phases 2–4)

| Task | File | Description | Target |
|---|---|---|---|
| ML-2 | `internal/ml/forecaster.go` | LSTM availability forecaster; 5/15/60-min probability per node | Q3 2026 |
| ML-3 | `internal/ml/graph.go` | GAT graph-aware shadow pair placement; co-failure correlation | Q4 2026 |
| ML-4 | `internal/ml/anomaly.go` | LSTM-Autoencoder + Isolation Forest; LBTAS reputation penalty | Q4 2026 |
