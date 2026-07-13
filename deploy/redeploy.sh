#!/bin/sh
# Rebuild portal + orchestrator and reconnect cloudflared tunnel
set -e
cd "$(dirname "$0")/.."

HEAD_SHA=$(git rev-parse HEAD)
echo "Checking CI status for ${HEAD_SHA}..."

# One API call; gh bundles jq so no system jq required.
# No leading slash: Git Bash rewrites /repos/... to a Windows filesystem path.
# Returns "<total> <completed> <non_success>" space-separated.
CI_COUNTS=$(gh api "repos/NTARI-RAND/SoHoLINK/commits/${HEAD_SHA}/check-runs" \
  --jq '(.check_runs | length | tostring) + " " +
        ([.check_runs[] | select(.status=="completed")] | length | tostring) + " " +
        ([.check_runs[] | select(.status=="completed") | select(.conclusion | IN("success","skipped","neutral") | not)] | length | tostring)')

if [ -z "$CI_COUNTS" ]; then
  echo "ERROR: gh api returned no output for ${HEAD_SHA}. Aborting." >&2
  exit 1
fi

TOTAL=$(echo "$CI_COUNTS" | cut -d' ' -f1)
COMPLETED=$(echo "$CI_COUNTS" | cut -d' ' -f2)
NON_SUCCESS=$(echo "$CI_COUNTS" | cut -d' ' -f3)

if [ "$TOTAL" = "0" ]; then
  echo "ERROR: No check runs found for ${HEAD_SHA}. CI may not have run yet. Aborting." >&2
  exit 1
fi
if [ "$COMPLETED" != "$TOTAL" ]; then
  echo "ERROR: CI for ${HEAD_SHA} is not finished (${COMPLETED}/${TOTAL} complete). Aborting." >&2
  exit 1
fi
if [ "$NON_SUCCESS" != "0" ]; then
  echo "ERROR: CI for ${HEAD_SHA} has ${NON_SUCCESS} non-success check(s). Aborting." >&2
  exit 1
fi
echo "CI green for ${HEAD_SHA}. Proceeding with deploy."

# Preflight: cloudflared reads its tunnel credentials from CLOUDFLARED_DIR (audit
# M5 replaced a hardcoded home path with this var, defaulting to ./deploy/cloudflared
# which does NOT hold real creds). If it is unset or holds no credential JSON, the
# recreate below crash-loops the tunnel on "tunnel credentials file not found" and
# the public site goes down. Fail fast instead — this exact gap caused a ~3-minute
# outage on 2026-07-13.
CLOUDFLARED_DIR_VAL=$(grep '^CLOUDFLARED_DIR=' .env 2>/dev/null | cut -d= -f2- || true)
if [ -z "$CLOUDFLARED_DIR_VAL" ]; then
  echo "ERROR: CLOUDFLARED_DIR is not set in .env. cloudflared would not find its tunnel credentials and the tunnel would crash-loop, taking soholink.org down. Set it to the credential dir (the one holding <tunnel-id>.json) and re-run. Aborting." >&2
  exit 1
fi
if ! ls "$CLOUDFLARED_DIR_VAL"/*.json >/dev/null 2>&1; then
  echo "ERROR: no tunnel-credential JSON found in CLOUDFLARED_DIR=$CLOUDFLARED_DIR_VAL. Aborting before the tunnel would crash-loop." >&2
  exit 1
fi
echo "Preflight OK: tunnel credentials present in $CLOUDFLARED_DIR_VAL."

docker compose up -d --build portal orchestrator frontend
docker compose up -d --force-recreate cloudflared
echo "Deploy complete."
