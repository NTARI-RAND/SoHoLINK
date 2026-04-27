package types

import "fmt"

// MarketplaceWorkloadType is the canonical workload identifier used by the
// portal, orchestrator, and database. Values match the PostgreSQL
// workload_type enum defined in migration 001.
type MarketplaceWorkloadType string

const (
	MarketplaceAppHosting    MarketplaceWorkloadType = "app_hosting"
	MarketplaceBatchCompute  MarketplaceWorkloadType = "batch_compute"
	MarketplaceAIInference   MarketplaceWorkloadType = "ai_inference"
	MarketplaceObjectStorage MarketplaceWorkloadType = "object_storage"
	MarketplaceCDNEdge       MarketplaceWorkloadType = "cdn_edge"
)

// AllMarketplaceWorkloadTypes returns every defined MarketplaceWorkloadType.
// Used by MustValidateWorkloadMapping and test helpers.
func AllMarketplaceWorkloadTypes() []MarketplaceWorkloadType {
	return []MarketplaceWorkloadType{
		MarketplaceAppHosting,
		MarketplaceBatchCompute,
		MarketplaceAIInference,
		MarketplaceObjectStorage,
		MarketplaceCDNEdge,
	}
}

// IsValid reports whether w is a known marketplace workload type.
func (w MarketplaceWorkloadType) IsValid() bool {
	for _, v := range AllMarketplaceWorkloadTypes() {
		if w == v {
			return true
		}
	}
	return false
}

// ParseMarketplaceWorkloadType parses s into a MarketplaceWorkloadType.
// Returns an error wrapping the unknown value if s is not recognised.
func ParseMarketplaceWorkloadType(s string) (MarketplaceWorkloadType, error) {
	w := MarketplaceWorkloadType(s)
	if !w.IsValid() {
		return "", fmt.Errorf("unknown marketplace workload type: %q", s)
	}
	return w, nil
}
