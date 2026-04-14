//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// seedMeteringJob creates the minimum rows needed to call ComputeMetering:
// a participant, a node with a default resource profile, and a completed job.
// Returns jobID.
func seedMeteringJob(t *testing.T, db *store.DB, email string, durationHours float64) string {
	t.Helper()
	ctx := context.Background()

	var participantID string
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name, soho_name)
		 VALUES ($1, $1, 'meter-node') RETURNING id`, email,
	).Scan(&participantID)
	if err != nil {
		t.Fatalf("seedMeteringJob participant: %v", err)
	}

	var nodeID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, $2, 'online', 'A', 'US', '{"CPUCores":4,"RAMMB":8192}', 100.0)
		 RETURNING id`,
		participantID, "meter-host-"+email,
	).Scan(&nodeID)
	if err != nil {
		t.Fatalf("seedMeteringJob node: %v", err)
	}

	// default resource profile: 100% CPU, 100% RAM, 50 GB storage
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO resource_profiles
		  (node_id, name, is_default, cpu_enabled, gpu_pct, ram_pct,
		   storage_gb, bandwidth_mbps, price_multiplier)
		 VALUES ($1, 'default', TRUE, TRUE, 0, 100, 50, 100, 1.0)`,
		nodeID,
	)
	if err != nil {
		t.Fatalf("seedMeteringJob resource_profile: %v", err)
	}

	completedAt := time.Now()
	startedAt := completedAt.Add(-time.Duration(float64(time.Hour) * durationHours))

	var consumerID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ($1, $1) RETURNING id`, "consumer+"+email,
	).Scan(&consumerID)
	if err != nil {
		t.Fatalf("seedMeteringJob consumer: %v", err)
	}

	var jobID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at, completed_at)
		 VALUES ($1, $2, 'app_hosting', 'completed', 0, 2, 4096, $3, $4)
		 RETURNING id`,
		consumerID, nodeID, startedAt, completedAt,
	).Scan(&jobID)
	if err != nil {
		t.Fatalf("seedMeteringJob job: %v", err)
	}
	return jobID
}

func TestComputeMetering_Basic(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	jobID := seedMeteringJob(t, db, "meter_basic@test.com", 2.0)

	if err := store.ComputeMetering(ctx, db, jobID); err != nil {
		t.Fatalf("ComputeMetering: %v", err)
	}

	var consumerPaid, contributorEarned int64
	err := db.Pool.QueryRow(ctx,
		`SELECT consumer_paid_cents, contributor_earned_cents
		 FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&consumerPaid, &contributorEarned)
	if err != nil {
		t.Fatalf("query job_metering: %v", err)
	}
	if consumerPaid <= 0 {
		t.Errorf("expected consumer_paid_cents > 0, got %d", consumerPaid)
	}
	if contributorEarned <= 0 {
		t.Errorf("expected contributor_earned_cents > 0, got %d", contributorEarned)
	}
	if contributorEarned >= consumerPaid {
		t.Errorf("contributor_earned (%d) should be less than consumer_paid (%d)", contributorEarned, consumerPaid)
	}
}

func TestComputeMetering_Idempotent(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	jobID := seedMeteringJob(t, db, "meter_idem@test.com", 1.0)

	if err := store.ComputeMetering(ctx, db, jobID); err != nil {
		t.Fatalf("first ComputeMetering: %v", err)
	}
	if err := store.ComputeMetering(ctx, db, jobID); err != nil {
		t.Fatalf("second ComputeMetering: %v", err)
	}

	var count int
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 job_metering row after two calls, got %d", count)
	}
}

func TestComputeMetering_PendingJob(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	// seed a participant and node just enough to get a job with no completed_at
	var participantID string
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ('meter_pending@test.com', 'Meter Pending') RETURNING id`,
	).Scan(&participantID)
	if err != nil {
		t.Fatalf("seed participant: %v", err)
	}

	var nodeID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'pending-host', 'online', 'A', 'US', '{"CPUCores":2,"RAMMB":4096}', 100.0)
		 RETURNING id`,
		participantID,
	).Scan(&nodeID)
	if err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var jobID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb)
		 VALUES ($1, $2, 'app_hosting', 'running', 0, 1, 2048)
		 RETURNING id`,
		participantID, nodeID,
	).Scan(&jobID)
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}

	if err := store.ComputeMetering(ctx, db, jobID); err != nil {
		t.Fatalf("ComputeMetering on pending job: %v", err)
	}

	var count int
	err = db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM job_metering WHERE job_id = $1`, jobID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no job_metering row for pending job, got %d", count)
	}
}
