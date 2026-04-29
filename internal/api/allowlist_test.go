package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleGetAllowlist_ServesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.json")
	body := []byte(`{"version":1,"entries":[],"signature":"abc"}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	h := handleGetAllowlist(path)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/allowlist", nil)
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control: got %q, want no-store", cc)
	}
	got, _ := io.ReadAll(w.Result().Body)
	if string(got) != string(body) {
		t.Errorf("body mismatch:\n got: %s\nwant: %s", got, body)
	}
}

func TestHandleGetAllowlist_ReturnsNotFoundWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	h := handleGetAllowlist(path)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/allowlist", nil)
	h(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}
