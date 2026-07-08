package sounding

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// captureWriter records everything written; optionally fails.
type captureWriter struct {
	mu     sync.Mutex
	shapes []JobShape
	rejs   []Rejection
	caps   []CapacitySnapshot
	fail   bool
}

func (w *captureWriter) WriteJobShapes(_ context.Context, rows []JobShape) error {
	if w.fail {
		return errors.New("boom")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.shapes = append(w.shapes, rows...)
	return nil
}

func (w *captureWriter) WriteRejections(_ context.Context, rows []Rejection) error {
	if w.fail {
		return errors.New("boom")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rejs = append(w.rejs, rows...)
	return nil
}

func (w *captureWriter) WriteCapacity(_ context.Context, rows []CapacitySnapshot) error {
	if w.fail {
		return errors.New("boom")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.caps = append(w.caps, rows...)
	return nil
}

func (w *captureWriter) counts() (int, int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.shapes), len(w.rejs), len(w.caps)
}

// waitFor polls fn until it returns true or the deadline elapses.
func waitFor(t *testing.T, d time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return fn()
}

func TestSink_BatchesAndWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &captureWriter{}
	s := newSinkWithWriter(ctx, w, Config{BufferSize: 64, BatchSize: 2, FlushInterval: 5 * time.Millisecond})

	s.RecordJobShape(JobShape{OperatorID: "op1", JobID: "j1", WorkloadType: "compute", Placed: true, Rung: "cumulus"})
	s.RecordRejection(Rejection{OperatorID: "op1", JobID: "j2", WorkloadType: "compute", Reason: ReasonNoCapacity})
	s.RecordCapacity(CapacitySnapshot{OperatorID: "op1", NodeClass: "A", WorkloadType: WorkloadCompute, NodesAvailable: 3})

	if !waitFor(t, time.Second, func() bool {
		sh, rj, cp := w.counts()
		return sh == 1 && rj == 1 && cp == 1
	}) {
		sh, rj, cp := w.counts()
		t.Fatalf("writes not observed: shapes=%d rejs=%d caps=%d", sh, rj, cp)
	}
}

// TestSink_FailOpen_WriteErrorSwallowed proves a broken writer never crashes
// the drain goroutine or blocks the producer; the sink keeps accepting events.
func TestSink_FailOpen_WriteErrorSwallowed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &captureWriter{fail: true}
	s := newSinkWithWriter(ctx, w, Config{BufferSize: 64, BatchSize: 1, FlushInterval: 2 * time.Millisecond})

	for i := 0; i < 20; i++ {
		s.RecordRejection(Rejection{OperatorID: "op", JobID: "j", Reason: ReasonTooBig})
	}
	// Give the drain time to attempt (and fail) several writes.
	time.Sleep(50 * time.Millisecond)

	// Nothing captured (all writes errored), no panic, drops stay 0 (buffer
	// never overflowed), and the sink still accepts new events.
	if sh, rj, cp := w.counts(); sh+rj+cp != 0 {
		t.Fatalf("expected no captured rows on failing writer, got shapes=%d rejs=%d caps=%d", sh, rj, cp)
	}
	if d := s.Dropped(); d != 0 {
		t.Fatalf("expected 0 drops (buffer not overflowed), got %d", d)
	}
	s.RecordRejection(Rejection{OperatorID: "op", JobID: "j2", Reason: ReasonNoCapacity}) // must not panic/block
}

// TestSink_DropsOnBackpressure proves that when the buffer is saturated (no
// drain able to keep up) sends are dropped rather than blocking the caller.
func TestSink_DropsOnBackpressure(t *testing.T) {
	// A writer that blocks forever inside the flush, so the single drain
	// goroutine parks on the first batch and the buffer fills.
	blockCh := make(chan struct{})
	bw := &blockingWriter{release: blockCh}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(blockCh)

	s := newSinkWithWriter(ctx, bw, Config{BufferSize: 4, BatchSize: 1, FlushInterval: time.Hour})

	// Enqueue far more than buffer+in-flight capacity. This must never block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			s.RecordJobShape(JobShape{OperatorID: "op", JobID: "j"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("producer blocked on a full buffer — drop-on-backpressure failed")
	}

	if !waitFor(t, time.Second, func() bool { return s.Dropped() > 0 }) {
		t.Fatalf("expected drops under backpressure, got %d", s.Dropped())
	}
}

type blockingWriter struct{ release <-chan struct{} }

func (b *blockingWriter) WriteJobShapes(ctx context.Context, _ []JobShape) error {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return nil
}
func (b *blockingWriter) WriteRejections(context.Context, []Rejection) error      { return nil }
func (b *blockingWriter) WriteCapacity(context.Context, []CapacitySnapshot) error { return nil }

// TestSink_NilSafe proves the enqueue methods are safe on a nil *Sink and that
// a nil Recorder is usable by callers guarding with a nil check.
func TestSink_NilSafe(t *testing.T) {
	var s *Sink
	s.RecordJobShape(JobShape{})
	s.RecordRejection(Rejection{})
	s.RecordCapacity(CapacitySnapshot{})
	if s.Dropped() != 0 {
		t.Fatalf("nil sink Dropped(): got %d, want 0", s.Dropped())
	}
}

func TestContextOperatorID(t *testing.T) {
	base := context.Background()
	if id, ok := OperatorIDFromContext(base); ok || id != "" {
		t.Fatalf("empty context: got (%q,%v), want (\"\",false)", id, ok)
	}
	if got := OperatorIDOrUnknown(base); got != OperatorUnknown {
		t.Fatalf("OperatorIDOrUnknown empty: got %q, want %q", got, OperatorUnknown)
	}
	ctx := ContextWithOperatorID(base, "cloudy")
	if id, ok := OperatorIDFromContext(ctx); !ok || id != "cloudy" {
		t.Fatalf("with operator: got (%q,%v), want (\"cloudy\",true)", id, ok)
	}
	if got := OperatorIDOrUnknown(ctx); got != "cloudy" {
		t.Fatalf("OperatorIDOrUnknown: got %q, want %q", got, "cloudy")
	}
	// Present-but-empty reports absent.
	if _, ok := OperatorIDFromContext(ContextWithOperatorID(base, "")); ok {
		t.Fatal("present-but-empty operator id should report ok=false")
	}
}
