# SoHoLINK Dev XXI Handoff

**Branch:** master · **HEAD:** `101b912` · **Build:** clean · **CI:** green (verified in-loop via `gh run list`) · **Date:** 2026-05-17
**Previous handoff:** Dev XX (`docs/handoffs/dev-xx.md`)

---

## What was accomplished this session

Seven commits across two sub-sessions. The first sub-session completed B4 commits
4 and 5, the agent-side printer wiring, and the TODO 23 integration test — all
the active B4 work except the auto-decline sweeper. The second sub-session fixed a
CI regression discovered from a report (CI #68–#71 failing), installed `gh` CLI
for in-loop CI verification, and wrote the fix-forward CLAUDE.md batch (TODOs 24,
25, 26 + Critical API Note).

No operational incidents. Production state unchanged from Dev XX (`0115d41`);
deploy hold in place until B4 commit 6 (auto-decline sweeper) is ready.

---

## Commits this session

| Hash | Message |
|---|---|
| `93d5381` | feat(portal): B4 commit 4 — print job confirmation page |
| `0431188` | feat(agent): wire printer_id through to ContainerSpec.ConnectionPath at job-poll |
| `ad2dda2` | feat(orchestrator): B4 commit 5 — decline reroute with worker loop |
| `dc1de1d` | test(orchestrator): B4 integration coverage — SubmitJob + RerouteDeclinedJob (TODO 23) |
| `d3bc3af` | docs(claude.md): Dev XXI session-close batch |
| `27157dc` | fix(test): update phase1 integration test for new orchestrator.New signature |
| `101b912` | docs(claude.md): fix-forward — TODO 24/25/26, Critical API Note, Dev XXI extension |

---

## B4 commit 4 — portal print job confirmation page (`93d5381`)

New routes in `internal/portal/server.go`:

- `GET /provider/job/{id}/confirm` — `handleJobConfirm`: verifies contributor
  ownership via `JOIN nodes n ON n.id = j.node_id WHERE n.participant_id = $2`,
  scans job spec fields + spec_hash (hex-encoded), renders
  `contributor_job_confirm.html`.
- `POST /provider/job/{id}/confirm` — `handleJobConfirmAction`: re-verifies
  ownership; `action=confirm` UPDATEs to `scheduled` + sets `confirmed_at`;
  `action=decline` runs a transaction: UPDATE returning `node_id`, INSERT into
  `job_node_declines`, commit. Race detection: `RowsAffected() == 0` after the
  confirm UPDATE redirects to GET showing current state.

`JobConfirmData` struct mirrors `SubmitJobRequest` field types exactly (`CPUCores
int`, `RAMMB int`, `StorageGB int`). New template `web/templates/
contributor_job_confirm.html` follows the `{{define "content"}}` /
`{{template "layout" .}}` pattern with status banner, stat grid, spec fingerprint
(`<code>{{.SpecHashHex}}</code>`), and two forms (accept / decline). Added
imports: `"errors"`, `"github.com/jackc/pgx/v5"` (needed for `pgx.ErrNoRows`
on the race-detection scan).

### Key audit findings caught before write

- `confirmed` is not a valid `job_status` enum value — accept path must go to
  `scheduled`, not `confirmed`. Caught at proposal stage.
- Type drift: initial proposal used `int32`/`int64`/`RAMMb`. Fixed to match
  `SubmitJobRequest`: `CPUCores int`, `RAMMB int`, `StorageGB int`.
- `tag interface{ RowsAffected() int64 }` wrapper on decline: over-engineering.
  Fixed to `var query string` + infer `pgconn.CommandTag` from `:=`.

---

## Agent wiring commit (`0431188`) — closes TODO 9

Two ends wired:

**Orchestrator side** (`internal/api/nodes.go`): `handleGetJobs` SELECT extended
to include `COALESCE(printer_id, '')`. `jobEntry` struct gains `PrinterID string
json:"printer_id,omitempty"`. `omitempty` tag is backward compat — old agents
on new orchestrators ignore the field; new agents on old orchestrators get empty
string and behave identically to today.

**Agent side**: `JobAssignment` struct in `internal/agent/heartbeat.go` gains
`PrinterID string json:"printer_id,omitempty"`. New `ResolveConnectionPath(printerID
string, printers []PrinterInfo) (string, error)` pure function in
`internal/agent/printers.go` — returns `""` for empty `printerID` (non-print jobs),
iterates local `PrinterInfo` slice for match, errors with diagnostic message on
miss. 3 unit tests: empty printerID, found, not found.

`runJob` in `cmd/agent/main.go` reordered — both validations (`job.Image == ""`
and `ResolveConnectionPath`) moved **before** `done := make(chan struct{})` / the
telemetry goroutine start. This fixes a pre-existing goroutine leak where early
returns after the goroutine start would leak the telemetry goroutine.

Deployment order if deploying separately: agent first, then orchestrator (agent
tolerates old orchestrator returning no `printer_id`; reverse order would surface
the new field before the agent knows what to do with it).

---

## B4 commit 5 — decline reroute (`ad2dda2`)

**Migration 017** (`internal/store/migrations/017_job_node_declines.{up,down}.sql`):
`job_node_declines` table with composite PK `(job_id, node_id)`, both FKs `ON
DELETE CASCADE`. Used by `RerouteDeclinedJob` to populate `ExcludedNodeIDs`.

**Registry**: `MatchRequest` gains `ExcludedNodeIDs []string`. `FindMatch` builds
an exclusion map (`map[string]bool`) before the candidate loop — O(1) lookup per
node.

**Orchestrator**: `RerouteDeclinedJob(ctx, jobID)` reads job spec, reads
`job_node_declines` for all prior declines, calls `FindMatch` with exclusions, then
either fails the job (`UPDATE ... WHERE status = 'declined'` guard, 0-rows-affected
on concurrent race) or re-dispatches to `awaiting_confirmation` on the new node.
No single transaction wraps the operation — the `AND status = 'declined'` guard on
the final UPDATE makes the outcome safe: concurrent workers produce a no-op on the
losing write. `StartDeclineRerouteLoop` 30s ticker goroutine mirrors
`StartEvictionLoop`. Also wired `StartEvictionLoop` in `cmd/orchestrator/main.go` —
it was defined in Dev XV but never called from main.

### Audit findings caught before write

- FK on `node_id` in migration 017 was initially missing. Added: consistent with
  `node_printers` pattern; orphan records on deleted nodes are useless.
- `updated_at = NOW()` missing from failed-path UPDATE. Added to match every other
  status transition in the codebase.
- No LIMIT on `rerouteDeclined` query. Added `LIMIT 100` to guard runaway iteration.
- Transaction choice documented in source comment on `RerouteDeclinedJob`.
- Portal decline branch restructured: confirm stays `Pool.Exec`; decline becomes a
  transaction with `RETURNING node_id` + `INSERT INTO job_node_declines`.

---

## TODO 23 integration test (`dc1de1d`)

New file: `internal/orchestrator/orchestrator_integration_test.go`

- `//go:build integration`, `package orchestrator_test`
- `setupOrchFixture` helper: connect, migrate, TRUNCATE with CASCADE (propagates
  to `node_printers`, `job_node_declines`), seed provider + consumer + node, register
  in memory, construct 7-arg `orchestrator.New`. Not safe for parallel use.
- `writeOrchAllowlist` helper: temp allowlist with compute + print_traditional entries.

Six tests, all green against local Postgres:

| Test | What it exercises |
|---|---|
| `FlagOn_PrinterResolves` | `awaiting_confirmation` written; `printer_id`, 32-byte `spec_hash`, future `confirmation_deadline` all verified in DB |
| `FlagOn_NoEnabledPrinter` | Registry/DB drift: `HasEnabledPrinter=true` in registry, no `node_printers` row → "registry/DB drift" error |
| `FlagOn_ComputeJob` | Compute job → `scheduled` even with flag on |
| `FlagOff_PrintJob` | Print job → `scheduled` with flag off; `UpdateOptOut(HasEnabledPrinter: true)` needed or `FindMatch` filters the node |
| `RerouteDeclinedJob_Success` | Declined job re-dispatches to node B; `awaiting_confirmation` with `node_id = nodeBID` |
| `RerouteDeclinedJob_NoCandidates` | Only node declined → `failed` |

**Bugs found during run** (not compile-time): `FlagOff_PrintJob` was missing
`UpdateOptOut(HasEnabledPrinter: true)` — `FindMatch`'s opt-out filter applies
regardless of the dispatcher flag; would have failed at `FindMatch` before any DB
assertion. Fixed before first commit. `node_printers` INSERTs were missing
`printer_name TEXT NOT NULL` — caught on first run against Postgres, fixed, all six
green on the second run.

---

## CI regression fix (`27157dc`)

**Root cause**: B4 commit 3 (`0115d41`) extended `orchestrator.New` to 7 args.
Audit grep scoped to `cmd/` and `internal/` missed `test/integration/
phase1_test.go:108` — a peer directory outside both scopes. The stale 5-arg call
compiled fine without the `integration` build tag, so local builds passed on every
B4 commit. CI runs with the tag set; CI #68–#71 all failed with
`not enough arguments in call to orchestrator.New`.

**Fix**: one-line change to `test/integration/phase1_test.go:108` — `false,
4*time.Hour` appended to match the 7-arg signature. `go build -tags integration
./...` clean. CI #72 green.

**Rules added to CLAUDE.md Critical API Notes**: repo-wide grep required for
exported signature changes (`grep -rn "Func(" .`, not scoped to `cmd/`/
`internal/`); `go build -tags integration ./...` required before commit.

---

## TODOs added this session

- **TODO 24** — zombie `running` rows: `handleGetJobs` flips `scheduled` →
  `running` optimistically on poll before the agent confirms container start. Printer
  vanished / image not in allowlist / container start failure leaves the job stuck
  with no recovery path. Fix: start-confirmation endpoint or running-timeout reaper.
- **TODO 25** — portal orphaned registry: `cmd/portal/main.go` constructs a
  `NodeRegistry` that never receives heartbeats. Consumer-side `POST /consumer/job`
  always fails `FindMatch`. Latent — no production traffic yet. Fix options are all
  architectural (expose orchestrator submission endpoint, merge processes, or DB-backed
  registry sync).
- **TODO 26** — integration tag workflow gap, RESOLVED `27157dc`. Rule now in
  Critical API Notes.

---

## TODOs resolved this session

- **TODO 9** — `DeviceUSBPrinter` device-mapping fully closed (`0431188`). Both
  halves now wired: `ContainerSpec.ConnectionPath` in commit 1, agent-side
  `ResolveConnectionPath` + poll surfacing in the agent wiring commit.
- **TODO 23** — `SubmitJob` DB-path integration test gap closed (`dc1de1d`). Six
  tests green.
- **TODO 26** — integration tag workflow gap, RESOLVED `27157dc`.

---

## `gh` CLI installed

`gh` was not installed at session open. Installed via `winget install --id
GitHub.cli` (v2.92.0), authenticated via `gh auth login` (browser flow),
`C:\Program Files\GitHub CLI` already present in Windows system PATH. New shells
find `gh` directly. In this session: `gh` called via full path (`& "C:\Program
Files\GitHub CLI\gh.exe" ...`); future sessions can call `gh` directly from a
fresh shell.

Proof-of-concept run list confirmed the failure/fix arc:
- CI #68–#71: failure (stale `orchestrator.New` call)
- CI #72+: success

---

## Production state

- Unchanged from Dev XX — still at `0115d41`.
- **Do not deploy** until B4 commit 6 (auto-decline sweeper) is reviewed and
  committed. Without the sweeper, jobs that hit `confirmation_deadline` with no
  contributor decision stay in `awaiting_confirmation` indefinitely.
- Migration 017 will apply at next orchestrator restart (no-op until code that
  writes to `job_node_declines` is live, which it is in `ad2dda2`).

---

## Outstanding work, by priority

**1. B4 commit 6 — auto-decline sweeper.** Background goroutine: query
`WHERE status = 'awaiting_confirmation' AND confirmation_deadline < NOW() LIMIT 100`,
flip to `declined`. Mirror `StartDeclineRerouteLoop` pattern (30s ticker, explicit
rows.Close before iteration). `idx_jobs_confirmation_deadline` partial index
already in place (migration 015). After this commit, the full B4 print
confirmation lifecycle is complete and production deploy can proceed.

**2. TODO 25 — portal orphaned registry.** Needs a design decision before code —
three fix paths identified (orchestrator HTTP submission endpoint, process merge,
DB-backed registry sync). Bring to Claude Chat for architecture review.

**3. TODO 24 — zombie running rows.** `handleGetJobs` flips status before the
agent confirms container start. Fix in B5 scope (long-running job lifecycle).
Start-confirmation endpoint is the cleaner fix.

**4. B5 — long-running job lifecycle.** TODOs 5 and 6 (completion endpoint body +
exit-code conditioned metering) are the immediate carry-forwards. Zombie running
rows (TODO 24) resolves naturally here.

**5. TODO 22 — printer-type discrimination.** Migration adding `printer_type` to
`node_printers`, filter in printer-resolve query. B8 territory unless pilot
surfaces the ambiguity earlier.

---

## Opening reads for next session

```powershell
cd "C:/Users/<user>/Documents/SoHoLINK"
git log --oneline -5
gh run list -R NTARI-RAND/SoHoLINK --limit 3
go build ./cmd/...
```

If picking up **B4 commit 6** (auto-decline sweeper):

```bash
grep -n "StartDeclineRerouteLoop\|rerouteDeclined\|StartEvictionLoop" internal/orchestrator/orchestrator.go
grep -n "idx_jobs_confirmation_deadline\|confirmation_deadline" internal/store/migrations/015_print_job_confirmation.up.sql
```

If picking up **TODO 25 design** (portal orphaned registry):

```bash
grep -n "NewNodeRegistry\|orchestrator.New\|SubmitJob" cmd/portal/main.go
sed -n '95,115p' cmd/portal/main.go
grep -n "POST.*consumer/job\|handleSubmitJob" internal/portal/server.go
```

---

## Secrets

No rotations this session. SPIRE join token from Dev XIX current in `.env`.
Dev allowlist keys unchanged. Production `SESSION_PRIVATE_KEY` unchanged.

---

*End of Dev XXI Handoff. Master at `101b912`. Seven commits: B4 commits 4 + 5,
agent wiring, TODO 23 integration test, CI regression fix, two CLAUDE.md batches.
Next session: B4 commit 6 (auto-decline sweeper) to close out the print
confirmation lifecycle and unblock production deploy.*
