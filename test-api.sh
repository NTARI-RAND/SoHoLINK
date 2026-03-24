#!/bin/bash
# SoHoLINK API Test Script
# Usage: ./test-api.sh [-e endpoint] [-u url]

BASE_URL="${BASE_URL:-http://localhost:8080}"
ENDPOINT="${ENDPOINT:-/api/reputation/ledger}"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    -e|--endpoint) ENDPOINT="$2"; shift 2 ;;
    -u|--url) BASE_URL="$2"; shift 2 ;;
    -h|--help)
      echo "Usage: $0 [options]"
      echo "  -e, --endpoint PATH  API endpoint (default: /api/reputation/ledger)"
      echo "  -u, --url URL        Base URL (default: http://localhost:8080)"
      echo ""
      echo "Examples:"
      echo "  $0 -e /api/reputation/stats"
      echo "  $0 -e /api/topology/cluster/members"
      echo "  $0 -e /api/reputation/nodes/{did}/pricing"
      echo "  $0 -u http://192.168.1.100:8080 -e /api/health"
      exit 0
      ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

echo "[*] SoHoLINK API Test"
echo "[*] URL: $BASE_URL"
echo "[*] Endpoint: $ENDPOINT"
echo ""

# Check public endpoints first (no auth required)
case "$ENDPOINT" in
  /api/health|/api/version|/api/auth/challenge)
    echo "[*] Querying public endpoint..."
    curl -s "$BASE_URL$ENDPOINT" | python3 -m json.tool 2>/dev/null || \
    curl -s "$BASE_URL$ENDPOINT"
    echo ""
    exit 0
    ;;
esac

# For protected endpoints, run the test-api Go program
echo "[*] Protected endpoint detected"
echo "[*] Attempting Ed25519 authentication..."
echo ""

cd "$(dirname "$0")"

if [ ! -f "cmd/test-api/main.go" ]; then
  echo "[!] test-api source not found"
  echo "[*] Please run from SoHoLINK root directory"
  exit 1
fi

# Build and run
go run ./cmd/test-api -url "$BASE_URL" -endpoint "$ENDPOINT" 2>&1
