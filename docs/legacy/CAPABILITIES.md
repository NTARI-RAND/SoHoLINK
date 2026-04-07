# SoHoLINK Platform Capabilities Report

**Date:** 2026-03-05
**Scope:** Full audit of all frontend surfaces, backend API, security posture, and
known limitations. Covers all code present in the repository as of this date.

---

## Executive Summary

SoHoLINK is a federated compute marketplace for SOHO (Small Office/Home Office) hardware.
It enables owners of ordinary desktop PCs, NAS devices, mini-PCs, and mobile devices to
earn real payments by sharing spare CPU, storage, and compute capacity. The platform is
production-capable at the single-node level and coordinator level, with the following major
capability groups fully implemented: hardware discovery, federated scheduling, IPFS storage,
dual-rail payments (Stripe + Lightning), OPA policy governance, ML-driven mobile scheduling,
P2P LAN mesh discovery, **full buyer-side marketplace** (node browsing, cost estimation,
prepaid sats wallet, workload purchase, order tracking, order cancellation with proportional
refund), and a multi-surface UI (Fyne desktop dashboard, Flutter mobile app with 9 pages,
Three.js 3D globe).

The platform builds to a self-contained headless binary (`fedaaa`) and an optional CGO GUI
binary. All dependencies are vendored.

---

## 1. Frontend Surfaces

### 1.1 Fyne Desktop Dashboard (`internal/gui/dashboard/dashboard.go`)

**Status:** ✅ Fully implemented (~1,650 lines). Requires CGO + GCC/MinGW to build.

**8-Step Setup Wizard:**
1. Hardware review (auto-detected CPU, RAM, GPU, disk)
2. Pricing (auto-suggested per-hour rate based on hardware tier)
3. Network configuration (RADIUS ports, HTTP API address)
4. Payment setup (Stripe secret key env, Lightning node host/macaroon)
5. K8s edge cluster configuration (API server URL, CA cert)
6. IPFS configuration (Kubo daemon address)
7. Provisioning limits (max CPU share, max memory per job, max connections)
8. Policy review and activation

**8 Dashboard Tabs:**
| Tab | Live Data | Key Actions |
|-----|-----------|-------------|
| Overview | Node status, peer count, active rentals, revenue | — |
| Hardware | CPU model, GPU, RAM, disk utilization | Refresh |
| Orchestration | Active workloads, placements, node list | Scale, restart, delete workloads |
| Storage | IPFS pool status, CID list, quota | Upload, delete blobs |
| Billing | Revenue history, active rentals, payout requests | Request payout |
| Users | Registered user list | Add, remove users |
| Policies | OPA policy status, raw policy viewer | Toggle policy rules |
| Logs | Node log tail | — |

**7 Settings Dialogs:** Node identity, pricing, network, payment, K8s edges, IPFS, provisioning limits.

**Ease-of-Use Assessment:**
- Wizard removes all manual config file editing for first-time setup.
- Settings dialogs are modally accessible from a menu bar.
- Tab auto-refresh works without page reload.
- Weakness: Fyne build requires CGO + MinGW on Windows — extra friction for Windows developers. The headless binary avoids this entirely.

---

### 1.2 Local Web Dashboard (`ui/dashboard/`)

**Status:** ✅ Embedded in binary; served at `http://localhost:8080/dashboard`.

**Technology:** Vanilla HTML/CSS/JavaScript — no framework, no build step, works offline.

**5 Screens:**
1. **Dashboard** — radial dials for CPU, memory, storage utilization; live stat polling
2. **Plan Work** — submit compute workloads (form interface)
3. **Management** — income/payments overview; payout request form
4. **Help** — local documentation rendered in browser
5. **Settings** — inline configuration adjustments

**Architecture Decision:** Serving the dashboard from the node binary means no cloud dependency,
no login, and zero CORS issues. The browser is the renderer; the running node is the program.

**Ease-of-Use Assessment:**
- Accessible to any device on the same network via `http://<node-ip>:8080/dashboard`.
- Dark theme, cyan accent, SVG radial dials — functional and readable.
- No external JS CDN dependencies — works fully offline.
- Weakness: the web dashboard and the Fyne dashboard have some overlapping scope. The web
  dashboard is accessible from a phone; the Fyne dashboard has more capability.

---

### 1.3 Flutter Mobile App (`mobile/flutter-app/`)

**Status:** ✅ 9 pages implemented; connects to live node API.

**Technology:** Flutter (Dart), Ed25519 Option C authentication, targets web + Android + iOS.

**9 Pages:**
| Page | Purpose |
|------|---------|
| Login | Device token auth via Ed25519 challenge/response |
| Dashboard | Node health, BTC/USD rate, real-time metrics |
| Peers | Federation peer list with status indicators |
| Revenue | Earnings history with USD conversion, chart |
| Workloads | Active rental workloads with status |
| Marketplace | Browse and filter provider nodes; select node to configure workload |
| Order | Configure workload resources + duration; live cost estimate; wallet balance; pay & launch |
| Settings | Node URL configuration |
| About | App version, project links |

**Authentication Flow:** Private key → challenge endpoint → Ed25519 signature → device token
(JWT-style). Token stored on device; auto-refreshed.

**API Dependencies:**

| Endpoint | Used By |
|----------|---------|
| `GET /api/health` | Login page connectivity check |
| `GET /api/status` | Dashboard page (metrics + BTC rate) |
| `GET /api/peers` | Peers page |
| `GET /api/revenue` | Revenue page |
| `GET /api/workloads` | Workloads page |
| `GET /api/marketplace/nodes` | Marketplace page — browse providers |
| `POST /api/marketplace/estimate` | Order page — live cost estimate |
| `POST /api/marketplace/purchase` | Order page — pay and launch workload |
| `GET /api/wallet/balance` | Order page — check prepaid balance |
| `POST /api/wallet/topup` | Order page — create top-up invoice |
| `POST /api/wallet/confirm-topup` | Order page — dev/manual confirmation |
| `GET /api/orders` | Order history |
| `POST /api/orders/{id}/cancel` | Cancel order and refund |

**Currency Display:** All earnings shown in USD; converted from satoshis using `btc_usd_rate`
from `/api/status`. Format: `NumberFormat.currency(symbol: '\$')`.

**Known Configuration Issue:** `kNodeUrl` is hardcoded to `http://192.168.1.220:4000` in
`lib/api/soholink_client.dart`. Users must edit this and rebuild to point at their own node.
This is intentional for zero-setup demo use but is a deployment friction point.

**Ease-of-Use Assessment:**
- Combined monitoring + marketplace design: operators check their node; requesters browse and buy compute — same app.
- Ed25519 auth is secure but requires the private key to be present on the mobile device.
- USD conversion requires the node to expose a live BTC price feed.
- Port mismatch: mock server uses port 4000, live `fedaaa` defaults to 8080 — users must
  remember to change both the IP and port.

---

### 1.4 Three.js 3D Globe (`ui/globe-interface/ntarios-globe.html`)

**Status:** ✅ Fully implemented. Requires a running WebSocket bridge.

**Technology:** Three.js, WebSocket, vanilla JS.

**Modes:**
- **Topology Mode (TOPO):** nodes positioned algorithmically on the globe surface; useful for
  visualizing federation graph structure without real location data.
- **Geographic Mode (GEO):** nodes positioned at real lat/lon coordinates received via
  WebSocket (`lat`, `lon`, `region` fields). Auto-activates when coordinates are present.

**Features:**
- Animated data-flow arcs between active node pairs
- Node health color coding (healthy = green, degraded = yellow, offline = red)
- GEO/TOPO toggle button
- WebSocket reconnect on disconnect

**Ease-of-Use Assessment:**
- Beautiful and informative for demos and operations centers.
- Requires a separate WebSocket bridge service to populate node data — not included in the
  headless binary's default configuration.
- Not served from the binary itself (unlike the web dashboard) — must be opened separately.

---

## 2. Backend API (`internal/httpapi/`)

### 2.1 Endpoint Inventory (46+ routes)

**Public endpoints (no auth required):**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Health check — returns `{"status":"ok"}` |
| GET | `/api/federation/info` | Coordinator metadata (DID, fee, regions) |
| GET | `/api/federation/peers` | List active registered nodes |
| POST | `/api/federation/announce` | Register/re-register provider node (Ed25519 signed) |
| POST | `/api/federation/heartbeat` | Keep-alive + resource update (Ed25519 signed) |
| POST | `/api/federation/deregister` | Clean offline notification |
| GET | `/api/peers` | LAN mesh peer list (P2P discovery) |
| GET | `/dashboard/*` | Embedded web dashboard static assets |
| GET | `/ws/nodes` | Mobile node WebSocket hub (upgrade) |

**Protected endpoints (device token required):**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/status` | Full node status (metrics, BTC rate, peer count) |
| GET | `/api/workloads` | List active workloads |
| POST | `/api/workloads` | Submit new workload |
| GET | `/api/workloads/{id}` | Get workload state |
| PATCH | `/api/workloads/{id}` | Update workload (replicas, cpu_cores, memory_mb) |
| DELETE | `/api/workloads/{id}` | Delete workload |
| POST | `/api/workloads/{id}/restart` | Re-queue workload for rescheduling |
| GET | `/api/workloads/{id}/logs` | Placement topology + node agent addresses |
| GET | `/api/workloads/{id}/metrics` | Workload health + placement count |
| GET | `/api/workloads/{id}/events` | Recent workload events |
| GET | `/api/revenue` | Revenue history and totals |
| POST | `/api/revenue/request-payout` | Initiate provider payout |
| GET | `/api/revenue/payouts` | Payout history |
| GET | `/api/rentals` | Active rental list |
| POST | `/api/rentals/accept` | Accept pending rental |
| POST | `/api/rentals/reject` | Reject pending rental |
| GET | `/api/nodes` | Federation node registry |
| POST | `/api/v1/nodes/mobile/register` | Register mobile compute node |
| GET | `/api/v1/nodes/mobile` | List active mobile nodes |
| GET | `/api/users` | List registered users |
| POST | `/api/users` | Register new user |
| DELETE | `/api/users/{did}` | Remove user |
| GET | `/api/policy` | Current OPA policy status |
| POST | `/api/policy/evaluate` | Evaluate policy for a given input |
| GET | `/api/storage/status` | IPFS pool + local storage stats |
| POST | `/api/storage/upload` | Upload blob to storage pool |
| GET | `/api/storage/blobs` | List stored blobs |
| DELETE | `/api/storage/blobs/{id}` | Delete blob |
| GET | `/api/payments` | Payment processor status |
| GET | `/api/accounting` | Accounting event log |
| GET | `/api/blockchain` | Local chain head and integrity |
| **GET** | **`/api/marketplace/nodes`** | Browse provider nodes; filters: `min_cpu`, `max_price_sats`, `region`, `gpu`, `min_reputation` |
| **POST** | **`/api/marketplace/estimate`** | Estimate cost in sats + USD for CPU/RAM/disk/duration spec |
| **GET** | **`/api/marketplace/services`** | List managed service plans from catalog |
| **POST** | **`/api/marketplace/purchase`** | Debit prepaid wallet + submit workload; returns `{order_id, workload_id, charged_sats}` |
| **POST** | **`/api/marketplace/purchase-service`** | Debit wallet + provision managed service instance |
| **GET** | **`/api/orders`** | List requester's orders (`?limit=N`) |
| **GET** | **`/api/orders/{id}`** | Order detail with live scheduler status |
| **POST** | **`/api/orders/{id}/cancel`** | Cancel order; proportional refund → wallet |
| **GET** | **`/api/wallet/balance`** | Prepaid balance in sats, BTC, USD |
| **POST** | **`/api/wallet/topup`** | Create Lightning invoice or Stripe payment intent |
| **GET** | **`/api/wallet/topups`** | Topup history |
| **POST** | **`/api/wallet/confirm-topup`** | Manual/dev topup confirmation |

### 2.2 Authentication

- **Device token** (protected endpoints): Ed25519 challenge/response via `/auth/challenge` +
  `/auth/token`; token stored in `Authorization: Bearer <token>` header.
- **Federation announce/heartbeat** (public endpoints): Ed25519 signature over canonical
  message included in request body; verified against stored public key.
- **CORS**: open `*` origin by design for Flutter web; protected endpoints still require
  a valid device token regardless of origin.

---

## 3. Security Posture

### 3.1 Strengths

| Area | Assessment |
|------|-----------|
| Federation auth | ✅ Ed25519 signatures required on announce and heartbeat; forged registrations rejected |
| Rate limiting | ✅ Per-IP sliding-window limiter on all mutating endpoints; mobile WebSocket + federation covered |
| Payment gating | ✅ Lightning HTLC hold invoices — payment released only after verification |
| TLS enforcement | ✅ TLS 1.2 minimum on K8s edges and LND connections |
| LND cert pinning | ✅ x509 CertPool from PEM bundle; `InsecureSkipVerify` only as documented fallback |
| OPA governance | ✅ 57 passing policy tests; HTLC lifecycle events covered |
| Tamper-evident logs | ✅ SHA3-256 Merkle chain over billing events |
| Startup validation | ✅ Non-fatal warnings for 7 common misconfigurations |
| gosec | ✅ All 29 HIGH findings resolved; `// #nosec` annotations with explanatory comments |
| Input validation | ✅ Field-level validation on all federation and workload endpoints |

### 3.2 Known Gaps / Future Work

| Area | Risk | Notes |
|------|------|-------|
| CORS open `*` | Low | Intentional for Flutter web; protected by device token auth |
| LND `InsecureSkipVerify` fallback | Medium | Falls back when `lnd_tls_cert_path` is unset; startup validator warns; production deployments must set the path |
| `nodeagent.go` compile errors | Medium | Pre-existing method redeclaration and missing struct fields; file is excluded from build targets |
| Mobile hardcoded URL | Low | `kNodeUrl` hardcoded in Flutter client; trivial to change but requires rebuild |
| Payout processor routing | Low | Payout record created immediately; processor dispatching is best-effort; failed payouts stay `"pending"` until manual retry |
| Prometheus endpoints | Low | `compute/prometheus.go` and `services/prometheus.go` have outstanding TODO stubs |
| WebSocket globe bridge | Info | Not shipped in binary; requires separate bridge service |

### 3.3 CVE / Dependency Status

- **GO-2026-4394** `go.opentelemetry.io/otel/sdk`: patched in v1.40.0 (updated)
- **GO-2026-4337** `crypto/tls`: mitigated by requiring Go ≥ 1.25.7 in `go.mod`
- Dependabot configured for weekly Go module + Actions PRs; monthly full-upgrade sweep workflow

---

## 4. Functionality Assessment

### 4.1 What Works End-to-End Today

| Feature | Status | Notes |
|---------|--------|-------|
| Hardware auto-detection | ✅ | Windows/Linux/macOS; GPU via WMI, /sys, IOKit |
| Provider onboarding wizard | ✅ | 8-step Fyne wizard or headless YAML config |
| RADIUS auth server | ✅ | Port 1812 (auth) + 1813 (accounting) |
| Ed25519 DID credentials | ✅ | `did:key` format; offline verification |
| Federation announce/heartbeat | ✅ | Ed25519 signed; rate limited; stored in SQLite |
| FedScheduler workload placement | ✅ | Scoring by available resources, price, reputation |
| Workload CRUD via REST API | ✅ | List, get, submit, patch, delete, restart |
| Auto-scaler | ✅ | Monitors health; adjusts replicas based on queue depth |
| IPFS storage pool | ✅ | Upload/download/delete via Kubo HTTP API; CID tracking |
| Stripe payment processing | ✅ | CreateCharge, ConfirmCharge, RefundCharge, GetStatus |
| Lightning HTLC invoices | ✅ | CreateHoldInvoice, SettleHoldInvoice, CancelHoldInvoice |
| Per-hour usage metering | ✅ | FedScheduler → UsageMeter → PaymentLedger wired |
| Provider payout requests | ✅ | `POST /api/revenue/request-payout` → DB record + processor |
| Payout history | ✅ | `GET /api/revenue/payouts` returns real DB rows |
| **Requester node browsing** | **✅** | `GET /api/marketplace/nodes`; filters: min_cpu, region, GPU, price, reputation |
| **Cost estimation** | **✅** | `POST /api/marketplace/estimate`; 100 sats/vCPU/hr, 10 sats/GB-RAM/hr, 1 sat/GB-disk/hr + 1% fee |
| **Prepaid satoshi wallet** | **✅** | Credit via Lightning invoice / Stripe; atomic debit on purchase; balance in sats/BTC/USD |
| **Workload purchase** | **✅** | `POST /api/marketplace/purchase`; wallet debit + workload submission + order record — atomic |
| **Managed service purchase** | **✅** | `POST /api/marketplace/purchase-service`; catalog lookup + wallet debit + Docker provisioning |
| **Order tracking** | **✅** | `GET /api/orders`, `GET /api/orders/{id}` with live scheduler status |
| **Order cancellation + refund** | **✅** | `POST /api/orders/{id}/cancel`; proportional unused-hours refund to wallet |
| OPA policy governance | ✅ | 57 test cases; HTLC + mobile + compute + storage rules |
| ML-driven mobile scheduling | ✅ | LinUCBBandit; falls back to random if unset |
| LAN mesh P2P discovery | ✅ | Ed25519 multicast UDP; auto-upserts peers into federation_nodes |
| Mobile WebSocket hub | ✅ | Gorilla WebSocket; 30s ping; 90s heartbeat timeout |
| Mobile task assignment | ✅ | ScheduleMobile, replication, preemption |
| APNs push notifications | ✅ | JWT auto-refresh, SendJobRequest, SendPaymentReceived |
| Local blockchain accounting | ✅ | SHA3-256 Merkle chain; VerifyChainIntegrity |
| LBTAS reputation scoring | ✅ | Trust-based node selection weight |
| CDN geographic routing | ✅ | Active health probing, score-based routing |
| SLA contract monitoring | ✅ | Tiered plans, credit computation |
| Schema migrations | ✅ | Versioned, idempotent, auto-applied on startup (v3 adds wallet + orders tables) |
| Startup config validation | ✅ | 7 production-readiness checks with log warnings |
| Windows service installer | ✅ | Scheduled Task (SYSTEM, at-boot, auto-restart) |
| GoReleaser distribution | ✅ | Windows NSIS, macOS universal, Linux deb/rpm/AppImage |

### 4.2 What Is Partially Implemented

| Feature | Status | Remaining Work |
|---------|--------|---------------|
| Wasm task executor | ⚠️ Stub | `StubExecutor` always returns mock output; wazero integration planned for v0.3 |
| Mobile native apps | ⚠️ Planned | Go protocol layer complete; native Android/iOS apps in development |
| Log streaming | ⚠️ Topology only | `GetWorkloadLogs` returns placement topology; real log streaming requires node agent on each provider |
| Stripe Connect payouts | ⚠️ Initiated | Payout record created and ledger called; actual Stripe Connect flow requires provider onboarding |
| Prometheus metrics | ⚠️ Stubbed | `compute/prometheus.go` and `services/prometheus.go` have TODO stubs for metric registration |

### 4.3 What Is Not Implemented

| Feature | Notes |
|---------|-------|
| Native Android app | Flutter app covers monitoring; background compute requires native SDK |
| Native iOS app | Flutter app covers monitoring; no compute role planned |
| Wasm execution (real) | Stub only; wazero integration scheduled |
| Distributed tracing (full) | OpenTelemetry imported but trace propagation not wired end-to-end |
| Multi-tenant coordinator | Coordinator role works; tenant isolation (per-org billing) not implemented |
| Federated ledger / settlement | Cross-coordinator payment settlement not implemented |

---

## 5. Ease-of-Use Assessment

### 5.1 Provider Onboarding

**Score: 8/10**

The 8-step wizard removes all manual YAML editing. Hardware is auto-detected, pricing is
auto-suggested based on CPU/GPU/RAM tier, and policies are pre-populated. The only friction
is the CGO build requirement for the Fyne GUI on Windows (MinGW needed). The headless binary
(`fedaaa`) with the embedded web dashboard sidesteps this entirely.

### 5.2 Requester Experience

**Score: 8/10**

Requesters browse available provider nodes via `GET /api/marketplace/nodes` (with CPU, region,
GPU, price, and reputation filters), get live cost estimates via `POST /api/marketplace/estimate`,
top up a prepaid satoshi wallet via Lightning or Stripe, and launch workloads with a single
`POST /api/marketplace/purchase` call. The Flutter mobile app exposes the full buyer flow —
Marketplace tab → configure node → Order page (sliders, live estimate, wallet balance) → Pay
& Launch → order confirmation. Order history and cancel/refund are available from both API and
app. A hosted coordinator web UI would further raise this for non-technical requesters.

### 5.3 Operator / Day-2 Experience

**Score: 7/10**

The 8-tab Fyne dashboard gives real-time visibility into all subsystems. The local web
dashboard provides the same from any browser on the LAN. The 3D globe provides visual
federation topology. Log access is topology-only for now (no in-dashboard log streaming).
The startup config validator catches misconfigurations early.

### 5.4 Developer Experience

**Score: 8/10**

- Pure-Go headless binary requires no CGO.
- All dependencies vendored; reproducible offline builds.
- `make build-cli` / `make test` / `make dist` cover all workflows.
- OPA policy tests: `opa test configs/policies/ --v1-compatible`.
- CI: GitHub Actions (build + test + gosec + OPA test).
- Schema migrations are append-only and auto-applied.
- The pre-existing `nodeagent.go` compile errors are a minor friction point.

---

## 6. Performance Characteristics

| Metric | Observed / Design |
|--------|------------------|
| Billing resolution | 1-hour intervals; 60-second minimum billable period |
| Scheduler placement | Synchronous; O(N) scan over active nodes at dispatch time |
| ML scoring (LinUCB) | O(d²) per arm at dispatch; d=20 context dimensions |
| P2P discovery | 10-second announce interval; 45-second peer TTL; 15-second stale reaper |
| WebSocket ping | 30-second interval; 90-second heartbeat timeout |
| Rate limit windows | 1-minute sliding window; per-IP buckets |
| DB | SQLite via modernc.org/sqlite (no CGO); WAL mode for concurrent readers |

---

## 7. Deployment Topology

### Minimal (Single Node, Solo Provider)

```
[SOHO PC]
  fedaaa  → serves HTTP API on :8080
           → RADIUS on :1812/:1813
           → LAN mesh UDP on :7946
           → Fyne GUI or web dashboard at /dashboard
```

### Federation (Coordinator + Providers)

```
[Coordinator Node]
  fedaaa (is_coordinator=true)
  → /api/federation/* — registry
  → /api/workloads    — placement coordinator
  → /api/revenue/*    — payout management

[Provider Nodes] (1..N)
  fedaaa (coordinator_url=http://coordinator:8080)
  → announces resources via POST /api/federation/announce
  → heartbeats every 30s
  → runs workloads placed by coordinator
```

### Extended (with Mobile + Globe)

```
[Coordinator] ← REST API
[Providers]   ← heartbeat
[Android TV]  ← WebSocket /ws/nodes
[iPhone]      ← Flutter app REST
[Browser]     ← 3D Globe WebSocket bridge
```

---

## 8. File Inventory Summary

| Directory | Files | Purpose |
|-----------|-------|---------|
| `internal/orchestration/` | ~12 | FedScheduler, mobile, ML features, K8s adapter, workload types |
| `internal/payment/` | ~8 | Stripe, Lightning, HTLC, ledger, settler, meter, payout |
| `internal/httpapi/` | ~17 | All REST handlers (46+ routes), WebSocket hub, rate limiter, marketplace, wallet |
| `internal/store/` | ~11 | SQLite store, migrations (v3 + wallet + orders), federation, central, LBTAS |
| `internal/ml/` | ~4 | LinUCBBandit, TelemetryRecorder, features |
| `internal/wizard/` | ~8 | Hardware detection (Win/Lin/Mac), cost calc, pricing |
| `internal/gui/dashboard/` | ~1 | Fyne dashboard (~1,650 lines) |
| `internal/p2p/` | ~2 | LAN mesh discovery, peers HTTP handler |
| `internal/auth/` | ~4 | Ed25519 DID credentials |
| `internal/policy/` | ~3 | OPA policy engine |
| `internal/blockchain/` | ~3 | Local Merkle chain |
| `internal/compute/` | ~8 | Firecracker, KVM, Hyper-V, cgroups, migration, Wasm stub |
| `configs/policies/` | ~2 | OPA Rego + 57 test cases |
| `ui/dashboard/` | ~3 | Embedded web dashboard (HTML/CSS/JS) |
| `ui/globe-interface/` | ~1 | Three.js 3D globe |
| `mobile/flutter-app/` | ~25 | Flutter mobile app (9 pages — includes marketplace + order buyer flow) |
| `cmd/` | ~4 | Binary entry points (CLI, GUI, fedaaa) |
| `docs/` | ~8 | Architecture, install, operations, mobile, research reports |

---

## 9. Recommendations

### Immediate (before public beta)

1. **Fix `nodeagent.go` compile errors** — method redeclaration and missing struct fields
   prevent the node agent package from being included in provider builds.
2. **Configurable mobile node URL** — the Flutter app's hardcoded `kNodeUrl` should become a
   runtime setting (stored in SharedPreferences or passed via URL parameter) rather than a
   compile-time constant.
3. **Real Wasm executor** — replace the `StubExecutor` with wazero to actually run submitted
   tasks.

### Short-Term (v0.2 milestone)

4. **Log streaming** — implement an SSH-proxied log streaming endpoint in `GetWorkloadLogs` or
   ship a lightweight node agent sidecar so operators can see actual container/VM logs from
   the dashboard.
5. **Hosted coordinator UI** — a simple HTML coordinator dashboard (listing providers, pricing,
   workload queue) would dramatically lower the bar for requesters.
6. **Stripe Connect onboarding** — complete the Stripe Connect flow so `request-payout` actually
   triggers real bank transfers rather than creating pending records.
7. **Prometheus endpoint wiring** — complete the metric registration stubs in
   `compute/prometheus.go` and `services/prometheus.go`.

### Long-Term

8. **Federated ledger** — cross-coordinator settlement for workloads that span multiple
   federation domains.
9. **Native Android/iOS compute** — the Go protocol layer and OPA policies are ready;
   the platform-native background execution layer (WorkManager, BGTaskScheduler) needs
   to be built.

---

*This report was generated by an automated audit of the SoHoLINK codebase on 2026-03-05.*
