# SoHoLINK Dev XX Handoff

**Branch:** master · **HEAD:** `d62cc29` · **Build:** clean · **CI:** not verified at session close (`gh` unavailable in bash PATH; confirm `d62cc29` green in GitHub Actions UI before treating as clean) · **Date:** 2026-05-16
**Previous handoff:** Dev XIX (note: `docs/handoffs/dev-xix.md` does not exist — Dev XIX produced a thorough session summary in Claude Chat but the file was never written to disk. CLAUDE.md now has the Dev XIX checkpoint as the authoritative record.)

---

## What was accomplished this session

Two commits, both clean: **B4 commit 3** (`0115d41`) — the print confirmation
dispatcher gate, the original carry-forward priority from Dev XVIII → XIX → XX;
and **CLAUDE.md Dev XX checkpoint** (`d62cc29`) — backfilling the missing Dev
XIX checkpoint, recording migration 016, marking TODO 21 RESOLVED, adding
TODOs 22 and 23, and updating the workload type vocabulary to reflect print
workload types entering the marketplace enum.

No operational incidents this session. State on entry reconciled cleanly
against Dev XIX (HEAD matched, all seven containers healthy, build clean) and
stayed clean throughout.

---

## B4 commit 3 — print confirmation dispatcher gate (`0115d41`)

The dispatcher gate that has been carry-forward priority for three sessions.
With `PRINT_CONFIRMATION_ENABLED` unset (production default), `SubmitJob`
writes `status='scheduled'` for every workload type — behavior unchanged.
With the flag set and the workload type in `{print_traditional, print_3d}`,
`SubmitJob` writes `status='awaiting_confirmation'` with three new columns
populated:

- `printer_id` — resolved from `node_printers` via lowest `printer_id`
  lexicographically among enabled printers on the matched node.
- `spec_hash` — SHA-256 over a fixed-field struct including container image,
  workload type, resource requirements, and country constraint. `ConsumerID`
  excluded as orchestrator-internal. Field declaration order in
  `canonicalJobSpecHash` is load-bearing (Go's `encoding/json` is deterministic
  per declaration order; reordering silently changes all hashes).
- `confirmation_deadline` — `now + 4h`, hardcoded for now.

Agent's job-poll continues to filter on `status='scheduled'`, so
`awaiting_confirmation` jobs are invisible to the agent until later commits
move them forward. With only commit 3 deployed and the flag on, print jobs
queue indefinitely — that's intentional sequencing. Flag stays off in
production until at least B4 commit 5 (decline reroute) is in place so a
contributor decline can actually route the job elsewhere.

### Unexpected scope addition: migration 016

The `workload_type` enum from migration 001 only had five compute/storage
values. `print_traditional` and `print_3d` had to be registered on the
Postgres side before the dispatcher could write them. Caught at the
proposal-stage audit, not at runtime. Schema-only addition; the down
migration documents that Postgres doesn't support removing enum values —
snapshot restore is the only complete rollback path.

### Eight files in the commit

- `internal/store/migrations/016_print_workload_types.{up,down}.sql` (new)
- `internal/types/workload.go` — +2 marketplace constants, extended
  `AllMarketplaceWorkloadTypes`
- `internal/orchestrator/workload.go` — +2 entries in `marketplaceToAgent` map
- `internal/orchestrator/orchestrator.go` — struct + `New()` signature extended
  with `printConfirmEnabled bool` + `confirmationWindow time.Duration`; gate
  branch in `SubmitJob`; `canonicalJobSpecHash` helper; imports for
  `crypto/sha256`, `errors`, `pgx/v5`
- `internal/orchestrator/orchestrator_test.go` — existing `New()` calls
  updated, 4 new tests (3 hash determinism cases + 1 flag-off gate behavior),
  stale comment fix at `TestNodeRegistry_FindMatch_PrintingRequiresEnabledPrinter`
- `cmd/orchestrator/main.go` + `cmd/portal/main.go` — `strconv` import,
  `PRINT_CONFIRMATION_ENABLED` env read defaulting to false, extended `New()`
  call passing `4*time.Hour` as the confirmation window

### Three audit findings caught at proposal stage

**1. `err` shadowing in the print branch.** The initial proposal had
`specHash, err := canonicalJobSpecHash(req)` followed by `_, err = tx.Exec(...)`
inside the same `if` block. The `:=` in the first line creates a new inner-scope
`err`; the subsequent `= tx.Exec(...)` writes to that inner err, not the outer
one. The outer err check after the if-else block would read stale state and
silently swallow the final UPDATE's error in the print path. Fixed by checking
each error at point of occurrence with `if err := ...; err != nil`. The build
would have been clean; existing tests wouldn't have caught it (all exit before
DB access); only the integration test in TODO 23 would exercise this path.

**2. `spec_hash` was missing `ContainerImage`.** Initial proposal excluded the
container image on the grounds it wasn't yet user-visible at confirmation stage.
Pushed back correctly: container image is exactly what a contributor is
acknowledging, and migration 015's stated purpose for `spec_hash` is detecting
drift between acknowledged and dispatched spec. Excluding the image defeated the
primary use case. Added as `container_image` with `json:"container_image"` tag.

**3. `pgx.ErrNoRows` defense-in-depth on printer resolution.** Added explicit
branch distinguishing "no enabled printer on the matched node" from generic DB
errors. The `HasEnabledPrinter` flag in the in-memory registry can drift briefly
from the DB state (registry is heartbeat-driven, not transactionally consistent
with the job write). Should be unreachable via `FindMatch`'s filter, but the
failure message becomes diagnostic rather than mystery on registry/DB drift.

### Known limitation accepted: TODO 22

`node_printers` (migration 014) has no `printer_type` column. The dispatcher's
printer-resolve query picks the lowest `printer_id` among enabled printers
regardless of whether the workload is `print_traditional` or `print_3d`. A node
enabled for any printing matches both. Acceptable for the single-printer-per-node
pilot. Correct fix: migration adding `printer_type` (enum: `traditional`/`threed`)
populated from agent `PrinterInfo` detection, plus `WHERE` clause filter in the
query. Tracked for B8.

### Tests

4 new unit tests in `internal/orchestrator/orchestrator_test.go`:
- `TestCanonicalJobSpecHash_Deterministic` — same input → same hash
- `TestCanonicalJobSpecHash_ContainerImageAffectsHash` — different image → different hash
- `TestCanonicalJobSpecHash_ConsumerIDExcluded` — ConsumerID change → hash unchanged
- `TestSubmitJob_PrintConfirmGate_FlagOff_WritesScheduled` — flag off + print workload
  → fails at FindMatch ("find nodes:"), not at any print-confirmation-specific path

Integration coverage of the `awaiting_confirmation` write path itself deferred as
TODO 23. Build clean. `go test ./internal/types/... ./internal/orchestrator/...`
green in 11.4s.

---

## CLAUDE.md update (`d62cc29`)

Nine `str_replace` edits, file grew 799 → 864 lines. Key additions:

- **Dev XIX checkpoint** (previously missing) — SPIRE recovery, `ca_ttl 720h`,
  CI Node 24 bump, orchestrator rebuild.
- **Dev XX checkpoint** — B4 commit 3, migration 016, flag-gated, no production
  behavior change.
- Migration table row 016.
- Repo structure migrations range `001–015` → `001–016`.
- TODO 9 updated: agent-side `ConnectionPath` wiring is a future B4 follow-up,
  not commit 3 (commit 3 was dispatcher-only).
- TODO 21 marked RESOLVED (`0dd2d77`, Dev XIX CI Node 24 bump).
- TODO 22 added (printer-type discrimination in `node_printers`).
- TODO 23 added (`SubmitJob` DB-path integration test gap, carry-forward from TODO 11).
- Workload type vocabulary block updated: Five → Seven values, migration reference
  updated to "001 + 016", "Print is deliberately out of the marketplace enum" bullet
  replaced with consent-per-job + flag mechanic framing.
- B4 sub-phase header bumped Dev XVIII → Dev XX; commit 3 bullet added; Remaining
  list trimmed to the four outstanding items.
- Dev XVIII checkpoint stale "will apply" sentence revised to past tense referencing
  the Dev XIX rebuild.

---

## Outstanding work, by priority

**1. B4 commit 4** — portal `/jobs/<id>/confirm` page. Natural next step in the
lifecycle. Contributor lands here when the agent surfaces a pending confirmation;
page shows the spec_hash-canonicalized fields and an accept/decline action. Read
existing portal handler patterns before designing — route slots into
`internal/portal/server.go`.

**2. Agent-side `printer_id` → `ConnectionPath` wiring at job-poll** — separate
commit, but blocks any actual run of a confirmed print job. Poll handler at
`internal/api/nodes.go:500-545` returns `{JobID, JobToken, Image}` for
`status='scheduled'`; needs to return `printer_id` for confirmed jobs and the
agent needs to look up the corresponding `PrinterInfo.ConnectionPath` from its
local hardware profile. Closes the second half of TODO 9.

**3. B4 commit 5** — orchestrator decline reroute. On contributor decline, set
`declined_at`, then re-run `FindMatch` and re-dispatch to a different node.
Requires bookkeeping for which nodes have already declined a job (to avoid loops).
Options: new column on `jobs` for excluded node IDs, or a separate
`job_node_declines` table.

**4. B4 commit 6** — auto-decline sweeper. Background goroutine finding rows with
`confirmation_deadline < NOW()` still in `awaiting_confirmation` and flipping them
to `declined`. Migration 015's partial index
(`idx_jobs_confirmation_deadline WHERE confirmation_deadline IS NOT NULL`) already
supports this query shape.

**5. TODO 22** — printer-type discrimination migration. Needs agent-side
`PrinterInfo` to expose a type discriminator, a migration adding the column,
repopulation logic, and updates to the printer-resolve query. Deferrable to B8.

**6. TODO 23** — `SubmitJob` integration test fixture. Scope: a
`_integration_test.go` build-tagged file under `internal/orchestrator/` with a
Postgres fixture matching the `test/integration/` Phase 1 pattern. Four matrix
cells: flag-on + print + printer resolves; flag-on + print + no enabled printer
(registry-DB drift); flag-on + compute (scheduled path); flag-off + print
(scheduled path).

**Carry-forwards from Dev XIX (no movement this session):**

- Power circuit fix — router and host on separate circuits.
- SPIRE re-attestable node attestor — replace `join_token` with `x509pop` or
  `unix`.
- `gh` on Claude Code bash PATH — small env fix; blocks CI verification at
  session close.
- SignPath (TODO 19) — blocked on Foundation approval.
- Postgres-recreate-during-orchestrator-up — watch only if it recurs.
- TODO 18 — Cloudflare tunnel config sync.
- B7 commit 4b worker image production keypair (TODO 14).
- `handleGenerateNodeToken` gRPC refactor.
- `handleHeartbeat` rename.
- Phase C legacy v1 cleanup.
- B5 long-running job lifecycle (TODOs 5, 6).

---

## Process notes from this session

**Audit-stage catches paid off.** The `err` shadowing bug and the `spec_hash`
`ContainerImage` omission both would have compiled clean and passed the existing
tests. Neither is the kind of thing static analysis catches. Proposal-first
workflow is doing real work, not adding ceremony.

**Memory had the commit-message convention inverted.** Chat-side memory said
"Jodson writes commit messages verbatim." CLAUDE.md and project instructions say
the opposite — Claude writes verbatim, Jodson authorizes. Caused mid-session
friction at commit time. Fixed: memory edit correcting the convention; should not
recur. Separately, the stale per-phase memory entry was replaced with a
CLAUDE.md-defer pointer so the same drift class doesn't bite again.

**CLAUDE.md must be updated at every phase boundary, including for sessions that
produce a handoff doc.** Dev XIX produced a thorough handoff but did not update
CLAUDE.md's checkpoint list. The handoff doc and CLAUDE.md serve different
purposes — handoff is narrative state for the next session, CLAUDE.md is the
authoritative project state document. Both are needed at session close.

**Proposal-first applies to docs, not just code.** Audit on the seven proposed
CLAUDE.md edits caught: Edit 3 dropped useful architectural framing; Edit 4
mis-described which future commit closes the remaining TODO 9 work; Edit 5 had
the wrong Dev number (XIX vs XX); Edit 7 was missing the Dev XIX checkpoint
entirely. These would have left CLAUDE.md drifting from ground truth.

**`dev-xix.md` was never written.** Dev XIX produced a thorough summary in Claude
Chat but the handoff file was never dispatched to disk. CLAUDE.md now references
`docs/handoffs/dev-xix.md` in the checkpoint, but that file does not exist. If
Dev XIX context is needed, the CLAUDE.md Dev XIX checkpoint is the available
record. Consider writing dev-xix.md retroactively if the SPIRE recovery runbook
detail matters for future sessions.

---

## Opening reads for next session

```
cd "C:/Users/Jodson Graves/Documents/SoHoLINK"
git log --oneline -10 && docker compose ps && go build ./cmd/... && echo "all clean"
```

If picking up **B4 commit 4** (portal confirm page):

```
grep -n "handleJobs\|handleJobConfirm\|jobs/.*/confirm" internal/portal/server.go
sed -n '700,780p' internal/portal/server.go
ls web/templates/ | grep -i "confirm\|job"
```

If picking up **agent-side `ConnectionPath` wiring**:

```
sed -n '480,545p' internal/api/nodes.go
grep -n "PrinterInfo\|ConnectionPath" internal/agent/hardware.go
grep -rn "printer_id\|PrinterID" internal/api/ internal/agent/
```

If picking up **TODO 23** (integration test fixture):

```
ls test/integration/
grep -rn "DATABASE_URL\|//go:build integration" test/ internal/orchestrator/
```

---

## Secrets

No rotations this session. SPIRE join token from Dev XIX still current in `.env`.
Dev allowlist keys unchanged. Production `SESSION_PRIVATE_KEY` unchanged.

---

*End of Dev XX Handoff. Master at `d62cc29`. Two commits: B4 commit 3 print
confirmation dispatcher gate (`0115d41`) and CLAUDE.md Dev XX checkpoint
(`d62cc29`). Next session: B4 commit 4 (portal confirm page) is the natural next
step.*
