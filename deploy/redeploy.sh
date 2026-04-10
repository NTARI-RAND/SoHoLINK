#!/bin/sh
# Rebuild portal and reconnect cloudflared tunnel
set -e
cd "$(dirname "$0")/.."
docker compose up -d --build portal
docker compose up -d --force-recreate cloudflared
echo "Deploy complete."
