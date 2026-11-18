package orchestrator

import (
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

// Named stand-in (CONFORMANCE.md): an operator-registered node is never
// matched for a job until slice 2 settles how such jobs are submitted and
// paid. A participant's node with the same capacity still matches.
func TestNodeRegistry_FindMatch_SkipsOperatorNodes(t *testing.T) {
	r := NewNodeRegistry()
	opNode := newOnlineNode("node-operator", "US", 8, 16384, 100, false)
	opNode.ParticipantID = ""
	opNode.OperatorID = "op-cloudy"
	r.Register(opNode)

	req := MatchRequest{WorkloadType: types.MarketplaceBatchCompute, CPUCores: 2, RAMMB: 4096}
	if got, err := r.FindMatch(req); err == nil {
		t.Fatalf("operator node matched: %+v", got)
	}

	// The job preflight uses the same matcher: the node is neither counted
	// eligible nor reported as a blocker.
	if ex := r.ExplainMatch(req); ex.EligibleNodes != 0 || len(ex.Blockers) != 0 {
		t.Fatalf("preflight counted the operator node: %+v", ex)
	}

	r.Register(newOnlineNode("node-person", "US", 8, 16384, 100, false))
	got, err := r.FindMatch(req)
	if err != nil {
		t.Fatalf("participant node did not match: %v", err)
	}
	for _, n := range got {
		if n.NodeID == "node-operator" {
			t.Fatal("operator node returned among candidates")
		}
	}
}
