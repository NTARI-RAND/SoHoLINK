// Package protocoladapter implements the sohocloud-protocol
// coordinator.Coordinator interface over SoHoLINK's existing orchestrator,
// store, and operator layers (B4 milestone: serve /v0). It DELEGATES: every
// business decision (dispatch flip, completion branching, decline reroute,
// fee reads) is the same code path the bespoke handlers use, so the two
// surfaces cannot drift. Transitional coexistence: all bespoke routes stay
// live; /v0 is mounted alongside them.
//
// Security model per SPEC §2/§5.5, applied by every node-named method:
//  1. bind the caller's SPIFFE identity to the message's NodeID
//     (identity.BindsTo — deterministic construction, never a lookup),
//  2. verify the message's own ed25519 signature against the key enrolled in
//     node_protocol_keys (out-of-band enrollment via POST /nodes/pubkey), and
//  3. enforce strictly-increasing Seq (listings/heartbeats) with a guarded
//     UPDATE against the persisted last_*_seq columns, so monotonicity
//     survives coordinator restarts.
package protocoladapter

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/NTARI-RAND/sohocloud-protocol/coordinator"
	"github.com/NTARI-RAND/sohocloud-protocol/employment"
	"github.com/NTARI-RAND/sohocloud-protocol/fees"
	protoidentity "github.com/NTARI-RAND/sohocloud-protocol/identity"
	"github.com/NTARI-RAND/sohocloud-protocol/listing"
	"github.com/NTARI-RAND/sohocloud-protocol/liveness"
	"github.com/jackc/pgx/v5"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// Adapter-level sentinel errors. The bindNodeID middleware produces the
// canonical 401/403 HTTP statuses BEFORE the reference handler runs; these
// re-checks are defense in depth per the Coordinator docstring (they surface
// as 500 through the reference fail(), never silently pass).
var (
	ErrNoIdentity       = errors.New("protocoladapter: no SPIFFE identity in context")
	ErrIdentityMismatch = errors.New("protocoladapter: SPIFFE identity does not match node")
	ErrBadSignature     = errors.New("protocoladapter: message signature verification failed")
	ErrSeqNotMonotonic  = errors.New("protocoladapter: Seq does not strictly exceed the last seen for this node")
	ErrNoProtocolKey    = errors.New("protocoladapter: no protocol key enrolled for node (POST /nodes/pubkey)")
)

// FeeSource is the fee-declaration read the adapter needs. Satisfied by
// *operator.Repository; an interface so tests can stub fees without a DB.
type FeeSource interface {
	CurrentFeeDeclaration(ctx context.Context, coordinatorID string) (fees.FeeDeclaration, error)
}

// Adapter implements coordinator.Coordinator over SoHoLINK's internals.
type Adapter struct {
	db            *store.DB
	registry      *orchestrator.NodeRegistry
	repo          FeeSource
	coordinatorID string
	coordKey      ed25519.PrivateKey // signs employment.Assignment offers; declaration SIGNING stays :8090-only
	tokenSecret   []byte             // reserved: bespoke job-token issuance if /v0 ever needs it
}

var _ coordinator.Coordinator = (*Adapter)(nil)

// New constructs the adapter. coordKey signs the Assignments PollJobs
// returns — a deliberate decision to let the coordinator signing key into the
// orchestrator process (there is no third option short of a signing sidecar);
// fee-declaration signing remains exclusively on the loopback :8090 surface.
func New(
	db *store.DB,
	registry *orchestrator.NodeRegistry,
	repo FeeSource,
	coordinatorID string,
	coordKey ed25519.PrivateKey,
	tokenSecret []byte,
) *Adapter {
	return &Adapter{
		db:            db,
		registry:      registry,
		repo:          repo,
		coordinatorID: coordinatorID,
		coordKey:      coordKey,
		tokenSecret:   tokenSecret,
	}
}

// bind enforces SPEC §2: the caller's SPIFFE path must be exactly
// /node/<NodeID> for the node named in the message.
func (a *Adapter) bind(ctx context.Context, n protoidentity.NodeID) error {
	spiffeID, ok := identity.SPIFFEIDFromContext(ctx)
	if !ok {
		return ErrNoIdentity
	}
	if !protoidentity.BindsTo(spiffeID.Path(), n) {
		return ErrIdentityMismatch
	}
	return nil
}

// nodeKey resolves the node's enrolled ed25519 verification key.
func (a *Adapter) nodeKey(ctx context.Context, nodeID string) (ed25519.PublicKey, error) {
	var key []byte
	err := a.db.Pool.QueryRow(ctx,
		`SELECT public_key FROM node_protocol_keys WHERE node_id = $1`,
		nodeID,
	).Scan(&key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("node %s: %w", nodeID, ErrNoProtocolKey)
		}
		return nil, fmt.Errorf("load protocol key for %s: %w", nodeID, err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("node %s: stored protocol key has invalid length %d", nodeID, len(key))
	}
	return ed25519.PublicKey(key), nil
}

// seqColumn names the two persisted Seq counters. Only these constants are
// ever interpolated into bumpSeq's statement — never caller input.
type seqColumn string

const (
	seqListing   seqColumn = "last_listing_seq"
	seqHeartbeat seqColumn = "last_heartbeat_seq"
)

// bumpSeq enforces SPEC §5.5 strict monotonicity with a guarded UPDATE:
// zero rows affected means the presented Seq does not strictly exceed the
// persisted one (replay/rollback) and the message is rejected.
func (a *Adapter) bumpSeq(ctx context.Context, nodeID string, col seqColumn, seq uint64) error {
	if seq > math.MaxInt64 {
		return fmt.Errorf("node %s: Seq %d exceeds the storable range: %w", nodeID, seq, ErrSeqNotMonotonic)
	}
	//nolint:gosec // col is one of two package-private constants, never input.
	q := fmt.Sprintf(
		`UPDATE node_protocol_keys SET %s = $2 WHERE node_id = $1 AND %s < $2`,
		col, col,
	)
	tag, err := a.db.Pool.Exec(ctx, q, nodeID, int64(seq))
	if err != nil {
		return fmt.Errorf("bump %s for %s: %w", col, nodeID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node %s seq %d: %w", nodeID, seq, ErrSeqNotMonotonic)
	}
	return nil
}

// SubmitListing records a node's signed capability advertisement as a
// capability UPDATE on an EXISTING node: the bespoke /nodes/claim flow
// remains the onboarding path transitionally (a CapabilityListing carries no
// country_code, which is NOT NULL on nodes), so an unknown NodeID is an
// error, not an implicit registration.
func (a *Adapter) SubmitListing(ctx context.Context, l listing.CapabilityListing) error {
	nodeID := string(l.NodeID)
	if err := a.bind(ctx, l.NodeID); err != nil {
		return err
	}
	pub, err := a.nodeKey(ctx, nodeID)
	if err != nil {
		return err
	}
	if !l.Verify(pub) {
		return fmt.Errorf("listing from %s: %w", nodeID, ErrBadSignature)
	}
	if err := a.bumpSeq(ctx, nodeID, seqListing, l.Seq); err != nil {
		return err
	}

	class, err := nodeClassForComputeClass(l.Class)
	if err != nil {
		return fmt.Errorf("listing from %s: %w", nodeID, err)
	}

	// Capacity mapping: VCPUs→CPUCores, MemMB→RAMMB, DiskMB/1024→StorageGB.
	// GPU/bandwidth are unknown to the protocol listing and preserved.
	storageGB := l.Capacity.DiskMB / 1024
	row, err := store.UpdateNodeCapabilities(ctx, a.db, nodeID, class, l.Capacity.VCPUs, l.Capacity.MemMB, storageGB)
	if err != nil {
		return err
	}

	// Printers: upsert with the derived printer_id "<kind>:<model>"
	// (transitional derivation — the listing carries no stable printer id).
	// ON CONFLICT preserves the portal-set enabled flag.
	for _, p := range l.Printers {
		printerID := string(p.Kind) + ":" + p.Model
		if _, err := a.db.Pool.Exec(ctx, `
			INSERT INTO node_printers (node_id, printer_id, printer_name)
			VALUES ($1, $2, $3)
			ON CONFLICT (node_id, printer_id) DO UPDATE SET
				printer_name = EXCLUDED.printer_name,
				detected_at  = NOW()`,
			nodeID, printerID, p.Model,
		); err != nil {
			return fmt.Errorf("listing from %s: upsert printer %s: %w", nodeID, printerID, err)
		}
	}

	// Refresh the in-memory registry. An existing entry is merged (GPU and
	// bandwidth preserved); a missing one is built from the DB row.
	entry, ok := a.registry.Get(nodeID)
	if !ok {
		entry = orchestrator.NodeEntry{
			NodeID:        nodeID,
			ParticipantID: row.ParticipantID,
			CountryCode:   row.CountryCode,
			Region:        row.Region,
		}
	}
	entry.NodeClass = class
	entry.Status = "online"
	entry.LastHeartbeat = time.Now()
	entry.HardwareProfile.CPUCores = l.Capacity.VCPUs
	entry.HardwareProfile.RAMMB = l.Capacity.MemMB
	entry.HardwareProfile.StorageGB = storageGB
	a.registry.Register(entry)

	// Advisory matcher hints from the inverted OptIn flags (SPEC §5.1: the
	// coordinator is NOT a security boundary for opt-out — the node's local
	// allowlist enforcement is). Storage rides the coarse Compute flag; the
	// protocol listing has no storage-specific opt-in. The bespoke heartbeat
	// path will overwrite these from portal DB state on its next beat —
	// accepted transitional-coexistence behavior. HasEnabledPrinter stays
	// DB-derived: enabling a printer remains a portal (member) action.
	var hasEnabledPrinter bool
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM node_printers WHERE node_id = $1 AND enabled = TRUE)`,
		nodeID,
	).Scan(&hasEnabledPrinter); err != nil {
		return fmt.Errorf("listing from %s: read enabled printers: %w", nodeID, err)
	}
	if err := a.registry.UpdateOptOut(nodeID, orchestrator.NodeOptOutState{
		OptOutCompute:     !l.OptIn.Compute,
		OptOutStorage:     !l.OptIn.Compute,
		OptOutPrinting:    !l.OptIn.Print,
		HasEnabledPrinter: hasEnabledPrinter,
	}); err != nil {
		// Eviction race after Register — advisory state; warn and continue.
		slog.Warn("protocoladapter: registry opt-out refresh failed", "node_id", nodeID, "err", err)
	}
	return nil
}

// Heartbeat records a node's signed liveness signal under the same
// signature-and-monotonic-Seq discipline as SubmitListing. The signed
// protocol Heartbeat carries no advisory load fields — the node's load
// sample simply goes stale in the registry and the scheduler's idleScore
// correctly reads 0 for it.
func (a *Adapter) Heartbeat(ctx context.Context, h liveness.Heartbeat) error {
	nodeID := string(h.NodeID)
	if err := a.bind(ctx, h.NodeID); err != nil {
		return err
	}
	pub, err := a.nodeKey(ctx, nodeID)
	if err != nil {
		return err
	}
	if !h.Verify(pub) {
		return fmt.Errorf("heartbeat from %s: %w", nodeID, ErrBadSignature)
	}
	if err := a.bumpSeq(ctx, nodeID, seqHeartbeat, h.Seq); err != nil {
		return err
	}

	if err := a.registry.Heartbeat(nodeID); err != nil {
		// Not in the in-memory registry: the node must (re)submit a listing
		// after a coordinator restart, exactly as bespoke agents re-register.
		return fmt.Errorf("heartbeat from %s: %w (submit a listing first)", nodeID, err)
	}
	if err := store.RecordNodeHeartbeat(ctx, a.db, nodeID); err != nil {
		return fmt.Errorf("heartbeat from %s: %w", nodeID, err)
	}
	return nil
}

// PollJobs returns the caller's scheduled jobs as coordinator-signed
// Assignments, flipping them scheduled → dispatched exactly like the bespoke
// GET /nodes/jobs (same store.PollScheduledJobs, same C5 self-print
// predicate). Fail-closed on fees: with no published FeeDeclaration there
// are no offers — an Assignment must carry real signed terms, never
// unsigned/zero-fee ones — and the fee read happens BEFORE the dispatch flip
// so a fees outage never strands claimed jobs.
func (a *Adapter) PollJobs(ctx context.Context, id protoidentity.NodeID) ([]employment.Assignment, error) {
	if err := a.bind(ctx, id); err != nil {
		return nil, err
	}

	decl, err := a.repo.CurrentFeeDeclaration(ctx, a.coordinatorID)
	if err != nil {
		return nil, fmt.Errorf("poll jobs for %s: fee declaration: %w", id, err)
	}

	jobs, err := store.PollScheduledJobs(ctx, a.db, string(id))
	if err != nil {
		return nil, fmt.Errorf("poll jobs for %s: %w", id, err)
	}

	offeredAt := time.Now().UTC()
	out := make([]employment.Assignment, 0, len(jobs))
	for _, j := range jobs {
		asg := assignmentForJob(j, id, decl.Terms, offeredAt)
		asg.Sign(a.coordKey)
		out = append(out, asg)
	}
	return out, nil
}

// assignmentForJob maps a dispatched job row onto a protocol Assignment
// (unsigned). Pure so the mapping and the coordinator signature can be
// unit-tested without a database.
func assignmentForJob(j store.DispatchedJob, id protoidentity.NodeID, terms fees.Terms, offeredAt time.Time) employment.Assignment {
	spec := employment.JobSpec{Image: j.Image}
	switch j.WorkloadType {
	case "print_traditional":
		spec.Workload = "print"
		spec.PrinterKind = string(listing.PrinterTraditional)
	case "print_3d":
		spec.Workload = "print"
		spec.PrinterKind = string(listing.Printer3D)
	default:
		spec.Workload = "compute"
	}
	return employment.Assignment{
		JobID:     j.JobID,
		NodeID:    id,
		Spec:      spec,
		Fee:       terms,
		OfferedAt: offeredAt,
	}
}

// Decline records a node's signed refusal: a job_node_declines row plus the
// guarded status flip to 'declined'. The existing StartDeclineRerouteLoop
// picks the job up and re-places it (delegation, not duplication).
func (a *Adapter) Decline(ctx context.Context, d employment.Decline) error {
	nodeID := string(d.NodeID)
	if err := a.bind(ctx, d.NodeID); err != nil {
		return err
	}
	pub, err := a.nodeKey(ctx, nodeID)
	if err != nil {
		return err
	}
	if !d.Verify(pub) {
		return fmt.Errorf("decline from %s: %w", nodeID, ErrBadSignature)
	}

	// Guarded status flip FIRST: only a job actually placed on THIS node
	// (scheduled/dispatched to it) can be declined. Doing the flip before the
	// decline-row INSERT means an authenticated node cannot spam
	// job_node_declines rows for arbitrary jobs it was never offered.
	tag, err := a.db.Pool.Exec(ctx,
		`UPDATE jobs
		 SET status      = 'declined'::job_status,
		     declined_at = NOW(),
		     updated_at  = NOW()
		 WHERE id = $1 AND node_id = $2
		   AND status IN ('scheduled'::job_status, 'dispatched'::job_status)`,
		d.JobID, nodeID,
	)
	if err != nil {
		return fmt.Errorf("decline from %s: update job %s: %w", nodeID, d.JobID, err)
	}
	if tag.RowsAffected() == 0 {
		// Lost race or the job is not currently placeable on this node — nothing
		// to flip, and (deliberately) no decline row to record for a job this
		// node was never offered.
		slog.Warn("protocoladapter: decline matched no scheduled/dispatched row",
			"job_id", d.JobID, "node_id", nodeID, "reason", string(d.Reason))
		return nil
	}

	// Record the decline so StartDeclineRerouteLoop excludes this node when it
	// re-places the job.
	if _, err := a.db.Pool.Exec(ctx,
		`INSERT INTO job_node_declines (job_id, node_id) VALUES ($1, $2)
		 ON CONFLICT (job_id, node_id) DO NOTHING`,
		d.JobID, nodeID,
	); err != nil {
		return fmt.Errorf("decline from %s: record decline for job %s: %w", nodeID, d.JobID, err)
	}
	a.registry.AddInFlight(nodeID, -1)
	return nil
}

// ReportJob records a node's signed outcome via the exact /complete code
// path (store.CompleteJob: exit-code branching, terminal timestamps,
// metering decision).
func (a *Adapter) ReportJob(ctx context.Context, r employment.JobReport) error {
	nodeID := string(r.NodeID)
	if err := a.bind(ctx, r.NodeID); err != nil {
		return err
	}
	pub, err := a.nodeKey(ctx, nodeID)
	if err != nil {
		return err
	}
	if !r.Verify(pub) {
		return fmt.Errorf("report from %s: %w", nodeID, ErrBadSignature)
	}

	var assignedNodeID string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT COALESCE(node_id::text, '') FROM jobs WHERE id = $1`,
		r.JobID,
	).Scan(&assignedNodeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("report from %s: job %s not found", nodeID, r.JobID)
		}
		return fmt.Errorf("report from %s: read job %s: %w", nodeID, r.JobID, err)
	}
	if assignedNodeID != nodeID {
		return fmt.Errorf("report from %s: job %s is not assigned to this node", nodeID, r.JobID)
	}

	// The protocol has no separate "started" signal, so a job handed out by
	// PollJobs is still 'dispatched'. store.CompleteJob's guarded UPDATE
	// requires 'running', so flip dispatched → running here — the signed report
	// IS the evidence the job ran (started_at from the report). Guarded by
	// node_id; a no-op if the job is already 'running'. Without this, every /v0
	// ReportJob would fail ErrJobNotRunning and expireDispatched would revert
	// the job to scheduled and re-offer it forever.
	if _, err := a.db.Pool.Exec(ctx,
		`UPDATE jobs SET status = 'running'::job_status,
		        started_at = COALESCE(started_at, $3), updated_at = NOW()
		 WHERE id = $1 AND node_id = $2 AND status = 'dispatched'::job_status`,
		r.JobID, nodeID, r.StartedAt,
	); err != nil {
		return fmt.Errorf("report from %s: mark job %s running: %w", nodeID, r.JobID, err)
	}

	exitCode := r.ExitCode
	newStatus, err := store.CompleteJob(ctx, a.db, r.JobID, &exitCode, r.FailureCause, r.TmpfsExhausted)
	if err != nil {
		return fmt.Errorf("report from %s: complete job %s: %w", nodeID, r.JobID, err)
	}
	// All statuses CompleteJob produces are terminal for placement.
	a.registry.AddInFlight(nodeID, -1)
	slog.Info("protocoladapter: job report applied",
		"job_id", r.JobID, "node_id", nodeID, "status", newStatus)
	return nil
}

// Fees returns the coordinator's current signed fee declaration — the exact
// stored artifact (round-trips Verify against the coordinator public key).
// Surfaces operator.ErrNoFeeDeclaration unwrapped so the HTTP handler can map
// it to 404.
func (a *Adapter) Fees(ctx context.Context) (fees.FeeDeclaration, error) {
	return a.repo.CurrentFeeDeclaration(ctx, a.coordinatorID)
}
