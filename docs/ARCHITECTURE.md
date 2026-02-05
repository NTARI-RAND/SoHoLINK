# Architecture Guide

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
