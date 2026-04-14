package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// ── test helpers ─────────────────────────────────────────────────────────────

func connectAPITestDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	db, err := store.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("store.Connect: %v", err)
	}
	t.Cleanup(func() { db.Pool.Close() })
	if err := store.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
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

func postJSON(t *testing.T, handler http.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

// ── handleRegisterNode ───────────────────────────────────────────────────────

func TestHandleRegisterNode_Valid(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "reg_node@test.com")

	w := postJSON(t, ps.handleRegisterNode, "/nodes/register", map[string]any{
		"node_id":      "test-node-001",
		"provider_id":  participantID,
		"hostname":     "test-host-001",
		"node_class":   "A",
		"country_code": "US",
		"hardware_profile": map[string]any{
			"CPUCores": 4,
			"RAMMB":    8192,
		},
	})

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
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)

	w := postJSON(t, ps.handleRegisterNode, "/nodes/register", map[string]any{
		"node_id": "",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRegisterNode_Upsert(t *testing.T) {
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "upsert_node@test.com")

	payload := map[string]any{
		"node_id":      "upsert-node-001",
		"provider_id":  participantID,
		"hostname":     "upsert-host",
		"node_class":   "B",
		"country_code": "CA",
		"hardware_profile": map[string]any{"CPUCores": 2, "RAMMB": 4096},
	}

	// first registration
	w1 := postJSON(t, ps.handleRegisterNode, "/nodes/register", payload)
	if w1.Code != http.StatusOK {
		t.Fatalf("first register: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// second registration — same node_id, should upsert not error
	w2 := postJSON(t, ps.handleRegisterNode, "/nodes/register", payload)
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
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	participantID := seedAPIParticipant(t, db, "hb_valid@test.com")

	// register first so node is in memory registry
	postJSON(t, ps.handleRegisterNode, "/nodes/register", map[string]any{
		"node_id":      "hb-node-001",
		"provider_id":  participantID,
		"hostname":     "hb-host-001",
		"node_class":   "A",
		"country_code": "US",
		"hardware_profile": map[string]any{"CPUCores": 2, "RAMMB": 4096},
	})

	w := postJSON(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id": "hb-node-001",
	})

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

	w := postJSON(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id": "ghost-node-999",
	})

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
	w := httptest.NewRecorder()
	ps.handleCompleteJob(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}
