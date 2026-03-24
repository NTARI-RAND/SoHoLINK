// Package reputation implements a network-wide reputation ledger for SoHoLINK providers.
//
// Reputation scores are derived from payment settlement telemetry and job execution metrics.
// Each reputation entry is Merkle-chained to create an immutable, auditable history.
// Reputation scores influence scheduling priority and dynamic pricing.
package reputation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// ReputationEntry represents a single reputation record for a provider (node).
// Entries are Merkle-chained: each entry includes the hash of the previous entry.
type ReputationEntry struct {
	// Identity
	NodeDID string    `json:"node_did"`
	Period  time.Time `json:"period"` // Period end time (e.g., end of week)

	// Metrics (from payment settlement telemetry)
	JobsCompleted    int     `json:"jobs_completed"`
	JobsAttempted    int     `json:"jobs_attempted"`
	FailureRate      float64 `json:"failure_rate"`       // [0, 1]: failed / attempted
	SettlementRate   float64 `json:"settlement_rate"`    // [0, 1]: settled / completed
	AccuracyRate     float64 `json:"accuracy_rate"`      // [0, 1]: accurate estimates / completed
	AvgExecutionTime float64 `json:"avg_execution_time"` // seconds

	// Score
	ReputationScore int `json:"reputation_score"` // [0, 100]

	// Merkle chain
	PreviousHash string `json:"previous_hash"` // SHA256 hash of previous entry (or "genesis")
	EntryHash    string `json:"entry_hash"`    // SHA256 hash of this entry
	Signature    string `json:"signature"`     // Ed25519 signature (optional, for audits)

	// Metadata
	CreatedAt time.Time `json:"created_at"`
	Source    string    `json:"source"` // "payment_telemetry", "manual_audit", etc.
}

// ComputeHash computes the SHA256 hash of this entry's essential fields.
// Used to build the Merkle chain.
func (re *ReputationEntry) ComputeHash() string {
	// Hash the tuple: (NodeDID, Period, JobsCompleted, FailureRate, SettlementRate, PreviousHash)
	hashData := struct {
		NodeDID       string
		Period        string
		JobsCompleted int
		FailureRate   float64
		SettlementRate float64
		PreviousHash  string
	}{
		NodeDID:        re.NodeDID,
		Period:         re.Period.Format(time.RFC3339),
		JobsCompleted:  re.JobsCompleted,
		FailureRate:    re.FailureRate,
		SettlementRate: re.SettlementRate,
		PreviousHash:   re.PreviousHash,
	}

	data, _ := json.Marshal(hashData)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ComputeScore derives the reputation score from metrics.
// Score: [0, 100] based on weighted metrics.
//
// Weights (simplified):
//   - Completion (30%): (1 - FailureRate)
//   - Settlement (40%): SettlementRate
//   - Accuracy (20%): AccuracyRate
//   - History bonus (10%): age-based decay (newer entries slightly lower)
func (re *ReputationEntry) ComputeScore() int {
	completion := (1.0 - re.FailureRate) * 30.0
	settlement := re.SettlementRate * 40.0
	accuracy := re.AccuracyRate * 20.0
	historyBonus := 10.0 // Assume fresh entry gets full bonus

	total := completion + settlement + accuracy + historyBonus
	if total < 0 {
		return 0
	}
	if total > 100 {
		return 100
	}
	return int(total)
}

// ReputationLedger maintains the immutable Merkle-chained history of provider reputation.
type ReputationLedger struct {
	chain map[string][]*ReputationEntry // key: node DID, value: chronological entries
	mu    sync.RWMutex
	nowFunc func() time.Time // Testable time source
}

// NewReputationLedger creates an empty reputation ledger.
func NewReputationLedger() *ReputationLedger {
	return &ReputationLedger{
		chain:   make(map[string][]*ReputationEntry),
		nowFunc: time.Now,
	}
}

// AddEntry appends a new reputation entry to a node's chain.
// Validates Merkle chain integrity before adding.
// Returns error if the entry doesn't properly extend the chain.
func (rl *ReputationLedger) AddEntry(entry *ReputationEntry) error {
	if entry.NodeDID == "" {
		return fmt.Errorf("entry must have node_did")
	}

	// Set creation timestamp if not already set
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = rl.nowFunc()
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	chain, ok := rl.chain[entry.NodeDID]

	// Validate chain link
	if ok && len(chain) > 0 {
		lastEntry := chain[len(chain)-1]
		if entry.PreviousHash != lastEntry.EntryHash {
			return fmt.Errorf("entry does not link to previous entry (expected %s, got %s)",
				lastEntry.EntryHash, entry.PreviousHash)
		}
	} else {
		// First entry in chain must link to "genesis"
		if entry.PreviousHash != "genesis" {
			return fmt.Errorf("first entry must have previous_hash='genesis'")
		}
	}

	// Compute and validate the entry hash
	computedHash := entry.ComputeHash()
	if entry.EntryHash == "" {
		entry.EntryHash = computedHash
	} else if entry.EntryHash != computedHash {
		return fmt.Errorf("entry hash mismatch (expected %s, got %s)", computedHash, entry.EntryHash)
	}

	// Compute score if not set
	if entry.ReputationScore == 0 {
		entry.ReputationScore = entry.ComputeScore()
	}

	// Append to chain
	rl.chain[entry.NodeDID] = append(chain, entry)

	log.Printf("[reputation] added entry for node %s: score=%d, settlement_rate=%.2f",
		entry.NodeDID, entry.ReputationScore, entry.SettlementRate)

	return nil
}

// GetLatestScore returns the most recent reputation score for a node.
// Returns 50 (neutral) if no history exists.
func (rl *ReputationLedger) GetLatestScore(nodeDID string) int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	chain, ok := rl.chain[nodeDID]
	if !ok || len(chain) == 0 {
		return 50 // Neutral score for new providers
	}

	return chain[len(chain)-1].ReputationScore
}

// GetHistory returns the full Merkle-chained history for a node.
func (rl *ReputationLedger) GetHistory(nodeDID string) []*ReputationEntry {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	chain, ok := rl.chain[nodeDID]
	if !ok {
		return nil
	}

	// Return a copy
	result := make([]*ReputationEntry, len(chain))
	copy(result, chain)
	return result
}

// VerifyChain validates the Merkle chain integrity for a node.
// Returns error if any link is broken or hash is invalid.
func (rl *ReputationLedger) VerifyChain(nodeDID string) error {
	rl.mu.RLock()
	chain, ok := rl.chain[nodeDID]
	rl.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no chain for node %s", nodeDID)
	}

	for i, entry := range chain {
		// Verify entry's own hash
		computedHash := entry.ComputeHash()
		if entry.EntryHash != computedHash {
			return fmt.Errorf("entry %d hash mismatch: expected %s, got %s",
				i, computedHash, entry.EntryHash)
		}

		// Verify link to previous entry
		if i == 0 {
			if entry.PreviousHash != "genesis" {
				return fmt.Errorf("entry 0 must link to 'genesis', got %s", entry.PreviousHash)
			}
		} else {
			if entry.PreviousHash != chain[i-1].EntryHash {
				return fmt.Errorf("entry %d does not link to entry %d", i, i-1)
			}
		}
	}

	log.Printf("[reputation] verified chain for node %s (%d entries)", nodeDID, len(chain))
	return nil
}

// GetAllProviders returns the latest scores for all known providers.
func (rl *ReputationLedger) GetAllProviders() map[string]int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	result := make(map[string]int)
	for nodeDID, chain := range rl.chain {
		if len(chain) > 0 {
			result[nodeDID] = chain[len(chain)-1].ReputationScore
		}
	}
	return result
}

// GetNodeStats returns aggregate statistics for a node across its entire history.
type NodeStats struct {
	NodeDID              string
	TotalJobs            int
	TotalAttempts        int
	AverageFailureRate   float64
	AverageSettlementRate float64
	AverageAccuracyRate  float64
	CurrentScore         int
	EntryCount           int
}

// GetStats computes aggregate statistics for a node.
func (rl *ReputationLedger) GetStats(nodeDID string) *NodeStats {
	rl.mu.RLock()
	chain, ok := rl.chain[nodeDID]
	rl.mu.RUnlock()

	if !ok || len(chain) == 0 {
		return &NodeStats{
			NodeDID:     nodeDID,
			CurrentScore: 50, // neutral
		}
	}

	stats := &NodeStats{
		NodeDID:    nodeDID,
		EntryCount: len(chain),
	}

	// Aggregate metrics across all entries
	sumFailure := 0.0
	sumSettlement := 0.0
	sumAccuracy := 0.0

	for _, entry := range chain {
		stats.TotalJobs += entry.JobsCompleted
		stats.TotalAttempts += entry.JobsAttempted
		sumFailure += entry.FailureRate
		sumSettlement += entry.SettlementRate
		sumAccuracy += entry.AccuracyRate
	}

	count := float64(len(chain))
	stats.AverageFailureRate = sumFailure / count
	stats.AverageSettlementRate = sumSettlement / count
	stats.AverageAccuracyRate = sumAccuracy / count
	stats.CurrentScore = chain[len(chain)-1].ReputationScore

	return stats
}
