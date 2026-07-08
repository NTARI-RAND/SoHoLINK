package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/sounding"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

// HardwareProfile holds the agent-reported capabilities of a node.
type HardwareProfile struct {
	CPUCores      int
	RAMMB         int
	GPUPresent    bool
	StorageGB     int
	BandwidthMbps int
}

// NodeEntry is the in-memory representation of a registered node.
type NodeEntry struct {
	NodeID          string
	ParticipantID   string // owner of this node; matches participants.id in the DB. Legacy field name was ProviderID (pre-migration 011, before unified participants table).
	NodeClass       string
	CountryCode     string
	Region          string
	HardwareProfile HardwareProfile
	LastHeartbeat   time.Time
	Status          string

	// Opt-out fields, refreshed by handleHeartbeat after each heartbeat.
	// FindMatch uses these to skip nodes that have opted out of a workload
	// category. Agent-side enforcement remains the canonical gate; this is
	// defense-in-depth at dispatch time. Default zero values mean "not opted
	// out, no enabled printers" — safe for fresh nodes that haven't yet
	// reported via heartbeat.
	OptOutCompute     bool
	OptOutStorage     bool
	OptOutPrinting    bool
	HasEnabledPrinter bool
}

// NodeOptOutState carries opt-out flags from the DB into the in-memory
// registry via UpdateOptOut.
type NodeOptOutState struct {
	OptOutCompute     bool
	OptOutStorage     bool
	OptOutPrinting    bool
	HasEnabledPrinter bool
}

// MatchRequest describes the resource requirements for a workload placement.
type MatchRequest struct {
	// WorkloadType drives both dispatch and opt-out filtering: candidate nodes
	// that have opted out of the corresponding agent category (compute / storage
	// / printing) are excluded from the result. The agent-side opt-out store
	// remains the canonical enforcement gate; this filter is defense-in-depth.
	// WorkloadType="" disables opt-out filtering (legacy callers / tests).
	WorkloadType                 types.MarketplaceWorkloadType
	CountryConstraint            string // empty = any country
	CPUCores                     int
	RAMMB                        int
	GPURequired                  bool
	StorageGB                    int
	ExcludedNodeIDs              []string // nodes that have already declined this job
	ExcludeConsumerParticipantID string   // C5: for print workloads only, exclude nodes owned by this participant. Prevents self-print (platform extraction on a transaction the participant could perform unaided is predatory by the platform operator). Compute/storage self-use is legitimate, so this field is interpreted only on print workload types.
}

// NodeRegistry is a concurrency-safe in-memory store of active nodes.
type NodeRegistry struct {
	mu    sync.RWMutex
	nodes map[string]NodeEntry
}

func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{nodes: make(map[string]NodeEntry)}
}

// Register adds or replaces a node entry.
func (r *NodeRegistry) Register(entry NodeEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[entry.NodeID] = entry
}

// Heartbeat updates the lastHeartbeat timestamp for a node.
// Returns an error if the node is not registered.
func (r *NodeRegistry) Heartbeat(nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.nodes[nodeID]
	if !ok {
		return fmt.Errorf("heartbeat: node %s not found", nodeID)
	}
	entry.LastHeartbeat = time.Now()
	r.nodes[nodeID] = entry
	return nil
}

// UpdateOptOut overwrites a node's opt-out fields. Returns an error if the
// node is not registered. Callers (typically handleHeartbeat) read current
// opt-out state from the DB and forward it here so FindMatch can filter
// without DB access.
func (r *NodeRegistry) UpdateOptOut(nodeID string, state NodeOptOutState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.nodes[nodeID]
	if !ok {
		return fmt.Errorf("update opt-out: node %s not found", nodeID)
	}
	entry.OptOutCompute = state.OptOutCompute
	entry.OptOutStorage = state.OptOutStorage
	entry.OptOutPrinting = state.OptOutPrinting
	entry.HasEnabledPrinter = state.HasEnabledPrinter
	r.nodes[nodeID] = entry
	return nil
}

// Evict removes a node from the registry.
func (r *NodeRegistry) Evict(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, nodeID)
}

// FindMatch returns all online nodes that satisfy req.
// Go map iteration is intentionally random, so candidate order is
// non-deterministic. Phase 1 Step 4 (Scheduler) scores and ranks this list.
// CountryConstraint is a hard requirement when non-empty.
func (r *NodeRegistry) FindMatch(req MatchRequest) ([]NodeEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	excluded := make(map[string]bool, len(req.ExcludedNodeIDs))
	for _, id := range req.ExcludedNodeIDs {
		excluded[id] = true
	}

	var candidates []NodeEntry
	for _, node := range r.nodes {
		if excluded[node.NodeID] {
			continue
		}
		if node.Status != "online" {
			continue
		}
		if req.CountryConstraint != "" && node.CountryCode != req.CountryConstraint {
			continue
		}
		if req.GPURequired && !node.HardwareProfile.GPUPresent {
			continue
		}
		if node.HardwareProfile.CPUCores < req.CPUCores {
			continue
		}
		if node.HardwareProfile.RAMMB < req.RAMMB {
			continue
		}
		if node.HardwareProfile.StorageGB < req.StorageGB {
			continue
		}
		// Opt-out filter. If WorkloadType is set and maps to a known agent
		// category, skip nodes that have opted out of that category. Printing
		// additionally requires at least one enabled printer.
		if req.WorkloadType != "" {
			if cat, err := MarketplaceToAgent(req.WorkloadType); err == nil {
				switch cat {
				case agent.WorkloadCompute:
					if node.OptOutCompute {
						continue
					}
				case agent.WorkloadStorage:
					if node.OptOutStorage {
						continue
					}
				case agent.WorkloadPrintTraditional, agent.WorkloadPrint3D:
					if node.OptOutPrinting || !node.HasEnabledPrinter {
						continue
					}
					// C5 self-print exclusion: a print workload routed to a node owned by the
					// consumer means the platform takes its share on a transaction the
					// participant could perform unaided. Reject the match.
					if req.ExcludeConsumerParticipantID != "" && node.ParticipantID == req.ExcludeConsumerParticipantID {
						continue
					}
				}
			}
		}
		candidates = append(candidates, node)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no available nodes match request")
	}
	return candidates, nil
}

// CapacityInputs returns a point-in-time supply snapshot of every ONLINE node,
// shaped for demand-sounding aggregation (sounding.AggregateCapacity). It is a
// read-only helper for the capacity sampler; it does NOT touch matching or
// placement. Units mirror operator_capacity_snapshots: VCPUs from CPUCores,
// MemMB from RAMMB, DiskMB from StorageGB (×1024).
//
// OperatorID is set to sounding.OperatorUnknown: nodes carry a ParticipantID,
// not an operator_id, and the node→operator mapping (frontend-as-operator) is
// not yet wired. When it lands, populate OperatorID here.
func (r *NodeRegistry) CapacityInputs() []sounding.NodeCapacityInput {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]sounding.NodeCapacityInput, 0, len(r.nodes))
	for _, node := range r.nodes {
		if node.Status != "online" {
			continue
		}
		out = append(out, sounding.NodeCapacityInput{
			OperatorID:        sounding.OperatorUnknown,
			NodeClass:         node.NodeClass,
			VCPUs:             node.HardwareProfile.CPUCores,
			MemMB:             int64(node.HardwareProfile.RAMMB),
			DiskMB:            int64(node.HardwareProfile.StorageGB) * 1024,
			OptOutCompute:     node.OptOutCompute,
			OptOutStorage:     node.OptOutStorage,
			OptOutPrinting:    node.OptOutPrinting,
			HasEnabledPrinter: node.HasEnabledPrinter,
		})
	}
	return out
}

// StartEvictionLoop runs a background goroutine that evicts nodes whose last
// heartbeat is older than maxAge. It checks every 30 seconds and stops when
// ctx is cancelled.
func StartEvictionLoop(ctx context.Context, registry *NodeRegistry, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				registry.evictStale(maxAge)
			}
		}
	}()
}

func (r *NodeRegistry) evictStale(maxAge time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for id, node := range r.nodes {
		if node.LastHeartbeat.Before(cutoff) {
			delete(r.nodes, id)
		}
	}
}
