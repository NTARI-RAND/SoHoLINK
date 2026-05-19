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

Commit messages are drafted by Claude Code (which has direct diff visibility),
audited by Claude Chat, and authorized by the human. Do not append
Co-Authored-By, Signed-off-by, or similar trailers.

**Reply format.** Claude Chat replies are split into two parts to minimize
the human's reading load:

1. **For the human (top):** 1-3 lines. Decision point or status, plus
   judgment calls. End with explicit ask ("Approve?", "Which option?").
2. **For Code (block below):** Paste-ready, self-contained. Commands,
   `str_replace` pairs, verification, commit message verbatim. The human
   pastes without reading line-by-line.

Avoid ambiguity that Code can read as self-authorization — "I will draft
a proposal" not "I'll write the X."

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
  store/          ← PostgreSQL pool, golang-migrate runner, migrations 001–017
  network/        ← WireGuard bootstrapper (stub)
web/
  templates/      ← layout.html, index.html, login.html, register.html,
                     join.html, dashboard.html, opt_out.html, download.html,
                     privacy.html, provider_onboarding.html, provider_provision.html,
                     consumer_marketplace.html, consumer_job_status.html,
                     dispute_queue.html
  static/css/     ← portal.css (complete design system)
installer/
  windows/        ← WiX v4 MSI: SoHoLINK.wxs, build.ps1, LICENSE.rtf, agpl-3.0.txt
scripts/
  allowlist-genkey/  ← Operator tool: generate Ed25519 signing keypair (one-time)
  allowlist-sign/    ← Operator tool: sign allowlist JSON with private key
docs/
  handoffs/       ← Session handoff records (one per session, Dev XVII forward)
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
| 014 | `014_opt_out_and_printers` | `opt_out_compute`, `opt_out_storage`, `opt_out_printing`, `opt_out_version`, `opt_out_updated_at` on `nodes`; `node_printers` table (columns: `printer_id TEXT`, `printer_name TEXT NOT NULL`, `enabled BOOLEAN DEFAULT FALSE`, `detected_at`; composite PK `(node_id, printer_id)`, FK → `nodes(id)` ON DELETE CASCADE); partial index `idx_node_printers_enabled WHERE enabled = TRUE` |
| 015 | `015_print_job_confirmation` | `awaiting_confirmation` and `declined` `job_status` values; `printer_id`, `spec_hash`, `confirmed_at`, `declined_at`, `confirmation_deadline` columns on `jobs`; partial index `idx_jobs_confirmation_deadline WHERE confirmation_deadline IS NOT NULL` for the auto-decline sweeper. `(node_id, printer_id)` pairing enforced at application layer — composite FK avoided due to cascade-semantics conflict with the existing `node_id` FK. See "Migration writing rules" below. |
| 016 | `016_print_workload_types` | `print_traditional` and `print_3d` added to `workload_type` enum. Values consumed by B4 commit 3 dispatcher and existing `FindMatch` opt-out filter. Enum values cannot be removed by rollback — snapshot restore required (see down migration). |
| 017 | `017_job_node_declines` | `job_node_declines` table: tracks which nodes have declined each job; composite PK `(job_id, node_id)`, both FKs `ON DELETE CASCADE`. Used by `RerouteDeclinedJob` to populate `ExcludedNodeIDs` in `FindMatch`, preventing re-dispatch to a node that already declined. |

To apply all migrations: run the Phase 1 integration test with DATABASE_URL set:
```
DATABASE_URL="postgres://postgres:changeme@localhost:5432/postgres?sslmode=disable" \
  go test -tags integration -v -run TestPhase1EndToEnd ./test/integration/
```
golang-migrate is idempotent — safe to run repeatedly.

### Migration writing rules
- `ALTER TYPE … ADD VALUE` works inside a transaction on PG 12+, but the new
  value cannot be USED in the same transaction. This includes references in
  `CREATE INDEX … WHERE` predicates, `INSERT` rows, `UPDATE … SET … = 'newvalue'`,
  or any other place the literal is parsed as an enum. PG raises "unsafe use of
  new value". Either split into two migrations (one adds the value, the next
  uses it), or write the migration to not reference the new value (a looser
  WHERE clause, no enum-typed defaults). This bit migration 015 — see commits
  `de2c091` → `9fa58ba`.
- Signed artifacts placed under `deploy/` must be paired with a `-text` rule
  in `.gitattributes` so Git never converts line endings (signatures are
  computed over raw file bytes).

## Test Coverage (current state — all green in CI)
| Package | File | Tests |
|---|---|---|
| `internal/agent` | `*_test.go` (8 files) | 91 tests: allowlist verification, executor hardening (allowlist + root rejection, HostConfig baseline, tmpfs presence, CUPS device mount, USB printer device mapping, opt-out gate ordering and fail-closed), hardware detection, opt-out store concurrency, printer detection (Unix + Windows), profile scheduling, telemetry signing |
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
| `internal/orchestrator` | `orchestrator_integration_test.go` | 6 integration tests (build tag: integration): `awaiting_confirmation` write path with printer resolve, registry/DB drift error, compute scheduled path, flag-off print path, `RerouteDeclinedJob` success + no-candidates → failed |

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

9. **`DeviceUSBPrinter` device-mapping RESOLVED (B4 commits 1 + agent wiring · `b1500f6`, `0431188`)**:
   `ContainerSpec.ConnectionPath` field + `deviceMountsFor` wired in commit 1. Agent-side wiring
   completed in `0431188`: `handleGetJobs` in `internal/api/nodes.go` surfaces `printer_id` from
   the DB (with `omitempty` for backward compat); `internal/agent/printers.go` gains
   `ResolveConnectionPath` (pure function, 3 unit tests); `cmd/agent/main.go`'s `runJob` calls
   `ResolveConnectionPath` before starting the telemetry goroutine and passes the result as
   `ContainerSpec.ConnectionPath`. Also fixes a pre-existing goroutine leak where image-empty
   and printer-not-found early returns came after the goroutine start. TODO 9 fully closed.

10. **`FindMatch` opt-out filter** — RESOLVED `fe83d19` (B6 commit #4): `NodeEntry`
    now carries opt-out state refreshed by `handleHeartbeat`; FindMatch maps
    `WorkloadType` → agent category and skips opted-out nodes. Agent-side gate (B2)
    remains canonical; this is defense-in-depth at dispatch time. Staleness window
    bounded by heartbeat interval. Printing branch covers `WorkloadPrintTraditional`
    + `WorkloadPrint3D` and additionally requires an enabled printer.

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

13. **RESOLVED (2026-05-07, Dev XV).** Full SPIRE agent wired into Compose stack. Option B implemented across four commits (`a3cce4b`, `095db23`, `f1f84be`, plus the `pid: "host"` fix). Key design decisions locked:
    - `spire-agent` service uses `pid: "host"` — required for the `unix` WorkloadAttestor to resolve caller PIDs across container namespaces. Gives the agent container read access to all host process metadata; cannot control processes. Accepted trade-off for single-host deployment; documented in SPIRE's own Docker guidance.
    - `deploy/spire/agent.conf` uses `insecure_bootstrap = true` — acceptable on Docker internal bridge network.
    - `TLSServerConfigOptional` added to `internal/identity/spiffe.go`: uses one-way TLS (`tlsconfig.TLSServerConfig`) + `tls.RequestClientCert`. Server presents SVID; client cert is requested but not required at TLS layer. `/health` and `/allowlist` reachable without client cert; `RequireSPIFFE` enforces SPIFFE identity at HTTP layer for protected routes.
    - Workload entry registered (one-time, in SPIRE server datastore): entry ID `9197354b-0ef7-4fec-a151-7cb7a7f9f4a0`, SPIFFE ID `spiffe://soholink.org/orchestrator`, selector `unix:uid:0`, parent `spiffe://soholink.org/spire/agent/join_token/dcd15ddf-68f8-4975-b854-0af818412fd2`.
    - Probe confirmed: `POST https://api.soholink.org/nodes/register` returns `mTLS required` (not 503). SPIFFE middleware is live.
    - **Re-attestation (if `spire_agent_data` volume is wiped):** generate new token, update `SPIRE_AGENT_JOIN_TOKEN` in `.env`, delete old workload entry (`entry delete -id 9197354b-...`), `docker compose up -d`, re-run `deploy/register-entries.sh`.
    - New env vars in `.env`: `SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock`, `SPIRE_AGENT_JOIN_TOKEN=<token>`.

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

17. **soholink.org 502 — Cloudflare Zero Trust remote tunnel config**: **RESOLVED 2026-05-06**
    (Cloudflare dashboard manual fix; no code commit). Two issues found and fixed:
    (a) `soholink.org` routed to `https://portal:8080` (wrong scheme — portal is plain HTTP),
    causing 502; (b) `api.soholink.org` was missing from the dashboard's public hostname list
    entirely (orphan DNS record only), causing 404 at the edge via the catch-all
    `http_status:404` ingress rule. Fixed by editing the `soholink.org` rule HTTPS→HTTP,
    deleting the orphan `api` Tunnel-type DNS record, and adding a new public hostname
    `api.soholink.org → http://orchestrator:8082`. Verified externally:
    `soholink.org` 200, `api.soholink.org/health` 200, `api.soholink.org/nodes/register` 503
    (fail-closed via TODO 13 — see below). Follow-up: see TODO 18 for canonical sync tooling
    so this category of dashboard-vs-local drift cannot happen silently again.

18. **`deploy/sync-tunnel-config.sh` — canonical local→Cloudflare-dashboard tunnel config sync**:
    Today's outage (TODO 17) was caused by silent drift between local `~/.cloudflared/config.yml`
    and the Cloudflare Zero Trust dashboard's remote-managed tunnel config. Local file is the
    intended source of truth. A script should `PUT /accounts/{account}/cfd_tunnel/{tunnel}/configurations`
    with the local ingress translated to the API's JSON shape, on demand or as part of every
    deploy. Account UUID `52f1117eaaa85f885309416a052b0687`, tunnel UUID
    `bb7b7f0d-0d50-4d58-858b-abc52f1d7cd4`, cert at `~/.cloudflared/cert.pem`. Auth format
    requires research before implementation — Cloudflare's modern API typically uses
    `Authorization: Bearer <api-token>` with a token scoped to "Cloudflare Tunnel:Edit",
    not raw cert.pem contents. Defer to a focused follow-up session. Not blocking
    participant testing.

19. **SignPath GitHub Actions code-signing integration** (NEW, Dev XVI):
    Application submitted to SignPath Foundation 2026-05-08. Forward-looking
    attribution language live on `/download` and `/privacy` pages. Once approved
    (typical 1–2 week lead time per OSS project anecdotes), implement: (a)
    `.github/workflows/sign-msi.yml` workflow that uploads MSI artifact to
    SignPath and retrieves signed version; (b) update `installer/windows/build.ps1`
    to integrate signing or add as separate step; (c) replace
    `web/static/SoHoLINK-Setup.msi` with signed build; (d) flip Code Signing
    card text on `download.html` and `privacy.html` from forward-looking
    ("once verification is complete") to present-tense ("is digitally signed").
    Removes the SmartScreen "Unknown publisher" warning that currently blocks
    non-technical participants.

20. **Sign-verify roundtrip check on `mustEd25519Key` startup** — RESOLVED Dev XVIII (`547374d`).
    `mustEd25519Key` performs a sign-then-verify probe ("soholink-key-self-test-v1")
    against the derived public key after the length check. Catches the
    64-bytes-pass-length-but-public-half-doesn't-match-seed failure mode that
    motivated this TODO. Codified as a coding convention so any future
    asymmetric-key loader picks up the same check.

21. **GitHub Actions Node.js 20 deprecation deadline 2026-06-02** — RESOLVED (Dev XIX,
    `0dd2d77`). `actions/checkout` and `actions/setup-go` bumped to v6 ahead of
    GitHub's 2026-06-02 Node 20 end-of-life. CI green.

22. **Printer-type discrimination missing in `node_printers`**: Migration 014's
    `node_printers` table has no `printer_type` column. The B4 commit 3 dispatcher
    picks the lowest `printer_id` lexicographically among enabled printers regardless
    of whether the workload is `print_traditional` or `print_3d`. A node enabled for
    one will match jobs for the other. Acceptable for the single-printer-per-node
    pilot; correct fix is a migration adding `printer_type` (enum: `traditional` /
    `threed`) populated from the agent's `PrinterInfo` detection, and a `WHERE`
    clause filter in the printer-resolve query. Tracked for B8 — Windows-native print
    agent will need this anyway since print spooler discrimination is
    platform-specific.

23. **`SubmitJob` DB-path integration test gap** — RESOLVED `dc1de1d` (Dev XXI): Six integration
    tests in `internal/orchestrator/orchestrator_integration_test.go` (build tag: integration).
    Covers: `awaiting_confirmation` write path with printer resolve (verifies `printer_id`,
    32-byte `spec_hash`, future `confirmation_deadline`), registry/DB drift error, compute
    scheduled path, flag-off print scheduled path, `RerouteDeclinedJob` success path, and
    `RerouteDeclinedJob` no-candidates → failed. All six tests green against local Postgres.
    Also surfaced that `printer_name TEXT NOT NULL` was missing from the migration 014
    description in CLAUDE.md (now corrected). Carry-forward from TODO 11 closed.

24. **Zombie `running` rows — `handleGetJobs` flips `scheduled` → `running` optimistically**:
    `internal/api/nodes.go`'s `handleGetJobs` UPDATEs a job's status to `running` at poll time,
    before the agent confirms it can actually start the container. If the agent's `runJob` then
    returns early (printer vanished, image not in allowlist, container start failure), the job
    stays stuck in `running` with no recovery path. Fix requires either a start-confirmation
    endpoint (`POST /jobs/<id>/started`) the agent calls after successful container launch, or a
    running-timeout reaper that flips stale `running` rows back to `scheduled` (or `failed`).

25. **Portal's orphaned registry — `cmd/portal/main.go` constructs a `NodeRegistry` that never
    receives heartbeats**: `portal/main.go` calls `orchestrator.NewNodeRegistry()` and passes it
    to `orchestrator.New`, but agent heartbeats land at the orchestrator binary's API server,
    not at the portal process. `FindMatch` on the portal's registry returns "no available nodes
    match request" for *every* job submitted through `POST /consumer/job`, regardless of workload
    type. Latent rather than active because no production traffic flows through this path yet —
    the consumer-side marketplace is non-functional but unused. Fix paths (all non-trivial): expose
    an orchestrator-side HTTP submission endpoint the portal calls over the network; merge portal
    and orchestrator into one process; or have both processes refresh a shared registry from the
    DB on heartbeat events.

26. **Integration tag missing from exported-signature audit workflow** — RESOLVED `27157dc`:
    B4 commit 3 (`0115d41`) extended `orchestrator.New` with two new parameters. The audit grep
    for callers scoped to `cmd/` and `internal/`, missing `test/integration/phase1_test.go:108`.
    The stale call site compiled fine without the `integration` build tag, so local
    `go build ./cmd/...` and `go test ./internal/...` passed on every B4 commit. CI runs with
    the tag set and broke on #68 through #71. Rule now documented in Critical API Notes
    (Exported function signature changes); pre-commit verification flow gained
    `go build -tags integration ./...` and `grep -rn "FuncName(" .` on signature-changing
    commits.

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

**Exported function signature changes:**
- When adding parameters to any exported function, grep the full repo for callers:
  `grep -rn "PackageName\.FuncName(" .` — `test/integration/` is a peer of `cmd/`
  and `internal/` and will be missed by a narrower scope.
- After the change, verify with `go build -tags integration ./...` before committing.
  Build-tagged files are invisible to ordinary `go build ./...` and `go test ./...`.
  CI runs with the tag set; missing this locally means CI is the first build attempt.
  (Root cause of `test/integration/phase1_test.go` stale call, CI #68–#71.)

**CI verification (in-loop via gh):**
- After pushing to master, run `gh run list -R NTARI-RAND/SoHoLINK --limit 1`
  to confirm the latest commit passes CI before continuing work. Without this,
  failures can compound across multiple commits unobserved (CI #68–#71 in
  Dev XXI ran red for four commits before being surfaced externally).
- `gh` was installed in Dev XXI via `winget install --id GitHub.cli` and
  authenticated with `gh auth login`. If missing on a fresh shell, reinstall
  the same way.

**Workload type vocabulary (post-B3):**
- Two enums, by design — they evolve independently:
  - `types.MarketplaceWorkloadType` (in `internal/types/workload.go`) is the
    customer-facing enum. Seven values: `app_hosting`, `batch_compute`, `ai_inference`,
    `object_storage`, `cdn_edge`, `print_traditional`, `print_3d`. Constants prefixed
    `Marketplace*`. Values match the PostgreSQL `workload_type` enum (migrations 001 + 016).
  - `agent.WorkloadType` (in `internal/agent/`) is the hardware-affinity / opt-out
    enum: `compute`, `storage`, `print_traditional`, `print_3d`. Note: there is
    NO `agent.WorkloadPrinting` constant — both print constants share the single
    `opt_out_printing` DB flag and the single `node_printers.enabled` check.
    Agent's `optout.go` groups them via `case WorkloadPrintTraditional,
    WorkloadPrint3D:`; orchestrator's `FindMatch` mirrors this exactly.
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
- **Print jobs require contributor consent per job, not anonymous matching.**
  `print_traditional` and `print_3d` are in the marketplace enum (migration 016)
  and dispatched by the same `SubmitJob` path as compute/storage, but routed through
  a confirmation lifecycle when `PRINT_CONFIRMATION_ENABLED` is set. With the flag
  off (production default), print jobs write `status='scheduled'` and behave
  identically to compute/storage. With the flag on, `SubmitJob` writes
  `awaiting_confirmation` with `printer_id`, `spec_hash`, and `confirmation_deadline`
  populated; the agent's `scheduled`-filtered poll never sees these jobs until later
  B4 commits implement the forward steps. Do not flip the flag in production
  until B4 is fully deployed — without the auto-decline sweeper (commit 6,
  `0475943`), expired confirmations stall in `awaiting_confirmation` indefinitely.
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
- Signed artifacts (allowlists, signed JSON, etc.) must be marked `-text` in `.gitattributes` so Git never converts line endings — signatures are computed over raw file bytes and any LF↔CRLF translation breaks verification (see `deploy/allowlist/*.json` in `.gitattributes`).
- Any function that loads an asymmetric private key from env (e.g. `mustEd25519Key`) must perform a sign-then-verify roundtrip on a constant probe message before returning. Catches the failure mode where the byte length is valid but the embedded public-key half doesn't correspond to the seed.

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
Docker Compose stack: postgres + spire-server + spire-agent + portal + NGINX +
cloudflared + orchestrator. Orchestrator obtains SPIRE SVID on startup via the
unix Workload API; full SPIFFE/SPIRE wiring complete (see TODO 13 RESOLVED, Dev XV).
- **`docker-compose.yml`** — postgres + spire-server + spire-agent + portal + NGINX + cloudflared + orchestrator services
- **`Dockerfile.portal`** — multi-stage Go build; final image copies binary + `web/` + `spire-server` binary (for portal-side token generation)
- **`Dockerfile.orchestrator`** — multi-stage Go build; final image copies orchestrator binary
- **`deploy/spire/agent.conf`** — SPIRE agent config: `unix` WorkloadAttestor, `join_token` NodeAttestor, `insecure_bootstrap = true` (acceptable on internal Docker bridge network)
- **`deploy/register-entries.sh`** — one-time SPIRE workload entry registration; re-run if `spire_agent_data` volume is wiped (see TODO 13 RESOLVED for procedure)
- **`nginx.conf`** — reverse proxy to `portal:8080` for `soholink.org`
- **`.env`** — `DATABASE_URL`, `SESSION_PRIVATE_KEY`, `ORCHESTRATOR_TOKEN_SECRET`, `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `SPIFFE_ENDPOINT_SOCKET`, `SPIRE_AGENT_JOIN_TOKEN`; gitignored
- **Cloudflare Tunnel** — `soholink-prod` (`bb7b7f0d-0d50-4d58-858b-abc52f1d7cd4`)
- **DNS** — CNAME `soholink.org` → tunnel (proxied); CNAME `api.soholink.org` → tunnel (proxied), live
- **Public pages** — `/`, `/login`, `/register`, `/join`, `/download`, `/privacy`, `/static/*` (auth-free)
- **MSI installer** — live at `https://soholink.org/static/SoHoLINK-Setup.msi` (~16 MB; currently unsigned dev build, SignPath Foundation application pending — see TODO 19)

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

### Sub-phase B4 — Print Job Confirmation Flow (complete, Dev XXII · `0475943`)
Pending-confirmation state for print workloads. Tray notification + portal page surface
job spec to contributor with explicit acknowledgment text. Acceptance logged with
timestamp + spec hash. Decline → orchestrator routes to next printer node. Auto-decline
timeout (~4 hours). Threads `PrinterInfo.ConnectionPath` through `ContainerSpec` so
`DeviceUSBPrinter` finally produces a device mapping (resolves TODO 9).

- **Commit 1 (`b1500f6`)** — `ContainerSpec.ConnectionPath` field added; `deviceMountsFor`
  on Unix consumes it and produces a `DeviceMapping` with rwm cgroup permissions when
  non-empty. Windows stub takes the new parameter and ignores it. New table-driven
  test `TestBuildHostConfig_USBPrinterDeviceMapping` (populated + empty cases,
  skips on Windows like the existing CUPS test).
- **Commit 2 (`de2c091` corrected by `9fa58ba`)** — Migration 015: `awaiting_confirmation`
  and `declined` `job_status` values; `printer_id`, `spec_hash`, `confirmed_at`,
  `declined_at`, `confirmation_deadline` columns on `jobs`; partial index for the
  auto-decline sweeper. `(node_id, printer_id)` pairing enforced at application
  layer, not by composite FK (cascade semantics conflict with the existing
  `node_id` FK from migration 001). Initial commit had a same-transaction enum
  reference in the index predicate (PG "unsafe use of new value" error); the
  follow-up corrected to `WHERE confirmation_deadline IS NOT NULL`. Schema-only:
  no code reads or writes the new columns yet — behavior unchanged until commit 3.
- **Commit 3 (`0115d41`)** — Print confirmation dispatcher gate. When
  `PRINT_CONFIRMATION_ENABLED` is set and the workload is `print_traditional` or
  `print_3d`, `SubmitJob` writes `status='awaiting_confirmation'` with `printer_id`
  (resolved from `node_printers`), `spec_hash` (SHA-256 over contributor-visible spec
  fields including container image; `ConsumerID` excluded as orchestrator-internal),
  and `confirmation_deadline` (now + 4h, hardcoded). With the flag off, identical to
  scheduled-path; production default keeps the flag off. Migration 016 adds
  `print_traditional`/`print_3d` to the `workload_type` enum. Six modified files +
  two new migrations; 4 new unit tests (3 hash determinism, 1 flag-off gate behavior).
- **Commit 4 (`93d5381`)** — Portal `/provider/job/{id}/confirm` page (GET + POST).
  Contributor sees job spec, spec fingerprint, deadline; accepts (→ `scheduled`) or
  declines (→ `declined` + `job_node_declines` row, transaction-guarded). Race
  detection via `RowsAffected() == 0`. `JobConfirmData` struct; two new routes;
  `contributor_job_confirm.html` template. Imports `"errors"` and `"github.com/jackc/pgx/v5"`.
- **Agent wiring (`0431188`)** — `printer_id` surfaced in `handleGetJobs` poll response
  (`omitempty` for backward compat); `ResolveConnectionPath` pure function in
  `internal/agent/printers.go` maps printer ID → `ConnectionPath`; `runJob` in
  `cmd/agent/main.go` calls it before the telemetry goroutine start (also fixes
  pre-existing goroutine leak). 3 new agent unit tests. Closes TODO 9.
- **Commit 5 (`ad2dda2`)** — Decline reroute + worker loop. Migration 017 adds
  `job_node_declines` table (composite PK, both FKs ON DELETE CASCADE).
  `MatchRequest.ExcludedNodeIDs` + exclusion map in `FindMatch`. `RerouteDeclinedJob`
  reads declines, calls FindMatch with exclusions, re-dispatches or fails job.
  `StartDeclineRerouteLoop` 30s ticker goroutine. Also wires `StartEvictionLoop`
  (defined Dev XV, never called from `main.go`). 1 new unit test.
- **Integration coverage (`dc1de1d`)** — `orchestrator_integration_test.go` (build tag:
  integration): 6 tests against real Postgres covering all SubmitJob print-gate paths
  and both RerouteDeclinedJob outcomes. Closes TODO 23.
- **Commit 6 (`0475943`)** — Auto-decline sweeper. Exported per-job function
  `ExpireConfirmation(ctx, jobID) (bool, error)` with a race-safe UPDATE that
  re-checks status and deadline; `flipped=false, err=nil` is the lost-race case.
  Unexported batch tick handler `expireConfirmations` mirrors the
  `rerouteDeclined` pattern (SELECT ids → iterate → UPDATE, LIMIT 100). Wired
  into `StartDeclineRerouteLoop` before `rerouteDeclined` so freshly-expired
  jobs reroute in the same 30s tick. `idx_jobs_confirmation_deadline` from
  migration 015 powers the SELECT. 2 new integration tests.

### Sub-phase B5 — Long-Running Job Lifecycle (design locked, Dev XXIII)

Closes TODOs 5 (`/complete` JSON body), 6 (exit-code-conditioned metering), and
24 (zombie `running` rows). Adds the print-specific lifecycle statuses
`awaiting_pickup`, `picked_up`, `delivered` named in the original scope, plus
failure-cause reporting for print workloads.

**State machine.**

Compute / storage: `scheduled → dispatched → running → completed | failed`

Print: `scheduled → dispatched → running → awaiting_pickup → picked_up → delivered | failed`

`dispatched` is a new intermediate status that closes TODO 24. `handleGetJobs`
flips `scheduled → dispatched` as the atomic claim; the agent transitions
`dispatched → running` via `POST /jobs/{id}/started` after `ContainerStart`
succeeds. A reaper reverts stale `dispatched` rows (> 60s without `/started`)
back to `scheduled`. Chosen over the alternative of keeping the
`scheduled → running` flip with a `started_at IS NULL` reaper — the new
intermediate status gives `running` an unambiguous meaning at the cost of one
enum value.

**Commit plan (7 commits).**

1. Migration 018 — extend `job_status` with `dispatched`, `awaiting_pickup`,
   `picked_up`, `delivered`; add columns `started_at`, `picked_up_at`,
   `delivered_at`, `exit_code`, `failure_cause`. Enum down migration is
   one-way (Postgres limitation, documented in down SQL).
2. Start-confirmation endpoint (`POST /jobs/{id}/started`) + `dispatched`-timeout
   reaper. `handleGetJobs` switches to `scheduled → dispatched`. Closes TODO 24.
3. `/complete` parses JSON body `{exit_code, failure_cause, tmpfs_exhausted}`.
   Closes TODO 5.
4. Exit-code-conditioned metering. `exit_code != 0` → `failed`, no meter.
   `exit_code == 0` → `completed` for compute/storage, `awaiting_pickup` for
   print. Closes TODO 6.
5. Print lifecycle endpoints `POST /jobs/{id}/pickup` and
   `POST /jobs/{id}/delivered`; transition logic; ownership/authorization per
   policy answers below.
6. Failure-cause reporting: agent detects filament runout, thermal runaway,
   print detachment via printer telemetry; orchestrator persists the cause.
7. Payout eligibility query update — prints metered on `delivered`; compute
   and storage on `completed`.

**Open policy questions (block C5+ only).**

- Who confirms `awaiting_pickup → picked_up`: contributor, consumer, or
  NTARI admin?
- Who confirms `picked_up → delivered`: most likely consumer; needs lock.
- Payout trigger for prints: fire at `picked_up` or hold until `delivered`?
  Working preference: hold until `delivered`; `picked_up → delivered` window
  acts as the dispute window.

### Sub-phase B6 — Portal UI for Opt-Out Management (complete, 2026-05-05 · `ff1e08d`, `101a6f3`, `5e6c8f5`, `fe83d19`)
- **Commit `ff1e08d`** — migration 014: `opt_out_compute`, `opt_out_storage`,
  `opt_out_printing`, `opt_out_version`, `opt_out_updated_at` on `nodes`;
  `node_printers` table with composite PK, ON DELETE CASCADE, `enabled` DEFAULT FALSE;
  partial index `idx_node_printers_enabled WHERE enabled = TRUE`.
- **Commit `101a6f3`** — bidirectional heartbeat protocol: `Printers` in register
  payload; `OptOutVersion` + `PrinterHash` in heartbeat request;
  `heartbeatResponse` with optional `OptOut` push (only when `agent < DB version`)
  and `RequestPrinterReport` flag; new `POST /nodes/printers` endpoint.
  Agent: `ResourceOptOut.Version`, `HeartbeatAgent.optOutStore`, `PrinterHash`
  helper, `ReportPrinters` method. 12/12 API integration tests pass, 3/3 agent
  hash tests pass.
- **Commit `5e6c8f5`** — Portal `/opt-out` page lists owned nodes with three
  category toggles (compute/storage/printing) + nested per-printer toggles;
  `GET`/`POST /api/opt-out` endpoints; ownership failures return 404 (not 403)
  to avoid leaking node existence; dashboard gains `Opt-out →` column. Also
  fixes latent `ExpiresAt` bug in `authenticatedRequest` test helper.
- **Commit `fe83d19`** — Orchestrator `FindMatch` filters by opt-out: `NodeEntry`
  gains `OptOutCompute`/`Storage`/`Printing` and `HasEnabledPrinter` fields;
  new `UpdateOptOut` method; `handleHeartbeat` extends opt-out SELECT with
  `EXISTS(node_printers.enabled)` and refreshes registry on every beat.
  FindMatch maps `WorkloadType` → agent category via `MarketplaceToAgent` and
  skips opted-out nodes. `WorkloadPrintTraditional` + `WorkloadPrint3D` share
  one case label (matching agent `optout.go`) and require an enabled printer.
  Closes TODO 10. 5 new orchestrator tests + full-repo sweep green.

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

### Deployment checkpoint — `fe83d19` (2026-05-05)
B6 commit #4 of 4 shipped — **B6 fully complete**. Orchestrator `FindMatch`
now filters opted-out nodes at dispatch time (TODO 10 closed). 5 new
orchestrator tests green; full repo sweep passes. Production unchanged
at `101a6f3` until next `deploy/redeploy.sh` cycle.

### Deployment checkpoint — `6eccf64` (2026-05-06)
Production rolled forward from `101a6f3` to `6eccf64` — B6 fully live, new opt-out
participant UI publicly accessible, orchestrator's `FindMatch` opt-out filter active
under real dispatch. Includes script fix `0a8b3e8` (`deploy/redeploy.sh` now rebuilds
orchestrator alongside portal — previous omission would have silently dropped
`fe83d19`'s orchestrator changes from production on any prior `redeploy.sh` invocation).
Cloudflare Zero Trust dashboard reconciled to local `config.yml` ingress (TODO 17 RESOLVED):
`soholink.org` rule HTTPS→HTTP, new `api.soholink.org → http://orchestrator:8082` public
hostname, orphan `api` DNS record deleted and recreated as part of the new public
hostname rule. All public routes green; SPIFFE-protected routes correctly fail-closed
pending TODO 13. No agent traffic yet — agents not deployed; participant testing
blocked on TODO 13.

### Deployment checkpoint — `f1f84be` (2026-05-07, Dev XV)
TODO 13 Option B complete. SPIRE agent service added to Compose stack (`pid: "host"`, `insecure_bootstrap = true`, `unix` WorkloadAttestor). Orchestrator obtains SVID on startup — no degraded mode. TLS listener uses `TLSServerConfigOptional` (optional client cert). Cloudflare `api.soholink.org` backend updated to HTTPS with no-verify. All routes healthy: `soholink.org` 200, `api.soholink.org/health` 200, `api.soholink.org/nodes/register` returns `mTLS required` (expected). Participant testing remains blocked on end-to-end self-test (next priority).

### Deployment checkpoint — `2675402` (2026-05-08, Dev XVI)
First-run participant onboarding overhaul. Dashboard empty state replaced with
inline 3-step panel (token → MSI download → install steps); reordered so agent
install precedes Stripe Connect setup. `HasStripe` field added to `DashboardData`;
Stripe nudge card visible only when `HasNodes && !HasStripe`. Token persistence:
`buildDashboardData` surfaces unused unexpired tokens across page reloads.
Max-1-unused cap enforced in `handleGenerateNodeToken`. New public pages:
`GET /download` (with SignPath Foundation forward-looking attribution) and
`GET /privacy` (full data practices, dated 2026-05-08). Tagline "Clouds Are
Everywhere" added to homepage hero. SignPath Foundation application submitted
same day; awaiting verification (see TODO 19). Login broke and was resolved
separately this session — `SESSION_PRIVATE_KEY` was malformed (bytes 32–63
didn't match seed-derived public key); rotated via `scripts/genkey/main.go`,
`.env` only, no code commit needed. See TODO 20 for proposed defensive
sign-verify roundtrip check at portal startup.

### Deployment checkpoint — `4c76919` (2026-05-14, Dev XVIII)
Eight commits, no production deploy. **B small wins**: gitignored dev allowlist
keys with `.gitattributes -text` rule for signed-artifact integrity (`a2a8e3a`),
Ed25519 sign-verify roundtrip on portal key load (`547374d`, closes TODO 20),
near-white installer bitmap backgrounds for wizard text legibility (`ff35b8b`).
**B4 commits 1–2**: `ContainerSpec.ConnectionPath` threading through
`deviceMountsFor` to produce USB printer `DeviceMapping` on Unix (`b1500f6`,
resolves device-mapping half of TODO 9); migration 015 for the print job
confirmation lifecycle (`de2c091`, corrected by `9fa58ba`). **CI fix**: workflow
Go version drift resolved via `go-version-file: go.mod` (`4c76919`) — green CI
restored. **Process record**: Dev XVII handoff doc landed at
`docs/handoffs/dev-xvii.md` (`97b3cef`); `docs/handoffs/` is now the canonical
location.

Production state unchanged from Dev XVII. Migration 015 applied at the Dev XIX
orchestrator rebuild. Dispatcher gate (B4 commit 3) shipped in Dev XX (see checkpoint
below); production behavior remains unchanged until `PRINT_CONFIRMATION_ENABLED` is
flipped. Service-start blocker remains open on TODO 19 (SignPath).

### Deployment checkpoint — `9af4c16` (2026-05-15, Dev XIX)
Two commits, no new features; recovery + hardening session. **SPIRE recovery**: a
56-hour host outage (router and machine shared a circuit, brought down by a
router-off sleep test) left the agent's cached trust bundle stale; production was
silently in SPIFFE-degraded mode for ~34 hours before symptoms surfaced. Recovery
sequence — evict attested node, generate new join token, redirect workload entry,
rotate `.env`, wipe `spire_agent_data` volume contents, restart — restored stack to
healthy. Detailed runbook in `docs/handoffs/dev-xix.md`. **`ca_ttl` 24h → 720h
(`9af4c16`)**: widens manual-recovery window to 30 days; does not eliminate the
recurrence vector (re-attestable node attestor is the proper fix, on the TODO list).
**CI Node 24 bump (`0dd2d77`)**: `actions/checkout` and `actions/setup-go` bumped
to v6 ahead of GitHub's 2026-06-02 deadline (closes TODO 21). **Orchestrator
rebuild**: live image was a 2026-05-09 snapshot missing migration 015 and commit
`b1500f6`; rebuilt to current master, applied migration 015. Tagged prior image
`:pre-rebuild` for rollback. Production: `api.soholink.org/health` 200 (genuine,
no longer degraded-mode masked); all seven containers healthy.

### Deployment checkpoint — `0115d41` (2026-05-16, Dev XX)
One commit, no production behavior change (flag-gated). **B4 commit 3 (`0115d41`)**:
print confirmation dispatcher gate behind `PRINT_CONFIRMATION_ENABLED`; migration 016
adds `print_traditional`/`print_3d` to the `workload_type` enum; `canonicalJobSpecHash`
helper added for spec-drift detection (portal confirm page, commit 4, will use this
hash). Eight files (six modified + two new migrations); 4 new unit tests. Flag stays
off in production until at least commit 5 lands. Migration 016 will apply at the next
orchestrator restart and is a no-op until application code references the new enum
values (which it does, gated on the flag).

### Deployment checkpoint — `dc1de1d` (2026-05-16, Dev XXI)
Four commits, no production deploy (production still at `0115d41`; flag remains off).
**B4 commit 4 (`93d5381`)**: portal confirm page for print jobs (`GET`/`POST /provider/job/{id}/confirm`).
**Agent wiring (`0431188`)**: `printer_id` threaded through job-poll → `ResolveConnectionPath` →
`ContainerSpec.ConnectionPath`; goroutine leak in `runJob` fixed; 3 agent unit tests. Closes TODO 9.
**B4 commit 5 (`ad2dda2`)**: decline reroute — migration 017 (`job_node_declines`), `ExcludedNodeIDs`
on `FindMatch`, `RerouteDeclinedJob`, `StartDeclineRerouteLoop`, `StartEvictionLoop` wired. 1 new unit test.
**TODO 23 (`dc1de1d`)**: 6 integration tests in `internal/orchestrator/orchestrator_integration_test.go`
against real Postgres; all green. `printer_name TEXT NOT NULL` missing from migration 014 CLAUDE.md
description corrected. Do not deploy until B4 commit 6 (auto-decline sweeper) is ready and reviewed — the
portal decline action without the sweeper leaves jobs in `awaiting_confirmation` indefinitely if the 4-hour
window expires without a sweeper running.
**CI fix (`27157dc`)**: `test/integration/phase1_test.go` stale 5-arg `orchestrator.New` call updated
to 7-arg (`false, 4*time.Hour`). CI #68–#71 were all failing on this; #72 green. Root cause documented
in TODO 26 and Critical API Notes (exported signature change workflow).

### Deployment checkpoint — `0475943` (2026-05-18, Dev XXII) · resolved `2528e79` (2026-05-18)
**`027ac81`**: Codifies the three-layer commit message convention (Code drafts →
Chat audits → human authorizes) that had emerged in practice but was not yet
written down.
**B4 commit 6 (`0475943`)**: Auto-decline sweeper. `ExpireConfirmation` exported
per-job function + `expireConfirmations` batch tick handler co-located on the
existing `StartDeclineRerouteLoop` 30s ticker (expire-then-reroute ordering).
2 new integration tests. B4 lifecycle complete — the dc1de1d deploy hold ("Do
not deploy until B4 commit 6") is resolved.

### Sub-phase B8 — Windows-Native Print Agent
Post-pilot architectural workstream. Native execution path separate from the
containerized agent, targeting Windows print spooler integration. Likely native
agent with Win32 API bindings, separate trust model from containerized workloads.
