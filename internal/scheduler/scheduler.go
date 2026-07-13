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

// Soft-placement weights and constants (B3). All tunable; documented here at
// the definition site per convention.
const (
	// wLocality weights the locality tier. 10.0 is deliberately dominant:
	// max locality contribution (10.0 × 0.6 = 6.0) exceeds the legacy
	// class+freshness+capacity maximum of 6.0, so a same-region node beats
	// any out-of-region node of any class — locality-first placement.
	wLocality = 10.0

	// wIdle weights the idle score. 2.0 is deliberately BELOW the legacy
	// class+freshness+capacity max of 6.0: idle state is self-reported and
	// spoofable, so it may tip ties but never overturn the certified terms.
	wIdle = 2.0

	// perInFlightPenalty is subtracted once per in-flight placement on the
	// node — an advisory load-spreading nudge.
	perInFlightPenalty = 0.5

	// loadSampleTTL bounds how long a heartbeat load sample counts as fresh:
	// 3× the 60s heartbeat interval. Older (or absent) samples score 0.0.
	loadSampleTTL = 3 * 60 * time.Second
)

// localityScore returns the soft locality tier of a node relative to the
// requester: 0.6 same region, 0.3 same country, 0 otherwise. Empty values on
// either side never match — an unknown requester location contributes 0
// everywhere rather than penalizing anyone. This is a SOFT preference; the
// hard residency filter remains MatchRequest.CountryConstraint in FindMatch.
func localityScore(node orchestrator.NodeEntry, pctx orchestrator.PlacementContext) float64 {
	switch {
	case node.Region != "" && node.Region == pctx.RequesterRegion:
		return 0.6
	case pctx.RequesterCountry != "" && node.CountryCode == pctx.RequesterCountry:
		return 0.3
	default:
		return 0.0
	}
}

// idleScore returns 0–1 from the node's self-reported load sample:
//
//   - OwnerActive → 0.0 (the member is using their machine; leave it alone)
//   - fresh sample (non-zero LoadSampledAt younger than loadSampleTTL)
//     → 1.0 − clamp(CPUUtilPct/100, 0, 1)  (codebase CPU% is 0–100)
//   - absent or stale sample → 0.0
//
// The stale/absent case scoring 0.0 (never a neutral 0.5) is the load-bearing
// invariant: a node that stops reporting must never outrank an honest node
// reporting 100% CPU via this term.
func idleScore(node orchestrator.NodeEntry) float64 {
	if node.OwnerActive {
		return 0.0
	}
	if node.LoadSampledAt.IsZero() || time.Since(node.LoadSampledAt) > loadSampleTTL {
		return 0.0
	}
	util := node.CPUUtilPct / 100.0
	if util < 0 {
		util = 0
	}
	if util > 1 {
		util = 1
	}
	return 1.0 - util
}

// Schedule scores and ranks candidates, returning the top N nodes for the
// given SLA tier. Returns an error if fewer candidates are available than
// the tier requires.
//
// Scoring formula: classScore + freshnessScore + capacityScore
// + wLocality×localityScore + wIdle×idleScore − perInFlightPenalty×InFlight
//
//   - classScore:     node class ordinal (A=4, B=3, C=2, D=1) — platform reliability cert
//   - freshnessScore: heartbeat recency, linear decay 1.0→0.0 over 30 minutes
//   - capacityScore:  CPU cores normalized 0–1 against the candidate pool — breaks ties
//   - localityScore:  soft tiers — same region 0.6, same country 0.3, else 0
//   - idleScore:      self-reported idleness 0–1; absent/stale sample scores 0
//   - InFlight:       advisory count of current placements on the node
//
// Ties are NOT broken deterministically by NodeID: Go's random map iteration
// in FindMatch stays the load-spreading mechanism among equals.
//
// Extension points: when a Reputation Engine is available, add a reputation
// component here. When Marketplace Engine pricing is live, replace or weight
// the capacityScore with a price-efficiency term.
func Schedule(candidates []orchestrator.NodeEntry, tier orchestrator.SLATier, pctx orchestrator.PlacementContext) ([]orchestrator.NodeEntry, error) {
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
			capacityScore +
			wLocality*localityScore(node, pctx) +
			wIdle*idleScore(node) -
			perInFlightPenalty*float64(node.InFlight)
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
