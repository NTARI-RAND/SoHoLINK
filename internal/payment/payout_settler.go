package payment

import (
	"context"
	"log"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// PayoutSettler periodically retries pending provider payouts and optionally
// triggers automatic payouts when a provider's unsettled balance exceeds a
// configured threshold.
//
// It complements OfflineSettler (which handles buyer-side charge retries) by
// covering the provider-side payout dispatch lifecycle:
//
//	pending → (processor online) → processing → settled
//	pending → (all processors fail) → stays pending, retried next tick
type PayoutSettler struct {
	store       *store.Store
	ledger      *Ledger
	providerDID string        // this node's own DID, used for auto-payout requests
	interval    time.Duration
	// AutoPayoutThresholdSats triggers an automatic payout request when
	// pending balance exceeds this value. Zero disables auto-payout.
	AutoPayoutThresholdSats int64
	// AutoPayoutProcessor selects which processor to use for auto-payouts.
	// Empty string means the ledger chooses the first available processor.
	AutoPayoutProcessor string
}

// NewPayoutSettler creates a PayoutSettler. providerDID is this node's own DID
// (used when auto-requesting a payout). interval is how often to retry pending
// payouts (recommended: 10–15 minutes). Set autoThresholdSats > 0 to enable
// balance-triggered automatic payout dispatch.
func NewPayoutSettler(s *store.Store, l *Ledger, providerDID string, interval time.Duration, autoThresholdSats int64, autoProcessor string) *PayoutSettler {
	return &PayoutSettler{
		store:                   s,
		ledger:                  l,
		providerDID:             providerDID,
		interval:                interval,
		AutoPayoutThresholdSats: autoThresholdSats,
		AutoPayoutProcessor:     autoProcessor,
	}
}

// Run starts the payout settlement loop. Blocks until ctx is cancelled.
func (ps *PayoutSettler) Run(ctx context.Context) {
	ticker := time.NewTicker(ps.interval)
	defer ticker.Stop()

	// Run immediately on startup so pending payouts left over from a restart
	// are dispatched without waiting a full interval.
	ps.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ps.tick(ctx)
		}
	}
}

func (ps *PayoutSettler) tick(ctx context.Context) {
	ps.retryPendingPayouts(ctx)
	if ps.AutoPayoutThresholdSats > 0 {
		ps.checkAutoPayoutThreshold(ctx)
	}
}

// retryPendingPayouts fetches all payouts in "pending" status and attempts to
// dispatch them through an available processor.
func (ps *PayoutSettler) retryPendingPayouts(ctx context.Context) {
	pending, err := ps.store.GetPayoutsByStatus(ctx, "pending", 100)
	if err != nil {
		log.Printf("[payout_settler] query pending payouts: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}
	log.Printf("[payout_settler] retrying %d pending payout(s)", len(pending))

	for _, p := range pending {
		ps.dispatchPayout(ctx, p)
	}
}

// dispatchPayout tries to send a single payout through the processor recorded
// on the row (or any available processor if none is specified).
func (ps *PayoutSettler) dispatchPayout(ctx context.Context, p store.PayoutRow) {
	processors := ps.ledger.processors
	if len(processors) == 0 {
		return
	}

	for _, proc := range processors {
		// If the payout has a preferred processor, skip others.
		if p.Processor != "" && proc.Name() != p.Processor {
			continue
		}
		if !proc.IsOnline(ctx) {
			continue
		}

		result, err := proc.CreateCharge(ctx, ChargeRequest{
			UserDID:      p.ProviderDID,
			ProviderDID:  p.ProviderDID,
			Amount:       p.AmountSats,
			Currency:     "sats",
			ResourceType: "payout",
			Metadata:     map[string]string{"payout_id": p.PayoutID},
		})
		if err != nil {
			log.Printf("[payout_settler] dispatch payout %s via %s: %v", p.PayoutID, proc.Name(), err)
			continue
		}

		extID := ""
		if result != nil {
			extID = result.ChargeID
		}
		if updateErr := ps.store.UpdatePayoutStatus(ctx, p.PayoutID, "processing", extID, ""); updateErr != nil {
			log.Printf("[payout_settler] update payout %s status: %v", p.PayoutID, updateErr)
		} else {
			log.Printf("[payout_settler] payout %s dispatched via %s (extID=%s)", p.PayoutID, proc.Name(), extID)
		}
		return
	}
	// All processors failed or offline — leave in "pending" for next tick.
}

// checkAutoPayoutThreshold inspects each provider's unsettled balance and
// automatically triggers a payout request if it exceeds AutoPayoutThresholdSats.
// It queries the central revenue table for pending sats and compares against
// already-pending/processing payouts to avoid double-requesting.
func (ps *PayoutSettler) checkAutoPayoutThreshold(ctx context.Context) {
	pendingRevenue, err := ps.store.GetPendingPayout(ctx)
	if err != nil {
		log.Printf("[payout_settler] auto-payout threshold check: %v", err)
		return
	}
	if pendingRevenue < ps.AutoPayoutThresholdSats {
		return
	}

	// Check if there's already an in-flight payout to avoid double-dispatch.
	inFlight, err := ps.store.GetPayoutsByStatus(ctx, "pending", 1)
	if err == nil && len(inFlight) > 0 {
		return // already queued
	}
	processing, err := ps.store.GetPayoutsByStatus(ctx, "processing", 1)
	if err == nil && len(processing) > 0 {
		return // already dispatched
	}

	log.Printf("[payout_settler] auto-payout triggered: pending balance %d sats ≥ threshold %d sats",
		pendingRevenue, ps.AutoPayoutThresholdSats)

	_, reqErr := ps.ledger.RequestPayout(ctx, PayoutRequest{
		ProviderDID: ps.providerDID,
		AmountSats:  pendingRevenue,
		Processor:   ps.AutoPayoutProcessor,
	})
	if reqErr != nil {
		log.Printf("[payout_settler] auto-payout request failed: %v", reqErr)
	}
}
