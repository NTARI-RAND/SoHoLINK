package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJobStatusFailureExplanation(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)
	owner := seedParticipant(t, db, "owner@test.com", "password123")
	other := seedParticipant(t, db, "other@test.com", "password123")

	const internalDetail = "open /var/lib/worker/private-input: permission denied"
	for _, tc := range []struct {
		name   string
		status string
		cause  any
		want   string
	}{
		{"temporary storage", "failed", "tmpfs_exhausted", "Reduce the amount of temporary data"},
		{"raw worker error", "failed", internalDetail, "Check your job settings"},
		{"missing cause", "failed", nil, "Check your job settings"},
		{"print no-show", "failed", "no_show_after_7d", "Contest no-show"},
		{"running", "running", "tmpfs_exhausted", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var id string
			err := db.Pool.QueryRow(context.Background(),
				`INSERT INTO jobs (participant_id, workload_type, status, failure_cause)
				 VALUES ($1, 'app_hosting', $2, $3) RETURNING id`,
				owner, tc.status, tc.cause,
			).Scan(&id)
			if err != nil {
				t.Fatal(err)
			}
			path := "/consumer/job/" + id

			page := httptest.NewRecorder()
			ps.srv.Handler.ServeHTTP(page, authenticatedRequest(t, ps.sm, http.MethodGet, path, owner, "owner@test.com"))
			if page.Code != http.StatusOK {
				t.Fatalf("owner response = %d, want 200", page.Code)
			}
			body := page.Body.String()
			if tc.want != "" && !strings.Contains(body, tc.want) {
				t.Errorf("owner page missing %q", tc.want)
			}
			if strings.Contains(body, internalDetail) || strings.Contains(body, "tmpfs_exhausted") {
				t.Error("owner page exposed raw failure details")
			}
			if (tc.name == "print no-show" || tc.status != "failed") && strings.Contains(body, "Why this job failed") {
				t.Error("page showed a failure explanation outside its intended states")
			}

			denied := httptest.NewRecorder()
			ps.srv.Handler.ServeHTTP(denied, authenticatedRequest(t, ps.sm, http.MethodGet, path, other, "other@test.com"))
			if denied.Code != http.StatusNotFound || denied.Body.String() != "job not found\n" {
				t.Errorf("non-owner response = %d %q, want generic 404", denied.Code, denied.Body.String())
			}
		})
	}
}
