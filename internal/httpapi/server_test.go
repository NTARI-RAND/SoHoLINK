package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/lbtas"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestration"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// ---------------------------------------------------------------------------
// Health & Existing Endpoint Tests
// ---------------------------------------------------------------------------

// TestServer_HandleHealth tests the health check endpoint.
func TestServer_HandleHealth(t *testing.T) {
	s, err := store.NewMemoryStore()
	if err != nil {
		t.Fatalf("Failed to create memory store: %v", err)
	}
	defer s.Close()

	lm := lbtas.NewManager(s)
	server := NewServer(s, lm, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var response struct {
		Status string                       `json:"status"`
		Time   string                       `json:"time"`
		Uptime string                       `json:"uptime"`
		Checks map[string]json.RawMessage   `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("Status = %q, want %q", response.Status, "healthy")
	}
	if response.Time == "" {
		t.Error("Expected non-empty time field")
	}
	if response.Checks == nil {
		t.Error("Expected checks map in health response")
	}
}

// ---------------------------------------------------------------------------
// Phase 2: Revenue Federation Endpoint Tests
// ---------------------------------------------------------------------------

// TestServer_HandleFederationRevenue tests the federation revenue endpoint.
func TestServer_HandleFederationRevenue(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		setupFunc  func(*store.Store)
		wantCode   int
		wantFields []string
	}{
		{
			name:   "successful revenue retrieval",
			method: http.MethodGet,
			setupFunc: func(s *store.Store) {
				// Create sample revenue records
				ctx := context.Background()
				for i := 0; i < 5; i++ {
					row := &store.RevenueRow{
						RevenueID:      "rev_" + string(rune(i)),
						TransactionID:  "tx_" + string(rune(i)),
						TenantID:       "tenant_123",
						TotalAmount:    int64(1000 * (i + 1)),
						CentralFee:     int64(100 * (i + 1)),
						ProducerPayout: int64(900 * (i + 1)),
						ProcessorFee:   int64(10 * (i + 1)),
						Currency:       "USD",
						CreatedAt:      time.Now(),
					}
					if err := s.RecordCentralRevenue(ctx, row); err != nil {
						t.Fatalf("Failed to create revenue record: %v", err)
					}
				}
			},
			wantCode: http.StatusOK,
			wantFields: []string{
				"total_revenue",
				"pending_payout",
				"revenue_today",
				"recent_revenue",
				"active_rentals",
			},
		},
		{
			name:       "empty revenue data",
			method:     http.MethodGet,
			setupFunc:  func(s *store.Store) {}, // No data
			wantCode:   http.StatusOK,
			wantFields: []string{"total_revenue", "pending_payout"},
		},
		{
			name:      "method not allowed",
			method:    http.MethodPost,
			setupFunc: func(s *store.Store) {},
			wantCode:  http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := store.NewMemoryStore()
			if err != nil {
				t.Fatalf("Failed to create memory store: %v", err)
			}
			defer s.Close()

			tt.setupFunc(s)

			lm := lbtas.NewManager(s)
			server := NewServer(s, lm, ":8080")

			req := httptest.NewRequest(tt.method, "/api/revenue/federation", nil)
			w := httptest.NewRecorder()

			server.handleFederationRevenue(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Status = %d, want %d", w.Code, tt.wantCode)
			}

			if tt.wantCode == http.StatusOK {
				var response FederationRevenueResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				// Verify expected fields are present
				bodyStr := w.Body.String()
				for _, field := range tt.wantFields {
					if !strings.Contains(bodyStr, field) {
						t.Errorf("Response missing field: %s", field)
					}
				}

				// Verify data types
				if response.TotalRevenue < 0 {
					t.Error("TotalRevenue should not be negative")
				}
				if response.PendingPayout < 0 {
					t.Error("PendingPayout should not be negative")
				}
				if response.RecentRevenue == nil {
					t.Error("RecentRevenue should not be nil")
				}
				if response.ActiveRentals == nil {
					t.Error("ActiveRentals should not be nil")
				}
			}
		})
	}
}

// TestServer_HandleFederationRevenue_RevenueCalculations tests revenue math.
func TestServer_HandleFederationRevenue_RevenueCalculations(t *testing.T) {
	s, err := store.NewMemoryStore()
	if err != nil {
		t.Fatalf("Failed to create memory store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create revenue records with known amounts
	totalExpected := int64(0)
	for i := 0; i < 3; i++ {
		amount := int64(1000 * (i + 1))
		totalExpected += amount

		row := &store.RevenueRow{
			RevenueID:      "rev_" + string(rune(i)),
			TransactionID:  "tx_" + string(rune(i)),
			TenantID:       "tenant_123",
			TotalAmount:    amount,
			CentralFee:     amount / 10,
			ProducerPayout: amount * 9 / 10,
			ProcessorFee:   0,
			Currency:       "USD",
			CreatedAt:      time.Now(),
		}
		if err := s.RecordCentralRevenue(ctx, row); err != nil {
			t.Fatalf("Failed to create revenue record: %v", err)
		}
	}

	lm := lbtas.NewManager(s)
	server := NewServer(s, lm, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/revenue/federation", nil)
	w := httptest.NewRecorder()

	server.handleFederationRevenue(w, req)

	var response FederationRevenueResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TotalRevenue != totalExpected {
		t.Errorf("TotalRevenue = %d, want %d", response.TotalRevenue, totalExpected)
	}

	if len(response.RecentRevenue) != 3 {
		t.Errorf("RecentRevenue count = %d, want 3", len(response.RecentRevenue))
	}
}

// ---------------------------------------------------------------------------
// Phase 2: Workload Orchestration Endpoint Tests
// ---------------------------------------------------------------------------

// TestServer_HandleSubmitWorkload tests workload submission.
func TestServer_HandleSubmitWorkload(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		body         interface{}
		withScheduler bool
		wantCode     int
		wantStatus   string
	}{
		{
			name:   "successful workload submission",
			method: http.MethodPost,
			body: SubmitWorkloadRequest{
				WorkloadID: "workload_123",
				Replicas:   3,
				Spec: orchestration.WorkloadSpec{
					CPUCores:    2,
					MemoryMB:    4096,
					DiskGB:      50,
					GPURequired: false,
				},
			},
			withScheduler: true,
			wantCode:      http.StatusAccepted,
			wantStatus:    "pending",
		},
		{
			name:   "missing workload ID",
			method: http.MethodPost,
			body: SubmitWorkloadRequest{
				WorkloadID: "",
				Replicas:   3,
			},
			withScheduler: true,
			wantCode:      http.StatusBadRequest,
		},
		{
			name:   "zero replicas",
			method: http.MethodPost,
			body: SubmitWorkloadRequest{
				WorkloadID: "workload_123",
				Replicas:   0,
			},
			withScheduler: true,
			wantCode:      http.StatusBadRequest,
		},
		{
			name:   "negative replicas",
			method: http.MethodPost,
			body: SubmitWorkloadRequest{
				WorkloadID: "workload_123",
				Replicas:   -1,
			},
			withScheduler: true,
			wantCode:      http.StatusBadRequest,
		},
		{
			name:   "no scheduler configured",
			method: http.MethodPost,
			body: SubmitWorkloadRequest{
				WorkloadID: "workload_123",
				Replicas:   3,
			},
			withScheduler: false,
			wantCode:      http.StatusServiceUnavailable,
		},
		{
			name:         "method not allowed",
			method:       http.MethodGet,
			body:         nil,
			withScheduler: true,
			wantCode:     http.StatusMethodNotAllowed,
		},
		{
			name:         "invalid JSON",
			method:       http.MethodPost,
			body:         "{invalid json",
			withScheduler: true,
			wantCode:     http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := store.NewMemoryStore()
			if err != nil {
				t.Fatalf("Failed to create memory store: %v", err)
			}
			defer s.Close()

			lm := lbtas.NewManager(s)
			server := NewServer(s, lm, ":8080")

			if tt.withScheduler {
				scheduler := orchestration.NewFedScheduler(s)
				server.SetScheduler(scheduler)
			}

			var bodyReader *bytes.Buffer
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					bodyReader = bytes.NewBufferString(str)
				} else {
					bodyBytes, _ := json.Marshal(tt.body)
					bodyReader = bytes.NewBuffer(bodyBytes)
				}
			} else {
				bodyReader = bytes.NewBuffer([]byte{})
			}

			req := httptest.NewRequest(tt.method, "/api/workloads/submit", bodyReader)
			w := httptest.NewRecorder()

			server.handleSubmitWorkload(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Status = %d, want %d", w.Code, tt.wantCode)
			}

			if tt.wantCode == http.StatusAccepted {
				var response SubmitWorkloadResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.Status != tt.wantStatus {
					t.Errorf("Status = %q, want %q", response.Status, tt.wantStatus)
				}
				if response.WorkloadID == "" {
					t.Error("Expected non-empty workload_id")
				}
			}
		})
	}
}

// TestServer_HandleWorkloadStatus tests workload status retrieval.
func TestServer_HandleWorkloadStatus(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		setupFunc    func(*orchestration.FedScheduler)
		withScheduler bool
		wantCode     int
	}{
		{
			name:   "successful status retrieval",
			method: http.MethodGet,
			path:   "/api/workloads/workload_123/status",
			setupFunc: func(sched *orchestration.FedScheduler) {
				// Manually add workload state
				sched.ActiveWorkloads["workload_123"] = &orchestration.WorkloadState{
					WorkloadID:      "workload_123",
					Status:          "running",
					DesiredReplicas: 3,
					Placements:      []orchestration.Placement{},
					CreatedAt:       time.Now(),
					UpdatedAt:       time.Now(),
				}
			},
			withScheduler: true,
			wantCode:      http.StatusOK,
		},
		{
			name:   "workload path without /status suffix",
			method: http.MethodGet,
			path:   "/api/workloads/workload_123",
			setupFunc: func(sched *orchestration.FedScheduler) {
				sched.ActiveWorkloads["workload_123"] = &orchestration.WorkloadState{
					WorkloadID: "workload_123",
					Status:     "running",
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}
			},
			withScheduler: true,
			wantCode:      http.StatusOK,
		},
		{
			name:         "workload not found",
			method:       http.MethodGet,
			path:         "/api/workloads/nonexistent/status",
			setupFunc:    func(sched *orchestration.FedScheduler) {},
			withScheduler: true,
			wantCode:     http.StatusNotFound,
		},
		{
			name:         "no scheduler configured",
			method:       http.MethodGet,
			path:         "/api/workloads/workload_123/status",
			setupFunc:    nil,
			withScheduler: false,
			wantCode:     http.StatusServiceUnavailable,
		},
		{
			name:         "method not allowed",
			method:       http.MethodPost,
			path:         "/api/workloads/workload_123/status",
			setupFunc:    func(sched *orchestration.FedScheduler) {},
			withScheduler: true,
			wantCode:     http.StatusMethodNotAllowed,
		},
		{
			name:         "missing workload ID",
			method:       http.MethodGet,
			path:         "/api/workloads//status",
			setupFunc:    func(sched *orchestration.FedScheduler) {},
			withScheduler: true,
			wantCode:     http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := store.NewMemoryStore()
			if err != nil {
				t.Fatalf("Failed to create memory store: %v", err)
			}
			defer s.Close()

			lm := lbtas.NewManager(s)
			server := NewServer(s, lm, ":8080")

			var scheduler *orchestration.FedScheduler
			if tt.withScheduler {
				scheduler = orchestration.NewFedScheduler(s)
				server.SetScheduler(scheduler)
				if tt.setupFunc != nil {
					tt.setupFunc(scheduler)
				}
			}

			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			server.handleWorkloadStatus(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Status = %d, want %d", w.Code, tt.wantCode)
			}

			if tt.wantCode == http.StatusOK {
				var response WorkloadStatusResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.WorkloadID == "" {
					t.Error("Expected non-empty workload_id")
				}
				if response.Status == "" {
					t.Error("Expected non-empty status")
				}
				if response.Placements == nil {
					t.Error("Placements should not be nil")
				}
			}
		})
	}
}

// TestServer_SetScheduler tests scheduler injection.
func TestServer_SetScheduler(t *testing.T) {
	s, err := store.NewMemoryStore()
	if err != nil {
		t.Fatalf("Failed to create memory store: %v", err)
	}
	defer s.Close()

	lm := lbtas.NewManager(s)
	server := NewServer(s, lm, ":8080")

	if server.scheduler != nil {
		t.Error("Expected nil scheduler initially")
	}

	scheduler := orchestration.NewFedScheduler(s)
	server.SetScheduler(scheduler)

	if server.scheduler == nil {
		t.Error("Expected non-nil scheduler after SetScheduler")
	}
}

// TestServer_WorkloadEndToEnd tests full workload submission and status flow.
func TestServer_WorkloadEndToEnd(t *testing.T) {
	s, err := store.NewMemoryStore()
	if err != nil {
		t.Fatalf("Failed to create memory store: %v", err)
	}
	defer s.Close()

	lm := lbtas.NewManager(s)
	server := NewServer(s, lm, ":8080")
	scheduler := orchestration.NewFedScheduler(s)
	server.SetScheduler(scheduler)

	// Step 1: Submit workload
	submitReq := SubmitWorkloadRequest{
		WorkloadID: "e2e_workload",
		Replicas:   2,
		Spec: orchestration.WorkloadSpec{
			CPUCores: 4,
			MemoryMB: 8192,
			DiskGB:   100,
		},
	}
	submitBody, _ := json.Marshal(submitReq)

	submitHTTP := httptest.NewRequest(http.MethodPost, "/api/workloads/submit", bytes.NewBuffer(submitBody))
	submitW := httptest.NewRecorder()

	server.handleSubmitWorkload(submitW, submitHTTP)

	if submitW.Code != http.StatusAccepted {
		t.Fatalf("Submit status = %d, want %d", submitW.Code, http.StatusAccepted)
	}

	// Step 2: Manually set workload state (simulating scheduler processing)
	scheduler.ActiveWorkloads["e2e_workload"] = &orchestration.WorkloadState{
		WorkloadID:      "e2e_workload",
		Status:          "running",
		DesiredReplicas: 2,
		Placements:      []orchestration.Placement{},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Step 3: Query workload status
	statusHTTP := httptest.NewRequest(http.MethodGet, "/api/workloads/e2e_workload/status", nil)
	statusW := httptest.NewRecorder()

	server.handleWorkloadStatus(statusW, statusHTTP)

	if statusW.Code != http.StatusOK {
		t.Fatalf("Status query status = %d, want %d", statusW.Code, http.StatusOK)
	}

	var statusResponse WorkloadStatusResponse
	if err := json.Unmarshal(statusW.Body.Bytes(), &statusResponse); err != nil {
		t.Fatalf("Failed to parse status response: %v", err)
	}

	if statusResponse.WorkloadID != "e2e_workload" {
		t.Errorf("WorkloadID = %q, want %q", statusResponse.WorkloadID, "e2e_workload")
	}
	if statusResponse.Status != "running" {
		t.Errorf("Status = %q, want %q", statusResponse.Status, "running")
	}
	if statusResponse.Replicas != 2 {
		t.Errorf("Replicas = %d, want 2", statusResponse.Replicas)
	}
}
