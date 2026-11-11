package portal

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJobHistoryRejectsIncompleteOrMismatchedCursor(t *testing.T) {
	for _, tc := range []struct {
		name, cursor string
	}{
		{"missing timestamp", `{"id":"00000000-0000-4000-8000-000000000001","status":"completed","workload_type":"batch_compute"}`},
		{"different workload", `{"created_at":"2026-01-02T03:04:05Z","id":"00000000-0000-4000-8000-000000000001","status":"completed","workload_type":"ai_inference"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := base64.RawURLEncoding.EncodeToString([]byte(tc.cursor))
			r := withClaims(httptest.NewRequest(http.MethodGet, "/consumer/jobs?status=completed&workload_type=batch_compute&cursor="+raw, nil), SessionClaims{UserID: "history-owner"})
			w := httptest.NewRecorder()
			// No database: invalid cursors must fail before any history query.
			new(PortalServer).handleJobHistory(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("got status %d, want 400", w.Code)
			}
		})
	}
}

func TestJobHistoryRejectsOversizedCursorWithoutDecodingAllocation(t *testing.T) {
	for _, size := range []int{1025, 1 << 20} {
		r := withClaims(httptest.NewRequest(http.MethodGet, "/consumer/jobs?cursor="+strings.Repeat("A", size), nil), SessionClaims{UserID: "history-owner"})
		ps := new(PortalServer)
		result := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				ps.handleJobHistory(w, r)
				if w.Code != http.StatusBadRequest {
					b.Fatalf("cursor length %d: got status %d, want 400", size, w.Code)
				}
			}
		})
		// Allow ample room for query parsing and the HTTP error response, but
		// not a decoded copy of an attacker-controlled megabyte cursor.
		if allocated := result.AllocedBytesPerOp(); allocated > 32<<10 {
			t.Fatalf("cursor length %d: allocated %d bytes/request, want at most 32768", size, allocated)
		}
	}
}
