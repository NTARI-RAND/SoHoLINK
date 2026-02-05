package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/accounting"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/did"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/policy"
	radiuspkg "github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/radius"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/verifier"
)

const sharedSecret = "testing123"

// testEnv holds all test infrastructure.
type testEnv struct {
	Store      *store.Store
	Verifier   *verifier.Verifier
	PolicyEng  *policy.Engine
	Accounting *accounting.Collector
	Server     *radiuspkg.Server
	AuthAddr   string
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	acctDir := filepath.Join(tmpDir, "accounting")
	policyDir := filepath.Join(tmpDir, "policies")

	os.MkdirAll(acctDir, 0750)
	os.MkdirAll(policyDir, 0750)

	// Write OPA v1 test policy
	policyContent := `
package soholink.authz

default allow = false

allow if {
    input.user != ""
    input.did != ""
    input.authenticated == true
}
`
	os.WriteFile(filepath.Join(policyDir, "test.rego"), []byte(policyContent), 0644)

	// Initialize store
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Initialize verifier
	v := verifier.NewVerifier(s, 1*time.Hour, 5*time.Minute)

	// Initialize policy engine
	pe, err := policy.NewEngine(policyDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Initialize accounting
	ac, err := accounting.NewCollector(acctDir)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	// Find a free port
	authAddr := findFreeUDPPort(t)

	// Create and start RADIUS server
	server := radiuspkg.NewServer(authAddr, findFreeUDPPort(t), sharedSecret, v, pe, ac)
	if err := server.Start(); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}

	// Give server time to bind
	time.Sleep(100 * time.Millisecond)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		ac.Close()
		s.Close()
	})

	return &testEnv{
		Store:      s,
		Verifier:   v,
		PolicyEng:  pe,
		Accounting: ac,
		Server:     server,
		AuthAddr:   authAddr,
	}
}

func findFreeUDPPort(t *testing.T) string {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve UDP addr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	conn.Close()
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func TestEndToEndAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupTestEnv(t)

	// Create a test user
	pub, priv, _ := did.GenerateKeypair()
	userDID := did.EncodeDIDKey(pub)
	err := env.Store.AddUser(context.Background(), "alice", userDID, pub, "basic")
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	// Create credential token (now includes username for security)
	token, err := verifier.CreateCredential("alice", priv)
	if err != nil {
		t.Fatalf("CreateCredential failed: %v", err)
	}

	// Send RADIUS Access-Request
	packet := radius.New(radius.CodeAccessRequest, []byte(sharedSecret))
	rfc2865.UserName_SetString(packet, "alice")
	rfc2865.UserPassword_SetString(packet, token)

	response, err := radius.Exchange(context.Background(), packet, env.AuthAddr)
	if err != nil {
		t.Fatalf("RADIUS exchange failed: %v", err)
	}

	if response.Code != radius.CodeAccessAccept {
		replyMsg := rfc2865.ReplyMessage_GetString(response)
		t.Errorf("expected Access-Accept, got %v: %s", response.Code, replyMsg)
	}
}

func TestAuthenticationInvalidUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupTestEnv(t)

	// Create credential for nonexistent user (username is bound to token)
	_, priv, _ := did.GenerateKeypair()
	token, _ := verifier.CreateCredential("nonexistent", priv)

	packet := radius.New(radius.CodeAccessRequest, []byte(sharedSecret))
	rfc2865.UserName_SetString(packet, "nonexistent")
	rfc2865.UserPassword_SetString(packet, token)

	response, err := radius.Exchange(context.Background(), packet, env.AuthAddr)
	if err != nil {
		t.Fatalf("RADIUS exchange failed: %v", err)
	}

	if response.Code != radius.CodeAccessReject {
		t.Errorf("expected Access-Reject, got %v", response.Code)
	}
}

func TestAuthenticationRevokedUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupTestEnv(t)

	// Create and revoke user
	pub, priv, _ := did.GenerateKeypair()
	userDID := did.EncodeDIDKey(pub)
	env.Store.AddUser(context.Background(), "bob", userDID, pub, "basic")
	env.Store.RevokeUser(context.Background(), "bob", "test")

	token, _ := verifier.CreateCredential("bob", priv)

	packet := radius.New(radius.CodeAccessRequest, []byte(sharedSecret))
	rfc2865.UserName_SetString(packet, "bob")
	rfc2865.UserPassword_SetString(packet, token)

	response, err := radius.Exchange(context.Background(), packet, env.AuthAddr)
	if err != nil {
		t.Fatalf("RADIUS exchange failed: %v", err)
	}

	if response.Code != radius.CodeAccessReject {
		t.Errorf("expected Access-Reject for revoked user, got %v", response.Code)
	}
}

func TestAuthenticationReplayProtection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupTestEnv(t)

	pub, priv, _ := did.GenerateKeypair()
	userDID := did.EncodeDIDKey(pub)
	env.Store.AddUser(context.Background(), "carol", userDID, pub, "basic")

	token, _ := verifier.CreateCredential("carol", priv)

	// First request should succeed
	packet1 := radius.New(radius.CodeAccessRequest, []byte(sharedSecret))
	rfc2865.UserName_SetString(packet1, "carol")
	rfc2865.UserPassword_SetString(packet1, token)

	resp1, _ := radius.Exchange(context.Background(), packet1, env.AuthAddr)
	if resp1.Code != radius.CodeAccessAccept {
		t.Fatalf("first auth should accept, got %v", resp1.Code)
	}

	// Replay should be rejected
	packet2 := radius.New(radius.CodeAccessRequest, []byte(sharedSecret))
	rfc2865.UserName_SetString(packet2, "carol")
	rfc2865.UserPassword_SetString(packet2, token)

	resp2, _ := radius.Exchange(context.Background(), packet2, env.AuthAddr)
	if resp2.Code != radius.CodeAccessReject {
		t.Errorf("replay should be rejected, got %v", resp2.Code)
	}
}

func TestAccountingEventLogged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := setupTestEnv(t)

	pub, priv, _ := did.GenerateKeypair()
	userDID := did.EncodeDIDKey(pub)
	env.Store.AddUser(context.Background(), "dave", userDID, pub, "basic")

	token, _ := verifier.CreateCredential("dave", priv)

	packet := radius.New(radius.CodeAccessRequest, []byte(sharedSecret))
	rfc2865.UserName_SetString(packet, "dave")
	rfc2865.UserPassword_SetString(packet, token)

	radius.Exchange(context.Background(), packet, env.AuthAddr)

	// Give time for event to be written
	time.Sleep(100 * time.Millisecond)

	// Verify events were logged
	if env.Accounting.EventCount() == 0 {
		t.Error("expected accounting events to be logged")
	}
}
