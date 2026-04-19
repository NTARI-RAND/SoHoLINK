package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
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
	WorkloadType      string
	ContainerImage    string
	CountryConstraint string
	CPUCores          int
	RAMMB             int
	GPURequired       bool
	StorageGB         int
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
	db          *store.DB
	registry    *NodeRegistry
	tokenSecret []byte
	schedule    ScheduleFunc
}

// New constructs an Orchestrator. schedule is called during SubmitJob to rank
// candidates; pass scheduler.Schedule from internal/scheduler.
func New(db *store.DB, registry *NodeRegistry, tokenSecret []byte, schedule ScheduleFunc) *Orchestrator {
	return &Orchestrator{
		db:          db,
		registry:    registry,
		tokenSecret: tokenSecret,
		schedule:    schedule,
	}
}

// SubmitJob validates the request, finds matching nodes, runs them through
// the scheduler, writes the job to PostgreSQL, generates a signed job token,
// and returns the placement result.
func (o *Orchestrator) SubmitJob(ctx context.Context, req SubmitJobRequest) (SubmitJobResponse, error) {
	if req.ConsumerID == "" {
		return SubmitJobResponse{}, fmt.Errorf("submit job: ConsumerID is required")
	}
	if req.WorkloadType == "" {
		return SubmitJobResponse{}, fmt.Errorf("submit job: WorkloadType is required")
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
