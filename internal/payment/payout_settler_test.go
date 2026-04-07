package payment

import (
	"context"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newTestPayoutSettler(t *testing.T, autoThreshold int64) (*PayoutSettler, *store.Store, *Ledger) {
	t.Helper()
	s, err := store.NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	ledger := NewLedger(s, nil)
	ps := NewPayoutSettler(s, ledger, "did:soho:provider1", 10*time.Minute, autoThreshold, "")
	return ps, s, ledger
}

// ── construction ─────────────────────────────────────────────────────────────

func TestNewPayoutSettler_Fields(t *testing.T) {
	ps, _, _ := newTestPayoutSettler(t, 50_000)

	if ps.providerDID != "did:soho:provider1" {
		t.Errorf("providerDID = %q, want %q", ps.providerDID, "did:soho:provider1")
	}
	if ps.AutoPayoutThresholdSats != 50_000 {
		t.Errorf("AutoPayoutThresholdSats = %d, want 50000", ps.AutoPayoutThresholdSats)
	}
	if ps.interval != 10*time.Minute {
		t.Errorf("interval = %s, want 10m", ps.interval)
	}
}

// ── tick with no pending payouts ─────────────────────────────────────────────

func TestPayoutSettler_RetryPendingPayouts_Empty(t *testing.T) {
	ps, _, _ := newTestPayoutSettler(t, 0)
	ctx := context.Background()
	// Should complete without error or panic when queue is empty.
	ps.retryPendingPayouts(ctx)
}

// ── auto-payout threshold disabled ───────────────────────────────────────────

func TestPayoutSettler_AutoPayoutDisabled(t *testing.T) {
	ps, _, _ := newTestPayoutSettler(t, 0) // threshold 0 = disabled
	ctx := context.Background()
	// tick should not invoke checkAutoPayoutThreshold when threshold = 0
	ps.tick(ctx) // must not panic
}

// ── auto-payout threshold: below threshold ────────────────────────────────────

func TestPayoutSettler_AutoPayout_BelowThreshold(t *testing.T) {
	ps, _, _ := newTestPayoutSettler(t, 100_000)
	ctx := context.Background()
	// GetPendingPayout will return 0 on empty store — below threshold.
	ps.checkAutoPayoutThreshold(ctx) // must not panic or request a payout
}

// ── Run context cancellation ──────────────────────────────────────────────────

func TestPayoutSettler_Run_CtxCancel(t *testing.T) {
	ps, _, _ := newTestPayoutSettler(t, 0)
	// Use a very long interval so the ticker never fires during the test.
	ps.interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ps.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("PayoutSettler.Run did not stop after context cancellation")
	}
}

// ── dispatchPayout with no processors ────────────────────────────────────────

func TestPayoutSettler_DispatchPayout_NoProcessors(t *testing.T) {
	ps, _, _ := newTestPayoutSettler(t, 0)
	ctx := context.Background()
	row := store.PayoutRow{
		PayoutID:    "payout-1",
		ProviderDID: "did:soho:provider1",
		AmountSats:  10_000,
		Status:      "pending",
	}
	// Ledger has no processors registered — should return silently.
	ps.dispatchPayout(ctx, row)
}

// ── GetPayoutsByStatus integration ───────────────────────────────────────────

func TestGetPayoutsByStatus_Empty(t *testing.T) {
	s, err := store.NewMemoryStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rows, err := s.GetPayoutsByStatus(context.Background(), "pending", 10)
	if err != nil {
		t.Fatalf("GetPayoutsByStatus: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}
