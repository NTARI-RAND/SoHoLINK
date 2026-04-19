package scheduler

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

// CandidateScore pairs a node with its computed placement score.
type CandidateScore struct {
	Node  orchestrator.NodeEntry
	Score float64
}

// classScore returns an ordinal score for a node's certified class.
// Class A nodes are the most reliable (SOHO servers, ≥95% uptime).
// Class D nodes are storage-only appliances.
func classScore(class string) float64 {
	switch class {
	case "A":
		return 4.0
	case "B":
		return 3.0
	case "C":
		return 2.0
	case "D":
		return 1.0
	default:
		return 0.0
	}
}

// freshnessScore returns a 0–1 score based on heartbeat recency.
// A node that heartbeated just now scores 1.0; a node 30 minutes stale
// (near the eviction threshold) scores 0.0. Linear decay between.
func freshnessScore(lastHeartbeat time.Time) float64 {
	const staleCutoff = 30 * time.Minute
	age := time.Since(lastHeartbeat)
	if age <= 0 {
		return 1.0
	}
	score := 1.0 - age.Seconds()/staleCutoff.Seconds()
	return math.Max(0.0, score)
}

// Schedule scores and ranks candidates, returning the top N nodes for the
// given SLA tier. Returns an error if fewer candidates are available than
// the tier requires.
//
// Scoring formula: classScore + freshnessScore + capacityScore
//
//   - classScore:     node class ordinal (A=4, B=3, C=2, D=1) — platform reliability cert
//   - freshnessScore: heartbeat recency, linear decay 1.0→0.0 over 30 minutes
//   - capacityScore:  CPU cores normalized 0–1 against the candidate pool — breaks ties
//
// Extension points: when a Reputation Engine is available, add a reputation
// component here. When Marketplace Engine pricing is live, replace or weight
// the capacityScore with a price-efficiency term.
func Schedule(candidates []orchestrator.NodeEntry, tier orchestrator.SLATier) ([]orchestrator.NodeEntry, error) {
	if len(candidates) < int(tier) {
		return nil, fmt.Errorf("schedule: tier requires %d node(s), only %d available", int(tier), len(candidates))
	}

	// Find max CPU in pool for capacity normalization.
	maxCPU := 1
	for _, node := range candidates {
		if node.HardwareProfile.CPUCores > maxCPU {
			maxCPU = node.HardwareProfile.CPUCores
		}
	}

	scored := make([]CandidateScore, len(candidates))
	for i, node := range candidates {
		capacityScore := float64(node.HardwareProfile.CPUCores) / float64(maxCPU)
		score := classScore(node.NodeClass) +
			freshnessScore(node.LastHeartbeat) +
			capacityScore
		scored[i] = CandidateScore{Node: node, Score: score}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	result := make([]orchestrator.NodeEntry, int(tier))
	for i := range result {
		result[i] = scored[i].Node
	}
	return result, nil
}
