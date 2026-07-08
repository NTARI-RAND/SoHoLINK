//go:build integration

package sounding

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// setupSoundingDB connects to TEST_DATABASE_URL, runs migrations, guards that
// the database name contains "test", and truncates the demand-sounding event
// tables so counts are deterministic. Skips if TEST_DATABASE_URL is unset.
// Mirrors the DB-safety discipline in orchestrator_integration_test.go.
func setupSoundingDB(t *testing.T) *store.DB {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
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
		t.Fatalf("refusing to run destructive test: database %q lacks \"test\"", dbName)
	}

	if _, err := db.Pool.Exec(ctx,
		`TRUNCATE operator_job_shapes, operator_placement_rejections, operator_capacity_snapshots`); err != nil {
		t.Fatalf("truncate sounding tables: %v", err)
	}
	return db
}

// TestPGXWriter_RoundTrip proves the pgxWriter INSERTs match the migration-025
// hypertable schemas exactly: column lists, the text→uuid cast on job_id, the
// "" → NULL mapping for rung/wanted_rung, and unit columns. It writes through
// the real async Sink so the drain path is exercised end to end.
func TestPGXWriter_RoundTrip(t *testing.T) {
	db := setupSoundingDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sink := NewSink(ctx, db, Config{BufferSize: 32, BatchSize: 16, FlushInterval: 10 * time.Millisecond})

	jobPlaced := uuid.New().String()
	jobRejected := uuid.New().String()

	sink.RecordJobShape(JobShape{
		OperatorID: "op-int", JobID: jobPlaced, WorkloadType: "batch_compute",
		Intensity: 0.5, DurationEst: 0, CPU: 4, MemMB: 8192, DiskMB: 51200,
		Footprint: 0.5, Placed: true, Rung: "congestus",
	})
	sink.RecordJobShape(JobShape{
		OperatorID: "op-int", JobID: jobRejected, WorkloadType: "batch_compute",
		CPU: 64, MemMB: 8192, DiskMB: 1024, Footprint: 2.0, Placed: false, Rung: "", // rung NULL
	})
	sink.RecordRejection(Rejection{
		OperatorID: "op-int", JobID: jobRejected, WorkloadType: "batch_compute",
		Reason: ReasonTooBig, Footprint: 2.0, WantedRung: "storm",
	})
	sink.RecordCapacity(CapacitySnapshot{
		OperatorID: "op-int", NodeClass: "A", WorkloadType: WorkloadCompute,
		NodesAvailable: 3, VCPUs: 24, MemMB: 49152, DiskMB: 614400, PrintQPS: 0,
	})

	// Poll until all four rows land (async drain).
	deadline := time.Now().Add(5 * time.Second)
	for {
		shapes, rejs, caps := soundingCounts(t, db)
		if shapes == 2 && rejs == 1 && caps == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rows not observed: shapes=%d rejs=%d caps=%d", shapes, rejs, caps)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify the placed shape's rung is stored and the unplaced shape's rung is NULL.
	var placedRung *string
	if err := db.Pool.QueryRow(ctx,
		`SELECT rung FROM operator_job_shapes WHERE job_id = $1::uuid`, jobPlaced,
	).Scan(&placedRung); err != nil {
		t.Fatalf("query placed rung: %v", err)
	}
	if placedRung == nil || *placedRung != "congestus" {
		t.Errorf("placed rung: got %v, want congestus", placedRung)
	}

	var unplacedRung *string
	if err := db.Pool.QueryRow(ctx,
		`SELECT rung FROM operator_job_shapes WHERE job_id = $1::uuid`, jobRejected,
	).Scan(&unplacedRung); err != nil {
		t.Fatalf("query unplaced rung: %v", err)
	}
	if unplacedRung != nil {
		t.Errorf("unplaced rung: got %v, want NULL", *unplacedRung)
	}

	// Verify the rejection reason + wanted_rung + operator_id landed correctly.
	var reason, operatorID string
	var wanted *string
	if err := db.Pool.QueryRow(ctx,
		`SELECT reason, wanted_rung, operator_id FROM operator_placement_rejections WHERE job_id = $1::uuid`, jobRejected,
	).Scan(&reason, &wanted, &operatorID); err != nil {
		t.Fatalf("query rejection: %v", err)
	}
	if reason != ReasonTooBig || wanted == nil || *wanted != "storm" || operatorID != "op-int" {
		t.Errorf("rejection: got reason=%q wanted=%v operator=%q", reason, wanted, operatorID)
	}
}

func soundingCounts(t *testing.T, db *store.DB) (shapes, rejs, caps int) {
	t.Helper()
	ctx := context.Background()
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM operator_job_shapes`).Scan(&shapes); err != nil {
		t.Fatalf("count shapes: %v", err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM operator_placement_rejections`).Scan(&rejs); err != nil {
		t.Fatalf("count rejs: %v", err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM operator_capacity_snapshots`).Scan(&caps); err != nil {
		t.Fatalf("count caps: %v", err)
	}
	return shapes, rejs, caps
}
