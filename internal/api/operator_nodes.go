package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// operatorNodeRequest is the JSON body for POST /operators/nodes. It names a
// machine and its key, and nothing else: an unknown field (a hostname, a
// country, an email) is rejected, so no person can be attached this way.
type operatorNodeRequest struct {
	NodeID    string `json:"node_id"`    // a UUID chosen by the operator
	PublicKey string `json:"public_key"` // standard base64 of the node's 32-byte ed25519 key
	Algo      string `json:"algo"`       // optional; only "ed25519" in v0
}

// handleRegisterOperatorNode lets a verified operator register one of its
// prosumers' nodes with no person attached (Cloudy decisions 0001 and 0002),
// enrolling the node's protocol key in the same transaction. Mounted behind
// OperatorAuth only; it also refuses on its own if no verified operator is in
// the context, so a mis-wiring fails closed rather than open.
//
// 201: node created. 200: the same operator re-registered the same node and
// key (idempotent). 409: the node belongs to someone else, or is enrolled
// with a different key — nothing changes.
func handleRegisterOperatorNode(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op, ok := OperatorFromContext(r.Context())
		if !ok || op.OperatorID == "" {
			writeError(w, http.StatusUnauthorized, "operator transmission required")
			return
		}

		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		var req operatorNodeRequest
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: only node_id, public_key and algo are accepted")
			return
		}
		id, err := uuid.Parse(req.NodeID)
		if err != nil || id.String() != req.NodeID {
			writeError(w, http.StatusBadRequest, "node_id must be a lowercase canonical UUID")
			return
		}
		if req.Algo != "" && req.Algo != "ed25519" {
			writeError(w, http.StatusBadRequest, "unsupported algo (v0 permits exactly ed25519)")
			return
		}
		key, err := base64.StdEncoding.DecodeString(req.PublicKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			writeError(w, http.StatusBadRequest, "public_key must be standard base64 of a 32-byte ed25519 public key")
			return
		}

		created, err := store.RegisterOperatorNode(r.Context(), db, op.OperatorID, req.NodeID, ed25519.PublicKey(key))
		switch {
		case errors.Is(err, store.ErrNodeOwnedElsewhere):
			writeError(w, http.StatusConflict, "node_id is already registered by someone else")
			return
		case errors.Is(err, store.ErrNodeKeyConflict):
			writeError(w, http.StatusConflict, "node is already enrolled with a different key; rotation is an operator action, not an overwrite")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, map[string]string{"node_id": req.NodeID})
	}
}
