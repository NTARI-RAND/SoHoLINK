package sounding

import (
	"context"
	"fmt"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// This file is the READ-MODEL for the LOCAL-ONLY :8090 demand-sounding dashboard
// (internal/api/governance_sounding.go). Every method here is a PURE READ over the
// migration-025 hypertables: it opens no transaction that mutates, writes no row,
// and never touches the placement hot path. The write side (sink.go / writer_pgx.go)
// and the read side share only the table shapes, never a code path.
//
// Every query takes an operatorID selector: an empty string means the
// ALL-OPERATORS AGGREGATE (no operator_id predicate), a non-empty value slices to
// that single operator. This is the per-operator-AND-aggregate discipline the
// dashboard's operator dropdown drives.
//
// Windowing: callers pass a Postgres interval literal for the lookback window
// (e.g. "7 days") and a bucket width for the time-series queries (e.g. "1 day").
// The queries lean on TimescaleDB's time_bucket, available because migration 025
// enables the extension. Buckets are returned only where rows exist; the handler
// zero-fills a continuous axis.

// Reader is the demand-sounding read model. Construct with NewReader over the
// same *store.DB the coordinator already holds; it uses the pool directly and
// holds no state.
type Reader struct {
	db *store.DB
}

// NewReader returns a Reader over db. A nil db yields a Reader whose methods
// return an error on first use rather than panicking (the handler treats that as
// an unconfigured surface).
func NewReader(db *store.DB) *Reader { return &Reader{db: db} }

// opArgs builds the operator predicate fragment and the argument list for a query
// whose fixed leading args are already in `base`. When operatorID is empty the
// fragment is empty (all-operators aggregate); otherwise it appends
// "AND operator_id = $N" and the value. The $N index is len(base)+1 because the
// fixed args occupy $1..$len(base).
func opArgs(operatorID string, base ...any) (string, []any) {
	if operatorID == "" {
		return "", base
	}
	frag := fmt.Sprintf(" AND operator_id = $%d", len(base)+1)
	return frag, append(base, operatorID)
}

// -----------------------------------------------------------------------------
// Operator list (dropdown population).
// -----------------------------------------------------------------------------

// Operators returns the distinct operator_ids that appear anywhere in the three
// event tables, sorted. It is the source for the dashboard's operator dropdown
// (plus an "all operators" aggregate the handler prepends). Pure read.
func (r *Reader) Operators(ctx context.Context) ([]string, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT operator_id FROM (
			SELECT DISTINCT operator_id FROM operator_job_shapes
			UNION
			SELECT DISTINCT operator_id FROM operator_placement_rejections
			UNION
			SELECT DISTINCT operator_id FROM operator_capacity_snapshots
		) s
		ORDER BY operator_id`)
	if err != nil {
		return nil, fmt.Errorf("sounding: list operators: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var op string
		if err := rows.Scan(&op); err != nil {
			return nil, fmt.Errorf("sounding: scan operator: %w", err)
		}
		out = append(out, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sounding: iterate operators: %w", err)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Headline totals (stat grid).
// -----------------------------------------------------------------------------

// Totals is the sounding headline: how many jobs were submitted in the window,
// how many placed, how many towered against the top available ceiling
// (footprint >= 1.0 — the congestus signal), and how many were rejected (the
// purest unmet-demand count).
type Totals struct {
	Jobs           int
	Placed         int
	AgainstCeiling int
	Rejections     int
}

// Totals returns the headline counts over the window for the given operator
// (empty = all-operators aggregate). Pure read.
func (r *Reader) Totals(ctx context.Context, operatorID, window string) (Totals, error) {
	var t Totals

	frag, args := opArgs(operatorID, window)
	err := r.db.Pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE placed),
		       COUNT(*) FILTER (WHERE footprint >= 1.0)
		FROM operator_job_shapes
		WHERE time > NOW() - $1::interval`+frag, args...).
		Scan(&t.Jobs, &t.Placed, &t.AgainstCeiling)
	if err != nil {
		return Totals{}, fmt.Errorf("sounding: job totals: %w", err)
	}

	frag, args = opArgs(operatorID, window)
	if err := r.db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM operator_placement_rejections
		WHERE time > NOW() - $1::interval`+frag, args...).
		Scan(&t.Rejections); err != nil {
		return Totals{}, fmt.Errorf("sounding: rejection total: %w", err)
	}
	return t, nil
}

// -----------------------------------------------------------------------------
// Chart 1: job-shape distribution across the rung ceilings.
// -----------------------------------------------------------------------------

// RungCount is the number of submitted jobs that fit a given rung. Rung is "" for
// jobs that were not placed (no rung recorded); the handler renders that as an
// "unplaced" bucket alongside the ladder rungs.
type RungCount struct {
	Rung  string
	Count int
}

// JobShapeDistribution returns the count of submitted jobs per rung over the
// window, keyed by the rung the job fit ("" = unplaced). Combined with the rung
// ladder (Ladder.Tiers) the handler renders demand distribution against the tier
// ceilings — showing whether demand is piling at the top rung (congestus). Pure read.
func (r *Reader) JobShapeDistribution(ctx context.Context, operatorID, window string) ([]RungCount, error) {
	frag, args := opArgs(operatorID, window)
	rows, err := r.db.Pool.Query(ctx, `
		SELECT COALESCE(rung, '') AS rung, COUNT(*)
		FROM operator_job_shapes
		WHERE time > NOW() - $1::interval`+frag+`
		GROUP BY COALESCE(rung, '')`, args...)
	if err != nil {
		return nil, fmt.Errorf("sounding: job-shape distribution: %w", err)
	}
	defer rows.Close()

	var out []RungCount
	for rows.Next() {
		var rc RungCount
		if err := rows.Scan(&rc.Rung, &rc.Count); err != nil {
			return nil, fmt.Errorf("sounding: scan rung count: %w", err)
		}
		out = append(out, rc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sounding: iterate rung counts: %w", err)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Chart 2: jobs towering against the top ceiling, over time.
// -----------------------------------------------------------------------------

// CongestusBucket is one time bucket of the congestus series: how many jobs were
// submitted and, of those, how many towered against the top available ceiling
// (footprint >= 1.0). Bucket is the bucket-start (UTC), aligned to time_bucket.
type CongestusBucket struct {
	Bucket         time.Time
	Total          int
	AgainstCeiling int
}

// CongestusSeries returns the per-bucket count of submitted jobs and the subset
// towering against the top available ceiling, over the window. Pure read.
func (r *Reader) CongestusSeries(ctx context.Context, operatorID, window, bucket string) ([]CongestusBucket, error) {
	frag, args := opArgs(operatorID, window, bucket)
	rows, err := r.db.Pool.Query(ctx, `
		SELECT time_bucket($2::interval, time) AS b,
		       COUNT(*),
		       COUNT(*) FILTER (WHERE footprint >= 1.0)
		FROM operator_job_shapes
		WHERE time > NOW() - $1::interval`+frag+`
		GROUP BY b
		ORDER BY b`, args...)
	if err != nil {
		return nil, fmt.Errorf("sounding: congestus series: %w", err)
	}
	defer rows.Close()

	var out []CongestusBucket
	for rows.Next() {
		var c CongestusBucket
		if err := rows.Scan(&c.Bucket, &c.Total, &c.AgainstCeiling); err != nil {
			return nil, fmt.Errorf("sounding: scan congestus bucket: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sounding: iterate congestus buckets: %w", err)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Chart 3: rejections by reason, over time.
// -----------------------------------------------------------------------------

// RejectionCell is one (bucket, reason) count from the rejection series. The
// handler pivots these into a stacked series with a fixed categorical reason
// order.
type RejectionCell struct {
	Bucket time.Time
	Reason string
	Count  int
}

// RejectionsByReason returns per-(bucket, reason) rejection counts over the
// window — the purest unmet-demand signal, sliced by cause. Pure read.
func (r *Reader) RejectionsByReason(ctx context.Context, operatorID, window, bucket string) ([]RejectionCell, error) {
	frag, args := opArgs(operatorID, window, bucket)
	rows, err := r.db.Pool.Query(ctx, `
		SELECT time_bucket($2::interval, time) AS b, reason, COUNT(*)
		FROM operator_placement_rejections
		WHERE time > NOW() - $1::interval`+frag+`
		GROUP BY b, reason
		ORDER BY b`, args...)
	if err != nil {
		return nil, fmt.Errorf("sounding: rejections by reason: %w", err)
	}
	defer rows.Close()

	var out []RejectionCell
	for rows.Next() {
		var c RejectionCell
		if err := rows.Scan(&c.Bucket, &c.Reason, &c.Count); err != nil {
			return nil, fmt.Errorf("sounding: scan rejection cell: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sounding: iterate rejection cells: %w", err)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Chart 4: capacity vs demand, over time (single axis: vCPU).
// -----------------------------------------------------------------------------

// LevelBucket is one time bucket carrying two vCPU levels on ONE axis: the
// average vCPU requested per submitted job (demand) and the average compute vCPU
// available per contributing node group (capacity). Averages (levels), not sums,
// so both series share a comparable vCPU scale — the "one axis" discipline. A
// bucket may carry only one side; the missing side is left at its zero value and
// the handler marks it absent.
type LevelBucket struct {
	Bucket       time.Time
	DemandVCPU   float64 // AVG(cpu) of submitted jobs
	HasDemand    bool
	CapacityVCPU float64 // AVG(vcpus) of compute capacity snapshots
	HasCapacity  bool
}

// CapacityVsDemand returns, per bucket over the window, the average requested
// vCPU per job (demand) and the average available compute vCPU per node group
// (capacity). Both are per-bucket AVERAGES so they compare on one vCPU axis. The
// two sides are read separately and merged by bucket. Pure read.
func (r *Reader) CapacityVsDemand(ctx context.Context, operatorID, window, bucket string) ([]LevelBucket, error) {
	byBucket := make(map[time.Time]*LevelBucket)
	order := []time.Time{}
	touch := func(b time.Time) *LevelBucket {
		lb, ok := byBucket[b]
		if !ok {
			lb = &LevelBucket{Bucket: b}
			byBucket[b] = lb
			order = append(order, b)
		}
		return lb
	}

	// Demand: average requested vCPU per job.
	frag, args := opArgs(operatorID, window, bucket)
	dRows, err := r.db.Pool.Query(ctx, `
		SELECT time_bucket($2::interval, time) AS b, AVG(cpu)
		FROM operator_job_shapes
		WHERE time > NOW() - $1::interval`+frag+`
		GROUP BY b
		ORDER BY b`, args...)
	if err != nil {
		return nil, fmt.Errorf("sounding: demand series: %w", err)
	}
	defer dRows.Close()
	for dRows.Next() {
		var (
			b   time.Time
			avg *float64
		)
		if err := dRows.Scan(&b, &avg); err != nil {
			return nil, fmt.Errorf("sounding: scan demand bucket: %w", err)
		}
		lb := touch(b)
		if avg != nil {
			lb.DemandVCPU = *avg
			lb.HasDemand = true
		}
	}
	if err := dRows.Err(); err != nil {
		return nil, fmt.Errorf("sounding: iterate demand buckets: %w", err)
	}

	// Capacity: average available compute vCPU per node group.
	frag, args = opArgs(operatorID, window, bucket)
	cRows, err := r.db.Pool.Query(ctx, `
		SELECT time_bucket($2::interval, time) AS b, AVG(vcpus)
		FROM operator_capacity_snapshots
		WHERE workload_type = 'compute'
		  AND time > NOW() - $1::interval`+frag+`
		GROUP BY b
		ORDER BY b`, args...)
	if err != nil {
		return nil, fmt.Errorf("sounding: capacity series: %w", err)
	}
	defer cRows.Close()
	for cRows.Next() {
		var (
			b   time.Time
			avg *float64
		)
		if err := cRows.Scan(&b, &avg); err != nil {
			return nil, fmt.Errorf("sounding: scan capacity bucket: %w", err)
		}
		lb := touch(b)
		if avg != nil {
			lb.CapacityVCPU = *avg
			lb.HasCapacity = true
		}
	}
	if err := cRows.Err(); err != nil {
		return nil, fmt.Errorf("sounding: iterate capacity buckets: %w", err)
	}

	out := make([]LevelBucket, 0, len(order))
	for _, b := range order {
		out = append(out, *byBucket[b])
	}
	return out, nil
}

// LoadLadderReader loads the rung ladder (rung_tiers, incl. coming_soon) for the
// dashboard. It is a thin convenience over LoadLadder bound to the Reader's db so
// the handler has a single dependency. On error the caller renders with an empty
// ladder (fail-soft) rather than failing the page.
func (r *Reader) LoadLadderReader(ctx context.Context) (Ladder, error) {
	return LoadLadder(ctx, r.db)
}
