# Installation Guide

## Prerequisites

- **Go 1.22+** - Required for building from source
- **Target Platforms:**
  - Windows (AMD64)
  - Linux (AMD64, ARM64)
  - Raspberry Pi 4 (ARM64 Linux)

## Building from Source

### Standard Build

```bash
# Clone repository
git clone https://github.com/NetworkTheoryAppliedResearchInstitute/soholink.git
cd soholink

# Build for current platform
go build -o fedaaa ./cmd/fedaaa

# Or use Makefile
make build
```

### Cross-Compilation for Raspberry Pi

```bash
# Build for ARM64 Linux (Raspberry Pi 4)
GOOS=linux GOARCH=arm64 go build -o fedaaa-linux-arm64 ./cmd/fedaaa

# Or use Makefile
make build-pi
```

No CGO is required - the SQLite driver is pure Go, enabling easy cross-compilation.

## Installation Steps

### 1. Run Install Command

```bash
./fedaaa install
```

This creates:
- Data directory with SQLite database
- Node Ed25519 keypair
- Default configuration file
- Default authorization policy

### 2. Directory Structure Created

**Linux:**
```
/etc/soholink/
  config.yaml           # Configuration file
  policies/
    default.rego        # Authorization policy

/var/lib/soholink/
  soholink.db           # SQLite database
  node_key.pem          # Node private key
  keys/                 # User private keys
  accounting/           # JSONL logs
  merkle/               # Merkle batch files
```

**Windows:**
```
%APPDATA%\SoHoLINK\
  config.yaml
  policies\
    default.rego

%LOCALAPPDATA%\SoHoLINK\data\
  soholink.db
  node_key.pem
  keys\
  accounting\
  merkle\
```

### 3. Configure

Edit configuration file to change defaults:

```yaml
# /etc/soholink/config.yaml (Linux)
# %APPDATA%\SoHoLINK\config.yaml (Windows)

node:
  name: "my-node"
  location: "Building A, Room 101"

radius:
  auth_address: "0.0.0.0:1812"
  acct_address: "0.0.0.0:1813"
  shared_secret: "your-secure-secret-here"  # CHANGE THIS!

auth:
  credential_ttl: 3600          # Token lifetime in seconds
  max_nonce_age: 300            # Nonce cache duration
  clock_skew_tolerance: 300     # Allow 5 min clock drift

storage:
  base_path: "/var/lib/soholink"

policy:
  directory: "/etc/soholink/policies"
  default_policy: "default.rego"

accounting:
  rotation_interval: "24h"
  compress_after_days: 7

merkle:
  batch_interval: "1h"

logging:
  level: "info"
  format: "json"
```

### 4. Create First User

```bash
./fedaaa users add alice --role basic
```

Output:
```
User created successfully!

Username:    alice
DID:         did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK
Role:        basic
Private Key: /var/lib/soholink/keys/alice.pem

Sample credential token (for testing):
  ZGVhZGJlZWYxMjM0NTY3ODlhYmNkZWZnaGlqa2xtbm9...

Test with radclient:
  echo "User-Name=alice,User-Password=ZGVhZGJl..." | radclient -x localhost:1812 auth testing123
```

### 5. Start the Server

```bash
./fedaaa start
```

The server will:
- Listen on RADIUS auth port (default 1812)
- Listen on RADIUS accounting port (default 1813)
- Run background tasks (nonce pruning, log compression, Merkle batching)
- Handle graceful shutdown on SIGINT/SIGTERM

## Configuration Reference

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `node.name` | string | "soholink-node" | Node identifier |
| `node.location` | string | "" | Physical location |
| `radius.auth_address` | string | "0.0.0.0:1812" | Auth listener address |
| `radius.acct_address` | string | "0.0.0.0:1813" | Accounting listener address |
| `radius.shared_secret` | string | "testing123" | RADIUS shared secret |
| `auth.credential_ttl` | int | 3600 | Token lifetime (seconds) |
| `auth.max_nonce_age` | int | 300 | Nonce cache duration (seconds) |
| `auth.clock_skew_tolerance` | int | 300 | Clock drift tolerance (seconds) |
| `storage.base_path` | string | platform-specific | Data directory |
| `policy.directory` | string | platform-specific | Policy files directory |
| `policy.default_policy` | string | "default.rego" | Default policy file |
| `accounting.rotation_interval` | string | "24h" | Log rotation period |
| `accounting.compress_after_days` | int | 7 | Days before compression |
| `merkle.batch_interval` | string | "1h" | Merkle tree batch period |
| `logging.level` | string | "info" | Log level (debug/info/warn/error) |
| `logging.format` | string | "json" | Log format (json/text) |

## Environment Variables

Configuration can be overridden with environment variables using the `SOHOLINK_` prefix:

```bash
export SOHOLINK_RADIUS_SHARED_SECRET="my-secret"
export SOHOLINK_STORAGE_BASE_PATH="/custom/path"
export SOHOLINK_AUTH_CREDENTIAL_TTL=7200
```

## Running as a Service

### Linux (systemd)

Create `/etc/systemd/system/soholink.service`:

```ini
[Unit]
Description=SoHoLINK AAA Platform
After=network.target

[Service]
Type=simple
User=soholink
ExecStart=/usr/local/bin/fedaaa start
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable soholink
sudo systemctl start soholink
```

### Windows (NSSM)

Use NSSM (Non-Sucking Service Manager):

```powershell
nssm install SoHoLINK "C:\path\to\fedaaa.exe" start
nssm start SoHoLINK
```

## Verification

After installation, verify the setup:

```bash
# Check status
./fedaaa status

# List users
./fedaaa users list

# List policies
./fedaaa policy list

# Test policy (should return allow=true)
./fedaaa policy test --user alice --did "did:key:z6Mk..." --authenticated
```

## Troubleshooting

### Port Already in Use

```
Error: failed to start auth server: listen udp 0.0.0.0:1812: bind: address already in use
```

Another process is using port 1812. Check with:
```bash
# Linux
sudo ss -tulpn | grep 1812

# Windows
netstat -ano | findstr 1812
```

### Permission Denied

```
Error: failed to create data directory: permission denied
```

Run with appropriate permissions or change `storage.base_path` to a writable location.

### Database Locked

```
Error: database is locked
```

Only one instance of fedaaa can access the database at a time. Stop any other running instances.
