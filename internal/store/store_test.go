package store

import (
	"context"
	"testing"
	"time"
)

func TestNewMemoryStore(t *testing.T) {
	s, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore failed: %v", err)
	}
	defer s.Close()
}

func TestAddAndGetUser(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	pubKey := []byte("test-public-key-32-bytes-padding!")
	err := s.AddUser(ctx, "alice", "did:key:z6MkTestAlice", pubKey, "basic")
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Get by username
	user, err := s.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}
	if user == nil {
		t.Fatal("user should not be nil")
	}
	if user.Username != "alice" {
		t.Errorf("expected username 'alice', got '%s'", user.Username)
	}
	if user.DID != "did:key:z6MkTestAlice" {
		t.Errorf("expected DID 'did:key:z6MkTestAlice', got '%s'", user.DID)
	}
	if user.Role != "basic" {
		t.Errorf("expected role 'basic', got '%s'", user.Role)
	}

	// Get by DID
	user2, err := s.GetUserByDID(ctx, "did:key:z6MkTestAlice")
	if err != nil {
		t.Fatalf("GetUserByDID failed: %v", err)
	}
	if user2 == nil {
		t.Fatal("user should not be nil")
	}
	if user2.Username != "alice" {
		t.Errorf("expected username 'alice', got '%s'", user2.Username)
	}
}

func TestGetUserNotFound(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	user, err := s.GetUserByUsername(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != nil {
		t.Error("user should be nil for nonexistent user")
	}
}

func TestDuplicateUser(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	pubKey := []byte("test-public-key-32-bytes-padding!")
	_ = s.AddUser(ctx, "alice", "did:key:z6MkTestAlice", pubKey, "basic")

	err := s.AddUser(ctx, "alice", "did:key:z6MkTestAlice2", pubKey, "basic")
	if err == nil {
		t.Error("should fail on duplicate username")
	}
}

func TestListUsers(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	_ = s.AddUser(ctx, "alice", "did:key:z6MkAlice", []byte("key1"), "basic")
	_ = s.AddUser(ctx, "bob", "did:key:z6MkBob", []byte("key2"), "premium")

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestRevokeUser(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	_ = s.AddUser(ctx, "alice", "did:key:z6MkAlice", []byte("key1"), "basic")

	// Revoke
	err := s.RevokeUser(ctx, "alice", "test revocation")
	if err != nil {
		t.Fatalf("RevokeUser failed: %v", err)
	}

	// Check revocation
	revoked, err := s.IsRevoked(ctx, "did:key:z6MkAlice")
	if err != nil {
		t.Fatalf("IsRevoked failed: %v", err)
	}
	if !revoked {
		t.Error("user should be revoked")
	}

	// Check user record
	user, _ := s.GetUserByUsername(ctx, "alice")
	if !user.RevokedAt.Valid {
		t.Error("user revoked_at should be set")
	}
}

func TestRevokeNonexistentUser(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	err := s.RevokeUser(ctx, "nonexistent", "test")
	if err == nil {
		t.Error("should fail for nonexistent user")
	}
}

func TestNonceReplayProtection(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	// First check - nonce not seen
	seen, err := s.CheckNonce(ctx, "nonce123")
	if err != nil {
		t.Fatalf("CheckNonce failed: %v", err)
	}
	if seen {
		t.Error("nonce should not be seen yet")
	}

	// Record nonce
	err = s.RecordNonce(ctx, "nonce123")
	if err != nil {
		t.Fatalf("RecordNonce failed: %v", err)
	}

	// Second check - nonce should be seen
	seen, err = s.CheckNonce(ctx, "nonce123")
	if err != nil {
		t.Fatalf("CheckNonce failed: %v", err)
	}
	if !seen {
		t.Error("nonce should be seen after recording")
	}
}

func TestPruneExpiredNonces(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	// Record a nonce
	_ = s.RecordNonce(ctx, "old-nonce")

	// Prune with very short age (should delete)
	pruned, err := s.PruneExpiredNonces(ctx, 0)
	if err != nil {
		t.Fatalf("PruneExpiredNonces failed: %v", err)
	}
	// Note: depending on timing, might be 0 or 1
	_ = pruned

	// Prune with long age (should not delete new nonces)
	_ = s.RecordNonce(ctx, "new-nonce")
	pruned, err = s.PruneExpiredNonces(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("PruneExpiredNonces failed: %v", err)
	}
	if pruned != 0 {
		t.Errorf("should not prune recent nonces, pruned: %d", pruned)
	}
}

func TestUserCount(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	count, _ := s.UserCount(ctx)
	if count != 0 {
		t.Errorf("expected 0 users, got %d", count)
	}

	_ = s.AddUser(ctx, "alice", "did:key:z6MkAlice", []byte("key1"), "basic")
	_ = s.AddUser(ctx, "bob", "did:key:z6MkBob", []byte("key2"), "basic")

	count, _ = s.UserCount(ctx)
	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}

	activeCount, _ := s.ActiveUserCount(ctx)
	if activeCount != 2 {
		t.Errorf("expected 2 active users, got %d", activeCount)
	}

	_ = s.RevokeUser(ctx, "alice", "test")
	activeCount, _ = s.ActiveUserCount(ctx)
	if activeCount != 1 {
		t.Errorf("expected 1 active user after revocation, got %d", activeCount)
	}
}

func TestNodeInfo(t *testing.T) {
	s, _ := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()

	// Set and get
	err := s.SetNodeInfo(ctx, "node_did", "did:key:z6MkTest")
	if err != nil {
		t.Fatalf("SetNodeInfo failed: %v", err)
	}

	val, err := s.GetNodeInfo(ctx, "node_did")
	if err != nil {
		t.Fatalf("GetNodeInfo failed: %v", err)
	}
	if val != "did:key:z6MkTest" {
		t.Errorf("expected 'did:key:z6MkTest', got '%s'", val)
	}

	// Update existing
	err = s.SetNodeInfo(ctx, "node_did", "did:key:z6MkUpdated")
	if err != nil {
		t.Fatalf("SetNodeInfo update failed: %v", err)
	}

	val, _ = s.GetNodeInfo(ctx, "node_did")
	if val != "did:key:z6MkUpdated" {
		t.Errorf("expected updated value, got '%s'", val)
	}

	// Get nonexistent
	val, err = s.GetNodeInfo(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetNodeInfo for missing key failed: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for missing key, got '%s'", val)
	}
}
