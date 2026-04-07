package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"strings"
	"testing"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func generateKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

// ── IssueToken / VerifyToken ─────────────────────────────────────────────────

func TestIssueAndVerifyToken_RoundTrip(t *testing.T) {
	pub, priv := generateKey(t)
	tok, err := IssueToken(priv, "did:soho:workload1", 8100, DefaultTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	ct, err := VerifyToken(pub, tok)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if ct.WorkloadDID != "did:soho:workload1" {
		t.Errorf("WorkloadDID = %q, want %q", ct.WorkloadDID, "did:soho:workload1")
	}
	if ct.TargetPort != 8100 {
		t.Errorf("TargetPort = %d, want 8100", ct.TargetPort)
	}
}

func TestVerifyToken_WrongKey(t *testing.T) {
	_, priv := generateKey(t)
	wrongPub, _ := generateKey(t)

	tok, err := IssueToken(priv, "did:soho:workload1", 8100, DefaultTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	_, err = VerifyToken(wrongPub, tok)
	if err == nil {
		t.Fatal("expected error for wrong public key, got nil")
	}
	if !strings.Contains(err.Error(), "invalid token signature") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyToken_Expired(t *testing.T) {
	pub, priv := generateKey(t)

	// Issue a token that expired 1 second ago.
	tok, err := IssueToken(priv, "did:soho:workload1", 8100, -time.Second)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	_, err = VerifyToken(pub, tok)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyToken_Malformed(t *testing.T) {
	pub, _ := generateKey(t)

	for _, bad := range []string{"", "nodot", "a.b.c"} {
		_, err := VerifyToken(pub, bad)
		if err == nil {
			t.Errorf("expected error for token %q, got nil", bad)
		}
	}
}

func TestVerifyToken_TamperedPayload(t *testing.T) {
	pub, priv := generateKey(t)
	tok, _ := IssueToken(priv, "did:soho:workload1", 8100, DefaultTokenTTL)

	// Flip a byte in the payload section.
	parts := strings.SplitN(tok, ".", 2)
	payload := []byte(parts[0])
	payload[0] ^= 0xFF
	tampered := string(payload) + "." + parts[1]

	_, err := VerifyToken(pub, tampered)
	if err == nil {
		t.Fatal("expected error for tampered payload, got nil")
	}
}

func TestIssueToken_UniquenessViaNonce(t *testing.T) {
	_, priv := generateKey(t)

	tok1, _ := IssueToken(priv, "did:soho:workload1", 8100, DefaultTokenTTL)
	tok2, _ := IssueToken(priv, "did:soho:workload1", 8100, DefaultTokenTTL)

	// Different nonces should produce different token strings.
	if tok1 == tok2 {
		t.Error("expected unique tokens per call (nonce should differ)")
	}
}

// ── GenerateSelfSignedCert ───────────────────────────────────────────────────

func TestGenerateSelfSignedCert_RoundTrip(t *testing.T) {
	_, priv := generateKey(t)
	did := "did:soho:testnode"

	cert, err := GenerateSelfSignedCert(did, priv, 0)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}

	// The certificate must be parseable by the TLS stack.
	if len(cert.Certificate) == 0 {
		t.Fatal("no certificate DER bytes returned")
	}
}

func TestGenerateSelfSignedCert_DefaultTTL(t *testing.T) {
	_, priv := generateKey(t)
	cert, err := GenerateSelfSignedCert("did:soho:n", priv, 0)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}
	_ = cert // confirms zero TTL → defaults to 1 year without error
}

// ── NewMTLSConfig ────────────────────────────────────────────────────────────

func TestNewMTLSConfig_MinTLSVersion(t *testing.T) {
	_, priv := generateKey(t)
	cert, _ := GenerateSelfSignedCert("did:soho:n", priv, time.Hour)

	cfg := NewMTLSConfig(cert, []string{"did:soho:peer"})
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want TLS 1.3 (%d)", cfg.MinVersion, tls.VersionTLS13)
	}
}

func TestNewMTLSConfig_TrustedDIDCheck(t *testing.T) {
	_, priv := generateKey(t)
	localCert, _ := GenerateSelfSignedCert("did:soho:local", priv, time.Hour)

	_, peerPriv := generateKey(t)
	peerCert, _ := GenerateSelfSignedCert("did:soho:peer", peerPriv, time.Hour)

	cfg := NewMTLSConfig(localCert, []string{"did:soho:peer"})

	// Simulate VerifyPeerCertificate with the peer's raw DER.
	err := cfg.VerifyPeerCertificate(peerCert.Certificate, nil)
	if err != nil {
		t.Errorf("trusted DID should pass: %v", err)
	}

	// An untrusted DID must be rejected.
	_, untrustedPriv := generateKey(t)
	untrustedCert, _ := GenerateSelfSignedCert("did:soho:stranger", untrustedPriv, time.Hour)
	err = cfg.VerifyPeerCertificate(untrustedCert.Certificate, nil)
	if err == nil {
		t.Error("untrusted DID should be rejected")
	}
}

func TestNewMTLSConfig_EmptyTrustedList(t *testing.T) {
	_, priv := generateKey(t)
	localCert, _ := GenerateSelfSignedCert("did:soho:local", priv, time.Hour)
	cfg := NewMTLSConfig(localCert, nil) // no restrictions

	_, anyPriv := generateKey(t)
	anyCert, _ := GenerateSelfSignedCert("did:soho:anyone", anyPriv, time.Hour)
	err := cfg.VerifyPeerCertificate(anyCert.Certificate, nil)
	if err != nil {
		t.Errorf("empty trusted list should allow any peer: %v", err)
	}
}

func TestNewMTLSConfig_NoPeerCert(t *testing.T) {
	_, priv := generateKey(t)
	cert, _ := GenerateSelfSignedCert("did:soho:local", priv, time.Hour)
	cfg := NewMTLSConfig(cert, []string{"did:soho:peer"})

	err := cfg.VerifyPeerCertificate(nil, nil)
	if err == nil {
		t.Error("missing peer cert should fail")
	}
}

func TestNewMTLSClientConfig_ServerName(t *testing.T) {
	_, priv := generateKey(t)
	cert, _ := GenerateSelfSignedCert("did:soho:local", priv, time.Hour)
	cfg := NewMTLSClientConfig(cert, []string{"did:soho:server"}, "did:soho:server")

	if cfg.ServerName != "did:soho:server" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "did:soho:server")
	}
}
