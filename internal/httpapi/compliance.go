package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

// complianceGroupsResponse is the JSON shape for GET /api/compliance/groups.
type complianceGroupsResponse struct {
	Groups []complianceGroupInfo `json:"groups"`
}

type complianceGroupInfo struct {
	Group       string   `json:"group"`
	Members     []string `json:"members"`
	MemberCount int      `json:"member_count"`
}

// handleComplianceGroups lists all compliance groups and their member DIDs.
// GET /api/compliance/groups
func (s *Server) handleComplianceGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.complianceMgr == nil {
		http.Error(w, "compliance manager not configured", http.StatusServiceUnavailable)
		return
	}

	groups := []string{"baseline", "high-security", "data-residency", "gpu-tier", "tpm-verified"}
	var infos []complianceGroupInfo
	for _, g := range groups {
		members, err := s.complianceMgr.GetGroupMembers(r.Context(), g)
		if err != nil {
			http.Error(w, "failed to query group members", http.StatusInternalServerError)
			return
		}
		if members == nil {
			members = []string{}
		}
		infos = append(infos, complianceGroupInfo{
			Group:       g,
			Members:     members,
			MemberCount: len(members),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(complianceGroupsResponse{Groups: infos})
}

// complianceNodeResponse is the JSON shape for GET /api/compliance/nodes/{did}.
type complianceNodeResponse struct {
	NodeDID         string `json:"node_did"`
	ComplianceLevel string `json:"compliance_level"`
	ComplianceGroup string `json:"compliance_group"`
	SLATier         string `json:"sla_tier"`
	Attestation     string `json:"attestation,omitempty"`
	LastChecked     int64  `json:"last_checked"`
}

// handleComplianceNode returns the compliance status of a specific node.
// GET /api/compliance/nodes/{did}
func (s *Server) handleComplianceNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	did := strings.TrimPrefix(r.URL.Path, "/api/compliance/nodes/")
	if did == "" {
		http.Error(w, "missing node DID in path", http.StatusBadRequest)
		return
	}

	nodes, err := s.store.GetNodesByComplianceGroup(r.Context(), "")
	if err != nil {
		http.Error(w, "store query failed", http.StatusInternalServerError)
		return
	}

	for _, n := range nodes {
		if n.NodeDID == did {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(complianceNodeResponse{
				NodeDID:         n.NodeDID,
				ComplianceLevel: n.ComplianceLevel,
				ComplianceGroup: n.ComplianceGroup,
				SLATier:         n.SLATier,
				Attestation:     n.AttestationData,
				LastChecked:     n.LastComplianceCheck,
			})
			return
		}
	}
	http.Error(w, "node not found", http.StatusNotFound)
}

// handleComplianceAttest triggers a compliance check and re-attestation for a node.
// POST /api/compliance/attest/{did}
func (s *Server) handleComplianceAttest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.complianceMgr == nil {
		http.Error(w, "compliance manager not configured", http.StatusServiceUnavailable)
		return
	}

	did := strings.TrimPrefix(r.URL.Path, "/api/compliance/attest/")
	if did == "" {
		http.Error(w, "missing node DID in path", http.StatusBadRequest)
		return
	}

	attestation, level, details, passed, err := s.complianceMgr.CheckAndAttestRaw(r.Context(), did)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"node_did":    did,
		"level":       level,
		"passed":      passed,
		"details":     details,
		"attestation": attestation,
	})
}

// handlePeerDashboard proxies a request to a remote peer's /api/status endpoint.
// GET /api/peers/{did}/dashboard
func (s *Server) handlePeerDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract DID from /api/peers/{did}/dashboard
	path := strings.TrimPrefix(r.URL.Path, "/api/peers/")
	did := strings.TrimSuffix(path, "/dashboard")
	if did == "" || did == path {
		http.Error(w, "missing peer DID in path", http.StatusBadRequest)
		return
	}

	nodes, err := s.store.GetNodesByComplianceGroup(r.Context(), "")
	if err != nil {
		http.Error(w, "store query failed", http.StatusInternalServerError)
		return
	}

	var peerAddr string
	for _, n := range nodes {
		if n.NodeDID == did {
			peerAddr = n.Address
			break
		}
	}
	if peerAddr == "" {
		http.Error(w, "peer not found or offline", http.StatusNotFound)
		return
	}

	peerURL := "http://" + peerAddr + "/api/status"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, peerURL, nil)
	if err != nil {
		http.Error(w, "failed to create proxy request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("X-Forwarded-For", r.RemoteAddr)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "peer unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)

	var body interface{}
	if decErr := json.NewDecoder(resp.Body).Decode(&body); decErr == nil {
		json.NewEncoder(w).Encode(body)
	}
}
