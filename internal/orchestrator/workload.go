package orchestrator

import (
	"fmt"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

// marketplaceToAgent maps every MarketplaceWorkloadType to the agent's
// WorkloadType enum. The mapping is a var (not const) so tests can
// temporarily remove entries to exercise MustValidateWorkloadMapping.
var marketplaceToAgent = map[types.MarketplaceWorkloadType]agent.WorkloadType{
	types.MarketplaceAppHosting:    agent.WorkloadCompute,
	types.MarketplaceBatchCompute:  agent.WorkloadCompute,
	types.MarketplaceAIInference:   agent.WorkloadCompute,
	types.MarketplaceObjectStorage: agent.WorkloadStorage,
	types.MarketplaceCDNEdge:       agent.WorkloadCompute,
}

// MarketplaceToAgent translates a MarketplaceWorkloadType to the agent
// WorkloadType used by OptOutStore.IsResourceEnabled and AllowlistEntry.Type.
// Returns an error if the marketplace value has no mapping — which should
// never happen in production after MustValidateWorkloadMapping has run.
func MarketplaceToAgent(w types.MarketplaceWorkloadType) (agent.WorkloadType, error) {
	a, ok := marketplaceToAgent[w]
	if !ok {
		return "", fmt.Errorf("no agent mapping for marketplace workload type %q", w)
	}
	return a, nil
}

// MustValidateWorkloadMapping panics if any value returned by
// AllMarketplaceWorkloadTypes() lacks an entry in marketplaceToAgent.
// Call this from an init() or a one-time startup check to catch
// incomplete mappings at process start rather than at job dispatch time.
func MustValidateWorkloadMapping() {
	for _, w := range types.AllMarketplaceWorkloadTypes() {
		if _, ok := marketplaceToAgent[w]; !ok {
			panic(fmt.Sprintf("workload mapping incomplete: no agent type for %q", w))
		}
	}
}
