// Package auth provides zero-trust connection tokens and mTLS helpers for
// SoHoLINK inter-node and workload network authentication.
//
// Connection tokens authorise a specific workload to receive inbound traffic
// on a specific host port. They are Ed25519-signed by the node key and have a
// bounded lifetime (default 1 hour). The firewall layer requires a valid token
// before opening a port to a source IP — replacing the legacy RFC 1918
// implicit-trust model.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DefaultTokenTTL is the default lifetime issued for new connection tokens.
const DefaultTokenTTL = time.Hour

// ConnectionToken authorises a workload to receive inbound traffic on one
// specific host port. All fields are signed together; any tampering is
// detected by VerifyToken.
type ConnectionToken struct {
	WorkloadDID string `json:"workload_did"`
	TargetPort  int    `json:"target_port"`
	IssuedAt    int64  `json:"issued_at"`  // Unix seconds UTC
	ExpiresAt   int64  `json:"expires_at"` // Unix seconds UTC
	Nonce       string `json:"nonce"`      // base64url-encoded random bytes
}

// IssueToken creates and signs a ConnectionToken for workloadDID on targetPort.
// privKey is the issuing node's Ed25519 private key. ttl is the token lifetime
// (use DefaultTokenTTL when unsure).
//
// The returned token is a compact string: base64url(payload) + "." + base64url(sig).
func IssueToken(privKey ed25519.PrivateKey, workloadDID string, targetPort int, ttl time.Duration) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("auth: generate nonce: %w", err)
	}

	now := time.Now().UTC()
	tok := ConnectionToken{
		WorkloadDID: workloadDID,
		TargetPort:  targetPort,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(ttl).Unix(),
		Nonce:       base64.RawURLEncoding.EncodeToString(nonce),
	}

	payload, err := json.Marshal(tok)
	if err != nil {
		return "", fmt.Errorf("auth: marshal token: %w", err)
	}

	sig := ed25519.Sign(privKey, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifyToken parses and verifies a token produced by IssueToken.
// Returns the decoded ConnectionToken on success. Returns an error if the
// signature is invalid or the token has expired.
func VerifyToken(pubKey ed25519.PublicKey, tokenStr string) (*ConnectionToken, error) {
	parts := strings.SplitN(tokenStr, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("auth: malformed token: expected payload.sig")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("auth: decode payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("auth: decode signature: %w", err)
	}

	if !ed25519.Verify(pubKey, payload, sig) {
		return nil, fmt.Errorf("auth: invalid token signature")
	}

	var tok ConnectionToken
	if err := json.Unmarshal(payload, &tok); err != nil {
		return nil, fmt.Errorf("auth: unmarshal token: %w", err)
	}

	if time.Now().UTC().Unix() > tok.ExpiresAt {
		return nil, fmt.Errorf("auth: token expired at %s", time.Unix(tok.ExpiresAt, 0).UTC())
	}

	return &tok, nil
}
