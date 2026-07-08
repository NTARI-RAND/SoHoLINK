# Operations Guide — SoHoLINK v2 (Coordinator)

This document describes how to operate the SoHoLINK v2 production stack as it
actually runs today. SoHoLINK is the COORDINATOR of the Substrate compute
economy: node recognition via capability listings, matching and scheduling,
the employment lifecycle, fee declarations, fiat settlement, dispute handling,
and federation of frontends. It never touches member hardware.

Honest transitional note: this stack also runs the member portal and serves
the node-agent installer. Those are Cloudy-owned capabilities (member portal,
node agent, installer) transitionally hosted in the coordinator repo pending
migration to the Cloudy frontend. They are documented here because an operator
of this stack runs them today — not because they are the coordinator's
long-term role. See CLAUDE.md "Architectural Philosophy" and the
MEMBER/PARTICIPANT/NODE glossary for the vocabulary used below.

## The Stack

Production is a single-host Docker Compose deployment (`docker-compose.yml`,
8 services), fronted by a Cloudflare Tunnel. `.env` (gitignored; template at
`.env.example`) supplies secrets to every service via `env_file`.

| Service | Image / build | Role | Exposure |
|---|---|---|---|
| `postgres` | `timescale/timescaledb:latest-pg16` | System of record. WAL archiving on (`archive_command` copies segments to `D:/SoHoLINK-backups/wal`) | `127.0.0.1:5432` only |
| `pg-backup` | `./deploy/pg-backup` (Alpine + postgresql16-client) | Daily `pg_dump` to `D:/SoHoLINK-backups/`, 90-day retention | none |
| `spire-server` | `ghcr.io/spiffe/spire-server:1.9.6` | Trust domain `spiffe://soholink.org`; issues SVIDs | host port `8081` (not externally routed — see blockers) |
| `spire-agent` | `ghcr.io/spiffe/spire-agent:1.9.6` | Workload API socket for the orchestrator; `pid: "host"`, `join_token` attestation | none |
| `orchestrator` | `Dockerfile.orchestrator` | The coordinator binary: public API `:8082`, Docker-internal submit listener `:8083`, metrics | `api.soholink.org` via tunnel |
| `portal` | `Dockerfile.portal` | Member portal (transitional, Cloudy-owned): HTML UI, Stripe, payout releaser, uptime scorer | `soholink.org` via nginx + tunnel |
| `nginx` | `nginx:alpine` | Reverse proxy to `portal:8080` | via tunnel |
| `cloudflared` | `cloudflare/cloudflared` | Tunnel `soholink-prod` (`bb7b7f0d-...`) | egress only |

Startup ordering is encoded in `depends_on`: orchestrator waits for a healthy
`spire-agent`; portal waits for a healthy orchestrator. `D:/` must be attached
before `docker compose up` (pg-backup and WAL bind mounts).

```
docker compose up -d          # bring up the stack
docker compose ps             # container health at a glance
docker compose logs -f orchestrator
```

Deploys go through `deploy/redeploy.sh`, which gates on GitHub CI check-runs
for the exact HEAD SHA before rebuilding the orchestrator and portal images.
Do not bypass the gate.

## Environment requirements per binary

All binaries fail fast (`log.Fatalf`) on a missing required variable. Source
of truth: each `cmd/<name>/main.go`.

### `cmd/orchestrator` (the coordinator)

| Variable | Required | Notes |
|---|---|---|
| `DATABASE_URL` | yes | pgx connection string |
| `ORCHESTRATOR_TOKEN_SECRET` | yes | hex; HMAC secret for job tokens |
| `API_ADDR` | yes | public API listener (prod `:8082`) |
| `METRICS_ADDR` | yes | Prometheus listener |
| `INTERNAL_ADDR` | yes | Docker-internal submit listener (prod `:8083`, set in compose) |
| `SPIFFE_ENDPOINT_SOCKET` | yes | SPIRE Workload API (`unix:///run/spire/sockets/agent.sock`) |
| `ALLOWLIST_PATH` | no | defaults to `/etc/soholink/allowlist.json` |
| `PRINT_CONFIRMATION_ENABLED` | no | bool; keep off in production until B4 is fully deployed |

If the SPIRE Workload API is unreachable at startup (5-second bounded attempt),
the orchestrator continues in **degraded mode**: plain HTTP, SPIFFE-protected
routes return 503, `/health` reports `"identity":"unavailable"`. Healthy state
is `{"identity":"ready","status":"ok"}`.

### `cmd/portal` (member portal — transitional, Cloudy-owned)

| Variable | Required | Notes |
|---|---|---|
| `DATABASE_URL` | yes | |
| `SESSION_PRIVATE_KEY` | yes | Ed25519, 128 hex chars; sign-verify roundtrip probed at startup |
| `STRIPE_SECRET_KEY` | yes | fiat settlement — never conflate with member credit |
| `STRIPE_WEBHOOK_SECRET` | yes | |
| `PORTAL_ADDR` | yes | prod `:8080` |
| `PORTAL_BASE_URL` | yes | `https://soholink.org` |
| `PORTAL_TEMPLATES_DIR` | yes | `/app/web/templates` in the image |
| `METRICS_ADDR` | yes | |
| `ORCHESTRATOR_INTERNAL_URL` | yes | `http://orchestrator:8083` (set in compose) |

The portal also runs the uptime scorer and the payout releaser as background
loops — settlement mechanics that belong to the coordinator role even though
they currently live in the portal binary.

### `cmd/agent` (node agent — Cloudy-owned, transitionally hosted here)

| Variable | Required | Notes |
|---|---|---|
| `AGENT_CONTROL_PLANE_ADDR` | yes | coordinator API address |
| `SPIFFE_ENDPOINT_SOCKET` | yes | node-local SPIRE agent socket |
| `AGENT_REGISTER_TOKEN`, `AGENT_COUNTRY_CODE` | first-run claim flow | single-use portal token |
| `AGENT_REGION` | no | |
| `AGENT_PROVIDER_ID`, `AGENT_NODE_CLASS`, `AGENT_TOKEN_SECRET` | legacy/programmatic registration path | normal installs use the claim flow + `agent.conf` |

### `cmd/seed` (dev/load-test only)

Reads `DATABASE_URL`, runs migrations, then inserts 10 seed providers (with
Class A nodes and resource profiles) and 10 seed consumers with bcrypt password
`changeme`. **Never point it at production.**

## Migrations

Migrations (`internal/store/migrations/`, currently 001–020) run automatically
at orchestrator, portal, and seed startup via `store.RunMigrations`.
golang-migrate is idempotent — safe to run repeatedly.

To apply them explicitly against a test database:

```
TEST_DATABASE_URL="postgres://postgres:changeme@localhost:5432/soholink_test?sslmode=disable" \
  go test -tags integration -v -run TestPhase1EndToEnd ./test/integration/
```

Integration tests read `TEST_DATABASE_URL`, never `DATABASE_URL` — this
separation is load-bearing (see the Dev XXIV data-loss incident record in
CLAUDE.md). Destructive fixtures refuse to run unless the connected database
name contains `"test"`.

## Allowlist signing

The signed allowlist is the root of trust for what container images agents may
run; agents fail closed without a verifiable one. The full operator runbook —
keypair bootstrap, signing, deployment, rotation, loss recovery — is
`docs/operations/allowlist-signing.md`. The orchestrator serves the signed file
at `GET /allowlist` from `ALLOWLIST_PATH` (compose mounts `./deploy/allowlist`
at `/etc/soholink`).

## Health and monitoring

| Surface | Where | Notes |
|---|---|---|
| `GET /health` | orchestrator public API (no SPIFFE required) | 200 with `{"identity":"ready"\|"unavailable","status":"ok"}` — check `identity`, not just the status code |
| `GET /metrics` | orchestrator and portal, each on its `METRICS_ADDR` | Prometheus format |
| Compose healthchecks | `docker compose ps` | spire-server, spire-agent, orchestrator, portal all have healthchecks |

External check: `curl https://api.soholink.org/health`. A 200 with
`"identity":"unavailable"` means the stack is up but SPIFFE-protected routes
are returning 503 — usually the SPIRE agent (see blockers below).

## Known operational blockers

1. **SPIRE agent join-token single-use behavior (TODO 36).** `join_token`
   attestation is one-time: the token is consumed at first attestation and the
   SVID is cached. If the SVID expires and the agent restarts, re-attestation
   with the consumed token crash-loops and the orchestrator drops to degraded
   mode. Recovery: `spire-server token generate` → update
   `SPIRE_AGENT_JOIN_TOKEN` in `.env` →
   `docker compose up -d --force-recreate spire-agent` → re-run
   `bash deploy/register-entries.sh` (the orchestrator workload entry's
   parentID encodes the token). Durable fix is a persistent attestation method
   (`x509pop`); the rotation procedure is the interim mitigation.
2. **External SPIRE reachability (TODO 37) — blocks all node bring-up.**
   `spire.soholink.org` is NXDOMAIN and port 8081 is not externally routed, so
   a member machine's bundled SPIRE agent cannot attest;
   `cmd/agent`'s `waitForSPIRE` fatals after 90s. Until the control plane is
   exposed (decision: Cloudflare tunnel gRPC/TCP route for now), no external
   node can join, which also gates the signed-service-start verification
   (TODO 19) and the Shenandoah pilot.

## Backups

Daily logical `pg_dump` (pg-backup sidecar) plus WAL segment archiving, both
under `D:/SoHoLINK-backups/`. WAL alone is not PITR — a binary base backup is
still an open TODO. Architecture, restore procedures, and honest limitations:
`docs/backups.md`. Backups are currently host-local only (off-host replication
is TODO 29).

## Legacy v1 (fedaaa)

The v1 system this document previously described — the `fedaaa` binary, its
RADIUS/DID authentication, SQLite store, Merkle-batched accounting logs, and
OPA policy engine — is **retired**. Its documentation has been removed, and
the v1 binaries still present at the repo root are deliberately unsigned. Do
not operate, extend, or document them. The old build lives on the `legacy-v1`
branch for archaeology only.
