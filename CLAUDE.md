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
  agent/          ← Hardware detection, resource profiles, heartbeat, executor,
                     telemetry, config.go (NodeConfig, ClaimNode, LoadConfig, SaveConfig)
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

To apply all migrations: run the Phase 1 integration test with DATABASE_URL set:
```
DATABASE_URL="postgres://postgres:changeme@localhost:5432/postgres?sslmode=disable" \
  go test -tags integration -v -run TestPhase1EndToEnd ./test/integration/
```
golang-migrate is idempotent — safe to run repeatedly.

## Test Coverage (current state — all green in CI)
| Package | File | Tests |
|---|---|---|
| `internal/portal` | `middleware_test.go` | Ed25519 token create/verify, tampered sig, expiry, RequireAuth redirect |
| `internal/portal` | `handlers_test.go` | 19 handler tests: login, register, job submission, dispute resolution |
| `internal/store` | `payouts_test.go` | EligiblePayouts query with seeded DB |
| `internal/store` | `metering_test.go` | 4 metering integration tests |
| `internal/store` | `uptime_test.go` | TestRunUptimeScorer — seeds 19152 heartbeats, verifies uptime_pct update |
| `internal/api` | `*_test.go` | 7 API handler tests: node registration, heartbeat, job completion |
| `internal/orchestrator` | `orchestrator_test.go` | 9 registry tests: geo match, GPU filter, offline exclusion, eviction, stale eviction |
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

**golang-migrate:**
- Use `stdlib.OpenDBFromPool(pool)` to bridge pgx pool to `database/sql`.
- Call `defer m.Close()` after `migrate.NewWithDatabaseInstance` to avoid deadlock.

**Go `html/template`:**
- All pages define `{{define "content"}}` — portal uses per-request
  `template.ParseFiles(layoutPath, pagePath)` to avoid last-parsed-wins collision.
  Do not revert to a shared parsed set.
- `template.ParseFiles` names templates by base filename only — keep base names unique.

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
Docker Compose stack: portal + NGINX + cloudflared.
- **`docker-compose.yml`** — portal + NGINX + cloudflared services
- **`Dockerfile.portal`** — multi-stage Go build; final image copies binary + `web/`
- **`nginx.conf`** — reverse proxy to `portal:8080` for `soholink.org`
- **`.env`** — `DATABASE_URL`, `SESSION_PRIVATE_KEY`, `ORCHESTRATOR_TOKEN_SECRET`; gitignored
- **Cloudflare Tunnel** — `soholink-prod` (`bb7b7f0d-0d50-4d58-858b-abc52f1d7cd4`)
- **DNS** — CNAME `soholink.org` → tunnel (proxied)

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
