# SoHoLINK Software Development Life Cycle

**Version:** 1.1 — 2026-03-04
**Module:** `github.com/NetworkTheoryAppliedResearchInstitute/soholink`
**Language:** Go 1.25.7

This document defines the end-to-end Software Development Life Cycle (SDLC) for SoHoLINK.  It covers every stage from idea to production and includes SoHoLINK-specific requirements for payment safety, multi-platform delivery, policy governance, and ML telemetry integrity.

---

## Table of Contents

1. [Principles](#1-principles)
2. [Phase 1 — Planning & Requirements](#2-phase-1--planning--requirements)
3. [Phase 2 — Architecture & Design](#3-phase-2--architecture--design)
4. [Phase 3 — Implementation Standards](#4-phase-3--implementation-standards)
5. [Phase 4 — Testing Strategy](#5-phase-4--testing-strategy)
6. [Phase 5 — Code Review](#6-phase-5--code-review)
7. [Phase 6 — Security Gates](#7-phase-6--security-gates)
8. [Phase 7 — Release Management](#8-phase-7--release-management)
9. [Phase 8 — Operations & Feedback](#9-phase-8--operations--feedback)
10. [Subsystem-Specific Requirements](#10-subsystem-specific-requirements)
11. [Emergency Hotfix Process](#11-emergency-hotfix-process)
12. [Toolchain Reference](#12-toolchain-reference)

---

## 1. Principles

| Principle | Application to SoHoLINK |
|-----------|------------------------|
| **Financial correctness first** | Payment, HTLC, and metering bugs cause real monetary loss. Every payment-path change requires a dedicated review pass before merge. |
| **Security by default** | New features must ship with rate limiting, input validation, and minimal privilege. Security is not a post-launch concern. |
| **Cross-platform parity** | A feature is not done until it builds and passes tests on Windows (CGO/MinGW), Linux, and macOS. |
| **Stable protocol contracts** | Wire formats (WebSocket messages, JSONL telemetry, Rego input schemas) are treated as public APIs. Breaking changes require a deprecation cycle. |
| **Traceable decisions** | Every architectural decision is recorded (ADR or PLAN.md entry). Every bug fix is linked to a CHANGELOG entry. |
| **Fearless refactoring** | A comprehensive test suite (race-detected, short-flag safe) enables confident changes. Coverage is a quality indicator, not a metric to game. |

---

## 2. Phase 1 — Planning & Requirements

### 2.1 Idea Intake

All work originates from one of three sources:

| Source | Initial artifact | Triage SLA |
|--------|-----------------|------------|
| Bug report (internal / user) | GitHub Issue labeled `bug` | 48 hours |
| Feature proposal | GitHub Issue labeled `enhancement` | 1 week |
| Security finding | Private disclosure → `SECURITY_FINDINGS.md` | 24 hours |

**Template for feature proposals:**
```
## Problem
What does the user/operator currently have to do, or can't do?

## Proposed Solution
High-level description. No implementation details yet.

## Subsystems Affected
[ ] orchestration  [ ] payment  [ ] ml  [ ] httpapi  [ ] storage
[ ] notification   [ ] wasm     [ ] gui [ ] rego policy  [ ] mobile

## Payment / Security Impact?
Does this touch any money movement, HTLC lifecycle, or credential handling?
Yes / No — if yes, flag for dual security review.

## Acceptance Criteria
Bullet list of observable, testable outcomes.
```

### 2.2 Milestone Alignment

Features are assigned to a milestone from ROADMAP.md:

```
v0.1.x — Local web dashboard (in progress — Phase 1 shipped 2026-03-04)
v0.2.0 — Android TV nodes
v0.3.0 — Android "Earn While Charging"
v0.4.0 — iOS management client
v0.5.0 — Container isolation
v0.6.0 — ML-driven scheduling (Phases 2–4)
v0.8.0 — P2P mesh
v0.9.0 — iOS Core ML inference
v0.10.0 — Hypervisor backends
v0.11.0 — Managed services
v1.0.0 — Production readiness
```

Work that does not belong to a milestone (bug fixes, security patches, doc updates) is tracked under the rolling `[Unreleased]` CHANGELOG section and may ship in any patch release.

### 2.3 Priority Classification

| Class | Definition | Examples |
|-------|-----------|---------|
| **P0 — Blocker** | Production system down or money at risk | Double-spend, node crash loop, payment stuck |
| **P1 — Critical** | Serious bug with no workaround | HTLC cancel not firing, APNs auth broken |
| **P2 — High** | Significant correctness or security issue | Rate limiting absent, path traversal possible |
| **P3 — Medium** | Feature gap or quality improvement | Missing validation, sub-optimal scheduling |
| **P4 — Low** | Polish, documentation, documentation bugs | Doc comment clarity, CHANGELOG phrasing |

P0/P1 issues bypass the normal planning cycle and enter the [Emergency Hotfix Process](#11-emergency-hotfix-process).

### 2.4 Definition of Ready

A work item may not enter implementation until:

- [ ] Acceptance criteria are written and approved
- [ ] Subsystems affected are identified
- [ ] Payment/security impact assessed (dual-review flag set if yes)
- [ ] ROADMAP milestone assigned (or `[Unreleased]` confirmed)
- [ ] Estimated effort is in the issue (use PLAN.md effort bands: hours, not story points)
- [ ] Dependencies on other issues or external systems noted

---

## 3. Phase 2 — Architecture & Design

### 3.1 When a Design Document is Required

A written design document (ADR or design note in `docs/design/`) is **required** before implementation when any of the following apply:

- The change touches two or more subsystem boundaries
- A new wire protocol or file format is introduced
- A payment flow or HTLC lifecycle is modified
- An OPA Rego rule is added or changed in a way that affects `allow_*` decisions
- An ML feature vector dimension changes (breaks JSONL telemetry compatibility)
- A new external dependency is proposed (adds to `go.mod`)

For smaller changes (bug fixes, doc updates, single-package refactors) a design doc is optional.

### 3.2 Architecture Decision Record Format

File location: `docs/design/ADR-NNNN-short-title.md`

```markdown
# ADR-NNNN: Short Title

**Date:** YYYY-MM-DD
**Status:** Proposed | Accepted | Superseded by ADR-MMMM

## Context
What problem are we solving? What constraints exist?

## Decision
What did we decide to do?

## Consequences
What are the trade-offs? What new work does this create?

## Alternatives Considered
What else was evaluated and why was it rejected?
```

### 3.3 Payment Flow Design Requirements

Any change to the payment lifecycle (Stripe, Lightning, HTLC, metering) must include:

1. A sequence diagram showing the happy path and every error branch
2. Identification of the exact point at which money moves (irreversible state transition)
3. An idempotency strategy for network failures at that point
4. A rollback or compensation plan if downstream systems are unavailable

### 3.4 OPA Policy Design Requirements

New or modified Rego rules must:

1. Define the exact `input.*` schema the rule reads (document in rule comment)
2. Specify the default deny behavior when required fields are absent
3. Be accompanied by at least one `rego_test` file covering the happy path, a denial case, and a missing-field case

### 3.5 ML Feature Vector Compatibility

The JSONL telemetry file (`scheduler_telemetry.jsonl`) is a durable training dataset.  Any change to feature vector structure must:

1. Not change `NodeFeatureDim`, `TaskFeatureDim`, or `SystemFeatureDim` constants without a new constant name and a migration note in `internal/ml/features.go`
2. Increment the `ContextDim` only additively (append new features, never reorder existing ones)
3. Record the schema version and change date in a comment block in `features.go`

---

## 4. Phase 3 — Implementation Standards

### 4.1 Branch Strategy

```
main                    — always releasable; direct commits forbidden
  └── feature/<issue-number>-short-name     — normal feature work
  └── fix/<issue-number>-short-name         — bug fixes
  └── security/<issue-number>-short-name    — security patches (private until merged)
  └── hotfix/<issue-number>-short-name      — P0/P1 emergency fixes (see §11)
  └── release/v<major>.<minor>              — release stabilisation branch (optional)
```

Branch naming rules:
- Always include the GitHub issue number
- Use kebab-case after the prefix
- Security branches must not be named in a way that describes the vulnerability until the fix is merged

### 4.2 Go Coding Standards

**Formatting & linting:**
```bash
gofmt -w ./...                          # format (enforced in CI)
golangci-lint run ./...                 # static analysis (enforced in CI)
go vet ./...                            # compiler checks
```

No project-level `.golangci.yml` exists; the CI uses golangci-lint defaults.  When a linter suppression is needed, use `//nolint:ruleID` with an explanatory comment — never suppress without justification.

**Error handling:**
- All errors must be handled or explicitly returned; `//nolint:errcheck` is **forbidden** in new code
- Use sentinel errors (`var ErrFoo = errors.New(...)`) for conditions callers need to distinguish
- Wrap errors with context: `fmt.Errorf("package: operation: %w", err)`

**Concurrency:**
- Every exported type that will be used concurrently must document its thread-safety guarantee in its doc comment
- Mutexes must be declared adjacent to the fields they protect, with a comment identifying those fields
- Prefer per-object locks over package-level locks; avoid holding locks across I/O or channel operations
- Channel sends that could block an HTTP handler must use `select`/`default` with a fallback

**Build tags:**
- GUI code: `//go:build gui`
- Platform-specific: `//go:build linux` / `//go:build windows` / `//go:build darwin`
- Stub files (non-GUI no-ops): `//go:build !gui`
- Every platform-specific file must have a corresponding stub or build-guarded counterpart so the package compiles on all targets

**Package boundaries — forbidden import directions:**
```
ml → orchestration          FORBIDDEN (import cycle)
orchestration → ml          ALLOWED (orchestration is the consumer)
httpapi → orchestration     ALLOWED
payment → httpapi           FORBIDDEN
```

**Vendoring:**
```bash
go mod tidy && go mod vendor   # run after any go.mod change
```
All dependencies must be vendored.  Never check in changes to `go.mod` / `go.sum` without an accompanying `vendor/` update.

### 4.3 Security Implementation Requirements

The following rules apply to **all** new code, regardless of subsystem:

| Requirement | Rule |
|-------------|------|
| **Path inputs** | Any file path accepted from user input, config, or a network peer must be validated with `strings.Contains(path, "..")` at minimum; use `filepath.Clean` + a base-directory prefix check for write operations |
| **Shell execution** | `exec.Command("sh", "-c", ...)` is forbidden. Build argument slices directly: `exec.Command("binary", arg1, arg2)` |
| **Rate limiting** | Any new HTTP or WebSocket endpoint exposed to external clients must have per-IP rate limiting wired at registration time |
| **Secrets in logs** | Payment keys, Lightning macaroons, APNs private keys, and user credentials must never appear in `log.Printf` output. Use `"<redacted>"` placeholders |
| **Temporary files** | Use `os.CreateTemp` with a restrictive mode (`0600`); always defer cleanup |
| **Crypto** | Use `crypto/rand` for all random material; never `math/rand` for security-sensitive values |

### 4.4 Commit Message Convention

```
<type>(<scope>): <short imperative summary>

[Optional body explaining WHY, not WHAT]

Fixes #<issue-number>
```

Types: `feat`, `fix`, `security`, `refactor`, `test`, `docs`, `chore`, `perf`

Scopes: `orchestration`, `payment`, `ml`, `httpapi`, `storage`, `notification`, `wasm`, `gui`, `rego`, `mobile`, `ci`, `release`

Example:
```
security(httpapi): add per-IP rate limiter to mobile endpoints

/ws/nodes and /api/v1/nodes/mobile/register had no flood protection.
Added ipRateLimiter (sync.Mutex + map[string]*rlBucket) with a 1-minute
sliding window; /ws/nodes capped at 30/min, /register at 20/min.
X-Forwarded-For respected for reverse-proxy deployments.

Fixes #312
```

---

## 5. Phase 4 — Testing Strategy

### 5.1 Test Pyramid

```
                    ┌───────────────┐
                    │   E2E / Demo  │  (manual; pre-release only)
                  ┌─┴───────────────┴─┐
                  │  Integration Tests │  test/integration/ — real SQLite, real OPA
                ┌─┴───────────────────┴─┐
                │    Unit Tests          │  internal/*/**_test.go — mocked deps
              ┌─┴─────────────────────────┴─┐
              │   Static Analysis / Security  │  golangci-lint, gosec, govulncheck
              └───────────────────────────────┘
```

### 5.2 Unit Test Requirements

Every new package must have:
- At least one `_test.go` file in the same package
- Table-driven tests for all public functions with more than one code path
- Use of `t.Helper()` in assertion helpers
- Compatibility with `-short` flag (skip network calls, skip slow loops)

```bash
# CI command (fast — runs in <30s per package):
go test -short -race ./...

# Full suite (with race detector — required before PR merge):
go test -race ./internal/...
```

### 5.3 Integration Test Requirements

Location: `test/integration/`

Integration tests use real subsystems (SQLite, OPA engine) but mock external services (Stripe API, LND gRPC, APNs).  They must:
- Be guarded with `//go:build integration` tag OR run cleanly without the tag when `-short` is not set (current convention)
- Clean up all created files and DB entries in `t.Cleanup`
- Not require network access to external services

### 5.4 Payment-Path Tests

Any change to `internal/payment/`, `internal/payment/htlc.go`, or `internal/ml/telemetry.go` reward functions requires:

- A test that exercises the exact financial state transition (charge created → confirmed → settled)
- A test that exercises the failure / cancellation path
- Verification that `ErrTimeout`, `ErrDeviceTokenInvalid`, and other sentinel errors propagate correctly to callers

Use Stripe test-mode API keys for Stripe tests; use a mock `LightningProcessor` interface implementation for Lightning tests.

### 5.5 OPA Policy Tests

Every new or modified Rego rule requires a corresponding `.rego` test file in `configs/policies/`:

```rego
# configs/policies/resource_sharing_test.rego
package resource_sharing_test
import data.resource_sharing

test_allow_htlc_cancel_valid {
    resource_sharing.allow_htlc_cancel with input as {
        "action": "htlc_cancel",
        "coordinator_did": "did:key:z6Mk...",
        "cancel_reason": "node_timeout",
    }
}

test_allow_htlc_cancel_missing_reason_denied {
    not resource_sharing.allow_htlc_cancel with input as {
        "action": "htlc_cancel",
        "coordinator_did": "did:key:z6Mk...",
    }
}
```

Run with: `opa test configs/policies/`

### 5.6 Platform Test Matrix

| Platform | Run in CI | Run locally before PR |
|----------|-----------|----------------------|
| Linux (ubuntu-latest, amd64) | ✅ Always | ✅ |
| macOS (macos-latest) | ✅ Always | If macOS available |
| Windows (windows-latest) | ✅ Always | If Windows available |
| Linux ARM64 (Raspberry Pi) | ⚠️ Manual only | Optional |

The CI test matrix runs `go test -v -race ./internal/...` on all three OS targets (`.github/workflows/test.yml`).  A PR may not merge if any OS target fails.

### 5.7 Security Tests

Run before every release and after any security-related change:

```bash
# Static security analysis
gosec -severity high ./...

# Known vulnerability check
govulncheck ./...

# Linting (includes shadow, errcheck, staticcheck)
golangci-lint run ./...

# OPA policy tests
opa test configs/policies/
```

The CI security job (`.github/workflows/test.yml` → `security`) runs gosec in SARIF mode and uploads to the GitHub Security tab.  SARIF findings of severity HIGH or CRITICAL must be resolved before release.

### 5.8 Definition of Done (Testing)

A work item is done when all of the following pass on the PR branch:

- [ ] `go build -tags "!gui" ./...` — clean
- [ ] `go test -short -race ./...` — all pass
- [ ] `go test -race ./internal/...` — all pass (full suite, all three OS in CI)
- [ ] `golangci-lint run ./...` — no new issues
- [ ] `gosec -severity high ./...` — no new HIGH/CRITICAL findings
- [ ] OPA tests pass if Rego was changed: `opa test configs/policies/`
- [ ] Manual smoke test on affected subsystem (described in PR)

---

## 6. Phase 5 — Code Review

### 6.1 PR Requirements

Every PR must:
- Reference the GitHub issue it closes (`Closes #NNN`)
- Have a description that explains the **why**, not just the what
- Include a **Test Plan** section describing what was manually verified
- Update `CHANGELOG.md` under `[Unreleased]` with an appropriate entry
- Pass all CI checks before requesting review

### 6.2 Reviewer Assignment

| Subsystem | Required reviewers | Note |
|-----------|-------------------|------|
| `internal/payment/` | 2 reviewers (one designated payment owner) | Financial correctness risk |
| `internal/payment/htlc.go` | 2 reviewers + security pass | HTLC lifecycle = irreversible money movement |
| `configs/policies/*.rego` | 1 reviewer familiar with OPA | Policy change can silently allow or deny operations |
| `internal/orchestration/` | 1 reviewer | Scheduler bugs affect all nodes |
| `internal/ml/` | 1 reviewer | Feature vector changes break telemetry compatibility |
| `internal/httpapi/` | 1 reviewer | Public API surface; rate limiting and validation must be verified; dashboard FS wiring |
| `internal/notification/apns.go` | 1 reviewer | APNs auth failures silently drop push notifications |
| All other packages | 1 reviewer | Standard |

### 6.3 Code Review Checklist

Reviewers must verify:

**Correctness**
- [ ] Logic matches the stated acceptance criteria
- [ ] Error paths are handled (no dropped errors)
- [ ] Concurrency: shared state protected by appropriate lock; no lock held across I/O
- [ ] No goroutine leaks (every goroutine has a stop mechanism)

**Security**
- [ ] No user-controlled string passed to `exec.Command("sh", "-c", ...)`
- [ ] File paths from external inputs are validated against traversal
- [ ] New HTTP endpoints have rate limiting
- [ ] No secrets logged
- [ ] Crypto: `crypto/rand` used, not `math/rand`

**Platform**
- [ ] Build tags are correct; package compiles on all three OS targets
- [ ] No OS-specific syscall without a guarded stub

**Payment (if applicable)**
- [ ] Irreversible state transition is identified and protected by idempotency
- [ ] Sentinel errors propagate to callers correctly
- [ ] HTLC hash/preimage encoding is base64 (not hex) for LND wire format

**Tests**
- [ ] New code paths have test coverage
- [ ] Tests are `-short` compatible
- [ ] No test writes to paths outside `t.TempDir()`

**Documentation**
- [ ] Exported types and functions have doc comments
- [ ] `CHANGELOG.md` updated
- [ ] `ROADMAP.md` prerequisite list updated if a milestone item was completed

### 6.4 Merge Policy

- Squash-merge is the default for feature branches (keeps `main` history linear)
- Merge commits are allowed for release branches
- Force-push to `main` is forbidden
- The author may not self-approve; at least one reviewer must be a different person

---

## 7. Phase 6 — Security Gates

### 7.1 Gate Overview

Security gates are checkpoints that must pass before work advances:

| Gate | Trigger | Blocker? |
|------|---------|---------|
| **G1** Static analysis (gosec) | Every PR | Yes — HIGH/CRITICAL findings |
| **G2** Vulnerability scan (govulncheck) | Every PR | Yes — known CVEs in direct deps |
| **G3** Dependency audit | Every `go.mod` change | Yes — new dep requires review |
| **G4** Dual payment review | Any payment path change | Yes |
| **G5** Threat model review | Design doc for new external surface | Yes |
| **G6** Penetration test | Before each major milestone release | No (track as P2 finding) |

### 7.2 Dependency Audit Process (G3)

When a new dependency is proposed:

1. Check license (must be Apache-2.0, MIT, BSD, or ISC — GPL-only deps are incompatible with AGPL outbound)
2. Check CVE history: `govulncheck` + manual check on OSV.dev
3. Verify the dep is actively maintained (last commit < 12 months)
4. Confirm the dep is vendorable (`go mod vendor` succeeds)
5. Record the decision in the PR description

### 7.3 Open Security Findings Tracking

All open findings from the February 2026 audit are tracked in `SECURITY_SUMMARY.md`.  No release may ship as a production release while a P0/P1 security finding remains open.

**Current open findings (as of 2026-03-03):**

| Finding | File | Priority | Status |
|---------|------|----------|--------|
| Command injection | `internal/compute/apparmor.go` | 🔴 CRITICAL | Fix ready — not yet applied |
| Path traversal | `internal/did/keygen.go`, `internal/accounting/collector.go` | 🔴 CRITICAL | Fix ready — not yet applied |
| Windows key permissions | `internal/did/keygen.go` | 🔴 CRITICAL | Fix ready — not yet applied |
| RADIUS rate limiting | `internal/radius/server.go` | 🟠 HIGH | Mobile endpoints done; RADIUS open |

These must be closed before v1.0.0.

### 7.4 Security Disclosure Policy

Security vulnerabilities must be reported privately (direct message to maintainer, not a public GitHub Issue).  Response SLA: 24 hours acknowledgement, 7 days for initial assessment, 30 days target remediation.  Public disclosure follows coordinated vulnerability disclosure (CVD) after a fix is merged and released.

---

## 8. Phase 7 — Release Management

### 8.1 Version Scheme

SoHoLINK follows [Semantic Versioning](https://semver.org/):

```
v<MAJOR>.<MINOR>.<PATCH>

MAJOR — incompatible wire-protocol or public API break
MINOR — new milestone shipped (aligned with ROADMAP milestones)
PATCH — bug fixes, security patches, documentation
```

Current version: **0.1.0** (shipped 2026-03-01).  While `MAJOR == 0`, minor bumps may contain breaking changes as the protocol stabilises; this will be noted explicitly in the CHANGELOG.

### 8.2 Release Checklist

**Pre-release (feature freeze):**
- [ ] All milestone items in ROADMAP.md marked ✅ or explicitly deferred with a note
- [ ] `CHANGELOG.md` `[Unreleased]` section promoted to `[vX.Y.Z] — YYYY-MM-DD`
- [ ] All open P0/P1 bugs resolved or milestone-deferred with documented justification
- [ ] Security gate G1 + G2 pass on the release branch HEAD
- [ ] Full test suite passes on all three OS targets with race detector
- [ ] OPA policy tests pass
- [ ] `go mod tidy && go mod vendor` run; no uncommitted changes
- [ ] Version in Makefile `VERSION ?= X.Y.Z` updated
- [ ] Git tag created: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`

**Build:**
```bash
# Snapshot (for staging / pre-release testing):
make dist

# Real release (requires tag and GITHUB_TOKEN):
make dist-release
```

GoReleaser produces:
- `soholink_vX.Y.Z_linux_amd64.tar.gz` (CLI)
- `soholink_vX.Y.Z_darwin_amd64.tar.gz` / `arm64.tar.gz` (CLI)
- `soholink_vX.Y.Z_windows_amd64.zip` (CLI)
- `soholink-gui_vX.Y.Z_windows_amd64.zip` (GUI — static MinGW)
- `soholink-gui_vX.Y.Z_macOS_universal.tar.gz` (GUI — lipo universal)
- `soholink-gui_vX.Y.Z_linux_amd64.tar.gz` (GUI)
- `sololink_X.Y.Z_amd64.deb` / `.rpm` (Linux packages)
- `FedAAA-vX.Y.Z-x86_64.AppImage`
- `SoHoLINK-Setup.exe` (NSIS installer)
- `sololink_vX.Y.Z_checksums.txt` (SHA256)

**Post-release:**
- [ ] GitHub Release created with notes auto-generated by GoReleaser
- [ ] `ROADMAP.md` milestone status updated (✅ shipped + date)
- [ ] `SECURITY_SUMMARY.md` updated if any security items were resolved
- [ ] Dependabot + `update-deps.yml` workflow re-enabled for next cycle (if paused)

### 8.3 Patch Release Process

For bug fixes and security patches between minor releases:

1. Branch from the release tag: `git checkout -b hotfix/vX.Y.Z+1 vX.Y.Z`
2. Apply the fix with a minimal, focused commit
3. Run the full test suite
4. Tag `vX.Y.Z+1` and run `make dist-release`
5. Cherry-pick the fix commit onto `main`

### 8.4 Platform Smoke Tests (Pre-Release)

Before publishing a release, manually verify on each platform:

| Test | Windows | Linux | macOS |
|------|---------|-------|-------|
| CLI `fedaaa --version` prints correct version | ✅ | ✅ | ✅ |
| `fedaaa start` → `GET /api/health` returns `{"status":"ok"}` | ✅ | ✅ | ✅ |
| `GET /` redirects to `/dashboard` | ✅ | ✅ | ✅ |
| Dashboard loads all 5 screens without JS error | ✅ | ✅ | ✅ |
| `GET /api/status` returns valid JSON with `uptime_seconds` > 0 | ✅ | ✅ | ✅ |
| Hardware detection returns non-zero CPU/RAM (wizard) | ✅ | ✅ | ✅ |
| OPA policy evaluation returns expected decisions | ✅ | ✅ | ✅ |
| `scripts\install-service.ps1` installs and `uninstall-service.ps1` removes | ✅ | — | — |
| AppImage runs on Ubuntu without install | — | ✅ | — |
| `.pkg` installs cleanly | — | — | ✅ |

---

## 9. Phase 8 — Operations & Feedback

### 9.1 Issue Triage (Weekly)

Every open GitHub Issue is reviewed weekly and assigned to a milestone or labeled `wont-fix` / `needs-info`.  Issues with no response from the reporter after 14 days are closed with a `needs-info` label and a comment.

### 9.2 Dependency Updates

`dependabot.yml` runs weekly updates for Go modules and GitHub Actions, grouped by ecosystem (Fyne, OTel, Prometheus, spf13, etc.).

The `update-deps.yml` workflow runs monthly: `go get -u ./...` → full test suite → opens a consolidated PR only if tests pass.

All dependency update PRs require:
- Full CI pass (all three OS targets)
- Security gate G2 (govulncheck on the updated dep set)
- Reviewer confirmation that no breaking API changes were introduced

### 9.3 Monitoring (Current & Planned)

| Capability | Status | Target milestone |
|-----------|--------|-----------------|
| `log.Printf` structured logging | ✅ Available now | — |
| Prometheus metrics (`/metrics`) | ⚠️ OTel plumbing present; dashboards not configured | v1.0.0 |
| OTel distributed tracing | ⚠️ Planned | v1.0.0 |
| APNs delivery rate monitoring | ⚠️ 410 errors exposed via `ErrDeviceTokenInvalid` | v0.4.0 |
| HTLC settle/cancel rate (bandit telemetry) | ✅ JSONL telemetry shipped | v0.6.0 dashboards |
| Alerting / IDS | ⬜ Not started | Post v1.0.0 |

### 9.4 Post-Incident Review

For any P0/P1 incident (money lost, node crash, security breach):

1. Immediate: stop the bleeding (patch or rollback)
2. Within 48 hours: draft a timeline of events
3. Within 1 week: root cause analysis with corrective actions
4. Within 2 weeks: corrective actions committed to the backlog with priority P0/P1

Post-incident reviews are stored in `docs/incidents/YYYY-MM-DD-short-title.md`.

---

## 10. Subsystem-Specific Requirements

### 10.1 Payment Subsystem (`internal/payment/`)

- **Idempotency:** Every charge creation must be idempotent on retry (use Stripe idempotency keys; Lightning preimage is inherently idempotent)
- **HTLC encoding:** All `hash`, `preimage`, and `payment_hash` fields sent to LND's REST gateway must be standard base64 (not hex)
- **Metering:** The per-hour billing loop (`meter.go`) must guard against negative elapsed time (`elapsed < 0`) before any charge is issued
- **Ledger:** All financial events must be recorded in the accounting ledger before the charge is attempted (write-ahead logging pattern)
- **Tests:** Use Stripe test-mode keys; never real keys in CI

### 10.2 Orchestration & Scheduler (`internal/orchestration/`)

- **Shadow IDs:** Shadow workload IDs must include a unique suffix (e.g., nanosecond timestamp) to prevent collision on concurrent dispatch
- **Active workloads map:** All reads and writes to `ActiveWorkloads` must hold the appropriate lock (`mu.RLock` for reads, `mu.Lock` for writes); deep-copy slices before releasing the lock
- **Arm index:** `ScheduleMobile` arm selection must use unsigned modulo arithmetic to avoid negative index panics on 32-bit platforms
- **ML fallback:** If `mlBandit` is nil or `Select()` returns an error, fall back to uniform random selection — never panic

### 10.3 Mobile Hub (`internal/httpapi/mobilehub.go`)

- **Register channel:** All sends on `h.register` must be non-blocking (`select`/`default`); HTTP handlers must never block on the hub event loop
- **Close once:** `close(client.send)` must always go through `client.closeOnce.Do(...)` to prevent double-close panics
- **LastSeen updates:** Use per-client `seenMu` (not hub write lock) for `refreshLastSeen`
- **Context propagation:** `Hub.Run` must accept `context.Context` and exit cleanly on cancellation

### 10.4 ML Telemetry (`internal/ml/`)

- **Pending records:** `RewardFor(OutcomePending, ...)` returns 0.0.  This value must **never** be passed to `bandit.Update()` — doing so incorrectly penalises the arm before the outcome is known
- **Path safety:** `NewTelemetryRecorder` must reject paths containing `..`
- **Write errors:** All write and flush errors in `writeOne` must be logged; never silently dropped
- **Feature vector stability:** Do not reorder elements in `NodeFeatures`, `TaskFeatures`, or `SystemFeatures` — append only

### 10.5 Wasm Executor (`internal/wasm/`)

- **Timeout context:** `timedExecutor.Execute` must check `tctx.Err()` (the child context with deadline), not the parent `ctx.Err()`
- **Sentinel error:** Wrap timeout errors with `ErrTimeout` via `%w` so callers can use `errors.Is`
- **Stub executor:** `StubExecutor` is test/dev only; production wiring must use a real wazero implementation (v0.3.0 milestone)

### 10.6 APNs Notifications (`internal/notification/apns.go`)

- **JWT timing:** Capture `now := time.Now()` before calling `mintJWT(now)` so that `tokenExpAt` and the JWT `iat` claim use the same timestamp
- **410 handling:** HTTP 410 Gone from APNs must be exposed as `ErrDeviceTokenInvalid` so callers can purge the stale token from storage
- **Key rotation:** The APNs private key path is config-driven; never hardcode

### 10.7 OPA Policies (`configs/policies/`)

- **Input validation:** Every `allow_*` rule must check that required input fields are non-empty before granting access
- **New operations:** Any new coordinator action (HTLC lifecycle event, preemption, etc.) must have a corresponding `allow_*` rule before the operation is used in production code
- **Reason allowlists:** Use explicit sets (`valid_*_reasons`) rather than open string matches for action reason fields

### 10.8 Multi-Platform Build (`cmd/`, build tags)

- **Headless entry point:** `cmd/fedaaa/main.go` — single self-contained binary; no external `configs/` or `ui/` directory required at runtime
- **GUI entry point:** `cmd/soholink/main.go` with `//go:build gui` — no args → GUI; with args → `cli.Execute()`
- **CGO discipline:** CGO is only enabled for GUI builds; all non-GUI packages must compile with `CGO_ENABLED=0`
- **Static linking (Windows):** Production Windows GUI uses `-extldflags "-static-libgcc -static-libstdc++ -static-libpthread"` to produce a zero-DLL-dependency binary
- **LDFLAGS injection:** `main.version`, `main.commit`, `main.buildTime` must be populated on all release builds
- **Embedded assets:** `embed.go` holds `//go:embed` directives for `configs/policies`, `ui/dashboard`; `cmd/fedaaa/main.go` must call `policy.SetEmbeddedFS()` and `cli.SetDashboardFS()` before `cli.Execute()`

### 10.9 Local Web Dashboard (`ui/dashboard/`, `internal/httpapi/dashboard.go`)

- **Technology:** Embedded HTML/CSS/JS served from `fedaaa.exe` at `http://localhost:8080/dashboard` — not a Fyne native app and not a cloud SaaS
- **5 screens:** Dashboard (radial dials), Plan Work, Management (income/payments), Help, Settings
- **Asset embedding:** `//go:embed ui/dashboard` in `embed.go`; `fs.Sub(soholink.DashboardFS, "ui/dashboard")` strips prefix before passing to server; use `http.ServeFileFS` (Go 1.22+) for Range, ETag, and caching
- **No CDN dependencies:** All JS/CSS must be embedded in the binary — no external network requests at runtime
- **Hash-router:** Unknown paths under `/dashboard/` fall through to `index.html`; the JS hash-router (`location.hash`) handles tab selection client-side
- **API contract:** `/api/status` returns node stats as JSON; zero-values are valid when sub-systems are unavailable — the frontend must handle zeros gracefully
- **PowerShell scripts:** Always use ASCII hyphens (`-`) in `.ps1` files; UTF-8 em dash (`—`) becomes CP1252 byte `0x94` in PS5.1, which terminates string literals prematurely

---

## 11. Emergency Hotfix Process

Used for P0/P1 issues only (money at risk, production outage, critical security vulnerability).

```
1. Triage (< 1 hour)
   - Confirm severity with a second engineer
   - Create a private GitHub Issue (security) or public Issue (crash/money)
   - Notify the team immediately

2. Branch (< 2 hours)
   - Branch from the affected release tag:
     git checkout -b hotfix/vX.Y.Z+1 vX.Y.Z
   - For security issues: use a non-descriptive branch name until patched

3. Fix & Test (< 24 hours for P0, < 72 hours for P1)
   - Implement the minimal fix — no refactoring, no features
   - Run: go test -race ./...
   - Run: gosec -severity high ./...
   - Manual smoke test on the affected subsystem

4. Review (expedited — 2 reviewers, 4-hour SLA for P0)
   - Payment / security changes still require dual review
   - Review checklist applied as normal — do not skip

5. Release
   - Merge to hotfix branch
   - Tag vX.Y.Z+1
   - make dist-release
   - Publish GitHub Release with hotfix notes

6. Backport
   - Cherry-pick the fix commit onto main
   - Add CHANGELOG entry under [Unreleased]

7. Post-incident review (see §9.4)
```

---

## 12. Toolchain Reference

### Commands

| Task | Command |
|------|---------|
| Build headless CLI | `make build-cli` |
| Build GUI (requires GCC) | `make build-gui` |
| Static Windows binary | `make build-static-windows` |
| Cross-compile Linux ARM64 | `make build-pi` |
| All-platform release packages | `make dist` |
| Tagged release | `make dist-release` |
| Short test suite (fast, CI-safe) | `make test-short` |
| Full test suite with race detector | `make test` |
| Lint | `make lint` |
| Update vendor directory | `go mod tidy && go mod vendor` |
| Security scan | `gosec -severity high ./...` |
| Vulnerability check | `govulncheck ./...` |
| OPA policy tests | `opa test configs/policies/` |

### Key Files

| File | Purpose |
|------|---------|
| `CHANGELOG.md` | All changes; `[Unreleased]` is the active section |
| `ROADMAP.md` | Milestone-level feature planning |
| `PLAN.md` | Legacy gap analysis (historical; do not add new items) |
| `SECURITY_SUMMARY.md` | Open security findings and their current status |
| `DEVELOPMENT.md` | Developer setup and project structure guide |
| `SDLC.md` | This document |
| `configs/policies/resource_sharing.rego` | OPA authorization rules |
| `.github/workflows/build.yml` | GoReleaser CI (Linux + cross-compile to Windows/macOS) |
| `.github/workflows/test.yml` | Test matrix (Linux, macOS, Windows) + lint + gosec |
| `.goreleaser.yml` | Multi-platform build and packaging spec |
| `Makefile` | Developer build targets |

### CI Pipeline Summary

```
PR opened / push to main or develop
    │
    ├── test.yml
    │     ├── test (matrix: ubuntu, macos, windows)
    │     │     └── go test -v -race ./internal/...
    │     ├── lint (ubuntu)
    │     │     └── golangci-lint run ./... --timeout=5m
    │     └── security (ubuntu)
    │           └── gosec -no-fail -fmt sarif ./... → GitHub Security tab
    │
    └── build.yml (after test passes)
          └── goreleaser (ubuntu, cross-compiles for all platforms)
                ├── snapshot build (non-tag push)
                └── real release (v* tag push) → GitHub Release + artifacts
```

### Go Build Tags Reference

| Tag | Effect |
|-----|--------|
| `gui` | Includes Fyne GUI; requires CGO + GCC |
| `!gui` | Excludes Fyne; CGO-free; default for CLI |
| `linux` | Linux-only platform code |
| `windows` | Windows-only platform code |
| `darwin` | macOS-only platform code |
| `integration` | Integration tests (used in test.yml) |

---

*This document should be reviewed and updated at the start of each milestone cycle.*
*Owner: SoHoLINK maintainers*
*Last updated: 2026-03-04*
