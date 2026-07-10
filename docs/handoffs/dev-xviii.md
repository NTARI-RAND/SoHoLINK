# SoHoLINK Dev XVIII Handoff
**Branch:** master · **HEAD:** `ec6ad9a` · **Build:** clean · **CI:** green · **Date:** 2026-05-14
**Previous handoff:** Dev XVII

## What was accomplished this session

Two original goals plus three correction/closeout streams.

**Original goals — both met.** Shipped sub-phase **B** (three small wins
slated in the Dev XVII handoff: gitignore + signed allowlist with
`.gitattributes` integrity rule, portal sign-verify roundtrip closing
TODO 20, readable installer bitmap backgrounds) and **B4 commit 1**
(ConnectionPath threading through ContainerSpec, resolving the
device-mapping half of TODO 9). Bonus B4 commit 2 added migration 015
for the print job confirmation schema.

**Three correction streams surfaced and resolved mid-session.**
Migration 015 as originally written had a same-transaction enum
reference in the partial index predicate (PG "unsafe use of new value"),
caught when CI ran the integration step. CI itself was broken on a
separate axis — workflow's Go version pinned to 1.24 while go.mod
requires 1.25, producing an irregular setup-go cache collision. The
Dev XVII handoff doc was committed to a new `docs/handoffs/` directory.
After commits 7 and 8 landed, CI returned to green.

**Top priority for next session:** decide between (a) continuing B4
into commits 3+ (dispatcher pending state, agent ConnectionPath fill,
portal confirmation page) and (b) the Node 20 deprecation deadline
(TODO 21, 2026-06-02) which sits 15 days out. (a) is the bigger
productive move if SignPath approval (TODO 19) hasn't landed; (b) is
short and date-driven. Both unblocked.

## Commits

| Hash | Subject |
|---|---|
| `ec6ad9a` | docs(claude.md): Dev XVIII checkpoint, migration 015, TODO 20 closed, new conventions |
| `4c76919` | fix(ci): sync workflow Go version to go.mod (go-version-file) |
| `9fa58ba` | fix(jobs): migration 015 — avoid same-transaction enum reference in index predicate |
| `97b3cef` | docs(handoffs): add Dev XVII handoff doc |
| `de2c091` | feat(jobs): migration 015 — print job confirmation schema (B4) |
| `b1500f6` | feat(agent): thread ConnectionPath through ContainerSpec to enable USB printer device mappings |
| `ff35b8b` | fix(installer): light bitmap backgrounds so wizard text is readable |
| `547374d` | feat(portal): sign/verify roundtrip on Ed25519 key load (TODO 20) |
| `a2a8e3a` | chore(deploy): gitignore dev allowlist keys; commit signed dev allowlist |

Nine commits since Dev XVII's tip (`189800b`).

## B small wins (`a2a8e3a`, `547374d`, `ff35b8b`)

**Gitignore + signed allowlist** (`a2a8e3a`). Added `*.priv` and
`/allowlist-dev*` to `.gitignore`. Committed `deploy/allowlist/allowlist.json`
(signed dev allowlist) so the orchestrator's bind-mounted file is
reproducible across machines without re-signing. Added `.gitattributes`
with `deploy/allowlist/*.json -text` because the signature is computed
over the file bytes and any LF↔CRLF translation breaks verification.
Verified the staged blob's hash matched the working copy before commit
(no conversion happened on stage).

**Sign-verify roundtrip on portal Ed25519 key load** (`547374d`, closes
TODO 20). `mustEd25519Key` now performs a sign-then-verify probe
("soholink-key-self-test-v1") against the public key derived from the
loaded bytes after the length check. Catches the failure mode where 64
bytes pass the length check but the embedded public-key half doesn't
correspond to the seed half — exactly the scenario that motivated TODO
20 (broken login from Dev XVI's bad `SESSION_PRIVATE_KEY` generation).
19 portal handler tests confirm the check is non-destructive on valid
keys. Codified as a coding convention so any future asymmetric-key
loader picks it up.

**Readable installer bitmap backgrounds** (`ff35b8b`). The wizard text
is rendered black by Windows Installer, but the previous bitmap
backgrounds were near-black (#0A0D12 banner, #11181F dialog) — text was
invisible against the background. Set both to #F8F9FA (near-white) as
a readability interim. TODO 4 still tracks the proper branded artwork.

## B4 commits 1–2 (`b1500f6`, `de2c091`, `9fa58ba`)

**Commit 1 — ConnectionPath threading** (`b1500f6`). `ContainerSpec`
gains a `ConnectionPath string` field, populated by the agent at
job-poll time from local `PrinterInfo` (wiring lands in B4 commit 3).
On Unix, `deviceMountsFor` consumes the path: when the allowlist entry
declares `usb_printer` access and the path is non-empty, the path is
mapped into the container at the same path with `rwm` cgroup permissions.
Empty path produces no mapping (defensive against allowlist entries
that declare USB printer access on non-print workloads or before the
agent has resolved the target printer). Windows stub takes the new
parameter and ignores it — print on Windows is B8 territory.

Added `TestBuildHostConfig_USBPrinterDeviceMapping` (table-driven,
populated + empty cases). Skips on Windows like the existing
`TestBuildHostConfig_CUPSDeviceAccess` — meaning the Linux branch is
exercised by CI only, not on the dev box. CI is now live and green, so
the test runs.

Resolves the device-mapping half of TODO 9. The dispatch-time wiring
remains for B4 commit 3.

**Commit 2 — migration 015 schema** (`de2c091`, corrected by
`9fa58ba`). Schema for the print job confirmation lifecycle:

- `awaiting_confirmation` and `declined` added to `job_status` enum
- `printer_id` (TEXT), `spec_hash` (BYTEA), `confirmed_at`, `declined_at`,
  `confirmation_deadline` (all TIMESTAMPTZ) on `jobs`
- Partial index for the auto-decline sweeper

Two design decisions worth noting. **First**: the
`(node_id, printer_id)` pairing is enforced at the application layer,
not by a composite FK. A composite FK with `ON DELETE SET NULL` would
nullify `jobs.node_id` on routine printer cleanup (defeating the
single-column `node_id` FK from migration 001 that nullifies only on
node deletion). `ON DELETE RESTRICT` would block node deletion via
cascade through `node_printers`. Neither action gives clean semantics
for both deletion paths; no FK was the cleanest choice.

**Second**: the original commit `de2c091` had a same-transaction enum
reference in the partial index predicate — `CREATE INDEX … WHERE status
= 'awaiting_confirmation'` immediately after `ALTER TYPE … ADD VALUE`.
PostgreSQL rejects this even on PG 12+ (the restriction on USING a new
value in the same transaction it was added is separate from whether
ADD VALUE itself is permitted in a transaction). CI's integration step
caught it. Follow-up `9fa58ba` changed the predicate to
`WHERE confirmation_deadline IS NOT NULL` — functionally equivalent for
the sweeper query, doesn't reference the enum. The migration was
amended in place because it never successfully applied anywhere
(strict immutability rule didn't bite). The whole gotcha is now
codified in CLAUDE.md "Migration writing rules".

Schema-only: no code reads or writes these columns yet. Behavior
unchanged until B4 commits 3+ ship the dispatcher and lifecycle code.
The migration will apply at next orchestrator restart.

## CI infrastructure (`4c76919`)

Workflow was pinned to `go-version: '1.24'` while `go.mod` requires
`go 1.25.0`. Go's auto-toolchain switching downloaded 1.25 at build
time, but setup-go's cache key incorporated the downloaded version,
producing an irregular tar collision: cache restoration would attempt
to extract toolchain files into `/home/runner/go/pkg/mod` where the
auto-download had already placed them, and `tar` would fail with
"File exists" exit 2. Fired on `de2c091` and `97b3cef` this session;
cache state would have continued to flake intermittently.

Switched to `go-version-file: go.mod` so the workflow tracks the
project's declared version. Removes the drift class entirely. CI
returned to green on `9fa58ba`, before `4c76919` even shipped — cache
state happened to self-clear — but the underlying drift was still
there and would have recurred. Defensive fix.

Also surfaced TODO 21: `actions/setup-go@v5` and `actions/checkout@v4`
both run on Node.js 20, which GitHub will force to Node.js 24 starting
2026-06-02 (15 days from this session). Plan: audit each action for a
Node 24-compatible version before that date.

## Process record (`97b3cef`, `ec6ad9a`)

**Dev XVII handoff doc** (`97b3cef`). Committed to a new
`docs/handoffs/` directory. Now the canonical location for session
handoff records.

**CLAUDE.md phase-boundary update** (`ec6ad9a`). Eleven edits across
Repository Structure, Database Migrations (with new "Migration writing
rules" subsection), Test Coverage, two TODO updates + one new TODO,
Coding Conventions (two new entries), Sub-phase B4 status, and a new
Deployment checkpoint for `4c76919`. Also corrected a pre-existing
"TODO 11" reference in the B4 section that should have been "TODO 9".

## Production state

No production deploy this session. Stack unchanged from Dev XVII
(`189800b`). 7 containers up. URLs healthy:

| URL | Status |
|---|---|
| `soholink.org/` | 200 |
| `soholink.org/download` | 200 |
| `soholink.org/privacy` | 200 |
| `api.soholink.org/health` | 200, identity ready |
| `api.soholink.org/allowlist` | 200 (signed dev allowlist) |
| `soholink.org/static/SoHoLINK-Setup.msi` | 200, ~16 MB, current build with ALLOWLIST_PUBLIC_KEY injected (Dev XVII build, unchanged this session) |

CI green on `ec6ad9a`. Migration 015 ready to apply at next
orchestrator restart; behavior unchanged. Service-start blocker
remains open on TODO 19 (SignPath approval pending).

## Outstanding work, by priority

**1. Node.js 20 deprecation deadline 2026-06-02 (TODO 21, ~15 days
out).** Audit `actions/setup-go` and `actions/checkout` for Node 24
compatibility. Target upgrade by 2026-05-29.

**2. Service-start blocker (TODO 19 SignPath).** Carry-forward from
Dev XVII. SignPath application submitted Dev XVI; no movement reported
this session. Once approved, sign MSI and re-attempt installation on
NTARI1 to confirm the service starts cleanly. The Dev XVII hypothesis
— Smart App Control / WDAC blocking unsigned binaries in the service
security context — remains the most defensible explanation; signing
should resolve.

**3. B4 lifecycle code (commits 3+).** Unblocked.
- Commit 3: dispatcher writes pending state with spec hash + assigned
  printer; agent fills `ConnectionPath` from local `PrinterInfo` at
  job-poll time
- Commit 4: portal `/jobs/<id>/confirm` page (GET + POST)
- Commit 5: orchestrator decline reroute (FindMatch re-runs excluding
  the declining node)
- Commit 6: auto-decline sweeper (background goroutine, scans the
  partial index for past-deadline rows)

Note: commits 3–6 must deploy together or behind a feature flag.
Deploying just commit 3 means print jobs go to `awaiting_confirmation`
with no UI to confirm them — they'd hang. The schema (commit 2) was
deliberately deployable in isolation; the lifecycle code is not.

**4. B5 long-running job lifecycle (TODOs 5, 6).** Unblocked. Container
progress reporting, `awaiting_pickup`/`picked_up`/`delivered` statuses,
exit-code-conditioned metering.

**5. Carry-forwards from Dev XVII.** TODO 18 Cloudflare tunnel config
sync script. B7 commit 4b worker image production keypair (TODO 14).
`handleGenerateNodeToken` gRPC refactor. `handleHeartbeat` variable
rename. Phase C legacy v1 cleanup.

## Process notes from this session

**Signed-artifact line endings.** Any signed file under Git needs a
matching `-text` rule in `.gitattributes`. Discovered when staging
`deploy/allowlist/allowlist.json` produced an LF→CRLF warning. The
warning was easy to dismiss as cosmetic; it isn't, for signed files.
Lesson: any time a signed artifact joins the tree, the
`.gitattributes` rule lands in the same commit.

**`go test` saying `ok` doesn't mean integration ran.** Without `-v`
or `-tags integration`, Go silently skips tag-gated tests but still
prints `ok`. I (Claude) misread `ok 2.251s` as evidence that
integration tests had exercised migration 015 locally. They hadn't.
Use `-v` when you need to know what actually ran.

**ALTER TYPE ADD VALUE + same-transaction reference is forbidden even
on PG 12+.** PG 12+ permits ADD VALUE inside a transaction. It does
*not* permit using the new value in that same transaction (in CREATE
INDEX WHERE, INSERT, UPDATE … = 'newvalue', etc.). Migration 015 hit
this. Codified in CLAUDE.md "Migration writing rules".

**setup-go cache collisions are non-deterministic, not flake-only.**
Workflow Go version drifting below go.mod's requirement triggers
toolchain auto-download into a path that the cache restoration step
then tries to overwrite. The collision depends on cache state, so
some runs succeed and others fail. The drift is the root cause; the
collision is a symptom. Fix the drift, not the symptom. (Use
`go-version-file: go.mod` so the workflow always tracks source of
truth.)

**Don't draft fixes ahead of error logs.** Claude Chat had the
migration 015 hypothesis (correct) drafted before the CI log arrived.
When the log showed a different proximate cause (setup-go cache),
the migration hypothesis was temporarily concluded to be wrong. It
wasn't — it was just hidden by the setup-go failure. Lesson:
distinguish "real bug" from "cause of today's symptom"; the same
code can have multiple bugs that aren't all visible at the same
time.

**Composite FK cascade semantics can defeat existing single-column
FKs.** Adding a composite FK on `(node_id, printer_id)` to
`node_printers` would have nullified `jobs.node_id` on routine printer
cleanup — defeating the existing `node_id → nodes(id) ON DELETE SET
NULL` from migration 001 which is meant to nullify only on actual
node deletion. Sometimes the right call is "no FK, document the
invariant at the application layer." Migration 015 did exactly this
and documented why inline.

**Migration immutability — when to bend the rule.** Strict rule:
never edit a published migration. Pragmatic exception: when the
migration has never successfully applied anywhere (CI broken, no
production deploy), editing in place is acceptable and produces
cleaner history than the "add a forward fix" pattern. Used for
`9fa58ba`'s correction to 015. Won't generalize once we have other
contributors syncing or production has applied the migration.

**Commit count discipline.** Commit counts drifted by 1–2 multiple
times this session. Anchor on `git rev-list --count 189800b..HEAD`
or equivalent rather than running counts.

## Opening reads for next session

```bash
cd "C:/Users/<user>/Documents/SoHoLINK"
git log --oneline -12 && docker compose ps && go build ./cmd/... && echo "all clean"
```

If picking up Node 20 deprecation (TODO 21):

```bash
cat .github/workflows/ci.yml
# Then check setup-go and checkout release pages for Node 24 versions
```

If picking up B4 commit 3 (dispatcher pending state):

```bash
grep -n "SubmitJob\|InsertJob\|status = 'scheduled'" internal/orchestrator/*.go internal/api/*.go
sed -n '500,560p' internal/api/nodes.go
```

If picking up service-start (TODO 19): test the current MSI on
NTARI1 if SignPath approval has landed; otherwise no action item
this session.

## Secrets

No production secret changes this session. `allowlist-dev.priv`,
`allowlist-dev.pub`, and `allowlist-dev-signed.json` are now
gitignored via `/allowlist-dev*` pattern; corresponding public key
(`2wB993q6Kh9YtMD3BMY2b+FZB/AAmf62PIb2a6I+Hqc=`) remains embedded in
the current MSI build. Production `SESSION_PRIVATE_KEY` and SPIRE
keys unchanged.

---

*End of Dev XVIII Handoff. Master at `ec6ad9a`. Nine commits since
Dev XVII; B and B4 commits 1–2 shipped, migration 015 corrected, CI
fixed and green. Next session: pick between Node 20 deprecation
(deadline-driven) and B4 commits 3+ (most productive); SignPath
remains pending for service-start.*
