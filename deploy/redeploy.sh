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

docker compose up -d --build portal orchestrator
docker compose up -d --force-recreate cloudflared
echo "Deploy complete."
