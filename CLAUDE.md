# SoHoLINK v2 — Claude Code Context

## What This Project Is
SoHoLINK is a federated marketplace where participants rent idle hardware
(SOHO servers, mobile GPUs, Smart TVs) to consumers who need compute,
storage, CDN edge caching, or AI inference. NTARI operates the coordination
layer. Participants own and control their hardware.

This is a ground-up rebuild. The old build is preserved on the `legacy-v1`
branch. Do not attempt to continue or fix the old build. Do not reference
old patterns unless explicitly told to salvage a specific component.

## Organization
- **Project:** SoHoLINK
- **Organization:** NTARI (Network Theory Applied Research Institute)
- **Domain:** soholink.org
- **Trust domain:** spiffe://soholink.org
- **Working branch:** v2-rebuild

## Architecture — Five Layers
Consumer Interface  → Marketplace portal, REST API, billing dashboard
Control Plane       → Orchestrator, Scheduler, Marketplace Engine,
Metering Service, Reputation Engine, Dispute Terminal
All hosted by NTARI.
Overlay Network     → WireGuard mesh, NAT traversal, NGINX ingress,
CDN edge cache, geo tag index
Node Runtime        → SoHoLINK Agent, Docker containers, GPU abstraction,
resource monitor, Watchtower auto-update
Physical Hardware   → SOHO servers, Android mobile, Smart TVs, NAS devices

## Technology Stack
| Layer | Technology |
|---|---|
| Language | Go — all services and agent |
| Frontend | Server-rendered HTML/CSS via Go `html/template` — no JS framework, no build step |
| Database | PostgreSQL 16 + TimescaleDB |
| Object storage | MinIO (S3-compatible) |
| Overlay network | WireGuard |
| Ingress | NGINX |
| Identity | SPIFFE/SPIRE — mTLS, short-lived X.509 SVIDs |
| Payments | Stripe Connect (destination charges, split payouts) |
| Monitoring | Prometheus + Grafana |
| Config management | Ansible |
| Container runtime | Docker + Portainer CE |
| Auto-update | Watchtower |
| CI/CD | GitHub Actions |
| Policy engine | OPA (Rego) — reused from v1 |

## Repository Structure (v2 target)
cmd/
orchestrator/     ← Control plane entry point
agent/            ← Node agent daemon (runs on participant hardware)
portal/           ← Web portal server (marketplace + provider UI)
dispute-terminal/ ← NTARI internal dispute management UI
internal/
orchestrator/     ← Orchestrator, Scheduler, geo-aware placement
marketplace/      ← Marketplace Engine, listings, Stripe settlement
agent/            ← Hardware detection, resource profiles, container lifecycle
identity/         ← SPIRE integration, mTLS, SVID management
metering/         ← Signed telemetry, usage reconciliation
reputation/       ← Node scoring, dispute outcomes
storage/          ← MinIO S3 client, bucket management
network/          ← WireGuard bootstrapper, NAT traversal
dispute/          ← Dispute capture, evidence, arbiter controls
payment/          ← Stripe Connect, destination charges, escrow holds
portal/           ← Go html/template handlers, static assets
policy/           ← OPA Rego policies (salvaged from v1 configs/policies/)
p2p/              ← LAN mesh discovery (salvaged from v1)
store/            ← PostgreSQL + TimescaleDB migrations and queries
web/
templates/        ← Go html/template .html files
static/
css/            ← Plain CSS
js/             ← Vanilla JS only — no framework
configs/
policies/         ← OPA Rego (preserved from v1)
default.yaml      ← Service config with env-var overrides for secrets
infra/
ansible/          ← Node provisioning playbooks
docker/           ← Docker Compose for local dev services
docs/
legacy/           ← Archived v1 documentation (do not reference)
ARCHITECTURE.md   ← v1.1 system architecture (source of truth)

## What to Salvage from v1
These specific v1 packages are worth adapting — do not rewrite from scratch:

| v1 location | v2 target | Notes |
|---|---|---|
| `internal/payment/` (Stripe only) | `internal/payment/` | Remove Lightning/LND entirely |
| `configs/policies/` | `internal/policy/` | OPA Rego reusable as-is |
| `internal/p2p/` | `internal/p2p/` | Ed25519 signed multicast UDP |
| `internal/wizard/` (hardware detection) | `internal/agent/` | gopsutil polling loop |
| `internal/blockchain/` | `internal/metering/` | Merkle accounting chain |
| `internal/lbtas/` | `internal/reputation/` | Trust scoring |
| `internal/auth/` (Ed25519) | `internal/identity/` | Alongside SPIRE |

## What to Remove from v1
Do not carry forward:

- `fyne.io/fyne` — all GUI code is gone
- Lightning Network / LND / HTLC — removed entirely
- IPFS / Kubo — replaced by MinIO
- SQLite / `modernc.org/sqlite` — replaced by PostgreSQL
- Flutter — replaced by server-rendered HTML portal
- RADIUS server — not part of v2
- `golang.org/x/mobile` — not needed

## Key Design Decisions
**Payment:** Stripe Connect, destination charges, split payouts.
NTARI collects from consumers and pays out to providers.
Platform fee deducted at settlement. 24-hour payout hold for dispute window.

**Frontend:** Server-rendered HTML via Go `html/template`.
No React, no Vue, no Node.js, no npm, no build step.
Vanilla JS only where strictly necessary (live dashboard updates).
Must work on a 2019 Android phone on a 3G connection.
Must work in Smart TV browsers (Tizen, webOS).

**Hardware detection:** Agent-side only using gopsutil.
Never browser-side. The portal displays what the agent already reported.
Agent polls every 60 seconds and auto-updates the listing on hardware change.

**Resource profiles:** Every provider has a default profile and can create
scheduled overrides. Each profile specifies per-resource toggles and capacity
caps (CPU on/off, GPU %, RAM %, storage GB, bandwidth Mbps).
cgroup v2 enforces caps on all launched containers.

**Identity:** SPIFFE/SPIRE issues short-lived X.509 SVIDs (1hr TTL).
All inter-service connections use mTLS. No node trusts another without
a valid current certificate.

**Geo scheduling:** Every node is geo-tagged at registration.
Consumer workloads may specify country/region constraints.
Scheduler refuses to violate a hard residency requirement.

**Disputes:** NTARI arbitrates via an internal Dispute Terminal.
Signed telemetry is the primary evidence. Arbiter controls
full/partial payment redistribution. Default 50/50 split
if unresolved within 5 business days.

**Workload types:**
- App Hosting (Class A nodes) — Docker + NGINX
- Object Storage (Class A, D) — MinIO S3
- CDN Edge Cache (Class A, C, D) — Varnish/Caddy
- Batch Compute (Class A, B) — Docker CPU/GPU
- AI Inference (Class B, C) — ONNX Runtime / llama.cpp

**Node classes:**
- Class A: SOHO servers (full Docker runtime)
- Class B: Mobile GPU (Android/iOS, idle-only)
- Class C: Smart TV (Tizen/webOS/AndroidTV, sideload)
- Class D: NAS/storage devices

## Local Dev Services
All running in Docker on localhost:

| Service | Port | Credentials |
|---|---|---|
| PostgreSQL 16 + TimescaleDB | 5432 | user: postgres / pw: changeme |
| MinIO S3 API | 9000 | user: admin / pw: changeme |
| MinIO Console | 9001 | user: admin / pw: changeme |
| SPIRE Server | 8081 | trust domain: soholink.org |

Stripe test keys are in environment variables — never hardcode them.
Use `STRIPE_SECRET_KEY` and `STRIPE_PUBLISHABLE_KEY`.

## Stripe Integration
Use the Stripe Connect destination charge model.
Refer to `docs/stripe-integration-prompt.md` for the exact API patterns,
V2 account creation properties, and webhook setup.
Do not use Stripe Products. Use dynamic destination charges based on
metered usage from the Metering Service.

## Coding Conventions
- All errors must be handled explicitly — no blank `_` error discards
- All inter-service calls use mTLS via SPIRE SVIDs
- No secrets in source code or committed config files — use env vars
- Database queries use `pgx` driver directly — no ORM
- HTML templates use Go `html/template` — never `text/template`
- All monetary amounts stored and calculated in cents (int64)
- Telemetry payloads must be cryptographically signed by the agent
- Resource caps enforced via cgroup v2 — never trust container self-reporting

## Current Phase
**Phase 0 — Pre-Flight** (in progress)
Services running. Repo cleaned. This file is the last pre-flight item.
Phase 1 begins next: Control Plane Core.

## Phase 1 Scope (what comes next)
1. Database schema — providers, consumers, nodes, jobs, resource profiles
2. SPIRE identity integration — SVID issuance for all services
3. Orchestrator — job submission, node matching, job token issuance
4. Geo-aware Scheduler — placement with residency constraints
5. WireGuard bootstrapper — overlay network setup
6. Stripe Connect pipeline — provider onboarding, charge, metering, settlement
7. Integration test — 31E node registers, receives job, settles payment

Do not begin Phase 2 (Node Agent) until the Phase 1 gate is confirmed:
end-to-end job submission → execution → Stripe settlement verified.
