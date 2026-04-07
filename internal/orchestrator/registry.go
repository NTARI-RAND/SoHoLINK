package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"
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
	ProviderID      string
	NodeClass       string
	CountryCode     string
	Region          string
	HardwareProfile HardwareProfile
	LastHeartbeat   time.Time
	Status          string
}

// MatchRequest describes the resource requirements for a workload placement.
type MatchRequest struct {
	WorkloadType      string
	CountryConstraint string // empty = any country
	CPUCores          int
	RAMMB             int
	GPURequired       bool
	StorageGB         int
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
	var candidates []NodeEntry
	for _, node := range r.nodes {
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
		candidates = append(candidates, node)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no available nodes match request")
	}
	return candidates, nil
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
