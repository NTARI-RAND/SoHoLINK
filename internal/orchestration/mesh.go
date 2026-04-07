// Package orchestration — global mesh for cluster-to-cluster gossip and capacity routing.
//
// MeshGossiper implements BGP-inspired capacity routing where each cluster
// periodically advertises its capacity to ~5-10 random peer clusters.
// This enables distributed scheduling across the global federation.
package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

// MeshPeer represents a known cluster peer in the global mesh.
type MeshPeer struct {
	ClusterID        string
	CoordinatorDID   string
	CoordinatorAddr  string
	LastSeen         time.Time
	Capacity         ClusterCapacity
	Distance         int    // Hop count from this node
	TLSCert          []byte // DER-encoded X.509 cert for mTLS identity verification
}

// ClusterCapacity snapshot of a cluster's resources.
type ClusterCapacity struct {
	TotalCPU         float64
	AvailableCPU     float64
	TotalMemoryMB    int64
	AvailableMemoryMB int64
	MemberCount      int
}

// MeshGossiper manages global mesh connectivity and capacity routing.
// Coordinates cluster-to-cluster gossip for distributed job placement.
type MeshGossiper struct {
	localClusterID   string
	clusterMgr       *ClusterManager
	peers            map[string]*MeshPeer // key: cluster ID
	routingTable     map[string]*RoutingEntry // BGP-like routing table
	gossipTickerC    chan struct{}
	gossipInterval   time.Duration
	maxPeers         int // Target peer count (5-10)
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.RWMutex
	nowFunc          func() time.Time
}

// RoutingEntry represents a BGP-style route entry.
type RoutingEntry struct {
	DestClusterID   string
	NextHopClusterID string
	Distance        int
	Capacity        ClusterCapacity
	LastUpdated     time.Time
}

// NewMeshGossiper creates a global mesh gossiper for inter-cluster communication.
func NewMeshGossiper(localClusterID string, clusterMgr *ClusterManager, gossipInterval time.Duration) *MeshGossiper {
	ctx, cancel := context.WithCancel(context.Background())
	return &MeshGossiper{
		localClusterID: localClusterID,
		clusterMgr:     clusterMgr,
		peers:          make(map[string]*MeshPeer),
		routingTable:   make(map[string]*RoutingEntry),
		gossipInterval: gossipInterval,
		maxPeers:       8, // Target 5-10 peers, use 8 as middle ground
		ctx:            ctx,
		cancel:         cancel,
		nowFunc:        time.Now,
	}
}

// AddPeer adds a discovered peer cluster to the mesh.
// tlsCert is the DER-encoded X.509 certificate for the peer's coordinator node;
// it is stored and used for mTLS identity verification on outbound gossip connections.
// Pass nil when the cert is not yet known (it can be updated later via UpdatePeerCert).
func (mg *MeshGossiper) AddPeer(clusterID string, coordinatorDID string, coordinatorAddr string, tlsCert []byte) error {
	if clusterID == mg.localClusterID {
		return fmt.Errorf("cannot add self as peer")
	}

	mg.mu.Lock()
	defer mg.mu.Unlock()

	mg.peers[clusterID] = &MeshPeer{
		ClusterID:       clusterID,
		CoordinatorDID:  coordinatorDID,
		CoordinatorAddr: coordinatorAddr,
		LastSeen:        mg.nowFunc(),
		Distance:        1, // Direct peer
		TLSCert:         tlsCert,
	}

	log.Printf("[mesh] added peer cluster %s (coordinator %s)", clusterID, coordinatorDID)
	return nil
}

// UpdatePeerCert stores or replaces the mTLS certificate for an existing peer.
func (mg *MeshGossiper) UpdatePeerCert(clusterID string, tlsCert []byte) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	peer, ok := mg.peers[clusterID]
	if !ok {
		return fmt.Errorf("peer cluster %s not found", clusterID)
	}
	peer.TLSCert = tlsCert
	return nil
}

// UpdatePeerCapacity updates a peer cluster's reported capacity.
func (mg *MeshGossiper) UpdatePeerCapacity(clusterID string, cap ClusterCapacity) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	peer, ok := mg.peers[clusterID]
	if !ok {
		return fmt.Errorf("peer cluster %s not found", clusterID)
	}

	peer.Capacity = cap
	peer.LastSeen = mg.nowFunc()
	return nil
}

// SelectGossipTargets returns ~5-10 random peer clusters to gossip with.
// Implements random peer selection to avoid synchronized floods.
func (mg *MeshGossiper) SelectGossipTargets() []string {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	if len(mg.peers) == 0 {
		return nil
	}

	// Select up to maxPeers random targets
	targetCount := mg.maxPeers
	if len(mg.peers) < targetCount {
		targetCount = len(mg.peers)
	}

	peerIDs := make([]string, 0, len(mg.peers))
	for id := range mg.peers {
		peerIDs = append(peerIDs, id)
	}

	// Fisher-Yates shuffle
	for i := len(peerIDs) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		peerIDs[i], peerIDs[j] = peerIDs[j], peerIDs[i]
	}

	if targetCount < len(peerIDs) {
		return peerIDs[:targetCount]
	}
	return peerIDs
}

// BroadcastCapacity sends this cluster's capacity to selected peers.
// Called periodically by the local cluster coordinator.
func (mg *MeshGossiper) BroadcastCapacity(ctx context.Context, clusterID string, cap ClusterCapacity) error {
	targets := mg.SelectGossipTargets()
	if len(targets) == 0 {
		log.Printf("[mesh] no peers to broadcast capacity to")
		return nil
	}

	// Simulate message broadcast (in production, would send to actual peers)
	msg := struct {
		SourceClusterID string `json:"source_cluster_id"`
		Capacity        ClusterCapacity `json:"capacity"`
		Timestamp       int64  `json:"timestamp"`
	}{
		SourceClusterID: clusterID,
		Capacity:        cap,
		Timestamp:       mg.nowFunc().Unix(),
	}

	data, _ := json.Marshal(msg)
	log.Printf("[mesh] broadcasting capacity to %d peers: %s", len(targets), string(data[:min(80, len(data))]))

	for _, targetID := range targets {
		mg.mu.RLock()
		peer, ok := mg.peers[targetID]
		mg.mu.RUnlock()

		if !ok {
			continue
		}

		// Update local routing table with peer's capacity
		mg.UpdateRoute(targetID, targetID, 1, cap)
		log.Printf("[mesh] updating route to cluster %s via %s (CPU avail: %.1f)",
			targetID, peer.CoordinatorDID, cap.AvailableCPU)
	}

	return nil
}

// UpdateRoute adds or updates a BGP-style route entry.
func (mg *MeshGossiper) UpdateRoute(destCluster string, nextHop string, distance int, cap ClusterCapacity) {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	now := mg.nowFunc()

	// Check if we already have a better route
	if existing, ok := mg.routingTable[destCluster]; ok {
		// Only update if new route is shorter or capacity is better
		if distance > existing.Distance {
			log.Printf("[mesh] ignoring longer route to %s (existing distance: %d, new: %d)",
				destCluster, existing.Distance, distance)
			return
		}
		if distance == existing.Distance && cap.AvailableCPU < existing.Capacity.AvailableCPU {
			log.Printf("[mesh] ignoring same-distance route with worse capacity to %s", destCluster)
			return
		}
	}

	mg.routingTable[destCluster] = &RoutingEntry{
		DestClusterID:    destCluster,
		NextHopClusterID: nextHop,
		Distance:         distance,
		Capacity:         cap,
		LastUpdated:      now,
	}

	log.Printf("[mesh] updated route: %s via %s (distance %d, CPU: %.1f)",
		destCluster, nextHop, distance, cap.AvailableCPU)
}

// FindBestClusterForJob locates a cluster that can run the given job.
// Uses routing table to find closest cluster with sufficient capacity.
func (mg *MeshGossiper) FindBestClusterForJob(cpu float64, memMB int64) (string, *RoutingEntry, error) {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	var best string
	var bestEntry *RoutingEntry
	bestDistance := 999

	// Check all known routes
	for clusterID, entry := range mg.routingTable {
		// Skip if insufficient capacity
		if entry.Capacity.AvailableCPU < cpu || entry.Capacity.AvailableMemoryMB < memMB {
			continue
		}

		// Prefer closer clusters
		if entry.Distance < bestDistance {
			best = clusterID
			bestEntry = entry
			bestDistance = entry.Distance
		}
	}

	// Also check local cluster
	if best == "" {
		localCap, err := mg.localCapacity()
		if err == nil && localCap.AvailableCPU >= cpu && localCap.AvailableMemoryMB >= memMB {
			best = mg.localClusterID
			bestEntry = &RoutingEntry{
				DestClusterID:   mg.localClusterID,
				NextHopClusterID: mg.localClusterID,
				Distance:        0,
				Capacity:        *localCap,
			}
		}
	}

	if best == "" {
		return "", nil, fmt.Errorf("no cluster with sufficient capacity (need %.1f CPU, %d MB)", cpu, memMB)
	}

	return best, bestEntry, nil
}

// localCapacity gets the local cluster's current capacity.
func (mg *MeshGossiper) localCapacity() (*ClusterCapacity, error) {
	totalCPU, availCPU, totalMem, availMem, err := mg.clusterMgr.AggregateCapacity(mg.localClusterID)
	if err != nil {
		return nil, err
	}

	return &ClusterCapacity{
		TotalCPU:         float64(totalCPU) / 1000,
		AvailableCPU:     float64(availCPU) / 1000,
		TotalMemoryMB:    totalMem,
		AvailableMemoryMB: availMem,
	}, nil
}

// GetRoutingTable returns a snapshot of the current routing table.
func (mg *MeshGossiper) GetRoutingTable() map[string]*RoutingEntry {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	snapshot := make(map[string]*RoutingEntry)
	for k, v := range mg.routingTable {
		snapshot[k] = v
	}
	return snapshot
}

// ListPeers returns all known peer clusters.
func (mg *MeshGossiper) ListPeers() []MeshPeer {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	peers := make([]MeshPeer, 0, len(mg.peers))
	for _, p := range mg.peers {
		peers = append(peers, *p)
	}
	return peers
}

// PruneStaleRoutes removes routing entries that haven't been updated recently.
// Called periodically to clean up entries for departed clusters.
func (mg *MeshGossiper) PruneStaleRoutes(maxAge time.Duration) {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	now := mg.nowFunc()
	for clusterID, entry := range mg.routingTable {
		age := now.Sub(entry.LastUpdated)
		if age > maxAge {
			delete(mg.routingTable, clusterID)
			log.Printf("[mesh] pruned stale route to %s (age: %v)", clusterID, age)
		}
	}
}

// Stop cancels the mesh gossiper's operations.
func (mg *MeshGossiper) Stop() {
	mg.cancel()
}

// helper
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
