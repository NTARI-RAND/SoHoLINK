//go:build integration

package orchestrator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/scheduler"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

// orchFixture holds shared DB state for orchestrator integration tests.
type orchFixture struct {
	db          *store.DB
	orch        *orchestrator.Orchestrator
	registry    *orchestrator.NodeRegistry
	tokenSecret []byte
	providerID  string
	consumerID  string
	nodeID      string
}

// setupOrchFixture connects to Postgres, runs migrations, truncates all tables,
// seeds a provider + consumer participant and one node, and registers the node
// in the in-memory registry. Skips the test if TEST_DATABASE_URL is not set.
// Not safe for parallel use across tests in the same process: TRUNCATE happens
// unconditionally at the start of every fixture build.
func setupOrchFixture(t *testing.T, allowlistPath string, printConfirmEnabled bool) *orchFixture {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test (see docs/test-database.md)")
	}

	ctx := context.Background()

	db, err := store.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("store.Connect: %v", err)
	}
	t.Cleanup(func() { db.Pool.Close() })

	if err := store.RunMigrations(db); err != nil {
		t.Fatalf("store.RunMigrations: %v", err)
	}

	var dbName string
	if err := db.Pool.QueryRow(ctx, `SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	if !strings.Contains(dbName, "test") {
		t.Fatalf("refusing to run destructive integration test: connected database %q does not contain \"test\" in its name; set TEST_DATABASE_URL to a dedicated test database", dbName)
	}

	if _, err := db.Pool.Exec(ctx, `
		TRUNCATE TABLE job_metering, disputes, jobs, node_heartbeat_events,
		               resource_profiles, nodes, participants
		RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}

	var providerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ('provider-orch@test.internal', 'Orch Test Provider')
		 RETURNING id`,
	).Scan(&providerID); err != nil {
		t.Fatalf("insert provider: %v", err)
	}

	var consumerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ('consumer-orch@test.internal', 'Orch Test Consumer')
		 RETURNING id`,
	).Scan(&consumerID); err != nil {
		t.Fatalf("insert consumer: %v", err)
	}

	var nodeID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, node_class, hostname, country_code, status)
		 VALUES ($1, 'A', 'orch-test-node', 'US', 'online')
		 RETURNING id`,
		providerID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("insert node: %v", err)
	}

	registry := orchestrator.NewNodeRegistry()
	registry.Register(orchestrator.NodeEntry{
		NodeID:        nodeID,
		ParticipantID: providerID,
		NodeClass:     "A",
		CountryCode:   "US",
		Status:        "online",
		LastHeartbeat: time.Now(),
		HardwareProfile: orchestrator.HardwareProfile{
			CPUCores:  8,
			RAMMB:     16384,
			StorageGB: 200,
		},
	})

	tokenSecret := []byte("orch-integration-test-secret")
	orch := orchestrator.New(db, registry, tokenSecret, scheduler.Schedule, allowlistPath, printConfirmEnabled, 4*time.Hour)

	return &orchFixture{
		db:          db,
		orch:        orch,
		registry:    registry,
		tokenSecret: tokenSecret,
		providerID:  providerID,
		consumerID:  consumerID,
		nodeID:      nodeID,
	}
}

// writeOrchAllowlist writes a temp allowlist with entries for both compute and
// print_traditional workloads.
func writeOrchAllowlist(t *testing.T) string {
	t.Helper()
	al := agent.Allowlist{
		Version:  1,
		IssuedAt: time.Now(),
		Entries: []agent.AllowlistEntry{
			{
				Name:   "soholink/compute-worker",
				Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Type:   agent.WorkloadCompute,
				Egress: agent.EgressOutbound,
			},
			{
				Name:   "soholink/print-worker",
				Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Type:   agent.WorkloadPrintTraditional,
				Egress: agent.EgressOutbound,
			},
		},
	}
	data, err := json.Marshal(al)
	if err != nil {
		t.Fatalf("marshal test allowlist: %v", err)
	}
	path := filepath.Join(t.TempDir(), "allowlist.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write test allowlist: %v", err)
	}
	return path
}

const (
	orchComputeImage = "soholink/compute-worker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	orchPrintImage   = "soholink/print-worker@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// TestSubmitJob_PrintConfirm_FlagOn_PrinterResolves verifies that SubmitJob
// writes awaiting_confirmation with printer_id, spec_hash, and
// confirmation_deadline when the flag is on and the node has an enabled printer.
func TestSubmitJob_PrintConfirm_FlagOn_PrinterResolves(t *testing.T) {
	ctx := context.Background()
	f := setupOrchFixture(t, writeOrchAllowlist(t), true)

	const printerID = "usb://vendor/printer/001"
	if _, err := f.db.Pool.Exec(ctx,
		`INSERT INTO node_printers (node_id, printer_id, printer_name, enabled) VALUES ($1, $2, $3, TRUE)`,
		f.nodeID, printerID, "Test Printer",
	); err != nil {
		t.Fatalf("insert node_printers: %v", err)
	}
	if err := f.registry.UpdateOptOut(f.nodeID, orchestrator.NodeOptOutState{HasEnabledPrinter: true}); err != nil {
		t.Fatalf("UpdateOptOut: %v", err)
	}

	resp, err := f.orch.SubmitJob(ctx, orchestrator.SubmitJobRequest{
		ConsumerID:     f.consumerID,
		WorkloadType:   types.MarketplacePrintTraditional,
		ContainerImage: orchPrintImage,
		CPUCores:       2,
		RAMMB:          4096,
	})
	if err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}
	if resp.JobID == "" {
		t.Fatal("SubmitJob: JobID is empty")
	}

	var status, storedPrinterID string
	var specHash []byte
	var deadline time.Time
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status, COALESCE(printer_id, ''), spec_hash, confirmation_deadline
		 FROM jobs WHERE id = $1`,
		resp.JobID,
	).Scan(&status, &storedPrinterID, &specHash, &deadline); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "awaiting_confirmation" {
		t.Errorf("status: got %q, want %q", status, "awaiting_confirmation")
	}
	if storedPrinterID != printerID {
		t.Errorf("printer_id: got %q, want %q", storedPrinterID, printerID)
	}
	if len(specHash) != 32 {
		t.Errorf("spec_hash: got %d bytes, want 32 (SHA-256)", len(specHash))
	}
	if deadline.IsZero() || deadline.Before(time.Now()) {
		t.Errorf("confirmation_deadline is zero or in the past: %v", deadline)
	}
}

// TestSubmitJob_PrintConfirm_FlagOn_NoEnabledPrinter verifies that SubmitJob
// returns a registry/DB drift error when the registry reports an enabled
// printer but node_printers has no matching row.
func TestSubmitJob_PrintConfirm_FlagOn_NoEnabledPrinter(t *testing.T) {
	ctx := context.Background()
	f := setupOrchFixture(t, writeOrchAllowlist(t), true)

	// Set HasEnabledPrinter in registry so FindMatch passes, but insert no
	// node_printers row — simulates the registry/DB skew window.
	if err := f.registry.UpdateOptOut(f.nodeID, orchestrator.NodeOptOutState{HasEnabledPrinter: true}); err != nil {
		t.Fatalf("UpdateOptOut: %v", err)
	}

	_, err := f.orch.SubmitJob(ctx, orchestrator.SubmitJobRequest{
		ConsumerID:     f.consumerID,
		WorkloadType:   types.MarketplacePrintTraditional,
		ContainerImage: orchPrintImage,
		CPUCores:       2,
		RAMMB:          4096,
	})
	if err == nil {
		t.Fatal("expected error for registry/DB drift, got nil")
	}
	if !strings.Contains(err.Error(), "registry/DB drift") {
		t.Errorf("expected 'registry/DB drift' in error, got: %v", err)
	}
}

// TestSubmitJob_PrintConfirm_FlagOn_ComputeJob verifies that a compute job
// writes status='scheduled' even when PRINT_CONFIRMATION_ENABLED is set.
func TestSubmitJob_PrintConfirm_FlagOn_ComputeJob(t *testing.T) {
	ctx := context.Background()
	f := setupOrchFixture(t, writeOrchAllowlist(t), true)

	resp, err := f.orch.SubmitJob(ctx, orchestrator.SubmitJobRequest{
		ConsumerID:     f.consumerID,
		WorkloadType:   types.MarketplaceBatchCompute,
		ContainerImage: orchComputeImage,
		CPUCores:       2,
		RAMMB:          4096,
	})
	if err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}

	var status string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, resp.JobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "scheduled" {
		t.Errorf("status: got %q, want %q", status, "scheduled")
	}
}

// TestSubmitJob_PrintConfirm_FlagOff_PrintJob verifies that a print job writes
// status='scheduled' when PRINT_CONFIRMATION_ENABLED is false.
func TestSubmitJob_PrintConfirm_FlagOff_PrintJob(t *testing.T) {
	ctx := context.Background()
	f := setupOrchFixture(t, writeOrchAllowlist(t), false)

	// FindMatch's opt-out filter applies regardless of the dispatcher flag —
	// the flag only gates the post-INSERT status transition. Without
	// HasEnabledPrinter: true the only candidate is filtered out before any
	// DB write happens. No node_printers insert is needed: the flag-off path
	// never queries that table.
	if err := f.registry.UpdateOptOut(f.nodeID, orchestrator.NodeOptOutState{HasEnabledPrinter: true}); err != nil {
		t.Fatalf("UpdateOptOut: %v", err)
	}

	resp, err := f.orch.SubmitJob(ctx, orchestrator.SubmitJobRequest{
		ConsumerID:     f.consumerID,
		WorkloadType:   types.MarketplacePrintTraditional,
		ContainerImage: orchPrintImage,
		CPUCores:       2,
		RAMMB:          4096,
	})
	if err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}

	var status string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, resp.JobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "scheduled" {
		t.Errorf("status: got %q, want %q", status, "scheduled")
	}
}

// TestRerouteDeclinedJob_Success verifies that RerouteDeclinedJob moves a
// declined job to awaiting_confirmation on a different node when candidates exist.
func TestRerouteDeclinedJob_Success(t *testing.T) {
	ctx := context.Background()
	f := setupOrchFixture(t, writeOrchAllowlist(t), true)

	// Seed an enabled printer on node A (the original, now-declined node).
	const printerIDA = "usb://vendor/printerA/001"
	if _, err := f.db.Pool.Exec(ctx,
		`INSERT INTO node_printers (node_id, printer_id, printer_name, enabled) VALUES ($1, $2, $3, TRUE)`,
		f.nodeID, printerIDA, "Test Printer A",
	); err != nil {
		t.Fatalf("insert node_printers node A: %v", err)
	}
	if err := f.registry.UpdateOptOut(f.nodeID, orchestrator.NodeOptOutState{HasEnabledPrinter: true}); err != nil {
		t.Fatalf("UpdateOptOut node A: %v", err)
	}

	// Register a second node (B) in DB and registry, also with an enabled printer.
	var nodeBID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, node_class, hostname, country_code, status)
		 VALUES ($1, 'A', 'orch-test-node-b', 'US', 'online')
		 RETURNING id`,
		f.providerID,
	).Scan(&nodeBID); err != nil {
		t.Fatalf("insert node B: %v", err)
	}
	f.registry.Register(orchestrator.NodeEntry{
		NodeID:        nodeBID,
		ParticipantID: f.providerID,
		NodeClass:     "A",
		CountryCode:   "US",
		Status:        "online",
		LastHeartbeat: time.Now(),
		HardwareProfile: orchestrator.HardwareProfile{
			CPUCores:  8,
			RAMMB:     16384,
			StorageGB: 200,
		},
	})
	const printerIDB = "usb://vendor/printerB/001"
	if _, err := f.db.Pool.Exec(ctx,
		`INSERT INTO node_printers (node_id, printer_id, printer_name, enabled) VALUES ($1, $2, $3, TRUE)`,
		nodeBID, printerIDB, "Test Printer B",
	); err != nil {
		t.Fatalf("insert node_printers node B: %v", err)
	}
	if err := f.registry.UpdateOptOut(nodeBID, orchestrator.NodeOptOutState{HasEnabledPrinter: true}); err != nil {
		t.Fatalf("UpdateOptOut node B: %v", err)
	}

	// Seed a job in 'declined' status originally assigned to node A.
	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image,
		                   printer_id, spec_hash, confirmation_deadline)
		 VALUES ($1, $2, 'print_traditional'::workload_type, 'declined'::job_status,
		         2, 4096, 0, FALSE, $3,
		         $4, $5, NOW() + INTERVAL '4 hours')
		 RETURNING id`,
		f.consumerID, f.nodeID, orchPrintImage, printerIDA, make([]byte, 32),
	).Scan(&jobID); err != nil {
		t.Fatalf("insert declined job: %v", err)
	}

	// Record node A as having declined this job.
	if _, err := f.db.Pool.Exec(ctx,
		`INSERT INTO job_node_declines (job_id, node_id) VALUES ($1, $2)`,
		jobID, f.nodeID,
	); err != nil {
		t.Fatalf("insert job_node_declines: %v", err)
	}

	if err := f.orch.RerouteDeclinedJob(ctx, jobID); err != nil {
		t.Fatalf("RerouteDeclinedJob: %v", err)
	}

	var status, newNodeID string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status, node_id FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &newNodeID); err != nil {
		t.Fatalf("query rerouted job: %v", err)
	}
	if status != "awaiting_confirmation" {
		t.Errorf("status: got %q, want %q", status, "awaiting_confirmation")
	}
	if newNodeID != nodeBID {
		t.Errorf("node_id: got %q, want %q (node B)", newNodeID, nodeBID)
	}
}

// TestRerouteDeclinedJob_NoCandidates verifies that RerouteDeclinedJob marks a
// job 'failed' when every known node has already declined it.
func TestRerouteDeclinedJob_NoCandidates(t *testing.T) {
	ctx := context.Background()
	f := setupOrchFixture(t, writeOrchAllowlist(t), true)

	// Seed a declined job — spec_hash nullable, omitted here.
	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image)
		 VALUES ($1, $2, 'print_traditional'::workload_type, 'declined'::job_status,
		         2, 4096, 0, FALSE, $3)
		 RETURNING id`,
		f.consumerID, f.nodeID, orchPrintImage,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert declined job: %v", err)
	}

	// The only node has declined — no candidates will remain after exclusion.
	if _, err := f.db.Pool.Exec(ctx,
		`INSERT INTO job_node_declines (job_id, node_id) VALUES ($1, $2)`,
		jobID, f.nodeID,
	); err != nil {
		t.Fatalf("insert job_node_declines: %v", err)
	}

	if err := f.orch.RerouteDeclinedJob(ctx, jobID); err != nil {
		t.Fatalf("RerouteDeclinedJob: %v", err)
	}

	var status string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "failed" {
		t.Errorf("status: got %q, want %q", status, "failed")
	}
}

// TestExpireConfirmation_DeadlinePast_FlipsToDeclined verifies that
// ExpireConfirmation flips an awaiting_confirmation job to declined when its
// deadline has passed, sets declined_at, and returns flipped=true.
func TestExpireConfirmation_DeadlinePast_FlipsToDeclined(t *testing.T) {
	allowlistPath := writeOrchAllowlist(t)
	f := setupOrchFixture(t, allowlistPath, false)
	ctx := context.Background()

	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image,
		                   printer_id, spec_hash, confirmation_deadline)
		 VALUES ($1, $2, 'print_traditional'::workload_type,
		         'awaiting_confirmation'::job_status,
		         2, 4096, 0, FALSE, $3,
		         'expire-test-printer', $4, NOW() - INTERVAL '1 second')
		 RETURNING id`,
		f.consumerID, f.nodeID, orchPrintImage, make([]byte, 32),
	).Scan(&jobID); err != nil {
		t.Fatalf("insert awaiting_confirmation job: %v", err)
	}

	flipped, err := f.orch.ExpireConfirmation(ctx, jobID)
	if err != nil {
		t.Fatalf("ExpireConfirmation: %v", err)
	}
	if !flipped {
		t.Fatalf("ExpireConfirmation flipped: got false, want true")
	}

	var status string
	var declinedAt *time.Time
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status, declined_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &declinedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "declined" {
		t.Errorf("status: got %q, want %q", status, "declined")
	}
	if declinedAt == nil {
		t.Errorf("declined_at: got nil, want non-nil")
	}
}

// TestExpireConfirmation_DeadlineFuture_NoChange verifies that
// ExpireConfirmation does not flip a job whose deadline is still in the
// future. Returns flipped=false with no error and leaves status unchanged.
func TestExpireConfirmation_DeadlineFuture_NoChange(t *testing.T) {
	allowlistPath := writeOrchAllowlist(t)
	f := setupOrchFixture(t, allowlistPath, false)
	ctx := context.Background()

	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image,
		                   printer_id, spec_hash, confirmation_deadline)
		 VALUES ($1, $2, 'print_traditional'::workload_type,
		         'awaiting_confirmation'::job_status,
		         2, 4096, 0, FALSE, $3,
		         'expire-test-printer', $4, NOW() + INTERVAL '4 hours')
		 RETURNING id`,
		f.consumerID, f.nodeID, orchPrintImage, make([]byte, 32),
	).Scan(&jobID); err != nil {
		t.Fatalf("insert awaiting_confirmation job: %v", err)
	}

	flipped, err := f.orch.ExpireConfirmation(ctx, jobID)
	if err != nil {
		t.Fatalf("ExpireConfirmation: %v", err)
	}
	if flipped {
		t.Fatalf("ExpireConfirmation flipped: got true, want false")
	}

	var status string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "awaiting_confirmation" {
		t.Errorf("status: got %q, want %q", status, "awaiting_confirmation")
	}
}

// TestExpireDispatched_FlipsBackToScheduled verifies that ExpireDispatched
// reverts a dispatched job to scheduled when updated_at is older than 2 minutes,
// and returns flipped=true.
func TestExpireDispatched_FlipsBackToScheduled(t *testing.T) {
	allowlistPath := writeOrchAllowlist(t)
	f := setupOrchFixture(t, allowlistPath, false)
	ctx := context.Background()

	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image,
		                   updated_at)
		 VALUES ($1, $2, 'app_hosting'::workload_type,
		         'dispatched'::job_status,
		         2, 4096, 0, FALSE, $3,
		         NOW() - INTERVAL '3 minutes')
		 RETURNING id`,
		f.consumerID, f.nodeID, orchComputeImage,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert dispatched job: %v", err)
	}

	flipped, err := f.orch.ExpireDispatched(ctx, jobID)
	if err != nil {
		t.Fatalf("ExpireDispatched: %v", err)
	}
	if !flipped {
		t.Fatalf("ExpireDispatched flipped: got false, want true")
	}

	var status string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "scheduled" {
		t.Errorf("status: got %q, want %q", status, "scheduled")
	}
}

// TestExpireDispatched_RaceLoss verifies that ExpireDispatched does not flip a
// dispatched job whose updated_at is within the 2-minute window (simulating
// a fresh dispatch where the agent has not yet timed out). Returns flipped=false.
func TestExpireDispatched_RaceLoss(t *testing.T) {
	allowlistPath := writeOrchAllowlist(t)
	f := setupOrchFixture(t, allowlistPath, false)
	ctx := context.Background()

	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image)
		 VALUES ($1, $2, 'app_hosting'::workload_type,
		         'dispatched'::job_status,
		         2, 4096, 0, FALSE, $3)
		 RETURNING id`,
		f.consumerID, f.nodeID, orchComputeImage,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert dispatched job: %v", err)
	}

	flipped, err := f.orch.ExpireDispatched(ctx, jobID)
	if err != nil {
		t.Fatalf("ExpireDispatched: %v", err)
	}
	if flipped {
		t.Fatalf("ExpireDispatched flipped: got true, want false")
	}

	var status string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "dispatched" {
		t.Errorf("status: got %q, want unchanged %q", status, "dispatched")
	}
}

// TestExpirePickedUp_AdvancesAfter7Days verifies that ExpirePickedUp advances a
// picked_up job to delivered when picked_up_at is older than 7 days, and that
// no job_metering row is created (C9 deferral guard).
func TestExpirePickedUp_AdvancesAfter7Days(t *testing.T) {
	allowlistPath := writeOrchAllowlist(t)
	f := setupOrchFixture(t, allowlistPath, false)
	ctx := context.Background()

	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image,
		                   picked_up_at)
		 VALUES ($1, $2, 'print_traditional'::workload_type,
		         'picked_up'::job_status,
		         2, 4096, 0, FALSE, $3,
		         NOW() - INTERVAL '8 days')
		 RETURNING id`,
		f.consumerID, f.nodeID, orchPrintImage,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert picked_up job: %v", err)
	}

	flipped, err := f.orch.ExpirePickedUp(ctx, jobID)
	if err != nil {
		t.Fatalf("ExpirePickedUp: %v", err)
	}
	if !flipped {
		t.Fatalf("ExpirePickedUp flipped: got false, want true")
	}

	var status string
	var deliveredAt, completedAt *time.Time
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status, delivered_at, completed_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &deliveredAt, &completedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "delivered" {
		t.Errorf("status: got %q, want %q", status, "delivered")
	}
	if deliveredAt == nil {
		t.Errorf("delivered_at: got nil, want non-nil")
	}
	if completedAt == nil {
		t.Errorf("completed_at: got nil, want non-nil")
	}

	var meterCount int
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&meterCount); err != nil {
		t.Fatalf("query job_metering: %v", err)
	}
	if meterCount != 0 {
		t.Errorf("job_metering count: got %d, want 0 (C9 metering deferred)", meterCount)
	}
}

// TestExpirePickedUp_DoesNotAdvanceBefore7Days verifies that ExpirePickedUp
// does not advance a picked_up job whose picked_up_at is within the 7-day
// window. Returns flipped=false with status unchanged.
func TestExpirePickedUp_DoesNotAdvanceBefore7Days(t *testing.T) {
	allowlistPath := writeOrchAllowlist(t)
	f := setupOrchFixture(t, allowlistPath, false)
	ctx := context.Background()

	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image,
		                   picked_up_at)
		 VALUES ($1, $2, 'print_traditional'::workload_type,
		         'picked_up'::job_status,
		         2, 4096, 0, FALSE, $3,
		         NOW() - INTERVAL '1 day')
		 RETURNING id`,
		f.consumerID, f.nodeID, orchPrintImage,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert picked_up job: %v", err)
	}

	flipped, err := f.orch.ExpirePickedUp(ctx, jobID)
	if err != nil {
		t.Fatalf("ExpirePickedUp: %v", err)
	}
	if flipped {
		t.Fatalf("ExpirePickedUp flipped: got true, want false")
	}

	var status string
	var deliveredAt *time.Time
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status, delivered_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &deliveredAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "picked_up" {
		t.Errorf("status: got %q, want unchanged %q", status, "picked_up")
	}
	if deliveredAt != nil {
		t.Errorf("delivered_at: got non-nil, want nil")
	}
}

// TestExpirePickedUp_OnlyAffectsPickedUpStatus verifies that ExpirePickedUp
// does not touch a job that is in awaiting_pickup status (not picked_up), even
// when the timestamp is older than 7 days.
func TestExpirePickedUp_OnlyAffectsPickedUpStatus(t *testing.T) {
	allowlistPath := writeOrchAllowlist(t)
	f := setupOrchFixture(t, allowlistPath, false)
	ctx := context.Background()

	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image,
		                   awaiting_pickup_at, updated_at)
		 VALUES ($1, $2, 'print_traditional'::workload_type,
		         'awaiting_pickup'::job_status,
		         2, 4096, 0, FALSE, $3,
		         NOW() - INTERVAL '8 days', NOW() - INTERVAL '8 days')
		 RETURNING id`,
		f.consumerID, f.nodeID, orchPrintImage,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert awaiting_pickup job: %v", err)
	}

	flipped, err := f.orch.ExpirePickedUp(ctx, jobID)
	if err != nil {
		t.Fatalf("ExpirePickedUp: %v", err)
	}
	if flipped {
		t.Fatalf("ExpirePickedUp flipped: got true, want false (awaiting_pickup is not picked_up)")
	}

	var status string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, jobID,
	).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "awaiting_pickup" {
		t.Errorf("status: got %q, want unchanged %q", status, "awaiting_pickup")
	}
}
