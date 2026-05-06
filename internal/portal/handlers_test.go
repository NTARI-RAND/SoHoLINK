package portal

import (
	"bytes"
	"context"
	"encoding/json"
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

	body := strings.NewReader("node_id=" + nodeID + "&container_image=nginx%3Alatest")
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
	arbiterID := seedStaffParticipant(t, db, "arbiter@test.com", "pw-staff-1")
	ps := newTestPortalServer(t, db)

	body := strings.NewReader("consumer_refund_pct=150")
	r := httptest.NewRequest(http.MethodPost, "/dispute/unused-id/resolve", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withClaims(r, SessionClaims{UserID: arbiterID, Email: "arbiter@test.com"})
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
	arbiterID := seedStaffParticipant(t, db, "arbiter2@test.com", "pw-staff-2")
	nodeID := seedNode(t, db, participantID, "online", "A", "US")
	jobID := seedJob(t, db, participantID, nodeID)
	disputeID := seedResolvedDispute(t, db, jobID, nodeID, participantID)

	body := strings.NewReader("consumer_refund_pct=50")
	r := httptest.NewRequest(http.MethodPost, "/dispute/"+disputeID+"/resolve", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withClaims(r, SessionClaims{UserID: arbiterID, Email: "arbiter2@test.com"})
	r.SetPathValue("id", disputeID)
	w := httptest.NewRecorder()

	ps.handleDisputeResolve(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for already-resolved dispute, got %d", w.Code)
	}
}

func TestHandleGetOptOut_returnsCurrentState(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	pid := seedParticipant(t, db, "user1@test.com", "password123")
	nid := seedNode(t, db, pid, "online", "A", "US")

	if _, err := db.Pool.Exec(context.Background(),
		`UPDATE nodes
		 SET opt_out_compute = true, opt_out_storage = false, opt_out_printing = false,
		     opt_out_version = 5
		 WHERE id = $1`, nid); err != nil {
		t.Fatalf("seed opt-out: %v", err)
	}
	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO node_printers (node_id, printer_id, printer_name, enabled) VALUES
		 ($1, 'HP_LaserJet_1', 'HP LaserJet 1', true),
		 ($1, 'HP_LaserJet_2', 'HP LaserJet 2', false)`, nid); err != nil {
		t.Fatalf("seed printers: %v", err)
	}

	req := authenticatedRequest(t, ps.sm, "GET", "/api/opt-out?node_id="+nid, pid, "user1@test.com")
	rec := httptest.NewRecorder()
	ps.srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NodeID   string `json:"node_id"`
		Compute  bool   `json:"compute"`
		Storage  bool   `json:"storage"`
		Printing bool   `json:"printing"`
		Version  int    `json:"version"`
		Printers []struct {
			PrinterID string `json:"printer_id"`
			Name      string `json:"printer_name"`
			Enabled   bool   `json:"enabled"`
		} `json:"printers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NodeID != nid {
		t.Errorf("node_id = %q, want %q", resp.NodeID, nid)
	}
	if !resp.Compute || resp.Storage || resp.Printing {
		t.Errorf("opt-out flags wrong: compute=%v storage=%v printing=%v",
			resp.Compute, resp.Storage, resp.Printing)
	}
	if resp.Version != 5 {
		t.Errorf("version = %d, want 5", resp.Version)
	}
	if len(resp.Printers) != 2 {
		t.Fatalf("printers count = %d, want 2", len(resp.Printers))
	}
	if resp.Printers[0].PrinterID != "HP_LaserJet_1" || !resp.Printers[0].Enabled {
		t.Errorf("printer[0] wrong: %+v", resp.Printers[0])
	}
	if resp.Printers[1].PrinterID != "HP_LaserJet_2" || resp.Printers[1].Enabled {
		t.Errorf("printer[1] wrong: %+v", resp.Printers[1])
	}
	if resp.Printers[0].Name != "HP LaserJet 1" || resp.Printers[1].Name != "HP LaserJet 2" {
		t.Errorf("printer names wrong: [0]=%q [1]=%q",
			resp.Printers[0].Name, resp.Printers[1].Name)
	}
}

func TestHandlePostOptOut_bumpsVersionAndAppliesState(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	pid := seedParticipant(t, db, "user2@test.com", "password123")
	nid := seedNode(t, db, pid, "online", "A", "US")

	body := map[string]any{
		"node_id":  nid,
		"compute":  true,
		"storage":  true,
		"printing": false,
		"printers": []any{},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "/api/opt-out", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	token, err := ps.sm.CreateToken(SessionClaims{UserID: pid, Email: "user2@test.com", ExpiresAt: time.Now().Add(24 * time.Hour).Unix()})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})

	rec := httptest.NewRecorder()
	ps.srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Version int  `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Errorf("success = false")
	}
	if resp.Version != 1 {
		t.Errorf("version = %d, want 1 (started at 0, +1)", resp.Version)
	}

	var (
		compute, storage, printing bool
		version                    int
		updatedAt                  time.Time
	)
	err = db.Pool.QueryRow(context.Background(),
		`SELECT opt_out_compute, opt_out_storage, opt_out_printing,
		        opt_out_version, opt_out_updated_at
		 FROM nodes WHERE id = $1`, nid,
	).Scan(&compute, &storage, &printing, &version, &updatedAt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !compute || !storage || printing {
		t.Errorf("flags wrong: compute=%v storage=%v printing=%v", compute, storage, printing)
	}
	if version != 1 {
		t.Errorf("db version = %d, want 1", version)
	}
	if time.Since(updatedAt) > 5*time.Second {
		t.Errorf("updated_at not refreshed: %v", updatedAt)
	}
}

func TestHandlePostOptOut_404OnNonOwnedNode(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	pid1 := seedParticipant(t, db, "owner@test.com", "password123")
	pid2 := seedParticipant(t, db, "intruder@test.com", "password123")
	nid := seedNode(t, db, pid1, "online", "A", "US")

	body := map[string]any{
		"node_id": nid, "compute": true, "storage": true, "printing": true,
		"printers": []any{},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/opt-out", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	token, _ := ps.sm.CreateToken(SessionClaims{UserID: pid2, Email: "intruder@test.com", ExpiresAt: time.Now().Add(24 * time.Hour).Unix()})
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})

	rec := httptest.NewRecorder()
	ps.srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	var version int
	err := db.Pool.QueryRow(context.Background(),
		`SELECT opt_out_version FROM nodes WHERE id = $1`, nid,
	).Scan(&version)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if version != 0 {
		t.Errorf("version mutated to %d, want 0 (no write should have happened)", version)
	}
}

func TestHandlePostOptOut_upsertsPrinterEnabledFlags(t *testing.T) {
	db := setupTestDB(t)
	ps := newTestPortalServer(t, db)

	pid := seedParticipant(t, db, "user3@test.com", "password123")
	nid := seedNode(t, db, pid, "online", "A", "US")

	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO node_printers (node_id, printer_id, printer_name, enabled) VALUES
		 ($1, 'PrinterA', 'Printer A', false),
		 ($1, 'PrinterB', 'Printer B', false),
		 ($1, 'PrinterC', 'Printer C', false)`, nid); err != nil {
		t.Fatalf("seed printers: %v", err)
	}

	body := map[string]any{
		"node_id": nid, "compute": false, "storage": false, "printing": false,
		"printers": []map[string]any{
			{"printer_id": "PrinterA", "enabled": true},
			{"printer_id": "PrinterB", "enabled": false},
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/opt-out", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	token, _ := ps.sm.CreateToken(SessionClaims{UserID: pid, Email: "user3@test.com", ExpiresAt: time.Now().Add(24 * time.Hour).Unix()})
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})

	rec := httptest.NewRecorder()
	ps.srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	check := func(printerID string, want bool) {
		var got bool
		err := db.Pool.QueryRow(context.Background(),
			`SELECT enabled FROM node_printers WHERE node_id = $1 AND printer_id = $2`,
			nid, printerID,
		).Scan(&got)
		if err != nil {
			t.Fatalf("query %s: %v", printerID, err)
		}
		if got != want {
			t.Errorf("%s.enabled = %v, want %v", printerID, got, want)
		}
	}
	check("PrinterA", true)
	check("PrinterB", false)
	check("PrinterC", false)
}
