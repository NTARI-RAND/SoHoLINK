package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

const jobTokenTTL = 24 * time.Hour

// SubmitJobRequest describes a consumer's workload placement request.
type SubmitJobRequest struct {
	ConsumerID        string
	WorkloadType      string
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
}

// New constructs an Orchestrator. tokenSecret must be a stable secret used to
// sign job tokens — rotating it will invalidate all outstanding tokens.
func New(db *store.DB, registry *NodeRegistry, tokenSecret []byte) *Orchestrator {
	return &Orchestrator{
		db:          db,
		registry:    registry,
		tokenSecret: tokenSecret,
	}
}

// SubmitJob validates the request, finds a matching node, writes the job to
// PostgreSQL, generates a signed job token, and returns the placement result.
func (o *Orchestrator) SubmitJob(ctx context.Context, req SubmitJobRequest) (SubmitJobResponse, error) {
	if req.ConsumerID == "" {
		return SubmitJobResponse{}, fmt.Errorf("submit job: ConsumerID is required")
	}
	if req.WorkloadType == "" {
		return SubmitJobResponse{}, fmt.Errorf("submit job: WorkloadType is required")
	}

	node, err := o.registry.FindMatch(MatchRequest{
		WorkloadType:      req.WorkloadType,
		CountryConstraint: req.CountryConstraint,
		CPUCores:          req.CPUCores,
		RAMMB:             req.RAMMB,
		GPURequired:       req.GPURequired,
		StorageGB:         req.StorageGB,
	})
	if err != nil {
		return SubmitJobResponse{}, fmt.Errorf("find node: %w", err)
	}

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
			id, consumer_id, node_id, workload_type, status,
			country_constraint, cpu_cores, ram_mb, storage_gb, gpu_required
		) VALUES (
			$1, $2, $3, $4::workload_type, 'pending'::job_status,
			$5, $6, $7, $8, $9
		)`,
		jobID, req.ConsumerID, node.NodeID, req.WorkloadType,
		countryConstraint, req.CPUCores, req.RAMMB, req.StorageGB, req.GPURequired,
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
		FROM providers p
		INNER JOIN nodes n ON n.provider_id = p.id
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
