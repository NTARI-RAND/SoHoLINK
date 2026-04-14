package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// stubOrchestrator satisfies jobSubmitter for tests. It returns a fake job ID
// on SubmitJob and records the last request for assertion.
type stubOrchestrator struct {
	lastReq orchestrator.SubmitJobRequest
	jobID   string
	err     error
}

func (s *stubOrchestrator) SubmitJob(_ context.Context, req orchestrator.SubmitJobRequest) (orchestrator.SubmitJobResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return orchestrator.SubmitJobResponse{}, s.err
	}
	id := s.jobID
	if id == "" {
		id = "test-job-id-1234"
	}
	return orchestrator.SubmitJobResponse{JobID: id}, nil
}

// mustCreateToken returns a valid, non-expired session token for the given user.
func mustCreateToken(t *testing.T, sm *SessionManager, userID, email string) string {
	t.Helper()
	tok, err := sm.CreateToken(SessionClaims{
		UserID:    userID,
		Email:     email,
		ExpiresAt: time.Now().Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("mustCreateToken: %v", err)
	}
	return tok
}

// seedStaff inserts a participant with is_staff=true and returns its UUID.
func seedStaff(t *testing.T, db *store.DB, email, password string) string {
	t.Helper()
	id := seedParticipant(t, db, email, password)
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE participants SET is_staff = TRUE WHERE id = $1`, id)
	if err != nil {
		t.Fatalf("seedStaff: %v", err)
	}
	return id
}

// seedJob inserts a minimal job row owned by participantID on nodeID and returns its UUID.
func seedJob(t *testing.T, db *store.DB, participantID, nodeID string) string {
	t.Helper()
	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status, amount_cents)
		 VALUES ($1, $2, 'app_hosting', 'disputed', 10000) RETURNING id`,
		participantID, nodeID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedJob: %v", err)
	}
	return id
}

// seedDisputeWithStatus inserts a dispute row with the given status and returns its UUID.
func seedDisputeWithStatus(t *testing.T, db *store.DB, jobID, nodeID, participantID, status string) string {
	t.Helper()
	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO disputes (job_id, node_id, participant_id, payment_intent_id, reason, status)
		 VALUES ($1, $2, $3, 'pi_test_123', 'test dispute', $4::dispute_status) RETURNING id`,
		jobID, nodeID, participantID, status,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedDisputeWithStatus(%s): %v", status, err)
	}
	return id
}

// seedOpenDispute inserts a dispute with status='open'.
func seedOpenDispute(t *testing.T, db *store.DB, jobID, nodeID, participantID string) string {
	return seedDisputeWithStatus(t, db, jobID, nodeID, participantID, "open")
}

// seedResolvedDispute inserts a dispute with status='resolved'.
func seedResolvedDispute(t *testing.T, db *store.DB, jobID, nodeID, participantID string) string {
	return seedDisputeWithStatus(t, db, jobID, nodeID, participantID, "resolved")
}

// withClaims injects SessionClaims into the request context, simulating what
// RequireAuth does. Used for direct handler calls that bypass the mux.
func withClaims(r *http.Request, claims SessionClaims) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), contextKey{}, claims))
}

// ── handleLogin ──────────────────────────────────────────────────────────────

func TestHandleLogin_ValidCredentials(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)
	seedParticipant(t, db, "valid@test.com", "password123")

	body := strings.NewReader("email=valid%40test.com&password=password123")
	r := httptest.NewRequest(http.MethodPost, "/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "1.2.3.4:1000"
	w := httptest.NewRecorder()

	ps.handleLogin(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", loc)
	}
}

func TestHandleLogin_WrongPassword(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)
	seedParticipant(t, db, "wrongpw@test.com", "correct123")

	body := strings.NewReader("email=wrongpw%40test.com&password=badpassword")
	r := httptest.NewRequest(http.MethodPost, "/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "1.2.3.5:1000"
	w := httptest.NewRecorder()

	ps.handleLogin(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleLogin_UnknownEmail(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	body := strings.NewReader("email=nobody%40test.com&password=anything")
	r := httptest.NewRequest(http.MethodPost, "/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "1.2.3.6:1000"
	w := httptest.NewRecorder()

	ps.handleLogin(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleLogin_EmptyFields(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	body := strings.NewReader("email=&password=")
	r := httptest.NewRequest(http.MethodPost, "/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "1.2.3.7:1000"
	w := httptest.NewRecorder()

	ps.handleLogin(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleLogin_RateLimiter(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)
	seedParticipant(t, db, "ratelimit@test.com", "correct123")

	ip := "9.9.9.9:1234"
	// Exhaust the 5-failure window with wrong passwords.
	for i := 0; i < 5; i++ {
		body := strings.NewReader("email=ratelimit%40test.com&password=wrong")
		r := httptest.NewRequest(http.MethodPost, "/login", body)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = ip
		w := httptest.NewRecorder()
		ps.handleLogin(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, w.Code)
		}
	}

	// 6th attempt — correct password — must be blocked by the rate limiter.
	body := strings.NewReader("email=ratelimit%40test.com&password=correct123")
	r := httptest.NewRequest(http.MethodPost, "/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = ip
	w := httptest.NewRecorder()

	ps.handleLogin(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after rate limit exhausted, got %d", w.Code)
	}
}

// ── handleRegister ───────────────────────────────────────────────────────────

func TestHandleRegister_ValidNewAccount(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	body := strings.NewReader("email=newuser%40test.com&password=strongpass1&soho_name=MyNode")
	r := httptest.NewRequest(http.MethodPost, "/register", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	ps.handleRegister(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", loc)
	}
}

func TestHandleRegister_DuplicateEmail(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)
	seedParticipant(t, db, "dup@test.com", "password123")

	body := strings.NewReader("email=dup%40test.com&password=newpassword&soho_name=ANode")
	r := httptest.NewRequest(http.MethodPost, "/register", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	ps.handleRegister(w, r)

	if w.Code == http.StatusSeeOther {
		t.Fatal("expected no redirect for duplicate email, got 303")
	}
	if !strings.Contains(w.Body.String(), "already registered") {
		t.Fatalf("expected 'already registered' in body, got: %s", w.Body.String())
	}
}

func TestHandleRegister_ShortPassword(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	body := strings.NewReader("email=short%40test.com&password=abc&soho_name=ANode")
	r := httptest.NewRequest(http.MethodPost, "/register", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	ps.handleRegister(w, r)

	if w.Code == http.StatusSeeOther {
		t.Fatal("expected no redirect for short password, got 303")
	}
	if !strings.Contains(w.Body.String(), "8 characters") {
		t.Fatalf("expected password length error in body, got: %s", w.Body.String())
	}
}

func TestHandleRegister_EmptySohoName(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	body := strings.NewReader("email=noname%40test.com&password=strongpass1&soho_name=")
	r := httptest.NewRequest(http.MethodPost, "/register", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	ps.handleRegister(w, r)

	if w.Code == http.StatusSeeOther {
		t.Fatal("expected no redirect for empty soho_name, got 303")
	}
	if !strings.Contains(w.Body.String(), "required") {
		t.Fatalf("expected 'required' error in body, got: %s", w.Body.String())
	}
}

// ── handleSubmitJob ──────────────────────────────────────────────────────────

func TestHandleSubmitJob_ValidNode(t *testing.T) {
	db := setupTestDB(t)
	stub := &stubOrchestrator{}
	ps := newTestPortalServerWithOrch(t, db, stub)
	participantID := seedParticipant(t, db, "jobsubmit@test.com", "pass1234")
	nodeID := seedNode(t, db, participantID, "online", "A", "US")

	body := strings.NewReader("node_id=" + nodeID)
	r := httptest.NewRequest(http.MethodPost, "/consumer/job", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withClaims(r, SessionClaims{UserID: participantID, Email: "jobsubmit@test.com"})
	w := httptest.NewRecorder()

	ps.handleSubmitJob(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/consumer/job/") {
		t.Fatalf("expected redirect to /consumer/job/{id}, got %q", loc)
	}
}

func TestHandleSubmitJob_NodeNotFound(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)
	participantID := seedParticipant(t, db, "jobnotfound@test.com", "pass1234")

	body := strings.NewReader("node_id=00000000-0000-0000-0000-000000000000")
	r := httptest.NewRequest(http.MethodPost, "/consumer/job", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withClaims(r, SessionClaims{UserID: participantID, Email: "jobnotfound@test.com"})
	w := httptest.NewRecorder()

	ps.handleSubmitJob(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleSubmitJob_NodeOffline(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)
	participantID := seedParticipant(t, db, "joboffline@test.com", "pass1234")
	nodeID := seedNode(t, db, participantID, "offline", "A", "US")

	body := strings.NewReader("node_id=" + nodeID)
	r := httptest.NewRequest(http.MethodPost, "/consumer/job", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withClaims(r, SessionClaims{UserID: participantID, Email: "joboffline@test.com"})
	w := httptest.NewRecorder()

	ps.handleSubmitJob(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

// ── handleDisputeResolve ─────────────────────────────────────────────────────

func TestHandleDisputeResolve_InvalidPct(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	body := strings.NewReader("consumer_refund_pct=150")
	r := httptest.NewRequest(http.MethodPost, "/dispute/unused-id/resolve", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withClaims(r, SessionClaims{UserID: "arbiter-id", Email: "arbiter@test.com"})
	w := httptest.NewRecorder()

	ps.handleDisputeResolve(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for pct=150, got %d", w.Code)
	}
}

func TestHandleDisputeResolve_AlreadyResolved(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)
	participantID := seedParticipant(t, db, "disputer@test.com", "pass1234")
	nodeID := seedNode(t, db, participantID, "online", "A", "US")
	jobID := seedJob(t, db, participantID, nodeID)
	disputeID := seedResolvedDispute(t, db, jobID, nodeID, participantID)

	body := strings.NewReader("consumer_refund_pct=50")
	r := httptest.NewRequest(http.MethodPost, "/dispute/"+disputeID+"/resolve", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withClaims(r, SessionClaims{UserID: participantID, Email: "disputer@test.com"})
	r.SetPathValue("id", disputeID)
	w := httptest.NewRecorder()

	ps.handleDisputeResolve(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for already-resolved dispute, got %d", w.Code)
	}
}
