package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestEngine(t *testing.T, policyContent string) *Engine {
	t.Helper()

	dir := t.TempDir()
	policyFile := filepath.Join(dir, "test.rego")
	if err := os.WriteFile(policyFile, []byte(policyContent), 0644); err != nil {
		t.Fatalf("failed to write policy: %v", err)
	}

	engine, err := NewEngine(dir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	return engine
}

// OPA v1 requires `if` keyword before rule bodies and `contains` for partial set rules
const defaultPolicy = `
package soholink.authz

default allow = false

allow if {
    input.user != ""
    input.did != ""
    input.authenticated == true
}

deny_reasons contains reason if {
    input.user == ""
    reason := "no_username"
}

deny_reasons contains reason if {
    input.did == ""
    reason := "no_did"
}

deny_reasons contains reason if {
    input.authenticated != true
    reason := "not_authenticated"
}
`

func TestEvaluateAllowAuthenticated(t *testing.T) {
	engine := setupTestEngine(t, defaultPolicy)

	input := &AuthzInput{
		User:          "alice",
		DID:           "did:key:z6MkTest",
		Role:          "basic",
		Authenticated: true,
		Timestamp:     time.Now(),
	}

	result, err := engine.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if !result.Allow {
		t.Errorf("expected ALLOW for authenticated user, got DENY: %v", result.DenyReasons)
	}
}

func TestEvaluateDenyUnauthenticated(t *testing.T) {
	engine := setupTestEngine(t, defaultPolicy)

	input := &AuthzInput{
		User:          "alice",
		DID:           "did:key:z6MkTest",
		Authenticated: false,
		Timestamp:     time.Now(),
	}

	result, err := engine.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result.Allow {
		t.Error("expected DENY for unauthenticated user")
	}
}

func TestEvaluateDenyNoUsername(t *testing.T) {
	engine := setupTestEngine(t, defaultPolicy)

	input := &AuthzInput{
		User:          "",
		DID:           "did:key:z6MkTest",
		Authenticated: true,
		Timestamp:     time.Now(),
	}

	result, err := engine.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result.Allow {
		t.Error("expected DENY for missing username")
	}
}

const rolePremiumPolicy = `
package soholink.authz

default allow = false

allow if {
    input.authenticated == true
    input.role == "premium"
    input.resource == "gpu_compute"
}

allow if {
    input.authenticated == true
    input.resource == "network_access"
}

deny_reasons contains reason if {
    input.role != "premium"
    input.resource == "gpu_compute"
    reason := "gpu_requires_premium"
}
`

func TestEvaluateRoleBasedPolicy(t *testing.T) {
	engine := setupTestEngine(t, rolePremiumPolicy)

	// Premium user accessing GPU - should allow
	result, _ := engine.Evaluate(context.Background(), &AuthzInput{
		User:          "alice",
		DID:           "did:key:z6MkTest",
		Role:          "premium",
		Authenticated: true,
		Resource:      "gpu_compute",
		Timestamp:     time.Now(),
	})
	if !result.Allow {
		t.Error("premium user should access GPU")
	}

	// Basic user accessing GPU - should deny
	result, _ = engine.Evaluate(context.Background(), &AuthzInput{
		User:          "bob",
		DID:           "did:key:z6MkBob",
		Role:          "basic",
		Authenticated: true,
		Resource:      "gpu_compute",
		Timestamp:     time.Now(),
	})
	if result.Allow {
		t.Error("basic user should not access GPU")
	}

	// Basic user accessing network - should allow
	result, _ = engine.Evaluate(context.Background(), &AuthzInput{
		User:          "bob",
		DID:           "did:key:z6MkBob",
		Role:          "basic",
		Authenticated: true,
		Resource:      "network_access",
		Timestamp:     time.Now(),
	})
	if !result.Allow {
		t.Error("basic user should access network")
	}
}

func TestEngineReload(t *testing.T) {
	dir := t.TempDir()
	policyFile := filepath.Join(dir, "test.rego")

	// Initial policy: deny all
	os.WriteFile(policyFile, []byte(`
package soholink.authz
default allow = false
`), 0644)

	engine, err := NewEngine(dir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	input := &AuthzInput{
		User:          "alice",
		DID:           "did:key:z6MkTest",
		Authenticated: true,
		Timestamp:     time.Now(),
	}

	result, _ := engine.Evaluate(context.Background(), input)
	if result.Allow {
		t.Error("should deny with deny-all policy")
	}

	// Update policy: allow all authenticated
	os.WriteFile(policyFile, []byte(`
package soholink.authz
default allow = false
allow if { input.authenticated == true }
`), 0644)

	if err := engine.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	result, _ = engine.Evaluate(context.Background(), input)
	if !result.Allow {
		t.Error("should allow after policy reload")
	}
}

func TestEngineNoPolicies(t *testing.T) {
	dir := t.TempDir()
	_, err := NewEngine(dir)
	if err == nil {
		t.Error("should fail with no policy files")
	}
}

func TestPolicyFiles(t *testing.T) {
	engine := setupTestEngine(t, defaultPolicy)
	files, err := engine.PolicyFiles()
	if err != nil {
		t.Fatalf("PolicyFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 policy file, got %d", len(files))
	}
}
