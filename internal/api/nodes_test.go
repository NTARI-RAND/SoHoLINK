package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// ── test helpers ─────────────────────────────────────────────────────────────

func connectAPITestDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test (see docs/test-database.md)")
	}
	db, err := store.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("store.Connect: %v", err)
	}
	t.Cleanup(func() { db.Pool.Close() })
	if err := store.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	var dbName string
	if err := db.Pool.QueryRow(context.Background(), `SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	if !strings.Contains(dbName, "test") {
		t.Fatalf("refusing to run destructive integration test: connected database %q does not contain \"test\" in its name; set TEST_DATABASE_URL to a dedicated test database", dbName)
	}
	// clean slate for each test
	if _, err := db.Pool.Exec(context.Background(), `TRUNCATE participants CASCADE`); err != nil {
		t.Fatalf("TRUNCATE: %v", err)
	}
	return db
}

func newAPIServer(t *testing.T, db *store.DB) *APIServer {
	t.Helper()
	registry := orchestrator.NewNodeRegistry()
	return &APIServer{
		db:       db,
		registry: registry,
	}
}

func seedAPIParticipant(t *testing.T, db *store.DB, email string) string {
	t.Helper()
	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO participants (email, display_name, soho_name)
		 VALUES ($1, $1, 'api-test-node') RETURNING id`, email,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedAPIParticipant: %v", err)
	}
	return id
}

func postJSON(t *testing.T, handler http.HandlerFunc, path string, body any, headers ...map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	for _, hm := range headers {
		for k, v := range hm {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

// postJSONAs is postJSON with a SPIFFE node identity injected into the request
// context (spiffe://soholink.org/node/<nodeID>), for handlers that enforce
// SPIFFE peer binding (heartbeat, report-printers).
func postJSONAs(t *testing.T, handler http.HandlerFunc, path string, body any, nodeID string, headers ...map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	for _, hm := range headers {
		for k, v := range hm {
			r.Header.Set(k, v)
		}
	}
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

// testPrinterHash mirrors the agent-side PrinterHash algorithm so tests can
// construct request bodies the server will accept or reject deterministically.
func testPrinterHash(ids ...string) string {
	if len(ids) == 0 {
		return ""
	}
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:])
}

// registerTestNode runs a register call and returns silently on success.
func registerTestNode(t *testing.T, ps *APIServer, participantID, nodeID string, printers []map[string]string) {
	t.Helper()
	payload := map[string]any{
		"node_id":      nodeID,
		"provider_id":  participantID,
		"hostname":     "test-host",
		"node_class":   "A",
		"country_code": "US",
		"hardware_profile": map[string]any{
			"CPUCores": 2,
			"RAMMB":    4096,
			"printers": printers,
		},
	}
	w := postJSON(t, ps.handleRegisterNode, "/nodes/register", payload,
		map[string]string{"X-Register-Secret": "test-secret"})
	if w.Code != http.StatusOK {
		t.Fatalf("registerTestNode: %d: %s", w.Code, w.Body.String())
	}
}

// withNodeSPIFFE returns a new request whose context carries a SPIFFE ID
// matching spiffe://soholink.org/node/<nodeID>. Tests use this to simulate
// the peer identity that RequireSPIFFE would populate in production.
func withNodeSPIFFE(r *http.Request, nodeID string) *http.Request {
	id := spiffeid.RequireFromString("spiffe://soholink.org/node/" + nodeID)
	return r.WithContext(identity.WithSPIFFEID(r.Context(), id))
}

// ── handleRegisterNode ───────────────────────────────────────────────────────

func TestHandleRegisterNode_Valid(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "reg_node@test.com")

	w := postJSON(t, ps.handleRegisterNode, "/nodes/register", map[string]any{
		"node_id":      "10000000-0000-0000-0000-000000000001",
		"provider_id":  participantID,
		"hostname":     "test-host-001",
		"node_class":   "A",
		"country_code": "US",
		"hardware_profile": map[string]any{
			"CPUCores": 4,
			"RAMMB":    8192,
		},
	}, map[string]string{"X-Register-Secret": "test-secret"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// verify node row exists in DB
	var count int
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM nodes WHERE participant_id = $1`, participantID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("db query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 node row, got %d", count)
	}
}

func TestHandleRegisterNode_MissingFields(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)

	w := postJSON(t, ps.handleRegisterNode, "/nodes/register", map[string]any{
		"node_id": "",
	}, map[string]string{"X-Register-Secret": "test-secret"})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRegisterNode_Upsert(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "upsert_node@test.com")

	payload := map[string]any{
		"node_id":      "20000000-0000-0000-0000-000000000002",
		"provider_id":  participantID,
		"hostname":     "upsert-host",
		"node_class":   "B",
		"country_code": "CA",
		"hardware_profile": map[string]any{"CPUCores": 2, "RAMMB": 4096},
	}

	// first registration
	w1 := postJSON(t, ps.handleRegisterNode, "/nodes/register", payload, map[string]string{"X-Register-Secret": "test-secret"})
	if w1.Code != http.StatusOK {
		t.Fatalf("first register: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// second registration — same node_id, should upsert not error
	w2 := postJSON(t, ps.handleRegisterNode, "/nodes/register", payload, map[string]string{"X-Register-Secret": "test-secret"})
	if w2.Code != http.StatusOK {
		t.Fatalf("upsert: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var count int
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM nodes WHERE participant_id = $1`, participantID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("db query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 node row after upsert, got %d", count)
	}
}

// ── handleHeartbeat ──────────────────────────────────────────────────────────

func TestHandleHeartbeat_Valid(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "hb_valid@test.com")

	// register first so node is in memory registry
	postJSON(t, ps.handleRegisterNode, "/nodes/register", map[string]any{
		"node_id":      "30000000-0000-0000-0000-000000000003",
		"provider_id":  participantID,
		"hostname":     "hb-host-001",
		"node_class":   "A",
		"country_code": "US",
		"hardware_profile": map[string]any{"CPUCores": 2, "RAMMB": 4096},
	}, map[string]string{"X-Register-Secret": "test-secret"})

	w := postJSONAs(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id": "30000000-0000-0000-0000-000000000003",
	}, "30000000-0000-0000-0000-000000000003")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// verify heartbeat event was inserted
	var count int
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM node_heartbeat_events WHERE node_id =
		 (SELECT id FROM nodes WHERE participant_id = $1)`, participantID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("db query: %v", err)
	}
	if count < 1 {
		t.Errorf("expected at least 1 heartbeat event, got %d", count)
	}
}

func TestHandleHeartbeat_UnknownNode(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)

	w := postJSONAs(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id": "ghost-node-999",
	}, "ghost-node-999")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ── handleCompleteJob ────────────────────────────────────────────────────────

func TestHandleCompleteJob_RunningJob(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_job@test.com")

	// seed node
	var nodeID string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID)
	if err != nil {
		t.Fatalf("seed node: %v", err)
	}

	// seed a running job
	var jobID string
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID)
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}

	// inject node_id into path value
	b, _ := json.Marshal(map[string]any{"exit_code": 0})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	err = db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status)
	if err != nil {
		t.Fatalf("query job status: %v", err)
	}
	if status != "completed" {
		t.Errorf("expected status=completed, got %q", status)
	}

	var exitCode *int
	var failureCause *string
	err = db.Pool.QueryRow(context.Background(),
		`SELECT exit_code, failure_cause FROM jobs WHERE id = $1`, jobID,
	).Scan(&exitCode, &failureCause)
	if err != nil {
		t.Fatalf("query job columns: %v", err)
	}
	if exitCode == nil || *exitCode != 0 {
		t.Errorf("expected exit_code=0, got %v", exitCode)
	}
	if failureCause != nil {
		t.Errorf("expected failure_cause=NULL, got %q", *failureCause)
	}

	var completedAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT completed_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&completedAt); err != nil {
		t.Fatalf("query completed_at: %v", err)
	}
	if completedAt == nil {
		t.Error("expected completed_at to be set, got NULL")
	}

	var meterCount int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&meterCount); err != nil {
		t.Fatalf("query job_metering: %v", err)
	}
	if meterCount == 0 {
		t.Error("expected job_metering row to exist after successful completion")
	}

	var awaitingPickupAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT awaiting_pickup_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&awaitingPickupAt); err != nil {
		t.Fatalf("query awaiting_pickup_at: %v", err)
	}
	if awaitingPickupAt != nil {
		t.Errorf("expected awaiting_pickup_at IS NULL for compute completed path, got %v", *awaitingPickupAt)
	}
}

func TestHandleCompleteJob_NotRunning(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_bad@test.com")

	var nodeID string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-bad-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID)
	if err != nil {
		t.Fatalf("seed node: %v", err)
	}

	// seed a scheduled (not running) job
	var jobID string
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb)
		 VALUES ($1, $2, 'app_hosting', 'scheduled', 0, 2, 4096)
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID)
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 0})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestHandleCompleteJob_PersistsExitCodeNonzero(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_nonzero@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-nonzero-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 1})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var exitCode *int
	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT exit_code, status FROM jobs WHERE id = $1`, jobID,
	).Scan(&exitCode, &status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if exitCode == nil || *exitCode != 1 {
		t.Errorf("expected exit_code=1, got %v", exitCode)
	}
	if status != "failed" {
		t.Errorf("expected status=failed, got %q", status)
	}

	var completedAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT completed_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&completedAt); err != nil {
		t.Fatalf("query completed_at: %v", err)
	}
	if completedAt == nil {
		t.Error("expected completed_at to be set on failed status")
	}
}

func TestHandleCompleteJob_PersistsFailureCause(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_cause@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-cause-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 1, "failure_cause": "container OOM"})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var exitCode *int
	var failureCause *string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT exit_code, failure_cause FROM jobs WHERE id = $1`, jobID,
	).Scan(&exitCode, &failureCause); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if exitCode == nil || *exitCode != 1 {
		t.Errorf("expected exit_code=1, got %v", exitCode)
	}
	if failureCause == nil || *failureCause != "container OOM" {
		t.Errorf("expected failure_cause=%q, got %v", "container OOM", failureCause)
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "failed" {
		t.Errorf("expected status=failed, got %q", status)
	}
}

func TestHandleCompleteJob_TmpfsFallback(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_tmpfs@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-tmpfs-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	// no explicit failure_cause — should be derived from tmpfs_exhausted flag
	b, _ := json.Marshal(map[string]any{"exit_code": 1, "tmpfs_exhausted": true})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var failureCause *string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT failure_cause FROM jobs WHERE id = $1`, jobID,
	).Scan(&failureCause); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if failureCause == nil || *failureCause != "tmpfs_exhausted" {
		t.Errorf("expected failure_cause=%q, got %v", "tmpfs_exhausted", failureCause)
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "failed" {
		t.Errorf("expected status=failed, got %q", status)
	}
}

func TestHandleCompleteJob_NilExitCode_Failed(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_nobody@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-nobody-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	// Old agents send no body. After C4, nil exit_code means we cannot confirm
	// success — treated as failed. This is intentional: metering must not fire
	// without explicit confirmation of exit_code == 0.
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", http.NoBody)
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	var exitCode *int
	var failureCause *string
	var completedAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status, exit_code, failure_cause, completed_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &exitCode, &failureCause, &completedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "failed" {
		t.Errorf("expected status=failed (nil exit_code → cannot confirm success), got %q", status)
	}
	if exitCode != nil {
		t.Errorf("expected exit_code=NULL, got %d", *exitCode)
	}
	if failureCause != nil {
		t.Errorf("expected failure_cause=NULL, got %q", *failureCause)
	}
	if completedAt == nil {
		t.Error("expected completed_at to be set on failed status")
	}
}

func TestHandleCompleteJob_MalformedBody_400(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_malformed@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-malformed-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	// exit_code is a string — malformed JSON for our struct, triggers 400
	b := []byte(`{"exit_code": "not a number"}`)
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// UPDATE was never executed — job should still be in running state
	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "running" {
		t.Errorf("expected status=running after 400, got %q", status)
	}
}

func TestHandleCompleteJob_Print_AwaitingPickup(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_print_trad@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-print-trad-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'print_traditional', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 0})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var respBody map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if respBody["status"] != "awaiting_pickup" {
		t.Errorf("expected response status=awaiting_pickup, got %q", respBody["status"])
	}

	var status string
	var completedAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status, completed_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &completedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "awaiting_pickup" {
		t.Errorf("expected status=awaiting_pickup, got %q", status)
	}
	if completedAt != nil {
		t.Error("expected completed_at=NULL for non-terminal awaiting_pickup")
	}

	var meterCount int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&meterCount); err != nil {
		t.Fatalf("query job_metering: %v", err)
	}
	if meterCount != 0 {
		t.Errorf("expected no metering for print job in awaiting_pickup, got %d rows", meterCount)
	}

	var awaitingPickupAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT awaiting_pickup_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&awaitingPickupAt); err != nil {
		t.Fatalf("query awaiting_pickup_at: %v", err)
	}
	if awaitingPickupAt == nil {
		t.Error("expected awaiting_pickup_at IS NOT NULL for print_traditional awaiting_pickup")
	}
}

func TestHandleCompleteJob_Print3D_AwaitingPickup(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_print3d@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-print3d-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'print_3d', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 0})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	var completedAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status, completed_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &completedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "awaiting_pickup" {
		t.Errorf("expected status=awaiting_pickup, got %q", status)
	}
	if completedAt != nil {
		t.Error("expected completed_at=NULL for non-terminal awaiting_pickup")
	}

	var meterCount int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&meterCount); err != nil {
		t.Fatalf("query job_metering: %v", err)
	}
	if meterCount != 0 {
		t.Errorf("expected no metering for print_3d job in awaiting_pickup, got %d rows", meterCount)
	}

	var awaitingPickupAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT awaiting_pickup_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&awaitingPickupAt); err != nil {
		t.Fatalf("query awaiting_pickup_at: %v", err)
	}
	if awaitingPickupAt == nil {
		t.Error("expected awaiting_pickup_at IS NOT NULL for print_3d awaiting_pickup")
	}
}

func TestHandleCompleteJob_FailedSetsCompletedAt(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_failed_at@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-failed-at-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 1})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	var completedAt *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status, completed_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &completedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "failed" {
		t.Errorf("expected status=failed, got %q", status)
	}
	if completedAt == nil {
		t.Error("expected completed_at to be set on failed status (failed is terminal)")
	}

	var meterCount int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&meterCount); err != nil {
		t.Fatalf("query job_metering: %v", err)
	}
	if meterCount != 0 {
		t.Errorf("expected no metering on failed status, got %d rows", meterCount)
	}
}

func TestHandleCompleteJob_NoMeteringOnFailed(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_no_meter@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-no-meter-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'batch_compute', 'running', 0, 2, 4096, NOW())
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 2, "failure_cause": "oom kill"})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var meterCount int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&meterCount); err != nil {
		t.Fatalf("query job_metering: %v", err)
	}
	if meterCount != 0 {
		t.Errorf("metering must not fire on failed status: got %d rows in job_metering", meterCount)
	}
}

// ── handleHeartbeat (opt-out + printer-hash) ─────────────────────────────────

func TestHandleHeartbeat_VersionMatch_NoOptOut(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "hb_match@test.com")
	nodeID := "40000000-0000-0000-0000-000000000001"
	registerTestNode(t, ps, pid, nodeID, nil)

	w := postJSONAs(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":         nodeID,
		"opt_out_version": 0,
		"printer_hash":    "",
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		OK                   bool        `json:"ok"`
		OptOut               interface{} `json:"opt_out"`
		RequestPrinterReport bool        `json:"request_printer_report"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OptOut != nil {
		t.Errorf("expected opt_out=nil when versions match, got %v", resp.OptOut)
	}
	if resp.RequestPrinterReport {
		t.Errorf("expected request_printer_report=false")
	}
}

func TestHandleHeartbeat_VersionDrift_PushesOptOut(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "hb_drift@test.com")
	nodeID := "40000000-0000-0000-0000-000000000002"
	registerTestNode(t, ps, pid, nodeID, nil)

	_, err := db.Pool.Exec(context.Background(), `
		UPDATE nodes
		SET opt_out_version = 5,
		    opt_out_compute = TRUE,
		    opt_out_updated_at = NOW()
		WHERE id = $1`, nodeID)
	if err != nil {
		t.Fatalf("seed opt-out: %v", err)
	}

	w := postJSONAs(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":         nodeID,
		"opt_out_version": 0,
		"printer_hash":    "",
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		OK     bool `json:"ok"`
		OptOut *struct {
			Version         int             `json:"version"`
			ComputeEnabled  bool            `json:"compute_enabled"`
			StorageEnabled  bool            `json:"storage_enabled"`
			PrintingEnabled bool            `json:"printing_enabled"`
			EnabledPrinters map[string]bool `json:"enabled_printers"`
		} `json:"opt_out"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OptOut == nil {
		t.Fatalf("expected opt_out payload when versions differ, got nil")
	}
	if resp.OptOut.Version != 5 {
		t.Errorf("expected version=5, got %d", resp.OptOut.Version)
	}
	if !resp.OptOut.ComputeEnabled {
		t.Errorf("expected ComputeEnabled=true")
	}
	if resp.OptOut.StorageEnabled || resp.OptOut.PrintingEnabled {
		t.Errorf("expected storage/printing=false")
	}
}

func TestHandleHeartbeat_PrinterHashMatch_NoReportRequest(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "hb_phmatch@test.com")
	nodeID := "40000000-0000-0000-0000-000000000003"
	registerTestNode(t, ps, pid, nodeID, []map[string]string{
		{"id": "printer-A", "name": "Office Laser"},
		{"id": "printer-B", "name": "Front Desk"},
	})

	w := postJSONAs(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":         nodeID,
		"opt_out_version": 0,
		"printer_hash":    testPrinterHash("printer-A", "printer-B"),
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		RequestPrinterReport bool `json:"request_printer_report"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp.RequestPrinterReport {
		t.Errorf("expected request_printer_report=false on hash match")
	}
}

func TestHandleHeartbeat_PrinterHashMismatch_RequestsReport(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "hb_phmiss@test.com")
	nodeID := "40000000-0000-0000-0000-000000000004"
	registerTestNode(t, ps, pid, nodeID, []map[string]string{
		{"id": "printer-A", "name": "Office Laser"},
	})

	w := postJSONAs(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":         nodeID,
		"opt_out_version": 0,
		"printer_hash":    testPrinterHash("printer-A", "printer-NEW"),
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		RequestPrinterReport bool `json:"request_printer_report"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if !resp.RequestPrinterReport {
		t.Errorf("expected request_printer_report=true on hash mismatch")
	}
}

// ── handleRegisterNode (printer upsert) ──────────────────────────────────────

func TestHandleRegisterNode_WithPrinters_CreatesRows(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "reg_printers@test.com")
	nodeID := "40000000-0000-0000-0000-000000000005"

	registerTestNode(t, ps, pid, nodeID, []map[string]string{
		{"id": "p1", "name": "Printer One"},
		{"id": "p2", "name": "Printer Two"},
	})

	var count int
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM node_printers WHERE node_id = $1`, nodeID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 node_printers rows, got %d", count)
	}

	var enabledCount int
	db.Pool.QueryRow(context.Background(), //nolint:errcheck
		`SELECT COUNT(*) FROM node_printers WHERE node_id = $1 AND enabled = TRUE`, nodeID,
	).Scan(&enabledCount)
	if enabledCount != 0 {
		t.Errorf("expected 0 enabled printers on first register, got %d", enabledCount)
	}
}

func TestHandleRegisterNode_PreservesEnabledFlagOnReRegister(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "reg_preserve@test.com")
	nodeID := "40000000-0000-0000-0000-000000000006"

	registerTestNode(t, ps, pid, nodeID, []map[string]string{
		{"id": "p1", "name": "Printer One"},
	})

	_, err := db.Pool.Exec(context.Background(),
		`UPDATE node_printers SET enabled = TRUE WHERE node_id = $1 AND printer_id = $2`,
		nodeID, "p1")
	if err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	registerTestNode(t, ps, pid, nodeID, []map[string]string{
		{"id": "p1", "name": "Printer One (renamed)"},
	})

	var enabled bool
	var name string
	err = db.Pool.QueryRow(context.Background(),
		`SELECT enabled, printer_name FROM node_printers WHERE node_id = $1 AND printer_id = $2`,
		nodeID, "p1").Scan(&enabled, &name)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !enabled {
		t.Errorf("expected enabled flag preserved across re-register, got false")
	}
	if name != "Printer One (renamed)" {
		t.Errorf("expected name updated on upsert, got %q", name)
	}
}

// ── handleReportPrinters ─────────────────────────────────────────────────────

func TestHandleReportPrinters_UpsertsPreservingEnabled(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "report_printers@test.com")
	nodeID := "40000000-0000-0000-0000-000000000007"

	registerTestNode(t, ps, pid, nodeID, []map[string]string{
		{"id": "p1", "name": "Original Name"},
	})
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE node_printers SET enabled = TRUE WHERE node_id = $1 AND printer_id = $2`,
		nodeID, "p1")
	if err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	w := postJSONAs(t, ps.handleReportPrinters, "/nodes/printers", map[string]any{
		"node_id": nodeID,
		"printers": []map[string]string{
			{"id": "p1", "name": "Updated Name"},
			{"id": "p2", "name": "New Printer"},
		},
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var enabled bool
	var name string
	err = db.Pool.QueryRow(context.Background(),
		`SELECT enabled, printer_name FROM node_printers WHERE node_id = $1 AND printer_id = $2`,
		nodeID, "p1").Scan(&enabled, &name)
	if err != nil {
		t.Fatalf("query p1: %v", err)
	}
	if !enabled {
		t.Errorf("expected p1.enabled preserved, got false")
	}
	if name != "Updated Name" {
		t.Errorf("expected p1.printer_name updated, got %q", name)
	}

	err = db.Pool.QueryRow(context.Background(),
		`SELECT enabled FROM node_printers WHERE node_id = $1 AND printer_id = $2`,
		nodeID, "p2").Scan(&enabled)
	if err != nil {
		t.Fatalf("query p2: %v", err)
	}
	if enabled {
		t.Errorf("expected p2 inserted with enabled=false, got true")
	}
}

// ── handleGetJobs ─────────────────────────────────────────────────────────────

func TestHandleGetJobs_FlipsToDispatched(t *testing.T) {
	db := connectAPITestDB(t)
	participantID := seedAPIParticipant(t, db, "getjobs_dispatched@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'getjobs-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, container_image)
		 VALUES ($1, $2, 'app_hosting', 'scheduled', 0, 2, 4096, 'img:latest')
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/nodes/jobs?node_id="+nodeID, nil)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	handleGetJobs(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	var startedAt *string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status, started_at::text FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &startedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "dispatched" {
		t.Errorf("status: got %q, want %q", status, "dispatched")
	}
	if startedAt != nil {
		t.Errorf("started_at: expected NULL, got %q", *startedAt)
	}
}

func TestHandleGetJobs_RejectsSelfPrintTraditional(t *testing.T) {
	db := connectAPITestDB(t)
	participantID := seedAPIParticipant(t, db, "selfprint_trad@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'selfprint-trad-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, container_image)
		 VALUES ($1, $2, 'print_traditional', 'scheduled', 0, 2, 4096, 'img:latest')
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/nodes/jobs?node_id="+nodeID, nil)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	handleGetJobs(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got []json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty job list for self-print, got %d jobs", len(got))
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "scheduled" {
		t.Errorf("status: got %q, want %q (self-print should not dispatch)", status, "scheduled")
	}
}

func TestHandleGetJobs_RejectsSelfPrint3D(t *testing.T) {
	db := connectAPITestDB(t)
	participantID := seedAPIParticipant(t, db, "selfprint_3d@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'selfprint-3d-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, container_image)
		 VALUES ($1, $2, 'print_3d', 'scheduled', 0, 2, 4096, 'img:latest')
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/nodes/jobs?node_id="+nodeID, nil)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	handleGetJobs(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got []json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty job list for self-print, got %d jobs", len(got))
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "scheduled" {
		t.Errorf("status: got %q, want %q (self-print should not dispatch)", status, "scheduled")
	}
}

func TestHandleGetJobs_AllowsOtherOwnedPrint(t *testing.T) {
	db := connectAPITestDB(t)
	nodeOwnerID := seedAPIParticipant(t, db, "selfprint_nodeowner@test.com")
	jobConsumerID := seedAPIParticipant(t, db, "selfprint_consumer@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'selfprint-other-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		nodeOwnerID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, container_image)
		 VALUES ($1, $2, 'print_traditional', 'scheduled', 0, 2, 4096, 'img:latest')
		 RETURNING id`,
		jobConsumerID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/nodes/jobs?node_id="+nodeID, nil)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	handleGetJobs(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "dispatched" {
		t.Errorf("status: got %q, want %q (different-participant print should dispatch)", status, "dispatched")
	}
}

// ── handleStartedJob ──────────────────────────────────────────────────────────

func TestHandleStartedJob_Success(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "started_success@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'started-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb)
		 VALUES ($1, $2, 'app_hosting', 'dispatched', 0, 2, 4096)
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/started", nil)
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleStartedJob(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	var startedAt *string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status, started_at::text FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &startedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "running" {
		t.Errorf("status: got %q, want %q", status, "running")
	}
	if startedAt == nil {
		t.Errorf("started_at: expected non-NULL, got nil")
	}
}

func TestHandleStartedJob_NotDispatched_409(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "started_409@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'started-409-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb)
		 VALUES ($1, $2, 'app_hosting', 'scheduled', 0, 2, 4096)
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/started", nil)
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, nodeID)
	w := httptest.NewRecorder()
	ps.handleStartedJob(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "scheduled" {
		t.Errorf("status: got %q, want unchanged %q", status, "scheduled")
	}
}

func TestHandleStartedJob_NotFound_404(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)

	r := httptest.NewRequest(http.MethodPost, "/jobs/00000000-0000-0000-0000-000000000000/started", nil)
	r.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
	w := httptest.NewRecorder()
	ps.handleStartedJob(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ── handleCompleteJob SPIFFE binding ─────────────────────────────────────────

func TestHandleCompleteJob_SPIFFEMissing_401(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_no_spiffe@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-no-spiffe', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`, participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 0})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	// Intentionally do NOT inject SPIFFE — simulates router misconfiguration.
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm side-effects did NOT occur: job remains running.
	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "running" {
		t.Errorf("expected job status unchanged 'running', got %q", status)
	}
}

func TestHandleCompleteJob_SPIFFEMismatch_403(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "complete_mismatch@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'complete-mismatch', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`, participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{"exit_code": 0})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/complete", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	// Inject a SPIFFE ID for a DIFFERENT node.
	r = withNodeSPIFFE(r, "00000000-0000-0000-0000-000000000099")
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "running" {
		t.Errorf("expected job status unchanged 'running', got %q", status)
	}
}

// ── handleStartedJob SPIFFE binding ──────────────────────────────────────────

func TestHandleStartedJob_SPIFFEMissing_401(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "started_no_spiffe@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'started-no-spiffe', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb)
		 VALUES ($1, $2, 'app_hosting', 'dispatched', 0, 2, 4096)
		 RETURNING id`, participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/started", nil)
	r.SetPathValue("id", jobID)
	w := httptest.NewRecorder()
	ps.handleStartedJob(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "dispatched" {
		t.Errorf("expected job status unchanged 'dispatched', got %q", status)
	}
}

func TestHandleStartedJob_SPIFFEMismatch_403(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "started_mismatch@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'started-mismatch', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb)
		 VALUES ($1, $2, 'app_hosting', 'dispatched', 0, 2, 4096)
		 RETURNING id`, participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/started", nil)
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, "00000000-0000-0000-0000-000000000099")
	w := httptest.NewRecorder()
	ps.handleStartedJob(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "dispatched" {
		t.Errorf("expected job status unchanged 'dispatched', got %q", status)
	}
}

// ── handleTelemetry SPIFFE binding ───────────────────────────────────────────

func TestHandleTelemetry_SPIFFEMissing_401(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "telemetry_no_spiffe@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'telemetry-no-spiffe', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`, participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{
		"node_id":        nodeID,
		"cpu_pct":        50.0,
		"ram_pct":        40.0,
		"bandwidth_mbps": 100,
		"timestamp":      time.Now().UTC(),
	})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/telemetry", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	w := httptest.NewRecorder()
	ps.handleTelemetry(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTelemetry_SPIFFEMismatch_403(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "telemetry_mismatch@test.com")

	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'telemetry-mismatch', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 2, 4096, NOW())
		 RETURNING id`, participantID, nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	b, _ := json.Marshal(map[string]any{
		"node_id":        nodeID,
		"cpu_pct":        50.0,
		"ram_pct":        40.0,
		"bandwidth_mbps": 100,
		"timestamp":      time.Now().UTC(),
	})
	r := httptest.NewRequest(http.MethodPost, "/jobs/"+jobID+"/telemetry", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", jobID)
	r = withNodeSPIFFE(r, "00000000-0000-0000-0000-000000000099")
	w := httptest.NewRecorder()
	ps.handleTelemetry(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// ── handleHeartbeat SPIFFE binding ───────────────────────────────────────────

func TestHandleHeartbeat_SPIFFEMissing_401(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "heartbeat_no_spiffe@test.com")
	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'heartbeat-no-spiffe', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	b, _ := json.Marshal(map[string]any{"node_id": nodeID})
	r := httptest.NewRequest(http.MethodPost, "/nodes/heartbeat", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	// Intentionally do NOT inject SPIFFE — simulates router misconfiguration.
	w := httptest.NewRecorder()
	ps.handleHeartbeat(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var lastHB *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT last_heartbeat_at FROM nodes WHERE id = $1`, nodeID,
	).Scan(&lastHB); err != nil {
		t.Fatalf("query last_heartbeat_at: %v", err)
	}
	if lastHB != nil {
		t.Errorf("expected last_heartbeat_at unchanged NULL, got %v", lastHB)
	}
}

func TestHandleHeartbeat_SPIFFEMismatch_403(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "heartbeat_mismatch@test.com")
	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'heartbeat-mismatch', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	b, _ := json.Marshal(map[string]any{"node_id": nodeID})
	r := httptest.NewRequest(http.MethodPost, "/nodes/heartbeat", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r = withNodeSPIFFE(r, "00000000-0000-0000-0000-000000000099")
	w := httptest.NewRecorder()
	ps.handleHeartbeat(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var lastHB *time.Time
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT last_heartbeat_at FROM nodes WHERE id = $1`, nodeID,
	).Scan(&lastHB); err != nil {
		t.Fatalf("query last_heartbeat_at: %v", err)
	}
	if lastHB != nil {
		t.Errorf("expected last_heartbeat_at unchanged NULL, got %v", lastHB)
	}
}

// ── handleReportPrinters SPIFFE binding ──────────────────────────────────────

func TestHandleReportPrinters_SPIFFEMissing_401(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "printers_no_spiffe@test.com")
	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'printers-no-spiffe', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	b, _ := json.Marshal(map[string]any{"node_id": nodeID, "printers": []any{}})
	r := httptest.NewRequest(http.MethodPost, "/nodes/printers", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	// Intentionally do NOT inject SPIFFE.
	w := httptest.NewRecorder()
	ps.handleReportPrinters(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM node_printers WHERE node_id = $1`, nodeID,
	).Scan(&count); err != nil {
		t.Fatalf("query node_printers: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no printers inserted, got %d", count)
	}
}

func TestHandleReportPrinters_SPIFFEMismatch_403(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "printers_mismatch@test.com")
	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'printers-mismatch', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	b, _ := json.Marshal(map[string]any{"node_id": nodeID, "printers": []any{}})
	r := httptest.NewRequest(http.MethodPost, "/nodes/printers", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r = withNodeSPIFFE(r, "00000000-0000-0000-0000-000000000099")
	w := httptest.NewRecorder()
	ps.handleReportPrinters(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM node_printers WHERE node_id = $1`, nodeID,
	).Scan(&count); err != nil {
		t.Fatalf("query node_printers: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no printers inserted, got %d", count)
	}
}

// ── handleGetJobs SPIFFE binding ─────────────────────────────────────────────

func TestHandleGetJobs_SPIFFEMissing_401(t *testing.T) {
	db := connectAPITestDB(t)
	participantID := seedAPIParticipant(t, db, "getjobs_no_spiffe@test.com")
	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'getjobs-no-spiffe', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/nodes/jobs?node_id="+nodeID, nil)
	// Intentionally do NOT inject SPIFFE.
	w := httptest.NewRecorder()
	handleGetJobs(db)(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetJobs_SPIFFEMismatch_403(t *testing.T) {
	db := connectAPITestDB(t)
	participantID := seedAPIParticipant(t, db, "getjobs_mismatch@test.com")
	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'getjobs-mismatch', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`, participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/nodes/jobs?node_id="+nodeID, nil)
	r = withNodeSPIFFE(r, "00000000-0000-0000-0000-000000000099")
	w := httptest.NewRecorder()
	handleGetJobs(db)(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// ── handleRegisterNodePubkey (A1) ────────────────────────────────────────────

// seedPubkeyNode inserts a node row directly and returns its UUID.
func seedPubkeyNode(t *testing.T, db *store.DB, participantID string) string {
	t.Helper()
	var nodeID string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code, hardware_profile)
		 VALUES ($1, 'pubkey-host', 'online', 'A', 'US', '{}')
		 RETURNING id`,
		participantID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	return nodeID
}

func testEd25519PubkeyB64(t *testing.T) (string, []byte) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pub), []byte(pub)
}

func TestHandleRegisterNodePubkey_Valid(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "pubkey_valid@test.com")
	nodeID := seedPubkeyNode(t, db, pid)
	keyB64, keyRaw := testEd25519PubkeyB64(t)

	w := postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id":    nodeID,
		"public_key": keyB64,
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stored []byte
	var algo string
	var listingSeq, heartbeatSeq int64
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT public_key, algo, last_listing_seq, last_heartbeat_seq
		 FROM node_protocol_keys WHERE node_id = $1`, nodeID,
	).Scan(&stored, &algo, &listingSeq, &heartbeatSeq); err != nil {
		t.Fatalf("query node_protocol_keys: %v", err)
	}
	if !bytes.Equal(stored, keyRaw) {
		t.Error("stored key does not match enrolled key")
	}
	if algo != "ed25519" {
		t.Errorf("algo: got %q, want ed25519", algo)
	}
	if listingSeq != 0 || heartbeatSeq != 0 {
		t.Errorf("seq columns: got %d/%d, want 0/0", listingSeq, heartbeatSeq)
	}
}

func TestHandleRegisterNodePubkey_SPIFFEMissing_401(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "pubkey_401@test.com")
	nodeID := seedPubkeyNode(t, db, pid)
	keyB64, _ := testEd25519PubkeyB64(t)

	w := postJSON(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id":    nodeID,
		"public_key": keyB64,
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var count int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM node_protocol_keys WHERE node_id = $1`, nodeID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no key row after 401, got %d", count)
	}
}

func TestHandleRegisterNodePubkey_SPIFFEMismatch_403(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "pubkey_403@test.com")
	nodeID := seedPubkeyNode(t, db, pid)
	keyB64, _ := testEd25519PubkeyB64(t)

	w := postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id":    nodeID,
		"public_key": keyB64,
	}, "some-other-node")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	var count int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM node_protocol_keys WHERE node_id = $1`, nodeID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no key row after 403, got %d", count)
	}
}

func TestHandleRegisterNodePubkey_FirstWriteWins(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "pubkey_fww@test.com")
	nodeID := seedPubkeyNode(t, db, pid)
	key1B64, key1Raw := testEd25519PubkeyB64(t)
	key2B64, _ := testEd25519PubkeyB64(t)

	// First enrollment wins.
	w := postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id": nodeID, "public_key": key1B64,
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("first enroll: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Re-enrolling the SAME key is an idempotent 200.
	w = postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id": nodeID, "public_key": key1B64,
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("idempotent re-enroll: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// A DIFFERENT key is refused 409 — rotation is an operator action.
	w = postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id": nodeID, "public_key": key2B64,
	}, nodeID)
	if w.Code != http.StatusConflict {
		t.Fatalf("different key: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var stored []byte
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT public_key FROM node_protocol_keys WHERE node_id = $1`, nodeID,
	).Scan(&stored); err != nil {
		t.Fatalf("query stored key: %v", err)
	}
	if !bytes.Equal(stored, key1Raw) {
		t.Error("stored key changed — first-write-wins violated")
	}
}

func TestHandleRegisterNodePubkey_UnknownNode_404(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	keyB64, _ := testEd25519PubkeyB64(t)
	const ghost = "99999999-9999-9999-9999-999999999999"

	w := postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id":    ghost,
		"public_key": keyB64,
	}, ghost)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown node, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRegisterNodePubkey_BadKey_400(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "pubkey_bad@test.com")
	nodeID := seedPubkeyNode(t, db, pid)

	// Not base64.
	w := postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id": nodeID, "public_key": "not-base64!!!",
	}, nodeID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad base64: expected 400, got %d", w.Code)
	}

	// Wrong length.
	w = postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id": nodeID, "public_key": base64.StdEncoding.EncodeToString([]byte("short")),
	}, nodeID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short key: expected 400, got %d", w.Code)
	}

	// Unsupported algo.
	keyB64, _ := testEd25519PubkeyB64(t)
	w = postJSONAs(t, ps.handleRegisterNodePubkey, "/nodes/pubkey", map[string]any{
		"node_id": nodeID, "public_key": keyB64, "algo": "mldsa",
	}, nodeID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported algo: expected 400, got %d", w.Code)
	}
}

// ── handleHeartbeat (B2 advisory load fields) ────────────────────────────────

func TestHandleHeartbeat_RefreshesLoadState(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "hb_load@test.com")
	nodeID := "40000000-0000-0000-0000-000000000010"
	registerTestNode(t, ps, pid, nodeID, nil)

	before := time.Now()
	w := postJSONAs(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":      nodeID,
		"owner_active": true,
		"cpu_pct":      37.5,
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	entry, ok := ps.registry.Get(nodeID)
	if !ok {
		t.Fatal("node missing from registry after heartbeat")
	}
	if !entry.OwnerActive {
		t.Error("OwnerActive not refreshed from heartbeat")
	}
	if entry.CPUUtilPct != 37.5 {
		t.Errorf("CPUUtilPct: got %v, want 37.5", entry.CPUUtilPct)
	}
	if entry.LoadSampledAt.Before(before) {
		t.Errorf("LoadSampledAt not refreshed: %v", entry.LoadSampledAt)
	}
}

func TestHandleHeartbeat_AbsentLoadFieldsDefaultZero(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "hb_load_absent@test.com")
	nodeID := "40000000-0000-0000-0000-000000000011"
	registerTestNode(t, ps, pid, nodeID, nil)

	// Old-agent heartbeat: no owner_active / cpu_pct fields at all.
	w := postJSONAs(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id": nodeID,
	}, nodeID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (backward compatible), got %d: %s", w.Code, w.Body.String())
	}

	entry, ok := ps.registry.Get(nodeID)
	if !ok {
		t.Fatal("node missing from registry after heartbeat")
	}
	if entry.OwnerActive {
		t.Error("OwnerActive should default false for old agents")
	}
	if entry.CPUUtilPct != 0 {
		t.Errorf("CPUUtilPct should default 0 for old agents, got %v", entry.CPUUtilPct)
	}
}
