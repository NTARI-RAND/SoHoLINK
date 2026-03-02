// Package payment — metering service.
//
// UsageMeter watches active workload placements and emits billing charges at
// regular intervals. It ties the orchestration scheduler to the payment ledger:
//
//	FedScheduler (placements) → UsageMeter (per-hour) → Ledger → Stripe / Lightning
//
// The meter bills in arrears: at the end of each billing interval it calculates
// how many milliseconds each placement ran and charges accordingly.
package payment

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// ActivePlacement is the minimal set of data the meter needs from the
// orchestration layer. The orchestration package fills this from WorkloadState.
type ActivePlacement struct {
	PlacementID  string
	WorkloadID   string
	OwnerDID     string    // user being billed
	ProviderDID  string    // node earning the revenue
	CPUCores     float64
	MemoryMB     int64
	DiskGB       int64
	PricePerHour int64     // total price in cents per hour for this placement
	StartedAt    time.Time
}

// PlacementSource is implemented by the orchestration scheduler.
// The meter calls this every billing interval to get the current placement list.
type PlacementSource interface {
	ActivePlacements() []ActivePlacement
}

// MeterConfig controls billing interval and idempotency.
type MeterConfig struct {
	// BillingInterval is how often the meter runs (e.g. 1 hour, 15 minutes for testing).
	BillingInterval time.Duration
	// MinBillableSeconds is the smallest billable unit (default: 60s = 1 minute).
	MinBillableSeconds int64
}

// DefaultMeterConfig returns production defaults.
func DefaultMeterConfig() MeterConfig {
	return MeterConfig{
		BillingInterval:    time.Hour,
		MinBillableSeconds: 60,
	}
}

// UsageMeter periodically charges tenants for active workload placements.
type UsageMeter struct {
	source PlacementSource
	ledger *Ledger
	cfg    MeterConfig

	mu      sync.Mutex
	billed  map[string]billedEntry // key: placementID
}

type billedEntry struct {
	lastBilledAt time.Time
	totalBilled  int64 // cumulative cents billed for this placement
}

// NewUsageMeter creates a new metering service.
func NewUsageMeter(source PlacementSource, ledger *Ledger, cfg MeterConfig) *UsageMeter {
	return &UsageMeter{
		source: source,
		ledger: ledger,
		cfg:    cfg,
		billed: make(map[string]billedEntry),
	}
}

// Run starts the metering loop. It blocks until ctx is cancelled.
// Call in a goroutine: go meter.Run(ctx).
func (m *UsageMeter) Run(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.BillingInterval)
	defer ticker.Stop()

	log.Printf("[meter] started — billing interval %s", m.cfg.BillingInterval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[meter] stopping")
			return
		case t := <-ticker.C:
			m.bill(ctx, t)
		}
	}
}

// bill iterates active placements and charges each one for the elapsed time
// since it was last billed.
func (m *UsageMeter) bill(ctx context.Context, now time.Time) {
	placements := m.source.ActivePlacements()
	if len(placements) == 0 {
		return
	}
	log.Printf("[meter] billing tick: %d active placements", len(placements))

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range placements {
		entry, seen := m.billed[p.PlacementID]
		if !seen {
			// First time we see this placement: start billing from StartedAt.
			entry = billedEntry{lastBilledAt: p.StartedAt}
		}

		elapsed := now.Sub(entry.lastBilledAt)
		if elapsed < 0 {
			// Clock skew: StartedAt is in the future relative to the billing clock.
			// Skip this cycle and log a warning so operators can investigate.
			log.Printf("[meter] warning: placement %s has future reference time (skew %s); skipping billing cycle",
				p.PlacementID, (-elapsed).Round(time.Second))
			continue
		}
		elapsedSec := int64(elapsed.Seconds())

		if elapsedSec < m.cfg.MinBillableSeconds {
			continue // not enough time accumulated
		}

		// Pro-rate: cents = pricePerHour * elapsedSeconds / 3600
		amount := p.PricePerHour * elapsedSec / 3600
		if amount <= 0 {
			continue
		}

		req := ChargeRequest{
			Amount:         amount,
			Currency:       "USD",
			UserDID:        p.OwnerDID,
			ProviderDID:    p.ProviderDID,
			ResourceType:   "compute",
			UsageRecordID:  p.PlacementID,
			IdempotencyKey: fmt.Sprintf("meter-%s-%d", p.PlacementID, now.Unix()),
			Metadata: map[string]string{
				"workload_id":   p.WorkloadID,
				"placement_id":  p.PlacementID,
				"elapsed_sec":   fmt.Sprintf("%d", elapsedSec),
				"cpu_cores":     fmt.Sprintf("%.2f", p.CPUCores),
				"memory_mb":     fmt.Sprintf("%d", p.MemoryMB),
				"disk_gb":       fmt.Sprintf("%d", p.DiskGB),
			},
		}

		_, err := m.ledger.ChargeForUsage(ctx, req)
		if err != nil {
			log.Printf("[meter] charge failed for placement %s (user %s): %v", p.PlacementID, p.OwnerDID, err)
			// Queue for offline settlement — don't update lastBilledAt so we retry.
			continue
		}

		log.Printf("[meter] billed %d cents for placement %s (user %s, %ds)", amount, p.PlacementID, p.OwnerDID, elapsedSec)

		entry.lastBilledAt = now
		entry.totalBilled += amount
		m.billed[p.PlacementID] = entry
	}

	// Prune entries for placements no longer active.
	m.pruneInactive(placements)
}

// pruneInactive removes billed entries for placements that are no longer in
// the active set.
func (m *UsageMeter) pruneInactive(active []ActivePlacement) {
	activeSet := make(map[string]bool, len(active))
	for _, p := range active {
		activeSet[p.PlacementID] = true
	}
	for id := range m.billed {
		if !activeSet[id] {
			delete(m.billed, id)
		}
	}
}

// TotalBilled returns how much has been billed for a placement so far.
func (m *UsageMeter) TotalBilled(placementID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.billed[placementID].totalBilled
}
