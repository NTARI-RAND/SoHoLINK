package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
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

	w := postJSON(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id": "30000000-0000-0000-0000-000000000003",
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

// ── handleHeartbeat (opt-out + printer-hash) ─────────────────────────────────

func TestHandleHeartbeat_VersionMatch_NoOptOut(t *testing.T) {
	t.Setenv("CONTROL_PLANE_REGISTER_SECRET", "test-secret")
	db := connectAPITestDB(t)
	ps := newAPIServer(t, db)
	pid := seedAPIParticipant(t, db, "hb_match@test.com")
	nodeID := "40000000-0000-0000-0000-000000000001"
	registerTestNode(t, ps, pid, nodeID, nil)

	w := postJSON(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":         nodeID,
		"opt_out_version": 0,
		"printer_hash":    "",
	})
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

	w := postJSON(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":         nodeID,
		"opt_out_version": 0,
		"printer_hash":    "",
	})
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

	w := postJSON(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":         nodeID,
		"opt_out_version": 0,
		"printer_hash":    testPrinterHash("printer-A", "printer-B"),
	})
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

	w := postJSON(t, ps.handleHeartbeat, "/nodes/heartbeat", map[string]any{
		"node_id":         nodeID,
		"opt_out_version": 0,
		"printer_hash":    testPrinterHash("printer-A", "printer-NEW"),
	})
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

	w := postJSON(t, ps.handleReportPrinters, "/nodes/printers", map[string]any{
		"node_id": nodeID,
		"printers": []map[string]string{
			{"id": "p1", "name": "Updated Name"},
			{"id": "p2", "name": "New Printer"},
		},
	})
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
