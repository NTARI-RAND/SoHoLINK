package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

// jobSubmitter is the subset of *orchestrator.Orchestrator the internal API
// uses. Defined as an interface so handler tests can inject a stub.
// Intentionally duplicates the same-named interface in internal/portal —
// both packages need it independently; see Chat audit note.
type jobSubmitter interface {
	SubmitJob(ctx context.Context, req orchestrator.SubmitJobRequest) (orchestrator.SubmitJobResponse, error)
}

// handleInternalSubmitJob decodes a SubmitJobRequest from the request body,
// invokes orch.SubmitJob, and returns the SubmitJobResponse as JSON.
// Relies on writeError from internal/api/server.go (same package, no import).
// Decode failures return 400; SubmitJob errors return 500. Error-class
// discrimination (validation vs no-nodes vs internal) is a future refinement.
func handleInternalSubmitJob(orch jobSubmitter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req orchestrator.SubmitJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "decode submit job request: "+err.Error())
			return
		}

		resp, err := orch.SubmitJob(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
