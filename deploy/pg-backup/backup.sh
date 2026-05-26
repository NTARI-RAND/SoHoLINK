#!/bin/sh
# Daily pg_dump backup loop for the SoHoLINK postgres container.
# Writes compressed dumps to /backups and prunes files older than
# RETENTION_DAYS. Container boot fires one immediate backup, then loops.
set -u

: "${PGHOST:=postgres}"
: "${PGUSER:=postgres}"
: "${PGDATABASE:=postgres}"
: "${BACKUP_INTERVAL_SECONDS:=86400}"
: "${RETENTION_DAYS:=90}"

if [ -z "${POSTGRES_PASSWORD:-}" ]; then
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) FATAL: POSTGRES_PASSWORD is not set" >&2
  exit 1
fi
export PGPASSWORD="$POSTGRES_PASSWORD"

run_once() {
  TS=$(date -u +%Y%m%dT%H%M%SZ)
  OUT="/backups/dump-${TS}.sql.gz"
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) INFO: starting backup → $OUT"
  if pg_dump -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" \
      --no-owner --no-privileges | gzip -c > "$OUT"; then
    SIZE=$(du -sh "$OUT" 2>/dev/null | cut -f1)
    echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) INFO: backup complete — $OUT ($SIZE)"
  else
    echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) ERROR: backup failed, removing partial file $OUT" >&2
    rm -f "$OUT"
  fi
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) INFO: pruning files older than ${RETENTION_DAYS} days"
  find /backups -maxdepth 1 -name 'dump-*.sql.gz' \
    -mtime +"$RETENTION_DAYS" -print -delete
}

case "${1:-loop}" in
  once)
    run_once
    ;;
  loop)
    while true; do
      run_once
      echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) INFO: sleeping ${BACKUP_INTERVAL_SECONDS}s until next backup"
      sleep "$BACKUP_INTERVAL_SECONDS"
    done
    ;;
  *)
    echo "usage: backup.sh [once|loop]" >&2
    exit 2
    ;;
esac
