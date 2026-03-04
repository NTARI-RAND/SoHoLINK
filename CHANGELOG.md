# Changelog

All notable changes to SoHoLINK are documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

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

[Unreleased]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.0.4...v0.1.0
[0.0.4]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/NetworkTheoryAppliedResearchInstitute/soholink/releases/tag/v0.0.1
