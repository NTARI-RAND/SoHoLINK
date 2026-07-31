# SoHoLINK

[![Go Version](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE.txt)
[![CI](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml/badge.svg)](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml)

**The reference coordinator of the SoHoLINK / Cloudy compute substrate.** SoHoLINK
turns the idle compute, storage, and print capacity of participant-owned machines
into a coordinated marketplace: members contribute hardware through a front end and
earn real (fiat) payments; other members buy compute, storage, print, and CDN
capacity on demand.

> **Role, in one line:** SoHoLINK is the **coordinator** — it orchestrates work
> across nodes and settles it. It is **not** the member economy, and it is a
> **replaceable** part. See [`CONFORMANCE.md`](CONFORMANCE.md) for the full
> role declaration in the architecture's own terms.

---

## What SoHoLINK is

SoHoLINK performs **node-side orchestration** for a Janus-Facing Architecture (JFA)
substrate:

- **Node recognition** — nodes advertise what work they will do via signed capability listings.
- **Matching & scheduling** — placement of workloads onto nodes (locality-aware, idle-aware, opt-out-respecting).
- **Employment lifecycle** — offer → accept/decline → run → report → settle, with a dispute window.
- **Fee declarations** — fees exist only as coordinator-signed, legible, contestable messages.
- **Fiat settlement** — payouts to contributors via Stripe Connect.
- **Federation of front ends** — a design target (see *Status* below), not yet built.

The coordinator is a **pluggable, replaceable role.** Any conformant implementation
may replace it, and a front end may run its own coordinator or match directly against
nodes. Nothing in the network is required to route through this particular coordinator —
that non-privilege is what keeps a coordinator from becoming an unremovable hub.

## What SoHoLINK is *not*

- **Not the JFA member economy.** No member-issued credit, no reputation covenant, no
  dialog-sealed record, no member-facing dispute adjudication. Those live in the **front
  end** (Cloudy), by architectural rule.
- **No tokens, no wallets.** Settlement is pure fiat via Stripe Connect, kept strictly
  separate from any front end's member credit.
- **No persons on the wire.** SoHoLINK's counterparties are front ends and nodes; the
  only identity it handles is workload identity (a `NodeID` with a SPIFFE binding).
  Member identity, credit, reputation, and PII are front-end concerns.

> **Transitional surfaces.** The node agent (`internal/agent`, `cmd/agent`), the member
> portal (`internal/portal`, `cmd/portal`, `web/`), and the `participants` table are
> front-end-owned capabilities currently hosted in this repo from SoHoLINK's earlier
> dual-role era. They are kept working and labeled as transitional, pending migration to
> the front end. Do not treat them as the coordinator's long-term role.

---

## Where it sits in the stack

```
Front end (Cloudy)  ──┐
                      ├──►  sohocloud-protocol  (the dependency leaf: coordination only)
Coordinator (this) ──┘
```

- SoHoLINK **consumes [`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)**
  at a published version tag — the thin-waist coordination module it shares with the front end.
- It **serves the protocol's `/v0` wire** alongside its own existing HTTP routes, so a
  conformant front end talks to it over the standard protocol while transitional routes
  keep working.
- It depends on **no** front end and **no** member-economy code. The member economy lives
  above this layer and must never migrate down into it.

---

## JFA invariants this repo holds

| Invariant | How it is met here |
|---|---|
| Persons never appear on the wire | Counterparties are front ends and nodes; wire identity is a SPIFFE `NodeID` |
| Single participant identity | One `participants` table — no provider/consumer split in schema or code |
| Fee legibility | Fees exist only as coordinator-signed `FeeDeclaration` messages |
| Fiat stays fiat; credit stays home | Settlement is pure Stripe Connect; strictly separate from member credit |
| No information asymmetry | Pricing, metering, and earnings are visible to participants |
| Governance surface separated | Admin/governance runs on a separate, local-only interface — never on the public site, never behind role flags |
| Provenance inbound = outbound | AGPL-3.0; DCO sign-off; no CLA |

---

## Technology stack

| Layer | Technology |
|---|---|
| Language | Go 1.24+ |
| Web | Server-rendered HTML via Go `html/template` — no JS framework, no build step (must work on a 2019 Android phone on 3G and in Smart TV browsers) |
| Database | PostgreSQL 16 + TimescaleDB, `pgx/v5` (no ORM) |
| Object storage | MinIO (S3-compatible) |
| Identity | SPIFFE/SPIRE — mTLS, short-lived X.509 SVIDs |
| Payments | Stripe Connect (fiat only) |
| Deploy | Docker Compose behind a Cloudflare tunnel |
| CI | GitHub Actions |

---

## Repository structure

```
cmd/
  orchestrator/   Coordinator service: matching, employment lifecycle, /v0 wire, settlement hooks
  portal/         Member portal (transitional, front-end-owned)
  agent/          Node agent (transitional, front-end-owned)
  seed/           Dev/load-test seeder — never point at production
internal/
  api/            Control-plane HTTP API (+ protocol /v0 handlers via protocoladapter)
  orchestrator/   Node registry, matching, placement scoring, job token issuance
  scheduler/      Scoring-based placement (class, freshness, capacity, locality, idle)
  protocoladapter/ Implements coordinator.Coordinator; mounts the /v0 wire
  payment/        Stripe Connect: onboarding, charge, payout, webhook
  identity/       SPIFFE integration, mTLS config, SPIFFE-binding middleware
  store/          PostgreSQL pool + migrations
  agent/ portal/  Transitional front-end-owned capabilities (see note above)
web/              Server-rendered templates + CSS
deploy/           Compose stack, SPIRE config, backup + deploy scripts
docs/             Architecture, operations, and session records
```

---

## Build

Prerequisites: Go 1.24+, a PostgreSQL 16 + TimescaleDB instance, and Docker (for the
full stack).

```bash
git clone https://github.com/NTARI-RAND/SoHoLINK
cd SoHoLINK

# Build the coordinator and portal binaries
go build ./cmd/orchestrator
go build ./cmd/portal

# Run tests (unit)
go test ./...

# Run integration tests against an isolated test database
#   (DB name MUST contain "test" — fixtures refuse to touch any other database)
TEST_DATABASE_URL="postgres://postgres:changeme@localhost:5432/soholink_test?sslmode=disable" \
  go test -tags integration ./...
```

Database migrations are applied at service startup (golang-migrate, idempotent). The
full production stack — Postgres, SPIRE, orchestrator, portal, ingress — runs via Docker
Compose under `deploy/`.

---

## Status (honest)

- **Built & running:** node recognition, matching/scheduling, the employment lifecycle,
  fiat settlement (Stripe Connect), coordinator-signed fee declarations, the `/v0`
  coordination wire, operator-or-SPIFFE authentication, and SPIFFE workload identity live
  in production at **[soholink.org](https://soholink.org)** (trust domain
  `spiffe://soholink.org`).
- **Single-coordinator deployment.** Cross-coordinator federation and witnessed
  checkpoints are a **design target, not built** — the architecture's open problem 7
  (sovereign compute buys *mechanical*, not *economic*, exit) applies in full and is named,
  not solved.
- **Operator enrollment** (front-end-as-operator) is verified in tests but has not
  completed end-to-end in production, pending the two-factor mail transport.
- **No live pilot nodes yet.** The first pilot targets a dense residential community
  adjacent to NTARI's headquarters.
- **The member economy is deliberately elsewhere** — credit, covenant, and record live in
  the front end (Cloudy), one layer up.

---

## Documentation

| Document | Description |
|---|---|
| [`CONFORMANCE.md`](CONFORMANCE.md) | Role declaration in JFA terms; invariant-to-mechanism bindings; named stand-ins |
| [`CLAUDE.md`](CLAUDE.md) | Engineering context, conventions, and the working history |
| `docs/` | Architecture, operations runbooks, and per-session records |

The shared coordination protocol is specified in the
[`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol) repository
(`SPEC.md`).

---

## License

AGPL-3.0-or-later — see [`LICENSE.txt`](LICENSE.txt). The AGPL keeps the whole substrate a
permanent commons: read it, reimplement it, fork it, leave.

*Network Theory Applied Research Institute, Inc. — 501(c)(3) — EIN 92-3047136 — info@ntari.org*
