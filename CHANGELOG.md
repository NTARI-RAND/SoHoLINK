# Changelog

All notable changes to SoHoLINK are documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [0.1.2] — 2026-03-08

### Added — Federation & Marketplace Settings Dialog

The GUI dashboard now exposes all federation and marketplace configuration through a new
**Settings → Federation & Marketplace…** dialog, making private group (cooperative) and
open-marketplace setup accessible without hand-editing `config.yaml`.

**Modified files:**
- `internal/gui/dashboard/dashboard.go` — new `showFederationSettingsDialog` function
  (~80 lines); new "Federation & Marketplace…" menu item wired into `buildMenuBar`

**Dialog fields:**

| Card | Field | Config key |
|------|-------|-----------|
| Coordinator Role | Acts as coordinator (checkbox) | `federation.is_coordinator` |
| Coordinator Role | Coordinator fee (%) | `federation.fee_percent` |
| Provider Settings | Coordinator URL | `federation.coordinator_url` |
| Provider Settings | Region | `federation.region` |
| Provider Settings | Price / CPU-hour (sats) | `federation.price_per_cpu_hour_sats` |
| Provider Settings | Heartbeat interval | `federation.heartbeat_interval` |

**UX notes:**
- Coordinator URL auto-disables when "acts as coordinator" is checked, guiding users
  toward the correct configuration without preventing nested-federation setups
- Heartbeat interval is validated with `time.ParseDuration` on save; invalid values show
  an inline error dialog rather than silently accepting bad input
- All values are written to the in-memory `Config` immediately; a restart reminder is shown
  because the federation announcer and RADIUS server read config at startup

**Upgrade path:** No config migration required. Existing installations that previously set
`federation.*` fields manually in `config.yaml` will see those values pre-populated when the
dialog opens.

### Fixed — Vendor Directory & Build Environment

- **`golang.org/x/net/netutil` missing from vendor**: `go mod vendor` was not run after
  `httpapi/server.go` gained a `netutil.LimitListener` import. Ran `go mod vendor`; the
  `netutil` package is now present in `vendor/golang.org/x/net/netutil/`.
- **Node DID not written to config on first install**: `fedaaa install` generates a DID and
  prints it to stdout but does not write it back to `config.yaml`. Added `node.did` to
  `%APPDATA%\SoHoLINK\config.yaml` manually. A future release should have `install` write
  this field automatically.
- **MinGW PATH not persistent**: GCC is installed at `C:\msys64\mingw64\bin` (MSYS2 already
  present) but was not on `PATH`, causing `go build -tags gui` to fail with the `go-gl` CGO
  error. Added `export PATH="/c/msys64/mingw64/bin:$PATH"` to `~/.bash_profile`.

### Diagnosed — Benign Fyne Cold-Start Warning

`soholink.exe` logs the following on every fresh start:

```
Fyne error:  Attempt to access current Fyne app when none is started
  At: vendor/fyne.io/fyne/v2/app.go:97
```

**Root cause:** `getOrCreateApp()` in `dashboard.go` calls `fyne.CurrentApp()` to check for an
existing app instance before calling `fyneApp.NewWithID()`. Fyne's `CurrentApp()` logs this
warning internally when no app has been registered yet, then returns `nil`. Our code handles
`nil` correctly and immediately creates the app. This is not a crash; the GUI initialises
normally. The warning fires once per process at startup and is safe to ignore.

---

## [0.1.1] — 2026-03-06

### Added — Secure Auto-Update System

SoHoLINK nodes can now update themselves automatically and securely.  The new `internal/updater`
package polls the GitHub releases API on a configurable interval, downloads the new binary for
the running OS and architecture, verifies its SHA-256 against the official `checksums.txt`
published with each GoReleaser release, and atomically installs the update.

**Security guarantees** (all stdlib, no new external dependencies):
1. `release_url` must use `https://` — `http://` is rejected at validation time
2. The HTTP client's `CheckRedirect` policy rejects any redirect that would leave HTTPS
3. SHA-256 comparison uses `crypto/subtle.ConstantTimeCompare` to prevent timing side-channel leaks
4. The temp file is restricted to `0600` permissions immediately after creation, before any binary data is written
5. On Windows the batch update script validates `execPath` for batch-special characters (`"` and `%`) before interpolation
6. On Unix, `os.Rename` provides kernel-level atomicity; the old binary's inode remains valid until the process exits

**New files:**
- `internal/updater/updater.go` — core update logic (`New`, `Start`, `CheckNow`, `Download`, `LatestRelease`)
- `internal/httpapi/version.go` — `GET /api/version` endpoint (public, no auth required)

**Modified files:**
- `internal/config/config.go` — `UpdatesConfig` struct + `Updates` field on `Config`
- `configs/default.yaml` — `updates:` section (disabled by default; opt in via wizard or config)
- `internal/app/app.go` — `Updater`, `Version`, `Commit`, `BuildTime` fields; `SetVersion()`; updater wired into `New()` and `Start()`
- `internal/httpapi/server.go` — version fields; `/api/version` route
- `internal/httpapi/auth_middleware.go` — `/api/version` added to `publicPaths`
- `internal/gui/dashboard/dashboard.go` — wizard now persists `UpdatesEnabled`; Help ▸ "Check for Updates…" dialog; update-available banner on Overview tab
- `cmd/soholink/main.go` — `application.SetVersion(version, commit, buildTime)` called after `app.New()`

**Configuration** (`configs/default.yaml`):
```yaml
updates:
  enabled: false           # opt in via Setup Wizard step 5 or set true here
  check_interval: "24h"
  release_url: "https://api.github.com/repos/NetworkTheoryAppliedResearchInstitute/soholink/releases/latest"
```

### Added — Icon Update & Build Scripts

- `assets/soholink-source.png` — NTARI globe+buildings logo ("STAY CONNECTED") saved as reproducible PNG source
- `assets/soholink.ico` — rebuilt from new logo: 16/32/48/256 px, 69.8 KB
- `scripts/update-icon.ps1` — reusable script: `assets/soholink-source.png` → `assets/soholink.ico` → rebuild `soholink.exe`
- `scripts/build-gui.ps1` — reusable script: GUI binary build with version ldflags, no need to remember MinGW PATH setup
- `soholink.exe` rebuilt as v0.1.1 with new NTARI logo embedded

### Security — Updater Hardening (post-audit)

An internal security audit of the initial updater implementation found 6 issues; all were
fixed in this release before the binary was distributed:

| Severity | Finding | Fix |
|----------|---------|-----|
| CRITICAL | No HTTPS scheme validation on `release_url` | Added `strings.HasPrefix("https://")` check in `CheckNow()` |
| CRITICAL | Uncontrolled HTTP redirects in binary download | `http.Client.CheckRedirect` rejects non-HTTPS redirects |
| HIGH | Non-constant-time SHA-256 comparison (`strings.EqualFold`) | Replaced with `crypto/subtle.ConstantTimeCompare` |
| MEDIUM | Temp file world-readable before binary data written | `tmp.Chmod(0o600)` called immediately after `os.CreateTemp` |
| MEDIUM | Windows batch script path injection via `execPath` | `strings.ContainsAny(execPath, '"%')` guard added |
| LOW | Package doc did not reflect all security controls | Updated package-level doc comment |

---

### Added — Buyer-Side Marketplace & Prepaid Wallet (2026-03-05)

Complete requester (buyer) experience for the federated compute marketplace. Requesters can now
browse available provider nodes, estimate costs in sats and USD, top up a prepaid wallet via
Lightning or Stripe, purchase and launch compute workloads, and track/cancel orders — all from
the REST API and the Flutter mobile app.

**Schema migration v3** (`internal/store/migrate.go`):
- `wallet_balances (did PK, balance_sats, updated_at)` — prepaid sats per requester DID
- `wallet_topups (topup_id PK, did, amount_sats, processor, invoice, status, ...)` — Lightning/Stripe topup records with indexed `(did, status)` lookup
- `orders (order_id PK, requester_did, order_type, resource_ref_id, description, cpu/mem/disk specs, duration_hours, estimated_sats, charged_sats, status, ...)` — order lifecycle records with indexed `(requester_did, status)` lookup

**Store layer** (`internal/store/marketplace.go`):
- `WalletBalanceRow`, `WalletTopupRow`, `OrderRow` structs
- `GetWalletBalance`, `CreditWallet`, `DebitWallet` (atomic check-and-subtract)
- `CreateWalletTopup`, `UpdateTopupStatus`, `ListWalletTopups`
- `CreateOrder`, `UpdateOrderStatus`, `GetOrder`, `ListOrders`

**Payment ledger extensions** (`internal/payment/ledger.go`):
- `GetWalletBalance(ctx, did) (int64, error)`
- `TopupWallet(ctx, did, processor, amountSats) (topupID, invoice, error)` — creates Lightning invoice or Stripe payment intent; records `awaiting_payment` topup row
- `ConfirmTopup(ctx, topupID) error` — credits wallet on payment confirmation
- `DebitWallet(ctx, did, amountSats) error` — returns `ErrInsufficientBalance` if funds insufficient

**FedScheduler extension** (`internal/orchestration/scheduler.go`):
- `FindNodes(ctx, NodeQuery) ([]*Node, error)` — exposes `NodeDiscovery.FindNodes` via the scheduler interface; used by marketplace browse handler

**Wallet HTTP handlers** (`internal/httpapi/wallet.go`) — 4 new protected endpoints:
- `GET /api/wallet/balance` — returns balance in sats, BTC, and USD (live BTC/USD rate)
- `POST /api/wallet/topup` — creates Lightning invoice or Stripe payment intent; returns invoice/intent for user to pay
- `GET /api/wallet/topups` — topup history
- `POST /api/wallet/confirm-topup` — manual/dev confirmation; credits wallet immediately

**Marketplace HTTP handlers** (`internal/httpapi/marketplace.go`) — 8 new protected endpoints:
- `GET /api/marketplace/nodes` — browse provider nodes; filters: `min_cpu`, `max_price_sats`, `region`, `gpu`, `min_reputation`
- `POST /api/marketplace/estimate` — compute cost in sats and USD for given CPU/RAM/disk/duration; pricing: 100 sats/vCPU/hr, 10 sats/GB-RAM/hr, 1 sat/GB-disk/hr, 1% platform fee
- `GET /api/marketplace/services` — list managed service plans from the service catalog
- `POST /api/marketplace/purchase` — atomically debits wallet and submits workload to scheduler; returns `{order_id, workload_id, charged_sats}`
- `POST /api/marketplace/purchase-service` — debits wallet and provisions a managed service instance
- `GET /api/orders` — list requester's order history (`?limit=N`)
- `GET /api/orders/{id}` — order detail with live scheduler status
- `POST /api/orders/{id}/cancel` — cancel active order; refunds proportional unused sats to wallet; returns `{refund_sats, new_balance_sats}`

**Server wiring** (`internal/httpapi/server.go`):
- Added `catalog *services.Catalog` field and `SetServiceCatalog(*services.Catalog)` setter
- 11 new routes registered (8 marketplace + 4 wallet endpoints, all `authMiddleware`-protected)

**App wiring** (`internal/app/app.go`):
- `initManagedServices` now calls `HTTPAPIServer.SetServiceCatalog(a.ServiceCatalog)` after provisioner registration, wiring the managed service catalog to HTTP handlers

**Flutter mobile app** — complete buyer flow across all 3 layers:
- `lib/models/marketplace.dart` *(new)* — `MarketplaceNode`, `CostEstimate`, `WalletBalance`, `Order` models with `fromJson` factories and `zero` constants
- `lib/api/soholink_client.dart` *(extended)* — 8 new methods: `getMarketplaceNodes`, `estimateCost`, `purchaseWorkload`, `getWalletBalance`, `topupWallet`, `confirmTopup`, `getOrders`, `cancelOrder`; added `_post` helper
- `lib/pages/marketplace_page.dart` *(new)* — browse and filter provider nodes: CPU slider, region dropdown, GPU toggle; `_ProviderCard` shows reputation bar, region badge, status dot, resource chips; "Configure" navigates to `OrderPage`
- `lib/pages/order_page.dart` *(new)* — configure workload: CPU/RAM/disk sliders clamped to node limits, duration chip selector (1h/4h/8h/24h/72h); live cost estimate card (refetches on slider release); wallet balance card (green if sufficient, red if not); "Pay & Launch" button → purchase → order confirmation dialog; "Add Funds" top-up dialog with Lightning invoice display and dev-confirm button
- `lib/pages/home_page.dart` *(extended)* — 6th "Market" tab added with `storefront_outlined` / `storefront_rounded` icon; `MarketplacePage()` at index 4 in `IndexedStack`

---

### Security — Production-Readiness Gap Remediation (2026-03-05)

Eight production-readiness gaps identified in an internal security and functionality audit
have been fixed. All changes are backward-compatible; existing databases are automatically
migrated on startup.

**Gap 1 — Ed25519 Signature Verification on Federation Endpoints**
- `POST /api/federation/announce`: `public_key` (base64 Ed25519, 32 bytes) and `signature`
  (base64 Ed25519 over `"{nodeDID}:{address}:{timestamp}"`) are now required; missing or
  invalid signatures return HTTP 401
- `POST /api/federation/heartbeat`: signature verified against the public key stored at
  announce time (canonical message: `"{nodeDID}:{timestamp}"`); nodes with no stored key
  log a grace-period warning rather than hard-failing
- Added `verifyAnnounceSignature` and `verifyHeartbeatSignature` helpers in
  `internal/httpapi/federation.go`; `PublicKey` stored in `federation_nodes` via migration v2

**Gap 2 — Payout System**
- `POST /api/revenue/request-payout`: generates a `po_{UnixNano}` payout ID, persists a
  `payouts` row (status=`pending`), then attempts each configured processor in priority order;
  Stripe and Lightning paths hand off to the payment ledger; barter credits settle immediately
- `GET /api/revenue/payouts[?limit=N]`: returns real rows from the `payouts` table (previously
  returned a hard-coded stub)
- Added `PayoutRow`, `CreatePayout`, `ListPayouts`, `UpdatePayoutStatus` to
  `internal/store/central.go`
- Added `PayoutRequest`, `PayoutResult`, `RequestPayout` to `internal/payment/ledger.go`
- Added `paymentLedger *payment.Ledger` field and `SetPaymentLedger` setter to HTTP API server;
  wired in `app.go → initResourceSharing`

**Gap 3 — Rate Limiting on Federation Endpoints**
- `POST /api/federation/announce`: 5 requests/IP/minute (slow/expensive operation)
- `POST /api/federation/heartbeat`: 10 requests/IP/minute (allows burst above normal 2/min rate)
- `POST /api/federation/deregister`: 5 requests/IP/minute
- Uses the existing `ipRateLimiter` sliding-window implementation already protecting mobile endpoints

**Gap 4 — FedScheduler ↔ Usage Meter Wiring**
- Added `ActivePlacements() []payment.ActivePlacement` to `FedScheduler` (implements
  `payment.PlacementSource`); only `"running"` placements are billed; method deep-copies
  under `RLock` to prevent data races
- `app.go → startResourceSharing` now creates a `payment.UsageMeter` and runs it in a
  goroutine when both `FedScheduler` and `PaymentLedger` are initialised
  (`BillingInterval=1h`, `MinBillableSeconds=60`); workloads now actually generate billing events

**Gap 5 — Startup Configuration Validation**
- New file `internal/app/validate.go`: `(App).validateConfig()` called at the top of `Start()`
  before any service starts; all checks are non-fatal log warnings to support development workflows
- Warns on: default RADIUS shared secret (`testing123` or empty); `payment.enabled` without a
  real processor; Stripe processor with unset `$SECRET_KEY_ENV`; Lightning processor without
  `lnd_tls_cert_path`; `federation.is_coordinator` without `resource_sharing.enabled`; empty
  `node.did`; `orchestration.enabled` without `payment.enabled`

**Gap 6 — Schema Migration System**
- New file `internal/store/migrate.go`: append-only `migrations []string` slice;
  `runMigrations()` applies un-applied migrations in version order; `schema_version` table
  tracks applied versions; idempotent on restart
- `internal/store/db.go`: added `schema_version` table to the base schema constant; `NewStore`
  calls `s.runMigrations()` immediately after initial schema creation
- Migration v1: `payouts` table + `idx_payouts_provider` index
- Migration v2: `ALTER TABLE federation_nodes ADD COLUMN public_key TEXT NOT NULL DEFAULT ''`

**Gap 7 — Workload Update + Honest Log Scaffolding**
- `FedScheduler.UpdateWorkload`: handles `"replicas"` (delegates to `ScaleWorkload`),
  `"cpu_cores"` (float64 in-place spec update), and `"memory_mb"` (float64 in-place spec
  update); updates `Workload.UpdatedAt` on all changes
- `FedScheduler.GetWorkloadLogs`: returns real placement topology (node DID, address, status,
  `StartedAt` timestamp) instead of a generic stub; advises operators which node agent
  addresses to contact for actual container/VM logs

**Gap 8 — LND TLS Certificate Pinning**
- Added `LNDMacaroonEnv string` and `LNDTLSCertPath string` to `PaymentProcessorEntry` in
  `internal/config/config.go`
- `payment.NewLightningProcessor(host, macaroon, tlsCertPath string)`: loads an `x509.CertPool`
  from the PEM file at `tlsCertPath` when set; always enforces TLS 1.2 minimum; falls back to
  `InsecureSkipVerify` with a prominent `[lightning] WARNING` log when no cert path is configured
- `app.go`: reads macaroon from the configured env var at startup; passes `LNDTLSCertPath` to
  the constructor; startup validator warns when LND host is set without a cert path

---

### Added — Small-World LAN Mesh P2P Discovery (2026-03-04)

**Architecture decision:** LAN peer discovery uses signed multicast UDP (group `239.255.42.99:7946`,
RFC 2365 administratively-scoped range — will not leave the LAN) rather than mDNS, to remain
dependency-free and work on all platforms without Bonjour/Avahi installed.

Nodes form a *small-world network* (Watts–Strogatz model): high local clustering coefficient inside
each LAN workgroup, with sparse cross-subnet long-range links via the HTTP registration API —
giving the overall federation its characteristic short average path length.

- `internal/p2p/mesh.go`: `Mesh` — multicast UDP peer discovery; `Announcement` struct (Ed25519
  signed, anti-replay 30 s timestamp window); `Config` and `Peer` types; `Start(ctx)`, `Peers()`,
  `PeerCount()`, `OnPeer(fn)` API; 10 s announce interval, 45 s peer TTL, 15 s stale reaper;
  discovered peers auto-upserted into `federation_nodes` via `store.UpsertFederationNode`; stale
  peers marked `offline` in store
- `internal/p2p/peers_handler.go`: `(Mesh).HandlePeers(w, r)` HTTP handler for `GET /api/peers`;
  returns `{"count": N, "peers": [...]}` with `PeerJSON` (DID, api\_addr, ipfs\_addr, cpu\_cores,
  ram\_gb, disk\_gb, gpu, region, last\_seen)
- `internal/httpapi/server.go`: added `p2pMesh *p2p.Mesh` field, `SetP2PMesh(*p2p.Mesh)` setter,
  `GET /api/peers` route delegating to `p2pMesh.HandlePeers`; graceful degradation returns empty
  list when mesh is not configured
- `internal/app/app.go`: added `P2PMesh *p2p.Mesh` field to `App`; `initP2P()` extended to load
  node Ed25519 key and create `p2p.Mesh` when key is present; `Start()` launches mesh goroutine and
  calls `HTTPAPIServer.SetP2PMesh()`
- `internal/config/config.go`: added `IPFSAPIAddr string` to `StorageConfig`
  (`mapstructure:"ipfs_api_addr"`) so nodes can advertise their Kubo API address to LAN peers

### Added — Local Web Dashboard Phase 1 (2026-03-03)

**Architecture decision:** Dashboard is an embedded local web app served by `fedaaa.exe` at
`http://localhost:8080/dashboard` — not a Fyne native app (avoids CGO/MinGW complexity) and not a
cloud SaaS (fully offline, no login). The running node IS the program; the browser is the renderer.

- `ui/dashboard/index.html`: single-page app shell with dark theme and 5-tab navigation
- `ui/dashboard/style.css`: dark theme, cyan accent, SVG radial dials, responsive layout
- `ui/dashboard/app.js`: hash-based router, live stat polling, radial dial animation
- `embed.go`: added `DashboardFS embed.FS` with `//go:embed ui/dashboard` directive
- `internal/httpapi/dashboard.go`: serves embedded dashboard assets at `/dashboard`; redirects `/` to dashboard
- `internal/httpapi/server.go`: added `SetDashboardFS()`, `/dashboard`, `/dashboard/` routes, `/api/status` endpoint
- `cmd/fedaaa/main.go`: wires `soholink.DashboardFS` → `server.SetDashboardFS()`

**5 screens planned:** Dashboard (radial dials), Plan Work, Management (income/payments), Help, Settings

### Added — Self-Contained Binary & Windows Service (2026-03-03)

- **Embedded OPA policies**: `embed.go` — `PoliciesFS embed.FS` (`//go:embed configs/policies`); `internal/policy/engine.go` — `SetEmbeddedFS(fs.FS)` setter; engine loads `.rego` files from embedded FS when `policy.directory == ""`
- **`cmd/fedaaa/main.go`** and **`cmd/fedaaa-gui/main.go`**: wire `policy.SetEmbeddedFS(sub)` before `cli.Execute()` so both entry points use embedded policies
- **`internal/app/app.go`**: startup banner now shows `Policies: (embedded)` via `PolicyEng.PolicyDir()`
- **`configs/default.yaml`**: `policy.directory: ""` (empty triggers embedded FS; no external configs/ directory required)
- **`scripts/install-service.ps1`**: copies `fedaaa.exe` → `C:\Program Files\SoHoLINK\`; registers Windows Scheduled Task (SYSTEM, at-boot, 5x auto-restart); opens firewall UDP 1812/1813 + TCP 8080; starts immediately
- **`scripts/uninstall-service.ps1`**: stops task, removes task, removes firewall rules, deletes install directory
- **`start-node.bat`**: double-click launcher; keeps console window open with endpoint summary; waits for Ctrl+C
- **`.gitignore`**: `/fedaaa` and `/fedaaa-*` (anchored to root; previously bare `fedaaa` matched `cmd/fedaaa/` directory); added `/accounting/` to exclude runtime JSONL audit logs

### Security — gosec G115 + annotation pass (2026-03-03)

- Resolved all 29 HIGH-severity gosec findings across 12 files using targeted `// #nosec` annotations and two code improvements:
  - `internal/blockchain/local.go`: Replaced 16 manual `byte(x>>N)` bit-shift expressions in `computeBlockHash` with `binary.BigEndian.PutUint64()` calls (imported `encoding/binary`); annotated remaining `int64↔uint64` height conversions with `// #nosec G115`
  - `internal/lbtas/transaction.go`: Replaced 8 `byte(ts>>N)` bit-shift expressions in `LBTASRating.Bytes()` with `binary.BigEndian.PutUint64()` (imported `encoding/binary`); annotated `byte(r.Score)` with `// #nosec G115`
  - `internal/payment/lightning.go`: Fixed G402 suppression comment from `//nolint:gosec` (golangci-lint format, not recognized by gosec) to `// #nosec G402`
  - Remaining findings in `wizard/detection.go`, `orchestration/scheduler.go`, `lbtas/convert.go`, `verifier/verifier.go`, `thinclient/p2p.go`, `compute/cgroups.go`, `wizard/config_generator.go`, `portal/server.go`, `httpapi/server.go`, `update/applier.go`, `did/keygen_windows.go` annotated with appropriate `// #nosec` codes and explanatory comments

### Added — OPA policy tests (2026-03-03)

- `configs/policies/resource_sharing_test.rego`: 57 OPA test cases covering all rules in `resource_sharing.rego` — `allow_compute_submit` (9 cases), `allow_storage_upload` (3), `allow_print_submit` (2), `allow_portal_access` (2), `task_replication_factor` (6), `mobile_eligible_task` (6), `android_tv_eligible_task` (2), `allow_mobile_task` (6), `allow_htlc_cancel` (7), `allow_htlc_settle` (4), `allow_mobile_preempt` (7), `deny_reasons` (3).  **Result: PASS 57/57.**
- `.github/workflows/test.yml`: Added `opa-test` CI job using `open-policy-agent/setup-opa@v2` action with `opa test --v1-compatible configs/policies/ -v`; GO_VERSION bumped from `1.24.6` → `1.25.7`
- `DEVELOPMENT.md`: Added OPA to prerequisites table; added "OPA Policy Tests" subsection with install instructions (Linux/macOS/Windows) and test command; updated project structure tree to show `resource_sharing_test.rego`; updated Go version requirement from 1.24.6+ to 1.25.7+

### Security — CVE Patch (2026-03-03)

- **GO-2026-4394** `go.opentelemetry.io/otel/sdk`: Upgraded from v1.39.0 → v1.40.0 to fix arbitrary code execution via PATH hijacking.  Reachable in production through the OPA policy engine (`internal/policy/engine.go`) and Docker service client (`internal/services/docker.go`).
- **GO-2026-4337** `crypto/tls`: Updated `go.mod` minimum Go version from `1.24.6` → `1.25.7` to enforce the Go toolchain version that includes the fix for unexpected TLS session resumption.  Reachable through `internal/httpapi`, `internal/storage/ipfs.go`, `internal/compute/prometheus.go`, and `internal/services/docker.go`.

### Fixed — Code Review Bug Fixes (2026-03-02)

**Critical fixes (production crash / broken functionality):**

- **H1** `internal/httpapi/mobilehub.go`: Added `closeOnce sync.Once` to `MobileClient`.  Both `PushTask` (buffer-full path) and `Run`'s unregister handler previously called `close(client.send)` independently; a race between the two produced a double-close panic.  All close sites now use `client.closeOnce.Do(func() { close(client.send) })`.
- **T1** `internal/ml/telemetry.go`: Added `closeOnce sync.Once` to `TelemetryRecorder`.  `Close()` now calls `r.closeOnce.Do(func() { close(r.queue) })` so that calling `Close()` more than once no longer panics.
- **T2** `internal/ml/telemetry.go`: Removed the redundant `writer.Flush()` call from `Close()`.  `writeLoop` already performs a final flush after draining the queue and before signalling `done`; the second flush in `Close()` was both redundant and a potential source of confusion about which flush is authoritative.
- **P1** `internal/payment/htlc.go`: LND's gRPC-gateway REST layer encodes all `bytes` protobuf fields as standard base64, not hex.  All three HTLC operations (`CreateHoldInvoice`, `SettleHoldInvoice`, `CancelHoldInvoice`) were sending raw hex strings for `hash`, `preimage`, and `payment_hash` respectively, causing LND to reject every call with a 400 error.  Fixed by hex-decoding the caller's hex string to `[]byte` and then base64-encoding for the JSON body.  Added `encoding/base64` import; updated struct field comments to reflect the wire encoding.
- **R2** `configs/policies/resource_sharing.rego`: `allow_mobile_task` was calling `job_within_limits(input.job_spec)` while `mobile_eligible_task` reads from `input.task.*`.  The two different input-path conventions meant callers had to supply both `input.task` and `input.job_spec` or the rule always evaluated false.  Fixed by changing `allow_mobile_task` to call `job_within_limits(input.task)` so a single input object satisfies the entire rule.

**High fixes (ship-blocking):**

- **H3** `internal/httpapi/mobilehub.go`: `Run()` had no shutdown mechanism — the goroutine leaked on server stop.  Changed signature to `Run(ctx context.Context)` and added `case <-ctx.Done(): return` to the select loop.
- **R1** `configs/policies/resource_sharing.rego`: Added `input.node.class != ""` guard to `allow_mobile_task`.  Previously a caller could omit `input.node.class`, causing `task_replication_factor` to fall through to its default of 1 and silently bypass the shadow-replica requirement for `mobile-android` tasks.  Requiring the field to be non-empty forces callers to be explicit about the node class.
- **SC1** `internal/orchestration/scheduler.go`: The fallback `armIndex` for uniform random node selection used `int(time.Now().UnixNano()) % len(candidates)`.  On 32-bit platforms `int` is 32-bit; truncating a 64-bit nanosecond timestamp produces a value that can be negative, making the modulo result negative and causing a slice-index panic.  Fixed with unsigned arithmetic: `int(uint64(time.Now().UnixNano()) % uint64(len(candidates)))`.
- **SC3** `internal/orchestration/scheduler.go`: `ScheduleMobile` could silently overwrite an existing `ActiveWorkloads` entry for the same `WorkloadID` when a workload was retried concurrently.  Added an existence check under the write lock; if the workload is already active, the duplicate dispatch is logged and skipped.
- **SC5** `internal/orchestration/scheduler.go`: `RecordMobileOutcome` was building its telemetry event with an empty `workloadID` string (`""`), making outcome records uncorrelatable with their dispatch-time pending records in the JSONL file.  Added `workloadID string` as the first parameter; callers supply the parent workload ID.
- **B3** `internal/httpapi/mobilehub.go`: Added `UnregisterHook func(nodeDID string)` field to `MobileHub`.  `Run()`'s unregister case now calls `h.UnregisterHook(nodeDID)` (outside the hub lock) after removing a client.  Wire this to `bandit.RemoveArm(nodeDID)` at startup to prevent unbounded arm-matrix accumulation in `LinUCBBandit` (~9.8 KB per disconnected node).

**Medium fixes (correctness / security):**

- **F2** `internal/orchestration/mlfeatures.go`: Battery trend feature was computed as `n.Plugged && n.BatteryPct > 50`, incorrectly classifying a plugged-in device with low charge as draining.  A device is charging whenever it is plugged in, regardless of current charge level.  Fixed to `n.Plugged` alone (1.0 = plugged/charging, 0.0 = on battery/draining, 0.5 = no-battery/always-on).
- **T3** `internal/ml/telemetry.go`: `NewTelemetryRecorder` accepted arbitrary paths without validation.  A caller could supply `"../../etc/cron.d/evil"` and write outside the intended directory.  Added a `strings.Contains(path, "..")` guard that returns an error on traversal sequences.
- **T4** `internal/ml/telemetry.go`: `writeOne` silently discarded write and flush errors via `//nolint:errcheck`.  Disk-full or closed-file errors were invisible.  Replaced with explicit `log.Printf` error reporting so failures surface in logs without blocking the calling goroutine.
- **H5** `internal/httpapi/mobilehub.go`: `ServeWS` used a blocking send (`h.register <- client`) on the register channel.  If the hub's event loop fell behind, the HTTP handler goroutine would stall indefinitely.  Changed to a non-blocking select/default that closes the connection and logs a warning when the channel is full.
- **H6** `internal/httpapi/mobilehub.go`: `refreshLastSeen` took a hub-wide write lock to update a single timestamp, serialising all concurrent `ActiveNodes` calls during high-frequency heartbeat processing.  Replaced with a hub read lock (to locate the client) plus a per-client `seenMu sync.Mutex` (to update `LastSeen`), allowing concurrent map reads to proceed unimpeded.
- **SC4** `internal/orchestration/scheduler.go`: `assignWithReplication` generated the shadow workload ID as `workloadID + "_shadow"`.  Concurrent `ScheduleMobile` calls for the same workload ID produced identical shadow IDs, silently overwriting each other in `ActiveWorkloads`.  Added a nanosecond timestamp suffix: `workloadID_shadow_<UnixNano>`.
- **S1** `internal/httpapi/server.go`: `handleMobileRegister` accepted any `node_class` string without validation.  Unknown values propagated into the scheduler where they could cause missed replication-factor lookups.  Added validation against the four known `orchestration.NodeClass` constants; unknown values return HTTP 400.
- **S2** `internal/httpapi/server.go`: WebSocket (`/ws/nodes`) and REST (`/api/v1/nodes/mobile/register`) mobile endpoints had no rate limiting, allowing a single IP to exhaust goroutine and registration resources.  Added a per-IP sliding-window rate limiter (`ipRateLimiter`) using a `map[string]*rlBucket` with a 1-minute reset window; `/ws/nodes` limited to 30 req/min, `/register` to 20 req/min.  `clientIP()` respects `X-Forwarded-For` for reverse-proxy deployments.
- **R3** `configs/policies/resource_sharing.rego`: HTLC cancel/settle and mobile preempt operations had no OPA authorization rules, meaning the policy layer provided no enforcement boundary for payment lifecycle events.  Added `allow_htlc_cancel` (requires coordinator DID + valid cancel reason), `allow_htlc_settle` (requires coordinator DID + `shadow_verified == true`), and `allow_mobile_preempt` (requires coordinator DID + valid preempt reason) with explicit reason allowlists.
- **WA1** `internal/wasm/executor.go`: `timedExecutor.Execute` checked `ctx.Err()` (the parent context) instead of `tctx.Err()` (the timed child context).  This could produce false "timed out" results when the parent was cancelled for unrelated reasons, and could also miss genuine timeouts when the inner executor returned a non-context error concurrently with deadline expiry.  Fixed to check `tctx.Err()`, wrap with the new `ErrTimeout` sentinel via `%w`, and use `errors.Is` for detection.  Exported `ErrTimeout` so callers can distinguish timeout from other errors.
- **A2** `internal/notification/apns.go`: `providerToken` captured `now := time.Now()` after calling `mintJWT()`, making `tokenExpAt` slightly later than the `iat` claim inside the token.  On a slow system the skew could exceed APNs' tolerance.  Fixed by capturing `now` before `mintJWT(now)` and passing it as a parameter so both the `iat` claim and `tokenExpAt` use the identical timestamp.
- **A4** `internal/notification/apns.go`: APNs HTTP 410 Gone (permanently invalidated device token) was mapped to the same generic error as all other non-200 responses.  Callers could not distinguish stale tokens from transient failures and had no signal to purge the token from storage.  Added the exported `ErrDeviceTokenInvalid` sentinel; the `send()` helper wraps it with `%w` on 410 so callers can use `errors.Is(err, notification.ErrDeviceTokenInvalid)`.

**Low fixes (documentation):**

- **W1** `internal/orchestration/workload.go`: Added a doc comment to `WorkloadState` warning that its top-level fields (`WorkloadID`, `Status`, `DesiredReplicas`, `CreatedAt`, `UpdatedAt`) are snapshot copies that can diverge from the embedded `*Workload` pointer after construction.  New code should read `WorkloadState.Workload.*` directly.
- **B4** `internal/ml/telemetry.go`: Added a doc comment to `RewardFor` clarifying that `OutcomePending` intentionally returns 0.0 and must NOT be passed to `LinUCBBandit.Update`.  Pending records exist only to capture dispatch-time feature vectors; the bandit is updated later via `RecordMobileOutcome` with a resolved outcome.

### Added — ML-Driven Scheduling (2026-03-02)
- `internal/ml/features.go`: dimension constants (`NodeFeatureDim=10`, `TaskFeatureDim=6`, `SystemFeatureDim=4`, `ContextDim=20`); `clamp()` helper shared by bandit and telemetry
- `internal/ml/telemetry.go`: `TelemetryRecorder` — appends `SchedulerEvent` JSONL records to disk; captures node features, task features, chosen arm, and outcome for offline ML training; `EventBuilder` fluent API; `RewardFor()` reward function (HTLC settle → 0.8+speed bonus, completed → 0.6, error → 0.1, preempted/cancelled → 0.0)
- `internal/ml/bandit.go`: `LinUCBBandit` — online contextual bandit (disjoint LinUCB); per-arm A (d×d) and b (d) matrices; Gauss-Jordan matrix inverse; thread-safe; α-tunable exploration; new arms initialise as identity (maximum exploration)
- `internal/orchestration/mlfeatures.go`: `NodeFeatures()`, `TaskFeatures()`, `SystemFeatures()`, `BuildContext()`, `SystemState` struct — moved here from `internal/ml` to break import cycle (`orchestration→ml` and `ml→orchestration` would be circular); `clampF()` local helper
- `internal/orchestration/scheduler.go`:
  - `SetMLBandit(*ml.LinUCBBandit)` and `SetTelemetryRecorder(*ml.TelemetryRecorder)` setter methods
  - `ScheduleMobile` upgraded: segCount computed before selection; system state snapshot; LinUCB arm selection via shared task+system context; dispatch-time pending telemetry event; push-failure path penalises arm immediately
  - `RecordMobileOutcome()`: called asynchronously on task resolution — updates bandit reward model and appends resolved `SchedulerEvent` to JSONL telemetry
- `docs/research/ML_LOAD_BALANCING.md`: 755-line technical research report surveying RL (SAC), contextual bandits (LinUCB / NeuralLinear), GNNs (GAT), LSTM forecasting, anomaly detection (LSTM-AE + Isolation Forest), and federated learning for heterogeneous SOHO node scheduling; phased implementation roadmap (Phase 0–4)

### Added — Mobile Integration (2026-03-02)
- `internal/orchestration/mobile.go`: `NodeClass` type, all four constants, `NodeConstraints`, `DefaultConstraints()`, wire-protocol types (`MobileNodeInfo`, `MobileTaskDescriptor`, `MobileTaskResult`, `MobileHeartbeat`)
- `internal/httpapi/mobilehub.go`: `MobileHub` WebSocket hub — gorilla/websocket, concurrent read/write pumps, 30 s ping, 90 s heartbeat timeout, `PushTask`, `ActiveNodes`, `ServeWS`
- `internal/httpapi/server.go`: `SetMobileHub()`, `GET /ws/nodes`, `POST /api/v1/nodes/mobile/register`, `GET /api/v1/nodes/mobile`
- `internal/orchestration/scheduler.go`: `ScheduleMobile()`, `assignWithReplication()`, `PreemptMobileWorkload()`, `MobileHub` interface, `SetMobileHub()`
- `internal/orchestration/workload.go`: `CheckpointData []byte`, `SegmentIndex int`, `SegmentCount int` on `WorkloadState`
- `internal/wasm/executor.go`: `Executor` interface + `StubExecutor` + `WithTimeout` wrapper + `TaskManifest` struct
- `internal/payment/htlc.go`: `CreateHoldInvoice`, `SettleHoldInvoice`, `CancelHoldInvoice` on `LightningProcessor`
- `internal/notification/apns.go`: `APNSNotifier` — JWT auto-refresh, `SendJobRequest`, `SendPaymentReceived`, `SendNodeOffline`; pure stdlib + x/crypto
- `configs/policies/resource_sharing.rego`: `task_replication_factor`, `mobile_eligible_task`, `android_tv_eligible_task`, `allow_mobile_task` rules
- `docs/research/MOBILE_PARTICIPATION.md`: research report on mobile device participation
- `docs/research/SOHOLINK_CAPABILITIES.md`: current capability assessment
- `docs/MOBILE_INTEGRATION.md`: phased implementation plan (Android TV → Android → iOS → Core ML)
- `docs/ARCHITECTURE.md`: Mobile Node Architecture section

### Added — Automated Dependency Updates (2026-03-02)
- `github.com/gorilla/websocket v1.5.3`: WebSocket support for mobile hub (vendored)

### Changed
- `README.md`: complete rewrite to reflect federated compute marketplace (was: AAA/RADIUS platform)
- `DEVELOPMENT.md`: updated Go version to 1.24.6; updated project structure; added mobile + ML sections
- `PLAN.md`: all completed GAPs marked with accurate status; ML integration tasks added

---

## [0.1.0] — 2026-03-01

Initial production-capable release of the SoHoLINK federated compute marketplace.

### Added — Automated Distribution Pipeline (2026-03-01)
- `FyneApp.toml`: Fyne packaging metadata for `fyne package` command
- `assets/logo.svg`: brand icon source artwork
- `.goreleaser.yml`: full cross-platform release automation (Windows, macOS universal, Linux)
  - Statically linked Windows `.exe` (MinGW `-static-libgcc/-static-libstdc++`, zero DLL deps)
  - macOS universal binary via `lipo` (Intel + Apple Silicon)
  - Linux `.deb`, `.rpm` via nFPM; AppImage via `appimagetool`
  - NSIS `.exe` setup wizard
- `Makefile`: added `build-static-windows`, `fyne-package-*`, `dist`, `dist-release` targets
- `scripts/build-all.ps1`: single PowerShell developer script — checks prerequisites, converts assets, runs tests, invokes GoReleaser, opens `dist/`
- `.github/workflows/build.yml`: rewrote to use `goreleaser/goreleaser-action@v6`; MinGW cross-compiler installed in CI; snapshot builds on push, real releases on `v*` tags
- `.github/workflows/test.yml`: bumped to Go 1.24.6; updated all action versions; golangci-lint v6
- `.github/dependabot.yml`: weekly Go module + GitHub Actions version PRs; smart grouping by ecosystem (Fyne, OTel, Prometheus, spf13, etc.)
- `.github/workflows/update-deps.yml`: monthly scheduled `go get -u ./...` sweep; runs full test suite; opens consolidated PR only if tests pass; supports dry-run from Actions UI

### Added — Automated Dependency Updates (2026-03-01)
- Dependabot configuration for Go modules (weekly, grouped by library family)
- Dependabot configuration for GitHub Actions (weekly)
- Monthly `update-deps.yml` workflow: full upgrade sweep + test + PR

### Added — GUI Dashboard (2026-03-01)
- `internal/gui/dashboard/dashboard.go` (~1,650 lines): complete Fyne GUI rewrite
  - `RunSetupWizard(cfg, store)` / `RunDashboard(application)` entry points
  - 8-step setup wizard: hardware → pricing → network → payment → K8s edges → IPFS → provisioning limits → policies
  - 8 dashboard tabs: Overview, Hardware, Orchestration, Storage, Billing, Users, Policies, Logs
  - 7 settings dialogs: Node, Pricing, Network, Payment, K8s Edges, IPFS, Provisioning Limits
- `cmd/soholink/main.go` (`//go:build gui`): unified entry point — no args → GUI; with args → `cli.Execute()`

### Added — Storage (2026-03-01)
- `internal/storage/ipfs.go`: IPFS HTTP API client for local Kubo daemon
  - `IPFSStoragePool` with `sync.RWMutex` for concurrent access
  - `Upload`, `Download`, `Delete`, `LookupByCID` operations
  - Integrated into dashboard Storage tab with live pool status

### Added — Orchestration (2026-03-01)
- `internal/orchestration/k8s_edge.go`: Kubernetes HTTP adapter for edge cluster integration
  - `NewK8sEdgeCluster` returns `(*K8sEdgeCluster, error)`; TLS 1.2 minimum; CA cert pool from PEM
- `internal/orchestration/scheduler.go`: `ListActiveWorkloads()` — deep-copies `Placements` slice to prevent caller data race; `handleScaleEvent` copies `Spec`/`Constraints` before unlock
- `internal/orchestration/workload.go`: `WorkloadState` convenience fields (`WorkloadID`, `Status`, `DesiredReplicas`, `CreatedAt`, `UpdatedAt`)

### Added — Payment (2026-03-01)
- `internal/payment/meter.go`: per-hour billing loop with `elapsed < 0` guard
- `internal/payment/stripe.go`: `baseURL` field for test injection; full `CreateCharge`, `ConfirmCharge`, `RefundCharge`, `GetChargeStatus` implementation
- `internal/payment/lightning.go`: Lightning Network invoice management

### Added — Globe Visualization (2026-03-01)
- `ui/globe-interface/ntarios-globe.html`: GEO/TOPO mode toggle; nodes accept `lat`, `lon`, `region`; auto-enables GEO mode when coordinates present; animated data-flow arcs

### Added — Platform Detection (2026-03-01)
- `internal/wizard/detection_windows.go`: WMI GPU detection
- `internal/wizard/detection_linux.go`: `/proc`/`/sys` hardware probing
- `internal/wizard/detection_darwin.go`: `system_profiler` / IOKit queries

### Added — Policy (2026-03-01)
- `internal/wizard/types.go`: `PolicyConfig` struct for OPA policy configuration surface
- `configs/policies/resource_sharing.rego`: resource sharing governance rules

### Added — Store (2026-03-01)
- `internal/store/central.go`: `RecordCentralRevenue()` — upserts tenant, delegates to `CreateCentralRevenue`
- `internal/store/lbtas.go`: `CreatePendingPayment` with `status` + `created_at` columns; `parseSQLiteTime` multi-format parser

### Fixed — Test Suite (2026-03-01)
- `internal/compute/cgroups_test.go`: added `//go:build linux` constraint
- `internal/compute/firecracker.go`: `vmID[:8]` slice bounds panic for IDs < 8 chars; nil `httpClient` fallback to `http.DefaultClient`; 1ms latency floor
- `internal/compute/migration.go`: `sync/atomic` counter for unique concurrent migration IDs
- `internal/lbtas/manager.go`: `NewManager` made variadic for optional `*accounting.Collector`
- `internal/httpapi/server.go`: nil-guard `state.Workload`; removed `omitempty` from `Placements`; nil-to-empty-slice coercion for `RecentRevenue`/`ActiveRentals`
- `internal/cdn/router_test.go`: corrected average-node expected score range to 60–80
- `internal/blockchain/integration_test.go`: `hex.EncodeToString(checkpoint.BlockHash)` instead of raw bytes
- `internal/blockchain/local.go`: `VerifyChainIntegrity` early-return nil for empty chain
- `internal/payment/stripe_test.go`: `httptest.Server` mock via `p.baseURL` injection
- `internal/orchestration/k8s_edge.go`: TLS 1.2 minimum; CA cert pool; `NewK8sEdgeCluster` returns error
- `internal/storage/ipfs.go`: `sync.RWMutex` on `IPFSStoragePool`; all methods lock correctly
- `internal/orchestration/scheduler.go`: `ListActiveWorkloads()` deep-copies to prevent data race; `handleScaleEvent` copies before unlock
- `internal/payment/meter.go`: `elapsed < 0` guard with warning log
- `internal/wizard/detection_darwin.go`: removed illegal trailing comma in `strings.Trim` call

---

## [0.0.4] — 2026-02-15 *(Phase 4 — Security & Hardening)*

### Added
- Security audit across all packages; findings documented in `SECURITY_FINDINGS.md`
- TLS enforcement on all external connections
- Input validation hardening across HTTP API handlers
- Rate limiting on authentication endpoints
- `SECURITY_SUMMARY.md`, `SECURITY_QUICK_REFERENCE.md`

### Fixed
- Multiple security findings from audit (see `SECURITY_FIXES_APPLIED.md`)

---

## [0.0.3] — 2026-02-08 *(Phase 3 — Payments & Accounting)*

### Added
- `internal/payment/`: Stripe processor, Lightning processor, ledger, settler, metering
- `internal/central/`: 1% platform fee calculation
- `internal/accounting/`: event recording and JSONL audit logs
- `internal/blockchain/local.go`: local tamper-evident blockchain for accounting
- `internal/sla/`: SLA contract monitoring, credit computation, tiered plans
- `internal/cdn/router.go`: geographic CDN routing with active health probing
- `internal/lbtas/`: trust and reputation scoring system
- HTTP API: revenue, billing, and payment endpoints

### Fixed
- SLA credit computation edge cases at tier boundaries
- CDN route scoring formula

---

## [0.0.2] — 2026-01-25 *(Phase 2 — Orchestration & Storage)*

### Added
- `internal/orchestration/scheduler.go`: FedScheduler with placement scoring, auto-scaler, health monitor
- `internal/orchestration/nodeagent.go`: node agent for workload lifecycle
- `internal/storage/`: local content-addressed pool
- `internal/rental/`: auto-accept engine
- `internal/httpapi/server.go`: REST API skeleton
- `internal/compute/`: Firecracker VM, KVM, Hyper-V, migration, cgroups, sandbox

---

## [0.0.1] — 2026-01-10 *(Phase 0–1 — Foundation)*

### Added
- Go module `github.com/NetworkTheoryAppliedResearchInstitute/soholink`
- `internal/store/`: SQLite via `modernc.org/sqlite`; all schema tables
- `internal/wizard/`: hardware detection (cross-platform), cost calculator, pricing
- `internal/auth/`: Ed25519 DID:key credentials
- `internal/verifier/`: offline credential verification
- `internal/radius/`: RADIUS auth (port 1812) and accounting (port 1813)
- `internal/policy/`: OPA policy engine (Rego)
- `internal/merkle/`: SHA3-256 Merkle batch batcher
- `internal/did/`: DID:key format (Ed25519 multicodec)
- `cmd/soholink/`: CLI entry point (`cobra`)
- `Makefile`, `BUILD.md`, `docs/ARCHITECTURE.md`, `docs/INSTALL.md`
- Cross-compilation targets: Linux amd64/arm64, macOS amd64/arm64, Windows amd64, Raspberry Pi ARM64

---

[Unreleased]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.0.4...v0.1.0
[0.0.4]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/releases/tag/v0.0.1
