// Package orchestration — two-tier federation topology with local clusters and global mesh.
//
// ClusterManager implements local cluster formation and coordinator election.
// Nodes on the same subnet form a cluster for efficient local task placement.
// One coordinator per cluster aggregates capacity and relays requests to the global mesh.
package orchestration

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"sync"
	"time"
)

// ClusterMember represents a node's participation in a local cluster.
type ClusterMember struct {
	NodeDID           string
	Address           string
	LastHeartbeat     time.Time
	UptimeSeconds     int64
	ReputationScore   int
	AvailableCPU      float64
	AvailableMemoryMB int64
	AvailableDiskGB   int64
	IsCoordinator     bool
}

// Cluster represents a local group of nodes on the same network subnet.
// Coordinates job placement and capacity aggregation within the cluster.
type Cluster struct {
	ClusterID      string             // e.g., "10.0.0.0/24"
	SubnetCIDR     string             // Network CIDR (e.g., "10.0.0.0/24")
	Members        map[string]*ClusterMember // key: node DID
	CoordinatorDID string             // Current cluster coordinator
	CreatedAt      time.Time
	LastUpdated    time.Time
	mu             sync.RWMutex
}

// ClusterManager maintains the set of active local clusters and their membership.
type ClusterManager struct {
	localNodeDID    string                  // This node's DID
	localAddress    string                  // This node's address
	clusters        map[string]*Cluster     // key: cluster ID (CIDR)
	nodeToCluster   map[string]string       // key: node DID, value: cluster ID
	electionTickerC chan struct{}           // Trigger coordinator election
	globalMeshC     chan MeshUpdate         // Updates to/from global mesh
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex

	// For testing/injection
	nowFunc func() time.Time // Testable time source
}

// MeshUpdate represents a capacity/availability update for global mesh routing.
type MeshUpdate struct {
	ClusterID           string
	CoordinatorDID      string
	TotalCapacityCPU    float64
	TotalCapacityMemMB  int64
	AvailableCapacityCPU float64
	AvailableCapacityMemMB int64
	MemberCount         int
}

// NewClusterManager creates a cluster manager for the local node.
func NewClusterManager(localNodeDID, localAddress string) *ClusterManager {
	ctx, cancel := context.WithCancel(context.Background())
	cm := &ClusterManager{
		localNodeDID:  localNodeDID,
		localAddress:  localAddress,
		clusters:      make(map[string]*Cluster),
		nodeToCluster: make(map[string]string),
		globalMeshC:   make(chan MeshUpdate, 100),
		nowFunc:       time.Now,
		ctx:           ctx,
		cancel:        cancel,
	}
	return cm
}

// AddNode adds or updates a node in the appropriate cluster based on its address.
// Nodes on the same subnet are grouped into the same cluster.
func (cm *ClusterManager) AddNode(nodeDID, nodeAddress string, uptime int64, reputation int, cpu float64, memMB int64, diskGB int64) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Detect subnet (determine cluster ID)
	clusterID := detectSubnet(nodeAddress)
	if clusterID == "" {
		return fmt.Errorf("cannot determine subnet for address %s", nodeAddress)
	}

	// Get or create cluster
	cluster, ok := cm.clusters[clusterID]
	if !ok {
		cluster = &Cluster{
			ClusterID:   clusterID,
			SubnetCIDR:  clusterID,
			Members:     make(map[string]*ClusterMember),
			CreatedAt:   cm.nowFunc(),
			LastUpdated: cm.nowFunc(),
		}
		cm.clusters[clusterID] = cluster
	}

	// Add member to cluster
	cluster.mu.Lock()
	cluster.Members[nodeDID] = &ClusterMember{
		NodeDID:           nodeDID,
		Address:           nodeAddress,
		LastHeartbeat:     cm.nowFunc(),
		UptimeSeconds:     uptime,
		ReputationScore:   reputation,
		AvailableCPU:      cpu,
		AvailableMemoryMB: memMB,
		AvailableDiskGB:   diskGB,
	}
	cluster.LastUpdated = cm.nowFunc()
	cluster.mu.Unlock()

	// Track node-to-cluster mapping
	cm.nodeToCluster[nodeDID] = clusterID

	log.Printf("[cluster] added node %s to cluster %s", nodeDID, clusterID)
	return nil
}

// ElectCoordinator runs a coordinator election for the given cluster.
// Election criteria (in priority order):
//   1. Uptime (prefer long-running nodes)
//   2. Reputation score (prefer trusted nodes)
//   3. Node DID (deterministic tiebreaker)
//
// Returns the elected coordinator's DID.
func (cm *ClusterManager) ElectCoordinator(clusterID string) (string, error) {
	cm.mu.RLock()
	cluster, ok := cm.clusters[clusterID]
	cm.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("cluster %s not found", clusterID)
	}

	cluster.mu.RLock()
	defer cluster.mu.RUnlock()

	if len(cluster.Members) == 0 {
		return "", fmt.Errorf("cluster %s has no members", clusterID)
	}

	// Collect and sort members by election criteria
	members := make([]*ClusterMember, 0, len(cluster.Members))
	for _, m := range cluster.Members {
		members = append(members, m)
	}

	sort.Slice(members, func(i, j int) bool {
		// Primary: longest uptime
		if members[i].UptimeSeconds != members[j].UptimeSeconds {
			return members[i].UptimeSeconds > members[j].UptimeSeconds
		}
		// Secondary: highest reputation
		if members[i].ReputationScore != members[j].ReputationScore {
			return members[i].ReputationScore > members[j].ReputationScore
		}
		// Tertiary: alphabetical by DID (deterministic tiebreaker)
		return members[i].NodeDID < members[j].NodeDID
	})

	elected := members[0].NodeDID
	cm.mu.Lock()
	cm.clusters[clusterID].CoordinatorDID = elected
	cm.mu.Unlock()

	log.Printf("[cluster] elected coordinator %s for cluster %s (uptime=%ds, reputation=%d)",
		elected, clusterID, members[0].UptimeSeconds, members[0].ReputationScore)

	return elected, nil
}

// IsCoordinator returns true if the given node is the current coordinator of its cluster.
func (cm *ClusterManager) IsCoordinator(nodeDID string) bool {
	cm.mu.RLock()
	clusterID, ok := cm.nodeToCluster[nodeDID]
	cm.mu.RUnlock()

	if !ok {
		return false
	}

	cm.mu.RLock()
	cluster, ok := cm.clusters[clusterID]
	cm.mu.RUnlock()

	if !ok {
		return false
	}

	cluster.mu.RLock()
	defer cluster.mu.RUnlock()
	return cluster.CoordinatorDID == nodeDID
}

// GetClusterMembers returns all members of a cluster (thread-safe snapshot).
func (cm *ClusterManager) GetClusterMembers(clusterID string) ([]*ClusterMember, error) {
	cm.mu.RLock()
	cluster, ok := cm.clusters[clusterID]
	cm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("cluster %s not found", clusterID)
	}

	cluster.mu.RLock()
	defer cluster.mu.RUnlock()

	members := make([]*ClusterMember, 0, len(cluster.Members))
	for _, m := range cluster.Members {
		members = append(members, m)
	}
	return members, nil
}

// AggregateCapacity computes total and available capacity for a cluster.
// Used by coordinator to report cluster capacity to global mesh.
func (cm *ClusterManager) AggregateCapacity(clusterID string) (totalCPU, availCPU, totalMemMB, availMemMB int64, err error) {
	members, err := cm.GetClusterMembers(clusterID)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	for _, m := range members {
		availCPU += int64(m.AvailableCPU * 1000) // milli-CPUs
		availMemMB += m.AvailableMemoryMB
	}

	// For demo, assume total = 2x available (50% utilization on average)
	totalCPU = availCPU * 2
	totalMemMB = availMemMB * 2

	return totalCPU, availCPU, totalMemMB, availMemMB, nil
}

// PublishClusterCapacity publishes this cluster's capacity to the global mesh.
// Called periodically by the coordinator to share cluster state.
func (cm *ClusterManager) PublishClusterCapacity(clusterID string) error {
	cm.mu.RLock()
	cluster, ok := cm.clusters[clusterID]
	cm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("cluster %s not found", clusterID)
	}

	totalCPU, availCPU, totalMemMB, availMemMB, err := cm.AggregateCapacity(clusterID)
	if err != nil {
		return err
	}

	cluster.mu.RLock()
	memberCount := len(cluster.Members)
	coordDID := cluster.CoordinatorDID
	cluster.mu.RUnlock()

	update := MeshUpdate{
		ClusterID:              clusterID,
		CoordinatorDID:         coordDID,
		TotalCapacityCPU:       float64(totalCPU) / 1000,
		AvailableCapacityCPU:   float64(availCPU) / 1000,
		TotalCapacityMemMB:     totalMemMB,
		AvailableCapacityMemMB: availMemMB,
		MemberCount:            memberCount,
	}

	// Non-blocking send to mesh channel
	select {
	case cm.globalMeshC <- update:
		log.Printf("[cluster] published capacity for cluster %s: CPU=%.1f/%.1f, Mem=%d/%d MB",
			clusterID, float64(availCPU)/1000, float64(totalCPU)/1000, availMemMB, totalMemMB)
	default:
		log.Printf("[cluster] mesh update channel full, dropping capacity update")
	}

	return nil
}

// GetNodeCluster returns the cluster ID for a given node.
func (cm *ClusterManager) GetNodeCluster(nodeDID string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.nodeToCluster[nodeDID]
}

// ListClusters returns all cluster IDs.
func (cm *ClusterManager) ListClusters() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ids := make([]string, 0, len(cm.clusters))
	for id := range cm.clusters {
		ids = append(ids, id)
	}
	return ids
}

// detectSubnet extracts the subnet CIDR from a node address.
// For now, uses a simple /24 classification based on the first 3 octets.
// In production, this would integrate with actual network routing tables.
func detectSubnet(nodeAddress string) string {
	// Parse IP address
	host, _, err := net.SplitHostPort(nodeAddress)
	if err != nil {
		// Try parsing as plain IP
		host = nodeAddress
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}

	// Convert to IPv4
	ipv4 := ip.To4()
	if ipv4 == nil {
		// For IPv6, use /48 subnet
		return ip.String() + "/48"
	}

	// For IPv4, use /24 subnet (e.g., "192.168.1.0/24")
	return fmt.Sprintf("%d.%d.%d.0/24", ipv4[0], ipv4[1], ipv4[2])
}

// Stop cancels the cluster manager's background operations.
func (cm *ClusterManager) Stop() {
	cm.cancel()
}
