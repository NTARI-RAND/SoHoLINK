# SoHoLINK Roadmap

**Current version:** 0.1.0 (2026-03-01)
**Module:** `github.com/NetworkTheoryAppliedResearchInstitute/soholink`

This roadmap is organized by milestone. Each milestone is a shippable increment with a clear goal. Items within a milestone are ordered by dependency.

For detailed implementation plans see:
- Mobile integration: [`docs/MOBILE_INTEGRATION.md`](docs/MOBILE_INTEGRATION.md)
- ML scheduling: [`docs/research/ML_LOAD_BALANCING.md`](docs/research/ML_LOAD_BALANCING.md)
- Gap analysis (legacy): [`PLAN.md`](PLAN.md)

---

## ✅ v0.1.x — Buyer-Side Marketplace & Prepaid Wallet (Shipped 2026-03-05)

**Goal:** Complete the requester (buyer) side of the marketplace so the platform is a true
two-sided marketplace: providers list compute → requesters browse, estimate, pay, and launch.

**All items shipped:**

- ✅ Schema migration v3: `wallet_balances`, `wallet_topups`, `orders` tables (auto-applied on startup)
- ✅ `internal/store/marketplace.go` — `WalletBalanceRow`, `WalletTopupRow`, `OrderRow` structs; CRUD helpers for wallet and orders
- ✅ `internal/payment/ledger.go` — `GetWalletBalance`, `TopupWallet`, `ConfirmTopup`, `DebitWallet` (atomic, `ErrInsufficientBalance`)
- ✅ `internal/orchestration/scheduler.go` — `FindNodes(ctx, NodeQuery)` delegates to `NodeDiscovery`
- ✅ `internal/httpapi/wallet.go` — 4 wallet endpoints: balance, topup, topup history, confirm-topup
- ✅ `internal/httpapi/marketplace.go` — 8 marketplace endpoints: browse nodes, estimate, list services, purchase workload, purchase service, list orders, get order, cancel order
- ✅ `internal/httpapi/server.go` — `catalog *services.Catalog` field + `SetServiceCatalog` setter; 11 new routes registered
- ✅ `internal/app/app.go` — `HTTPAPIServer.SetServiceCatalog(a.ServiceCatalog)` wired in `initManagedServices`
- ✅ `mobile/flutter-app/lib/models/marketplace.dart` — `MarketplaceNode`, `CostEstimate`, `WalletBalance`, `Order` models
- ✅ `mobile/flutter-app/lib/api/soholink_client.dart` — 8 new API methods + `_post` helper
- ✅ `mobile/flutter-app/lib/pages/marketplace_page.dart` — browse + filter provider nodes
- ✅ `mobile/flutter-app/lib/pages/order_page.dart` — configure workload, live estimate, wallet balance, pay & launch
- ✅ `mobile/flutter-app/lib/pages/home_page.dart` — 6th "Market" tab with `storefront_outlined` icon

---

## 🔨 v0.1.x — Local Web Dashboard (In Progress, 2026-03-03)

**Goal:** Replace the Fyne native GUI with an embedded local web dashboard served by `fedaaa.exe` at
`http://localhost:8080/dashboard`. No CGO/MinGW required; richer UI for radial dials, charts, and
globe integration; works 100% offline — the node IS the program, the browser is the renderer.

**Phase 1 — Shell + navigation (done 2026-03-03):**

- ✅ `ui/dashboard/index.html` — SPA shell with 5-tab nav (Dashboard, Plan Work, Management, Help, Settings)
- ✅ `ui/dashboard/style.css` — dark theme, cyan accent, SVG radial dials
- ✅ `ui/dashboard/app.js` — hash router, live polling, dial animation
- ✅ `internal/httpapi/dashboard.go` — embed + serve assets at `/dashboard`
- ✅ `internal/httpapi/server.go` — `/dashboard`, `/api/status` routes

**Phase 2 — Dashboard screen (next):**

- [ ] `GET /api/status` — CPU, RAM, storage, uptime, active rentals, federation nodes
- [ ] Radial dials pulling real data; globe iframe using existing `/ntarios-globe.html`
- [ ] Remove demo buttons (ADD NODE, ADD 5, ADD 10) from globe; connect to real `/ws/graph`

**Phase 3 — Management screen:**

- [ ] `GET /api/ledger` — revenue events from `internal/payment/ledger`
- [ ] `GET /api/rentals/active` — from `internal/rental`
- [ ] `POST /api/payout` — trigger Stripe / Lightning settlement
- [ ] Income chart (30-day bar chart)

**Phase 4 — Plan Work + Settings:**

- [ ] `GET/POST /api/config` — read/write config.yaml safely
- [ ] Resource offer sliders → live earnings estimate
- [ ] Settings form with validation

**Phase 5 — Help + polish:**

- [ ] Rendered documentation, FAQ, glossary
- [ ] Desktop shortcut created by `install-service.ps1`
- [ ] Dark theme polish; mobile-friendly layout

---

## ✅ v0.1.0 — Core Platform (Shipped 2026-03-01)

The foundational federated compute marketplace. All core subsystems operational.

- ✅ Hardware discovery (Windows, Linux, macOS)
- ✅ FedScheduler with placement constraints and auto-scaling
- ✅ IPFS storage pool (Kubo HTTP client)
- ✅ Stripe + Lightning payment processors
- ✅ Per-hour metering loop
- ✅ OPA policy governance (resource_sharing.rego)
- ✅ Auto-accept rental engine
- ✅ Fyne GUI dashboard (8 tabs, 7 dialogs, 8-step wizard)
- ✅ 3D globe visualization (GEO + topology modes)
- ✅ K8s edge cluster adapter (TLS, CA cert pool)
- ✅ GoReleaser cross-platform distribution pipeline
- ✅ NSIS Windows installer, macOS universal `.pkg`, Linux AppImage + .deb/.rpm
- ✅ Dependabot + monthly dep-update workflow
- ✅ Full test suite passing (`go test ./...` with race detector)

---

## 🔨 v0.2.0 — Mobile Foundation: Android TV Nodes

**Goal:** Extend the network into always-on Android TV / Fire TV boxes — the easiest mobile expansion with no battery or thermal constraints.

**Target:** Q2 2026 (3–4 weeks)

**Prerequisite Go-side work (in preparation for mobile):**

- ✅ `internal/orchestration/mobile.go` — `NodeClass` type, `mobile-android` / `android-tv` / `mobile-ios` constants, `NodeConstraints` struct
- ✅ `internal/httpapi/mobilehub.go` — WebSocket hub for mobile node registration and task push (`/ws/nodes`)
- ✅ `internal/httpapi/server.go` — `POST /api/v1/nodes/mobile/register` endpoint
- ✅ `internal/orchestration/scheduler.go` — `ScheduleMobile()`: route tasks to WebSocket-connected nodes; heartbeat tracking (2 missed = unavailable)
- ✅ `internal/orchestration/workload.go` — `CheckpointData []byte`, `SegmentIndex int`, `SegmentCount int` fields on `WorkloadState`
- ✅ `configs/policies/resource_sharing.rego` — `task_replication_factor` rule; `mobile_eligible_task` rule

**Android TV application (`mobile/android-tv/`):**

- [ ] Kotlin project with WorkManager background task loop
- [ ] WebSocket connection to coordinator (task pull)
- [ ] Wasm task executor (JNI bridge to `wasmer` or `wasmtime`)
- [ ] Custodial Lightning wallet (LDK)
- [ ] Leanback TV UI: earnings dashboard + settings
- [ ] APK published to GitHub Releases alongside desktop installers

**Acceptance criteria:**
- Android TV node appears in dashboard Orchestration tab as class `android-tv`
- Test Wasm task executes and result is recorded
- Lightning payment credited after task completion
- Node disappears from active list within 90 seconds of disconnect

---

## 🗓 v0.3.0 — Mobile Phase 2: Android "Earn While Charging"

**Goal:** Android smartphones contribute compute while plugged in and on WiFi, with explicit user consent.

**Target:** Q3 2026 (6–8 weeks, can overlap with v0.2 on separate workstream)

**Prerequisite Go-side work:**

- ✅ `internal/wasm/executor.go` — `Executor` interface + `StubExecutor` + `WithTimeout` wrapper + `TaskManifest`; wazero implementation deferred to v0.3 (stub compiles now); `ErrTimeout` sentinel for caller-detectable timeout errors
- ✅ `internal/payment/htlc.go` — Lightning hold invoice helpers (`CreateHoldInvoice`, `SettleHoldInvoice`, `CancelHoldInvoice`)
- ✅ `internal/orchestration/scheduler.go` — `assignWithReplication()`: shadow replica for `mobile-android` tasks; preemption tolerance (`PreemptMobileWorkload` reassigns to desktop from last checkpoint)
- ✅ `configs/policies/resource_sharing.rego` — `allow_htlc_cancel`, `allow_htlc_settle`, `allow_mobile_preempt` OPA rules with explicit reason allowlists; required for the coordinator to authorize payment lifecycle events and mid-task preemption
- [ ] Task micro-segmentation: all new tasks must declare `SegmentDurationSeconds ≤ 120`
- [ ] Wasm task packaging standard: `task.wasm` + `task.json` manifest + `inputs/`
- [ ] `internal/wasm/executor.go` — replace `StubExecutor` with real wazero implementation

**Android smartphone application (`mobile/android/`):**

- [ ] `ForegroundService` with persistent notification ("SoHoLINK: earning 0.004 SATS — tap to pause")
- [ ] `BroadcastReceiver`: `ACTION_POWER_CONNECTED` → start; `ACTION_POWER_DISCONNECTED` → stop
- [ ] `ConnectivityManager.NetworkCallback`: WiFi lost → pause task intake
- [ ] `PowerManager.getThermalHeadroom()` governor: < 0.5 reduce concurrency; < 0.2 pause entirely
- [ ] Battery optimization exemption flow (user-directed, Play-policy-compliant)
- [ ] Wasm task executor (ARM64)
- [ ] Custodial Lightning wallet with configurable auto-withdrawal
- [ ] Dashboard: earnings, active tasks, thermal state, on/off toggle
- [ ] Google Play submission (or APK via GitHub Releases)

**Acceptance criteria:**
- Compute only activates when: plugged in + WiFi + user explicitly enabled
- Foreground notification always visible during compute
- Shadow replication verifies results before HTLC payment releases
- Node checkpoint survives Android killing foreground service

---

## 🗓 v0.4.0 — Mobile Phase 3: iOS Management Client

**Goal:** iPhone and iPad as first-class monitoring, earnings, and management clients. No compute — iOS restrictions make it structurally impossible.

**Target:** Q3 2026 (4–5 weeks, parallel with v0.3)

**Prerequisite Go-side work:**

- ✅ `internal/notification/apns.go` — APNs client (`SendJobRequest`, `SendPaymentReceived`, `SendNodeOffline`); JWT auto-refresh
- [ ] Coordinator: APNs device token registration endpoint (`POST /api/v1/devices/apns`)
- [ ] Coordinator: push notification triggers (new job, payment received, node offline)

**iOS application (`mobile/ios/`):**

- [ ] SwiftUI: Dashboard, Globe (WKWebView), Jobs, Wallet, Settings screens
- [ ] APNs push for job request approval, payment milestones, node alerts
- [ ] Lightning wallet balance + withdrawal flow
- [ ] Live earnings from coordinator WebSocket
- [ ] TestFlight beta → App Store submission

---

## 🗓 v0.5.0 — Container Isolation & Sandbox Hardening

**Goal:** Linux namespace isolation, seccomp filtering, and cgroup v2 resource limits for all compute workloads. Prerequisite for production SOHO deployments.

**Target:** Q3 2026 (4–5 weeks)

- [ ] `internal/compute/sandbox_linux.go`: Linux namespaces (`CLONE_NEWUSER`, `CLONE_NEWNS`, `CLONE_NEWPID`, `CLONE_NEWNET`), UID/GID mapping to nobody
- [ ] `internal/compute/seccomp_linux.go`: syscall allowlist BPF filter (deny ptrace, mount, reboot, etc.)
- [ ] `internal/compute/cgroup_linux.go`: cgroup v2 — CPU quota, memory max, IO limit per job; cleanup on completion
- [ ] `configs/apparmor/soholink-sandbox`: AppArmor profile (deny network, /proc, /sys writes)
- [ ] Graceful fallback on non-Linux (macOS, Windows)
- [ ] Sandbox isolation tests (`//go:build linux`)

---

## 🔨 v0.6.0 — ML-Driven Scheduling

**Goal:** Replace the static linear-weighted `Placer.ScoreNodes` with an adaptive ML scheduler that learns optimal node selection from live HTLC outcome signals. The phased approach starts with zero-cost telemetry, then online bandit dispatch, then LSTM forecasting, then graph-aware placement.

**Target:** Q3–Q4 2026 (parallel workstream; each phase ships independently)

**Research reference:** [`docs/research/ML_LOAD_BALANCING.md`](docs/research/ML_LOAD_BALANCING.md)

**Phase 0 — Telemetry Infrastructure (shipped 2026-03-02):**

- ✅ `internal/ml/features.go` — node + task + system feature extraction to float64 vectors; dimension-documented constants
- ✅ `internal/ml/telemetry.go` — `TelemetryRecorder` appends `SchedulerEvent` JSONL to disk; captures pre-dispatch node features, chosen arm, outcome (completed / preempted / settled / cancelled), duration
- ✅ `internal/orchestration/scheduler.go` — `SetTelemetryRecorder()` wiring; telemetry hooks in `ScheduleMobile`

**Phase 1 — Contextual Bandit Node Selection (shipped 2026-03-02):**

- ✅ `internal/ml/bandit.go` — `LinUCBBandit`: disjoint linear UCB; per-arm A/b matrices; `Select()` returns highest UCB arm; `Update()` on task outcome; thread-safe; α-tunable exploration; heuristic fallback on inference failure
- ✅ `internal/orchestration/scheduler.go` — `SetMLBandit()` wiring; `ScheduleMobile` uses bandit over round-robin when bandit is set

**Phase 2 — LSTM Availability Forecaster (Q3 2026, 4–6 weeks):**

- [ ] `internal/ml/forecaster.go` — sliding-window node availability time series; LSTM cell in pure Go; predicts 5/15/60-minute availability probability per node
- [ ] `internal/ml/thermal.go` — Android thermal headroom degradation curve model (exponential decay regression); blocks dispatch to nodes predicted to throttle mid-segment
- [ ] `internal/orchestration/scheduler.go` — integrate forecaster into candidate filtering before bandit arm selection

**Phase 3 — Graph-Aware Shadow Pair Placement (Q4 2026, 4–5 weeks):**

- [ ] `internal/ml/graph.go` — dynamic federation graph; nodes as vertices with feature embeddings; edges weighted by historical co-failure correlation (same ISP / same geographic block)
- [ ] Graph Attention Network (GAT) scorer for joint mobile-primary + desktop-shadow placement; ensures shadow node is not correlated with primary
- [ ] `internal/orchestration/scheduler.go` — `assignWithReplication` uses GAT scorer instead of random desktop selection

**Phase 4 — Anomaly Detection + LBTAS Integration (Q4 2026, 3–4 weeks):**

- [ ] `internal/ml/anomaly.go` — LSTM-Autoencoder on node telemetry stream; Isolation Forest for statistical outliers (high accept rate + low HTLC settle rate)
- [ ] `internal/lbtas/manager.go` — accept ML-detected anomaly signals as immediate reputation penalty (bypass lagged rating window)
- [ ] Coordinator: emit anomaly alerts via `SendNodeOffline` APNs channel

**Acceptance criteria:**
- HTLC settle rate (mobile-android) ≥ 85% (up from ~70% with round-robin)
- Mid-task preemption rate ≤ 15% (down from ~30% with no availability forecasting)
- Bandit inference latency ≤ 2 ms per dispatch decision (measured p99)
- All ML components fall back to heuristic within 5 ms on inference failure

---

## 🔨 v0.8.0 — P2P Mesh Networking

**Goal:** Nodes discover each other automatically, form a resilient mesh when the coordinator is
offline, and sync back when it returns.  Topology follows the Watts–Strogatz small-world model:
dense LAN clusters (high clustering coefficient) connected by sparse cross-subnet links (short path
length) via the HTTP registration API.

**Target:** Q4 2026 (phased delivery)

**Phase 1 — LAN Peer Discovery (shipped 2026-03-04):**

- ✅ `internal/p2p/mesh.go` — signed multicast UDP on `239.255.42.99:7946` (RFC 2365
  administratively-scoped; will not leave the LAN); `Announcement` (Ed25519 signed, 30 s anti-replay
  window); 10 s announce, 45 s TTL, 15 s stale reaper; auto-upserts into `federation_nodes`
- ✅ `internal/p2p/peers_handler.go` — `GET /api/peers` HTTP handler
- ✅ `internal/httpapi/server.go` — `SetP2PMesh()` + `/api/peers` route (graceful degradation when
  mesh is not configured)
- ✅ `internal/app/app.go` — `P2PMesh` field, key-loading in `initP2P()`, mesh goroutine in `Start()`
- ✅ `internal/config/config.go` — `StorageConfig.IPFSAPIAddr` for Kubo API address advertisement

**Phase 2 — Cross-Subnet Registration & Gossip (Q4 2026, 3–4 weeks):**

- [ ] `POST /api/peers/register` — HTTP endpoint to manually register WAN peers (provides the sparse
  long-range edges that give the federation its small-world path-length properties)
- [ ] Gossip protocol for peer capability exchange across subnets
- [ ] Peer capability propagation: each node relays a bounded subset of known peers to newcomers

**Phase 3 — Offline Mesh & Coordinator Sync (Q4 2026, 3–4 weeks):**

- [ ] Mesh block voting protocol (collect signed votes with timeout)
- [ ] Central sync: `writeBlockToCentral()` with exponential backoff when coordinator returns
- [ ] Anti-eclipse protection (minimum peer diversity requirement)
- [ ] P2P integration tests: two-node mesh, central-offline continuity, sync-on-return

---

## 🗓 v0.9.0 — iOS Core ML Inference Endpoint

**Goal:** iPhones with high-TOPS Neural Engines serve as ML inference nodes while the app is in the foreground.

**Target:** Q4 2026 (3–4 weeks, requires v0.4.0)

- [ ] `TaskTypeInference` with `InferenceTaskSpec` (model CID, input CID, quantization)
- [ ] FedScheduler: route inference tasks to iOS nodes confirmed active within 60 seconds
- [ ] iOS: Core ML model fetch from IPFS, Neural Engine execution, result push
- [ ] Inference-capable capability advertisement (`neural_engine_tops`, `foreground_only: true`)
- [ ] No replication required (deterministic) — HTLC still enforces payment after hash check

---

## 🗓 v0.10.0 — Hypervisor Backends

**Goal:** Real VM lifecycle via KVM/QEMU (Linux) and Hyper-V (Windows) instead of simulated goroutines.

**Target:** Q1 2027 (5–6 weeks)

- [ ] `internal/compute/kvm.go`: exec `qemu-system-x86_64`, QMP monitor socket, AMD SEV support
- [ ] `internal/compute/hyperv.go`: PowerShell `New-VM`, `Set-VMFirmware`, secure boot + TPM
- [ ] Snapshot/restore for both backends
- [ ] Integration tests (skip when hypervisor unavailable)

---

## 🗓 v0.11.0 — Managed Service Provisioning

**Goal:** Providers can offer PostgreSQL, MinIO object storage, and RabbitMQ message queues as managed services running in Docker on their SOHO nodes.

**Target:** Q1 2027 (6–8 weeks)

**Already wired (2026-03-05):**
- ✅ `internal/services/catalog.go` — `ServiceCatalog` with `GetPlans`, `Provision` — initialized in `app.go`
- ✅ `internal/httpapi/server.go` — `catalog` field + `SetServiceCatalog` setter wired from `initManagedServices`
- ✅ `GET /api/marketplace/services` — lists plans from the wired catalog
- ✅ `POST /api/marketplace/purchase-service` — debit wallet + `catalog.Provision()` + order record

**Remaining (Docker provisioners):**
- [ ] `internal/services/postgres.go`: Docker pull + start, streaming replication for HA, `pg_isready` health
- [ ] `internal/services/objectstore.go`: MinIO container, bucket + service account creation
- [ ] `internal/services/queue.go`: RabbitMQ container, vhost + user provisioning
- [ ] Service management HTTP API endpoints (`POST/GET/DELETE /api/services`)
- [ ] All tests skip gracefully when Docker is unavailable

---

## 🗓 v1.0.0 — Production Readiness

**Goal:** All mobile tiers stable, P2P mesh operational, full HTTP API, AGPL compliance, auto-update system.

**Target:** Q2 2027

- [ ] Auto-update system: `internal/update/` — GitHub Releases check, Ed25519 binary signature verification, atomic binary replacement with rollback
- [ ] AGPL compliance: `/source` HTTP endpoint, `NOTICE.txt` generator, SPDX headers
- [ ] Complete HTTP API: workloads, services, storage, governance, revenue (GAP 16 all tasks)
- [ ] Full orchestration deployment: scheduler calls node agent API (`/api/worker/deploy`) instead of only recording placement DB records
- [ ] Production monitoring: OTel tracing + Prometheus metrics dashboards
- [ ] Android v0.3 and iOS v0.4 both in their respective app stores
- [ ] Documentation complete and accurate

---

## Dependency Map

```
v0.1.0 (shipped 2026-03-01)
    │
    ├──► v0.2.0  Android TV nodes          (Go WebSocket hub ✅ done; Kotlin app pending)
    │       │
    │       └──► v0.3.0  Android smartphone  (Wasm executor + HTLC ✅ done; Android app pending)
    │               │
    │               └──► v1.0.0
    │
    ├──► v0.4.0  iOS management client      (APNs ✅ done; Swift app pending)
    │       │
    │       └──► v0.9.0  iOS Core ML        (requires v0.4 base app)
    │               │
    │               └──► v1.0.0
    │
    ├──► v0.5.0  Container isolation        (independent; Linux sandbox)
    │
    ├──► v0.6.0  ML-Driven Scheduling       (Phase 0+1 ✅ shipped 2026-03-02; Phase 2-4 pending)
    │
    ├──► v0.8.0  P2P mesh                   (Phase 1 ✅ LAN discovery shipped 2026-03-04; Phase 2-3 pending)
    │
    ├──► v0.10.0 Hypervisor backends        (requires v0.5 sandbox)
    │
    └──► v0.11.0 Managed services           (catalog + HTTP API ✅ wired 2026-03-05; Docker provisioners pending)
```

---

## What Is NOT on the Roadmap

Items explicitly deferred or out of scope:

| Item | Reason |
|---|---|
| iOS background compute | Structurally impossible (iOS platform restrictions) |
| Ethereum/Polygon on-chain anchoring | Optional — local blockchain sufficient for v1.0 |
| AMD SEV confidential compute | Requires specialized hardware; deferred post-v1.0 |
| Windows sandbox (AppContainer) | Lower priority than Linux sandbox; v0.5 is Linux-only |
| Barter processor | Low usage; deferred until demand is demonstrated |

---

*Last updated: 2026-03-05 (Buyer-side marketplace + prepaid wallet shipped; P2P LAN mesh Phase 1 shipped; web dashboard Phase 1 in progress; ML Phases 0+1 shipped — see CHANGELOG.md [Unreleased])*
*Detailed mobile plan: [`docs/MOBILE_INTEGRATION.md`](docs/MOBILE_INTEGRATION.md)*
*ML scheduling research: [`docs/research/ML_LOAD_BALANCING.md`](docs/research/ML_LOAD_BALANCING.md)*
*Full change history: [`CHANGELOG.md`](CHANGELOG.md)*
