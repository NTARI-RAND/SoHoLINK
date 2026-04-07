package orchestrator

import (
	"testing"
	"time"
)

func TestGenerateAndVerifyJobToken(t *testing.T) {
	secret := []byte("test-secret")
	jobID := "job-abc-123"
	nodeID := "node-xyz-456"

	token, err := GenerateJobToken(jobID, nodeID, time.Hour, secret)
	if err != nil {
		t.Fatalf("GenerateJobToken: unexpected error: %v", err)
	}

	gotJobID, gotNodeID, err := VerifyJobToken(token, secret)
	if err != nil {
		t.Fatalf("VerifyJobToken: unexpected error: %v", err)
	}
	if gotJobID != jobID {
		t.Errorf("jobID: got %q, want %q", gotJobID, jobID)
	}
	if gotNodeID != nodeID {
		t.Errorf("nodeID: got %q, want %q", gotNodeID, nodeID)
	}
}

func TestVerifyJobToken_TamperedSignature(t *testing.T) {
	secret := []byte("test-secret")
	token, err := GenerateJobToken("job-abc-123", "node-xyz-456", time.Hour, secret)
	if err != nil {
		t.Fatalf("GenerateJobToken: unexpected error: %v", err)
	}

	// Flip the last byte of the signature; base64url uses [A-Za-z0-9_-] so
	// replacing with a character outside that set guarantees a different value.
	tampered := token[:len(token)-1] + "!"

	_, _, err = VerifyJobToken(tampered, secret)
	if err == nil {
		t.Error("VerifyJobToken: expected error for tampered signature, got nil")
	}
}

func TestVerifyJobToken_Expired(t *testing.T) {
	secret := []byte("test-secret")

	// Negative TTL produces a token whose expiry is in the past.
	token, err := GenerateJobToken("job-abc-123", "node-xyz-456", -time.Second, secret)
	if err != nil {
		t.Fatalf("GenerateJobToken: unexpected error: %v", err)
	}

	_, _, err = VerifyJobToken(token, secret)
	if err == nil {
		t.Error("VerifyJobToken: expected error for expired token, got nil")
	}
}

func TestVerifyJobToken_InvalidFormat(t *testing.T) {
	secret := []byte("test-secret")

	_, _, err := VerifyJobToken("nodotsinhere", secret)
	if err == nil {
		t.Error("VerifyJobToken: expected error for token missing '.' separator, got nil")
	}
}
