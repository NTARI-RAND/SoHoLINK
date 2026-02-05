package verifier

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/did"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

func setupTestVerifier(t *testing.T) (*Verifier, *store.Store, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()

	s, err := store.NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore failed: %v", err)
	}

	pub, priv, err := did.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair failed: %v", err)
	}

	v := NewVerifier(s, 1*time.Hour, 5*time.Minute)

	didStr := did.EncodeDIDKey(pub)
	err = s.AddUser(context.Background(), "alice", didStr, pub, "basic")
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	return v, s, pub, priv
}

func TestVerifyValidCredential(t *testing.T) {
	v, _, _, priv := setupTestVerifier(t)

	token, err := CreateCredential("alice", priv)
	if err != nil {
		t.Fatalf("CreateCredential failed: %v", err)
	}

	result, err := v.Verify(context.Background(), "alice", token)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected ALLOW, got DENY: %s", result.Reason)
	}
	if result.Username != "alice" {
		t.Errorf("expected username 'alice', got '%s'", result.Username)
	}
}

func TestVerifyUserNotFound(t *testing.T) {
	v, _, _, priv := setupTestVerifier(t)
	token, _ := CreateCredential("nonexistent", priv)

	result, err := v.Verify(context.Background(), "nonexistent", token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected DENY for nonexistent user")
	}
}

func TestVerifyExpiredCredential(t *testing.T) {
	v, _, _, priv := setupTestVerifier(t)
	v.credentialTTL = 1 * time.Millisecond
	v.clockSkewTolerance = 0 // Disable skew tolerance for this test

	token, _ := CreateCredential("alice", priv)
	time.Sleep(10 * time.Millisecond)

	result, err := v.Verify(context.Background(), "alice", token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected DENY for expired credential")
	}
}

func TestVerifyInvalidSignature(t *testing.T) {
	v, _, _, _ := setupTestVerifier(t)
	_, otherPriv, _ := did.GenerateKeypair()
	// Create token with alice's username but wrong key
	token, _ := CreateCredential("alice", otherPriv)

	result, err := v.Verify(context.Background(), "alice", token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected DENY for invalid signature")
	}
}

func TestVerifyRevokedUser(t *testing.T) {
	v, s, _, priv := setupTestVerifier(t)
	_ = s.RevokeUser(context.Background(), "alice", "test revocation")

	token, _ := CreateCredential("alice", priv)
	result, err := v.Verify(context.Background(), "alice", token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected DENY for revoked user")
	}
}

func TestVerifyNonceReplay(t *testing.T) {
	v, _, _, priv := setupTestVerifier(t)
	token, _ := CreateCredential("alice", priv)

	result, _ := v.Verify(context.Background(), "alice", token)
	if !result.Allowed {
		t.Errorf("first use should be allowed: %s", result.Reason)
	}

	result, _ = v.Verify(context.Background(), "alice", token)
	if result.Allowed {
		t.Error("replay should be denied")
	}
}

func TestVerifyMalformedToken(t *testing.T) {
	v, _, _, _ := setupTestVerifier(t)

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"not base64", "!!!invalid!!!"},
		{"too short", base64.RawURLEncoding.EncodeToString([]byte("short"))},
		{"too long", base64.RawURLEncoding.EncodeToString(make([]byte, 100))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := v.Verify(context.Background(), "alice", tt.token)
			if err != nil {
				return
			}
			if result.Allowed {
				t.Error("malformed token should be denied")
			}
		})
	}
}

func TestCreateCredentialFitsRADIUS(t *testing.T) {
	_, priv, _ := did.GenerateKeypair()
	token, _ := CreateCredential("alice", priv)

	if len(token) > 128 {
		t.Errorf("token too long for RADIUS PAP: %d bytes (max 128)", len(token))
	}
	t.Logf("Token length: %d characters", len(token))
}

func TestCreateCredentialRoundtrip(t *testing.T) {
	pub, priv, _ := did.GenerateKeypair()
	token, _ := CreateCredential("alice", priv)

	cred, err := ParseCredential(token)
	if err != nil {
		t.Fatalf("ParseCredential failed: %v", err)
	}

	if !ed25519.Verify(pub, cred.RawMsg, cred.Signature) {
		t.Error("signature verification failed on parsed credential")
	}

	age := time.Since(cred.Timestamp)
	if age > 5*time.Second {
		t.Errorf("credential timestamp too old: %v", age)
	}
}

// =============================================================================
// SECURITY TESTS - Username Swap Prevention
// =============================================================================

// TestUsernameSwapPrevented verifies the critical security fix:
// A credential created for one user cannot be used for another user.
func TestUsernameSwapPrevented(t *testing.T) {
	v, s, _, _ := setupTestVerifier(t)
	ctx := context.Background()

	// Create second user "bob"
	bobPub, bobPriv, _ := did.GenerateKeypair()
	bobDID := did.EncodeDIDKey(bobPub)
	err := s.AddUser(ctx, "bob", bobDID, bobPub, "basic")
	if err != nil {
		t.Fatalf("AddUser bob failed: %v", err)
	}

	// Create credential for bob
	bobToken, err := CreateCredential("bob", bobPriv)
	if err != nil {
		t.Fatalf("CreateCredential for bob failed: %v", err)
	}

	// Attack: Try to use bob's token with alice's username
	result, err := v.Verify(ctx, "alice", bobToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CRITICAL: This must be denied - credential was issued for bob, not alice
	if result.Allowed {
		t.Error("CRITICAL SECURITY VULNERABILITY: username swap attack succeeded - credential for bob accepted for alice")
	}

	// Verify the denial reason mentions username mismatch
	if !strings.Contains(result.Reason, "username_mismatch") {
		t.Errorf("expected username_mismatch denial, got: %s", result.Reason)
	}

	// Verify bob's token still works for bob
	result, err = v.Verify(ctx, "bob", bobToken)
	if err != nil {
		t.Fatalf("Verify for legitimate use failed: %v", err)
	}
	if !result.Allowed {
		t.Errorf("bob's credential should work for bob: %s", result.Reason)
	}
}

// TestUsernameSwapWithValidSignature tests edge case where both users exist.
func TestUsernameSwapWithValidSignature(t *testing.T) {
	v, s, _, alicePriv := setupTestVerifier(t)
	ctx := context.Background()

	// Create second user "bob"
	bobPub, _, _ := did.GenerateKeypair()
	bobDID := did.EncodeDIDKey(bobPub)
	err := s.AddUser(ctx, "bob", bobDID, bobPub, "basic")
	if err != nil {
		t.Fatalf("AddUser bob failed: %v", err)
	}

	// Alice creates credential (bound to "alice")
	aliceToken, _ := CreateCredential("alice", alicePriv)

	// Attack: Try to use alice's token with bob's username
	result, _ := v.Verify(ctx, "bob", aliceToken)
	if result.Allowed {
		t.Fatal("CRITICAL: alice's credential was accepted for bob")
	}
}

// TestCredentialBindingToUsername verifies username is cryptographically bound.
func TestCredentialBindingToUsername(t *testing.T) {
	_, priv, _ := did.GenerateKeypair()

	// Create credentials for different usernames with same key
	token1, _ := CreateCredential("alice", priv)
	token2, _ := CreateCredential("bob", priv)
	token3, _ := CreateCredential("alice", priv)

	// Tokens for different usernames must be different
	if token1 == token2 {
		t.Error("Credentials for alice and bob are identical (username not bound)")
	}

	// Tokens for same username at different times must be different (nonce)
	if token1 == token3 {
		t.Error("Credentials for alice are identical (no randomness)")
	}

	// Parse and verify username hashes are different
	cred1, _ := ParseCredential(token1)
	cred2, _ := ParseCredential(token2)

	if bytes.Equal(cred1.UsernameHash, cred2.UsernameHash) {
		t.Error("Username hashes are identical for alice and bob")
	}
}

// TestEmptyUsername verifies error handling for empty username.
func TestEmptyUsername(t *testing.T) {
	_, priv, _ := did.GenerateKeypair()

	_, err := CreateCredential("", priv)
	if err == nil {
		t.Error("CreateCredential should reject empty username")
	}
}

// =============================================================================
// SECURITY TESTS - Clock Skew Tolerance
// =============================================================================

// TestClockSkewToleranceFuture verifies credentials from "fast" clocks are accepted.
func TestClockSkewToleranceFuture(t *testing.T) {
	v, _, _, priv := setupTestVerifier(t)
	v.clockSkewTolerance = 5 * time.Minute // 5 min tolerance
	ctx := context.Background()

	// Simulate client clock 2 minutes fast (within tolerance)
	futureTime := time.Now().Add(2 * time.Minute)
	token, err := CreateCredentialAtTime("alice", priv, futureTime)
	if err != nil {
		t.Fatalf("CreateCredentialAtTime failed: %v", err)
	}

	result, err := v.Verify(ctx, "alice", token)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if !result.Allowed {
		t.Errorf("Should accept credential within clock skew tolerance (2min < 5min): %s",
			result.Reason)
	}
}

// TestClockSkewToleranceExcessive verifies far-future credentials are rejected.
func TestClockSkewToleranceExcessive(t *testing.T) {
	v, _, _, priv := setupTestVerifier(t)
	v.clockSkewTolerance = 5 * time.Minute
	ctx := context.Background()

	// Simulate client clock 10 minutes fast (exceeds tolerance)
	futureTime := time.Now().Add(10 * time.Minute)
	token, _ := CreateCredentialAtTime("alice", priv, futureTime)

	result, _ := v.Verify(ctx, "alice", token)
	if result.Allowed {
		t.Error("Should reject credential exceeding clock skew tolerance (10min > 5min)")
	}

	if !strings.Contains(result.Reason, "credential_future") {
		t.Errorf("Expected 'credential_future' error, got: %s", result.Reason)
	}
}

// TestClockSkewTolerancePast verifies recently expired credentials are accepted within tolerance.
func TestClockSkewTolerancePast(t *testing.T) {
	v, _, _, priv := setupTestVerifier(t)
	v.credentialTTL = 10 * time.Minute    // 10 min TTL
	v.clockSkewTolerance = 5 * time.Minute // 5 min tolerance
	ctx := context.Background()

	// Credential created 12 minutes ago (expired by 2 minutes, but within tolerance)
	pastTime := time.Now().Add(-12 * time.Minute)
	token, _ := CreateCredentialAtTime("alice", priv, pastTime)

	result, _ := v.Verify(ctx, "alice", token)

	// Effective TTL = 10min + 5min skew = 15min
	// Credential age = 12min < 15min → should succeed
	if !result.Allowed {
		t.Errorf("Should accept recently expired credential within skew tolerance: %s",
			result.Reason)
	}
}

// TestClockSkewBoundary tests exact boundary conditions.
func TestClockSkewBoundary(t *testing.T) {
	v, _, _, priv := setupTestVerifier(t)
	v.credentialTTL = 10 * time.Minute
	v.clockSkewTolerance = 5 * time.Minute
	ctx := context.Background()

	tests := []struct {
		name   string
		offset time.Duration
		wantOk bool
	}{
		{"Normal case (2min old)", -2 * time.Minute, true},
		{"At TTL boundary (10min old)", -10 * time.Minute, true},
		{"Within skew (14min old)", -14 * time.Minute, true},
		{"Just beyond TTL+skew", -16 * time.Minute, false},
		{"Future within tolerance (4min)", 4 * time.Minute, true},
		{"Future at tolerance (5min)", 5 * time.Minute, true},
		{"Future beyond tolerance (6min)", 6 * time.Minute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credTime := time.Now().Add(tt.offset)
			token, _ := CreateCredentialAtTime("alice", priv, credTime)
			result, _ := v.Verify(ctx, "alice", token)

			if result.Allowed != tt.wantOk {
				t.Errorf("Allowed=%v, want=%v. Offset=%v, Reason=%s",
					result.Allowed, tt.wantOk, tt.offset, result.Reason)
			}
		})
	}
}

// TestNewVerifierWithSkew verifies custom skew tolerance is applied.
func TestNewVerifierWithSkew(t *testing.T) {
	s, _ := store.NewMemoryStore()
	v := NewVerifierWithSkew(s, 1*time.Hour, 5*time.Minute, 10*time.Minute)

	if v.clockSkewTolerance != 10*time.Minute {
		t.Errorf("clockSkewTolerance = %v, want 10m", v.clockSkewTolerance)
	}
}
