//go:build integration

package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

func TestEligiblePayouts(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
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

	// Seed: provider with stripe_account_id
	var providerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO providers (email, display_name, stripe_account_id)
		 VALUES ('provider-payouts@test.internal', 'Payouts Test Provider', 'acct_testpayouts')
		 RETURNING id`,
	).Scan(&providerID); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID) //nolint:errcheck
	})

	// Seed: node
	var nodeID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (provider_id, node_class, hostname, country_code, status, hardware_profile)
		 VALUES ($1, 'A', 'payouts-test-node', 'US', 'online', '{"CPUCores":4,"RAMMB":8192}')
		 RETURNING id`,
		providerID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID) //nolint:errcheck
	})

	// Seed: consumer
	var consumerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO consumers (email, display_name)
		 VALUES ('consumer-payouts@test.internal', 'Payouts Test Consumer')
		 RETURNING id`,
	).Scan(&consumerID); err != nil {
		t.Fatalf("insert consumer: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM consumers WHERE id = $1`, consumerID) //nolint:errcheck
	})

	// Seed: completed job older than 24 hours with amount_cents set
	completedAt := time.Now().Add(-25 * time.Hour)
	var jobID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (consumer_id, node_id, workload_type, status, amount_cents, started_at, completed_at)
		 VALUES ($1, $2, 'app_hosting', 'completed', 1000, $3, $4)
		 RETURNING id`,
		consumerID, nodeID,
		completedAt.Add(-1*time.Hour),
		completedAt,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM job_metering WHERE job_id = $1`, jobID) //nolint:errcheck
		db.Pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, jobID)             //nolint:errcheck
	})

	// Seed: metering record
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO job_metering (job_id, cpu_core_hours, ram_gb_hours, consumer_paid_cents, contributor_earned_cents, platform_fee_cents)
		 VALUES ($1, 1.0, 2.0, 1000, 650, 350)`,
		jobID,
	); err != nil {
		t.Fatalf("insert job_metering: %v", err)
	}

	// Call EligiblePayouts and assert our job appears
	candidates, err := store.EligiblePayouts(ctx, db)
	if err != nil {
		t.Fatalf("EligiblePayouts: %v", err)
	}

	found := false
	for _, c := range candidates {
		if c.JobID == jobID {
			found = true
			if c.ProviderStripeAccountID != "acct_testpayouts" {
				t.Errorf("expected stripe account acct_testpayouts, got %s", c.ProviderStripeAccountID)
			}
			if c.ContributorEarnedCents != 650 {
				t.Errorf("expected contributor_earned_cents 650, got %d", c.ContributorEarnedCents)
			}
		}
	}
	if !found {
		t.Errorf("expected job %s in EligiblePayouts results, not found (got %d candidates)", jobID, len(candidates))
	}
}
