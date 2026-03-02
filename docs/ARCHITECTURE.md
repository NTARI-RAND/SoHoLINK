# Architecture Guide

## Platform Model

SoHoLINK operates in two deployment contexts:

```
┌─────────────────────────────────────────────────────────────────┐
│  Member's machine (Windows / macOS / Linux)                     │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  SoHoLINK Desktop Client (fedaaa-gui, Fyne)              │   │
│  │                                                          │   │
│  │  ├── AAA (RADIUS, Ed25519, OPA)                          │   │
│  │  ├── Hardware contribution config  ← NEW                 │   │
│  │  ├── Cooperative network client    ← NEW                 │   │
│  │  ├── Globe UI (ntarios-globe.html)                       │   │
│  │  └── LBTAS-NIM behavioral scoring  ← NEW                 │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                               │
                   LAN / WireGuard federation
                               │
┌─────────────────────────────────────────────────────────────────┐
│  Cooperative infrastructure node (always-on server / Pi)        │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  NTARI OS (Alpine 3.23, headless)                        │   │
│  │                                                          │   │
│  │  ├── ROS2 Jazzy + Cyclone DDS (DDS graph, domain 0)      │   │
│  │  └── Services: DNS · DHCP · NTP · Caddy · Redis          │   │
│  │                WireGuard · LDAP · Samba                  │   │
│  │                hw-profile · node-policy · scheduler      │   │
│  │                WAN · BGP                                 │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  SoHoLINK application services (installed by SoHoLINK)   │   │
│  │  ├── ntari-globe-bridge.initd  (WebSocket → DDS graph)   │   │
│  │  └── soholink.initd            (fedaaa daemon)           │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

**Most cooperative members run only the desktop client** on their existing
Windows/macOS/Linux machine. NTARI OS is for infrastructure nodes — always-on
servers, Raspberry Pis, and routers that provide shared cooperative services.
A cooperative of 200 members might run on 3–5 NTARI OS nodes.

---

## System Overview

SoHoLINK is a federated edge AAA (Authentication, Authorization, Accounting) platform designed for offline-first operation. The system uses Ed25519 digital signatures for authentication, eliminating the need for password databases or network connectivity during the authentication process.

```
+-------------------+     +-------------------+     +-------------------+
|   NAS / Client    |     |   SoHoLINK Node   |     |   Federation      |
|                   |     |                   |     |   (Future v0.2+)  |
| - WiFi AP         |     | - RADIUS Server   |     |                   |
| - Captive Portal  |<--->| - Verifier        |<--->| - Peer Discovery  |
| - Network Switch  | UDP | - Policy Engine   | P2P | - State Sync      |
|                   |     | - Accounting      |     | - Governance      |
+-------------------+     +-------------------+     +-------------------+
```

## Core Components

### 1. RADIUS Server (`internal/radius/`)

**Purpose:** Accept RADIUS Access-Request and Accounting-Request packets via UDP.

**Implementation:**
- Uses `layeh.com/radius` library
- Auth listener on port 1812
- Accounting listener on port 1813
- PAP (Password Authentication Protocol) for credential transport

**Flow:**
```
UDP Packet → Parse → Extract User-Name + User-Password → Verifier → Policy → Response
```

### 2. Credential Verifier (`internal/verifier/`)

**Purpose:** Validate Ed25519-signed credential tokens without network calls.

**Token Format (84 bytes, base64url encoded to ~112 characters):**
```
+-------------+----------+---------------+------------------+
| Timestamp   | Nonce    | Username Hash | Ed25519 Signature|
| (4 bytes)   | (8 bytes)| (8 bytes)     | (64 bytes)       |
+-------------+----------+---------------+------------------+
     ^             ^            ^                ^
     |             |            |                |
  Unix epoch   Random     SHA3-256(user)[:8]  Sign([0:19])
  (uint32 BE)
```

**Security Properties:**

| Property | Implementation |
|----------|---------------|
| Username Binding | SHA3-256 hash prevents credential reuse for different user |
| Replay Protection | 8-byte random nonce, recorded in database |
| Temporal Validity | Timestamp + TTL + clock skew tolerance |
| Revocation | Database lookup before acceptance |
| Signature Verification | Ed25519 over message bytes [0:19] |
| Timing Attack Prevention | Constant-time comparison for username hash |

**Verification Pipeline:**
```
1. Parse token (base64url decode, extract fields)
2. Lookup user by username in SQLite
3. Verify username hash matches (constant-time)
4. Verify Ed25519 signature
5. Check timestamp within TTL + clock skew
6. Check nonce not replayed
7. Check user not revoked
8. Record nonce to prevent replay
```

### 3. Policy Engine (`internal/policy/`)

**Purpose:** Flexible authorization using OPA (Open Policy Agent) Rego language.

**Default Policy:**
```rego
package soholink.authz

default allow = false

allow if {
    input.user != ""
    input.did != ""
    input.authenticated == true
}
```

**Input Schema:**
```json
{
  "user": "alice",
  "did": "did:key:z6Mk...",
  "role": "basic",
  "authenticated": true,
  "nas_address": "192.168.1.1",
  "resource": "",
  "timestamp": 1707123456,
  "attributes": {}
}
```

**Output:**
```json
{
  "allow": true,
  "deny_reasons": []
}
```

### 4. SQLite Store (`internal/store/`)

**Purpose:** Persist users, revocations, and nonce cache.

**Schema:**
```sql
-- Users table
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    did TEXT UNIQUE NOT NULL,
    public_key BLOB NOT NULL,
    role TEXT NOT NULL DEFAULT 'basic',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    revoked_at DATETIME
);

-- Revocations table
CREATE TABLE revocations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    did TEXT NOT NULL,
    reason TEXT,
    revoked_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Nonce cache for replay protection
CREATE TABLE nonce_cache (
    nonce TEXT PRIMARY KEY,
    seen_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Node metadata
CREATE TABLE node_info (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Indexes for performance
CREATE INDEX idx_users_did ON users(did);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_revocations_did ON revocations(did);
CREATE INDEX idx_revocations_revoked_at ON revocations(revoked_at);
CREATE INDEX idx_nonce_cache_seen_at ON nonce_cache(seen_at);
```

**Why Pure Go SQLite:**
- `modernc.org/sqlite` requires no CGO
- Enables cross-compilation to ARM64 from Windows
- Single file database, no external dependencies

### 5. Accounting Collector (`internal/accounting/`)

**Purpose:** Append-only JSONL logs for audit trail.

**Event Schema:**
```json
{
  "timestamp": "2026-02-05T10:30:00Z",
  "event_type": "auth_success",
  "user_did": "did:key:z6Mk...",
  "username": "alice",
  "nas_address": "192.168.1.1",
  "nas_port": "1",
  "decision": "allow",
  "reason": "authenticated",
  "latency_us": 2500
}
```

**File Structure:**
```
/var/lib/soholink/accounting/
  2026-02-05.jsonl      # Today's events
  2026-02-04.jsonl      # Yesterday's events
  2026-02-03.jsonl.gz   # Compressed (>7 days old)
```

**Guarantees:**
- Files opened with `O_APPEND|O_WRONLY|O_CREATE`
- Periodic fsync (every 100 events or 30 seconds)
- Daily rotation at midnight UTC
- Automatic compression after 7 days

### 6. Merkle Batcher (`internal/merkle/`)

**Purpose:** Cryptographic commitment to accounting logs.

**Algorithm:**
- SHA3-256 binary Merkle tree
- Leaf hash: `SHA3-256(0x00 || data)`
- Node hash: `SHA3-256(0x01 || left || right)`

**Batch Record:**
```json
{
  "timestamp": "2026-02-05T11:00:00Z",
  "source_file": "2026-02-05.jsonl",
  "root_hash": "a1b2c3d4...",
  "leaf_count": 1500,
  "tree_height": 11
}
```

**Usage:**
- Hourly batches by default
- Root hash can be published to blockchain (future)
- Inclusion proofs verify specific events

### 7. DID:key Format (`internal/did/`)

**Purpose:** Self-certifying decentralized identifiers.

**Format:**
```
did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK
        │    └─ base58btc(0xed01 + 32-byte Ed25519 public key)
        └─ multibase prefix 'z' (base58btc)
```

**Key Components:**
- `did:key:` - Method prefix
- `z` - Multibase prefix for base58btc
- `6Mk...` - Multicodec `0xed01` (Ed25519) + public key

## Authentication Flow

```
┌──────────────┐                    ┌──────────────┐
│   Client     │                    │  SoHoLINK    │
│  (NAS/AP)    │                    │    Node      │
└──────┬───────┘                    └──────┬───────┘
       │                                   │
       │  1. Access-Request (UDP)          │
       │  User-Name: "alice"               │
       │  User-Password: <base64url token> │
       │──────────────────────────────────>│
       │                                   │
       │                          2. Parse token
       │                          3. Lookup alice in SQLite
       │                          4. Verify username hash (constant-time)
       │                          5. Verify Ed25519 signature
       │                          6. Check timestamp + clock skew
       │                          7. Check nonce not replayed
       │                          8. Check not revoked
       │                          9. Evaluate OPA policy
       │                          10. Record accounting event
       │                                   │
       │  11. Access-Accept (UDP)          │
       │  Reply-Message: "authenticated"   │
       │<──────────────────────────────────│
       │                                   │
```

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Package layout | `internal/` | Modern Go convention; enforces encapsulation |
| SQLite driver | `modernc.org/sqlite` | Pure Go, no CGO, easy cross-compilation |
| Auth mechanism | Ed25519 signed tokens | No passwords to store; offline verification |
| Policy engine | Embedded OPA | Industry standard; Rego is expressive |
| Log format | JSONL | Machine-readable, appendable, greppable |
| Merkle hash | SHA3-256 | Quantum-resistant, no length extension attacks |
| Clock handling | 5-minute skew tolerance | Handles NTP sync issues gracefully |

## Security Model

### Threat Mitigations

| Threat | Mitigation |
|--------|------------|
| Credential theft | Short TTL (1 hour default), nonce prevents replay |
| Username swap | Username hash in signed message (constant-time comparison) |
| Replay attack | Random nonce recorded in database |
| Clock manipulation | Timestamp + skew tolerance; rejects far-future tokens |
| Key compromise | Immediate revocation via database |
| Timing attacks | Constant-time comparison for username hash |
| Log tampering | Append-only files, Merkle tree commitments |

### Trust Boundaries

```
┌────────────────────────────────────────────────────────────┐
│                    TRUSTED ZONE                            │
│  ┌─────────────────────────────────────────────────────┐  │
│  │  SoHoLINK Node                                      │  │
│  │  - SQLite database                                  │  │
│  │  - Private keys (0600 permissions)                  │  │
│  │  - Policy files                                     │  │
│  │  - Accounting logs                                  │  │
│  └─────────────────────────────────────────────────────┘  │
│                                                            │
│  BOUNDARY: File system permissions, process isolation      │
└────────────────────────────────────────────────────────────┘
                              │
                              │ RADIUS (UDP, shared secret)
                              │
┌────────────────────────────────────────────────────────────┐
│                   UNTRUSTED ZONE                           │
│  - Network Access Servers (NAS)                            │
│  - Client devices                                          │
│  - User-provided credentials                               │
└────────────────────────────────────────────────────────────┘
```

## Future Architecture (v0.2+)

### Federation Layer

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Node A    │<───>│   Node B    │<───>│   Node C    │
│  (School)   │ P2P │  (Library)  │ P2P │  (Center)   │
└─────────────┘     └─────────────┘     └─────────────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           │
                    ┌──────┴──────┐
                    │  Blockchain │
                    │  (Anchors)  │
                    └─────────────┘
```

### Planned Components

- **Peer Discovery** - DHT-based with bootstrap nodes
- **State Sync** - CRDT for user/revocation replication
- **Governance** - On-chain voting for policy changes
- **Merkle Anchoring** - Publish batch roots to blockchain

---

## Desktop Client (`fedaaa-gui`)

The SoHoLINK desktop client is a cross-platform Fyne application that members
run on their existing Windows/macOS/Linux machine. It is the primary interface
between cooperative members and the cooperative network.

**Build targets:**
- Windows (amd64)
- macOS (amd64, arm64)
- Linux (amd64, arm64)

**Responsibilities:**
1. AAA operations (RADIUS, Ed25519 token generation, OPA policy evaluation)
2. Hardware contribution configuration — what the member's machine offers to the cooperative
3. Cooperative network client — discover and connect to NTARI OS service nodes on LAN or over WireGuard
4. Globe UI — visualise the cooperative's live DDS computation graph
5. LBTAS-NIM behavioral scoring — trust and reputation layer for member interactions

---

## Hardware Contribution Layer

Members contribute CPU, RAM, disk, and bandwidth from their own machine.
SoHoLINK detects available resources cross-platform and lets the member set
contribution limits before publishing a capabilities profile to the cooperative.

### Resource Detection

```
┌─────────────────────────────────────────────────────┐
│  SoHoLINK Hardware Contribution                      │
│                                                      │
│  Detect (cross-platform):                            │
│  ├── CPU: cores, architecture, clock speed           │
│  ├── RAM: total, available                           │
│  ├── Disk: volumes, free space per volume            │
│  ├── Network: interface names, measured bandwidth    │
│  └── GPU: detected if present (future)              │
│                                                      │
│  Member sets limits:                                 │
│  ├── CPU: max % to contribute (default: 25%)         │
│  ├── RAM: max GB to contribute (default: 1 GB)       │
│  ├── Disk: max GB to contribute (default: 10 GB)     │
│  └── Bandwidth: max Mbps up/down (default: 10 Mbps)  │
│                                                      │
│  Publishes capabilities profile (JSON):              │
│  └── /soholink/member/<did>/capabilities             │
└─────────────────────────────────────────────────────┘
```

### Capabilities Profile Schema

Published to the cooperative network when the member connects:

```json
{
  "did": "did:key:z6Mk...",
  "hostname": "alice-laptop",
  "platform": "windows",
  "arch": "amd64",
  "timestamp": 1707123456,
  "contributed": {
    "cpu_cores": 2,
    "cpu_arch": "amd64",
    "ram_mb": 1024,
    "disk_gb": 10,
    "bandwidth_up_mbps": 10,
    "bandwidth_down_mbps": 10
  },
  "available": {
    "cpu_cores": 8,
    "ram_mb": 16384,
    "disk_gb": 500,
    "bandwidth_up_mbps": 100,
    "bandwidth_down_mbps": 250
  }
}
```

**Note:** Hardware detection is strictly read-only and local. No hardware data
is transmitted without the member explicitly joining a cooperative session.

### Cross-Platform Implementation

| Resource | Windows | macOS | Linux |
|----------|---------|-------|-------|
| CPU cores | `runtime.NumCPU()` | `runtime.NumCPU()` | `runtime.NumCPU()` |
| RAM | WMI `Win32_OperatingSystem` | `sysctl hw.memsize` | `/proc/meminfo` |
| Disk | `os.Getwd()` + `GetDiskFreeSpaceEx` | `syscall.Statfs` | `syscall.Statfs` |
| Network interfaces | `net.Interfaces()` | `net.Interfaces()` | `net.Interfaces()` |
| Bandwidth | iperf3 probe to nearest node | iperf3 probe | iperf3 probe |

Go's `runtime`, `os`, `net`, and `syscall` packages handle the majority of
detection. WMI queries on Windows use `github.com/yusufpapurcu/wmi`.

---

## Cooperative Network Client

The cooperative network client discovers NTARI OS service nodes on the local
network (or over WireGuard federation) and connects members to cooperative
services without requiring them to configure anything manually.

### Discovery

```
Member machine running SoHoLINK
  │
  ├── LAN scan (mDNS: _ntari._tcp.local)
  │     └── discovers NTARI OS nodes advertising services
  │
  └── WireGuard (if federation VPN configured)
        └── discovers nodes across cooperatives via ntari-federation
```

NTARI OS nodes advertise themselves via mDNS using Avahi
(`_ntari._tcp.local`, port 5353). SoHoLINK queries this to build a node list
without requiring manual IP configuration.

### Service Consumption

Once a node is discovered, SoHoLINK connects the member's machine to the
cooperative services running on NTARI OS:

| Service | NTARI OS provider | SoHoLINK action |
|---------|-------------------|-----------------|
| DNS | ntari-dns (Unbound, port 53) | configure OS DNS resolver |
| NTP | ntari-ntp (Chrony) | sync system clock |
| File sharing | ntari-files (Samba) | mount cooperative share |
| VPN | ntari-vpn (WireGuard) | configure WireGuard peer |
| DDS graph | ROS2 domain 0 (multicast) | subscribe to health topics |
| Web admin | ntari-web (Caddy, port 443) | open in browser |

SoHoLINK handles all configuration changes on the member's host OS (DNS
resolver, WireGuard peer, Samba mount) with explicit member approval for each
change. No changes are made silently.

### Connection State Machine

```
DISCONNECTED
     │
     ▼  (member clicks "Connect")
DISCOVERING  ─── mDNS scan + WireGuard probe
     │
     ▼  (node found)
AUTHENTICATING  ─── Ed25519 token → RADIUS → Access-Accept
     │
     ▼  (auth success)
CONNECTED  ─── capabilities profile published, services available
     │
     ▼  (member clicks "Disconnect" or node unreachable)
DISCONNECTED  ─── capabilities withdrawn, service config reverted
```

### DDS Graph Subscription

When connected to an NTARI OS node, SoHoLINK subscribes to the node's health
topics (ROS2 domain 0) to populate the Globe UI and LBTAS-NIM:

```
/ntari/node/capabilities       → hardware capabilities JSON
/ntari/scheduler/roles         → active role assignments
/ntari/<service>/health        → per-service health state
/ntari/node/policy             → current contribution policy
```

The DDS connection is read-only from the member's machine — members observe
the graph, they do not publish to it. Publishing is reserved for NTARI OS
services running on the node.

---

## LBTAS-NIM (Behavioral Scoring)

LBTAS-NIM (Learning-Based Trust and Accountability System — Network Integrity
Module) is a SoHoLINK-layer trust and reputation system. It runs entirely on
the member's machine and uses local data — no central server, no surveillance.

**Inputs:**
- Member's own contribution history (from local accounting logs)
- DDS health state observations from connected NTARI OS nodes
- Federation event logs (if federation VPN is active)

**Outputs:**
- Local trust scores for cooperative nodes and members
- Role eligibility recommendations → fed into ntari-scheduler
- Anomaly flags for cooperative governance review

**Scope boundary:** LBTAS-NIM is a SoHoLINK application layer concern.
NTARI OS publishes observable facts (health states, capabilities, role
assignments). LBTAS-NIM interprets those facts. The OS does not make trust
decisions.

---

## Network Graph Interface (`ui/globe-interface/`)

The Globe Network Graph is the SoHoLINK operator interface for visualizing a
live NTARI OS node's ROS2 DDS computation graph. It runs entirely in the
browser — no build step, no dependencies — and connects to the NTARI OS
WebSocket bridge to receive live graph data.

**File:** `ui/globe-interface/ntarios-globe.html`

**How it works:**

```
NTARI OS node (host)
  └── ntari-globe-bridge  ← SoHoLINK application service
        (ntari-os-services/ntari-globe-bridge.initd)
        ├── polls ROS2 graph at 1-2 Hz via ros2 CLI
        └── streams JSON over WebSocket at ws://<node-ip>/ws/graph
                                                      │
                                               SoHoLINK UI (browser)
                                                 ntarios-globe.html
                                                      │
                                               Canvas 2D wireframe globe
                                               Nodes = ROS2 nodes
                                               Edges = active topic links
```

**The bridge is a SoHoLINK-managed OpenRC service**, not part of NTARI OS.
SoHoLINK installs it onto the NTARI OS node when deployed. Bridge files:
- `ui/globe-interface/ntari-globe-bridge.sh` — bridge shell script
- `ntari-os-services/ntari-globe-bridge.initd` — OpenRC service definition
- `ntari-os-services/ntari-globe-bridge.confd` — configuration defaults

**Key properties:**
- Abstract wireframe sphere — no geography, purely topological
- Node position = network latency distance from local node
- Globe radius scales logarithmically with node count
- Searches by node name or topic name
- Falls back to demo simulation when bridge is unreachable
- Health state colour-coding: white (healthy), amber ring (degraded),
  red ring + X (failed)

**Deployment:**

1. Install the bridge onto the NTARI OS node:
   ```sh
   cp ntari-os-services/ntari-globe-bridge.initd /etc/init.d/ntari-globe-bridge
   cp ntari-os-services/ntari-globe-bridge.confd /etc/conf.d/ntari-globe-bridge
   cp ui/globe-interface/ntari-globe-bridge.sh /usr/local/bin/ntari-globe-bridge.sh
   rc-update add ntari-globe-bridge default && rc-service ntari-globe-bridge start
   ```
2. Serve `ntarios-globe.html` from SoHoLINK's portal or any web server
   on the same network.
3. Open in a browser — it auto-connects to `ws://<same-host>/ws/graph`.

**Source:** `ui/globe-interface/ntarios-globe.html`
**Bridge:** `ui/globe-interface/ntari-globe-bridge.sh`
**Service:** `ntari-os-services/ntari-globe-bridge.initd`
