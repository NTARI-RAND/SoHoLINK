# Allowlist Signing Runbook

This runbook covers the operator procedure for managing the SoHoLINK signed
allowlist: bootstrapping the production Ed25519 signing keypair, building and
signing allowlist documents, deploying signed files to the orchestrator, and
rotating keys when needed.

## Background

The allowlist is the signed authority that tells every SoHoLINK agent which
container images it may run. Each agent verifies the signature on every
allowlist fetch using a public key baked into the agent binary at build time.
The orchestrator serves the signed allowlist file via `GET /allowlist`. The
agent never trusts an unverified allowlist — there is no "skip verification"
fallback.

The signing keypair is the production root of trust for workload identity.
Loss of the private key means all current agents must be rebuilt with a new
public key and redistributed. Treat the private key accordingly.

## Tools

Two operator binaries live under `scripts/`:

- `allowlist-genkey` — generates a fresh Ed25519 keypair (one-time bootstrap)
- `allowlist-sign` — signs an unsigned allowlist JSON with the private key

Build them once:
```
go build -o allowlist-genkey.exe ./scripts/allowlist-genkey
go build -o allowlist-sign.exe ./scripts/allowlist-sign
```

## One-time keypair bootstrap

Run this once per deployment lifetime. Subsequent allowlist publications
reuse the same keypair until rotation.

### 1. Generate the keypair
```
allowlist-genkey -priv allowlist-priv.b64 -pub allowlist-pub.b64
```

The tool prints the public key to stdout and writes both files. The private
key file is mode 0600.

### 2. Store the private key

The private key must be stored somewhere that satisfies four properties:

1. **Findable later.** You can locate it months from now when signing the next
   allowlist version.
2. **Survives the dev box dying.** Replicated, backed up, or located on
   storage other than the dev machine's local disk.
3. **Not in any git repository.** Never committed.
4. **Access-restricted.** Only people authorized to sign allowlists can read
   it.

The minimum viable arrangement is an encrypted archive (7-Zip with passphrase,
VeraCrypt container, or equivalent) stored in a personal cloud-synced folder
that is encrypted at rest by the provider and protected by your account.
Belt-and-suspenders: keep a second copy on a separate medium (encrypted USB
in a drawer, second cloud account, etc.).

After the private key is stored safely, **delete the local `allowlist-priv.b64`
file from the working directory**. Never leave it sitting in the repo or in
a shell history-accessible location.

### 3. Upload the public key

The public key is not a secret. Two places need it:

**GitHub Actions secret** (for CI builds):
1. Open repository Settings → Secrets and variables → Actions
2. Click "New repository secret"
3. Name: `ALLOWLIST_PUBLIC_KEY`
4. Value: paste the contents of `allowlist-pub.b64` (no trailing newline)
5. Save

CI's `Verify build` step will pick this up automatically on the next push.
Builds with no secret set produce a binary that fails at first allowlist
fetch — fine for CI, not fine for distributable artifacts.

**Local Windows dev box** (for MSI builds via `installer/windows/build.ps1`):
Set the environment variable persistently:
```powershell
[Environment]::SetEnvironmentVariable("ALLOWLIST_PUBLIC_KEY", (Get-Content allowlist-pub.b64), "User")
```

Open a new PowerShell session for the variable to take effect.

For production MSI builds, also set `RELEASE=1` in the build session:
```powershell
$env:RELEASE = "1"
.\installer\windows\build.ps1 -Version 2.0.0
```

`RELEASE=1` causes `build.ps1` to hard-fail if `ALLOWLIST_PUBLIC_KEY` is empty,
preventing accidental shipment of a binary with no baked-in key.

### 4. Verify the bake-in

After a fresh agent build, confirm the public key is embedded:
```
strings soholink-agent.exe | findstr "<first 16 chars of pub key>"
```

If the substring appears, the ldflags injection worked.

## Building and signing a new allowlist

This is the recurring procedure: every time the set of approved worker images
changes, build a new allowlist, sign it, and deploy.

### 1. Start from the template

Copy `examples/allowlist.example.json` to a working file:
```
copy examples\allowlist.example.json allowlist-v2-unsigned.json
```

### 2. Edit the entries

Replace the example entries with the real worker images. Each entry needs:

- `name`: human-readable image name (e.g. `soholink/compute-worker`)
- `digest`: the actual `sha256:...` digest of the published image
- `type`: one of `compute`, `storage`, `print_traditional`, `print_3d`
- `egress`: `none` (no outbound) or `outbound` (standard bridge)
- `allowed_destinations`: optional list of allowed outbound hosts (currently
  unused by executor — `EgressOutbound` allows arbitrary outbound; see
  CLAUDE.md TODO 9)
- `device_access`: optional list of `cups_socket` or `usb_printer`

Bump the `version` field. Set `issued_at` to the current UTC timestamp in
RFC 3339 format. Leave `signature` as an empty string — `allowlist-sign`
populates it.

### 3. Sign

Retrieve the private key from secure storage to a temporary local path
(`allowlist-priv.b64`). Sign:
```
allowlist-sign -input allowlist-v2-unsigned.json -key allowlist-priv.b64 -output allowlist-v2.json
```

**Immediately delete the local copy of `allowlist-priv.b64`** when done.

### 4. Deploy

Copy the signed file to the orchestrator host at the path the orchestrator
expects (default `/etc/soholink/allowlist.json`, configurable via the
`ALLOWLIST_PATH` env var on the orchestrator process).

For the current Cloudflare Tunnel deployment on NTARIHQ, this means SSH to
the host, place the file, and (if the orchestrator process caches anything,
which currently it does not — every request re-reads the file) restart the
orchestrator.

### 5. Verify

From any machine that can reach the orchestrator's public address:
```
curl https://soholink.org/allowlist
```

Should return the signed JSON. Then test from an agent install: the agent's
startup `LoadAllowlistFromURL` call should succeed without `ErrAllowlistNoKey`
or `ErrAllowlistSignature`.

## Key rotation

Rotation is required when the private key is suspected compromised, or as
periodic hygiene (e.g. annually).

### Procedure

1. Generate a new keypair (`allowlist-genkey -priv new-priv.b64 -pub new-pub.b64`)
2. Store the new private key per the storage requirements above
3. Update the `ALLOWLIST_PUBLIC_KEY` GitHub Actions secret to the new public key
4. Update the local `ALLOWLIST_PUBLIC_KEY` env var on the dev box
5. Sign the current allowlist with the new private key
6. Build a new MSI (`build.ps1` will bake in the new public key)
7. Deploy the new signed allowlist to the orchestrator
8. Distribute the new MSI to all agents
9. **Old agents stop working** until they're updated to the new MSI — they
   carry the old public key and will reject the newly-signed allowlist with
   `ErrAllowlistSignature`. Plan distribution accordingly.

### Bridge mode (avoid if possible)

If a hard cutover would strand agents, you can run a transition period where
agents accept either the old or new public key. This requires code changes
(supporting two `AllowlistPublicKey` values, e.g. `AllowlistPublicKey` and
`AllowlistPublicKeyPrev`) and is not currently implemented. If you ever need
this, it's a small code change in `internal/agent/allowlist.go`'s `Verify`
method. Avoid by planning rotation around natural agent-update windows.

## Loss recovery

If the private key is lost (no backup, all copies destroyed):

1. Generate a fresh keypair
2. Update the GitHub Actions secret and local env var
3. Build a new MSI with the new public key
4. **Every existing agent install becomes incapable of fetching new allowlists**
   until updated to the new MSI
5. There is no recovery path that avoids reinstalling all agents

This is why the storage requirements above are not optional. Belt-and-suspenders
storage (two independent copies on different media) is the minimum responsible
practice.
