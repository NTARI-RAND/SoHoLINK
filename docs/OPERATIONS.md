# Operations Guide

## Startup and Shutdown

### Starting the Service

```bash
# Foreground (for debugging)
./fedaaa start

# Background (Linux)
./fedaaa start &

# Check status
./fedaaa status
```

### Graceful Shutdown

The server handles `SIGINT` (Ctrl+C) and `SIGTERM` gracefully:

1. Stops accepting new RADIUS requests
2. Waits for in-flight requests to complete
3. Flushes accounting logs
4. Completes final Merkle batch
5. Closes database connection

### Systemd Management (Linux)

```bash
# Start
sudo systemctl start soholink

# Stop
sudo systemctl stop soholink

# Restart
sudo systemctl restart soholink

# View logs
sudo journalctl -u soholink -f
```

## User Management

### Add User

```bash
./fedaaa users add alice
./fedaaa users add bob --role premium
./fedaaa users add admin --role admin
```

Roles are arbitrary strings used in policy evaluation.

### List Users

```bash
./fedaaa users list
```

Output:
```
+----------+----------------------------+-------+--------+---------------------+
| USERNAME | DID                        | ROLE  | STATUS | CREATED             |
+----------+----------------------------+-------+--------+---------------------+
| alice    | did:key:z6MkhaXgBZDvot...  | basic | active | 2026-02-05 10:30:00 |
| bob      | did:key:z6MkpTHR8VNsBx...  | premium| active | 2026-02-05 10:31:00 |
| carol    | did:key:z6MkvZ5yKMU7Zi...  | basic | REVOKED| 2026-02-05 10:32:00 |
+----------+----------------------------+-------+--------+---------------------+

Total: 3 users
```

### Revoke User

```bash
./fedaaa users revoke alice --reason "left organization"
```

Revocation takes effect **immediately** - the user cannot authenticate even with a valid token.

### User Private Keys

User private keys are stored in `<data-dir>/keys/<username>.pem`:

```
/var/lib/soholink/keys/
  alice.pem
  bob.pem
```

**Security:** Keys are created with 0600 permissions (owner read/write only).

## Log Files

### Accounting Logs

Location: `<data-dir>/accounting/`

```
/var/lib/soholink/accounting/
  2026-02-05.jsonl      # Today's events
  2026-02-04.jsonl      # Yesterday's events
  2026-02-03.jsonl.gz   # Older (compressed)
```

### Viewing Logs

```bash
# View recent events
./fedaaa logs

# Follow in real-time
./fedaaa logs --follow

# Filter by event type
./fedaaa logs --type auth_success
./fedaaa logs --type auth_denied

# Filter by user
./fedaaa logs --user alice

# View specific date
./fedaaa logs --date 2026-02-04

# Last N events
./fedaaa logs --last 50
```

### Log Event Schema

```json
{
  "timestamp": "2026-02-05T10:30:00.123Z",
  "event_type": "auth_success",
  "user_did": "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
  "username": "alice",
  "nas_address": "192.168.1.1:32456",
  "nas_port": "",
  "decision": "allow",
  "reason": "authenticated",
  "latency_us": 2500
}
```

### Log Rotation

- **Daily rotation** at midnight UTC
- **Compression** after 7 days (configurable)
- Files are never deleted automatically

## Merkle Batch Verification

### View Latest Batch

```bash
./fedaaa status
```

Output includes:
```
Latest Merkle Batch:
  Timestamp:   2026-02-05T11:00:00Z
  Source:      2026-02-05.jsonl
  Root Hash:   a1b2c3d4e5f6...
  Leaf Count:  1500
  Tree Height: 11
```

### Batch Files

Location: `<data-dir>/merkle/`

```
/var/lib/soholink/merkle/
  2026-02-05T10.batch.json
  2026-02-05T11.batch.json
```

### Verify Event Inclusion

```bash
# Generate proof for specific event
./fedaaa merkle proof --event-index 42 --batch 2026-02-05T11.batch.json

# Verify proof
./fedaaa merkle verify --leaf-hash <hash> --proof <proof> --root <root>
```

## Policy Management

### List Policies

```bash
./fedaaa policy list
```

Output:
```
+---------------+------------------------------------------------------------------+
| FILE          | SHA3-256 HASH                                                    |
+---------------+------------------------------------------------------------------+
| default.rego  | a1b2c3d4e5f67890abcdef1234567890abcdef1234567890abcdef12345678 |
| custom.rego   | fedcba0987654321fedcba0987654321fedcba0987654321fedcba09876543 |
+---------------+------------------------------------------------------------------+
```

### Test Policy

```bash
# Test with sample input
./fedaaa policy test --user alice --did "did:key:z6Mk..." --role basic --authenticated

# Output:
# Result: ALLOW
# Deny Reasons: []
```

### Hot-Reload Policies

Policies are loaded from the policy directory. To update:

1. Edit or add `.rego` files in the policy directory
2. The engine reloads automatically on next evaluation

Note: In production, restart the service for reliability:
```bash
sudo systemctl restart soholink
```

### Custom Policy Example

Create `/etc/soholink/policies/time-based.rego`:

```rego
package soholink.authz

import rego.v1

# Allow premium users 24/7
allow if {
    input.role == "premium"
    input.authenticated == true
}

# Allow basic users only during business hours (8am-6pm)
allow if {
    input.role == "basic"
    input.authenticated == true
    hour := time.clock(time.now_ns())[0]
    hour >= 8
    hour < 18
}

# Deny with reason for off-hours access
deny_reasons contains reason if {
    input.role == "basic"
    input.authenticated == true
    hour := time.clock(time.now_ns())[0]
    not (hour >= 8; hour < 18)
    reason := "basic users allowed only 8am-6pm"
}
```

## Troubleshooting

### RADIUS Connection Refused

**Symptom:**
```
radclient: Failed to connect: Connection refused
```

**Causes:**
1. Server not running
2. Wrong port
3. Firewall blocking UDP

**Solutions:**
```bash
# Check if server is running
./fedaaa status

# Check if port is listening
sudo ss -tulpn | grep 1812

# Check firewall (Linux)
sudo iptables -L -n | grep 1812
sudo ufw status
```

### Authentication Failures

**Invalid Signature:**
```
auth: denied user 'alice': invalid_signature: Ed25519 signature verification failed
```

The token was signed with the wrong key. Regenerate:
```bash
# The user's token must match their stored public key
# If the key was regenerated, create a new user or re-add
./fedaaa users revoke alice --reason "key rotation"
./fedaaa users add alice
```

**Username Mismatch:**
```
auth: denied user 'bob': username_mismatch: credential was not issued for this username
```

The token was created for a different username. Tokens are bound to the specific user.

**Credential Expired:**
```
auth: denied user 'alice': credential_expired: expired 3700 seconds ago
```

Token exceeded TTL. Generate a new token.

**Credential Future:**
```
auth: denied user 'alice': credential_future: timestamp is 400 seconds in the future
```

Client clock is too far ahead. Check NTP synchronization.

**Nonce Replay:**
```
auth: denied user 'alice': nonce_replay: credential token has already been used
```

Token was already used. Generate a new token for each authentication.

**User Revoked:**
```
auth: denied user 'alice': user_revoked: user 'alice' has been revoked
```

User was revoked. To restore:
```bash
# Currently must re-add the user (with new keypair)
./fedaaa users add alice
```

### Policy Denials

**Symptom:** Auth succeeds but policy denies.

```
auth: denied user 'alice': policy: authorization denied
```

**Debug:**
```bash
# Test policy with user's attributes
./fedaaa policy test --user alice --did "did:key:z6Mk..." --role basic --authenticated

# Check policy files
./fedaaa policy list
cat /etc/soholink/policies/*.rego
```

### Database Issues

**Database Locked:**
```
Error: database is locked
```

Only one process can access SQLite at a time. Stop other instances.

**Database Corrupted:**
```
Error: database disk image is malformed
```

Restore from backup or reinitialize:
```bash
# Backup current (if partially readable)
cp /var/lib/soholink/soholink.db /var/lib/soholink/soholink.db.bak

# Reinitialize (LOSES ALL DATA)
rm /var/lib/soholink/soholink.db
./fedaaa install
```

## Monitoring

### Health Check

```bash
# Quick status check (suitable for monitoring)
./fedaaa status --json
```

Output:
```json
{
  "node_did": "did:key:z6Mk...",
  "status": "healthy",
  "radius_auth_port": 1812,
  "radius_acct_port": 1813,
  "user_count": 42,
  "active_user_count": 40,
  "revoked_user_count": 2,
  "latest_merkle_root": "a1b2c3d4...",
  "policy_count": 2
}
```

### Metrics to Monitor

| Metric | Warning | Critical |
|--------|---------|----------|
| Auth success rate | < 95% | < 90% |
| Auth latency p99 | > 100ms | > 500ms |
| Nonce cache size | > 100K | > 500K |
| Disk usage | > 80% | > 95% |
| Clock skew | > 2 min | > 4 min |

### NTP Requirement

**Critical:** Clock skew tolerance is 5 minutes. Ensure NTP is configured:

```bash
# Check time sync status (Linux)
timedatectl status

# Enable NTP
sudo timedatectl set-ntp true

# Check NTP servers
chronyc sources -v
```

## Backup and Recovery

### What to Backup

1. **Database:** `/var/lib/soholink/soholink.db`
2. **Node Key:** `/var/lib/soholink/node_key.pem`
3. **User Keys:** `/var/lib/soholink/keys/*.pem`
4. **Config:** `/etc/soholink/config.yaml`
5. **Policies:** `/etc/soholink/policies/*.rego`
6. **Accounting Logs:** `/var/lib/soholink/accounting/*.jsonl*`
7. **Merkle Batches:** `/var/lib/soholink/merkle/*.batch.json`

### Backup Script

```bash
#!/bin/bash
BACKUP_DIR="/backups/soholink/$(date +%Y%m%d)"
DATA_DIR="/var/lib/soholink"
CONFIG_DIR="/etc/soholink"

mkdir -p "$BACKUP_DIR"

# Stop service for consistent backup
sudo systemctl stop soholink

# Database (most critical)
cp "$DATA_DIR/soholink.db" "$BACKUP_DIR/"

# Keys
cp -r "$DATA_DIR/keys" "$BACKUP_DIR/"
cp "$DATA_DIR/node_key.pem" "$BACKUP_DIR/"

# Config and policies
cp -r "$CONFIG_DIR" "$BACKUP_DIR/config"

# Accounting logs (optional, can be large)
tar -czf "$BACKUP_DIR/accounting.tar.gz" -C "$DATA_DIR" accounting/

# Restart service
sudo systemctl start soholink

echo "Backup completed: $BACKUP_DIR"
```

### Restore Procedure

```bash
#!/bin/bash
BACKUP_DIR="/backups/soholink/20260205"
DATA_DIR="/var/lib/soholink"
CONFIG_DIR="/etc/soholink"

# Stop service
sudo systemctl stop soholink

# Restore database
cp "$BACKUP_DIR/soholink.db" "$DATA_DIR/"

# Restore keys
cp -r "$BACKUP_DIR/keys" "$DATA_DIR/"
cp "$BACKUP_DIR/node_key.pem" "$DATA_DIR/"

# Restore config
cp -r "$BACKUP_DIR/config/"* "$CONFIG_DIR/"

# Fix permissions
chmod 600 "$DATA_DIR/node_key.pem"
chmod 600 "$DATA_DIR/keys/"*.pem

# Start service
sudo systemctl start soholink
```

## Capacity Planning

### Resource Requirements

| Metric | Minimum | Recommended |
|--------|---------|-------------|
| CPU | 1 core | 2 cores |
| RAM | 256 MB | 512 MB |
| Disk | 1 GB | 10 GB |

### Scaling Limits

| Parameter | Tested Limit | Notes |
|-----------|--------------|-------|
| Users | 100,000 | SQLite indexed |
| Auth/sec | 1,000 | Single node |
| Nonce cache | 1M entries | Pruned daily |
| Log events/day | 10M | Rotated daily |

### Disk Growth

- Database: ~1 KB per user
- Accounting: ~200 bytes per event
- Merkle: ~1 KB per batch

Example: 1000 auths/day = ~200 KB/day = ~73 MB/year (before compression)
