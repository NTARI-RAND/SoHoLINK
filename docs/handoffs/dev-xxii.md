# SoHoLINK Dev XXII Handoff

**Branch:** master · **HEAD:** `b365200` · **Build:** clean · **CI:** green (verified on `027ac81` and `0475943`; `b365200` in_progress at session close — verify on next session open) · **Date:** 2026-05-18
**Previous handoff:** Dev XXI (`docs/handoffs/dev-xxi.md`)

---

## What was accomplished this session

Three commits, one focused session. Opened with a CLAUDE.md inconsistency that
needed resolution (the warm-up), shipped B4 commit 6 to close the auto-decline
sweeper gap and complete the B4 print confirmation lifecycle, then updated CLAUDE.md
to reflect the new state and resolve the deploy hold imposed in Dev XXI.

No new TODOs opened. No bugs found mid-session. One durable process refinement:
the three-layer commit-message convention (Code drafts → Chat audits → human
authorizes) was codified as canonical at `027ac81` — earlier sessions had run
two different inverted versions of the same convention.

Production state unchanged from Dev XXI (`0115d41`) — deploy not yet run.
B4 is now structurally ready to deploy; the explicit first action of Dev XXIII
is the production deploy of `b365200`.

---

## Commits this session

| Hash | Message |
|---|---|
| `027ac81` | docs(claude.md): commit message convention — Code drafts, Chat audits, human authorizes |
| `0475943` | feat(orchestrator): B4 commit 6 — auto-decline sweeper with ExpireConfirmation |
| `b365200` | docs(claude.md): Dev XXII — B4 complete, commit 6 documented, deploy hold resolved |

---

## Warm-up — commit-message convention (`027ac81`)

CLAUDE.md Workflow Discipline (lines 31-32) stated "Commit messages are written
by the human verbatim." This contradicted lines 39-41 of the same section
(which describe the Code-block paste-ready format that the human "pastes without
reading line-by-line" — implying Claude Chat authored it), the Dev XX/XXI
practice (Claude Chat dictated commit messages, Jodson authorized), and the
2026-05-16 memory correction.

Jodson proposed a third option mid-discussion: Code drafts the commit message
(has direct diff visibility), Chat audits, human authorizes. This extends the
propose-audit-authorize loop already used for code changes to the commit message
itself. Lines 31-32 updated to reflect the new convention.

This commit was the first application of the new convention — Chat drafted
verbatim, Jodson authorized. Subsequent commits this session used the
Code-drafts → Chat-audits → human-authorizes pattern.

---

## B4 commit 6 — auto-decline sweeper (`0475943`)

Single file changed: `internal/orchestrator/orchestrator.go`. Two new functions
added between `rerouteDeclined` and `StartDeclineRerouteLoop`. Two integration
tests added to `internal/orchestrator/orchestrator_integration_test.go`.

**`ExpireConfirmation(ctx context.Context, jobID string) (bool, error)`** — exported
per-job function. Issues a single UPDATE:

```sql
UPDATE jobs
SET status      = 'declined'::job_status,
    declined_at = NOW(),
    updated_at  = NOW()
WHERE id = $1
  AND status = 'awaiting_confirmation'::job_status
  AND confirmation_deadline < NOW()
```

Returns `flipped=true` when `RowsAffected() == 1`; `flipped=false, err=nil` is the
lost-race case (portal confirm or prior expiry landed between the caller's SELECT
and this UPDATE). Race-safe by construction — the WHERE re-checks both status and
deadline.

**`expireConfirmations(ctx context.Context)`** — unexported batch tick handler.
Mirrors the `rerouteDeclined` pattern exactly: `Query` with `LIMIT 100`, iterate
with `rows.Close()` before error check, call `ExpireConfirmation` per ID, log
`slog.Info` on `flipped=true` and `slog.Debug` on `flipped=false`. Errors logged
via `slog.Error` with `continue` (one job failure does not abort the batch).

**`StartDeclineRerouteLoop` updated** — `case <-ticker.C:` arm now calls
`o.expireConfirmations(ctx)` before `o.rerouteDeclined(ctx)`. Expire-then-reroute
ordering means a job that just crossed its deadline gets rerouted in the same 30s
tick. Doc comment updated to describe both responsibilities. No new goroutine —
both passes share the existing ticker.

`idx_jobs_confirmation_deadline` (partial index from migration 015,
`WHERE confirmation_deadline IS NOT NULL`) powers the SELECT in
`expireConfirmations`.

### Audit findings caught before write

- **Co-locate on existing ticker vs. separate goroutine.** Chose co-locate:
  one ticker, one cancellation point, and sequential expire-then-reroute means
  same-tick reroute for freshly-expired jobs.
- **Mirror SELECT-iterate-UPDATE pattern vs. single CTE bulk UPDATE.** Mirrored
  the pattern per Dev XXI handoff guidance — per-job logging and consistency with
  the rerouter outweighed the marginal efficiency gain.
- **Per-job exported + batch unexported.** Originally proposed a single batch
  function; restructured mid-design to match the `RerouteDeclinedJob` +
  `rerouteDeclined` shape so the test could call `ExpireConfirmation` directly.
  The `(bool, error)` return on the exported per-job function is an improvement
  over `RerouteDeclinedJob`'s `error`-only return — makes the lost-race case
  explicit and testable without a follow-up DB query.
- **`slog.Debug` for race-loss vs. silent success.** Jodson's call: log the
  lost-race case at Debug level so ops can surface it without alarming at Info.
- **Don't rename `StartDeclineRerouteLoop`.** The name no longer captures both
  responsibilities, but renaming touches `cmd/orchestrator/main.go` and isn't
  worth a wider diff. Updated the loop's doc comment instead.

### Workflow learnings

- **Edit tool requires prior Read.** The first `str_replace` attempt this session
  failed because the Edit tool requires a Read tool invocation on the target file
  first. Bash `cat`/`sed`/`grep` doesn't satisfy this prereq. Workaround used:
  `head -N file > /tmp/new && cat >> /tmp/new << 'GOEOF' ... GOEOF && cp /tmp/new file`.
  Works but introduces an extra blank line that `gofmt` normalizes — fine for Go
  files, but for non-gofmt'd files (Markdown, YAML, shell) it would leave the
  artifact. **Adjustment for next sessions**: pre-dispatch a `view` of the target
  region alongside the `str_replace` instruction so the Edit prereq is satisfied
  without a fallback.
- **Bash heredoc on Windows produces CRLF warnings.** "LF will be replaced by
  CRLF the next time Git touches it" appeared on the splice commits. Harmless —
  Git's `core.autocrlf` handles normalization — but a marker that the Edit-tool
  path is preferable when available.
- **`DATABASE_URL` in `.env` points to Docker-internal hostname.** `.env` has
  `postgres://...@postgres:5432/...` (the Docker service name `postgres`). Host-shell
  test runs need `127.0.0.1:5432` with the same password. Worked around by
  extracting the password from `.env` and constructing a localhost URL.
  Pre-existing friction, not introduced this session. A `Makefile` target or
  `.env.local` would be the ergonomic fix; not pursued.
- **Dispatch text reachability.** A Chat dispatch that references content "in the
  previous message" only works if that content was in the For Code block of the
  prior turn — content in For Jodson sections doesn't reach Code. When approved
  text needs to land verbatim in a file, the text must appear inside the For Code
  block of the dispatch that creates the file. Codified here after a failed first
  attempt at this very handoff doc.

### Integration tests (`0475943`)

Two new tests appended to `orchestrator_integration_test.go`:

| Test | What it exercises |
|---|---|
| `TestExpireConfirmation_DeadlinePast_FlipsToDeclined` | Inserts `awaiting_confirmation` job with `confirmation_deadline = NOW() - INTERVAL '1 second'`; asserts `flipped=true`, `status='declined'`, `declined_at IS NOT NULL` |
| `TestExpireConfirmation_DeadlineFuture_NoChange` | Inserts `awaiting_confirmation` job with `confirmation_deadline = NOW() + INTERVAL '4 hours'`; asserts `flipped=false`, `status='awaiting_confirmation'` unchanged |

Both use `setupOrchFixture(t, writeOrchAllowlist(t), false)` — no printer
registration or registry opt-out wiring needed since `ExpireConfirmation` is a
pure DB operation. Full integration suite (8 tests) green against real Postgres.

Incidental `gofmt` fix on `orchFixture` struct field alignment was kept in the
diff — pre-existing column-alignment drift that `gofmt -w` re-aligned during the
verification step. Shipping with known `gofmt` drift is worse hygiene than a
cosmetic alignment change.

---

## CLAUDE.md Dev XXII update (`b365200`)

Four `str_replace` edits, one file:

1. **Sub-phase B4 header** — `(commits 1–5 + test coverage complete, Dev XXI)` →
   `(complete, Dev XXII · `0475943`)`
2. **"Remaining" bullet** — forward-looking placeholder replaced with retrospective
   Commit 6 bullet documenting `ExpireConfirmation`, `expireConfirmations`, loop
   wiring, index, and test count. Matches the commits 1-5 format.
3. **Flag-flip ops note** — "Do not flip the flag until B4 commit 5 is deployed"
   updated to reference commit 6 and name the failure mode (expired confirmations
   stall indefinitely in `awaiting_confirmation`).
4. **New deployment checkpoint** — `0475943` (2026-05-18, Dev XXII) appended after
   the `dc1de1d` checkpoint, before `### Sub-phase B8`. Time-ordered narrative:
   the hold was imposed in `dc1de1d`, resolved in `0475943`. The `dc1de1d`
   checkpoint text was left unchanged (historical record).

---

## TODOs

No new TODOs opened this session. No numbered TODOs closed — the "Remaining" note
at the B4 section tail was an internal forward-looking marker, not a numbered
TODO. It is now resolved by `0475943`. The deploy hold documented in `dc1de1d`
is also now resolved.

---

## Production state

- **Still at `0115d41`** — deploy not yet run.
- Master is at `b365200`. Rolling forward to `b365200` deploys: B4 commits 3-6
  (`0115d41`, `93d5381`, `ad2dda2`, `0475943`), agent wiring (`0431188`),
  integration test coverage (`dc1de1d`), and the Dev XXII CLAUDE.md updates.
- Code at `0475943` is the unlocking change; the `dc1de1d` deploy hold ("Do not
  deploy until B4 commit 6") is resolved in CLAUDE.md.
- `PRINT_CONFIRMATION_ENABLED` remains off in production. The deploy of
  `b365200` is functionally close to a no-op at runtime — the sweeper goroutine
  runs but finds no `awaiting_confirmation` rows until the flag is flipped.
  Safe to flip once a pilot participant with a print node is onboarded and the
  flow is manually verified end-to-end.
- Migration 017 (`job_node_declines`) will apply at next orchestrator restart.
  No-op until application code that reads it is live (it is, in `ad2dda2`).
- **Deploy is the explicit first action of Dev XXIII.** Closing the session
  here rather than deploying at the tail of a long session is a discipline
  choice — production deploys deserve a fresh-head session with pre-flight
  checks visible and a rollback plan loaded.

---

## Outstanding work, by priority

**1. Production deploy of `b365200`.** First action of Dev XXIII.
`deploy/redeploy.sh` is the canonical entry point per recent deployment
checkpoints. Verify all containers healthy post-deploy, check migrations applied
(expect version 17 at portal startup log), smoke test `api.soholink.org/health`
200 and `soholink.org` 200. After deploy greens, update CLAUDE.md with a
"resolved by" annotation on the Dev XXII checkpoint header (per the
`6f8d9a2 · resolved 0a28f88` precedent).

**2. TODO 25 — portal orphaned registry.** `cmd/portal/main.go` constructs a
`NodeRegistry` that never receives heartbeats. Consumer-side `POST /consumer/job`
always returns "no available nodes match request." Latent — no production traffic
yet. Three fix paths: expose orchestrator HTTP submission endpoint, merge portal
and orchestrator processes, or DB-backed registry sync on heartbeat. Needs
architecture decision in Claude Chat before code.

**3. TODO 24 — zombie `running` rows.** `handleGetJobs` flips `scheduled` →
`running` optimistically at poll time before the agent confirms container start.
Failure before container launch leaves the job stuck. Fix: start-confirmation
endpoint (`POST /jobs/<id>/started`) or running-timeout reaper. Natural B5 scope.

**4. B5 — long-running job lifecycle.** TODOs 5 and 6 (completion endpoint JSON
body + exit-code conditioned metering) are the immediate carry-forwards. Zombie
running rows (TODO 24) resolves naturally in B5.

**5. TODO 22 — printer-type discrimination.** `node_printers` has no
`printer_type` column. Dispatcher picks lowest `printer_id` lexicographically
regardless of `print_traditional` vs `print_3d`. Acceptable for single-printer
pilot; migration + query filter needed before multi-printer nodes. B8 territory
unless pilot surfaces the ambiguity earlier.

**6. TODO 18 — `deploy/sync-tunnel-config.sh`.** Cloudflare tunnel config drift
prevention. Not blocking participant testing.

**7. TODO 19 — SignPath code-signing.** Application submitted 2026-05-08.
Awaiting SignPath Foundation verification. Not blocking participant testing.

---

## Opening reads for next session

Standard state check:

```bash
cd "C:/Users/Jodson Graves/Documents/SoHoLINK"
git log --oneline -5
gh run list -R NTARI-RAND/SoHoLINK --limit 3
go build ./cmd/... && echo "all clean"
```

For CLAUDE.md: **use `cat` via Bash, not the Read tool.** The Read tool renders
to a VS Code panel that doesn't paste through to chat. A useful section map:

```bash
grep -n "^### \|^## " CLAUDE.md | head -50
```

If deploying first (Priority 1):

```bash
ls deploy/
grep -n "Deployment checkpoint\|deploy/" CLAUDE.md | head -20
git log 0115d41..b365200 --oneline   # what's being shipped
bash deploy/redeploy.sh
docker ps --format "{{.Names}}\t{{.Status}}"
curl -s https://soholink.org | head -5
curl -s https://api.soholink.org/health
```

If picking up **TODO 25 design** (Priority 2):

```bash
grep -n "NewNodeRegistry\|orchestrator.New\|SubmitJob" cmd/portal/main.go
sed -n '85,120p' cmd/portal/main.go
grep -n "POST.*consumer/job\|handleSubmitJob\|handleConsumerJob" internal/portal/server.go
```

If picking up **B5 scoping** (Priority 4):

```bash
grep -n "TODO 5\|TODO 6\|TODO 24" CLAUDE.md | head -20
grep -n "handleJobComplete\|/jobs/.*complete" internal/api/nodes.go
```

---

## Process refinements codified this session

- **Three-layer commit message convention** (`027ac81`): Code drafts (has diff
  visibility) → Chat audits → human authorizes. Written into CLAUDE.md Workflow
  Discipline. No Co-Authored-By or Signed-off-by trailers.
- **Edit tool pre-read requirement**: dispatch a `view`/Read of the target region
  before any `str_replace` instruction to avoid the "File has not been read yet"
  error and the `/tmp` fallback path.
- **`.env` DATABASE_URL vs host shell**: when running integration tests from the
  host shell, use `127.0.0.1:5432` with the password extracted from `.env`
  (Docker-internal `postgres` hostname is unreachable from the host).
- **Dispatch text reachability**: content that must reach Code must appear inside
  the For Code block of the dispatch. References to "the previous message" only
  resolve to what was actually pasted to Code — content in For Jodson sections
  does not reach Code.

---

## Secrets

No rotations this session. SPIRE join token from Dev XIX current in `.env`.
Dev allowlist keys unchanged. Production `SESSION_PRIVATE_KEY` unchanged.

---

*End of Dev XXII Handoff. Master at `b365200`. Three commits: commit-message
convention codification, B4 commit 6 (auto-decline sweeper), CLAUDE.md Dev XXII
update. B4 lifecycle complete. Production deploy unblocked but not yet run —
deploy of `b365200` is the explicit first action of Dev XXIII, followed by TODO
25 architecture design or B5 scoping.*
