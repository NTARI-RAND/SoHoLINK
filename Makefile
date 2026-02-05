.PHONY: all build build-pi test test-short lint clean install

VERSION ?= 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"

all: build

deps:
	go mod download
	go mod tidy

build:
	go build $(LDFLAGS) -o bin/fedaaa ./cmd/fedaaa

build-pi:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/fedaaa-linux-arm64 ./cmd/fedaaa

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/fedaaa-linux-amd64 ./cmd/fedaaa

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

install: build
	sudo cp bin/fedaaa /usr/local/bin/
