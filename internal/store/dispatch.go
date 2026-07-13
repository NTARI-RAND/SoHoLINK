package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// This file holds the dispatch/lifecycle business logic shared between the
// bespoke node API handlers (internal/api/nodes.go) and the sohocloud-protocol
// adapter (internal/protocoladapter). Extracted downward (A2) so the adapter
// can delegate without an api-package import cycle. Behavior is byte-for-byte
// the handlers' pre-extraction logic; HTTP concerns (status codes, SPIFFE
// binding, metrics) stay in the callers.

// ErrJobNotRunning is returned by CompleteJob when the guarded UPDATE matched
// no row — the job is not (or no longer) in 'running' status. Callers map
// this to HTTP 409.
var ErrJobNotRunning = errors.New("store: job is not in running state")

// DispatchedJob is one job claimed by PollScheduledJobs.
type DispatchedJob struct {
	JobID        string
	JobToken     string
	Image        string
	PrinterID    string
	WorkloadType string
}

// PollScheduledJobs returns the node's scheduled jobs and atomically flips
// them scheduled → dispatched (the agent's claim; see B5/TODO 24). Both the
// SELECT and the UPDATE carry the C5 self-print predicate: print jobs whose
// consumer owns the polling node are never dispatched to it.
func PollScheduledJobs(ctx context.Context, db *DB, nodeID string) ([]DispatchedJob, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, COALESCE(job_token, ''), COALESCE(container_image, ''), COALESCE(printer_id, ''),
		        workload_type::text
		 FROM jobs
		 WHERE node_id = $1 AND status = 'scheduled'::job_status
		 AND NOT (
		     workload_type IN ('print_traditional'::workload_type, 'print_3d'::workload_type)
		     AND participant_id = (SELECT participant_id FROM nodes WHERE id = $1)
		 )`,
		nodeID,
	)
	if err != nil {
		return nil, fmt.Errorf("poll scheduled jobs: query: %w", err)
	}
	defer rows.Close()

	jobs := []DispatchedJob{}
	var jobIDs []string
	for rows.Next() {
		var j DispatchedJob
		if err := rows.Scan(&j.JobID, &j.JobToken, &j.Image, &j.PrinterID, &j.WorkloadType); err != nil {
			return nil, fmt.Errorf("poll scheduled jobs: scan: %w", err)
		}
		jobs = append(jobs, j)
		jobIDs = append(jobIDs, j.JobID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("poll scheduled jobs: rows: %w", err)
	}

	if len(jobIDs) > 0 {
		_, err = db.Pool.Exec(ctx,
			`UPDATE jobs SET status = 'dispatched'::job_status, updated_at = NOW()
			 WHERE id = ANY($1) AND status = 'scheduled'::job_status
			 AND NOT (
			     workload_type IN ('print_traditional'::workload_type, 'print_3d'::workload_type)
			     AND participant_id = (SELECT participant_id FROM nodes WHERE id = $2)
			 )`,
			jobIDs, nodeID,
		)
		if err != nil {
			return nil, fmt.Errorf("poll scheduled jobs: dispatch flip: %w", err)
		}
	}
	return jobs, nil
}

// CompleteJob applies a job's reported outcome: derives the terminal status
// from exit code and workload type, executes the running-guarded UPDATE, and
// fires compute metering when warranted (C3/C4 semantics).
//
//   - exitCode == nil (no body / old agent) or non-zero → failed
//   - zero + print workload → awaiting_pickup (non-terminal; C5 continues)
//   - zero + anything else → completed, with metering
//
// failureCause: explicit value wins; otherwise derived from tmpfsExhausted;
// otherwise NULL. Returns the new status, or ErrJobNotRunning when the job
// was not in 'running' status.
func CompleteJob(ctx context.Context, db *DB, jobID string, exitCode *int, failureCause string, tmpfsExhausted bool) (string, error) {
	var workloadType string
	if err := db.Pool.QueryRow(ctx,
		`SELECT workload_type::text FROM jobs WHERE id = $1`, jobID,
	).Scan(&workloadType); err != nil {
		return "", fmt.Errorf("complete job %s: read workload type: %w", jobID, err)
	}

	cause := failureCause
	if cause == "" && tmpfsExhausted {
		cause = "tmpfs_exhausted"
	}

	var newStatus string
	var shouldMeter bool
	switch {
	case exitCode == nil || *exitCode != 0:
		newStatus = "failed"
	case workloadType == "print_traditional" || workloadType == "print_3d":
		newStatus = "awaiting_pickup"
	default:
		newStatus = "completed"
		shouldMeter = true
	}

	// completed_at is set only on terminal statuses. awaiting_pickup is
	// non-terminal — completed_at gets set later when the job reaches
	// delivered (C5) or failed.
	var completedAt *time.Time
	if newStatus == "completed" || newStatus == "failed" {
		t := time.Now().UTC()
		completedAt = &t
	}

	// C5: set awaiting_pickup_at when transitioning into awaiting_pickup, so
	// the no-show window has a stable anchor. NULL on all other transitions.
	var awaitingPickupAt *time.Time
	if newStatus == "awaiting_pickup" {
		t := time.Now().UTC()
		awaitingPickupAt = &t
	}

	tag, err := db.Pool.Exec(ctx,
		`UPDATE jobs SET status = $4::job_status, completed_at = $5, awaiting_pickup_at = $6, updated_at = NOW(),
			exit_code = $2, failure_cause = NULLIF($3, '')
		 WHERE id = $1 AND status = 'running'::job_status`,
		jobID, exitCode, cause, newStatus, completedAt, awaitingPickupAt,
	)
	if err != nil {
		return "", fmt.Errorf("complete job %s: update: %w", jobID, err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrJobNotRunning
	}

	if shouldMeter {
		if err := ComputeMetering(ctx, db, jobID); err != nil {
			log.Printf("ComputeMetering job=%s error=%v", jobID, err)
		}
	}
	return newStatus, nil
}

// RecordNodeHeartbeat persists a node liveness signal: refreshes
// nodes.last_heartbeat_at and appends a node_heartbeat_events row (the uptime
// scorer's raw input).
func RecordNodeHeartbeat(ctx context.Context, db *DB, nodeID string) error {
	if _, err := db.Pool.Exec(ctx,
		`UPDATE nodes SET last_heartbeat_at = NOW(), updated_at = NOW() WHERE id = $1`,
		nodeID,
	); err != nil {
		return fmt.Errorf("record heartbeat %s: update node: %w", nodeID, err)
	}
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO node_heartbeat_events (node_id) VALUES ($1)`,
		nodeID,
	); err != nil {
		return fmt.Errorf("record heartbeat %s: insert event: %w", nodeID, err)
	}
	return nil
}
