package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

// stubSubmitter implements jobSubmitter for handler tests.
type stubSubmitter struct {
	resp   orchestrator.SubmitJobResponse
	err    error
	gotReq orchestrator.SubmitJobRequest
}

func (s *stubSubmitter) SubmitJob(_ context.Context, req orchestrator.SubmitJobRequest) (orchestrator.SubmitJobResponse, error) {
	s.gotReq = req
	return s.resp, s.err
}

func TestHandleInternalSubmitJob_HappyPath(t *testing.T) {
	stub := &stubSubmitter{
		resp: orchestrator.SubmitJobResponse{JobID: "job-abc", NodeID: "node-1"},
	}
	handler := handleInternalSubmitJob(stub)

	req := orchestrator.SubmitJobRequest{
		ConsumerID:   "participant-1",
		WorkloadType: types.MarketplaceAppHosting,
		CPUCores:     1,
		RAMMB:        512,
	}
	w := postJSON(t, handler, "/internal/jobs/submit", req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var got orchestrator.SubmitJobResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.JobID != "job-abc" {
		t.Errorf("JobID: want %q, got %q", "job-abc", got.JobID)
	}
	if stub.gotReq.ConsumerID != req.ConsumerID {
		t.Errorf("ConsumerID forwarded: want %q, got %q", req.ConsumerID, stub.gotReq.ConsumerID)
	}
	if stub.gotReq.WorkloadType != req.WorkloadType {
		t.Errorf("WorkloadType forwarded: want %q, got %q", req.WorkloadType, stub.gotReq.WorkloadType)
	}
}

func TestHandleInternalSubmitJob_MalformedBodyReturns400(t *testing.T) {
	stub := &stubSubmitter{}
	handler := handleInternalSubmitJob(stub)

	r := httptest.NewRequest(http.MethodPost, "/internal/jobs/submit", bytes.NewBufferString("not json"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "decode submit job request:") {
		t.Errorf("expected %q in body, got: %s", "decode submit job request:", w.Body.String())
	}
}

func TestHandleInternalSubmitJob_SubmitJobErrorReturns500(t *testing.T) {
	stub := &stubSubmitter{
		err: errors.New("no available nodes match request"),
	}
	handler := handleInternalSubmitJob(stub)

	req := orchestrator.SubmitJobRequest{
		ConsumerID:   "participant-1",
		WorkloadType: types.MarketplaceAppHosting,
	}
	w := postJSON(t, handler, "/internal/jobs/submit", req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no available nodes match request") {
		t.Errorf("expected error string in body, got: %s", w.Body.String())
	}
}
