# SoHoLINK v2 — Claude Code Context

## What This Project Is
SoHoLINK is a decentralized compute marketplace. Participants contribute idle
hardware (SOHO servers, phones, Smart TVs, laptops) and earn fiat dollars.
Consumers buy compute, storage, and CDN capacity on demand. NTARI operates the
coordination layer — matching, scheduling, metering, and dispute arbitration.

No tokens, no wallets. Pure fiat via Stripe Connect.
Participants own and control their hardware. NTARI never touches the hardware.

This is a ground-up v2 rebuild. The old build is on the `legacy-v1` branch.
Do not reference it. Do not continue or fix it.

## Organization
- **Project:** SoHoLINK
- **Organization:** NTARI (Network Theory Applied Research Institute)
- **Module:** `github.com/NetworkTheoryAppliedResearchInstitute/soholink`
- **Domain:** soholink.org
- **Trust domain:** spiffe://soholink.org
- **Working branch:** v2-rebuild
- **Main branch:** master

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

## Repository Structure (current state)
```
cmd/
  agent/          ← Node agent daemon entry point (main.go complete)
  orchestrator/   ← Control plane entry point (stub)
  portal/         ← Web portal entry point (stub)
internal/
  agent/          ← Hardware detection, resource profiles, heartbeat, executor, telemetry
  api/            ← Control plane HTTP API (node registration, heartbeat, telemetry routes)
  identity/       ← SPIRE integration, TLSClientConfig, TLSServerConfig, RequireSPIFFE middleware
  orchestrator/   ← NodeRegistry, job submission, node matching, job token issuance
  payment/        ← Stripe Connect: client, onboarding, charge, payout, webhook
  portal/         ← Portal HTTP server, session middleware, all handler implementations
  scheduler/      ← Geo-aware job placement with residency constraints
  store/          ← PostgreSQL pool, golang-migrate runner, migrations 001–006
  network/        ← WireGuard bootstrapper (stub)
web/
  templates/      ← layout.html, index.html, login.html, provider_dashboard.html,
                     provider_onboarding.html, provider_provision.html,
                     consumer_marketplace.html, dispute_queue.html
  static/css/     ← portal.css (complete design system)
test/integration/ ← Phase 1 end-to-end integration test (build tag: integration)
```

## Database Migrations (internal/store/migrations/)
| # | File | What it adds |
|---|---|---|
| 001 | `001_initial_schema` | providers, consumers, nodes, jobs, resource_profiles, node_class/job_status enums |
| 002 | `002_add_auth_columns` | `password_hash TEXT`, `is_staff BOOLEAN` on providers; `password_hash TEXT` on consumers |
| 003 | `003_provider_onboarding` | `onboarding_complete BOOL`, `isp_tier TEXT`, `disclosure_accepted_at TIMESTAMPTZ` on providers |
| 004 | `004_resource_pricing` | `resource_type` enum, `resource_pricing` table, seeded with 5 initial rates |
| 005 | `005_resource_profile_pricing` | `price_multiplier NUMERIC(4,3)` on resource_profiles |
| 006 | `006_disputes` | `dispute_status` enum, `disputes` table with evidence_log JSONB, arbiter fields |

To apply all migrations: run the Phase 1 integration test with DATABASE_URL set:
```
DATABASE_URL="postgres://postgres:changeme@localhost:5432/postgres?sslmode=disable" \
  go test -tags integration -v -run TestPhase1EndToEnd ./test/integration/
```
golang-migrate is idempotent — safe to run repeatedly.

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

## Known TODOs
These are acknowledged gaps, not bugs — do not silently fix them without discussion:

1. **`cmd/agent/main.go` — telemetry mTLS client**: `runJob` uses a plain
   `http.Client` for telemetry emission. The control plane wraps every route
   with `identity.RequireSPIFFE`, which will reject a client with no SVID.
   Must be replaced with `identity.NewSource` + `identity.TLSClientConfig`
   in Phase 4. Comment in the file explains this.

2. **`cmd/agent/main.go` — container image placeholder**: `executor.Run` is
   called with `Image: "alpine:latest"`. Real image must come from the job
   assignment payload in Phase 4.

3. **`cmd/orchestrator/main.go` — not yet implemented**: Entry point stub only.
   Must wire `store.Connect`, `store.RunMigrations`, `identity.NewSource`,
   `api.New`, and graceful shutdown — same pattern as `cmd/portal/main.go`.

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
  There is no `StorageSizeBytes` field.
- `dockerclient.IsErrNotFound(err)` to check image presence before pulling.
- Deferred `ContainerRemove` must use `context.Background()` — the request ctx
  may be cancelled before the container exits.

**golang-migrate:**
- Use `stdlib.OpenDBFromPool(pool)` to bridge pgx pool to `database/sql`.
- Call `defer m.Close()` after `migrate.NewWithDatabaseInstance` to avoid deadlock.
- Migration source path: `file://internal/store/migrations`.

**Go `html/template`:**
- All pages define `{{define "content"}}` — if parsed into one shared set, the
  last-parsed definition wins. Portal uses per-request `template.ParseFiles(layoutPath, pagePath)`
  to avoid this. Do not revert to a shared parsed set.
- `template.ParseFiles` names templates by base filename only — subdirectory paths
  are stripped. Keep template base names unique across all subdirectories.

## Coding Conventions
- All errors handled explicitly — no blank `_` discards (except `//nolint:errcheck` on fire-and-forget JSON encoders)
- All inter-service calls use mTLS via SPIRE SVIDs
- No secrets in source or committed config — use env vars
- Database queries use `pgx/v5` directly — no ORM
- HTML templates use Go `html/template` — never `text/template`
- All monetary amounts stored and calculated in cents (`int64`)
- Telemetry payloads HMAC-SHA256 signed: `base64RawURL(payload) + "." + base64RawURL(HMAC(payload, secret))`
- Same signing pattern used for job tokens (`internal/orchestrator`) and session tokens (`internal/portal/middleware.go`)
- JSON struct tags always snake_case — e.g. `json:"node_id"` not `json:"NodeID"`
- `RequireAuth(sm, RequireRole("role", handler))` — auth always wraps role, never the reverse
- `context.Background()` in deferred cleanups that must outlive the request context

## Key Design Decisions
**Payment:** Stripe Connect destination charges. NTARI collects from consumers,
pays out to providers. Platform fee deducted at settlement. 24-hour payout hold
for dispute window.

**Frontend:** Server-rendered HTML via Go `html/template`. No React, no Vue, no
Node.js, no npm, no build step. Vanilla JS only where strictly necessary.
Must work on a 2019 Android phone on 3G. Must work in Smart TV browsers.

**Hardware detection:** Agent-side only via gopsutil. Never browser-side.
Agent polls every 60s and re-registers on hardware change.

**Resource profiles:** Default profile + scheduled overrides per node.
Per-resource toggles: CPU on/off, GPU %, RAM %, storage GB, bandwidth Mbps,
price_multiplier (0.5–2.0×). cgroup v2 enforces caps on launched containers.

**Identity:** SPIFFE/SPIRE, short-lived X.509 SVIDs (1hr TTL). All inter-service
connections use mTLS. Portal sits behind NGINX (plain HTTP to NGINX, mTLS
between internal services).

**Geo scheduling:** Every node geo-tagged at registration. Consumer workloads may
specify country/region constraints. Scheduler refuses to violate hard residency.

**Disputes:** NTARI arbitrates via the Dispute Terminal. Signed telemetry is
primary evidence. Arbiter controls full/partial redistribution.
Default 50/50 split if unresolved after 5 business days.

**Node classes:**
- Class A: SOHO servers — full Docker runtime, all workload types
- Class B: Mobile GPU — Android/iOS, idle-only, batch compute + AI inference
- Class C: Smart TV — Tizen/webOS/AndroidTV, CDN edge cache
- Class D: NAS/storage devices — object storage, CDN

## Current Phase
**Phase 6 — Metering, Payouts & Provider Experience** (starting)

### Phase 3 — Marketplace Portal (complete)
- Portal server with session middleware (HMAC tokens, cookie auth)
- Login handler (bcrypt, role-based redirect)
- Provider onboarding flow (ISP disclosure, Stripe Connect, return handler)
- Provider provisioning page (resource profile form with price_multiplier)
- Consumer marketplace (live node listing with computed pricing)
- Consumer job submission (`/consumer/job` POST + `/consumer/job/{id}` GET)
- Dispute queue terminal (arbiter controls, Accept/Reject/Review, Stripe refund)
- `handleDisputeResolve` and `handleDisputeReview` fully implemented
- `cmd/portal/main.go` wired and building clean
- Migrations 001–006 all applied and passing integration test

### Phase 4 — Control Plane & Agent Hardening (complete)
- Migration 007: `node_heartbeat_events` table, `uptime_pct` column on nodes
- Uptime scorer goroutine (`internal/store/uptime.go`) — runs every 10 min in portal daemon
- Heartbeat event INSERT in `handleHeartbeat` (API server)
- Prometheus metrics package (`internal/metrics/metrics.go`) — counters, gauges, histogram
- Metrics endpoints on separate plain HTTP port (portal `:9090`, API `:9091`)
- `HeartbeatsTotal`, `JobsSubmittedTotal`, `NodesOnlineGauge` wired at call sites
- `RunNodeGauge` goroutine polling online node count every 60s
- Ansible playbook, NGINX config, systemd units, and deployment README in `deploy/`
- `consumer_job_status.html` fully implemented (job ID, status badge, node, created time)

### Phase 5 — Orchestrator & Observability (complete)
- `cmd/orchestrator/main.go` wired: `store.Connect`, `store.RunMigrations`, `identity.NewSource`, `api.New`, graceful shutdown
- Grafana dashboard definitions in `deploy/grafana/`: `network-health.json`, `job-activity.json`
- Session token refresh: `POST /auth/refresh` endpoint with 5-minute sliding window
- Auto-refresh script in `layout.html` — fires every 10 minutes when page is visible
- Orchestrator systemd unit, secrets file, and Ansible tasks added to `deploy/`
- Grafana import instructions added to `deploy/README.md`

### Phase 6 goals
- **Metering service** — track resource consumption per job and compute contributor earnings
- **Payout automation** — `TriggerPayout` exists but no UI or automated trigger for the 24-hour hold release; build the release scheduler and provider-facing payout history
- **Provider earnings dashboard** — `provider_dashboard.html` shows placeholder stats; wire live earnings, pending payouts, and job history
- **Node reliability tiers** — use `uptime_pct` to gate which node classes a provider can list; enforce minimum uptime thresholds per class
- **Rate limiting on auth endpoints** — brute-force protection on `POST /login`
