package orchestrator

import (
	"strings"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

func TestPlacementBlockerCatalogComplete(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		workload   types.MarketplaceWorkloadType
		change     func(*NodeEntry)
	}{
		{"offline", "offline", types.MarketplaceBatchCompute, func(n *NodeEntry) { n.Status = "offline" }},
		{"country", "country", types.MarketplaceBatchCompute, func(n *NodeEntry) { n.CountryCode = "GB" }},
		{"gpu", "gpu", types.MarketplaceBatchCompute, func(n *NodeEntry) { n.HardwareProfile.GPUPresent = false }},
		{"cpu", "cpu", types.MarketplaceBatchCompute, func(n *NodeEntry) { n.HardwareProfile.CPUCores = 1 }},
		{"ram", "ram", types.MarketplaceBatchCompute, func(n *NodeEntry) { n.HardwareProfile.RAMMB = 1 }},
		{"storage", "storage", types.MarketplaceBatchCompute, func(n *NodeEntry) { n.HardwareProfile.StorageGB = 1 }},
		{"compute consent", "compute_disabled", types.MarketplaceBatchCompute, func(n *NodeEntry) { n.OptOutCompute = true }},
		{"storage consent", "storage_disabled", types.MarketplaceObjectStorage, func(n *NodeEntry) { n.OptOutStorage = true }},
		{"print consent", "printing_disabled", types.MarketplacePrintTraditional, func(n *NodeEntry) { n.OptOutPrinting = true }},
		{"no printer", "printing_disabled", types.MarketplacePrint3D, func(n *NodeEntry) { n.HasEnabledPrinter = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := newOnlineNode("catalog-fixture", "US", 8, 16384, 100, true)
			node.HasEnabledPrinter = true
			tc.change(&node)
			req := MatchRequest{WorkloadType: tc.workload, CountryConstraint: "US", CPUCores: 4, RAMMB: 8192, StorageGB: 20, GPURequired: true}
			code := placementBlocker(node, req)
			if code != tc.want {
				t.Fatalf("placement blocker = %q, want %q", code, tc.want)
			}
			description := (MatchBlocker{Code: code, Count: 1}).Description()
			if strings.Contains(description, "another placement requirement") {
				t.Fatalf("placement blocker %q is missing from description catalog", code)
			}
		})
	}
}
