package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnavailableHandler_Returns503(t *testing.T) {
	h := UnavailableHandler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["error"] != "identity unavailable" {
		t.Errorf(`error = %q, want "identity unavailable"`, body["error"])
	}
	if body["detail"] == "" {
		t.Error("detail field is empty")
	}
}

func TestUnavailableHandler_RespondsToAnyMethod(t *testing.T) {
	h := UnavailableHandler()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/jobs", nil)
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
			}
		})
	}
}
