package portal

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SessionClaims holds the authenticated user's identity and role.
type SessionClaims struct {
	UserID    string
	Email     string
	Role      string // "provider" | "consumer" | "ntari_staff"
	ExpiresAt int64  // Unix timestamp
}

type contextKey struct{}

// SessionManager creates and verifies HMAC-signed session tokens.
type SessionManager struct {
	secret []byte
}

// NewSessionManager constructs a SessionManager with the given signing secret.
func NewSessionManager(secret []byte) *SessionManager {
	return &SessionManager{secret: secret}
}

// CreateToken builds a signed session token for claims. The token format is:
//
//	base64RawURL(userID|email|role|expiresAt) . base64RawURL(HMAC-SHA256(payload, secret))
func (sm *SessionManager) CreateToken(claims SessionClaims) (string, error) {
	raw := claims.UserID + "|" + claims.Email + "|" + claims.Role + "|" +
		strconv.FormatInt(claims.ExpiresAt, 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
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
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(encoded))
	if !hmac.Equal(actualSig, mac.Sum(nil)) {
		return SessionClaims{}, fmt.Errorf("verify token: invalid signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return SessionClaims{}, fmt.Errorf("verify token: decode payload: %w", err)
	}
	fields := strings.SplitN(string(raw), "|", 4)
	if len(fields) != 4 {
		return SessionClaims{}, fmt.Errorf("verify token: malformed claims")
	}

	expiresAt, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return SessionClaims{}, fmt.Errorf("verify token: parse expiry: %w", err)
	}
	if time.Now().Unix() > expiresAt {
		return SessionClaims{}, fmt.Errorf("verify token: expired")
	}

	return SessionClaims{
		UserID:    fields[0],
		Email:     fields[1],
		Role:      fields[2],
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

// RequireRole checks that the authenticated user's role matches the required
// role. Must be used inside RequireAuth. Returns 403 if the role does not match.
func RequireRole(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || claims.Role != role {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClaimsFromContext retrieves the SessionClaims stored by RequireAuth.
func ClaimsFromContext(ctx context.Context) (SessionClaims, bool) {
	c, ok := ctx.Value(contextKey{}).(SessionClaims)
	return c, ok
}
