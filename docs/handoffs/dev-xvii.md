# SoHoLINK Dev XVII Handoff
**Branch:** master · **HEAD:** `189800b` · **Build:** clean · **Date:** 2026-05-09
**Previous handoff:** Dev XVI

## What was accomplished this session

Drove to first end-to-end self-test attempt. Found and fixed two latent blockers that would have prevented every fresh Windows install permanently, then discovered a third blocker we could not resolve in this session.

**Self-test result:** never completed. Stopped at the Windows service start step on both NTARIHQ and NTARI1, with consistent SCM 30-second timeout. Working hypothesis: unsigned binary blocked from executing in the service security context. Likely ties directly to TODO 19 (SignPath), making SignPath approval the critical path for participant testing.

**Top priority for next session:** Either install the latest MSI on NTARI1 to confirm whether commit `189800b` makes the installer complete silently (leaving services unstarted), or wait for SignPath approval and rebuild with a signed binary and re-attempt.

## Commits

| Hash | Subject |
|---|---|
| `189800b` | fix(installer): set ErrorControl=ignore on both ServiceInstall elements |
| `e5ae9a4` | fix(installer): remove Start=install from ServiceControl |
| `fb12f20` | fix(installer): change ServiceControl Wait=yes to Wait=no |
| `f4d2c34` | fix(agent): wrap agent.Detect in 15s timeout to unblock WMI hangs on Windows |
| `6a0a72f` | fix(agent,api,installer): fix first-run SPIRE bootstrapping deadlock |

## SPIRE bootstrapping deadlock — fixed (`6a0a72f`)

Three architectural problems on the first-run path, all fixed in one commit.

`POST /nodes/claim` was behind `RequireSPIFFE` middleware on the orchestrator, but the agent needed `/nodes/claim` to receive its SPIRE join token. The agent couldn't get the token without SPIRE, and couldn't bootstrap SPIRE without the token. Endpoint moved to the plain top mux next to `/health` and `/allowlist`; registration token is the auth for this endpoint.

The agent claim path tried to use an mTLS client via `identity.NewSource` on the SPIRE socket. Same chicken-and-egg. Replaced with a plain HTTPS client for the claim call only. mTLS still used for everything post-claim.

After writing `spire-agent.conf`, the agent now calls `sc start SoHoLINKSPIREAgent` on Windows (the service had failed at install time with no config) and waits up to 90 seconds for the SPIRE socket to become available via a new `waitForSPIRE` helper. This wait runs on every startup, not just first-run, so restart-after-reboot is also covered.

`SPIFFE_ENDPOINT_SOCKET` in the WiX installer was hardcoded to `unix:///run/spire/sockets/agent.sock` (Linux). Changed to `npipe:spire-agent` per go-spiffe v2.6.0 Windows address format. `spire-agent.conf` `socket_path` field receives the Win32 form `\\.\pipe\spire-agent` via `strings.HasPrefix(spiffeSocket, "npipe:")` conversion in `cmd/agent/main.go`.

## WMI hardware detection hang — fixed (`f4d2c34`)

`agent.Detect` called `cpu.InfoWithContext(ctx)` first, which runs `Win32_Processor` via gopsutil's WMI client on Windows. The call ignored context cancellation in practice and blocked indefinitely on NTARI1 (verified by console-mode test: agent hung silently >120 seconds before any slog output). This was the root cause of the original SCM service timeouts.

Added `detectHW` wrapper in `cmd/agent/main.go` that runs `agent.Detect` in a goroutine with a hard 15-second timeout via `select`. On timeout, returns a minimal `HardwareProfile{Platform: runtime.GOOS, Arch: runtime.GOARCH}` and logs a warning. Non-fatal — node can still claim and heartbeat with partial hardware. Hung WMI goroutines complete in the background. Both `agent.Detect` call sites in `runMain` converted.

## Dev allowlist deployed

The build script warned `ALLOWLIST_PUBLIC_KEY not set; agent will fail at first allowlist fetch (dev build)`. Confirmed via reading `internal/agent/allowlist.go`: `Verify()` returns `ErrAllowlistNoKey` when the public key is unset, which is a fatal `os.Exit(1)` in main.

Steps taken:
- Generated dev keypair via `scripts/allowlist-genkey/main.go -priv allowlist-dev.priv -pub allowlist-dev.pub`
- Public key: `2wB993q6Kh9YtMD3BMY2b+FZB/AAmf62PIb2a6I+Hqc=`
- Created minimal unsigned allowlist `{"version":1,"issued_at":"2026-05-09T00:00:00Z","entries":[]}`
- Signed with `scripts/allowlist-sign` → `allowlist-dev-signed.json`
- Deployed to `deploy/allowlist/allowlist.json` (bind-mounted into orchestrator at `/etc/soholink/`)
- Verified live via `docker exec soholink-orchestrator-1 wget -qO- --no-check-certificate https://localhost:8082/allowlist`
- MSI rebuilt with `$env:ALLOWLIST_PUBLIC_KEY = "2wB993q6Kh9YtMD3BMY2b+FZB/AAmf62PIb2a6I+Hqc="`

This effectively prototypes the production keypair-and-deploy flow that's still blocked on worker image existence in TODO 14.

## WiX service-start dialog — three iterations, none resolved

Same dialog ("Service 'SoHoLINK Node Agent' (SoHoLINKAgent) failed to start. Verify that you have sufficient privileges to start system services") on every install attempt on both NTARIHQ and NTARI1.

Three commits attempting suppression:
- `fb12f20` — `Wait="yes"` → `Wait="no"` on both ServiceControl elements. No effect.
- `e5ae9a4` — Removed `Start="install"` from both ServiceControl elements entirely. No effect.
- `189800b` — `ErrorControl="normal"` → `"ignore"` on both ServiceInstall elements. **Not tested.**

The first two failures mean the dialog originates from somewhere other than the ServiceControl table — most likely WiX 4 auto-generating service-start entries from `ServiceInstall`, or the MSI engine surfacing the failure via the ServiceInstall `ErrorControl` field independent of our ServiceControl settings. `ErrorControl="ignore"` in commit `189800b` should suppress at the MSI engine level. Whether it works is the first thing to confirm next session.

The deeper question remains: **even if the dialog is suppressed, why won't the service actually start?** Console execution of the binary works (it only hangs at WMI, now fixed). Service execution fails consistently across two machines. The most defensible hypothesis is that Windows Smart App Control / WDAC blocks unsigned binaries from executing in the service security context regardless of their interactive-execution status. If that's right, no further WiX configuration will help — only signing will.

## Production state

Stack healthy. 7 containers up. HEAD `189800b` on master, pushed to origin.

| URL | Status |
|---|---|
| `soholink.org/` | 200 |
| `soholink.org/download` | 200 |
| `soholink.org/privacy` | 200 |
| `api.soholink.org/health` | 200, identity ready |
| `api.soholink.org/allowlist` | 200 (signed dev allowlist) |
| `soholink.org/static/SoHoLINK-Setup.msi` | 200, ~16 MB, current build with ALLOWLIST_PUBLIC_KEY injected |

Orchestrator rebuilt mid-session from `6a0a72f` to pick up the `/nodes/claim` plain-mux routing.

## Outstanding work, by priority

**1. Resolve the service-start blocker.**
First action: test the current MSI (`189800b`) on NTARI1. If the wizard completes silently with the install succeeding, run `sc query SoHoLINKAgent` and `sc start SoHoLINKAgent` from an elevated prompt to capture the actual Windows error code instead of the SCM timeout. That error code is the data we don't have.

If that fails, the diagnosis is almost certainly Smart App Control blocking unsigned service execution, and the resolution path is SignPath approval (TODO 19).

**2. Carryovers from Dev XVI still apply.**
End-to-end self-test (blocked on item 1). TODO 19 SignPath approval pending. TODO 20 sign-verify roundtrip ready to implement anytime. TODO 18 Cloudflare tunnel config sync script deferred. `handleGenerateNodeToken` gRPC refactor still queued. `handleHeartbeat` variable rename still queued. Phase C legacy v1 cleanup deferred.

**3. New small wins available without waiting on SignPath.**
- Installer UI bitmaps are near-black with black text in `build.ps1` (`R:10 G:13 B:18` for banner, `R:17 G:24 B:32` for dialog). TODO 4 already covers branded artwork; quick interim is light-background colors so the wizard text is readable.
- Add `allowlist-dev.priv` to `.gitignore`. The private key is currently sitting in the repo root untracked.
- Commit `deploy/allowlist/allowlist.json` so the dev allowlist is reproducible across machines.
- TODO 20 sign-verify roundtrip — tiny defensive commit in `cmd/portal/main.go`.

**4. Phase progression.**
B6 is done (2026-05-05). The actual next product workstreams are **B4** (print job confirmation flow, closes TODO 9) and **B5** (long-running job lifecycle, closes TODOs 5 and 6). Both are unblocked by anything we discovered today.

## Process notes from this session

**Memory drift on phase status.** Memory said B6 was 2 of 4 commits in progress; CLAUDE.md showed it complete since 2026-05-05. Same drift pattern noted in Dev XVI handoff. Reconcile CLAUDE.md against memory at session start, not just against the prior handoff.

**Three WiX commits without a confirmed diagnosis.** We iterated `Wait=no` → no `Start=install` → `ErrorControl=ignore` without ever inspecting the generated MSI's `ServiceControl` and `ServiceInstall` tables to confirm what was actually changing. Next session, before any more WiX changes, extract the MSI with `msiexec /a` and inspect the relevant MSI tables to see what's really there.

**Claude Code's Read tool output doesn't transfer to chat.** Repeated lesson from Dev XV/XVI. The Read tool renders to a VS Code panel that doesn't survive the paste. Always specify `cat` or `sed` via bash explicitly.

**WiX `ServiceControl` and `ServiceInstall` are two different MSI tables.** WiX 4 may auto-generate ServiceControl rows from ServiceInstall metadata. Inspect the MSI before making further assumptions.

## Opening reads for next session

```bash
cd "C:/Users/<user>/Documents/SoHoLINK"
git log --oneline -8 && docker compose ps && go build ./cmd/... && echo "all clean"
```

If picking up the service-start investigation:

```bash
# Extract MSI to inspect tables
msiexec /a "web/static/SoHoLINK-Setup.msi" /qn TARGETDIR="C:/MSIExtract"
# Then inspect ServiceControl and ServiceInstall tables
# Recommended tool: lessmsi.exe (or Orca from Windows SDK)
```

If picking up TODO 20 sign-verify roundtrip:

```bash
grep -n "mustEd25519Key" "C:/Users/<user>/Documents/SoHoLINK/cmd/portal/main.go"
```

If picking up B4 or B5 — read the phase descriptions in CLAUDE.md lines 587–600 and scope the first commit.

## Secrets

`allowlist-dev.priv` is a dev-only ed25519 private key currently in the repo root, untracked. **Gitignore before the next commit.** It corresponds to public key `2wB993q6Kh9YtMD3BMY2b+FZB/AAmf62PIb2a6I+Hqc=` which is embedded in the current MSI binary. Rotation: regenerate via `go run scripts/allowlist-genkey/main.go`, re-sign the allowlist, redeploy `deploy/allowlist/allowlist.json`, and rebuild the MSI with the new public key.

`SESSION_PRIVATE_KEY` and other production secrets unchanged this session.

---

*End of Dev XVII Handoff. Master at `189800b`. Three real bugs fixed; one new blocker open and likely tied to SignPath. Next session: confirm whether the latest MSI completes the install silently, and if so capture the actual service start error code via `sc start`.*
