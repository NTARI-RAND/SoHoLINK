# Database Backups

## Overview

A `pg-backup` sidecar container runs alongside the postgres service and
writes daily compressed `pg_dump` snapshots to `D:\SoHoLINK-backups\` on
the host. On each container start, one backup fires immediately; subsequent
backups run every 24 hours. Files are retained for 90 days and then pruned
automatically.

Each dump is a plain-SQL gzip archive named `dump-<TIMESTAMP>.sql.gz`
(e.g. `dump-20260526T012345Z.sql.gz`). The backup covers only the
`postgres` database — the production dataset. The `soholink_test` database
used by integration tests is not backed up.

## Verifying backups are running

Check recent log output from the sidecar:

```bash
docker compose logs --tail=50 pg-backup
```

A healthy run shows lines like:

```
2026-05-26T01:23:45Z INFO: starting backup → /backups/dump-20260526T012345Z.sql.gz
2026-05-26T01:23:52Z INFO: backup complete — /backups/dump-20260526T012345Z.sql.gz (14M)
2026-05-26T01:23:52Z INFO: pruning files older than 90 days
2026-05-26T01:23:52Z INFO: sleeping 86400s until next backup
```

List the files on disk:

```bash
ls D:/SoHoLINK-backups/
```

## Manual on-demand backup

To trigger an immediate backup without waiting for the next scheduled run:

```bash
docker compose exec pg-backup /usr/local/bin/backup.sh once
```

This runs one backup cycle (dump + prune) and exits. It does not interrupt
the scheduled loop running in the main container process.

## Restoration procedure

These steps restore the `postgres` database from a dump file. The portal
and orchestrator are stopped first to prevent writes during restore.

```bash
# 1. Stop application services (leave postgres running)
docker compose stop portal orchestrator

# 2. Drop and recreate the public schema to clear all tables
docker compose exec -T postgres psql -U postgres -d postgres \
  -c "DROP SCHEMA public CASCADE;"
docker compose exec -T postgres psql -U postgres -d postgres \
  -c "CREATE SCHEMA public;"
docker compose exec -T postgres psql -U postgres -d postgres \
  -c "GRANT ALL ON SCHEMA public TO postgres;"
docker compose exec -T postgres psql -U postgres -d postgres \
  -c "GRANT ALL ON SCHEMA public TO public;"

# 3. Restore from the chosen dump file (substitute the actual filename)
gunzip -c "D:/SoHoLINK-backups/dump-<TIMESTAMP>.sql.gz" \
  | docker compose exec -T postgres psql -U postgres -d postgres

# 4. Restart application services
docker compose start portal orchestrator
```

After restart, run `docker compose logs --tail=20 portal orchestrator` to
confirm the services come up cleanly and migrations report "no change".

## Drive attachment requirement

The bind mount `D:\SoHoLINK-backups:/backups` only works when the
My Passport drive is attached **before** Docker Desktop starts.
Docker Desktop's internal WSL2 distro auto-mounts Windows drives
at boot; drives plugged in after that become invisible to running
containers, and the bind mount silently resolves to a phantom
path inside the VM (the sidecar logs report "backup complete,"
but no file reaches D:).

If the host reboots without the drive attached, or the drive is
unplugged and reattached while Docker Desktop is running:

1. Plug in the drive
2. `wsl --shutdown` (this stops Docker Desktop's internal distro)
3. Docker Desktop's daemon will auto-restart within ~15 seconds,
   or restart it manually from the system tray
4. `docker compose up -d` to bring the stack back

After restart, trigger a manual backup via `docker compose exec
pg-backup /usr/local/bin/backup.sh once` and verify a new file
appears in `D:\SoHoLINK-backups\` before assuming backups are
running correctly.

## Scope and limitations

- **Database covered:** `postgres` only (production data). The
  `soholink_test` database used by integration tests is not backed up.
- **Granularity:** daily snapshots. Data written between the last backup
  and an incident is not recoverable from these dumps.
- **WAL archiving:** continuous WAL archiving (point-in-time recovery) will
  be configured separately in Step 3. When in place, it provides
  finer-grained recovery between the daily snapshots — restoring to any
  point in time rather than only to the previous midnight boundary.
- **Off-host copies:** `D:\SoHoLINK-backups\` is local to NTARIHQ. A
  hardware failure affecting the host disk would destroy both the postgres
  data volume and the backup directory simultaneously. Off-host replication
  (e.g. rclone to S3 or a remote NAS) is not yet configured.
