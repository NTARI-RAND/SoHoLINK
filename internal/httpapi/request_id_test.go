package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddleware_AssignsID(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requestIDMiddleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	rid := w.Header().Get("X-Request-ID")
	if rid == "" {
		t.Error("expected X-Request-ID header to be set")
	}
	if len(rid) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("X-Request-ID length = %d, want 16", len(rid))
	}
}

func TestRequestIDMiddleware_PreservesClientID(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requestIDMiddleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "client-provided-id")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	got := w.Header().Get("X-Request-ID")
	if got != "client-provided-id" {
		t.Errorf("X-Request-ID = %q, want %q", got, "client-provided-id")
	}
}

func TestRequestIDMiddleware_UniqueIDs(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requestIDMiddleware(inner)
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		rid := w.Header().Get("X-Request-ID")
		if ids[rid] {
			t.Fatalf("duplicate request ID generated: %s", rid)
		}
		ids[rid] = true
	}
}

func TestGenerateRequestID_Length(t *testing.T) {
	for i := 0; i < 50; i++ {
		id := generateRequestID()
		if len(id) != 16 {
			t.Errorf("generateRequestID() length = %d, want 16", len(id))
		}
	}
}
