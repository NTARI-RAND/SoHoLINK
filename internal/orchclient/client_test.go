package orchclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

func TestSubmitJob_HappyPath(t *testing.T) {
	want := orchestrator.SubmitJobResponse{JobID: "job-xyz", NodeID: "node-42"}
	var gotReq orchestrator.SubmitJobRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: want POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/jobs/submit" {
			t.Errorf("path: want /internal/jobs/submit, got %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: want application/json, got %s", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want) //nolint:errcheck
	}))
	defer srv.Close()
	client := New(srv.URL)
	req := orchestrator.SubmitJobRequest{ConsumerID: "participant-1", WorkloadType: types.MarketplaceAppHosting, CPUCores: 2, RAMMB: 1024}
	got, err := client.SubmitJob(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID: want %q, got %q", want.JobID, got.JobID)
	}
	if got.NodeID != want.NodeID {
		t.Errorf("NodeID: want %q, got %q", want.NodeID, got.NodeID)
	}
	if gotReq.ConsumerID != req.ConsumerID {
		t.Errorf("ConsumerID forwarded: want %q, got %q", req.ConsumerID, gotReq.ConsumerID)
	}
	if gotReq.WorkloadType != req.WorkloadType {
		t.Errorf("WorkloadType forwarded: want %q, got %q", req.WorkloadType, gotReq.WorkloadType)
	}
}

func TestSubmitJob_ServerReturns500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"no available nodes match request"}`)) //nolint:errcheck
	}))
	defer srv.Close()
	client := New(srv.URL)
	_, err := client.SubmitJob(context.Background(), orchestrator.SubmitJobRequest{ConsumerID: "participant-1", WorkloadType: types.MarketplaceAppHosting})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected %q in error, got: %v", "status 500", err)
	}
	if !strings.Contains(err.Error(), "no available nodes match request") {
		t.Errorf("expected body text in error, got: %v", err)
	}
}

func TestSubmitJob_ServerReturns400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"decode submit job request: invalid character"}`)) //nolint:errcheck
	}))
	defer srv.Close()
	client := New(srv.URL)
	_, err := client.SubmitJob(context.Background(), orchestrator.SubmitJobRequest{ConsumerID: "participant-1", WorkloadType: types.MarketplaceAppHosting})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("expected %q in error, got: %v", "status 400", err)
	}
}

func TestSubmitJob_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	url := srv.URL
	srv.Close()
	client := New(url)
	_, err := client.SubmitJob(context.Background(), orchestrator.SubmitJobRequest{ConsumerID: "participant-1", WorkloadType: types.MarketplaceAppHosting})
	if err == nil {
		t.Fatal("expected error on closed server, got nil")
	}
}

func TestSubmitJob_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := New(srv.URL)
	_, err := client.SubmitJob(ctx, orchestrator.SubmitJobRequest{ConsumerID: "participant-1", WorkloadType: types.MarketplaceAppHosting})
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}
