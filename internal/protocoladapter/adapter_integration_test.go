//go:build integration

package protocoladapter

import (
	"context"
	"crypto/ed25519"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NTARI-RAND/sohocloud-protocol/employment"
	"github.com/NTARI-RAND/sohocloud-protocol/fees"
	protoidentity "github.com/NTARI-RAND/sohocloud-protocol/identity"
	"github.com/NTARI-RAND/sohocloud-protocol/listing"
	"github.com/NTARI-RAND/sohocloud-protocol/liveness"
	"github.com/spiffe/go-spiffe/v2/spiffeid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// adapterFixture holds shared DB state for adapter integration tests.
type adapterFixture struct {
	db         *store.DB
	registry   *orchestrator.NodeRegistry
	adapter    *Adapter
	providerID string
	consumerID string
	nodeID     string
	nodePub    ed25519.PublicKey
	nodePriv   ed25519.PrivateKey
	coordPub   ed25519.PublicKey
	ctx        context.Context // carries the node's SPIFFE identity
}

func setupAdapterFixture(t *testing.T, feeSrc FeeSource) *adapterFixture {
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
		               node_protocol_keys, node_printers, resource_profiles,
		               nodes, participants
		RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}

	var providerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ('provider-proto@test.internal', 'Proto Test Provider')
		 RETURNING id`,
	).Scan(&providerID); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	var consumerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name)
		 VALUES ('consumer-proto@test.internal', 'Proto Test Consumer')
		 RETURNING id`,
	).Scan(&consumerID); err != nil {
		t.Fatalf("insert consumer: %v", err)
	}
	var nodeID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, node_class, hostname, country_code, status)
		 VALUES ($1, 'C', 'proto-test-node', 'US', 'online')
		 RETURNING id`,
		providerID,
	).Scan(&nodeID); err != nil {
		t.Fatalf("insert node: %v", err)
	}

	nodePub, nodePriv := testKeypair(t)
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO node_protocol_keys (node_id, public_key) VALUES ($1, $2)`,
		nodeID, []byte(nodePub),
	); err != nil {
		t.Fatalf("insert node_protocol_keys: %v", err)
	}

	registry := orchestrator.NewNodeRegistry()
	registry.Register(orchestrator.NodeEntry{
		NodeID:        nodeID,
		ParticipantID: providerID,
		NodeClass:     "C",
		CountryCode:   "US",
		Status:        "online",
		LastHeartbeat: time.Now(),
		HardwareProfile: orchestrator.HardwareProfile{
			CPUCores:      2,
			RAMMB:         4096,
			StorageGB:     50,
			GPUPresent:    true, // must be preserved across a listing merge
			BandwidthMbps: 100,
		},
	})

	coordPub, coordPriv := testKeypair(t)
	adapter := New(db, registry, feeSrc, "soholink", coordPriv, []byte("proto-test-secret"))

	nodeCtx := identity.WithSPIFFEID(context.Background(),
		spiffeid.RequireFromString("spiffe://soholink.org/node/"+nodeID))

	return &adapterFixture{
		db:         db,
		registry:   registry,
		adapter:    adapter,
		providerID: providerID,
		consumerID: consumerID,
		nodeID:     nodeID,
		nodePub:    nodePub,
		nodePriv:   nodePriv,
		coordPub:   coordPub,
		ctx:        nodeCtx,
	}
}

func testFeeDecl() fees.FeeDeclaration {
	return fees.FeeDeclaration{
		CoordinatorID: "soholink",
		Terms:         fees.Terms{ContributorShareBps: 6500, PlatformFeeBps: 3500},
		EffectiveAt:   time.Now().UTC(),
		Seq:           1,
		Signature:     make([]byte, ed25519.SignatureSize),
	}
}

func (f *adapterFixture) signedListing(seq uint64) listing.CapabilityListing {
	l := listing.CapabilityListing{
		NodeID: protoidentity.NodeID(f.nodeID),
		Class:  listing.ClassServer,
		Printers: []listing.PrinterCapability{
			{Kind: listing.PrinterTraditional, Model: "HP LaserJet 4"},
		},
		Capacity: listing.Capacity{VCPUs: 8, MemMB: 16384, DiskMB: 200 * 1024},
		OptIn:    listing.WorkloadOptIn{Compute: true, Print: true},
		IssuedAt: time.Now().UTC(),
		Seq:      seq,
	}
	l.Sign(f.nodePriv)
	return l
}

func TestAdapter_SubmitListing_UpdatesNodeAndRegistry(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})
	ctx := context.Background()

	if err := f.adapter.SubmitListing(f.ctx, f.signedListing(1)); err != nil {
		t.Fatalf("SubmitListing: %v", err)
	}

	// DB: class updated, capacity merged, printer upserted.
	var nodeClass string
	var cpuCores, ramMB, storageGB int
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT node_class::text,
		        (hardware_profile->>'cpu_cores')::int,
		        (hardware_profile->>'ram_mb')::int,
		        (hardware_profile->>'storage_gb')::int
		 FROM nodes WHERE id = $1`, f.nodeID,
	).Scan(&nodeClass, &cpuCores, &ramMB, &storageGB); err != nil {
		t.Fatalf("query node: %v", err)
	}
	if nodeClass != "A" {
		t.Errorf("node_class: got %q, want A (server→A)", nodeClass)
	}
	if cpuCores != 8 || ramMB != 16384 || storageGB != 200 {
		t.Errorf("capacity: got cpu=%d ram=%d storage=%d, want 8/16384/200", cpuCores, ramMB, storageGB)
	}

	var printerName string
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT printer_name FROM node_printers WHERE node_id = $1 AND printer_id = $2`,
		f.nodeID, "traditional:HP LaserJet 4",
	).Scan(&printerName); err != nil {
		t.Fatalf("query node_printers: %v", err)
	}
	if printerName != "HP LaserJet 4" {
		t.Errorf("printer_name: got %q", printerName)
	}

	// Registry: merged entry preserves GPU/bandwidth, refreshes capacity/class.
	entry, ok := f.registry.Get(f.nodeID)
	if !ok {
		t.Fatal("registry entry missing after listing")
	}
	if entry.NodeClass != "A" {
		t.Errorf("registry NodeClass: got %q, want A", entry.NodeClass)
	}
	if entry.HardwareProfile.CPUCores != 8 || entry.HardwareProfile.RAMMB != 16384 || entry.HardwareProfile.StorageGB != 200 {
		t.Errorf("registry capacity not refreshed: %+v", entry.HardwareProfile)
	}
	if !entry.HardwareProfile.GPUPresent || entry.HardwareProfile.BandwidthMbps != 100 {
		t.Errorf("registry GPU/bandwidth not preserved: %+v", entry.HardwareProfile)
	}
	if entry.OptOutCompute || entry.OptOutPrinting {
		t.Errorf("advisory opt-outs should be false for OptIn{true,true}: %+v", entry)
	}
}

func TestAdapter_SubmitListing_BadSignatureRejected(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})

	l := f.signedListing(1)
	_, wrongPriv := testKeypair(t)
	l.Sign(wrongPriv) // re-sign with a key that is not enrolled

	err := f.adapter.SubmitListing(f.ctx, l)
	if err == nil || !strings.Contains(err.Error(), ErrBadSignature.Error()) {
		t.Fatalf("expected ErrBadSignature, got %v", err)
	}
}

func TestAdapter_SubmitListing_SeqRollbackRejected(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})

	if err := f.adapter.SubmitListing(f.ctx, f.signedListing(5)); err != nil {
		t.Fatalf("SubmitListing seq=5: %v", err)
	}
	// Equal Seq — replay — rejected.
	err := f.adapter.SubmitListing(f.ctx, f.signedListing(5))
	if err == nil || !strings.Contains(err.Error(), ErrSeqNotMonotonic.Error()) {
		t.Fatalf("replayed seq: expected ErrSeqNotMonotonic, got %v", err)
	}
	// Lower Seq — rollback — rejected.
	err = f.adapter.SubmitListing(f.ctx, f.signedListing(4))
	if err == nil || !strings.Contains(err.Error(), ErrSeqNotMonotonic.Error()) {
		t.Fatalf("rolled-back seq: expected ErrSeqNotMonotonic, got %v", err)
	}
	// Strictly greater — accepted.
	if err := f.adapter.SubmitListing(f.ctx, f.signedListing(6)); err != nil {
		t.Fatalf("SubmitListing seq=6: %v", err)
	}
}

func TestAdapter_SubmitListing_BindingEnforced(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})

	// No identity → ErrNoIdentity.
	if err := f.adapter.SubmitListing(context.Background(), f.signedListing(1)); err != ErrNoIdentity {
		t.Errorf("no identity: got %v, want ErrNoIdentity", err)
	}
	// Wrong node identity → ErrIdentityMismatch.
	wrongCtx := identity.WithSPIFFEID(context.Background(),
		spiffeid.RequireFromString("spiffe://soholink.org/node/some-other-node"))
	if err := f.adapter.SubmitListing(wrongCtx, f.signedListing(1)); err != ErrIdentityMismatch {
		t.Errorf("mismatch: got %v, want ErrIdentityMismatch", err)
	}
}

func TestAdapter_Heartbeat_RecordsAndEnforcesSeq(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})
	ctx := context.Background()

	hb := liveness.Heartbeat{NodeID: protoidentity.NodeID(f.nodeID), SentAt: time.Now().UTC(), Seq: 1}
	hb.Sign(f.nodePriv)
	if err := f.adapter.Heartbeat(f.ctx, hb); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	var events int
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM node_heartbeat_events WHERE node_id = $1`, f.nodeID,
	).Scan(&events); err != nil {
		t.Fatalf("query heartbeat events: %v", err)
	}
	if events != 1 {
		t.Errorf("heartbeat events: got %d, want 1", events)
	}

	// Replay is rejected.
	replay := liveness.Heartbeat{NodeID: protoidentity.NodeID(f.nodeID), SentAt: time.Now().UTC(), Seq: 1}
	replay.Sign(f.nodePriv)
	err := f.adapter.Heartbeat(f.ctx, replay)
	if err == nil || !strings.Contains(err.Error(), ErrSeqNotMonotonic.Error()) {
		t.Fatalf("replayed heartbeat: expected ErrSeqNotMonotonic, got %v", err)
	}
}

// seedScheduledJob inserts a scheduled compute job bound to the fixture node.
func (f *adapterFixture) seedScheduledJob(t *testing.T) string {
	t.Helper()
	var jobID string
	if err := f.db.Pool.QueryRow(context.Background(),
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image, job_token)
		 VALUES ($1, $2, 'batch_compute'::workload_type, 'scheduled'::job_status,
		         2, 4096, 0, FALSE, 'soholink/compute-worker@sha256:aaaa', 'test-token')
		 RETURNING id`,
		f.consumerID, f.nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert scheduled job: %v", err)
	}
	return jobID
}

func TestAdapter_PollJobs_ReturnsCoordinatorSignedAssignments(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})
	ctx := context.Background()
	jobID := f.seedScheduledJob(t)

	assignments, err := f.adapter.PollJobs(f.ctx, protoidentity.NodeID(f.nodeID))
	if err != nil {
		t.Fatalf("PollJobs: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("assignments: got %d, want 1", len(assignments))
	}
	asg := assignments[0]
	if asg.JobID != jobID {
		t.Errorf("JobID: got %q, want %q", asg.JobID, jobID)
	}
	if asg.Spec.Workload != "compute" || asg.Spec.PrinterKind != "" {
		t.Errorf("Spec: got %+v, want compute/no-printer", asg.Spec)
	}
	if asg.Fee.ContributorShareBps != 6500 || asg.Fee.PlatformFeeBps != 3500 {
		t.Errorf("Fee: got %+v, want 6500/3500", asg.Fee)
	}
	if !asg.Verify(f.coordPub) {
		t.Error("assignment signature does not verify against the coordinator public key")
	}

	// The poll flipped the job to dispatched (same claim semantics as bespoke).
	var status string
	if err := f.db.Pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1`, jobID).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "dispatched" {
		t.Errorf("status after poll: got %q, want dispatched", status)
	}
}

func TestAdapter_PollJobs_NoFeeDeclaration_FailsClosedBeforeDispatch(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{err: operator.ErrNoFeeDeclaration})
	ctx := context.Background()
	jobID := f.seedScheduledJob(t)

	_, err := f.adapter.PollJobs(f.ctx, protoidentity.NodeID(f.nodeID))
	if err == nil || !strings.Contains(err.Error(), operator.ErrNoFeeDeclaration.Error()) {
		t.Fatalf("expected fail-closed fee error, got %v", err)
	}

	// Fail-closed must fire BEFORE the dispatch flip: the job stays scheduled.
	var status string
	if err := f.db.Pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1`, jobID).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "scheduled" {
		t.Errorf("status after failed poll: got %q, want scheduled (no unsigned offers, no stranded claims)", status)
	}
}

func TestAdapter_Decline_RecordsAndFlips(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})
	ctx := context.Background()
	jobID := f.seedScheduledJob(t)
	f.registry.AddInFlight(f.nodeID, +1)

	d := employment.Decline{
		JobID:      jobID,
		NodeID:     protoidentity.NodeID(f.nodeID),
		Reason:     employment.DeclineLocalPolicy,
		DeclinedAt: time.Now().UTC(),
	}
	d.Sign(f.nodePriv)
	if err := f.adapter.Decline(f.ctx, d); err != nil {
		t.Fatalf("Decline: %v", err)
	}

	var status string
	var declinedAt *time.Time
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status, declined_at FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &declinedAt); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "declined" {
		t.Errorf("status: got %q, want declined", status)
	}
	if declinedAt == nil {
		t.Error("declined_at not set")
	}

	var declineRows int
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM job_node_declines WHERE job_id = $1 AND node_id = $2`,
		jobID, f.nodeID,
	).Scan(&declineRows); err != nil {
		t.Fatalf("query job_node_declines: %v", err)
	}
	if declineRows != 1 {
		t.Errorf("job_node_declines rows: got %d, want 1", declineRows)
	}

	if entry, _ := f.registry.Get(f.nodeID); entry.InFlight != 0 {
		t.Errorf("InFlight after decline: got %d, want 0", entry.InFlight)
	}
}

func TestAdapter_ReportJob_CompletesViaSharedPath(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})
	ctx := context.Background()

	var jobID string
	if err := f.db.Pool.QueryRow(ctx,
		// Seed as 'dispatched' (NOT 'running') so ReportJob exercises the real
		// dispatched→running→completed path — the protocol has no separate
		// "started" signal, so ReportJob itself must flip the status. Seeding
		// 'running' here previously masked that gap.
		`INSERT INTO jobs (participant_id, node_id, workload_type, status,
		                   cpu_cores, ram_mb, storage_gb, gpu_required, container_image, started_at)
		 VALUES ($1, $2, 'batch_compute'::workload_type, 'dispatched'::job_status,
		         2, 4096, 0, FALSE, 'soholink/compute-worker@sha256:aaaa', NULL)
		 RETURNING id`,
		f.consumerID, f.nodeID,
	).Scan(&jobID); err != nil {
		t.Fatalf("insert dispatched job: %v", err)
	}
	f.registry.AddInFlight(f.nodeID, +1)

	r := employment.JobReport{
		JobID:      jobID,
		NodeID:     protoidentity.NodeID(f.nodeID),
		ExitCode:   0,
		StartedAt:  time.Now().UTC().Add(-time.Minute),
		FinishedAt: time.Now().UTC(),
	}
	r.Sign(f.nodePriv)
	if err := f.adapter.ReportJob(f.ctx, r); err != nil {
		t.Fatalf("ReportJob: %v", err)
	}

	var status string
	var exitCode *int
	if err := f.db.Pool.QueryRow(ctx,
		`SELECT status, exit_code FROM jobs WHERE id = $1`, jobID,
	).Scan(&status, &exitCode); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != "completed" {
		t.Errorf("status: got %q, want completed", status)
	}
	if exitCode == nil || *exitCode != 0 {
		t.Errorf("exit_code: got %v, want 0", exitCode)
	}
	if entry, _ := f.registry.Get(f.nodeID); entry.InFlight != 0 {
		t.Errorf("InFlight after report: got %d, want 0", entry.InFlight)
	}
}

func TestAdapter_ReportJob_WrongNodeRejected(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})
	ctx := context.Background()

	// A second node with its own enrolled key reports a job it does not own.
	var otherNodeID string
	if err := f.db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, node_class, hostname, country_code, status)
		 VALUES ($1, 'C', 'proto-test-node-2', 'US', 'online')
		 RETURNING id`,
		f.providerID,
	).Scan(&otherNodeID); err != nil {
		t.Fatalf("insert node 2: %v", err)
	}
	otherPub, otherPriv := testKeypair(t)
	if _, err := f.db.Pool.Exec(ctx,
		`INSERT INTO node_protocol_keys (node_id, public_key) VALUES ($1, $2)`,
		otherNodeID, []byte(otherPub),
	); err != nil {
		t.Fatalf("insert node 2 key: %v", err)
	}

	jobID := f.seedScheduledJob(t) // bound to f.nodeID, not otherNodeID

	r := employment.JobReport{
		JobID:      jobID,
		NodeID:     protoidentity.NodeID(otherNodeID),
		ExitCode:   0,
		StartedAt:  time.Now().UTC().Add(-time.Minute),
		FinishedAt: time.Now().UTC(),
	}
	r.Sign(otherPriv)
	otherCtx := identity.WithSPIFFEID(context.Background(),
		spiffeid.RequireFromString("spiffe://soholink.org/node/"+otherNodeID))

	err := f.adapter.ReportJob(otherCtx, r)
	if err == nil || !strings.Contains(err.Error(), "not assigned to this node") {
		t.Fatalf("expected not-assigned rejection, got %v", err)
	}
}
