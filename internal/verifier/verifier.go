package verifier

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/sha3"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/did"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// Verifier handles offline credential verification using Ed25519 signatures.
type Verifier struct {
	store              *store.Store
	credentialTTL      time.Duration
	maxNonceAge        time.Duration
	clockSkewTolerance time.Duration // NEW: allows for clock drift between client and server
}

// Credential represents a parsed authentication credential token.
// Binary format: 4-byte timestamp (big-endian uint32) + 8-byte nonce + 8-byte username hash + 64-byte signature
// Wire format: base64url(84 bytes) = ~113 characters (fits RADIUS PAP 128-byte limit)
type Credential struct {
	Timestamp    time.Time
	Nonce        []byte // 8 bytes
	UsernameHash []byte // 8 bytes - SECURITY FIX: binds credential to specific username
	Signature    []byte // 64 bytes
	NonceHex     string // hex string of nonce for DB storage
	RawMsg       []byte // raw 20-byte message (timestamp + nonce + username-hash) for signature verification
}

// VerifyResult contains the outcome of credential verification.
type VerifyResult struct {
	Allowed  bool
	Reason   string
	Username string
	DID      string
	Role     string
}

const (
	timestampSize    = 4  // uint32 big-endian
	nonceSize        = 8  // 8 bytes of randomness
	usernameHashSize = 8  // SECURITY FIX: first 8 bytes of SHA3-256(username)
	signatureSize    = 64 // Ed25519 signature
	credentialSize   = timestampSize + nonceSize + usernameHashSize + signatureSize // 84 bytes
)

// NewVerifier creates a new Verifier with the given store and configuration.
func NewVerifier(s *store.Store, credentialTTL, maxNonceAge time.Duration) *Verifier {
	return &Verifier{
		store:              s,
		credentialTTL:      credentialTTL,
		maxNonceAge:        maxNonceAge,
		clockSkewTolerance: 5 * time.Minute, // Default: 5 minutes
	}
}

// NewVerifierWithSkew creates a new Verifier with explicit clock skew tolerance.
func NewVerifierWithSkew(s *store.Store, credentialTTL, maxNonceAge, clockSkewTolerance time.Duration) *Verifier {
	return &Verifier{
		store:              s,
		credentialTTL:      credentialTTL,
		maxNonceAge:        maxNonceAge,
		clockSkewTolerance: clockSkewTolerance,
	}
}

// ParseCredential parses a base64url-encoded credential token.
// Format: base64url(4-byte-timestamp + 8-byte-nonce + 8-byte-username-hash + 64-byte-ed25519-signature)
func ParseCredential(token string) (*Credential, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("invalid credential encoding: %w", err)
	}

	if len(raw) != credentialSize {
		return nil, fmt.Errorf("invalid credential length: expected %d bytes, got %d", credentialSize, len(raw))
	}

	// Extract fields
	tsUint32 := binary.BigEndian.Uint32(raw[0:timestampSize])
	ts := time.Unix(int64(tsUint32), 0)
	nonce := raw[timestampSize : timestampSize+nonceSize]
	usernameHash := raw[timestampSize+nonceSize : timestampSize+nonceSize+usernameHashSize]
	sig := raw[timestampSize+nonceSize+usernameHashSize:]

	return &Credential{
		Timestamp:    ts,
		Nonce:        nonce,
		UsernameHash: usernameHash,
		Signature:    sig,
		NonceHex:     hex.EncodeToString(nonce),
		RawMsg:       raw[0 : timestampSize+nonceSize+usernameHashSize], // first 20 bytes are the signed message
	}, nil
}

// CreateCredential creates a signed credential token for a user.
// SECURITY FIX: Now includes username hash to bind credential to specific username.
// Returns a base64url-encoded string that fits within RADIUS PAP 128-byte limit.
func CreateCredential(username string, privateKey ed25519.PrivateKey) (string, error) {
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid private key size")
	}

	raw := make([]byte, credentialSize)

	// 4-byte timestamp (big-endian uint32)
	ts := uint32(time.Now().Unix())
	binary.BigEndian.PutUint32(raw[0:timestampSize], ts)

	// 8-byte random nonce
	if _, err := rand.Read(raw[timestampSize : timestampSize+nonceSize]); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// 8-byte username hash (first 8 bytes of SHA3-256)
	// SECURITY FIX: This binds the credential to the specific username
	usernameHash := sha3.Sum256([]byte(username))
	copy(raw[timestampSize+nonceSize:timestampSize+nonceSize+usernameHashSize], usernameHash[:usernameHashSize])

	// Sign the message (timestamp + nonce + username-hash = first 20 bytes)
	message := raw[0 : timestampSize+nonceSize+usernameHashSize]
	signature := ed25519.Sign(privateKey, message)
	copy(raw[timestampSize+nonceSize+usernameHashSize:], signature)

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// CreateCredentialAtTime creates a credential with a specific timestamp (for testing).
func CreateCredentialAtTime(username string, privateKey ed25519.PrivateKey, timestamp time.Time) (string, error) {
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid private key size")
	}

	raw := make([]byte, credentialSize)

	// 4-byte timestamp (big-endian uint32) - use provided timestamp
	ts := uint32(timestamp.Unix())
	binary.BigEndian.PutUint32(raw[0:timestampSize], ts)

	// 8-byte random nonce
	if _, err := rand.Read(raw[timestampSize : timestampSize+nonceSize]); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// 8-byte username hash
	usernameHash := sha3.Sum256([]byte(username))
	copy(raw[timestampSize+nonceSize:timestampSize+nonceSize+usernameHashSize], usernameHash[:usernameHashSize])

	// Sign the message
	message := raw[0 : timestampSize+nonceSize+usernameHashSize]
	signature := ed25519.Sign(privateKey, message)
	copy(raw[timestampSize+nonceSize+usernameHashSize:], signature)

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Verify performs the complete offline credential verification pipeline:
// 1. Parse credential token
// 2. Look up user by username in SQLite
// 3. SECURITY FIX: Verify username hash matches (constant-time comparison)
// 4. Verify Ed25519 signature
// 5. Check credential expiration (with clock skew tolerance)
// 6. Check nonce replay
// 7. Check revocation
// 8. Record nonce
//
// This function makes ZERO network calls. All operations are local.
func (v *Verifier) Verify(ctx context.Context, username, credentialToken string) (*VerifyResult, error) {
	// Step 1: Parse credential token
	cred, err := ParseCredential(credentialToken)
	if err != nil {
		return deny("invalid_credential", err.Error()), nil
	}

	// Step 2: Look up user
	user, err := v.store.GetUserByUsername(ctx, username)
	if err != nil {
		return deny("internal_error", "database lookup failed"), err
	}
	if user == nil {
		return deny("user_not_found", fmt.Sprintf("user '%s' not found", username)), nil
	}

	// Step 3: SECURITY FIX - Verify username hash matches using constant-time comparison
	// This prevents an attacker from using alice's token as bob
	expectedHash := sha3.Sum256([]byte(username))
	if subtle.ConstantTimeCompare(cred.UsernameHash, expectedHash[:usernameHashSize]) != 1 {
		return deny("username_mismatch", "credential was not issued for this username"), nil
	}

	// Step 4: Resolve public key from DID or stored key
	var pubKey ed25519.PublicKey
	if strings.HasPrefix(user.DID, "did:key:") {
		pubKey, err = did.DecodeDIDKey(user.DID)
		if err != nil {
			// Fall back to stored public key
			pubKey = ed25519.PublicKey(user.PublicKey)
		}
	} else {
		pubKey = ed25519.PublicKey(user.PublicKey)
	}

	// Step 5: Verify Ed25519 signature over the message (timestamp + nonce + username-hash)
	if !ed25519.Verify(pubKey, cred.RawMsg, cred.Signature) {
		return deny("invalid_signature", "Ed25519 signature verification failed"), nil
	}

	// Step 6: Check credential expiration with clock skew tolerance
	now := time.Now()
	age := now.Sub(cred.Timestamp)

	// Allow clock skew: credential can be slightly in the future (client clock fast)
	if age < -v.clockSkewTolerance {
		return deny("credential_future",
			fmt.Sprintf("credential timestamp is %.0f seconds in the future (max allowed: %.0f)",
				-age.Seconds(), v.clockSkewTolerance.Seconds())), nil
	}

	// Check expiration with skew tolerance (client clock slow)
	effectiveTTL := v.credentialTTL + v.clockSkewTolerance
	if age > effectiveTTL {
		return deny("credential_expired",
			fmt.Sprintf("credential expired %.0f seconds ago (TTL: %.0f seconds, skew tolerance: %.0f seconds)",
				age.Seconds()-v.credentialTTL.Seconds(), v.credentialTTL.Seconds(), v.clockSkewTolerance.Seconds())), nil
	}

	// Step 7: Check nonce replay
	seen, err := v.store.CheckNonce(ctx, cred.NonceHex)
	if err != nil {
		return deny("internal_error", "nonce check failed"), err
	}
	if seen {
		return deny("nonce_replay", "credential token has already been used"), nil
	}

	// Step 8: Check revocation
	revoked, err := v.store.IsRevoked(ctx, user.DID)
	if err != nil {
		return deny("internal_error", "revocation check failed"), err
	}
	if revoked {
		return deny("user_revoked", fmt.Sprintf("user '%s' has been revoked", username)), nil
	}

	// Step 9: Record nonce (prevent replay)
	if err := v.store.RecordNonce(ctx, cred.NonceHex); err != nil {
		// Log but don't fail auth
		_ = err
	}

	// All checks passed
	return &VerifyResult{
		Allowed:  true,
		Reason:   "authenticated",
		Username: user.Username,
		DID:      user.DID,
		Role:     user.Role,
	}, nil
}

func deny(reason, detail string) *VerifyResult {
	return &VerifyResult{
		Allowed: false,
		Reason:  reason + ": " + detail,
	}
}
