package wasm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// StubExecutor tests
// ---------------------------------------------------------------------------

func TestNewStubExecutor(t *testing.T) {
	exec := NewStubExecutor()
	if exec == nil {
		t.Fatal("NewStubExecutor returned nil")
	}
	// Must satisfy the Executor interface
	var _ Executor = exec
}

func TestStubExecutor_Execute_ReturnsErrNotImplemented(t *testing.T) {
	exec := NewStubExecutor()
	result, err := exec.Execute(context.Background(), []byte("module"), []byte("input"))
	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestStubExecutor_Execute_NilInputs(t *testing.T) {
	exec := NewStubExecutor()
	result, err := exec.Execute(context.Background(), nil, nil)
	if result != nil {
		t.Error("expected nil result for nil inputs")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestStubExecutor_Execute_EmptyInputs(t *testing.T) {
	exec := NewStubExecutor()
	result, err := exec.Execute(context.Background(), []byte{}, []byte{})
	if result != nil {
		t.Error("expected nil result for empty inputs")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got: %v", err)
	}
}

func TestStubExecutor_Close(t *testing.T) {
	exec := NewStubExecutor()
	err := exec.Close(context.Background())
	if err != nil {
		t.Errorf("Close should return nil, got: %v", err)
	}
}

func TestStubExecutor_Close_CancelledContext(t *testing.T) {
	exec := NewStubExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := exec.Close(ctx)
	if err != nil {
		t.Errorf("StubExecutor.Close should succeed even with cancelled ctx, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sentinel error tests
// ---------------------------------------------------------------------------

func TestErrTimeout_IsSentinel(t *testing.T) {
	if ErrTimeout == nil {
		t.Fatal("ErrTimeout should not be nil")
	}
	if ErrTimeout.Error() != "wasm: execution timed out" {
		t.Errorf("unexpected message: %s", ErrTimeout.Error())
	}
}

func TestErrNotImplemented_IsSentinel(t *testing.T) {
	if ErrNotImplemented == nil {
		t.Fatal("ErrNotImplemented should not be nil")
	}
	if ErrNotImplemented.Error() != "wasm: executor not yet implemented (planned v0.3)" {
		t.Errorf("unexpected message: %s", ErrNotImplemented.Error())
	}
}

// ---------------------------------------------------------------------------
// WithTimeout wrapper tests
// ---------------------------------------------------------------------------

func TestWithTimeout_ZeroSeconds_ReturnsOriginal(t *testing.T) {
	stub := NewStubExecutor()
	wrapped := WithTimeout(stub, 0)
	if wrapped != stub {
		t.Error("WithTimeout(exec, 0) should return the original executor")
	}
}

func TestWithTimeout_NegativeSeconds_ReturnsOriginal(t *testing.T) {
	stub := NewStubExecutor()
	wrapped := WithTimeout(stub, -5)
	if wrapped != stub {
		t.Error("WithTimeout(exec, -5) should return the original executor")
	}
}

func TestWithTimeout_PositiveSeconds_ReturnsWrapper(t *testing.T) {
	stub := NewStubExecutor()
	wrapped := WithTimeout(stub, 10)
	if wrapped == stub {
		t.Error("WithTimeout(exec, 10) should return a wrapper, not the original")
	}
	// Must still satisfy Executor
	var _ Executor = wrapped
}

func TestWithTimeout_PassesThroughError(t *testing.T) {
	stub := NewStubExecutor()
	wrapped := WithTimeout(stub, 60) // generous timeout
	_, err := wrapped.Execute(context.Background(), nil, nil)
	// StubExecutor returns ErrNotImplemented immediately (no timeout)
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented passthrough, got: %v", err)
	}
}

func TestWithTimeout_Close_DelegatesToInner(t *testing.T) {
	stub := NewStubExecutor()
	wrapped := WithTimeout(stub, 10)
	err := wrapped.Close(context.Background())
	if err != nil {
		t.Errorf("Close should delegate to inner (nil), got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// WithTimeout — timeout behavior with a mock executor
// ---------------------------------------------------------------------------

// slowExecutor is a test-only Executor that blocks until context cancellation.
type slowExecutor struct {
	closed bool
}

func (s *slowExecutor) Execute(ctx context.Context, _ []byte, _ []byte) (*ExecuteResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *slowExecutor) Close(_ context.Context) error {
	s.closed = true
	return nil
}

func TestWithTimeout_Fires_ErrTimeout(t *testing.T) {
	slow := &slowExecutor{}
	wrapped := WithTimeout(slow, 1) // 1 second timeout

	start := time.Now()
	_, err := wrapped.Execute(context.Background(), []byte("mod"), []byte("in"))
	elapsed := time.Since(start)

	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout, got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("timeout took too long: %v (expected ~1s)", elapsed)
	}
}

func TestWithTimeout_ParentCancellation(t *testing.T) {
	slow := &slowExecutor{}
	wrapped := WithTimeout(slow, 60) // long timeout

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := wrapped.Execute(ctx, []byte("mod"), []byte("in"))
	// Parent cancelled, not our timeout — should NOT be ErrTimeout
	// The inner executor returns ctx.Err() which is context.Canceled,
	// and the timed wrapper checks tctx.Err() for DeadlineExceeded
	if errors.Is(err, ErrTimeout) {
		t.Error("parent cancellation should not produce ErrTimeout")
	}
	if err == nil {
		t.Error("expected an error from cancelled context")
	}
}

// successExecutor returns a canned result immediately.
type successExecutor struct{}

func (s *successExecutor) Execute(_ context.Context, _ []byte, _ []byte) (*ExecuteResult, error) {
	return &ExecuteResult{
		Output:     []byte("hello"),
		ResultHash: "abc123",
		DurationMs: 42,
	}, nil
}

func (s *successExecutor) Close(_ context.Context) error { return nil }

func TestWithTimeout_SuccessPassthrough(t *testing.T) {
	exec := &successExecutor{}
	wrapped := WithTimeout(exec, 10)

	result, err := wrapped.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if string(result.Output) != "hello" {
		t.Errorf("expected output 'hello', got %q", string(result.Output))
	}
	if result.ResultHash != "abc123" {
		t.Errorf("expected hash abc123, got %s", result.ResultHash)
	}
	if result.DurationMs != 42 {
		t.Errorf("expected 42ms, got %d", result.DurationMs)
	}
}

// ---------------------------------------------------------------------------
// ExecuteResult / TaskManifest struct tests
// ---------------------------------------------------------------------------

func TestExecuteResult_ZeroValue(t *testing.T) {
	var r ExecuteResult
	if r.Output != nil {
		t.Error("zero ExecuteResult should have nil Output")
	}
	if r.ResultHash != "" {
		t.Error("zero ExecuteResult should have empty ResultHash")
	}
	if r.DurationMs != 0 {
		t.Error("zero ExecuteResult should have 0 DurationMs")
	}
}

func TestTaskManifest_Defaults(t *testing.T) {
	tm := TaskManifest{
		Name:               "test-task",
		Version:            "1.0.0",
		MaxDurationSeconds: 60,
	}
	if tm.WasmFilename != "" {
		t.Error("WasmFilename should default to empty (caller uses 'task.wasm')")
	}
	if tm.InputDir != "" {
		t.Error("InputDir should default to empty (caller uses 'inputs/')")
	}
	if tm.MemoryLimitMB != 0 {
		t.Error("MemoryLimitMB should default to 0")
	}
	if len(tm.Arch) != 0 {
		t.Error("Arch should default to empty (any architecture)")
	}
}

// errorOnCloseExecutor returns an error from Close.
type errorOnCloseExecutor struct{}

func (e *errorOnCloseExecutor) Execute(_ context.Context, _ []byte, _ []byte) (*ExecuteResult, error) {
	return nil, ErrNotImplemented
}

func (e *errorOnCloseExecutor) Close(_ context.Context) error {
	return errors.New("close failed")
}

func TestWithTimeout_Close_PropagatesError(t *testing.T) {
	exec := &errorOnCloseExecutor{}
	wrapped := WithTimeout(exec, 10)
	err := wrapped.Close(context.Background())
	if err == nil {
		t.Fatal("expected error from inner Close")
	}
	if err.Error() != "close failed" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}
