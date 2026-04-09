package portal

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestSM(t *testing.T) *SessionManager {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return NewSessionManager(priv)
}

func TestCreateVerifyToken_RoundTrip(t *testing.T) {
	sm := newTestSM(t)
	claims := SessionClaims{
		UserID:    "user-123",
		Email:     "test@example.com",
		Role:      "provider",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	token, err := sm.CreateToken(claims)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	got, err := sm.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if got.UserID != claims.UserID {
		t.Errorf("UserID: got %q, want %q", got.UserID, claims.UserID)
	}
	if got.Email != claims.Email {
		t.Errorf("Email: got %q, want %q", got.Email, claims.Email)
	}
	if got.Role != claims.Role {
		t.Errorf("Role: got %q, want %q", got.Role, claims.Role)
	}
	if got.ExpiresAt != claims.ExpiresAt {
		t.Errorf("ExpiresAt: got %d, want %d", got.ExpiresAt, claims.ExpiresAt)
	}
}

func TestVerifyToken_TamperedSignature(t *testing.T) {
	sm := newTestSM(t)
	claims := SessionClaims{
		UserID:    "user-123",
		Email:     "test@example.com",
		Role:      "provider",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	token, err := sm.CreateToken(claims)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	// Flip the last character of the signature to corrupt it.
	tampered := token[:len(token)-1] + "X"

	_, err = sm.VerifyToken(tampered)
	if err == nil {
		t.Error("expected error for tampered signature, got nil")
	}
}

func TestVerifyToken_ExpiredToken(t *testing.T) {
	sm := newTestSM(t)
	claims := SessionClaims{
		UserID:    "user-123",
		Email:     "test@example.com",
		Role:      "consumer",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	}

	token, err := sm.CreateToken(claims)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	_, err = sm.VerifyToken(token)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestVerifyToken_MalformedToken(t *testing.T) {
	sm := newTestSM(t)

	_, err := sm.VerifyToken("nodotinthisstring")
	if err == nil {
		t.Error("expected error for malformed token (no dot separator), got nil")
	}
}

func TestRequireAuth_RedirectsWhenNoCookie(t *testing.T) {
	sm := newTestSM(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(sm, next)
	req := httptest.NewRequest(http.MethodGet, "/provider/dashboard", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location: got %q, want /login", loc)
	}
}

func TestRequireRole_ForbiddenWhenRoleMismatch(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireRole("consumer", next)

	req := httptest.NewRequest(http.MethodGet, "/consumer/marketplace", nil)
	// Inject provider claims directly via contextKey — same package, so the
	// unexported key is accessible here.
	ctx := context.WithValue(req.Context(), contextKey{}, SessionClaims{
		UserID: "user-456",
		Email:  "provider@example.com",
		Role:   "provider",
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusForbidden)
	}
}
