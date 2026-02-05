# SoHoLINK Federated Edge AAA Platform

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-Apache%202.0-green.svg)](LICENSE)

**Sovereign, offline-first RADIUS authentication with Ed25519 credentials, OPA policy engine, and tamper-evident accounting.**

SoHoLINK is a federated Authentication, Authorization, and Accounting (AAA) platform designed for edge computing environments. It enables secure network access control without requiring internet connectivity, making it ideal for community networks, rural deployments, and Teen Tech Centers.

## Features

- **Ed25519 DID:key Credentials** - Modern elliptic curve cryptography with decentralized identifiers
- **Offline-First Architecture** - Zero network calls in authentication path; all verification is local
- **OPA Policy Engine** - Flexible authorization rules using Rego policy language
- **SHA3-256 Merkle Trees** - Tamper-evident accounting logs with cryptographic verification
- **Single Binary Deployment** - One executable for all platforms (Windows, Linux x64, Raspberry Pi ARM64)
- **RADIUS Protocol** - Standard UDP-based authentication (port 1812) and accounting (port 1813)

## Quick Start

```bash
# Build
go build -o fedaaa ./cmd/fedaaa

# Initialize node (creates directories, database, node keypair)
./fedaaa install

# Add a user
./fedaaa users add alice

# Start RADIUS server
./fedaaa start

# In another terminal, test authentication with radclient (Linux)
echo "User-Name=alice,User-Password=<token>" | radclient -x localhost:1812 auth testing123
```

## Architecture Overview

```
NAS / radclient                    fedaaa (single binary)
     |                                    |
     | UDP Access-Request                 |
     | User-Name + User-Password          |
     |                                    |
     +---> [RADIUS Server :1812] ---------+
                    |                     |
                    v                     |
            [Verifier]                    |
            - Parse credential token      |
            - Verify username binding     |
            - Verify Ed25519 signature    |
            - Check expiration + skew     |
            - Check nonce replay          |
            - Check revocation            |
                    |                     |
                    v                     |
            [Policy Engine (OPA)]         |
            - Evaluate Rego rules         |
                    |                     |
                    v                     |
            [Accounting Collector]        |
            - Append event to JSONL       |
                    |                     |
     <--- Access-Accept / Access-Reject --+
```

## Credential Token Format

```
Format: base64url(84 bytes)
  [0-3]    Timestamp (4 bytes, big-endian uint32, Unix epoch)
  [4-11]   Nonce (8 bytes, random)
  [12-19]  Username Hash (first 8 bytes of SHA3-256(username))
  [20-83]  Ed25519 Signature (64 bytes, signs bytes [0-19])

Security Properties:
  - Username binding: credential cannot be reused for different user
  - Replay protection: nonce + timestamp prevents token reuse
  - Temporal validity: TTL + clock skew tolerance
  - Revocation support: user can be revoked instantly
  - Tamper-evident: Ed25519 signature over all fields
```

## Documentation

- [Installation Guide](docs/INSTALL.md) - Build, install, and configure
- [Architecture](docs/ARCHITECTURE.md) - System design and components
- [Testing Guide](docs/TESTING.md) - Run tests and verify deployment
- [Operations Guide](docs/OPERATIONS.md) - Day-to-day management and troubleshooting

## CLI Commands

| Command | Description |
|---------|-------------|
| `fedaaa install` | Initialize node: create directories, database, generate keypair |
| `fedaaa start` | Start RADIUS server (blocks until SIGINT/SIGTERM) |
| `fedaaa status` | Show node status: DID, user count, Merkle root |
| `fedaaa users add <name>` | Create user with Ed25519 keypair |
| `fedaaa users list` | List all users with status |
| `fedaaa users revoke <name>` | Revoke user access immediately |
| `fedaaa logs [--follow]` | View accounting logs |
| `fedaaa policy list` | List loaded Rego policies |
| `fedaaa policy test` | Test policy with sample input |

## Building for Raspberry Pi

```bash
# Cross-compile for ARM64 Linux (Raspberry Pi 4)
GOOS=linux GOARCH=arm64 go build -o fedaaa-linux-arm64 ./cmd/fedaaa

# Copy to Pi and run
scp fedaaa-linux-arm64 pi@raspberrypi:~/
ssh pi@raspberrypi './fedaaa-linux-arm64 install && ./fedaaa-linux-arm64 start'
```

## Configuration

Default configuration is embedded in the binary. Override with `config.yaml`:

```yaml
node:
  name: "soholink-node"

radius:
  auth_address: "0.0.0.0:1812"
  acct_address: "0.0.0.0:1813"
  shared_secret: "change-me-in-production"

auth:
  credential_ttl: 3600          # 1 hour
  max_nonce_age: 300            # 5 minutes
  clock_skew_tolerance: 300     # 5 minutes (allows for clock drift)

storage:
  base_path: "/var/lib/soholink"

policy:
  directory: "/etc/soholink/policies"

accounting:
  rotation_interval: "24h"
  compress_after_days: 7

merkle:
  batch_interval: "1h"

logging:
  level: "info"
  format: "json"
```

## Security Considerations

1. **Change the shared secret** - The default `testing123` is for development only
2. **NTP synchronization** - Clock skew tolerance is 5 minutes; ensure nodes have accurate time
3. **Key file permissions** - Private keys are stored with 0600 permissions
4. **Network isolation** - RADIUS uses UDP; consider firewall rules

## Dependencies

- [layeh.com/radius](https://github.com/layeh/radius) - RADIUS protocol implementation
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) - Pure Go SQLite (no CGO)
- [github.com/open-policy-agent/opa](https://github.com/open-policy-agent/opa) - Policy engine
- [golang.org/x/crypto](https://golang.org/x/crypto) - SHA3-256 cryptography
- [github.com/spf13/cobra](https://github.com/spf13/cobra) - CLI framework

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Contributing

This project is part of the Network Theory Applied Research Institute's work on community networking infrastructure. Contributions welcome!
