#!/bin/sh
# NTARI OS — Globe WebSocket Bridge
# Phase 8: Globe Interface — Production
#
# Bridges the live ROS2 DDS graph to the browser globe interface via WebSocket.
#
# Architecture:
#   This script runs a lightweight HTTP + WebSocket server in POSIX sh using
#   socat (a dependency available in Alpine's main repo).  It polls the ROS2
#   graph at POLL_INTERVAL seconds, builds a JSON snapshot, and serves it to
#   all connected WebSocket clients.
#
#   The bridge exposes two endpoints:
#     GET  /status   — HTTP 200 JSON health check (non-WebSocket, for curl)
#     GET  /graph    — WebSocket upgrade → streams graph JSON at POLL_INTERVAL
#
# WebSocket message schema (JSON, sent at each poll cycle):
#   {
#     "type":      "graph_snapshot",
#     "timestamp": "2026-02-19T12:00:00Z",
#     "domain":    0,
#     "local_node_uuid": "...",
#     "nodes": [
#       {
#         "id":      "/ntari/dns",
#         "name":    "ntari_dns_node",
#         "health":  "healthy",
#         "latency": 4,
#         "topics":  ["/ntari/dns/health", "/ntari/dns/status"]
#       }, ...
#     ],
#     "edges": [
#       { "from": "/ntari/dns", "to": "/ntari/ntp", "topic": "/ntari/ntp/health" }
#     ]
#   }
#
# Dependencies:
#   - socat        (apk add socat)     — WebSocket/TCP server
#   - ros2 CLI     (/usr/ros/jazzy)    — graph introspection
#   - ros2 daemon  (running)           — fast node/topic queries
#
# Usage:
#   ntari-globe-bridge [--port PORT] [--interval SECONDS] [--bind ADDR]
#
# The OpenRC service (ntari-globe-bridge.initd) manages lifecycle.

set -e

# ── Defaults ────────────────────────────────────────────────────────────────
BRIDGE_PORT="${NTARI_GLOBE_PORT:-9090}"
BRIDGE_BIND="${NTARI_GLOBE_BIND:-127.0.0.1}"
POLL_INTERVAL="${NTARI_GLOBE_INTERVAL:-2}"
LOG_FILE="${NTARI_GLOBE_LOG:-/var/log/ntari/globe-bridge.log}"
GRAPH_CACHE="/run/ntari-globe-graph.json"
DOMAIN_ID="${ROS_DOMAIN_ID:-0}"

# ── Parse args ──────────────────────────────────────────────────────────────
while [ $# -gt 0 ]; do
    case "$1" in
        --port)     BRIDGE_PORT="$2"; shift 2 ;;
        --interval) POLL_INTERVAL="$2"; shift 2 ;;
        --bind)     BRIDGE_BIND="$2"; shift 2 ;;
        --log)      LOG_FILE="$2"; shift 2 ;;
        --help|-h)
            echo "Usage: ntari-globe-bridge [--port PORT] [--interval SECS] [--bind ADDR]"
            exit 0
            ;;
        *) shift ;;
    esac
done

# ── ROS2 environment ─────────────────────────────────────────────────────────
export AMENT_PREFIX_PATH="/usr/ros/jazzy"
export CMAKE_PREFIX_PATH="/usr/ros/jazzy"
export LD_LIBRARY_PATH="/usr/ros/jazzy/lib:/usr/ros/jazzy/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export PATH="/usr/ros/jazzy/bin:/usr/local/sbin:/usr/sbin:/sbin:${PATH}"
export PYTHONPATH="/usr/ros/jazzy/lib/python3.12/site-packages${PYTHONPATH:+:$PYTHONPATH}"
export RMW_IMPLEMENTATION="${RMW_IMPLEMENTATION:-rmw_cyclonedds_cpp}"
export ROS_DOMAIN_ID="${DOMAIN_ID}"
export ROS_VERSION="2"
export ROS_PYTHON_VERSION="3"
export ROS_DISTRO="jazzy"
if [ -f /etc/ntari/cyclonedds.xml ]; then
    export CYCLONEDDS_URI="file:///etc/ntari/cyclonedds.xml"
fi

# ── Helpers ──────────────────────────────────────────────────────────────────
log()  { echo "[globe-bridge] $*" | tee -a "${LOG_FILE}" >&2; }
ts()   { date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown"; }

# ── Read node UUID ────────────────────────────────────────────────────────────
LOCAL_UUID=""
if [ -f /var/lib/ntari/identity/node-uuid ]; then
    LOCAL_UUID=$(cat /var/lib/ntari/identity/node-uuid)
fi

# ── Measure round-trip latency to a ROS2 service node ─────────────────────────
# We approximate latency by timing a `ros2 topic echo --once` call.
# This is not true DDS round-trip time but gives a useful graph-topology proxy.
_measure_latency() {
    NODE_NAME="$1"
    # Try to ping the /ntari/<basename>/health topic
    BASE=$(echo "${NODE_NAME}" | sed 's|.*/||')
    HEALTH_TOPIC="/ntari/${BASE}/health"
    START_NS=$(date +%s%N 2>/dev/null || echo "0")
    timeout 1 ros2 topic echo --once "${HEALTH_TOPIC}" \
        >/dev/null 2>&1 || true
    END_NS=$(date +%s%N 2>/dev/null || echo "0")
    # Compute milliseconds
    DIFF=$(( (END_NS - START_NS) / 1000000 ))
    # Clamp to sane range: 1–9999ms; fallback to 999 on failure
    if [ "${DIFF}" -lt 1 ] || [ "${DIFF}" -gt 9999 ]; then
        echo "999"
    else
        echo "${DIFF}"
    fi
}

# ── Build graph JSON snapshot ──────────────────────────────────────────────────
_build_graph_json() {
    TS=$(ts)

    # Get node list from ros2 daemon cache (fast)
    NODE_LIST=$(ros2 node list 2>/dev/null || echo "")
    TOPIC_LIST=$(ros2 topic list 2>/dev/null || echo "")

    # Start JSON
    JSON="{\"type\":\"graph_snapshot\",\"timestamp\":\"${TS}\",\"domain\":${DOMAIN_ID},\"local_node_uuid\":\"${LOCAL_UUID}\",\"nodes\":["

    FIRST_NODE=1
    EDGE_JSON=""
    FIRST_EDGE=1

    for node in ${NODE_LIST}; do
        # Node name: strip leading slash, replace / with .
        DISPLAY=$(echo "${node}" | sed 's|^/||; s|/|.|g')

        # Get topics this node publishes/subscribes (best effort)
        NODE_TOPICS=$(ros2 node info "${node}" 2>/dev/null \
            | awk '/Publishers:|Subscribers:/{p=1} p && /Topic:/{print $2} /^$/{p=0}' \
            | sort -u | head -10 | tr '\n' ',' | sed 's/,$//')

        # Latency measurement (only for NTARI service nodes to keep poll fast)
        LATENCY="1"
        if echo "${node}" | grep -q "ntari"; then
            LATENCY=$(_measure_latency "${node}")
        fi

        # Read health from the DDS health topic (non-blocking)
        HEALTH="unknown"
        BASE=$(echo "${node}" | sed 's|.*/||')
        CACHED_HEALTH=$(timeout 0.3 ros2 topic echo --once \
            "/ntari/${BASE}/health" 2>/dev/null \
            | awk '/data:/{print $2}' | tr -d "'" || echo "")
        [ -n "${CACHED_HEALTH}" ] && HEALTH="${CACHED_HEALTH}"

        # Build topics JSON array
        TOPICS_ARR="["
        FIRST_T=1
        IFS=','
        for t in ${NODE_TOPICS}; do
            [ -z "${t}" ] && continue
            if [ "${FIRST_T}" = "1" ]; then
                TOPICS_ARR="${TOPICS_ARR}\"${t}\""
                FIRST_T=0
            else
                TOPICS_ARR="${TOPICS_ARR},\"${t}\""
            fi
            # Build edge for each topic shared with another node
        done
        unset IFS
        TOPICS_ARR="${TOPICS_ARR}]"

        NODE_JSON="{\"id\":\"${node}\",\"name\":\"${DISPLAY}\",\"health\":\"${HEALTH}\",\"latency\":${LATENCY},\"topics\":${TOPICS_ARR}}"

        if [ "${FIRST_NODE}" = "1" ]; then
            JSON="${JSON}${NODE_JSON}"
            FIRST_NODE=0
        else
            JSON="${JSON},${NODE_JSON}"
        fi
    done

    JSON="${JSON}],\"edges\":["

    # Build edges from shared topic subscriptions
    # For each topic, find which nodes publish/subscribe to it
    for topic in ${TOPIC_LIST}; do
        # Get publisher and subscriber node names for this topic
        PUB=$(ros2 topic info "${topic}" 2>/dev/null \
            | awk '/Publisher count:/{p=1} /Subscription count:/{p=0} p && /Node name:/{print $3}' \
            | head -1)
        SUB=$(ros2 topic info "${topic}" 2>/dev/null \
            | awk '/Subscription count:/{p=1} p && /Node name:/{print $3}' \
            | head -1)

        if [ -n "${PUB}" ] && [ -n "${SUB}" ] && [ "${PUB}" != "${SUB}" ]; then
            PUB_PATH=$(echo "${NODE_LIST}" | grep "${PUB}" | head -1)
            SUB_PATH=$(echo "${NODE_LIST}" | grep "${SUB}" | head -1)
            if [ -n "${PUB_PATH}" ] && [ -n "${SUB_PATH}" ]; then
                # Escape topic for JSON
                TOPIC_ESC=$(printf '%s' "${topic}" | sed 's/"/\\"/g')
                EDGE="{\"from\":\"${PUB_PATH}\",\"to\":\"${SUB_PATH}\",\"topic\":\"${TOPIC_ESC}\"}"
                if [ "${FIRST_EDGE}" = "1" ]; then
                    EDGE_JSON="${EDGE}"
                    FIRST_EDGE=0
                else
                    EDGE_JSON="${EDGE_JSON},${EDGE}"
                fi
            fi
        fi
    done

    JSON="${JSON}${EDGE_JSON}]}"
    echo "${JSON}"
}

# ── WebSocket handshake helpers (RFC 6455) ────────────────────────────────────
# We implement the minimal WebSocket server protocol in sh using socat.
# The handshake requires computing SHA-1(key + GUID) and base64-encoding it.
# We use openssl for this since it's always available on Alpine.

_ws_accept_key() {
    KEY="$1"
    GUID="258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
    printf '%s' "${KEY}${GUID}" \
        | openssl dgst -sha1 -binary \
        | openssl enc -base64 \
        | tr -d '\n'
}

# ── HTTP/WebSocket request handler ───────────────────────────────────────────
# This function is called once per socat connection (each client connection).
# It reads the HTTP request, performs a WebSocket upgrade, then streams JSON.
_handle_connection() {
    # Read HTTP request headers
    WS_KEY=""
    IS_WEBSOCKET=0
    PATH_REQ="/"

    while IFS= read -r line; do
        # Strip carriage return
        line=$(printf '%s' "${line}" | tr -d '\r')
        [ -z "${line}" ] && break

        case "${line}" in
            GET\ /graph*)   PATH_REQ="/graph" ;;
            GET\ /status*)  PATH_REQ="/status" ;;
            *Upgrade:*websocket*) IS_WEBSOCKET=1 ;;
            *Sec-WebSocket-Key:*)
                WS_KEY=$(echo "${line}" | sed 's/.*: //' | tr -d '\r\n ')
                ;;
        esac
    done

    if [ "${PATH_REQ}" = "/status" ]; then
        # Simple HTTP health check (no WebSocket)
        CACHED=$(cat "${GRAPH_CACHE}" 2>/dev/null || echo "{}")
        BODY="{\"status\":\"ok\",\"bridge\":\"ntari-globe-bridge\",\"domain\":${DOMAIN_ID}}"
        printf 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s' \
            "${#BODY}" "${BODY}"
        return
    fi

    if [ "${IS_WEBSOCKET}" = "1" ] && [ -n "${WS_KEY}" ]; then
        ACCEPT=$(_ws_accept_key "${WS_KEY}")
        # Send 101 Switching Protocols
        printf 'HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n' \
            "${ACCEPT}"

        # Stream graph updates
        while true; do
            JSON=$(cat "${GRAPH_CACHE}" 2>/dev/null || echo '{"type":"error","message":"graph not ready"}')
            # Encode as WebSocket text frame (opcode 0x1, no masking for server→client)
            LEN=${#JSON}
            if [ "${LEN}" -lt 126 ]; then
                # Single-byte length
                printf '\x81'
                printf "\\x$(printf '%02x' "${LEN}")"
            else
                # Two-byte extended length (handles up to 65535 bytes)
                printf '\x81\x7e'
                HI=$(( (LEN >> 8) & 0xff ))
                LO=$(( LEN & 0xff ))
                printf "\\x$(printf '%02x' "${HI}")\\x$(printf '%02x' "${LO}")"
            fi
            printf '%s' "${JSON}"
            sleep "${POLL_INTERVAL}"
        done
    else
        # Not a WebSocket request
        printf 'HTTP/1.1 400 Bad Request\r\nContent-Type: text/plain\r\nContent-Length: 22\r\nConnection: close\r\n\r\nWebSocket required here'
    fi
}

# ── Graph poller (background) ─────────────────────────────────────────────────
# Runs continuously, refreshing the graph cache every POLL_INTERVAL seconds.
_poll_loop() {
    log "Graph poller started (interval: ${POLL_INTERVAL}s, domain: ${DOMAIN_ID})"
    while true; do
        if ros2 daemon status >/dev/null 2>&1; then
            JSON=$(_build_graph_json)
            printf '%s' "${JSON}" > "${GRAPH_CACHE}.tmp"
            mv "${GRAPH_CACHE}.tmp" "${GRAPH_CACHE}"
        else
            log "WARN: ros2 daemon not responding — skipping poll"
            printf '{"type":"graph_snapshot","timestamp":"%s","domain":%d,"local_node_uuid":"%s","nodes":[],"edges":[],"error":"ros2_daemon_unavailable"}' \
                "$(ts)" "${DOMAIN_ID}" "${LOCAL_UUID}" > "${GRAPH_CACHE}"
        fi
        sleep "${POLL_INTERVAL}"
    done
}

# ── Main ──────────────────────────────────────────────────────────────────────
mkdir -p /var/log/ntari
mkdir -p /run/ntari-globe

log "NTARI Globe Bridge starting"
log "  Bind:     ${BRIDGE_BIND}:${BRIDGE_PORT}"
log "  Interval: ${POLL_INTERVAL}s"
log "  Domain:   ${DOMAIN_ID}"

# Verify dependencies
if [ ! -f /usr/ros/jazzy/bin/ros2 ]; then
    log "ERROR: ros2 not found — install ROS2 Jazzy first"
    exit 1
fi
if ! command -v socat >/dev/null 2>&1; then
    log "ERROR: socat not found — install via: apk add socat"
    exit 1
fi
if ! command -v openssl >/dev/null 2>&1; then
    log "ERROR: openssl not found — install via: apk add openssl"
    exit 1
fi

# Write initial empty cache
printf '{"type":"graph_snapshot","timestamp":"%s","domain":%d,"local_node_uuid":"%s","nodes":[],"edges":[]}' \
    "$(ts)" "${DOMAIN_ID}" "${LOCAL_UUID}" > "${GRAPH_CACHE}"

# Start graph poller in background
_poll_loop >> "${LOG_FILE}" 2>&1 &
POLLER_PID=$!
log "Graph poller started (PID ${POLLER_PID})"

# Trap for clean shutdown
trap 'kill ${POLLER_PID} 2>/dev/null; exit 0' TERM INT

# Start socat WebSocket server
# Each connection forks a shell that calls _handle_connection
log "WebSocket server listening on ${BRIDGE_BIND}:${BRIDGE_PORT}"

# Export everything the forked handler needs
export GRAPH_CACHE POLL_INTERVAL DOMAIN_ID LOCAL_UUID LOG_FILE

socat \
    TCP-LISTEN:"${BRIDGE_PORT}",bind="${BRIDGE_BIND}",reuseaddr,fork \
    EXEC:"sh -c 'export GRAPH_CACHE=$GRAPH_CACHE POLL_INTERVAL=$POLL_INTERVAL DOMAIN_ID=$DOMAIN_ID LOCAL_UUID=$LOCAL_UUID LOG_FILE=$LOG_FILE; . /usr/local/bin/ntari-globe-bridge.sh; _handle_connection'" \
    >> "${LOG_FILE}" 2>&1 &

SOCAT_PID=$!
log "socat server started (PID ${SOCAT_PID})"

# Wait for both processes
wait ${SOCAT_PID}
kill ${POLLER_PID} 2>/dev/null
