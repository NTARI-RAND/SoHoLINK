package tpm

import (
	"context"
	"errors"
	"testing"
)

// TestNoopAttester verifies that NoopAttester satisfies the Attester interface
// and returns ErrNotAvailable for every method.
func TestNoopAttester_Available(t *testing.T) {
	a := &NoopAttester{}
	if a.Available() {
		t.Error("NoopAttester.Available() should return false")
	}
}

func TestNoopAttester_Quote(t *testing.T) {
	a := &NoopAttester{}
	ctx := context.Background()
	_, _, err := a.Quote(ctx, []int{0, 1, 7}, []byte("nonce"))
	if !errors.Is(err, ErrNotAvailable) {
		t.Errorf("Quote() error = %v, want ErrNotAvailable", err)
	}
}

func TestNoopAttester_EKCert(t *testing.T) {
	a := &NoopAttester{}
	_, err := a.EKCert(context.Background())
	if !errors.Is(err, ErrNotAvailable) {
		t.Errorf("EKCert() error = %v, want ErrNotAvailable", err)
	}
}

func TestNoopAttester_PCRValues(t *testing.T) {
	a := &NoopAttester{}
	_, err := a.PCRValues(context.Background(), []int{0, 1, 2})
	if !errors.Is(err, ErrNotAvailable) {
		t.Errorf("PCRValues() error = %v, want ErrNotAvailable", err)
	}
}

// TestNewAttester_ReturnsAttester ensures NewAttester() always returns a
// non-nil Attester (even if it is the noop implementation on this platform).
func TestNewAttester_ReturnsAttester(t *testing.T) {
	a := NewAttester()
	if a == nil {
		t.Fatal("NewAttester() returned nil")
	}
	// Available() is allowed to be false on CI hosts without a TPM.
	// Just confirm it doesn't panic.
	_ = a.Available()
}

// TestAttesterInterface verifies that *NoopAttester satisfies Attester at
// compile time — if this file compiles, the interface is satisfied.
var _ Attester = (*NoopAttester)(nil)
