# SoHoLINK — Coordinator Operator Guide

*Version: 2.0 | Supersedes the v1 (2026-03-06) guide, which documented the retired
v1 node software.*
*Reference operator: Network Theory Applied Research Institute (NTARI)*

This guide defines the role of a SoHoLINK **coordinator operator** under the
resolved Substrate architecture (NTARI decision, 2026-07-04): what you run,
what you own, what you explicitly do not own, and how frontends connect to
you. Day-to-day mechanics (compose stack, env vars, migrations, health,
backups) live in `docs/OPERATIONS.md`; this document is about the boundaries
of the role. Vocabulary — MEMBER, PARTICIPANT, NODE — is defined in the
glossary in CLAUDE.md and used strictly here.

---

## The role in one paragraph

A coordinator operator runs the orchestration layer of a coordinated compute
economy. Nodes — machines contributed by members of a frontend platform —
present capability listings; the coordinator recognizes them, matches jobs to
them, runs the employment lifecycle, declares its fees, settles fiat, and
arbitrates disputes. The coordinator's counterparties are **frontends and
nodes**, not persons: persons never appear on the coordination wire
(`sohocloud-protocol` invariant), because a coordinator that models persons
becomes a hub every member must trust with their identity — exactly the
enclosure the thin-waist design exists to prevent.

## What you run and own

| Duty | Status | Notes |
|---|---|---|
| **Orchestrator** | built, in production | Node recognition via listings, matching/scheduling, employment lifecycle, fee declarations. The `cmd/orchestrator` binary and its SPIRE, Postgres, and allowlist dependencies. |
| **Fiat settlement** | built (Stripe Connect), counterparty question open | Charging job submitters, releasing payouts after the dispute window. Fiat-side only — it must never be conflated with member credit, which is Cloudy's economy layer. The settlement counterparty under the new spec is a **named open question**: does the coordinator settle directly with the member's payout identity (presented via the frontend), or with the frontend, which then settles its members through the member economy? Do not resolve this by accident in code or docs. |
| **Dispute handling** | built (staff dispute queue) | Signed telemetry is primary evidence; default 50/50 split if unresolved after 5 business days. |
| **Federation of frontends** | design target | Enrolling frontends as operators and, later, syncing with peer coordinators. See "How frontends connect" below. |
| **Allowlist signing authority** | built — held by the coordinator operator *for now* | The Ed25519 keypair that signs the container-image allowlist every agent verifies. Runbook: `docs/operations/allowlist-signing.md`. "For now" because the agent is a Cloudy-owned capability; where signing authority lands after the frontend migration is part of that migration's design, not something this guide predetermines. |

## What you do NOT own

These boundaries are the point of the architecture, not incidental gaps:

- **Member hardware.** Never. Members own and control their machines; the
  agent enforces opt-out and the allowlist locally, and a coordinator that
  mislabels a job's workload type cannot route past a member's opt-out — the
  agent ignores the wire claim. The coordinator has no login, no shell, no
  management channel to any node.
- **Member PII.** Member identity (platform-scoped MemberID), credit, LBTAS
  standing, sealed records, and erasable member-local PII are membership
  facts, owned by the member's frontend. Long-term the coordinator models no
  persons at all. Transitional honesty: today this repo's `participants`
  table and portal accounts DO hold person-records — a leftover of SoHoLINK's
  dual frontend+coordinator era, documented as Cloudy-owned surfaces
  transitionally hosted here. Treat that data as held in trust for the
  frontend, not as a coordinator asset.
- **The member portal, long-term.** Register/login/dashboard/job
  submission/opt-out UI is frontend territory. You run it today (see next
  section); you do not get to keep it.

## How frontends connect

**Today (transitional, built):** there is no separate frontend in production.
The member-facing surface is this repo's own portal (`cmd/portal`, `web/`) —
the surface formerly called the "participant portal," henceforth Cloudy's
MEMBER PORTAL — running inside the coordinator's compose stack and calling the
orchestrator over a Docker-internal submit endpoint (`:8083`). The node agent
and MSI installer ship from this repo too. All of it keeps working, and all of
it is labeled: Cloudy-owned capability, currently hosted in the coordinator
repo pending migration. Do not describe hosting these as the coordinator's
long-term role, and do not pretend the migration has happened.

**Design target (not yet implemented): frontend-as-operator enrollment,**
modeled on the Agrinet Phase 5 operator scheme. The frontend enrolls as an
OPERATOR with the coordinator — an `operators` + `operator_keys` registry —
and holds a rotating Ed25519 key set: 7 keys, each transmission signed with 2,
canonical message `v1:operator_id:ts:nonce:seq:idx0:idx1`, replay bounded by a
5-minute timestamp window plus a nonce cache. This is how Cloudy will
authenticate to SoHoLINK. On the coordination wire itself, the frontend speaks
the protocol's node-side surface
(SubmitListing/Heartbeat/PollJobs/Decline/ReportJob/Fees) on behalf of member
machines. Workload identity is separate and unchanged: each node holds a
SPIFFE SVID under `/node/<id>`, authorized coordinator-side exactly per the
protocol SPEC — machine identity, already built and enforced.

**Design target, further out: coordinator-to-coordinator federation,** modeled
on Agrinet's federation layer with its gaps named rather than inherited:
registry-enrolled peers, authenticated export/import sync with incremental
`since` cursors (interim auth: shared secret; target: operator-signed
transmissions), last-write-wins as the acknowledged interim conflict stance,
and witnessed checkpoints (the record/anchor Certificate-Transparency model)
as the target for cross-operator non-equivocation. Anti-patterns you must not
adopt from the reference: a static never-rotated shared key, no TLS/cert
verification between peers, silent last-write-wins overwrites with no audit
trail, and provenance fields dropped on import.

## Where member-facing duties will live

**Cloudy.** Cloudy owns the member's whole world: the JFA member economy
(member-issued credit, LBTAS reputation, dialog-sealed record — built as
libraries in the Cloudy repo), the node agent (hardware detection, resource
profiles, capability listings, heartbeat, job executor, local
opt-out/allowlist enforcement, telemetry, the installer, eventually the mobile
agent), and the member portal. Cloudy currently has no ingress or persistence;
its member portal and agent are its next build milestones. Until they exist,
the coordinator operator carries those duties transitionally — which is a fact
to state plainly, not a precedent to build on.

## Operational entry points

- Stack, env vars, migrations, health surfaces, blockers: `docs/OPERATIONS.md`
- Allowlist keypair bootstrap / signing / rotation: `docs/operations/allowlist-signing.md`
- Backup architecture and restore: `docs/backups.md`
- Deploys: `deploy/redeploy.sh` (CI-gated; do not bypass)

---

*The v1 operator guide described the retired fedaaa-era node software (DID
auth, SQLite, OPA policies, Lightning payments). That system is retired, its
docs removed, and its root-directory binaries deliberately unsigned.*
