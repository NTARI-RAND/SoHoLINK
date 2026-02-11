.PHONY: all build build-cli build-gui build-vendor build-pi build-wizards build-installer-windows test test-short lint clean install vendor

VERSION ?= 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"
LDFLAGS_GUI := -ldflags "-s -w -H=windowsgui -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"

all: build-cli build-wizards

deps:
	go mod download
	go mod tidy

# Vendor dependencies for offline builds
vendor:
	go mod vendor
	@echo "Dependencies vendored to ./vendor/"
	@echo "To build with vendored deps: make build-vendor"

# CLI build (no GUI dependencies)
build:
	go build $(LDFLAGS) -o bin/fedaaa ./cmd/fedaaa

# Build using vendored dependencies (offline-capable)
build-vendor:
	go build $(LDFLAGS) -mod=vendor -o bin/fedaaa ./cmd/fedaaa

# Alias for clarity
build-cli: build

# GUI build (includes Fyne GUI toolkit)
build-gui:
	go build $(LDFLAGS) -tags gui -o bin/fedaaa-gui ./cmd/fedaaa-gui

build-pi:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/fedaaa-linux-arm64 ./cmd/fedaaa

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/fedaaa-linux-amd64 ./cmd/fedaaa

# GUI builds for different platforms
build-gui-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -tags gui -o bin/fedaaa-gui.exe ./cmd/fedaaa-gui

build-gui-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -tags gui -o bin/fedaaa-gui-linux ./cmd/fedaaa-gui

build-gui-macos:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -tags gui -o bin/fedaaa-gui-macos ./cmd/fedaaa-gui

test:
	go test -v -race -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out -o coverage.html

test-short:
	go test -v -short ./internal/...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Build wizards (GUI and CLI)
build-wizards:
	go build $(LDFLAGS_GUI) -o bin/soholink-wizard.exe ./cmd/soholink-wizard
	go build $(LDFLAGS) -o bin/soholink-wizard-cli.exe ./cmd/soholink-wizard-cli

# Build wizard demo
build-wizard-demo:
	go build $(LDFLAGS) -o bin/wizard-demo.exe ./cmd/wizard-demo

# Build complete Windows installer package
build-installer-windows: build-wizards
	@echo "Building Windows installer package..."
	powershell -ExecutionPolicy Bypass -File ./scripts/build-installer-windows.ps1 -Version $(VERSION)

# Build complete Windows installer with embedded Go
build-installer-windows-portable: build-wizards
	@echo "Building portable Windows installer package (with embedded Go)..."
	powershell -ExecutionPolicy Bypass -File ./scripts/build-installer-windows.ps1 -Version $(VERSION)

# Quick build for testing (skips Go download)
build-installer-windows-quick: build-wizards
	@echo "Building Windows installer package (quick mode)..."
	powershell -ExecutionPolicy Bypass -File ./scripts/build-installer-windows.ps1 -Version $(VERSION) -SkipGoDownload

install: build
	sudo cp bin/fedaaa /usr/local/bin/

# Help target
help:
	@echo "SoHoLINK Build Targets:"
	@echo ""
	@echo "  make build-cli                    - Build CLI binary only"
	@echo "  make build-gui                    - Build GUI binary"
	@echo "  make build-wizards                - Build configuration wizards (GUI + CLI)"
	@echo "  make build-wizard-demo            - Build wizard demo"
	@echo "  make build-installer-windows      - Build complete Windows installer (uses system Go)"
	@echo "  make build-installer-windows-portable - Build Windows installer with embedded Go"
	@echo "  make build-pi                     - Cross-compile for Raspberry Pi (ARM64)"
	@echo "  make test                         - Run all tests with coverage"
	@echo "  make test-short                   - Run quick tests"
	@echo "  make clean                        - Remove build artifacts"
	@echo "  make vendor                       - Vendor dependencies for offline builds"
	@echo ""
