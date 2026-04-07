package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
)

// handleTPMAttest handles POST /api/compliance/tpm-attest/{did}
//
// Triggers a TPM PCR quote + compliance check for the node identified by {did}.
// A fresh 32-byte nonce is generated server-side for anti-replay.
// Returns the full TPMAttestation struct as JSON.
func (s *Server) handleTPMAttest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeDID := strings.TrimPrefix(r.URL.Path, "/api/compliance/tpm-attest/")
	nodeDID = strings.TrimSuffix(nodeDID, "/")
	if nodeDID == "" {
		http.Error(w, "missing node DID", http.StatusBadRequest)
		return
	}

	if s.complianceMgr == nil {
		http.Error(w, "compliance manager not configured", http.StatusServiceUnavailable)
		return
	}
	if s.tpmAttester == nil {
		http.Error(w, "TPM attester not configured", http.StatusServiceUnavailable)
		return
	}

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "failed to generate nonce", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	sig, level, details, passed, ta, err := s.complianceMgr.CheckAndAttestTPMRaw(ctx, nodeDID, s.tpmAttester, nonce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"node_did":    nodeDID,
		"attestation": sig,
		"level":       level,
		"details":     details,
		"passed":      passed,
		"tpm":         ta,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleTPMVerify handles GET /api/compliance/tpm-verify/{did}
//
// Retrieves and reports the latest stored TPM attestation for {did}.
// Returns whether the node has a valid TPM attestation and its compliance level.
func (s *Server) handleTPMVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeDID := strings.TrimPrefix(r.URL.Path, "/api/compliance/tpm-verify/")
	nodeDID = strings.TrimSuffix(nodeDID, "/")
	if nodeDID == "" {
		http.Error(w, "missing node DID", http.StatusBadRequest)
		return
	}

	if s.complianceMgr == nil {
		http.Error(w, "compliance manager not configured", http.StatusServiceUnavailable)
		return
	}

	tpmAvailable := s.tpmAttester != nil && s.tpmAttester.Available()

	latest, err := s.store.GetLatestTPMAttestation(r.Context(), nodeDID)
	if err != nil {
		// No attestation on record yet — return a safe zero state
		resp := map[string]interface{}{
			"node_did":      nodeDID,
			"verified":      false,
			"tpm_available": tpmAvailable,
			"level":         "baseline",
			"last_attested": 0,
			"message":       "no TPM attestation on record — run POST /api/compliance/tpm-attest/" + nodeDID,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := map[string]interface{}{
		"node_did":      nodeDID,
		"verified":      latest.Verified,
		"tpm_available": tpmAvailable,
		"level":         latest.Level,
		"last_attested": latest.AttestedAt,
		"tpm_present":   latest.TPMPCRQuote != "",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
