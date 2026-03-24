package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleGetClusterMembers returns members of the local cluster (GET /api/topology/cluster/members)
func (s *Server) handleGetClusterMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.scheduler == nil {
		http.Error(w, "Scheduler not available", http.StatusServiceUnavailable)
		return
	}

	// Get cluster manager from scheduler
	clusterMgr := s.scheduler.GetClusterManager()
	if clusterMgr == nil {
		http.Error(w, "Cluster manager not available", http.StatusServiceUnavailable)
		return
	}

	// For now, return the first (local) cluster
	// In production, this would be more sophisticated
	clusters := clusterMgr.ListClusters()
	if len(clusters) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"cluster_id": "unknown",
			"members":    []interface{}{},
		})
		return
	}

	localCluster := clusters[0]
	members, err := clusterMgr.GetClusterMembers(localCluster)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get cluster members: %v", err), http.StatusInternalServerError)
		return
	}

	// Format response
	type MemberResponse struct {
		NodeDID           string  `json:"node_did"`
		Address           string  `json:"address"`
		AvailableCPU      float64 `json:"available_cpu"`
		AvailableMemoryMB int64   `json:"available_memory_mb"`
		AvailableDiskGB   int64   `json:"available_disk_gb"`
		UptimeSeconds     int64   `json:"uptime_seconds"`
		ReputationScore   int     `json:"reputation_score"`
		IsCoordinator     bool    `json:"is_coordinator"`
	}

	memberResponses := make([]MemberResponse, 0, len(members))
	for _, m := range members {
		memberResponses = append(memberResponses, MemberResponse{
			NodeDID:           m.NodeDID,
			Address:           m.Address,
			AvailableCPU:      m.AvailableCPU,
			AvailableMemoryMB: m.AvailableMemoryMB,
			AvailableDiskGB:   m.AvailableDiskGB,
			UptimeSeconds:     m.UptimeSeconds,
			ReputationScore:   m.ReputationScore,
			IsCoordinator:     m.IsCoordinator,
		})
	}

	coordinator, _ := clusterMgr.ElectCoordinator(localCluster)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cluster_id":    localCluster,
		"coordinator":   coordinator,
		"member_count":  len(members),
		"members":       memberResponses,
	})
}

// handleGetMeshPeers returns known peer clusters in the global mesh (GET /api/topology/mesh/peers)
func (s *Server) handleGetMeshPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.scheduler == nil {
		http.Error(w, "Scheduler not available", http.StatusServiceUnavailable)
		return
	}

	meshGossip := s.scheduler.GetMeshGossiper()
	if meshGossip == nil {
		http.Error(w, "Mesh gossiper not available", http.StatusServiceUnavailable)
		return
	}

	peers := meshGossip.ListPeers()

	type PeerResponse struct {
		ClusterID       string  `json:"cluster_id"`
		CoordinatorDID  string  `json:"coordinator_did"`
		Distance        int     `json:"distance"`
		TotalCPU        float64 `json:"total_cpu"`
		AvailableCPU    float64 `json:"available_cpu"`
		TotalMemoryMB   int64   `json:"total_memory_mb"`
		AvailableMemMB  int64   `json:"available_memory_mb"`
	}

	peerResponses := make([]PeerResponse, 0, len(peers))
	for _, p := range peers {
		peerResponses = append(peerResponses, PeerResponse{
			ClusterID:      p.ClusterID,
			CoordinatorDID: p.CoordinatorDID,
			Distance:       p.Distance,
			TotalCPU:       p.Capacity.TotalCPU,
			AvailableCPU:   p.Capacity.AvailableCPU,
			TotalMemoryMB:  p.Capacity.TotalMemoryMB,
			AvailableMemMB: p.Capacity.AvailableMemoryMB,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"peer_count": len(peerResponses),
		"peers":      peerResponses,
	})
}

// handleGetRoutingTable returns the BGP-style routing table (GET /api/topology/routing-table)
func (s *Server) handleGetRoutingTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.scheduler == nil {
		http.Error(w, "Scheduler not available", http.StatusServiceUnavailable)
		return
	}

	meshGossip := s.scheduler.GetMeshGossiper()
	if meshGossip == nil {
		http.Error(w, "Mesh gossiper not available", http.StatusServiceUnavailable)
		return
	}

	routingTable := meshGossip.GetRoutingTable()

	type RouteResponse struct {
		DestClusterID    string  `json:"dest_cluster_id"`
		NextHopClusterID string  `json:"next_hop_cluster_id"`
		Distance         int     `json:"distance"`
		AvailableCPU     float64 `json:"available_cpu"`
		AvailableMemMB   int64   `json:"available_memory_mb"`
		LastUpdated      int64   `json:"last_updated_unix"`
	}

	routes := make([]RouteResponse, 0, len(routingTable))
	for _, entry := range routingTable {
		routes = append(routes, RouteResponse{
			DestClusterID:    entry.DestClusterID,
			NextHopClusterID: entry.NextHopClusterID,
			Distance:         entry.Distance,
			AvailableCPU:     entry.Capacity.AvailableCPU,
			AvailableMemMB:   entry.Capacity.AvailableMemoryMB,
			LastUpdated:      entry.LastUpdated.Unix(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"route_count": len(routes),
		"routes":      routes,
	})
}
