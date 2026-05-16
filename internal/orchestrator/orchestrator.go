package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

const jobTokenTTL = 24 * time.Hour

// SLATier controls how many nodes are selected for placement.
type SLATier int

const (
	SLAStandard SLATier = 1 // single node
	SLAReliable SLATier = 2 // two nodes
	SLAPremium  SLATier = 3 // three nodes
)

// ScheduleFunc scores and ranks a candidate list, returning the top N nodes
// for the given tier. Injected at construction to avoid a circular import
// between the orchestrator and scheduler packages.
type ScheduleFunc func(candidates []NodeEntry, tier SLATier) ([]NodeEntry, error)

// SubmitJobRequest describes a consumer's workload placement request.
type SubmitJobRequest struct {
	ConsumerID        string
	WorkloadType      types.MarketplaceWorkloadType
	ContainerImage    string
	CountryConstraint string
	CPUCores          int
	RAMMB             int
	GPURequired       bool
	StorageGB         int
}

// Validate checks all required fields and returns the first error found.
func (r SubmitJobRequest) Validate() error {
	if r.ConsumerID == "" {
		return fmt.Errorf("ConsumerID is required")
	}
	if r.WorkloadType == "" {
		return fmt.Errorf("WorkloadType is required")
	}
	if !r.WorkloadType.IsValid() {
		return fmt.Errorf("unknown WorkloadType %q", r.WorkloadType)
	}
	return nil
}

// SubmitJobResponse carries the placement result returned to the consumer.
type SubmitJobResponse struct {
	JobID                   string
	NodeID                  string
	JobToken                string
	ProviderStripeAccountID string
}

// Orchestrator coordinates job placement across the node registry and database.
type Orchestrator struct {
	db                  *store.DB
	registry            *NodeRegistry
	tokenSecret         []byte
	schedule            ScheduleFunc
	allowlistPath       string
	printConfirmEnabled bool
	confirmationWindow  time.Duration
}

// New constructs an Orchestrator. schedule is called during SubmitJob to rank
// candidates; pass scheduler.Schedule from internal/scheduler.
// printConfirmEnabled gates the awaiting_confirmation path for print jobs.
// confirmationWindow sets the auto-decline deadline (e.g. 4*time.Hour).
func New(
	db *store.DB,
	registry *NodeRegistry,
	tokenSecret []byte,
	schedule ScheduleFunc,
	allowlistPath string,
	printConfirmEnabled bool,
	confirmationWindow time.Duration,
) *Orchestrator {
	return &Orchestrator{
		db:                  db,
		registry:            registry,
		tokenSecret:         tokenSecret,
		schedule:            schedule,
		allowlistPath:       allowlistPath,
		printConfirmEnabled: printConfirmEnabled,
		confirmationWindow:  confirmationWindow,
	}
}

// loadAllowlist reads and parses the signed allowlist from disk.
// Per Defense 3 design (B7 commit 5), the orchestrator does not verify
// the Ed25519 signature here — the operator-placed file is trusted; the
// agent's signature verification is the security boundary for workload
// identity. This loader is for consistency checking only.
//
// Fail-closed: any error here causes SubmitJob to reject. An empty or
// missing file means no submits are accepted, matching the agent's
// posture of "no allowlist = no work."
func loadAllowlist(path string) (*agent.Allowlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load allowlist %s: %w", path, err)
	}
	al := &agent.Allowlist{}
	if err := json.Unmarshal(data, al); err != nil {
		return nil, fmt.Errorf("parse allowlist %s: %w", path, err)
	}
	return al, nil
}

// SubmitJob validates the request, finds matching nodes, runs them through
// the scheduler, writes the job to PostgreSQL, generates a signed job token,
// and returns the placement result.
func (o *Orchestrator) SubmitJob(ctx context.Context, req SubmitJobRequest) (SubmitJobResponse, error) {
	if err := req.Validate(); err != nil {
		return SubmitJobResponse{}, fmt.Errorf("submit job: %w", err)
	}

	// Defense 3 (B7 commit 5): verify marketplace workload type, mapping,
	// and allowlist entry all agree on the workload's agent type. Fail-closed
	// on missing or unparseable allowlist (no allowlist = no submits).
	al, err := loadAllowlist(o.allowlistPath)
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("submit job: %w", err)
	}
	entry, err := al.Lookup(req.ContainerImage)
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("submit job: image not in allowlist: %w", err)
	}
	expectedAgentType, ok := marketplaceToAgent[req.WorkloadType]
	if !ok {
		// Should be unreachable: req.Validate() rejects unknown marketplace
		// types, and MustValidateWorkloadMapping at startup ensures every
		// known type has a mapping entry.
		return SubmitJobResponse{}, fmt.Errorf("submit job: no mapping for workload type %q", req.WorkloadType)
	}
	if entry.Type != expectedAgentType {
		return SubmitJobResponse{}, fmt.Errorf("submit job: workload type mismatch: marketplace=%s maps to agent=%s, but allowlist entry for %s declares agent=%s",
			req.WorkloadType, expectedAgentType, req.ContainerImage, entry.Type)
	}

	candidates, err := o.registry.FindMatch(MatchRequest{
		WorkloadType:      req.WorkloadType,
		CountryConstraint: req.CountryConstraint,
		CPUCores:          req.CPUCores,
		RAMMB:             req.RAMMB,
		GPURequired:       req.GPURequired,
		StorageGB:         req.StorageGB,
	})
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("find nodes: %w", err)
	}

	scheduled, err := o.schedule(candidates, SLAStandard)
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("schedule: %w", err)
	}
	node := scheduled[0]

	isPrintJob := req.WorkloadType == types.MarketplacePrintTraditional ||
		req.WorkloadType == types.MarketplacePrint3D

	jobID := uuid.New().String()

	tx, err := o.db.Pool.Begin(ctx)
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback on non-commit paths is intentional

	var countryConstraint *string
	if req.CountryConstraint != "" {
		countryConstraint = &req.CountryConstraint
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO jobs (
			id, participant_id, node_id, workload_type, status,
			country_constraint, cpu_cores, ram_mb, storage_gb, gpu_required,
			container_image
		) VALUES (
			$1, $2, $3, $4::workload_type, 'pending'::job_status,
			$5, $6, $7, $8, $9, $10
		)`,
		jobID, req.ConsumerID, node.NodeID, req.WorkloadType,
		countryConstraint, req.CPUCores, req.RAMMB, req.StorageGB, req.GPURequired,
		req.ContainerImage,
	)
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("insert job: %w", err)
	}

	token, err := GenerateJobToken(jobID, node.NodeID, jobTokenTTL, o.tokenSecret)
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("generate job token: %w", err)
	}

	if isPrintJob && o.printConfirmEnabled {
		// Resolve a specific enabled printer for this node. The HasEnabledPrinter
		// flag in FindMatch is set from heartbeat data; a brief registry/DB skew
		// is possible, so ErrNoRows here is a detectable synchronization gap.
		//
		// Picks the lowest printer_id lexicographically for determinism. Does not
		// yet discriminate by printer type — node_printers (migration 014) has no
		// type column, so a node enabled for any printing matches both
		// print_traditional and print_3d. Printer-type discrimination is a B8
		// concern; tracked in CLAUDE.md TODOs.
		var printerID string
		if err := tx.QueryRow(ctx,
			`SELECT printer_id FROM node_printers
			 WHERE node_id = $1 AND enabled = TRUE
			 ORDER BY printer_id LIMIT 1`,
			node.NodeID,
		).Scan(&printerID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return SubmitJobResponse{}, fmt.Errorf("resolve printer: node %s passed FindMatch but has no enabled printer (registry/DB drift)", node.NodeID)
			}
			return SubmitJobResponse{}, fmt.Errorf("resolve printer: %w", err)
		}

		specHash, err := canonicalJobSpecHash(req)
		if err != nil {
			return SubmitJobResponse{}, fmt.Errorf("spec hash: %w", err)
		}

		deadline := time.Now().Add(o.confirmationWindow)
		if _, err := tx.Exec(ctx, `
			UPDATE jobs
			SET job_token             = $1,
			    status                = 'awaiting_confirmation'::job_status,
			    printer_id            = $2,
			    spec_hash             = $3,
			    confirmation_deadline = $4,
			    updated_at            = NOW()
			WHERE id = $5`,
			token, printerID, specHash, deadline, jobID,
		); err != nil {
			return SubmitJobResponse{}, fmt.Errorf("update job to awaiting_confirmation: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx,
			`UPDATE jobs SET job_token = $1, status = 'scheduled'::job_status WHERE id = $2`,
			token, jobID,
		); err != nil {
			return SubmitJobResponse{}, fmt.Errorf("update job to scheduled: %w", err)
		}
	}

	var stripeAccountID string
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(p.stripe_account_id, '')
		FROM participants p
		INNER JOIN nodes n ON n.participant_id = p.id
		WHERE n.id = $1`,
		node.NodeID,
	).Scan(&stripeAccountID)
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("fetch provider stripe account: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return SubmitJobResponse{}, fmt.Errorf("commit transaction: %w", err)
	}

	return SubmitJobResponse{
		JobID:                   jobID,
		NodeID:                  node.NodeID,
		JobToken:                token,
		ProviderStripeAccountID: stripeAccountID,
	}, nil
}

// canonicalJobSpecHash returns a SHA-256 digest of the job spec fields that
// the contributor sees and acknowledges at confirmation time. Field declaration
// order is load-bearing — encoding/json marshals struct fields in declaration
// order, so reordering this struct silently changes all hashes.
func canonicalJobSpecHash(req SubmitJobRequest) ([]byte, error) {
	type spec struct {
		WorkloadType      string `json:"workload_type"`
		ContainerImage    string `json:"container_image"`
		CPUCores          int    `json:"cpu_cores"`
		RAMMB             int    `json:"ram_mb"`
		StorageGB         int    `json:"storage_gb"`
		GPURequired       bool   `json:"gpu_required"`
		CountryConstraint string `json:"country_constraint"`
	}
	b, err := json.Marshal(spec{
		WorkloadType:      string(req.WorkloadType),
		ContainerImage:    req.ContainerImage,
		CPUCores:          req.CPUCores,
		RAMMB:             req.RAMMB,
		StorageGB:         req.StorageGB,
		GPURequired:       req.GPURequired,
		CountryConstraint: req.CountryConstraint,
	})
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(b)
	return h[:], nil
}

// RerouteDeclinedJob attempts to find a new node for a job in 'declined' status.
// It reads the job's stored spec, queries job_node_declines for all nodes that
// have already declined, then calls FindMatch with those nodes excluded.
//
// No single transaction wraps the whole operation: the final UPDATE guards with
// AND status = 'declined'::job_status so concurrent reroute workers (or a future
// multi-instance deployment) see a no-op on a row another worker already moved
// forward. The two read queries (job row + decline records) running outside a
// transaction means we may read slightly stale data in a race, but the UPDATE
// guard makes the outcome safe regardless — at worst, we do redundant work that
// produces no row update.
func (o *Orchestrator) RerouteDeclinedJob(ctx context.Context, jobID string) error {
	var (
		workloadType      string
		cpuCores          int
		ramMB             int
		storageGB         int
		gpuRequired       bool
		countryConstraint string
		specHash          []byte
	)
	err := o.db.Pool.QueryRow(ctx,
		`SELECT workload_type, COALESCE(cpu_cores, 0), COALESCE(ram_mb, 0),
		        COALESCE(storage_gb, 0), gpu_required, COALESCE(country_constraint, ''),
		        spec_hash
		 FROM jobs WHERE id = $1`,
		jobID,
	).Scan(&workloadType, &cpuCores, &ramMB, &storageGB, &gpuRequired, &countryConstraint, &specHash)
	if err != nil {
		return fmt.Errorf("reroute: read job %s: %w", jobID, err)
	}

	rows, err := o.db.Pool.Query(ctx,
		`SELECT node_id FROM job_node_declines WHERE job_id = $1`, jobID)
	if err != nil {
		return fmt.Errorf("reroute: read declines for %s: %w", jobID, err)
	}
	var excludedIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("reroute: scan decline: %w", err)
		}
		excludedIDs = append(excludedIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reroute: declines rows: %w", err)
	}

	candidates, findErr := o.registry.FindMatch(MatchRequest{
		WorkloadType:      types.MarketplaceWorkloadType(workloadType),
		CountryConstraint: countryConstraint,
		CPUCores:          cpuCores,
		RAMMB:             ramMB,
		StorageGB:         storageGB,
		GPURequired:       gpuRequired,
		ExcludedNodeIDs:   excludedIDs,
	})
	if findErr != nil {
		// No eligible nodes remain — fail the job.
		if _, err := o.db.Pool.Exec(ctx,
			`UPDATE jobs SET status = 'failed'::job_status, updated_at = NOW()
			 WHERE id = $1 AND status = 'declined'::job_status`,
			jobID,
		); err != nil {
			return fmt.Errorf("reroute: fail job %s: %w", jobID, err)
		}
		return nil
	}

	scheduled, err := o.schedule(candidates, SLAStandard)
	if err != nil {
		return fmt.Errorf("reroute: schedule %s: %w", jobID, err)
	}
	node := scheduled[0]

	token, err := GenerateJobToken(jobID, node.NodeID, jobTokenTTL, o.tokenSecret)
	if err != nil {
		return fmt.Errorf("reroute: generate token: %w", err)
	}

	var printerID string
	if err := o.db.Pool.QueryRow(ctx,
		`SELECT printer_id FROM node_printers
		 WHERE node_id = $1 AND enabled = TRUE
		 ORDER BY printer_id LIMIT 1`,
		node.NodeID,
	).Scan(&printerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("reroute: node %s passed FindMatch but has no enabled printer (registry/DB drift)", node.NodeID)
		}
		return fmt.Errorf("reroute: resolve printer: %w", err)
	}

	deadline := time.Now().Add(o.confirmationWindow)
	if _, err := o.db.Pool.Exec(ctx,
		`UPDATE jobs
		 SET node_id              = $1,
		     job_token            = $2,
		     printer_id           = $3,
		     confirmation_deadline = $4,
		     status               = 'awaiting_confirmation'::job_status,
		     updated_at           = NOW()
		 WHERE id = $5 AND status = 'declined'::job_status`,
		node.NodeID, token, printerID, deadline, jobID,
	); err != nil {
		return fmt.Errorf("reroute: update job %s: %w", jobID, err)
	}
	return nil
}

// rerouteDeclined finds all declined jobs and attempts to reroute each one.
// Called on every tick of StartDeclineRerouteLoop.
func (o *Orchestrator) rerouteDeclined(ctx context.Context) {
	rows, err := o.db.Pool.Query(ctx,
		`SELECT id FROM jobs WHERE status = 'declined'::job_status LIMIT 100`)
	if err != nil {
		slog.Error("reroute: query declined jobs", "error", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			slog.Error("reroute: scan job id", "error", err)
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.Error("reroute: declined rows", "error", err)
		return
	}

	for _, id := range ids {
		if err := o.RerouteDeclinedJob(ctx, id); err != nil {
			slog.Error("reroute declined job", "job_id", id, "error", err)
		}
	}
}

// StartDeclineRerouteLoop runs a background goroutine that periodically finds
// declined jobs and attempts to re-dispatch them to a different node. Stops
// when ctx is cancelled. Run only from cmd/orchestrator — not from cmd/portal,
// which has a separate registry instance that never receives agent heartbeats.
func (o *Orchestrator) StartDeclineRerouteLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				o.rerouteDeclined(ctx)
			}
		}
	}()
}
