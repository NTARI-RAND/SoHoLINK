package portal

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SessionClaims holds the authenticated user's identity.
type SessionClaims struct {
	UserID    string
	Email     string
	ExpiresAt int64 // Unix timestamp
}

type contextKey struct{}

// SessionManager creates and verifies Ed25519-signed session tokens.
type SessionManager struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

// NewSessionManager constructs a SessionManager with the given Ed25519 private key.
// The public key is derived from the private key.
func NewSessionManager(privateKey ed25519.PrivateKey) *SessionManager {
	return &SessionManager{
		privateKey: privateKey,
		publicKey:  privateKey.Public().(ed25519.PublicKey),
	}
}

// CreateToken builds a signed session token for claims. The token format is:
//
//	base64RawURL(userID|email|role|expiresAt) . base64RawURL(Ed25519Signature)
func (sm *SessionManager) CreateToken(claims SessionClaims) (string, error) {
	raw := claims.UserID + "|" + claims.Email + "|" +
		strconv.FormatInt(claims.ExpiresAt, 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	sig := ed25519.Sign(sm.privateKey, []byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifyToken parses and validates a session token, returning the embedded claims.
// Returns an error if the token is malformed, the signature is invalid, or the
// token has expired.
func (sm *SessionManager) VerifyToken(token string) (SessionClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return SessionClaims{}, fmt.Errorf("verify token: malformed")
	}
	encoded, sigB64 := parts[0], parts[1]

	actualSig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return SessionClaims{}, fmt.Errorf("verify token: decode signature: %w", err)
	}
	if !ed25519.Verify(sm.publicKey, []byte(encoded), actualSig) {
		return SessionClaims{}, fmt.Errorf("verify token: invalid signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return SessionClaims{}, fmt.Errorf("verify token: decode payload: %w", err)
	}
	fields := strings.SplitN(string(raw), "|", 3)
	if len(fields) != 3 {
		return SessionClaims{}, fmt.Errorf("verify token: malformed claims")
	}

	expiresAt, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return SessionClaims{}, fmt.Errorf("verify token: parse expiry: %w", err)
	}
	if time.Now().Unix() > expiresAt {
		return SessionClaims{}, fmt.Errorf("verify token: expired")
	}

	return SessionClaims{
		UserID:    fields[0],
		Email:     fields[1],
		ExpiresAt: expiresAt,
	}, nil
}

// RequireAuth reads the session_token cookie, verifies it, and stores the claims
// in the request context. Redirects to /login with 302 if the cookie is absent
// or the token is invalid.
func RequireAuth(sm *SessionManager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_token")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		claims, err := sm.VerifyToken(cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), contextKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClaimsFromContext retrieves the SessionClaims stored by RequireAuth.
func ClaimsFromContext(ctx context.Context) (SessionClaims, bool) {
	c, ok := ctx.Value(contextKey{}).(SessionClaims)
	return c, ok
}
