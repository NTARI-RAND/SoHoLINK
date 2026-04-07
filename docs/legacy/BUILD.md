# SoHoLINK Build Guide

This document explains how to build SoHoLINK for different platforms and configurations.

## Prerequisites

- **Go 1.22+** - [Download from golang.org](https://go.dev/dl/)
- **Git** - For cloning the repository
- **Make** - Build automation (optional, but recommended)

### Platform-Specific Requirements

#### Linux
```bash
# Debian/Ubuntu - Required for GUI builds
sudo apt-get install gcc libgl1-mesa-dev xorg-dev

# Fedora/RHEL - Required for GUI builds
sudo dnf install gcc libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel mesa-libGL-devel libXi-devel libXxf86vm-devel
```

#### macOS
```bash
# Xcode command line tools (required for GUI builds)
xcode-select --install
```

#### Windows
- No additional dependencies required for basic builds
- For GUI builds: MinGW-w64 or Visual Studio Build Tools (optional, Go's native toolchain works)

## Build Modes

SoHoLINK supports two build modes:

### 1. CLI-Only Build (Default)

Builds the command-line tool without GUI dependencies. This is the default and produces a smaller binary.

```bash
# Using Make
make build-cli
# or simply
make build

# Direct go build
go build -o bin/fedaaa ./cmd/fedaaa
```

**Output:** `bin/fedaaa` (or `bin/fedaaa.exe` on Windows)

**Use when:**
- Running on headless servers
- Deploying to embedded systems
- Building minimal Docker images
- You don't need the graphical installer/dashboard

### 2. GUI Build

Builds the full application including the Fyne-based graphical user interface.

```bash
# Using Make
make build-gui

# Direct go build
go build -tags gui -o bin/fedaaa-gui ./cmd/fedaaa-gui
```

**Output:** `bin/fedaaa-gui` (or `bin/fedaaa-gui.exe` on Windows)

**Use when:**
- Running on desktop systems
- You want the graphical installer wizard
- You want the dashboard GUI for node management

## Cross-Platform Builds

### Linux ARM64 (Raspberry Pi)
```bash
make build-pi
# Output: bin/fedaaa-linux-arm64
```

### Linux AMD64
```bash
make build-linux-amd64
# Output: bin/fedaaa-linux-amd64
```

### Cross-Platform GUI Builds
```bash
# Windows GUI
make build-gui-windows
# Output: bin/fedaaa-gui.exe

# Linux GUI
make build-gui-linux
# Output: bin/fedaaa-gui-linux

# macOS GUI
make build-gui-macos
# Output: bin/fedaaa-gui-macos
```

## Build Targets Reference

| Target | Description | Output |
|--------|-------------|--------|
| `make build` | CLI-only build (default) | `bin/fedaaa` |
| `make build-cli` | Alias for `build` | `bin/fedaaa` |
| `make build-gui` | GUI build (native platform) | `bin/fedaaa-gui` |
| `make build-pi` | CLI for Raspberry Pi ARM64 | `bin/fedaaa-linux-arm64` |
| `make build-linux-amd64` | CLI for Linux x86-64 | `bin/fedaaa-linux-amd64` |
| `make build-gui-windows` | GUI for Windows x86-64 | `bin/fedaaa-gui.exe` |
| `make build-gui-linux` | GUI for Linux x86-64 | `bin/fedaaa-gui-linux` |
| `make build-gui-macos` | GUI for macOS x86-64 | `bin/fedaaa-gui-macos` |
| `make deps` | Download and tidy dependencies | N/A |
| `make test` | Run all tests with coverage | `coverage.out`, `coverage.html` |
| `make test-short` | Run short tests only | N/A |
| `make lint` | Run golangci-lint | N/A |
| `make clean` | Remove build artifacts | N/A |
| `make install` | Install CLI to `/usr/local/bin/` | N/A |

## Build Variables

You can customize the build with these environment variables:

```bash
VERSION=1.0.0 COMMIT=abc123 make build
```

- **VERSION** - Semantic version (default: `0.1.0`)
- **COMMIT** - Git commit hash (auto-detected)
- **BUILD_TIME** - Build timestamp (auto-generated)

These are embedded in the binary and shown with `fedaaa version`.

## Development Workflow

### Initial Setup
```bash
# Clone the repository
git clone https://github.com/NetworkTheoryAppliedResearchInstitute/soholink.git
cd soholink

# Download dependencies
make deps

# Build CLI
make build-cli

# Test your build
./bin/fedaaa version
```

### Building Both Modes
```bash
# Build CLI and GUI in one command
make build-cli && make build-gui

# Verify both binaries
ls -lh bin/
```

### Testing Before Release
```bash
# Run tests
make test-short

# Build for all target platforms
make build-linux-amd64
make build-gui-linux
make build-pi
```

## Troubleshooting

### "GUI support not compiled in"

**Problem:** You ran `fedaaa dashboard` but built without the `gui` tag.

**Solution:** Build with GUI support:
```bash
make build-gui
./bin/fedaaa-gui dashboard
```

### "cannot find package fyne.io/fyne/v2"

**Problem:** Dependencies not downloaded.

**Solution:**
```bash
make deps
# or
go mod download
```

### CGO Errors on Cross-Compilation

**Problem:** CGO is disabled by default for cross-compilation, but some dependencies may require it.

**Solution for Linux → Windows:**
```bash
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
  go build -tags gui -o bin/fedaaa-gui.exe ./cmd/fedaaa-gui
```

**Solution for macOS → Linux:**
```bash
CGO_ENABLED=1 CC=x86_64-linux-gnu-gcc GOOS=linux GOARCH=amd64 \
  go build -tags gui -o bin/fedaaa-gui-linux ./cmd/fedaaa-gui
```

### Binary Size Optimization

The default build includes symbol stripping (`-s -w` ldflags). If you need even smaller binaries:

```bash
# Use UPX compression (install upx first)
make build
upx --best --lzma bin/fedaaa

# Original: ~40MB → Compressed: ~10MB
```

## CI/CD Integration

### GitHub Actions Example
```yaml
name: Build
on: [push, pull_request]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install GUI dependencies
        run: |
          sudo apt-get update
          sudo apt-get install -y gcc libgl1-mesa-dev xorg-dev

      - name: Build CLI
        run: make build-cli

      - name: Build GUI
        run: make build-gui

      - name: Run tests
        run: make test-short

      - name: Upload artifacts
        uses: actions/upload-artifact@v4
        with:
          name: binaries
          path: bin/
```

## Next Steps

After building:

1. **Initialize the node:** `./bin/fedaaa install`
2. **Start the server:** `./bin/fedaaa start`
3. **Open the dashboard:** `./bin/fedaaa-gui dashboard` (GUI build only)

For more information:
- [Installation Guide](docs/INSTALL.md)
- [Architecture Overview](docs/ARCHITECTURE.md)
- [README](README.md)
