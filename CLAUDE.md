# SoHoLINK v2 — Claude Code Context

## What This Project Is
SoHoLINK is a participatory distributed compute platform. Participants contribute
idle personal devices — SOHO servers, phones, Smart TVs, laptops — as compute nodes
and earn fiat dollars. Other participants buy compute, storage, and CDN capacity on
demand. NTARI operates the coordination layer: matching, scheduling, metering, and
dispute arbitration.

No tokens, no wallets. Pure fiat via Stripe Connect.
Participants own and control their hardware. NTARI never touches the hardware.

This is a ground-up v2 rebuild. The old build is on the `legacy-v1` branch.
Do not reference it. Do not continue or fix it.

## Workflow Discipline
SoHoLINK uses a three-layer workflow. Claude Code is layer 2 — the execution layer.

1. **Claude Chat (design layer):** Produces the specification. Audits files, reads
   SDK source, identifies all changes needed, proposes the complete implementation
   plan. Does not write code.
2. **Claude Code (execution layer):** Receives precise, fully-specified instructions.
   Writes only what is specified. Does not deviate, does not add unrequested cleanup,
   does not take autonomous action between instructions.
3. **Human (review layer):** Reviews every diff before commit. Approves or rejects.

**Never act between instructions.** Autonomous cleanup, reformatting, memory writes,
or CLAUDE.md edits that were not requested are violations of this discipline.
When in doubt, stop and report — do not act.

Commit messages are written by the human verbatim. Do not append Co-Authored-By,
Signed-off-by, or similar trailers.

## Organization
- **Project:** SoHoLINK
- **Organization:** NTARI (Network Theory Applied Research Institute)
- **Module:** `github.com/NetworkTheoryAppliedResearchInstitute/soholink`
- **Domain:** soholink.org
- **Trust domain:** spiffe://soholink.org
- **Working branch:** master

## Technology Stack
| Layer | Technology |
|---|---|
| Language | Go 1.24+ — all services and agent |
| Frontend | Server-rendered HTML/CSS via Go `html/template` — no JS framework, no build step |
| Database | PostgreSQL 16 + TimescaleDB, `pgx/v5` driver (no ORM) |
| Object storage | MinIO (S3-compatible) |
| Overlay network | WireGuard |
| Ingress | NGINX — TLS termination in front of portal |
| Identity | SPIFFE/SPIRE — mTLS, short-lived X.509 SVIDs (1hr TTL) |
| Payments | Stripe Connect (destination charges, split payouts) via `stripe-go/v82` |
| Monitoring | Prometheus + Grafana |
| Config management | Ansible |
| Container runtime | Docker + Portainer CE |
| Auto-update | Watchtower |
| CI/CD | GitHub Actions |

## Architectural Philosophy — Janus Facing Application
SoHoLINK is a Janus Facing Application (JFA) as defined by NTARI document P3-011.
Reference: https://www.ntari.org/post/janusfacingapplications

Key principles implemented here:
- Single participant identity — one account, simultaneous contributor/buyer roles
- No information asymmetry — pricing, metering, and earnings visible to all participants
- Governance layer architecturally separated — admin portal runs on a separate local-only
  port (8090), never exposed publicly, not hidden behind role flags on the public portal
- AGPL-3 licensed — permanent commons, prevents enclosure

The `participants` table (migration 011) replaces the separate `providers`/`consumers`
tables as the concrete implementation of unified identity. There are no "providers" or
"consumers" in the codebase — only participants who may contribute nodes, submit jobs,
or both.

## Repository Structure (current state)
```
cmd/
  agent/          ← Node agent daemon: main.go, service_windows.go, install_windows.go
  orchestrator/   ← Control plane entry point (stub)
  portal/         ← Web portal entry point (stub)
internal/
  agent/          ← Hardware detection, resource profiles, heartbeat,
                     executor (with allowlist enforcement, hardened HostConfig,
                     per-job network, tmpfs scratch, CUPS device mount on Unix,
                     contributor opt-out gate),
                     allowlist, optout, printers (cross-platform via build tags),
                     telemetry, config (NodeConfig, ClaimNode, LoadConfig, SaveConfig)
  types/          ← Cross-cutting vocabulary (MarketplaceWorkloadType enum,
                     Validate/Parse helpers); imported by portal and orchestrator
  api/            ← Control plane HTTP API (node registration, claim, heartbeat, telemetry)
  identity/       ← SPIRE integration, TLSClientConfig, TLSServerConfig, RequireSPIFFE middleware
  orchestrator/   ← NodeRegistry, job submission, node matching, job token issuance
  payment/        ← Stripe Connect: client, onboarding, charge, payout, webhook
  portal/         ← Portal HTTP server, session middleware, all handler implementations
  scheduler/      ← Scoring-based job placement (classScore + freshnessScore + capacityScore)
  store/          ← PostgreSQL pool, golang-migrate runner, migrations 001–013
  network/        ← WireGuard bootstrapper (stub)
web/
  templates/      ← layout.html, index.html, login.html, dashboard.html,
                     provider_onboarding.html, provider_provision.html,
                     consumer_marketplace.html, consumer_job_status.html,
                     dispute_queue.html
  static/css/     ← portal.css (complete design system)
installer/
  windows/        ← WiX v4 MSI: SoHoLINK.wxs, build.ps1, LICENSE.rtf, agpl-3.0.txt
scripts/
  allowlist-genkey/  ← Operator tool: generate Ed25519 signing keypair (one-time)
  allowlist-sign/    ← Operator tool: sign allowlist JSON with private key
docs/
  operations/     ← Operator runbooks (allowlist-signing.md)
examples/         ← Templates (allowlist.example.json + README)
test/integration/ ← Phase 1 end-to-end integration test (build tag: integration)
```

## Database Migrations (internal/store/migrations/)
| # | File | What it adds |
|---|---|---|
| 001 | `001_initial_schema` | providers, consumers, nodes, jobs, resource_profiles, node_class/job_status enums |
| 002 | `002_add_auth_columns` | `password_hash`, `is_staff` on providers; `password_hash` on consumers |
| 003 | `003_provider_onboarding` | `onboarding_complete`, `isp_tier`, `disclosure_accepted_at` on providers |
| 004 | `004_resource_pricing` | `resource_type` enum, `resource_pricing` table, seeded with 5 initial rates |
| 005 | `005_resource_profile_pricing` | `price_multiplier NUMERIC(4,3)` on resource_profiles |
| 006 | `006_disputes` | `dispute_status` enum, `disputes` table with evidence_log JSONB, arbiter fields |
| 007 | `007_uptime_tracking` | `node_heartbeat_events` table, `uptime_pct` column on nodes |
| 008 | `008_job_metering` | `job_metering` table, `started_at`/`completed_at` on jobs |
| 009 | `009_payout_released_at` | `payout_released_at TIMESTAMPTZ` on job_metering |
| 010 | `010_unique_node_hostname` | `uq_nodes_provider_hostname` unique index on nodes |
| 011 | `011_participants` | Unified `participants` table replacing `providers`+`consumers`; `participant_id` FKs on nodes/jobs/disputes |
| 012 | `012_container_image` | `container_image TEXT` nullable column on `jobs` |
| 013 | `013_node_registration_tokens` | `node_registration_tokens` table: single-use installer tokens tied to a participant |
| 014 | `014_opt_out_and_printers` | `opt_out_compute`, `opt_out_storage`, `opt_out_printing`, `opt_out_version`, `opt_out_updated_at` on `nodes`; `node_printers` table (composite PK `(node_id, printer_id)`, FK → `nodes(id)` ON DELETE CASCADE, `enabled` DEFAULT FALSE, `detected_at`); partial index `idx_node_printers_enabled WHERE enabled = TRUE` |

To apply all migrations: run the Phase 1 integration test with DATABASE_URL set:
```
DATABASE_URL="postgres://postgres:changeme@localhost:5432/postgres?sslmode=disable" \
  go test -tags integration -v -run TestPhase1EndToEnd ./test/integration/
```
golang-migrate is idempotent — safe to run repeatedly.

## Test Coverage (current state — all green in CI)
| Package | File | Tests |
|---|---|---|
| `internal/agent` | `*_test.go` (8 files) | 90 tests: allowlist verification, executor hardening (allowlist + root rejection, HostConfig baseline, tmpfs presence, CUPS device mount, opt-out gate ordering and fail-closed), hardware detection, opt-out store concurrency, printer detection (Unix + Windows), profile scheduling, telemetry signing |
| `internal/types` | `workload_test.go` | 3 tests: IsValid coverage, ParseMarketplaceWorkloadType round-trip and unknown-rejection |
| `internal/portal` | `middleware_test.go` | Ed25519 token create/verify, tampered sig, expiry, RequireAuth redirect |
| `internal/portal` | `handlers_test.go` | 19 handler tests: login, register, job submission, dispute resolution |
| `internal/store` | `payouts_test.go` | EligiblePayouts query with seeded DB |
| `internal/store` | `metering_test.go` | 4 metering integration tests |
| `internal/store` | `uptime_test.go` | TestRunUptimeScorer — seeds 19152 heartbeats, verifies uptime_pct update |
| `internal/api` | `*_test.go` | 7 API handler tests: node registration, heartbeat, job completion |
| `internal/orchestrator` | `orchestrator_test.go` | 9 registry tests: geo match, GPU filter, offline exclusion, eviction, stale eviction |
| `internal/orchestrator` | `workload_test.go` | 5 tests: marketplace→agent mapping coverage, MustValidateWorkloadMapping pass and panic-on-missing |
| `internal/orchestrator` | `orchestrator_test.go` (Validate) | TestSubmitJobRequest_Validate — table-driven, 4 cases (valid, empty consumer, empty workload type, unknown workload type) |
| `internal/orchestrator` | `orchestrator_test.go` (Defense 3) | 2 tests: TestSubmitJob_RejectsImageNotInAllowlist, TestSubmitJob_RejectsMappingInconsistency. Happy path covered by integration test |
| `internal/api` | `allowlist_test.go` | 2 tests: TestHandleGetAllowlist_ServesFile, TestHandleGetAllowlist_ReturnsNotFoundWhenMissing |
| `internal/agent` | `allowlist_test.go` (Sign) | 2 additional tests: TestAllowlist_SignVerifyRoundTrip, TestAllowlist_SignRejectsBadKey |
| `internal/scheduler` | `scheduler_test.go` | 8 scheduler tests: classScore, freshnessScore, ordering, tier size, insufficient candidates |
| `test/integration` | `phase1_test.go` | End-to-end: migrations, SubmitJob, token round-trip, Stripe (skipped without key) |

## Local Dev Services
All running in Docker on localhost:

| Service | Port | Credentials |
|---|---|---|
| PostgreSQL 16 + TimescaleDB | 5432 | user: postgres / pw: changeme |
| MinIO S3 API | 9000 | user: admin / pw: changeme |
| MinIO Console | 9001 | user: admin / pw: changeme |
| SPIRE Server | 8081 | trust domain: soholink.org |

Stripe keys are environment variables — never hardcode them.
Use `STRIPE_SECRET_KEY` and `STRIPE_PUBLISHABLE_KEY`.

For agent MSI builds, `ALLOWLIST_PUBLIC_KEY` (base64-encoded Ed25519 public
key, produced by `scripts/allowlist-genkey`) is read by `installer/windows/build.ps1`.
Set `RELEASE=1` for production builds — the build will hard-fail if the public
key is missing. Dev builds without `RELEASE=1` warn and continue, producing a
binary that will fail at first allowlist fetch.

Operator runbooks for allowlist keypair bootstrap, signing, deployment, key
rotation, and loss recovery: `docs/operations/allowlist-signing.md`.
Unsigned template: `examples/allowlist.example.json`.

## Known TODOs
These are acknowledged gaps, not bugs — do not silently fix them without discussion:

1. **Orchestrator test binary blocked on NTARIHQ**: Windows Application Control
   (AppLocker/WDAC) blocks `internal/orchestrator` test binary execution on the
   dev machine. Tests pass in CI (Linux). Not a code issue — do not attempt to fix.

2. **`/nodes/claim` — node class always `C`**: The claim endpoint inserts all
   installer-claimed nodes as Class C. Class should be derived from hardware profile
   or set by the participant during onboarding. Deferred until hardware classification
   logic is defined.

3. **Telemetry HMAC verification not server-side**: `token_secret` is returned by
   `/nodes/claim` and stored in `agent.conf`. The control plane does not yet verify
   HMAC signatures on telemetry payloads — it only checks SPIFFE identity. Add
   server-side verification when the dispute evidence layer is hardened.

4. **WiX installer bitmap assets**: `banner.bmp` and `dialog.bmp` are generated by
   `build.ps1` as solid-color placeholders. Replace with branded artwork before
   public release.

5. **Orchestrator `/jobs/<id>/complete` ignores JSON body (carry-forward, resolve
   in B5)**: Agent sends `{"tmpfs_exhausted": bool}` but the handler accepts no body.

6. **Job completion fires on any non-error return regardless of ExitCode (resolve in
   B5)**: Metering triggers even on exit-nonzero. Pre-existing bug discovered during
   B2 audit.

7. **CUPS bind-mount path untested in CI**: `executor_devices_unix.go` is only
   exercised by inspection on the Windows dev box. `TestBuildHostConfig_CUPSDeviceAccess`
   skips on Windows. Needs a Linux GitHub Actions matrix entry or first run on
   Shenandoah pilot host.

8. **`AllowedDestinations` egress filtering deferred (carry-forward)**:
   `EgressOutbound` allows arbitrary outbound. `AllowedDestinations` field is fetched
   from the allowlist but not consumed in the executor.

9. **`DeviceUSBPrinter` not yet wired (carry-forward, resolve in B4)**:
   `deviceMountsFor` recognizes the constant but produces no mapping.
   `PrinterInfo.ConnectionPath` needs threading through `ContainerSpec`.

10. **`FindMatch` does not filter on `WorkloadType` (B3 carry-forward)**: Documented
    inline on `MatchRequest.WorkloadType`. Jobs may be dispatched to nodes whose
    contributors have opted out of that workload type — the agent rejects them (B2
    gate is the security boundary), but the round-trip is wasted effort. Fix
    requires orchestrator visibility into agent opt-out state, which isn't plumbed
    today (heartbeat is fire-and-forget). Likely B6 or later.

11. **Orchestrator unit tests don't hit a real database (B3 carry-forward)**: B3
    fixed the dead `"inference"` / `"batch"` test fixture values. The underlying
    gap remains: `SubmitJob`'s DB cast (`$4::workload_type`) is never exercised in
    unit tests because they don't hit a real DB. Test-rigor concern, not a
    B-phase blocker.

12. **Orchestrator unhealthy in production — SPIRE Workload API unreachable**:
    **RESOLVED via Option A, 2026-05-01** (commits `303744b`, `6a6f3e3`,
    `0a28f88`). `identity.NewSource` now runs under a 5-second bounded context;
    on failure the orchestrator continues in degraded mode: plain HTTP,
    SPIFFE-protected routes return 503 with JSON body, `/health` always returns
    200 with `"identity":"unavailable"`. Production container is healthy.
    Option B (full SPIRE wiring) remains the correct long-term path — see TODO 13.

13. **No SPIRE agent in Compose stack**: `deploy/spire/server.conf` configures
    the SPIRE server only — `NodeAttestor "join_token"`, no `WorkloadAttestor`.
    The Compose stack has no `spire-agent` service. Full SPIRE wiring requires:
    adding a `spire-agent` service with Docker workload attestor
    (`unix_workload_attestor`), registering workload entries for each container
    (orchestrator, portal), and providing agent config and join token. Until this
    is done, no container can obtain a SVID from the local socket.
    **Note (Option B healthcheck reconciliation):** When Option B is implemented,
    the Compose `healthcheck` for the orchestrator (currently probing plain HTTP
    `/health`) must be updated — once `Start()` switches to `ListenAndServeTLS`,
    the probe needs mTLS or a sidecar. The `/health` route was intentionally placed
    on the plain outer mux so it stays reachable in degraded mode; verify this
    placement still works after full SPIRE wiring.

14. **B7 commit 4b deferred until worker images exist**: B7 shipped the keypair
    tooling, the `/allowlist` endpoint, the build-time public-key injection,
    Defense 3, and the operations runbook (commits `4514c10`, `1481cf6`,
    `dd8ffd1`, `17a63f8`, `9710f32`). The remaining piece — generating the
    production keypair, signing v1 with real Shenandoah worker image digests,
    and placing the signed file at `/etc/soholink/allowlist.json` on the
    orchestrator host — is blocked on the worker images existing. References
    to `soholink/compute-worker` and `soholink/storage-worker` exist only in
    test fixtures today. When the worker images are built and published,
    follow `docs/operations/allowlist-signing.md` to complete this step.

15. **`/health` endpoint moved off SPIFFE auth (B7 commit 2)**: As part of
    restructuring the orchestrator mux to expose `/allowlist` plain-HTTP, the
    `/health` route was also moved to the plain top-level mux. External monitors
    and load balancers can now reach `/health` without an SVID. Documented as
    deliberate, not regression. No action item — listed for visibility.

16. **NTARIHQ Application Control — elevated subprocess required for boot-path
    management**: Windows Application Control (AppLocker/WDAC) on NTARIHQ blocks
    non-elevated PowerShell from managing scheduled tasks and stopping processes in
    Session 0. `Disable-ScheduledTask` and `Stop-Process` fail with access denied.
    Workaround: `Start-Process powershell -Verb RunAs -ArgumentList "-NoProfile
    -Command ""<cmd>""" -Wait`. Any future automation that manages boot-path
    services on NTARIHQ must either run elevated or use a pre-authorized scheduled
    task. Host policy constraint — not a code issue, do not attempt to bypass.

17. **soholink.org 502 — Cloudflare Zero Trust remote tunnel config**: Cloudflare
    Zero Trust dashboard has a remote-managed tunnel config that overrides the local
    `config.yml`, routing `soholink.org → https://portal:8080` (wrong scheme —
    portal is plain HTTP). Fix: Cloudflare dashboard → Networks → Tunnels →
    soholink-prod → Configure → Public Hostname; either delete the remote rules to
    fall back to local `config.yml`, or correct the service URL to
    `http://portal:8080` and re-add `api.soholink.org → http://orchestrator:8082`.
    Not a code issue — requires Cloudflare dashboard access. Separate track from B6.

## Critical API Notes
These have caused bugs before — read before touching related code:

**SPIFFE:**
- Use `spiffeid.RequireFromString(...)` — NOT `RequireIDFromString` (doesn't exist).

**Stripe (`stripe-go/v82`):**
- Use V1 API only. V2 account creation params (`stripe.V2...`) do not exist in v82.
- `CreateConnectedAccount(ctx, displayName, email string)` — args in that order.
- `CreateOnboardingLink(ctx, accountID, refreshURL, returnURL string)`.
- `CheckOnboardingStatus` returns `OnboardingStatus{TransfersActive, RequirementsPending}`.
- Do not use Stripe Products. Use dynamic destination charges from metered usage.

**Docker SDK (`docker/docker v28+incompatible`):**
- Storage quotas: `container.HostConfig.StorageOpt["size"]` (a `map[string]string`).
- `dockerclient.IsErrNotFound(err)` to check image presence before pulling.
- Deferred `ContainerRemove` must use `context.Background()`.
- `ImageInspect` is variadic: `(ctx context.Context, ref string, opts ...ImageInspectOption)`.
  Fakes and interface definitions must include the variadic parameter — omitting it
  causes a compile error even when no options are passed.
- `Devices []DeviceMapping` is on `container.Resources`, not directly on
  `container.HostConfig`. Go struct literals do not promote embedded fields — set it
  inside `Resources: container.Resources{..., Devices: ...}`.
- `mount.Mount` with `TypeTmpfs`: `Source` must be empty string. Options go in
  `TmpfsOptions{SizeBytes, Mode}`. Setting `Source` on a tmpfs mount causes a
  Docker daemon error.

**golang-migrate:**
- Use `stdlib.OpenDBFromPool(pool)` to bridge pgx pool to `database/sql`.
- Call `defer m.Close()` after `migrate.NewWithDatabaseInstance` to avoid deadlock.

**Go `html/template`:**
- All pages define `{{define "content"}}` — portal uses per-request
  `template.ParseFiles(layoutPath, pagePath)` to avoid last-parsed-wins collision.
  Do not revert to a shared parsed set.
- `template.ParseFiles` names templates by base filename only — keep base names unique.

**Workload type vocabulary (post-B3):**
- Two enums, by design — they evolve independently:
  - `types.MarketplaceWorkloadType` (in `internal/types/workload.go`) is the
    customer-facing enum. Five values: `app_hosting`, `batch_compute`, `ai_inference`,
    `object_storage`, `cdn_edge`. Constants prefixed `Marketplace*`. Values match the
    PostgreSQL `workload_type` enum from migration 001 exactly.
  - `agent.WorkloadType` (in `internal/agent/`) is the hardware-affinity / opt-out
    enum: `compute`, `storage`, `print_traditional`, `print_3d`.
- Translation lives in `internal/orchestrator/workload.go` as the
  `marketplaceToAgent` map. Multiple marketplace values may map to the same agent
  value (`app_hosting`, `batch_compute`, `ai_inference`, `cdn_edge` → `compute`).
- `MustValidateWorkloadMapping()` panics if any marketplace value lacks a mapping
  entry. Wired as the **very first action** in `cmd/orchestrator/main.go` —
  before env validation, before DB connection. Mapping staleness is a noisy boot
  failure, never a silent dispatch-time failure.
- **Opt-out enforcement reads workload type from `AllowlistEntry.Type`, not from
  the wire.** The orchestrator is not a security boundary for opt-out. A
  misbehaving or compromised orchestrator that mislabels a job's workload type
  cannot route past a contributor's opt-out — the agent ignores the wire claim
  entirely. Mirror this on any future similar gate.
- **Print is deliberately out of the marketplace enum.** Print's submission flow
  is consent-per-job, not anonymous-matching. Decision deferred to whichever phase
  first needs to submit print jobs through the marketplace API (likely B4 or B6).
- **Validation lives on a method, not inline.** `SubmitJobRequest.Validate()`
  exists so tests can exercise validation logic without constructing an
  `Orchestrator`. Tests that require "this constructor must never be hardened"
  are wrong tests, not right constructors.

**Build-time ldflags injection (post-B7 commit 3):**
- `internal/agent.AllowlistPublicKey` is a package-level `var string` injected
  at build time via `-ldflags "-X ..."`. The full ldflags target is
  `github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent.AllowlistPublicKey`
  (full module path, not relative).
- `installer/windows/build.ps1` reads `$env:ALLOWLIST_PUBLIC_KEY` and injects
  it. Behavior on empty: hard-fail when `$env:RELEASE -eq "1"`, warn-and-continue
  otherwise. Production MSI builds must set `RELEASE=1`.
- `.github/workflows/ci.yml` reads `${{ secrets.ALLOWLIST_PUBLIC_KEY }}` and
  injects it into the agent build. Empty key produces a binary that fails at
  first allowlist fetch with `ErrAllowlistNoKey` — fine for CI verification,
  not fine for distributable builds.
- An empty `AllowlistPublicKey` is a runtime fail-closed condition (Verify
  returns `ErrAllowlistNoKey`), not a build error. The fail-closed behavior is
  why warn-and-continue is acceptable for dev builds.

**Orchestrator-side allowlist consumption (post-B7 commit 5 / Defense 3):**
- Both `cmd/orchestrator/main.go` and `cmd/portal/main.go` read `ALLOWLIST_PATH`
  (default `/etc/soholink/allowlist.json`) and pass it to `orchestrator.New`.
  Both binaries construct Orchestrators that handle SubmitJob, so both need
  the path.
- `internal/orchestrator/orchestrator.go` defines `loadAllowlist(path)` which
  reads + parses on every call. **The orchestrator does not verify the
  Ed25519 signature** — by design (Defense 3 design call A1). The agent is
  the security boundary for workload identity; the orchestrator's check is
  consistency only. This means the orchestrator binary does not need
  `AllowlistPublicKey` baked in.
- `SubmitJob` calls `loadAllowlist` after `req.Validate()` and before
  `FindMatch`. Three rejection conditions: (a) file missing/unparseable,
  (b) image not in allowlist, (c) `marketplaceToAgent[req.WorkloadType] !=
  allowlistEntry.Type`. All three return errors with `"submit job: ..."`
  prefix and a descriptive sub-message.
- Fail-closed: missing or malformed allowlist file rejects all submits.
  Matches the agent's "no allowlist = no work" posture.
- Per-submit file read is intentional. Allowlist updates are rare; an
  `os.ReadFile` is cheap relative to the existing DB calls in `SubmitJob`.
  If submit performance ever becomes a bottleneck, swap to startup-load
  with reload-on-SIGHUP — interface stays the same.

## Coding Conventions
- All errors handled explicitly — no blank `_` discards (except `//nolint:errcheck` on fire-and-forget cleanups)
- All inter-service calls use mTLS via SPIRE SVIDs
- No secrets in source or committed config — use env vars
- Database queries use `pgx/v5` directly — no ORM
- HTML templates use Go `html/template` — never `text/template`
- All monetary amounts stored and calculated in cents (`int64`)
- Telemetry payloads HMAC-SHA256 signed
- Job tokens use the same HMAC-SHA256 pattern
- Session tokens signed with Ed25519 — `SESSION_PRIVATE_KEY` env var (64-byte key, 128 hex chars)
- JSON struct tags always snake_case
- `RequireAuth(sm, handler)` — auth wraps all protected routes; staff-only routes additionally check `is_staff` from DB
- `context.Background()` in deferred cleanups that must outlive the request context

## Key Design Decisions
**Identity:** Single `participants` table — every account can contribute nodes, submit
jobs, or both. No role column. Contributor capability is inferred from whether the
participant has nodes registered. Staff access is gated by `is_staff BOOLEAN`.

**Payment:** Stripe Connect destination charges. NTARI collects from participants
buying compute, pays out to participants contributing nodes. 60–70% to contributors,
30–40% platform fee. 24-hour payout hold for dispute window.

**Frontend:** Server-rendered HTML via Go `html/template`. No React, no Vue, no
Node.js, no npm, no build step. Vanilla JS only where strictly necessary.
Must work on a 2019 Android phone on 3G. Must work in Smart TV browsers.

**Hardware detection:** Agent-side only via gopsutil. Never browser-side.

**Resource profiles:** Default profile + scheduled overrides per node.
Per-resource toggles: CPU on/off, GPU %, RAM %, storage GB, bandwidth Mbps,
price_multiplier (0.5–2.0×). cgroup v2 enforces caps on launched containers.

**Identity/mTLS:** SPIFFE/SPIRE, short-lived X.509 SVIDs (1hr TTL). Portal sits
behind NGINX (plain HTTP to NGINX, mTLS between internal services).

**Geo scheduling:** Every node geo-tagged at registration. Jobs may specify
country/region constraints. Scheduler refuses to violate hard residency.

**Disputes:** NTARI arbitrates via the Dispute Terminal (`/dispute/queue` —
staff-only, enforced by `is_staff` DB check). Signed telemetry is primary evidence.
Default 50/50 split if unresolved after 5 business days.

**Node self-registration (installer flow):** Participants generate a single-use token
on their dashboard (`POST /node/token`). The Windows MSI wizard collects this token,
the agent binary calls `POST /nodes/claim` on first run (SPIFFE mTLS, validated against
`node_registration_tokens`), receives its `node_id` + `token_secret`, and writes
`%PROGRAMDATA%\SoHoLINK\agent.conf`. Subsequent starts load the conf and skip the
claim step. `/nodes/register` is retained for programmatic use (seeding, CI) but
requires `X-Register-Secret` header matching `CONTROL_PLANE_REGISTER_SECRET` env var.

**Node classes:**
- Class A: SOHO servers — full Docker runtime, all workload types, ≥95% uptime
- Class B: Mobile GPU — Android/iOS, idle-only, batch + AI inference, ≥85% uptime
- Class C: Smart TV — Tizen/webOS/AndroidTV, CDN edge cache, ≥70% uptime
- Class D: NAS/storage devices — object storage, CDN, ≥80% uptime

## Production Deployment
soholink.org live on NTARIHQ via Cloudflare Tunnel (`soholink-prod bb7b7f0d`).
Docker Compose stack: portal + NGINX + cloudflared + orchestrator. Orchestrator
added in `6f8d9a2` — currently **unhealthy** (crash-looping): `identity.NewSource`
blocks at startup because no SPIRE agent is present in the Compose stack — see TODO 12.
- **`docker-compose.yml`** — portal + NGINX + cloudflared + orchestrator services
- **`Dockerfile.portal`** — multi-stage Go build; final image copies binary + `web/`
- **`Dockerfile.orchestrator`** — multi-stage Go build; final image copies orchestrator binary
- **`nginx.conf`** — reverse proxy to `portal:8080` for `soholink.org`
- **`.env`** — `DATABASE_URL`, `SESSION_PRIVATE_KEY`, `ORCHESTRATOR_TOKEN_SECRET`; gitignored
- **Cloudflare Tunnel** — `soholink-prod` (`bb7b7f0d-0d50-4d58-858b-abc52f1d7cd4`)
- **DNS** — CNAME `soholink.org` → tunnel (proxied); CNAME `api.soholink.org` → tunnel (proxied), live

## First Live Pilot
**Shenandoah Condominiums, 1 Dupont Way, Louisville KY 40207** — dense residential
building adjacent to NTARI HQ. Target: onboard residents as contributors (personal
laptops, smart TVs, idle phones) and validate the full node registration → heartbeat
→ job completion → payout flow against real residential NAT and ISP conditions.

Pre-pilot checklist:
- Validate agent installer on Windows laptops and Android devices
- Test node registration and heartbeat stability through residential NAT
- Verify `residential` ISP tier classification and ACH payout flow end-to-end
- Validate uptime scorer thresholds (A ≥95%, B ≥85%, C ≥70%) against real hardware
- Document a non-technical onboarding flow for Shenandoah residents

## Executor Security Baseline (post-B1)
Every container launched by the agent enforces this baseline — do not relax without
a signed-off design change:

1. **Allowlist lookup first** — rejects tag-only refs and unknown digests before any
   Docker call. `Allowlist.Lookup` is the gate; if it returns an error, `Run` returns
   immediately.
2. **Root-user rejection** — image inspect reads `Config.User`; empty, "0", "0:0",
   "root", and "root:<group>" all count as root. A nil `Config` is treated as uid 0.
3. **Per-job Docker network** — `EgressNone` → internal bridge (no host routing);
   `EgressOutbound` → standard bridge. Network created before container, removed
   after container is gone (LIFO defer order enforces this).
4. **Hardened HostConfig** — `ReadonlyRootfs: true`, `CapDrop: ["ALL"]`,
   `SecurityOpt: ["no-new-privileges:true"]`. Default seccomp profile preserved
   automatically by Docker (verified: `Seccomp_filters: 2` with no-new-privileges,
   vs. 1 for `seccomp=unconfined`).
5. **tmpfs scratch** — `/tmp` mounted tmpfs, capped at 256 MiB (`tmpfsScratchSize`),
   mode `01777`. `Source` field is empty — required for `TypeTmpfs` mounts.
6. **Device mounts** — `deviceMountsFor(entry.DeviceAccess)` dispatches per platform:
   Unix wires CUPS socket bind-mount; Windows stub returns empty set.
7. **ENOSPC detection** — on non-zero exit, last 100 lines of stderr scanned for
   "no space left on device" / "enospc". Result forwarded as `TmpfsExhausted` in
   `ExecutionResult` and in the JSON completion body to the control plane.

## Build Phases

### Sub-phase A — Foundation (complete)
Portal, database migrations 001–013, SPIFFE/SPIRE identity, Stripe Connect onboarding,
job submission, scheduler, node registration (claim + token flow), Windows MSI installer,
Phase 1 end-to-end integration test.

### Sub-phase B1 — Executor Hardening (complete, 2026-04-26)
Commits `43db91d` and `665ef44` on master. Allowlist enforcement, root-user rejection,
per-job Docker network, hardened HostConfig, tmpfs scratch, CUPS bind-mount on Unix,
ENOSPC detection. Carry-forwards → see Known TODOs 5, 6, 9, 10.

### Sub-phase B2 — Job-Poll Opt-Out Wiring (complete, 2026-04-26)
Commit `85b8498` on master. `Executor.optout` is a fail-closed constructor
dependency (`NewExecutor` returns an error on nil store). Opt-out gate sits
inside `Executor.Run` immediately after `Allowlist.Lookup` and before
`ImageInspect` — single enforcement point, mirrors B1's pattern. New sentinel
`ErrWorkloadOptedOut`. Workload type read from trusted `AllowlistEntry.Type`,
never from wire (see Critical API Notes). `cmd/agent/main.go` loads
`opt-out.json` via `agent.OptOutCachePath()`; missing or malformed file →
warn-and-fall-back to `agent.DefaultOptOut()` (all categories disabled — fresh
agents accept no work until contributor opts in via portal in B6). `printerID=""`
threading deferred to B4. 5 new agent unit tests.

### Sub-phase B3 — Typed Marketplace Enum + Mapping (complete, 2026-04-27)
Commits `7f6919e` and `0121be4` on master. New `internal/types/` package owns
`MarketplaceWorkloadType` (5 values matching the migration 001 `workload_type`
enum). `internal/orchestrator/workload.go` owns `marketplaceToAgent` map
translating to `agent.WorkloadType`. `MustValidateWorkloadMapping()` is the
first action in orchestrator `main()` — mapping staleness is a noisy boot
failure, not a silent dispatch failure. `SubmitJobRequest.Validate()` lifted
out of inline checks for testability. Portal handler validates form input at the
HTTP boundary, defaulting empty `workload_type` through the typed
`MarketplaceAppHosting` constant. `MatchRequest.WorkloadType` carries an
explicit field comment documenting that `FindMatch` does not yet filter on it
(see TODO 11). Resolved former TODO 6 (WorkloadType string mismatch).
8 new unit tests across `internal/types` (3) and `internal/orchestrator` (5).
Defense 3 deferred to B7 (see TODO 13).

### Sub-phase B4 — Print Job Confirmation Flow
Pending-confirmation state for print workloads. Tray notification + portal page surface
job spec to contributor with explicit acknowledgment text. Acceptance logged with
timestamp + spec hash. Decline → orchestrator routes to next printer node. Auto-decline
timeout (~4 hours). Threads `PrinterInfo.ConnectionPath` through `ContainerSpec` so
`DeviceUSBPrinter` finally produces a device mapping (resolves TODO 11).

### Sub-phase B5 — Long-Running Job Lifecycle
Container progress reporting. New statuses: `awaiting_pickup`, `picked_up`, `delivered`.
Failure detection (filament runout, thermal runaway, print detachment) reported as
`failed` with cause. Payout eligibility gated on `picked_up`/`delivered` for prints,
`completed` for compute/storage. Orchestrator `/jobs/<id>/complete` consumes JSON body.
Metering conditioned on exit code 0 (resolves TODOs 7 and 8).

### Sub-phase B6 — Portal UI for Opt-Out Management (in progress, 3/4 commits · `ff1e08d`, `101a6f3`, `5e6c8f5`)
- **Commit `ff1e08d`** — migration 014: `opt_out_compute`, `opt_out_storage`,
  `opt_out_printing`, `opt_out_version`, `opt_out_updated_at` on `nodes`;
  `node_printers` table with composite PK, ON DELETE CASCADE, `enabled` DEFAULT FALSE;
  partial index `idx_node_printers_enabled WHERE enabled = TRUE`.
- **Commit `101a6f3`** — bidirectional heartbeat protocol: `Printers` in register
  payload; `OptOutVersion` + `PrinterHash` in heartbeat request;
  `heartbeatResponse` with optional `OptOut` push (only when `agent < DB version`)
  and `RequestPrinterReport` flag; new `POST /nodes/printers` endpoint.
- **Commit `5e6c8f5`** — Portal `/opt-out` page lists owned nodes with three
  category toggles (compute/storage/printing) + nested per-printer toggles;
  `GET`/`POST /api/opt-out` endpoints; ownership failures return 404 (not 403)
  to avoid leaking node existence; dashboard gains `Opt-out →` column. Also
  fixes latent `ExpiresAt` bug in `authenticatedRequest` test helper.
  Agent: `ResourceOptOut.Version`, `HeartbeatAgent.optOutStore`, `PrinterHash` helper,
  `ReportPrinters` method. 12/12 API integration tests pass, 3/3 agent hash tests pass.
- **Commit #3 (pending)** — portal `/opt-out` page + `GET /api/opt-out` +
  `POST /api/opt-out` handlers; dashboard column with anchor-jump links.
- **Commit #4 (pending)** — `FindMatch` opt-out filter in orchestrator (closes TODO 10).

### Sub-phase B7 — Allowlist Signing + Distribution + Defense 3 (complete, 2026-04-29)
Five commits on master closing TODO 5 (orchestrator `/allowlist` endpoint) and
TODO 13 (Defense 3). Operator action to generate the actual production
keypair and sign v1 deferred to TODO 13 (post-worker-image existence).

- **Commit `4514c10`** — `feat(agent): add Allowlist.Sign + allowlist-genkey + allowlist-sign tools`.
  New `Sign` method on `*Allowlist` mirroring existing `Verify` (reuses
  `canonicalSigningBytes` so they cannot diverge). Two operator binaries
  under `scripts/`: `allowlist-genkey` (one-time keypair bootstrap, refuses
  to overwrite, 0600 perms on private key) and `allowlist-sign` (signs an
  unsigned allowlist JSON, supports stdin/stdout or file flags).
- **Commit `1481cf6`** — `feat(api): publish GET /allowlist endpoint`. New
  `internal/api/allowlist.go` handler reads `ALLOWLIST_PATH` file on every
  request, serves as `application/json` with `Cache-Control: no-store`.
  `internal/api/server.go` restructured: top-level mux holds plain routes
  (`/allowlist` + `/health`), nested mux holds SPIFFE-protected node/job
  routes. `/health` deliberately moved off SPIFFE auth so external monitors
  can reach it (TODO 14).
- **Commit `dd8ffd1`** — `build(installer,ci): inject AllowlistPublicKey via ldflags`.
  `installer/windows/build.ps1` reads `$env:ALLOWLIST_PUBLIC_KEY`,
  hard-fails when `$env:RELEASE -eq "1"` and key is missing, otherwise
  warns and continues. `.github/workflows/ci.yml` reads
  `${{ secrets.ALLOWLIST_PUBLIC_KEY }}` and injects on every build. Doc
  comment in `internal/agent/allowlist.go` corrected to show the full
  module path (was misleading `internal/agent.AllowlistPublicKey`).
- **Commit `17a63f8`** — `docs(b7): allowlist signing runbook + example template`.
  `docs/operations/allowlist-signing.md` (213 lines): one-time keypair
  bootstrap, building/signing allowlist, deployment, key rotation, loss
  recovery. `examples/allowlist.example.json` template with placeholder
  digests. `examples/README.md` explaining usage.
- **Commit `9710f32`** — `feat(orchestrator): Defense 3 submit-time mapping consistency check`.
  `Orchestrator` struct gains `allowlistPath`. `New()` constructor signature
  extended (also threaded through `cmd/orchestrator/main.go` and
  `cmd/portal/main.go` — both binaries construct orchestrators).
  `loadAllowlist()` helper parses but does not verify signature (operator
  trusts local file; agent is the security boundary). `SubmitJob` now
  rejects unknown images, mapping-inconsistent submissions, and missing/
  unparseable allowlist files. Two new unit tests cover rejection paths;
  integration test (`phase1_test.go`) covers the happy path with a real DB.

Operator action remaining (deferred to TODO 13): generate production
keypair via `scripts/allowlist-genkey`, store private key per the four
storage requirements in the runbook, upload public key to GitHub Actions
secret + local env var, build v1 allowlist with real worker image digests,
sign, deploy to `/etc/soholink/allowlist.json` on the orchestrator host.
Blocked on worker images (`soholink/compute-worker`,
`soholink/storage-worker`) being built and published.

### Deployment checkpoint — `6f8d9a2` (2026-04-30) · resolved `0a28f88` (2026-05-01)
Orchestrator added to production Compose stack (`Dockerfile.orchestrator`,
`docker-compose.yml` orchestrator service, `deploy/allowlist/` mount point).
`api.soholink.org` ingress added to cloudflared config; CNAME live in Cloudflare.
Orchestrator image builds cleanly and container is healthy in production following
TODO 12 Option A resolution (commits `303744b`, `6a6f3e3`, `0a28f88`): degraded
mode with plain HTTP, 503 on SPIFFE-protected routes, 200 on `/health`.
Full SPIRE wiring (Option B) still pending — see TODO 13.

### Deployment checkpoint — `101a6f3` (2026-05-05)
Migration 014 applied at portal startup (confirmed `migrations: at version 14`).
B6 wire protocol complete (2/4 commits). Build clean; all tests green.
Production: portal healthy, orchestrator healthy (degraded mode, TODO 13 unchanged).
soholink.org returning 502 — Cloudflare tunnel remote config issue (see TODO 17);
api.soholink.org healthy.

### Deployment checkpoint — `5e6c8f5` (2026-05-05)
B6 commit #3 of 4 shipped: Portal `/opt-out` page + `GET`/`POST /api/opt-out` endpoints.
Build clean; 4 new portal tests green (23/23 portal tests pass overall).
Production unchanged at `101a6f3` until next `deploy/redeploy.sh` cycle.

### Sub-phase B8 — Windows-Native Print Agent
Post-pilot architectural workstream. Native execution path separate from the
containerized agent, targeting Windows print spooler integration. Likely native
agent with Win32 API bindings, separate trust model from containerized workloads.
