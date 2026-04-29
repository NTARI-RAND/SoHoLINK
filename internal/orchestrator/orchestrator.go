package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

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
	db            *store.DB
	registry      *NodeRegistry
	tokenSecret   []byte
	schedule      ScheduleFunc
	allowlistPath string
}

// New constructs an Orchestrator. schedule is called during SubmitJob to rank
// candidates; pass scheduler.Schedule from internal/scheduler.
func New(db *store.DB, registry *NodeRegistry, tokenSecret []byte, schedule ScheduleFunc, allowlistPath string) *Orchestrator {
	return &Orchestrator{
		db:            db,
		registry:      registry,
		tokenSecret:   tokenSecret,
		schedule:      schedule,
		allowlistPath: allowlistPath,
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

	_, err = tx.Exec(ctx,
		`UPDATE jobs SET job_token = $1, status = 'scheduled'::job_status WHERE id = $2`,
		token, jobID,
	)
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("update job to scheduled: %w", err)
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
