package protocoladapter

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spiffe/go-spiffe/v2/spiffeid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
)

// nextRecorder is a terminal handler that records whether it ran and what
// body it could still read (bindNodeID must restore the buffered body).
type nextRecorder struct {
	called bool
	body   string
}

func (n *nextRecorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.called = true
		b, _ := io.ReadAll(r.Body)
		n.body = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
}

func withNode(r *http.Request, nodeID string) *http.Request {
	id := spiffeid.RequireFromString("spiffe://soholink.org/node/" + nodeID)
	return r.WithContext(identity.WithSPIFFEID(r.Context(), id))
}

func TestBindNodeID_POST_NoIdentity_401(t *testing.T) {
	for _, path := range []string{"/v0/listing", "/v0/heartbeat", "/v0/decline", "/v0/report"} {
		next := &nextRecorder{}
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"NodeID":"node-1"}`))
		w := httptest.NewRecorder()
		bindNodeID(next.handler()).ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", path, w.Code)
		}
		if next.called {
			t.Errorf("%s: inner handler ran despite missing identity", path)
		}
	}
}

func TestBindNodeID_POST_Mismatch_403(t *testing.T) {
	for _, path := range []string{"/v0/listing", "/v0/heartbeat", "/v0/decline", "/v0/report"} {
		next := &nextRecorder{}
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"NodeID":"node-1"}`))
		r = withNode(r, "node-OTHER")
		w := httptest.NewRecorder()
		bindNodeID(next.handler()).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", path, w.Code)
		}
		if next.called {
			t.Errorf("%s: inner handler ran despite identity mismatch", path)
		}
	}
}

func TestBindNodeID_POST_Match_PassesWithBodyRestored(t *testing.T) {
	const body = `{"NodeID":"node-1","Seq":7}`
	next := &nextRecorder{}
	r := httptest.NewRequest(http.MethodPost, "/v0/heartbeat", strings.NewReader(body))
	r = withNode(r, "node-1")
	w := httptest.NewRecorder()
	bindNodeID(next.handler()).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from inner handler, got %d: %s", w.Code, w.Body.String())
	}
	if !next.called {
		t.Fatal("inner handler did not run")
	}
	if next.body != body {
		t.Errorf("inner handler saw body %q, want %q (bindNodeID must restore it)", next.body, body)
	}
}

func TestBindNodeID_POST_MissingNodeID_400(t *testing.T) {
	next := &nextRecorder{}
	r := httptest.NewRequest(http.MethodPost, "/v0/listing", strings.NewReader(`{}`))
	r = withNode(r, "node-1")
	w := httptest.NewRecorder()
	bindNodeID(next.handler()).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing NodeID, got %d", w.Code)
	}
}

func TestBindNodeID_POST_BadJSON_400(t *testing.T) {
	next := &nextRecorder{}
	r := httptest.NewRequest(http.MethodPost, "/v0/report", strings.NewReader(`{nope`))
	r = withNode(r, "node-1")
	w := httptest.NewRecorder()
	bindNodeID(next.handler()).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", w.Code)
	}
}

func TestBindNodeID_GETJobs_BindsQueryParam(t *testing.T) {
	// Missing node_id → 400.
	next := &nextRecorder{}
	r := httptest.NewRequest(http.MethodGet, "/v0/jobs", nil)
	r = withNode(r, "node-1")
	w := httptest.NewRecorder()
	bindNodeID(next.handler()).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing node_id: expected 400, got %d", w.Code)
	}

	// No identity → 401.
	next = &nextRecorder{}
	r = httptest.NewRequest(http.MethodGet, "/v0/jobs?node_id=node-1", nil)
	w = httptest.NewRecorder()
	bindNodeID(next.handler()).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no identity: expected 401, got %d", w.Code)
	}

	// Mismatch → 403.
	next = &nextRecorder{}
	r = httptest.NewRequest(http.MethodGet, "/v0/jobs?node_id=node-1", nil)
	r = withNode(r, "node-OTHER")
	w = httptest.NewRecorder()
	bindNodeID(next.handler()).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("mismatch: expected 403, got %d", w.Code)
	}

	// Match → inner handler runs.
	next = &nextRecorder{}
	r = httptest.NewRequest(http.MethodGet, "/v0/jobs?node_id=node-1", nil)
	r = withNode(r, "node-1")
	w = httptest.NewRecorder()
	bindNodeID(next.handler()).ServeHTTP(w, r)
	if !next.called {
		t.Fatal("match: inner handler did not run")
	}
}

func TestBindNodeID_OversizedBody_413(t *testing.T) {
	next := &nextRecorder{}
	big := `{"NodeID":"node-1","pad":"` + strings.Repeat("x", maxBindBody) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/v0/listing", strings.NewReader(big))
	r = withNode(r, "node-1")
	w := httptest.NewRecorder()
	bindNodeID(next.handler()).ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d", w.Code)
	}
}
