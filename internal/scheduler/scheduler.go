package scheduler

import (
	"fmt"
	"sort"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

// CandidateScore pairs a node with its computed placement score.
type CandidateScore struct {
	Node  orchestrator.NodeEntry
	Score float64
}

// Schedule scores and ranks candidates, returning the top N nodes for the
// given SLA tier. Returns an error if fewer candidates are available than
// the tier requires.
//
// Scoring formula: reputationScore + (1.0 / priceWeight)
// TODO(Phase 2): replace reputationScore with a live score from the Reputation
// Engine and priceWeight with the node's listed price from the Marketplace Engine.
func Schedule(candidates []orchestrator.NodeEntry, tier orchestrator.SLATier) ([]orchestrator.NodeEntry, error) {
	if len(candidates) < int(tier) {
		return nil, fmt.Errorf("schedule: tier requires %d node(s), only %d available", int(tier), len(candidates))
	}

	scored := make([]CandidateScore, len(candidates))
	for i, node := range candidates {
		reputationScore := 1.0 // TODO(Phase 2): fetch from Reputation Engine
		priceWeight := 1.0     // TODO(Phase 2): fetch from Marketplace Engine listing
		scored[i] = CandidateScore{
			Node:  node,
			Score: reputationScore + (1.0 / priceWeight),
		}
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
