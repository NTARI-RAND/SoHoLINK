//go:build integration

package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

func connectTestDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := store.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("store.Connect: %v", err)
	}
	t.Cleanup(func() { db.Pool.Close() })
	if err := store.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	// Clean slate: cascade from participants wipes nodes, jobs, disputes,
	// job_metering, node_heartbeat_events, and resource_profiles.
	if _, err := db.Pool.Exec(context.Background(),
		`TRUNCATE participants CASCADE`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return db
}

func TestEligiblePayouts(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	// seed participant (provider role)
	var participantID string
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name, soho_name, stripe_account_id, onboarding_complete)
		 VALUES ('payout_provider@test.com', 'Payout Provider', 'test-node', 'acct_test_payout', TRUE)
		 RETURNING id`,
	).Scan(&participantID)
	if err != nil {
		t.Fatalf("seed participant: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM participants WHERE id = $1`, participantID) //nolint:errcheck
	})

	// seed node owned by that participant
	var nodeID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, 'payout-host', 'online', 'A', 'US',
		  '{"CPUCores":4,"RAMMB":8192}', 100.0) RETURNING id`,
		participantID,
	).Scan(&nodeID)
	if err != nil {
		t.Fatalf("seed node: %v", err)
	}

	// seed consumer participant
	var consumerID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ('payout_consumer@test.com', 'Payout Consumer') RETURNING id`,
	).Scan(&consumerID)
	if err != nil {
		t.Fatalf("seed consumer: %v", err)
	}

	// seed a completed job — completed 25 hours ago (past the 24h hold)
	completedAt := time.Now().Add(-25 * time.Hour)
	var jobID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		  amount_cents, cpu_cores, ram_mb, started_at, completed_at)
		 VALUES ($1, $2, 'app_hosting', 'completed',
		  5000, 2, 4096, $3, $4) RETURNING id`,
		consumerID, nodeID,
		completedAt.Add(-time.Hour), completedAt,
	).Scan(&jobID)
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}

	// seed the metering row — no payout_released_at yet
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO job_metering
		  (job_id, cpu_core_hours, ram_gb_hours, storage_gb_months,
		   consumer_paid_cents, contributor_earned_cents, platform_fee_cents)
		 VALUES ($1, 1.0, 4.0, 0.0, 5000, 3250, 1750)`,
		jobID,
	)
	if err != nil {
		t.Fatalf("seed metering: %v", err)
	}

	candidates, err := store.EligiblePayouts(ctx, db)
	if err != nil {
		t.Fatalf("EligiblePayouts: %v", err)
	}

	var found bool
	for _, c := range candidates {
		if c.JobID == jobID {
			found = true
			if c.ProviderStripeAccountID != "acct_test_payout" {
				t.Errorf("expected stripe account acct_test_payout, got %q", c.ProviderStripeAccountID)
			}
			if c.ContributorEarnedCents != 3250 {
				t.Errorf("expected contributor_earned_cents=3250, got %d", c.ContributorEarnedCents)
			}
		}
	}
	if !found {
		t.Errorf("eligible job %s not returned by EligiblePayouts", jobID)
	}
}
