#!/usr/bin/env bash
# One-time workload entry registration for SPIRE.
# Run from repo root after spire-agent first comes healthy.
# Safe to re-run — SPIRE will reject a duplicate entry without modifying it.
# Usage: bash deploy/register-entries.sh

set -euo pipefail

if [ ! -f .env ]; then
  echo "ERROR: .env not found. Run from repo root." >&2
  exit 1
fi

SPIRE_AGENT_JOIN_TOKEN=$(grep '^SPIRE_AGENT_JOIN_TOKEN=' .env | cut -d= -f2-)
if [ -z "${SPIRE_AGENT_JOIN_TOKEN}" ]; then
  echo "ERROR: SPIRE_AGENT_JOIN_TOKEN not set in .env" >&2
  exit 1
fi

PARENT_ID="spiffe://soholink.org/spire/agent/join_token/${SPIRE_AGENT_JOIN_TOKEN}"
SOCKET="/run/spire-server/private/api.sock"

echo "Registering orchestrator workload entry..."
echo "  parentID : ${PARENT_ID}"
echo "  spiffeID : spiffe://soholink.org/orchestrator"
echo "  selector : unix:uid:0"
echo "  ttl      : 3600s (SVID rotation interval)"

docker compose exec spire-server \
  /opt/spire/bin/spire-server entry create \
    -socketPath "${SOCKET}" \
    -parentID   "${PARENT_ID}" \
    -spiffeID   "spiffe://soholink.org/orchestrator" \
    -selector   "unix:uid:0" \
    -ttl        3600 \
  || echo "NOTE: entry may already exist — continuing."

echo "Done. Verify with:"
echo "  docker compose exec spire-server /opt/spire/bin/spire-server entry show -socketPath ${SOCKET}"
