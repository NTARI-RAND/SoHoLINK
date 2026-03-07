# SoHoLINK — Operator Guide

*Version: 1.0 | Effective Date: 2026-03-06*
*Operator: Network Theory Applied Research Institute*

> **DRAFT — Review before public release.**

This guide documents every point at which a human operator must interact with SoHoLINK — from
first install through day-to-day operations and maintenance. Each step is executable: commands
are copy-pasteable, file paths are exact, and UI actions are named precisely.

---

## Command Notation Key

| Symbol | Meaning |
|--------|---------|
| `$ command` | Run in a POSIX shell (bash/zsh) on Linux or macOS |
| `> command` | Run in Windows Command Prompt (cmd.exe) |
| `PS> command` | Run in Windows PowerShell |
| `yaml block` | Content to place in a config file (YAML) |
| `rego block` | Content to place in an OPA policy file |
| `<PLACEHOLDER>` | Replace with your actual value before running |
| `# comment` | Explanation only — do not type this |
| **Bold step** | A GUI click, menu selection, or form field |
| ⚠️ **Required** | Must be done before production launch; omitting causes a startup error or security gap |
| ℹ️ **Optional** | Skip if the subsystem is disabled in your config |

**Platform path separators:**
- Linux / macOS: forward slash `/`
- Windows: backslash `\` (PowerShell also accepts `/`)

**Environment variables:**
```bash
$ export VAR=value              # POSIX (bash/zsh)
> set VAR=value                 # Windows cmd
PS> $env:VAR = "value"          # PowerShell
```

**`curl` availability:** Linux/macOS include `curl`; Windows 10+ includes `curl.exe` in
PowerShell and cmd. All examples use POSIX syntax — adapt as needed for Windows.

---

## Phase 0 — Prerequisites

Install the following before attempting to run SoHoLINK.

### 0.1 Go Runtime

```bash
$ go version        # must print go1.24 or later
```

Download from https://go.dev/dl/ if missing.

### 0.2 GCC / MinGW (Windows GUI build only)

Required only when building the Fyne GUI (`-tags gui`).

```powershell
PS> winget install msys2.msys2
# Then in the MSYS2 shell:
$ pacman -S mingw-w64-x86_64-gcc
```

Headless builds (`go build ./cmd/soholink/...` without `-tags gui`) do **not** need GCC.

### 0.3 ClamAV (content scanning) ℹ️

Required when `resource_sharing.storage_pool.content_scanning: true` (the default).

```bash
$ apt install clamav clamav-daemon      # Debian/Ubuntu
$ brew install clamav                   # macOS
```

After install, update signatures:
```bash
$ freshclam
$ systemctl start clamav-daemon         # Linux
```

Default socket path: `/var/run/clamav/clamd.ctl`
Config key: `resource_sharing.storage_pool.clamav_socket`

### 0.4 IPFS Kubo Daemon ℹ️

Required when `storage.ipfs_api_addr` is set (enables IPFS pinning for uploaded files).

```bash
# Install Kubo from https://docs.ipfs.tech/install/command-line/
$ ipfs init
$ ipfs daemon &          # runs on http://127.0.0.1:5001 by default
```

### 0.5 LND Node ℹ️

Required when using the Lightning payment processor.

Install LND from https://github.com/lightningnetwork/lnd/releases.
Fund a channel with sufficient inbound/outbound liquidity before enabling.

---

## Phase 1 — Build & Install

### 1.1 Build from Source

```bash
# Headless (no GUI):
$ go build -o soholink ./cmd/soholink/...

# GUI (requires GCC on Windows):
$ go build -tags gui -o soholink ./cmd/soholink/...
```

### 1.2 Install Binary

```bash
# Linux / macOS:
$ sudo cp soholink /usr/local/bin/soholink
$ sudo chmod +x /usr/local/bin/soholink

# Windows: copy soholink.exe to a directory on %PATH%
> copy soholink.exe C:\Windows\System32\soholink.exe
```

### 1.3 Verify Installation

```bash
$ soholink --version
```

---

## Phase 2 — First-Run Setup Wizard (GUI)

Launch with no arguments when no DID has been generated yet. The wizard detects this
automatically and starts the 6-step setup flow.

```bash
$ soholink          # no args → GUI launches
```

### Wizard Steps

| Step | Screen Title | Operator Action |
|------|-------------|-----------------|
| 1 | **Welcome** | Read the licence summary. **Click "Accept & Continue"** |
| 2 | **System Detection** | SoHoLINK auto-detects CPU cores, RAM, disk, and GPU. Review the detected values. **Click "Confirm"** |
| 3 | **Cost Calculation** | Enter your local electricity rate in `$/kWh`. The wizard calculates operating cost. **Click "Next"** |
| 4 | **Pricing Suggestions** | Review suggested `$/CPU-core/hour` pricing. Adjust if desired. **Click "Accept Pricing"** |
| 5 | **Node Configuration** | Enter a **Node Name** (e.g. `home-office-1`). Select **Mode**: `provider`, `requester`, or `both`. **Click "Next"** |
| 6 | **Config Generation** | Review the displayed `ConfigPath` and `IdentityPath`. **Click "Finish"** |

### Generated Files (platform defaults)

| File | Linux/macOS | Windows |
|------|-------------|---------|
| Config file | `/etc/soholink/config.yaml` | `%APPDATA%\SoHoLINK\config.yaml` |
| Private key | `/var/lib/soholink/node_key.pem` | `%LOCALAPPDATA%\SoHoLINK\data\node_key.pem` |
| Database | `/var/lib/soholink/soholink.db` | `%LOCALAPPDATA%\SoHoLINK\data\soholink.db` |
| Policy dir | `/etc/soholink/policies/` | `%APPDATA%\SoHoLINK\policies\` |

---

## Phase 3 — Config File Editing

Edit the config file generated by the wizard (or `configs/default.yaml` in the repo for
development). The full reference is at `configs/default.yaml`.

### 3.1 ⚠️ Required Fields

These must be set correctly before the node will operate in production:

```yaml
node:
  name: "<YOUR_NODE_NAME>"    # human-readable; shown in federation registry

resource_sharing:
  http_api_address: "0.0.0.0:8080"  # change port or bind to 127.0.0.1 for LAN-only
  tls_cert_file: "/etc/soholink/tls/cert.pem"  # ⚠️ required for HTTPS
  tls_key_file:  "/etc/soholink/tls/key.pem"   # ⚠️ required for HTTPS
  allowed_origins:
    - "https://<YOUR_DASHBOARD_DOMAIN>"         # ⚠️ restrict from "*" before public launch
```

### 3.2 ⚠️ Payment Config

```yaml
payment:
  enabled: true
  processors:
    # Stripe (card payments):
    - type: stripe
      priority: 1
      secret_key_env: "STRIPE_SECRET_KEY"          # name of env var holding the Stripe secret key

    # Lightning Network (Bitcoin):
    - type: lightning
      priority: 2
      lnd_host: "localhost:10009"                  # LND gRPC host:port
      lnd_macaroon_env: "LND_MACAROON_HEX"         # name of env var holding the macaroon (hex)
      lnd_tls_cert_path: "/home/<USER>/.lnd/tls.cert"  # path to LND TLS cert
  stripe_webhook_secret: ""   # ⚠️ set via env SOHOLINK_PAYMENT_STRIPE_WEBHOOK_SECRET
```

### 3.3 Federation Config

```yaml
federation:
  coordinator_url: "https://coordinator.soholink.network"  # URL of the federation coordinator
  region: "us-east-1"           # AWS-style region hint for workload placement
  price_per_cpu_hour_sats: 100  # your asking price (satoshis per CPU-core-hour)
  heartbeat_interval: "30s"     # how often to re-announce to coordinator
  is_coordinator: false         # set true only if this node serves as a federation coordinator
  fee_percent: 1.0              # coordinator fee % (relevant only when is_coordinator: true)
```

### 3.4 P2P Allowlist (LAN peer discovery) ℹ️

```yaml
p2p:
  enabled: true
  listen_addr: "0.0.0.0:9090"
  allowed_node_dids:            # leave empty to accept all verified peers (default)
    - "did:key:<TRUSTED_NODE_1>"
    - "did:key:<TRUSTED_NODE_2>"
```

---

## Phase 4 — Environment Variables

Set these before starting the node. Never put secrets in the config file.

| Variable | Purpose | ⚠️ Required when |
|----------|---------|-----------------|
| `SOHOLINK_RADIUS_SHARED_SECRET` | RADIUS auth shared secret | `radius.enabled: true` |
| `SOHOLINK_PAYMENT_STRIPE_WEBHOOK_SECRET` | Stripe webhook signing key | Stripe processor enabled |
| `STRIPE_SECRET_KEY` | Stripe API secret key | Stripe processor enabled |
| `LND_MACAROON_HEX` | LND admin macaroon in hex | Lightning processor enabled |
| `SOHOLINK_NODE_DID` | Override the auto-generated DID | Multi-instance deployments |
| `SOHOLINK_RESOURCE_SHARING_HTTP_API_ADDRESS` | Override API listen address | Container/k8s deployments |
| `SOHOLINK_LOGGING_LEVEL` | Log verbosity: `debug`/`info`/`warn`/`error` | Diagnostics |
| `SOHOLINK_STORAGE_BASE_PATH` | Override data directory | Custom data locations |

**Setting multiple variables:**

```bash
# POSIX — write to a .env file (never commit this):
$ cat > /etc/soholink/env <<'EOF'
STRIPE_SECRET_KEY=sk_live_...
LND_MACAROON_HEX=0201...
SOHOLINK_PAYMENT_STRIPE_WEBHOOK_SECRET=whsec_...
EOF
$ source /etc/soholink/env && soholink start

# systemd service: add EnvironmentFile=/etc/soholink/env to the [Service] section
```

---

## Phase 5 — TLS Certificate Setup ⚠️

TLS is required for production. Without it, API tokens and payment data travel in cleartext.

### 5.1 Self-Signed Certificate (dev / LAN only)

```bash
$ mkdir -p /etc/soholink/tls
$ openssl req -x509 -newkey rsa:4096 \
    -keyout /etc/soholink/tls/key.pem \
    -out    /etc/soholink/tls/cert.pem \
    -days 365 -nodes \
    -subj "/CN=<YOUR_NODE_HOSTNAME>"
$ chmod 600 /etc/soholink/tls/key.pem
```

### 5.2 Let's Encrypt (public nodes)

```bash
$ certbot certonly --standalone -d <YOUR_DOMAIN>
# Certificates land in /etc/letsencrypt/live/<YOUR_DOMAIN>/
# Point config at:
#   tls_cert_file: /etc/letsencrypt/live/<YOUR_DOMAIN>/fullchain.pem
#   tls_key_file:  /etc/letsencrypt/live/<YOUR_DOMAIN>/privkey.pem
```

---

## Phase 6 — LND Macaroon Baking ℹ️

SoHoLINK only needs invoice-level permissions. Bake a restricted macaroon:

```bash
$ lncli bakemacaroon \
    invoices:read invoices:write \
    --save_to /etc/soholink/soholink.macaroon

# Convert to hex for the environment variable:
$ xxd -p /etc/soholink/soholink.macaroon | tr -d '\n'
# → copy this hex string into: export LND_MACAROON_HEX="<hex>"
```

---

## Phase 7 — Stripe Webhook Registration ℹ️

1. Open **Stripe Dashboard** → **Developers** → **Webhooks** → **Add endpoint**
2. **Endpoint URL:** `https://<YOUR_NODE_HOST>:8080/api/webhooks/stripe`
3. **Events to listen for:**
   - `payment_intent.succeeded`
   - `payment_intent.payment_failed`
4. **Click "Add endpoint"**
5. Copy the **Signing secret** (starts with `whsec_`)
6. Set the environment variable:
   ```bash
   $ export SOHOLINK_PAYMENT_STRIPE_WEBHOOK_SECRET="whsec_<YOUR_SECRET>"
   ```

---

## Phase 8 — Starting the Node

```bash
# Headless with explicit config:
$ soholink start --config /etc/soholink/config.yaml

# Headless with debug logging:
$ SOHOLINK_LOGGING_LEVEL=debug soholink start --config /etc/soholink/config.yaml

# GUI mode (no args):
$ soholink

# Show all CLI flags:
$ soholink --help
```

Expected startup log lines (confirm each subsystem initialised):

```
[app] node DID: did:key:...
[app] API server listening on 0.0.0.0:8080
[app] small-world mesh initialized (multicast 239.255.42.99:7946)
[app] payment processor: stripe (priority 1)
[app] payment processor: lightning (priority 2)
```

---

## Phase 9 — API Bootstrap (Client / Requester Flow)

Any client that submits workloads must authenticate via the challenge-response flow.

```bash
BASE="https://<YOUR_NODE_HOST>:8080"

# Step 1 — Request a single-use nonce (no auth required):
$ NONCE=$(curl -s "$BASE/api/auth/challenge" | jq -r .nonce)
$ echo "Nonce: $NONCE"

# Step 2 — Sign the nonce with your Ed25519 private key:
$ SIG=$(printf '%s' "$NONCE" \
    | openssl pkeyutl -sign -inkey client_key.pem \
    | base64 -w0)

# Step 3 — Verify and receive a bearer token (valid for 3600 s by default):
$ TOKEN=$(curl -s -X POST "$BASE/api/auth/verify" \
    -H "Content-Type: application/json" \
    -d "{\"did\":\"did:key:<YOUR_DID>\",\"nonce\":\"$NONCE\",\"signature\":\"$SIG\"}" \
    | jq -r .token)
$ echo "Token: $TOKEN"

# Step 4 — Check wallet balance:
$ curl -s "$BASE/api/wallet/balance" \
    -H "Authorization: Bearer $TOKEN" | jq

# Step 5 — Top up wallet (Lightning):
$ INVOICE=$(curl -s -X POST "$BASE/api/wallet/topup" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"amount_cents": 1000}' | jq -r .invoice)
$ lncli payinvoice "$INVOICE"    # pay from your own LND wallet

# Step 6 — Submit a compute workload:
$ curl -s -X POST "$BASE/api/marketplace/purchase" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{
      "workload_type": "compute",
      "cpu_cores": 2,
      "memory_mb": 4096,
      "timeout_seconds": 600,
      "image": "docker.io/library/python:3.12-slim",
      "command": ["python", "-c", "print(\"hello world\")"]
    }' | jq
```

---

## Phase 10 — OPA Policy Customisation ℹ️

The OPA policy controls which workloads are accepted. Edit
`configs/policies/resource_sharing.rego` (or the deployed copy in the policy directory).

```rego
# Current defaults — change the numeric limits to match your hardware capacity:
job_within_limits(spec) if {
    spec.cpu_cores        <= 4      # ← maximum CPU cores per job
    spec.memory_mb        <= 8192   # ← maximum RAM per job (MB)
    spec.timeout_seconds  <= 3600   # ← maximum job runtime (seconds)
    spec.disk_mb          <= 10240  # ← maximum disk per job (MB)
}
```

**No restart required** — the OPA engine re-evaluates the policy file on every request.

Verify the policy is loaded:
```bash
$ curl -s http://localhost:8080/api/orchestration/workloads \
    -H "Authorization: Bearer $TOKEN" | jq
```

---

## Phase 11 — Dashboard Settings Dialogs (GUI)

When the GUI is running, access all operator-configurable settings through the menu bar:

**Settings menu → [dialog name]**

| Dialog | Fields operator configures |
|--------|---------------------------|
| **Node Settings** | Node name, DID (read-only after generation), geographic location |
| **Pricing Settings** | CPU-core/hr price in sats, minimum order size, surge multiplier |
| **Network Settings** | API listen address, CORS allowed origins, TLS certificate paths |
| **Payment Settings** | Enable/disable processors, webhook secrets, offline queue size |
| **K8s Edges** | kubeconfig file path, cluster API endpoint, namespace |
| **IPFS Settings** | Kubo daemon API address (default `http://127.0.0.1:5001`) |
| **Provisioning Limits** | Max CPU/RAM/disk/timeout per job (mirrors OPA policy values) |
| **Users** | Add admin users, remove users, reset device credentials |

Changes in the GUI take effect immediately for session-level settings; settings that require
restart (TLS, API address) show a ⚠️ badge prompting a restart.

---

## Phase 12 — Rental Audit Log Review

The auto-accept engine records every accept/reject decision. Review periodically to verify
workloads comply with your AUP.

```bash
# Linux/macOS — using sqlite3:
$ sqlite3 /var/lib/soholink/soholink.db \
    "SELECT decided_at, action, reason, user_did \
     FROM rental_audit \
     ORDER BY decided_at DESC LIMIT 20;"

# Windows — PowerShell (requires sqlite3.exe on PATH):
PS> sqlite3.exe "$env:LOCALAPPDATA\SoHoLINK\data\soholink.db" `
    "SELECT decided_at, action, reason, user_did FROM rental_audit ORDER BY decided_at DESC LIMIT 20;"
```

---

## Phase 13 — Prometheus Metrics

The `/metrics` endpoint is public (no auth required) and exposes Prometheus-format counters.

```bash
# Verify the endpoint is live:
$ curl -s http://localhost:8080/metrics | grep soholink_

# Expected output (counters may be zero on a fresh node):
# soholink_http_requests_total{method="GET",path="/api/wallet/balance",status="200"} 0
# soholink_wallet_topup_total 0
# soholink_workload_purchase_total{result="success"} 0
# soholink_workload_purchase_total{result="policy_denied"} 0
```

**Prometheus scrape config:**
```yaml
# prometheus.yml:
scrape_configs:
  - job_name: soholink
    static_configs:
      - targets: ["<YOUR_NODE_HOST>:8080"]
    metrics_path: /metrics
```

---

## Phase 14 — Auto-Updates ℹ️

SoHoLINK can update its own binary securely without manual intervention.  The updater is
**disabled by default** — opt in via the Setup Wizard (step 5, "Enable auto-updates") or
by editing `config.yaml`.

### 14.1 Enable in Config

```yaml
updates:
  enabled: true
  check_interval: "24h"   # how often to poll GitHub releases
  release_url: "https://api.github.com/repos/NetworkTheoryAppliedResearchInstitute/soholink/releases/latest"
```

> ⚠️ `release_url` **must** start with `https://` — the updater will refuse any `http://` URL.

### 14.2 How It Works

1. At startup and then every `check_interval`, the node calls the GitHub releases API
2. If a newer semver tag is found, the binary asset for the current OS/arch is downloaded
3. The SHA-256 of the download is verified against the official `checksums.txt`
   (generated by GoReleaser and published alongside each release)
4. If the checksum matches, the binary is installed atomically:
   - **Unix**: `os.Rename` — kernel-level atomic; old binary keeps running until exit
   - **Windows**: the new binary is staged as `soholink_new.exe`; a helper batch script
     (`_update.bat`) waits for the process to exit, then swaps the files and restarts
5. On the **Overview** tab of the dashboard, an update-available notice appears when a newer
   release is detected
6. In the **Help ▸ Check for Updates…** dialog, you can trigger an immediate check and
   download at any time

### 14.3 Verify the Running Version

```bash
# Via CLI:
$ soholink --version

# Via HTTP API (no auth required):
$ curl http://localhost:8080/api/version
# → {"version":"0.1.1","commit":"537b76f","build_time":"2026-03-06"}
```

### 14.4 Manual Upgrade (auto-updates disabled)

```bash
# 1. Graceful stop:
$ kill -TERM $(pgrep soholink)
# Wait for process to exit (check with: ps aux | grep soholink)

# 2. Replace binary:
$ sudo cp soholink-new /usr/local/bin/soholink

# 3. Verify version:
$ soholink --version

# 4. Restart:
$ soholink start --config /etc/soholink/config.yaml
```

Database migrations run automatically on startup. No manual schema changes needed.

---

## Phase 15 — Backup & Recovery

```bash
# Back up the SQLite database (node must be stopped for a clean copy):
$ sqlite3 /var/lib/soholink/soholink.db ".backup /backup/soholink-$(date +%Y%m%d).db"

# Back up the node private key (store offline — losing this loses your DID):
$ cp /var/lib/soholink/node_key.pem /secure-offline-location/node_key.pem

# Back up the config:
$ cp /etc/soholink/config.yaml /backup/config-$(date +%Y%m%d).yaml
```

**Recovery:** restore `node_key.pem` and the database, then start the node normally.
The DID is derived from `node_key.pem` — restoring the key restores your identity.

---

## Quick Reference — Touchpoints Summary

| Phase | Who | Action | Time estimate |
|-------|-----|--------|--------------|
| Prerequisites | Admin | Install Go, GCC, ClamAV, IPFS, LND | 30–60 min |
| Build | Admin | `go build` | 2–5 min |
| Wizard | Admin | 6-step GUI flow | 5–10 min |
| Config editing | Admin | Edit `config.yaml` | 15–30 min |
| Env vars | Admin | Set secrets in environment | 5 min |
| TLS setup | Admin | `openssl req` or certbot | 5–20 min |
| LND macaroon | Admin | `lncli bakemacaroon` | 5 min |
| Stripe webhook | Admin | 4 clicks in Stripe Dashboard | 5 min |
| First start | Admin | `soholink start` | 1 min |
| API auth (client) | Client dev | `curl` challenge → verify flow | 10 min |
| OPA tuning | Admin | Edit `.rego` file | 5–15 min |
| Rental log review | Admin | `sqlite3` query | Ongoing |
| Metrics scraping | Admin | Configure Prometheus | 10 min |
| Auto-updates | Admin | Enable in config or wizard | 2 min |
| Manual upgrades | Admin | Stop → replace → start | 5 min |

---

*This document is provided for operational guidance and does not constitute legal or financial advice.*
