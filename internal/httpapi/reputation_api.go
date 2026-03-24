package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/reputation"
)

// handleGetReputationLedger returns the full reputation ledger (GET /api/reputation/ledger)
func (s *Server) handleGetReputationLedger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reputationMgr := s.getReputationManager()
	if reputationMgr == nil {
		http.Error(w, "Reputation manager not available", http.StatusServiceUnavailable)
		return
	}

	// Query parameter: ?min_score=50 to filter by minimum reputation
	minScore := 0
	if minStr := r.URL.Query().Get("min_score"); minStr != "" {
		var n int
		if _, err := fmt.Sscanf(minStr, "%d", &n); err == nil && n >= 0 && n <= 100 {
			minScore = n
		}
	}

	providers := reputationMgr.GetAllProviders()

	type ProviderReputation struct {
		NodeDID string `json:"node_did"`
		Score   int    `json:"reputation_score"`
	}

	var entries []ProviderReputation
	for nodeDID, score := range providers {
		if score >= minScore {
			entries = append(entries, ProviderReputation{
				NodeDID: nodeDID,
				Score:   score,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"provider_count": len(entries),
		"providers":      entries,
		"min_score":      minScore,
	})
}

// handleGetNodeReputation returns reputation history for a specific node (GET /api/reputation/nodes/{node_did})
func (s *Server) handleGetNodeReputation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reputationMgr := s.getReputationManager()
	if reputationMgr == nil {
		http.Error(w, "Reputation manager not available", http.StatusServiceUnavailable)
		return
	}

	// Extract node DID from path: /api/reputation/nodes/{node_did}
	path := strings.TrimPrefix(r.URL.Path, "/api/reputation/nodes/")
	nodeDID := strings.Split(path, "/")[0]

	if nodeDID == "" {
		http.Error(w, "Node DID required", http.StatusBadRequest)
		return
	}

	history := reputationMgr.GetNodeHistory(nodeDID)
	stats := reputationMgr.GetNodeStats(nodeDID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_did":       nodeDID,
		"current_score":  stats.CurrentScore,
		"total_jobs":     stats.TotalJobs,
		"average_settlement_rate": stats.AverageSettlementRate,
		"average_failure_rate":    stats.AverageFailureRate,
		"history_length":          len(history),
		"history":                 history,
		"stats":                   stats,
	})
}

// handleGetReputationStats returns aggregate statistics (GET /api/reputation/stats)
func (s *Server) handleGetReputationStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reputationMgr := s.getReputationManager()
	if reputationMgr == nil {
		http.Error(w, "Reputation manager not available", http.StatusServiceUnavailable)
		return
	}

	providers := reputationMgr.GetAllProviders()

	// Compute aggregate statistics
	totalProviders := len(providers)
	var sumScore, minScore, maxScore, avgScore int
	if totalProviders > 0 {
		minScore = 100
		maxScore = 0
		for _, score := range providers {
			sumScore += score
			if score < minScore {
				minScore = score
			}
			if score > maxScore {
				maxScore = score
			}
		}
		avgScore = sumScore / totalProviders
	}

	// Count by tier
	tier1Count := 0   // score >= 80 (excellent)
	tier2Count := 0   // score >= 60 (good)
	tier3Count := 0   // score >= 40 (acceptable)
	tier4Count := 0   // score < 40 (poor)
	for _, score := range providers {
		if score >= 80 {
			tier1Count++
		} else if score >= 60 {
			tier2Count++
		} else if score >= 40 {
			tier3Count++
		} else {
			tier4Count++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_providers":       totalProviders,
		"average_score":         avgScore,
		"min_score":             minScore,
		"max_score":             maxScore,
		"tier_excellent":        tier1Count,
		"tier_good":             tier2Count,
		"tier_acceptable":       tier3Count,
		"tier_poor":             tier4Count,
	})
}

// handleGetDynamicPrice returns the dynamic pricing for a node (GET /api/reputation/nodes/{node_did}/pricing)
func (s *Server) handleGetDynamicPrice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reputationMgr := s.getReputationManager()
	if reputationMgr == nil {
		http.Error(w, "Reputation manager not available", http.StatusServiceUnavailable)
		return
	}

	// Extract node DID from path: /api/reputation/nodes/{node_did}/pricing
	path := strings.TrimPrefix(r.URL.Path, "/api/reputation/nodes/")
	pathParts := strings.Split(path, "/")
	nodeDID := pathParts[0]

	if nodeDID == "" {
		http.Error(w, "Node DID required", http.StatusBadRequest)
		return
	}

	// Query parameter: ?base_price=100 (in cents)
	basePrice := int64(100) // default 100 cents
	if basePriceStr := r.URL.Query().Get("base_price"); basePriceStr != "" {
		var p int64
		if _, err := fmt.Sscanf(basePriceStr, "%d", &p); err == nil && p > 0 {
			basePrice = p
		}
	}

	score := reputationMgr.GetLatestScore(nodeDID)
	adjustedPrice := reputationMgr.ComputeDynamicPrice(nodeDID, basePrice)
	multiplier := reputationMgr.GetPricingMultiplier(nodeDID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_did":       nodeDID,
		"reputation_score": score,
		"base_price_cents": basePrice,
		"adjusted_price_cents": adjustedPrice,
		"multiplier":     fmt.Sprintf("%.2f", multiplier),
		"price_delta_pct": fmt.Sprintf("%.1f%%", (multiplier-1.0)*100.0),
	})
}

// handleVerifyChain verifies Merkle chain integrity (POST /api/reputation/verify/{node_did})
func (s *Server) handleVerifyChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.reputationMgr == nil {
		http.Error(w, "Reputation manager not available", http.StatusServiceUnavailable)
		return
	}

	// Extract node DID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/reputation/verify/")
	nodeDID := strings.Split(path, "/")[0]

	if nodeDID == "" {
		http.Error(w, "Node DID required", http.StatusBadRequest)
		return
	}

	// Cast to reputation manager
	reputationMgr, ok := s.reputationMgr.(*reputation.ReputationManager)
	if !ok {
		http.Error(w, "Reputation manager not available", http.StatusServiceUnavailable)
		return
	}

	err := reputationMgr.VerifyChain(nodeDID)

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_did": nodeDID,
			"verified": false,
			"error":    err.Error(),
		})
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_did": nodeDID,
			"verified": true,
		})
	}
}

// routeReputationNode dispatches reputation requests to the appropriate handler
func (s *Server) routeReputationNode(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/reputation/nodes/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) >= 2 && pathParts[1] == "pricing" {
		s.handleGetDynamicPrice(w, r)
	} else if len(pathParts) >= 1 && pathParts[0] != "" {
		s.handleGetNodeReputation(w, r)
	} else {
		http.Error(w, "Node DID required", http.StatusBadRequest)
	}
}

// Cast helper to convert interface{} reputationMgr to typed manager
func (s *Server) getReputationManager() *reputation.ReputationManager {
	if s.reputationMgr == nil {
		return nil
	}
	mgr, ok := s.reputationMgr.(*reputation.ReputationManager)
	if !ok {
		return nil
	}
	return mgr
}

// Update the handler functions to use the cast helper
func (s *Server) handleGetReputationLedgerTyped(w http.ResponseWriter, r *http.Request) {
	reputationMgr := s.getReputationManager()
	if reputationMgr == nil {
		http.Error(w, "Reputation manager not available", http.StatusServiceUnavailable)
		return
	}

	minScore := 0
	if minStr := r.URL.Query().Get("min_score"); minStr != "" {
		var n int
		if _, err := fmt.Sscanf(minStr, "%d", &n); err == nil && n >= 0 && n <= 100 {
			minScore = n
		}
	}

	providers := reputationMgr.GetAllProviders()

	type ProviderReputation struct {
		NodeDID string `json:"node_did"`
		Score   int    `json:"reputation_score"`
	}

	var entries []ProviderReputation
	for nodeDID, score := range providers {
		if score >= minScore {
			entries = append(entries, ProviderReputation{
				NodeDID: nodeDID,
				Score:   score,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"provider_count": len(entries),
		"providers":      entries,
		"min_score":      minScore,
	})
}
