//go:build integration

package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/scheduler"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

func TestPhase1EndToEnd(t *testing.T) {
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

	// TRUNCATE guard — removes any rows left by a previous interrupted run.
	// Order respects FK constraints: jobs → nodes/participants, disputes → jobs.
	if _, err := db.Pool.Exec(ctx, `
		TRUNCATE TABLE job_metering, disputes, jobs, node_heartbeat_events,
		               resource_profiles, nodes, participants
		RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}

	// Seed DB fixtures. Cleanups run LIFO so jobs are deleted before the
	// consumers and nodes they reference.
	var providerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ('provider-phase1@test.internal', 'Phase 1 Test Provider')
		 RETURNING id`,
	).Scan(&providerID); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM participants WHERE id = $1`, providerID) //nolint:errcheck
	})

	var consumerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ('consumer-phase1@test.internal', 'Phase 1 Test Consumer')
		 RETURNING id`,
	).Scan(&consumerID); err != nil {
		t.Fatalf("insert consumer: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM participants WHERE id = $1`, consumerID) //nolint:errcheck
	})

	var nodeID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, node_class, hostname, country_code, status)
		 VALUES ($1, 'A', 'phase1-test-node', 'US', 'online')
		 RETURNING id`,
		providerID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID) //nolint:errcheck
	})

	// Populate the in-memory registry with the DB node ID so SubmitJob can
	// match the registry entry to the jobs.node_id FK and the provider query.
	registry := orchestrator.NewNodeRegistry()
	registry.Register(orchestrator.NodeEntry{
		NodeID:        nodeID,
		ProviderID:    providerID,
		NodeClass:     "A",
		CountryCode:   "US",
		Status:        "online",
		LastHeartbeat: time.Now(),
		HardwareProfile: orchestrator.HardwareProfile{
			CPUCores:  4,
			RAMMB:     8192,
			StorageGB: 100,
		},
	})

	tokenSecret := []byte("phase1-integration-test-secret")
	orch := orchestrator.New(db, registry, tokenSecret, scheduler.Schedule)

	resp, err := orch.SubmitJob(ctx, orchestrator.SubmitJobRequest{
		ConsumerID:   consumerID,
		WorkloadType: "app_hosting",
		CPUCores:     2,
		RAMMB:        4096,
	})
	if err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}
	if resp.JobID == "" {
		t.Fatal("SubmitJob: JobID is empty")
	}
	if resp.JobToken == "" {
		t.Fatal("SubmitJob: JobToken is empty")
	}
	if resp.NodeID != nodeID {
		t.Errorf("SubmitJob NodeID: got %q, want %q", resp.NodeID, nodeID)
	}
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, resp.JobID) //nolint:errcheck
	})

	// Verify DB state directly.
	var status, jobToken string
	if err := db.Pool.QueryRow(ctx,
		`SELECT status, job_token FROM jobs WHERE id = $1`,
		resp.JobID,
	).Scan(&status, &jobToken); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "scheduled" {
		t.Errorf("job status: got %q, want %q", status, "scheduled")
	}
	if jobToken == "" {
		t.Error("job_token is empty in database")
	}

	// Verify the token round-trips correctly.
	verifiedJobID, _, err := orchestrator.VerifyJobToken(resp.JobToken, tokenSecret)
	if err != nil {
		t.Fatalf("VerifyJobToken: %v", err)
	}
	if verifiedJobID != resp.JobID {
		t.Errorf("VerifyJobToken jobID: got %q, want %q", verifiedJobID, resp.JobID)
	}

	t.Run("stripe_charge", func(t *testing.T) {
		stripeKey := os.Getenv("STRIPE_SECRET_KEY")
		if stripeKey == "" {
			t.Skip("STRIPE_SECRET_KEY not set")
		}

		pc := payment.New(stripeKey)
		_, err := pc.CreateDestinationCharge(ctx, 1000, 150, "acct_test")
		// "acct_test" is not a real Stripe account — we expect a Stripe error.
		// The assertion is only that the client initialises and makes the call
		// without panicking.
		if err == nil {
			t.Log("CreateDestinationCharge: unexpectedly succeeded")
		} else {
			t.Logf("CreateDestinationCharge: got expected Stripe error: %v", err)
		}
	})
}
