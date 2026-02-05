# Testing Guide

## Running Tests

### All Tests

```bash
# Run all tests
go test -v ./...

# Run with verbose output
go test -v ./internal/... ./test/...

# Skip integration tests (faster)
go test -v -short ./...
```

### Unit Tests Only

```bash
# All internal packages
go test -v ./internal/...

# Specific package
go test -v ./internal/verifier/...
go test -v ./internal/store/...
go test -v ./internal/policy/...
```

### Integration Tests

```bash
# Full end-to-end RADIUS tests
go test -v ./test/integration/...

# With timeout (integration tests can take longer)
go test -v -timeout 60s ./test/integration/...
```

## Test Coverage

### Generate Coverage Report

```bash
# Generate coverage profile
go test -coverprofile=coverage.out ./internal/...

# View in terminal
go tool cover -func=coverage.out

# Generate HTML report
go tool cover -html=coverage.out -o coverage.html
```

### Coverage by Package

| Package | Tests | Coverage Target |
|---------|-------|-----------------|
| `internal/verifier` | 19 tests | 90%+ |
| `internal/store` | 11 tests | 85%+ |
| `internal/policy` | 7 tests | 80%+ |
| `internal/accounting` | 5 tests | 80%+ |
| `internal/merkle` | 12 tests | 90%+ |
| `internal/did` | 5 tests | 95%+ |

## Test Categories

### Cryptographic Verification Tests

```bash
go test -v -run "TestVerify" ./internal/verifier/...
go test -v -run "TestCreate" ./internal/verifier/...
go test -v -run "TestDID" ./internal/did/...
```

Key tests:
- `TestVerifyValidCredential` - Happy path authentication
- `TestVerifyInvalidSignature` - Rejects wrong key
- `TestUsernameSwapPrevented` - Security: credential bound to username
- `TestCredentialBindingToUsername` - Username hash differs per user

### Security Tests

```bash
go test -v -run "TestUsernameSwap" ./internal/verifier/...
go test -v -run "TestClockSkew" ./internal/verifier/...
go test -v -run "TestNonceReplay" ./internal/verifier/...
```

Key tests:
- `TestUsernameSwapPrevented` - CRITICAL: token can't be reused for different user
- `TestUsernameSwapWithValidSignature` - Edge case with both users existing
- `TestClockSkewToleranceFuture` - Accepts tokens from fast clocks
- `TestClockSkewToleranceExcessive` - Rejects far-future tokens
- `TestClockSkewTolerancePast` - Accepts recently expired within tolerance
- `TestVerifyNonceReplay` - Rejects duplicate tokens

### SQLite Operations Tests

```bash
go test -v ./internal/store/...
```

Key tests:
- `TestAddAndGetUser` - User CRUD
- `TestRevokeUser` - Revocation flow
- `TestNonceReplayProtection` - Nonce cache
- `TestPruneExpiredNonces` - Cleanup old nonces

### RADIUS Protocol Tests

```bash
go test -v ./test/integration/...
```

Key tests:
- `TestEndToEndAuthentication` - Full auth flow with real RADIUS
- `TestAuthenticationInvalidUser` - Rejects unknown user
- `TestAuthenticationRevokedUser` - Rejects revoked user
- `TestAuthenticationReplayProtection` - Rejects replayed token
- `TestAccountingEventLogged` - Verifies event recording

### OPA Policy Evaluation Tests

```bash
go test -v ./internal/policy/...
```

Key tests:
- `TestEvaluateAllowAuthenticated` - Default policy allows auth users
- `TestEvaluateDenyUnauthenticated` - Denies without auth
- `TestEvaluateRoleBasedPolicy` - Role-based access control
- `TestEngineReload` - Hot-reload policies

## Manual Testing

### With radclient (Linux)

```bash
# Install freeradius-utils
sudo apt install freeradius-utils

# Create user
./fedaaa users add testuser

# Note the token from output, then:
echo "User-Name=testuser,User-Password=<token>" | radclient -x localhost:1812 auth testing123

# Expected: Access-Accept with Reply-Message
```

### With Go Test Client

```go
package main

import (
    "context"
    "fmt"
    "layeh.com/radius"
    "layeh.com/radius/rfc2865"
)

func main() {
    packet := radius.New(radius.CodeAccessRequest, []byte("testing123"))
    rfc2865.UserName_SetString(packet, "alice")
    rfc2865.UserPassword_SetString(packet, "<token>")

    response, err := radius.Exchange(context.Background(), packet, "localhost:1812")
    if err != nil {
        panic(err)
    }

    fmt.Printf("Response: %v\n", response.Code)
    fmt.Printf("Message: %s\n", rfc2865.ReplyMessage_GetString(response))
}
```

### Policy Testing

```bash
# Test policy with sample input
./fedaaa policy test --user alice --did "did:key:z6Mk..." --role basic --authenticated

# Test denial scenarios
./fedaaa policy test --user "" --authenticated  # Should deny: empty user
./fedaaa policy test --user alice               # Should deny: not authenticated
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Run tests
        run: go test -v -coverprofile=coverage.out ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: ./coverage.out

  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        goos: [linux, windows, darwin]
        goarch: [amd64, arm64]
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Build
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: go build -o fedaaa-${{ matrix.goos }}-${{ matrix.goarch }} ./cmd/fedaaa
```

### GitLab CI Example

```yaml
stages:
  - test
  - build

test:
  stage: test
  image: golang:1.22
  script:
    - go test -v -coverprofile=coverage.out ./...
    - go tool cover -func=coverage.out
  coverage: '/total:\s+\(statements\)\s+(\d+.\d+)%/'

build:
  stage: build
  image: golang:1.22
  script:
    - GOOS=linux GOARCH=amd64 go build -o fedaaa-linux-amd64 ./cmd/fedaaa
    - GOOS=linux GOARCH=arm64 go build -o fedaaa-linux-arm64 ./cmd/fedaaa
  artifacts:
    paths:
      - fedaaa-*
```

## Benchmarking

```bash
# Run benchmarks
go test -bench=. ./internal/verifier/...
go test -bench=. ./internal/merkle/...

# With memory profiling
go test -bench=. -benchmem ./internal/verifier/...
```

## Debugging Test Failures

### Verbose Logging

```bash
# Show all log output
go test -v ./internal/verifier/... 2>&1 | tee test.log

# Filter specific test
go test -v -run TestUsernameSwapPrevented ./internal/verifier/...
```

### Race Detection

Note: Race detection requires CGO, which may not be available on all platforms.

```bash
# With CGO enabled
CGO_ENABLED=1 go test -race ./internal/...
```

### Database Inspection

For store tests, you can inspect the in-memory database state by adding temporary debug code:

```go
func TestDebug(t *testing.T) {
    s, _ := store.NewMemoryStore()
    // ... test operations ...

    // Debug: dump users table
    rows, _ := s.DB().Query("SELECT * FROM users")
    // ... inspect rows ...
}
```
