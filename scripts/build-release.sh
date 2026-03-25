#!/usr/bin/env bash
# build-release.sh — Build SoHoLINK release binaries
#
# Usage:
#   ./scripts/build-release.sh          # build for current OS
#   ./scripts/build-release.sh windows  # cross-compile for Windows
#
# Output:  bin/SoHoLINK.exe (launcher, no console)
#          bin/fedaaa-gui.exe (CLI, for advanced users)
set -euo pipefail

VERSION="${SOHOLINK_VERSION:-1.0.0}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}"

TARGET="${1:-$(go env GOOS)}"
BINDIR="bin"
mkdir -p "$BINDIR"

echo "=== SoHoLINK Release Build ==="
echo "  Version:  $VERSION"
echo "  Commit:   $COMMIT"
echo "  Target:   $TARGET"
echo ""

if [ "$TARGET" = "windows" ]; then
    EXT=".exe"
    # Launcher: -H windowsgui hides the console window
    LAUNCHER_LDFLAGS="-H windowsgui ${LDFLAGS}"
else
    EXT=""
    LAUNCHER_LDFLAGS="${LDFLAGS}"
fi

echo "[1/2] Building SoHoLINK launcher..."
GOOS="$TARGET" GOARCH=amd64 go build \
    -ldflags "${LAUNCHER_LDFLAGS}" \
    -o "${BINDIR}/SoHoLINK${EXT}" \
    ./cmd/soholink-launcher/

echo "[2/2] Building fedaaa CLI..."
GOOS="$TARGET" GOARCH=amd64 go build \
    -ldflags "${LDFLAGS}" \
    -o "${BINDIR}/fedaaa-gui${EXT}" \
    ./cmd/fedaaa-gui/

echo ""
echo "=== Build Complete ==="
ls -lh "${BINDIR}/SoHoLINK${EXT}" "${BINDIR}/fedaaa-gui${EXT}"
echo ""
echo "To install on Windows:"
echo "  1. Copy bin/SoHoLINK.exe to the target machine"
echo "  2. Double-click SoHoLINK.exe"
echo "  3. Browser opens automatically — that's it"
echo ""
echo "To build the NSIS installer (requires makensis):"
echo "  makensis installer/windows/installer.nsi"
