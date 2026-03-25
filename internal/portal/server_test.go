package portal

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSessionStruct(t *testing.T) {
	now := time.Now()
	s := Session{
		SessionID:    "sess_123",
		UserDID:      "did:key:z123",
		Username:     "alice",
		StartTime:    now,
		ExpiresAt:    now.Add(24 * time.Hour),
		BytesIn:      1024,
		BytesOut:     2048,
		MaxBandwidth: 100,
	}

	if s.SessionID != "sess_123" {
		t.Errorf("SessionID = %q, want %q", s.SessionID, "sess_123")
	}
	if s.UserDID != "did:key:z123" {
		t.Errorf("UserDID = %q, want %q", s.UserDID, "did:key:z123")
	}
	if s.Username != "alice" {
		t.Errorf("Username = %q, want %q", s.Username, "alice")
	}
	if s.BytesIn != 1024 {
		t.Errorf("BytesIn = %d, want 1024", s.BytesIn)
	}
	if s.BytesOut != 2048 {
		t.Errorf("BytesOut = %d, want 2048", s.BytesOut)
	}
	if s.MaxBandwidth != 100 {
		t.Errorf("MaxBandwidth = %d, want 100", s.MaxBandwidth)
	}
}

func TestSessionExpiry(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		expired   bool
	}{
		{
			name:      "future expiry is not expired",
			expiresAt: time.Now().Add(1 * time.Hour),
			expired:   false,
		},
		{
			name:      "past expiry is expired",
			expiresAt: time.Now().Add(-1 * time.Hour),
			expired:   true,
		},
		{
			name:      "far future expiry is not expired",
			expiresAt: time.Now().Add(24 * time.Hour),
			expired:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := Session{ExpiresAt: tc.expiresAt}
			isExpired := time.Now().After(s.ExpiresAt)
			if isExpired != tc.expired {
				t.Errorf("expired = %v, want %v", isExpired, tc.expired)
			}
		})
	}
}

func newTestServer() *Server {
	return &Server{
		listenAddr: ":0",
		sessions:   make(map[string]*Session),
	}
}

func TestNewServerFieldsInitialized(t *testing.T) {
	srv := newTestServer()
	if srv.sessions == nil {
		t.Fatal("sessions map should be initialized")
	}
	if len(srv.sessions) != 0 {
		t.Errorf("sessions should be empty, got %d", len(srv.sessions))
	}
}

func TestActiveSessions_Empty(t *testing.T) {
	srv := newTestServer()
	if got := srv.ActiveSessions(); got != 0 {
		t.Errorf("ActiveSessions() = %d, want 0", got)
	}
}

func TestActiveSessions_WithActiveSessions(t *testing.T) {
	srv := newTestServer()
	srv.sessions["s1"] = &Session{ExpiresAt: time.Now().Add(1 * time.Hour)}
	srv.sessions["s2"] = &Session{ExpiresAt: time.Now().Add(2 * time.Hour)}

	if got := srv.ActiveSessions(); got != 2 {
		t.Errorf("ActiveSessions() = %d, want 2", got)
	}
}

func TestActiveSessions_ExcludesExpired(t *testing.T) {
	srv := newTestServer()
	srv.sessions["active"] = &Session{ExpiresAt: time.Now().Add(1 * time.Hour)}
	srv.sessions["expired"] = &Session{ExpiresAt: time.Now().Add(-1 * time.Hour)}

	if got := srv.ActiveSessions(); got != 1 {
		t.Errorf("ActiveSessions() = %d, want 1 (should exclude expired)", got)
	}
}

func TestActiveSessions_AllExpired(t *testing.T) {
	srv := newTestServer()
	srv.sessions["e1"] = &Session{ExpiresAt: time.Now().Add(-1 * time.Hour)}
	srv.sessions["e2"] = &Session{ExpiresAt: time.Now().Add(-2 * time.Hour)}

	if got := srv.ActiveSessions(); got != 0 {
		t.Errorf("ActiveSessions() = %d, want 0 (all expired)", got)
	}
}

func TestActiveSessions_ConcurrentAccess(t *testing.T) {
	srv := newTestServer()
	srv.sessions["s1"] = &Session{ExpiresAt: time.Now().Add(1 * time.Hour)}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = srv.ActiveSessions()
		}()
	}
	wg.Wait()
}

func TestHandleLanding_ReturnsHTML(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	srv.handleLanding(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/html" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html")
	}
	body := w.Body.String()
	if body == "" {
		t.Error("body should not be empty")
	}
}

func TestHandleLanding_ContainsSoHoLINK(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	srv.handleLanding(w, req)

	body := w.Body.String()
	if !containsSubstring(body, "SoHoLINK") {
		t.Error("landing page should mention SoHoLINK")
	}
	if !containsSubstring(body, "<form") {
		t.Error("landing page should contain a form")
	}
}

func TestHandleAuth_GetMethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	w := httptest.NewRecorder()

	srv.handleAuth(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /auth status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleStatus_NoCookie(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	srv.handleStatus(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status without cookie = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleStatus_InvalidSession(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.AddCookie(&http.Cookie{Name: "soholink_session", Value: "nonexistent"})
	w := httptest.NewRecorder()

	srv.handleStatus(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status with invalid session = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleStatus_ExpiredSession(t *testing.T) {
	srv := newTestServer()
	srv.sessions["expired_sess"] = &Session{
		SessionID: "expired_sess",
		Username:  "bob",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.AddCookie(&http.Cookie{Name: "soholink_session", Value: "expired_sess"})
	w := httptest.NewRecorder()

	srv.handleStatus(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status with expired session = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleStatus_ValidSession(t *testing.T) {
	srv := newTestServer()
	srv.sessions["valid_sess"] = &Session{
		SessionID: "valid_sess",
		Username:  "alice",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		BytesIn:   500,
		BytesOut:  1000,
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.AddCookie(&http.Cookie{Name: "soholink_session", Value: "valid_sess"})
	w := httptest.NewRecorder()

	srv.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status with valid session = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Result().Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	body := w.Body.String()
	if !containsSubstring(body, "valid_sess") {
		t.Error("response should contain session ID")
	}
	if !containsSubstring(body, "alice") {
		t.Error("response should contain username")
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
