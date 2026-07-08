package sounding

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// pgxWriter is the production batchWriter: it writes each group to its
// migration-025 hypertable with a single pgx.Batch round-trip. The event
// tables carry NO foreign keys and NO enum coupling (referential columns are
// TEXT, job_id is cast from text to uuid), so a benign race — an operator row
// pruned, a workload string the schema never enumerated — cannot fail an
// INSERT. Any error that does occur is returned to the drain loop, which logs
// and drops it (fail-open).
type pgxWriter struct {
	db *store.DB
}

func newPGXWriter(db *store.DB) *pgxWriter { return &pgxWriter{db: db} }

// nullStr maps "" to a nil interface so a TEXT column receives SQL NULL
// (rung / wanted_rung are nullable in migration 025).
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (w *pgxWriter) WriteJobShapes(ctx context.Context, rows []JobShape) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue(`
			INSERT INTO operator_job_shapes
				(operator_id, job_id, workload_type, intensity, duration_est,
				 cpu, mem_mb, disk_mb, footprint, placed, rung)
			VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			r.OperatorID, r.JobID, r.WorkloadType, r.Intensity, r.DurationEst,
			r.CPU, r.MemMB, r.DiskMB, r.Footprint, r.Placed, nullStr(r.Rung))
	}
	return sendBatch(ctx, w.db, b)
}

func (w *pgxWriter) WriteRejections(ctx context.Context, rows []Rejection) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue(`
			INSERT INTO operator_placement_rejections
				(operator_id, job_id, workload_type, reason, footprint, wanted_rung)
			VALUES ($1, $2::uuid, $3, $4, $5, $6)`,
			r.OperatorID, r.JobID, r.WorkloadType, r.Reason, r.Footprint, nullStr(r.WantedRung))
	}
	return sendBatch(ctx, w.db, b)
}

func (w *pgxWriter) WriteCapacity(ctx context.Context, rows []CapacitySnapshot) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue(`
			INSERT INTO operator_capacity_snapshots
				(operator_id, node_class, workload_type, nodes_available,
				 vcpus, mem_mb, disk_mb, print_qps)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			r.OperatorID, r.NodeClass, r.WorkloadType, r.NodesAvailable,
			r.VCPUs, r.MemMB, r.DiskMB, r.PrintQPS)
	}
	return sendBatch(ctx, w.db, b)
}

// sendBatch executes every queued statement and returns the first error. The
// caller (drain loop) is responsible for fail-open handling.
func sendBatch(ctx context.Context, db *store.DB, b *pgx.Batch) error {
	br := db.Pool.SendBatch(ctx, b)
	defer br.Close() //nolint:errcheck // Close error is subsumed by the Exec errors below
	for i := 0; i < b.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}
