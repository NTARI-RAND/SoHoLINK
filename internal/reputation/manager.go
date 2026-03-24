// Package reputation — reputation manager integrates payment telemetry into the reputation ledger.
package reputation

import (
	"log"
	"sync"
	"time"
)

// PaymentMetrics are the telemetry signals that drive reputation scoring.
// These come from the payment settlement layer.
type PaymentMetrics struct {
	NodeDID           string
	PeriodEnd         time.Time
	JobsCompleted     int
	JobsAttempted     int
	SettlementCount   int // Number of settlements (HTLC settle)
	CancelCount       int // Number of cancellations (HTLC cancel)
	AccurateCount     int // Jobs with accurate duration estimates
	TotalExecutionSec float64
}

// ReputationManager periodically snapshots payment metrics and creates reputation entries.
type ReputationManager struct {
	ledger *ReputationLedger

	// Metrics accumulator (per node, per period)
	metrics map[string]*PaymentMetrics
	mu      sync.RWMutex

	// Pricing integration
	basePricePerCPUHour int64 // Base price in cents (e.g., 100)
	maxPricingBonus     float64 // Max bonus percentage (e.g., 0.50 = 50%)

	nowFunc func() time.Time
}

// NewReputationManager creates a manager backed by a reputation ledger.
func NewReputationManager(ledger *ReputationLedger, basePricePerCPUHour int64) *ReputationManager {
	return &ReputationManager{
		ledger:              ledger,
		metrics:             make(map[string]*PaymentMetrics),
		basePricePerCPUHour: basePricePerCPUHour,
		maxPricingBonus:     0.50, // Providers with score 100 get 50% bonus
		nowFunc:             time.Now,
	}
}

// RecordJobOutcome updates metrics for a completed job.
// Called by payment settlement to feed telemetry into reputation.
func (rm *ReputationManager) RecordJobOutcome(nodeDID string, settled bool, accurateEstimate bool, executionSec float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	metrics, ok := rm.metrics[nodeDID]
	if !ok {
		metrics = &PaymentMetrics{
			NodeDID:   nodeDID,
			PeriodEnd: rm.nowFunc().AddDate(0, 0, 7), // Weekly periods
		}
		rm.metrics[nodeDID] = metrics
	}

	metrics.JobsAttempted++
	metrics.JobsCompleted++
	metrics.TotalExecutionSec += executionSec

	if settled {
		metrics.SettlementCount++
	} else {
		metrics.CancelCount++
	}

	if accurateEstimate {
		metrics.AccurateCount++
	}

	log.Printf("[reputation] recorded outcome for %s: settled=%v, accurate=%v",
		nodeDID, settled, accurateEstimate)
}

// FinalizeWeek completes the current period and creates reputation entries.
// Called weekly (or on-demand for testing).
func (rm *ReputationManager) FinalizeWeek() error {
	rm.mu.Lock()
	metricsSnapshot := make(map[string]*PaymentMetrics)
	for k, v := range rm.metrics {
		// Make a copy
		copy := *v
		metricsSnapshot[k] = &copy
	}
	// Reset for next week
	rm.metrics = make(map[string]*PaymentMetrics)
	rm.mu.Unlock()

	// Create reputation entries from finalized metrics
	now := rm.nowFunc()
	for nodeDID, metrics := range metricsSnapshot {
		if metrics.JobsAttempted == 0 {
			continue // Skip nodes with no activity
		}

		entry := &ReputationEntry{
			NodeDID:    nodeDID,
			Period:     metrics.PeriodEnd,
			JobsCompleted: metrics.JobsCompleted,
			JobsAttempted: metrics.JobsAttempted,
			FailureRate: float64(metrics.CancelCount) / float64(metrics.JobsAttempted),
			SettlementRate: float64(metrics.SettlementCount) / float64(metrics.JobsCompleted),
			AccuracyRate: float64(metrics.AccurateCount) / float64(metrics.JobsCompleted),
			AvgExecutionTime: metrics.TotalExecutionSec / float64(metrics.JobsCompleted),
			CreatedAt: now,
			Source: "payment_telemetry",
		}

		// Link to previous entry
		history := rm.ledger.GetHistory(nodeDID)
		if len(history) > 0 {
			entry.PreviousHash = history[len(history)-1].EntryHash
		} else {
			entry.PreviousHash = "genesis"
		}

		// Compute hash and score
		entry.EntryHash = entry.ComputeHash()
		entry.ReputationScore = entry.ComputeScore()

		if err := rm.ledger.AddEntry(entry); err != nil {
			log.Printf("[reputation] failed to add entry for %s: %v", nodeDID, err)
			continue
		}

		log.Printf("[reputation] finalized week for %s: score=%d, settlement_rate=%.2f",
			nodeDID, entry.ReputationScore, entry.SettlementRate)
	}

	return nil
}

// ComputeDynamicPrice calculates a provider's pricing based on reputation.
// Formula: base_price * (1 + bonus_pct)
// Bonus is derived from reputation score: score_100 = 50% bonus, score_50 = 0% bonus, score_0 = -50% discount.
func (rm *ReputationManager) ComputeDynamicPrice(nodeDID string, basePrice int64) int64 {
	score := rm.ledger.GetLatestScore(nodeDID)

	// Normalize score to [-1, 1] range, then apply to bonus
	// score 50 (neutral) = 0 bonus
	// score 100 (excellent) = +maxBonus
	// score 0 (terrible) = -maxBonus
	normalized := (float64(score) - 50.0) / 50.0 // [-1, 1]
	bonus := normalized * rm.maxPricingBonus

	adjustedPrice := float64(basePrice) * (1.0 + bonus)
	if adjustedPrice < 0 {
		adjustedPrice = 0
	}

	return int64(adjustedPrice)
}

// GetPricingMultiplier returns the multiplier (e.g., 1.25 for 25% markup) based on reputation.
func (rm *ReputationManager) GetPricingMultiplier(nodeDID string) float64 {
	score := rm.ledger.GetLatestScore(nodeDID)
	normalized := (float64(score) - 50.0) / 50.0
	return 1.0 + (normalized * rm.maxPricingBonus)
}

// SetMaxPricingBonus adjusts the maximum pricing bonus (e.g., 0.50 for 50% max markup).
func (rm *ReputationManager) SetMaxPricingBonus(maxBonus float64) {
	if maxBonus < 0 || maxBonus > 1.0 {
		log.Printf("[reputation] invalid max bonus %.2f (must be 0-1), ignoring", maxBonus)
		return
	}
	rm.maxPricingBonus = maxBonus
}

// GetReputationMultiplier returns a scheduling priority multiplier based on reputation.
// Higher reputation = higher priority (closer to 2.0 for top-tier providers).
// Formula: 1.0 + (score - 50) / 100
//   score 100 = 1.5x priority
//   score 50 = 1.0x priority (neutral)
//   score 0 = 0.5x priority
func GetReputationMultiplier(score int) float64 {
	return 1.0 + float64(score-50)/100.0
}

// FilterByReputationThreshold returns a subset of nodes that meet a minimum reputation score.
// Used during scheduling to exclude unreliable providers.
func FilterByReputationThreshold(ledger *ReputationLedger, nodes map[string]interface{}, minScore int) map[string]interface{} {
	filtered := make(map[string]interface{})
	for nodeDID, nodeData := range nodes {
		score := ledger.GetLatestScore(nodeDID)
		if score >= minScore {
			filtered[nodeDID] = nodeData
		}
	}
	return filtered
}

// API helper methods for HTTP handlers

// GetLatestScore returns the current reputation score for a node.
func (rm *ReputationManager) GetLatestScore(nodeDID string) int {
	return rm.ledger.GetLatestScore(nodeDID)
}

// GetNodeHistory returns the full reputation history for a node.
func (rm *ReputationManager) GetNodeHistory(nodeDID string) []*ReputationEntry {
	return rm.ledger.GetHistory(nodeDID)
}

// GetNodeStats returns aggregate statistics for a node.
func (rm *ReputationManager) GetNodeStats(nodeDID string) *NodeStats {
	return rm.ledger.GetStats(nodeDID)
}

// GetAllProviders returns the latest scores for all providers.
func (rm *ReputationManager) GetAllProviders() map[string]int {
	return rm.ledger.GetAllProviders()
}

// VerifyChain validates the Merkle chain for a node.
func (rm *ReputationManager) VerifyChain(nodeDID string) error {
	return rm.ledger.VerifyChain(nodeDID)
}
