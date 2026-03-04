package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/governance"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/lbtas"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestration"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// ---------------------------------------------------------------------------
// Per-IP rate limiter (S2)
// ---------------------------------------------------------------------------

// ipRateLimiter is a simple per-IP sliding-window rate limiter.
// It allows at most maxPerWindow requests per source IP per minute.
// No external dependencies required.
type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rlBucket
}

type rlBucket struct {
	count   int
	resetAt time.Time
}

func newIPRateLimiter() *ipRateLimiter {
	return &ipRateLimiter{buckets: make(map[string]*rlBucket)}
}

// Allow returns true if the source IP of r has not exceeded maxPerWindow
// requests in the current 1-minute window.
func (l *ipRateLimiter) Allow(r *http.Request, maxPerWindow int) bool {
	ip := clientIP(r)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok || now.After(b.resetAt) {
		l.buckets[ip] = &rlBucket{count: 1, resetAt: now.Add(time.Minute)}
		return true
	}
	b.count++
	return b.count <= maxPerWindow
}

// clientIP returns the originating IP for a request, respecting
// X-Forwarded-For when set (e.g. behind a trusted reverse proxy).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Use only the first (leftmost) address — closest to the client.
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			xff = xff[:idx]
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Server provides the HTTP API for resource sharing operations.
type Server struct {
	store          *store.Store
	lbtas          *lbtas.Manager
	scheduler      *orchestration.FedScheduler
	governance     *governance.Manager
	serviceManager ServiceManager
	storageBackend StorageBackend
	mobileHub      *MobileHub
	rateLimiter    *ipRateLimiter // S2: per-IP rate limiter for mobile endpoints
	listenAddr     string
	server         *http.Server
}

// NewServer creates a new HTTP API server.
func NewServer(s *store.Store, lm *lbtas.Manager, listenAddr string) *Server {
	return &Server{
		store:       s,
		lbtas:       lm,
		listenAddr:  listenAddr,
		rateLimiter: newIPRateLimiter(),
	}
}

// SetScheduler sets the orchestration scheduler (optional, for workload API).
func (s *Server) SetScheduler(sched *orchestration.FedScheduler) {
	s.scheduler = sched
}

// SetGovernance sets the governance manager (optional, for governance API).
func (s *Server) SetGovernance(gov *governance.Manager) {
	s.governance = gov
}

// SetMobileHub sets the mobile WebSocket hub (optional, for mobile node API).
func (s *Server) SetMobileHub(hub *MobileHub) {
	s.mobileHub = hub
}

// Start begins listening for HTTP API requests.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Dashboard SPA — serves embedded ui/dashboard/ assets.
	// /          → redirect to /dashboard
	// /dashboard → index.html (hash-router handles tab selection)
	// /dashboard/* → static assets (style.css, app.js, …)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/dashboard", s.handleDashboard)
	mux.HandleFunc("/dashboard/", s.handleDashboard)

	// Health and discovery
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/resources/discover", s.handleDiscoverResources)
	mux.HandleFunc("/api/status", s.handleStatus)

	// LBTAS reputation
	mux.HandleFunc("/api/lbtas/score/", s.handleGetScore)
	mux.HandleFunc("/api/lbtas/rate-provider", s.handleRateProvider)
	mux.HandleFunc("/api/lbtas/rate-user", s.handleRateUser)

	// Revenue endpoints (Phase 4)
	mux.HandleFunc("/api/revenue/balance", s.handleGetBalance)
	mux.HandleFunc("/api/revenue/history", s.handleGetRevenueHistory)
	mux.HandleFunc("/api/revenue/stats", s.handleGetRevenueStats)
	mux.HandleFunc("/api/revenue/active-rentals", s.handleGetActiveRentals)
	mux.HandleFunc("/api/revenue/request-payout", s.handleRequestPayout)
	mux.HandleFunc("/api/revenue/payouts", s.handleGetPayoutHistory)
	mux.HandleFunc("/api/revenue/federation", s.handleFederationRevenue) // Phase 2 compat

	// Workload orchestration endpoints (Phase 2 + Phase 4)
	mux.HandleFunc("/api/workloads/submit", s.handleSubmitWorkload)
	mux.HandleFunc("/api/workloads/", s.routeWorkload) // Phase 4: comprehensive routing
	mux.HandleFunc("/api/workloads", s.routeWorkload)  // Phase 4: list/create

	// Service management endpoints (Phase 4)
	mux.HandleFunc("/api/services/", s.routeService)
	mux.HandleFunc("/api/services", s.routeService)

	// Storage endpoints (Phase 4)
	mux.HandleFunc("/api/storage/buckets/", s.handleDeleteBucket)
	mux.HandleFunc("/api/storage/buckets", s.handleListBuckets)
	mux.HandleFunc("/api/storage/objects/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			s.handlePutObject(w, r)
		case http.MethodGet:
			s.handleGetObject(w, r)
		case http.MethodDelete:
			s.handleDeleteObject(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Governance endpoints (Phase 2)
	mux.HandleFunc("/api/governance/proposals", s.handleGovernanceProposals)
	mux.HandleFunc("/api/governance/proposals/", s.handleGovernanceProposal)
	mux.HandleFunc("/api/governance/vote", s.handleGovernanceVote)

	// Mobile node endpoints (v0.2)
	mux.HandleFunc("/ws/nodes", s.handleMobileWS)
	mux.HandleFunc("/api/v1/nodes/mobile/register", s.handleMobileRegister)
	mux.HandleFunc("/api/v1/nodes/mobile", s.handleListMobileNodes)

	s.server = &http.Server{
		Addr:         s.listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("[httpapi] API server listening on %s", s.listenAddr)
	go func() { // #nosec G118 -- context.Background() intentional: shutdown must complete regardless of parent context cancellation
		<-ctx.Done()
		s.server.Shutdown(context.Background())
	}()

	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("API server error: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleGetScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract DID from path: /api/lbtas/score/{did}
	did := r.URL.Path[len("/api/lbtas/score/"):]
	if did == "" {
		http.Error(w, "Missing DID parameter", http.StatusBadRequest)
		return
	}

	score, err := s.lbtas.GetScore(r.Context(), did)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(score)
}

func (s *Server) handleRateProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req lbtas.UserRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.lbtas.UserRatesProvider(r.Context(), req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleRateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req lbtas.ProviderRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.lbtas.ProviderRatesUser(r.Context(), req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleDiscoverResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Placeholder - resource discovery would query announcement cache
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"resources": []interface{}{},
		"total":     0,
	})
}

// ---------------------------------------------------------------------------
// Phase 2 HTTP API Wrappers: Revenue & Workload Endpoints
// ---------------------------------------------------------------------------

// FederationRevenueResponse is the JSON response for /api/revenue/federation.
type FederationRevenueResponse struct {
	TotalRevenue   int64                  `json:"total_revenue"`
	PendingPayout  int64                  `json:"pending_payout"`
	RevenueToday   int64                  `json:"revenue_today"`
	RecentRevenue  []store.RevenueRow     `json:"recent_revenue"`
	ActiveRentals  []store.ActiveRentalRow `json:"active_rentals"`
}

// handleFederationRevenue returns aggregated revenue stats for the federation.
func (s *Server) handleFederationRevenue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Get total revenue
	totalRevenue, err := s.store.GetTotalRevenue(ctx)
	if err != nil {
		log.Printf("[httpapi] GetTotalRevenue error: %v", err)
		http.Error(w, "Failed to retrieve total revenue", http.StatusInternalServerError)
		return
	}

	// Get pending payout
	pendingPayout, err := s.store.GetPendingPayout(ctx)
	if err != nil {
		log.Printf("[httpapi] GetPendingPayout error: %v", err)
		http.Error(w, "Failed to retrieve pending payout", http.StatusInternalServerError)
		return
	}

	// Get revenue since midnight today (UTC)
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	revenueToday, err := s.store.GetRevenueSince(ctx, todayStart)
	if err != nil {
		log.Printf("[httpapi] GetRevenueSince error: %v", err)
		http.Error(w, "Failed to retrieve today's revenue", http.StatusInternalServerError)
		return
	}

	// Get recent revenue entries (last 10)
	recentRevenue, err := s.store.GetRecentRevenue(ctx, 10)
	if err != nil {
		log.Printf("[httpapi] GetRecentRevenue error: %v", err)
		http.Error(w, "Failed to retrieve recent revenue", http.StatusInternalServerError)
		return
	}

	// Get active rentals
	activeRentals, err := s.store.GetActiveRentals(ctx)
	if err != nil {
		log.Printf("[httpapi] GetActiveRentals error: %v", err)
		http.Error(w, "Failed to retrieve active rentals", http.StatusInternalServerError)
		return
	}

	// Ensure slices are non-nil for JSON serialisation
	if recentRevenue == nil {
		recentRevenue = []store.RevenueRow{}
	}
	if activeRentals == nil {
		activeRentals = []store.ActiveRentalRow{}
	}

	// Build response
	response := FederationRevenueResponse{
		TotalRevenue:  totalRevenue,
		PendingPayout: pendingPayout,
		RevenueToday:  revenueToday,
		RecentRevenue: recentRevenue,
		ActiveRentals: activeRentals,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// SubmitWorkloadRequest is the JSON body for POST /api/workloads/submit.
type SubmitWorkloadRequest struct {
	WorkloadID string                       `json:"workload_id"`
	Replicas   int                          `json:"replicas"`
	Spec       orchestration.WorkloadSpec   `json:"spec"`
	Constraints orchestration.PlacementConstraints   `json:"constraints,omitempty"`
}

// SubmitWorkloadResponse is the JSON response for workload submission.
type SubmitWorkloadResponse struct {
	WorkloadID string `json:"workload_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}

// handleSubmitWorkload submits a new workload to the scheduler.
func (s *Server) handleSubmitWorkload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.scheduler == nil {
		http.Error(w, "Orchestration scheduler not configured", http.StatusServiceUnavailable)
		return
	}

	var req SubmitWorkloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkloadID == "" {
		http.Error(w, "Missing workload_id", http.StatusBadRequest)
		return
	}
	if req.Replicas <= 0 {
		http.Error(w, "replicas must be positive", http.StatusBadRequest)
		return
	}

	// Create workload and submit to scheduler
	workload := &orchestration.Workload{
		WorkloadID:  req.WorkloadID,
		Replicas:    req.Replicas,
		Spec:        req.Spec,
		Constraints: req.Constraints,
	}

	s.scheduler.SubmitWorkload(workload)

	response := SubmitWorkloadResponse{
		WorkloadID: req.WorkloadID,
		Status:     "pending",
		Message:    "Workload submitted for scheduling",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

// WorkloadStatusResponse is the JSON response for GET /api/workloads/{id}/status.
type WorkloadStatusResponse struct {
	WorkloadID string                            `json:"workload_id"`
	Status     string                            `json:"status"`
	Replicas   int                               `json:"replicas"`
	Placements []orchestration.Placement         `json:"placements"`
	CreatedAt  time.Time                         `json:"created_at"`
	UpdatedAt  time.Time                         `json:"updated_at"`
}

// handleWorkloadStatus retrieves the status of a workload.
func (s *Server) handleWorkloadStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.scheduler == nil {
		http.Error(w, "Orchestration scheduler not configured", http.StatusServiceUnavailable)
		return
	}

	// Extract workload ID from path: /api/workloads/{id}/status
	// or /api/workloads/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/workloads/")
	path = strings.TrimSuffix(path, "/status")
	workloadID := path

	if workloadID == "" {
		http.Error(w, "Missing workload ID", http.StatusBadRequest)
		return
	}

	// Get workload state from scheduler
	state := s.scheduler.GetWorkloadState(workloadID)
	if state == nil {
		http.Error(w, "Workload not found", http.StatusNotFound)
		return
	}

	placements := state.Placements
	if placements == nil {
		placements = []orchestration.Placement{}
	}
	response := WorkloadStatusResponse{
		WorkloadID: state.WorkloadID,
		Status:     state.Status,
		Replicas:   state.DesiredReplicas,
		Placements: placements,
		CreatedAt:  state.CreatedAt,
		UpdatedAt:  state.UpdatedAt,
	}
	if state.Workload != nil {
		response.WorkloadID = state.Workload.WorkloadID
		response.Status = state.Workload.Status
		response.Replicas = state.Workload.Replicas
		response.CreatedAt = state.Workload.CreatedAt
		response.UpdatedAt = state.Workload.UpdatedAt
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ---------------------------------------------------------------------------
// Phase 2 HTTP API Wrappers: Governance Endpoints
// ---------------------------------------------------------------------------

// CreateProposalRequest is the JSON body for POST /api/governance/proposals.
type CreateProposalRequest struct {
	ProposerDID  string                   `json:"proposer_did"`
	Title        string                   `json:"title"`
	Description  string                   `json:"description"`
	ProposalType governance.ProposalType  `json:"proposal_type"`
	VotingStart  *time.Time               `json:"voting_start,omitempty"`
	VotingEnd    *time.Time               `json:"voting_end,omitempty"`
	QuorumPct    int                      `json:"quorum_pct,omitempty"`
	PassPct      int                      `json:"pass_pct,omitempty"`
}

// CastVoteRequest is the JSON body for POST /api/governance/vote.
type CastVoteRequest struct {
	ProposalID string              `json:"proposal_id"`
	VoterDID   string              `json:"voter_did"`
	Choice     governance.VoteChoice `json:"choice"`
	Signature  string              `json:"signature"`
}

// handleGovernanceProposals handles listing proposals (GET) and creating proposals (POST).
func (s *Server) handleGovernanceProposals(w http.ResponseWriter, r *http.Request) {
	if s.governance == nil {
		http.Error(w, "Governance system not configured", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleListProposals(w, r)
	case http.MethodPost:
		s.handleCreateProposal(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListProposals lists governance proposals with optional state filter.
func (s *Server) handleListProposals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	state := r.URL.Query().Get("state")
	limit := 20 // Default limit
	offset := 0

	// List proposals
	proposals, err := s.governance.ListProposals(ctx, governance.ProposalState(state), limit, offset)
	if err != nil {
		log.Printf("[httpapi] ListProposals error: %v", err)
		http.Error(w, "Failed to retrieve proposals", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"proposals": proposals,
		"total":     len(proposals),
	})
}

// handleCreateProposal creates a new governance proposal.
func (s *Server) handleCreateProposal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ProposerDID == "" {
		http.Error(w, "Missing proposer_did", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, "Missing title", http.StatusBadRequest)
		return
	}
	if req.Description == "" {
		http.Error(w, "Missing description", http.StatusBadRequest)
		return
	}

	// Create proposal
	proposal := &governance.Proposal{
		ProposerDID:  req.ProposerDID,
		Title:        req.Title,
		Description:  req.Description,
		ProposalType: req.ProposalType,
		QuorumPct:    req.QuorumPct,
		PassPct:      req.PassPct,
	}

	if req.VotingStart != nil {
		proposal.VotingStart = *req.VotingStart
	}
	if req.VotingEnd != nil {
		proposal.VotingEnd = *req.VotingEnd
	}

	if err := s.governance.CreateProposal(ctx, proposal); err != nil {
		log.Printf("[httpapi] CreateProposal error: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create proposal: %v", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(proposal)
}

// handleGovernanceProposal handles single proposal operations (GET /api/governance/proposals/{id}).
func (s *Server) handleGovernanceProposal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.governance == nil {
		http.Error(w, "Governance system not configured", http.StatusServiceUnavailable)
		return
	}

	// Extract proposal ID from path: /api/governance/proposals/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/governance/proposals/")
	proposalID := path

	if proposalID == "" || proposalID == "proposals" {
		http.Error(w, "Missing proposal ID", http.StatusBadRequest)
		return
	}

	// Get proposal
	proposal, err := s.governance.GetProposal(r.Context(), proposalID)
	if err != nil {
		log.Printf("[httpapi] GetProposal error: %v", err)
		http.Error(w, "Failed to retrieve proposal", http.StatusInternalServerError)
		return
	}
	if proposal == nil {
		http.Error(w, "Proposal not found", http.StatusNotFound)
		return
	}

	// Get votes for this proposal
	votes, err := s.governance.GetVotesForProposal(r.Context(), proposalID)
	if err != nil {
		log.Printf("[httpapi] GetVotesForProposal error: %v", err)
		// Continue without votes
		votes = []*governance.Vote{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"proposal": proposal,
		"votes":    votes,
	})
}

// handleGovernanceVote handles casting votes on proposals.
func (s *Server) handleGovernanceVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.governance == nil {
		http.Error(w, "Governance system not configured", http.StatusServiceUnavailable)
		return
	}

	var req CastVoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ProposalID == "" {
		http.Error(w, "Missing proposal_id", http.StatusBadRequest)
		return
	}
	if req.VoterDID == "" {
		http.Error(w, "Missing voter_did", http.StatusBadRequest)
		return
	}
	if req.Choice == "" {
		http.Error(w, "Missing choice", http.StatusBadRequest)
		return
	}
	if req.Signature == "" {
		http.Error(w, "Missing signature", http.StatusBadRequest)
		return
	}

	// Cast vote
	vote := &governance.Vote{
		ProposalID: req.ProposalID,
		VoterDID:   req.VoterDID,
		Choice:     req.Choice,
		Signature:  req.Signature,
	}

	if err := s.governance.CastVote(r.Context(), vote); err != nil {
		log.Printf("[httpapi] CastVote error: %v", err)
		http.Error(w, fmt.Sprintf("Failed to cast vote: %v", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"vote_id": vote.VoteID,
		"message": "Vote cast successfully",
	})
}

// ---------------------------------------------------------------------------
// Mobile node endpoints (v0.2)
// ---------------------------------------------------------------------------

// handleMobileWS upgrades an HTTP connection to a WebSocket connection and
// hands it off to the MobileHub.  Path: GET /ws/nodes
func (s *Server) handleMobileWS(w http.ResponseWriter, r *http.Request) {
	if s.mobileHub == nil {
		http.Error(w, "Mobile hub not configured", http.StatusServiceUnavailable)
		return
	}
	// S2: Limit WebSocket upgrade attempts to 30 per IP per minute to
	// mitigate trivial connection-flood DoS attacks.
	if !s.rateLimiter.Allow(r, 30) {
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}
	s.mobileHub.ServeWS(w, r)
}

// MobileRegisterRequest is the JSON body for POST /api/v1/nodes/mobile/register.
// Mobile nodes may call this REST endpoint before (or instead of) opening a
// WebSocket, to pre-register metadata with the coordinator.
type MobileRegisterRequest struct {
	NodeDID   string                          `json:"node_did"`
	NodeClass orchestration.NodeClass         `json:"node_class"`
	NodeInfo  orchestration.MobileNodeInfo    `json:"node_info"`
}

// MobileRegisterResponse is returned on a successful registration.
type MobileRegisterResponse struct {
	Status     string `json:"status"`
	WSEndpoint string `json:"ws_endpoint"` // where the node should connect
}

// handleMobileRegister handles POST /api/v1/nodes/mobile/register.
func (s *Server) handleMobileRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MobileRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// S2: Limit pre-registration calls to 20 per IP per minute.
	if !s.rateLimiter.Allow(r, 20) {
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}

	if req.NodeDID == "" {
		http.Error(w, "Missing node_did", http.StatusBadRequest)
		return
	}
	if req.NodeClass == "" {
		http.Error(w, "Missing node_class", http.StatusBadRequest)
		return
	}

	// S1: Validate node_class against the known set of constants.
	validClasses := map[orchestration.NodeClass]bool{
		orchestration.NodeClassDesktop:       true,
		orchestration.NodeClassMobileAndroid: true,
		orchestration.NodeClassMobileIOS:     true,
		orchestration.NodeClassAndroidTV:     true,
	}
	if !validClasses[req.NodeClass] {
		http.Error(w, fmt.Sprintf(
			"unknown node_class %q; valid values: desktop, mobile-android, mobile-ios, android-tv",
			req.NodeClass), http.StatusBadRequest)
		return
	}

	log.Printf("[httpapi] mobile node pre-registered: did=%s class=%s", req.NodeDID, req.NodeClass)

	resp := MobileRegisterResponse{
		Status:     "ok",
		WSEndpoint: "/ws/nodes",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// handleListMobileNodes handles GET /api/v1/nodes/mobile.
// Returns the list of currently connected mobile nodes from the hub.
func (s *Server) handleListMobileNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.mobileHub == nil {
		// Hub not configured — return empty list rather than an error so
		// dashboard polling does not break before the hub is wired up.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"nodes": []interface{}{},
			"total": 0,
		})
		return
	}

	nodes := s.mobileHub.ActiveNodes()
	if nodes == nil {
		nodes = []orchestration.MobileNodeInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"total": len(nodes),
	})
}
