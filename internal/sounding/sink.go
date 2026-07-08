package sounding

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// Recorder is the fire-and-forget telemetry surface the hot path depends on.
// Every method MUST be non-blocking and panic-free: callers on the placement
// hot path invoke these AFTER the placement decision and must never be slowed
// or failed by telemetry. A nil Recorder (interface value) is handled by the
// callers with a nil check; a nil *Sink is itself safe (see the methods).
type Recorder interface {
	RecordJobShape(JobShape)
	RecordRejection(Rejection)
	RecordCapacity(CapacitySnapshot)
}

// JobShape is one submitted job's shape, placed or not. Rung is "" (→ NULL)
// when Placed is false. Units: CPU in vCPU; MemMB/DiskMB in MB; DurationEst in
// seconds; Intensity/Footprint are normalized ratios (see Ladder).
type JobShape struct {
	OperatorID   string
	JobID        string // job UUID (text; cast to uuid on insert)
	WorkloadType string
	Intensity    float64
	DurationEst  int
	CPU          float64
	MemMB        int64
	DiskMB       int64
	Footprint    float64
	Placed       bool
	Rung         string
}

// Rejection is the purest unmet-demand signal: a job that found no home.
// Reason MUST be one of the Reason* constants. WantedRung is "" (→ NULL) when
// unknown.
type Rejection struct {
	OperatorID   string
	JobID        string
	WorkloadType string
	Reason       string
	Footprint    float64
	WantedRung   string
}

// CapacitySnapshot is one aggregated supply sample for an
// (operator_id, node_class, workload_type) group.
type CapacitySnapshot struct {
	OperatorID     string
	NodeClass      string
	WorkloadType   string
	NodesAvailable int
	VCPUs          int
	MemMB          int64
	DiskMB         int64
	PrintQPS       float64
}

// eventKind tags the union carried on the channel.
type eventKind uint8

const (
	kindJobShape eventKind = iota
	kindRejection
	kindCapacity
)

type envelope struct {
	kind  eventKind
	shape JobShape
	rej   Rejection
	cap   CapacitySnapshot
}

// batchWriter is the DB-facing seam. Kept unexported and injectable so tests
// can substitute a capturing / failing writer without a database. The
// production implementation is pgxWriter.
type batchWriter interface {
	WriteJobShapes(ctx context.Context, rows []JobShape) error
	WriteRejections(ctx context.Context, rows []Rejection) error
	WriteCapacity(ctx context.Context, rows []CapacitySnapshot) error
}

// Config tunes the sink. Zero values are replaced with defaults (DefaultConfig).
type Config struct {
	BufferSize    int           // channel capacity; sends beyond this are DROPPED
	BatchSize     int           // flush when this many events accumulate
	FlushInterval time.Duration // flush at least this often
	WriteTimeout  time.Duration // per-flush DB write deadline
}

// DefaultConfig returns production-sane defaults.
func DefaultConfig() Config {
	return Config{
		BufferSize:    4096,
		BatchSize:     128,
		FlushInterval: 2 * time.Second,
		WriteTimeout:  5 * time.Second,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.BufferSize <= 0 {
		c.BufferSize = d.BufferSize
	}
	if c.BatchSize <= 0 {
		c.BatchSize = d.BatchSize
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = d.FlushInterval
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = d.WriteTimeout
	}
	return c
}

// Sink is an async, drop-on-backpressure, fail-open telemetry sink. Construct
// with NewSink (or newSinkWithWriter in tests); the drain goroutine runs until
// the ctx passed at construction is cancelled.
type Sink struct {
	ch      chan envelope
	writer  batchWriter
	cfg     Config
	dropped atomic.Uint64
}

// NewSink builds a Sink backed by the given database and starts its drain
// goroutine bound to ctx. Cancelling ctx stops the goroutine after a final
// best-effort flush.
func NewSink(ctx context.Context, db *store.DB, cfg Config) *Sink {
	return newSinkWithWriter(ctx, newPGXWriter(db), cfg)
}

// newSinkWithWriter is the test seam: same as NewSink but with an injected writer.
func newSinkWithWriter(ctx context.Context, w batchWriter, cfg Config) *Sink {
	cfg = cfg.withDefaults()
	s := &Sink{
		ch:     make(chan envelope, cfg.BufferSize),
		writer: w,
		cfg:    cfg,
	}
	go s.run(ctx)
	return s
}

// Dropped returns the number of events dropped due to backpressure since start.
// Exposed for tests and future metrics; never used on the hot path.
func (s *Sink) Dropped() uint64 {
	if s == nil {
		return 0
	}
	return s.dropped.Load()
}

// enqueue performs a NON-BLOCKING send. When the buffer is full the event is
// dropped and the drop counter incremented — the hot path is never blocked.
// Safe on a nil *Sink.
func (s *Sink) enqueue(e envelope) {
	if s == nil {
		return
	}
	select {
	case s.ch <- e:
	default:
		s.dropped.Add(1)
	}
}

// RecordJobShape enqueues a job-shape event (non-blocking, nil-safe).
func (s *Sink) RecordJobShape(js JobShape) { s.enqueue(envelope{kind: kindJobShape, shape: js}) }

// RecordRejection enqueues a rejection event (non-blocking, nil-safe).
func (s *Sink) RecordRejection(r Rejection) { s.enqueue(envelope{kind: kindRejection, rej: r}) }

// RecordCapacity enqueues a capacity-snapshot event (non-blocking, nil-safe).
func (s *Sink) RecordCapacity(c CapacitySnapshot) {
	s.enqueue(envelope{kind: kindCapacity, cap: c})
}

// run is the single drain goroutine: it accumulates a batch and flushes on
// size or interval, then does a final flush on ctx cancellation.
func (s *Sink) run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]envelope, 0, s.cfg.BatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.writeBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Best-effort drain of anything already buffered, then stop.
			for {
				select {
				case e := <-s.ch:
					batch = append(batch, e)
					if len(batch) >= s.cfg.BatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case e := <-s.ch:
			batch = append(batch, e)
			if len(batch) >= s.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// writeBatch groups the batch by kind and writes each group. FAIL-OPEN: a
// write error is logged and the offending group is dropped; it never
// propagates and never crashes the goroutine. A fresh bounded context is used
// so a hung DB cannot wedge the drain indefinitely.
func (s *Sink) writeBatch(batch []envelope) {
	var (
		shapes []JobShape
		rejs   []Rejection
		caps   []CapacitySnapshot
	)
	for _, e := range batch {
		switch e.kind {
		case kindJobShape:
			shapes = append(shapes, e.shape)
		case kindRejection:
			rejs = append(rejs, e.rej)
		case kindCapacity:
			caps = append(caps, e.cap)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.WriteTimeout)
	defer cancel()

	if len(shapes) > 0 {
		if err := s.writer.WriteJobShapes(ctx, shapes); err != nil {
			slog.Warn("demand-sounding: job-shape write failed (dropped, fail-open)", "count", len(shapes), "error", err)
		}
	}
	if len(rejs) > 0 {
		if err := s.writer.WriteRejections(ctx, rejs); err != nil {
			slog.Warn("demand-sounding: rejection write failed (dropped, fail-open)", "count", len(rejs), "error", err)
		}
	}
	if len(caps) > 0 {
		if err := s.writer.WriteCapacity(ctx, caps); err != nil {
			slog.Warn("demand-sounding: capacity write failed (dropped, fail-open)", "count", len(caps), "error", err)
		}
	}
}

// compile-time assertion that *Sink satisfies Recorder.
var _ Recorder = (*Sink)(nil)
