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

	"golang.org/x/net/netutil"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/governance"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/lbtas"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/moderation"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestration"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/p2p"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/services"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// maxConnections caps the number of concurrent TCP connections accepted
	// by the API server.  Connections beyond this limit are held in the OS
	// accept queue until a slot frees up — preventing goroutine exhaustion
	// under connection-flood DoS attacks (T-003).
	maxConnections = 1000

	// maxWorkloadQueue limits the number of workload submissions being
	// processed concurrently.  Requests beyond the limit receive 503 so
	// clients can back off and retry rather than pile up indefinitely (T-003).
	maxWorkloadQueue = 50
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
	p2pMesh        *p2p.Mesh           // small-world LAN discovery mesh (optional)
	rateLimiter    *ipRateLimiter      // S2: per-IP rate limiter for mobile endpoints
	coordMeta      *coordinatorMeta    // non-nil when this node acts as a federation coordinator
	paymentLedger  *payment.Ledger     // optional, enables real payout dispatch
	catalog        *services.Catalog   // optional, enables marketplace service endpoints
	reputationMgr  interface{}         // *reputation.ReputationManager, optional for reputation scoring
	// Content safety (Items 1-3)
	hashChecker    *moderation.CSAMHashChecker // optional — nil = no hash blocking
	blocklist      *moderation.DIDBlocklist    // optional — nil = no DID blocking
	safetyPolicy   *moderation.SafetyPolicy    // optional — nil = no OPA policy check
	listenAddr     string
	server         *http.Server
	workloadSem    chan struct{}        // S1-3: limits concurrent workload submissions (T-003)
	// Network security settings (set via Set* methods before Start).
	tlsCertFile         string   // path to PEM TLS certificate
	tlsKeyFile          string   // path to PEM TLS private key
	allowedOrigins      []string // CORS allowed origins; nil/empty = wildcard
	stripeWebhookSecret string   // Stripe webhook signing secret for signature verification
	// Build-time version metadata (set via SetVersionInfo before Start).
	version   string
	commit    string
	buildTime string
}

// NewServer creates a new HTTP API server.
func NewServer(s *store.Store, lm *lbtas.Manager, listenAddr string) *Server {
	return &Server{
		store:       s,
		lbtas:       lm,
		listenAddr:  listenAddr,
		rateLimiter: newIPRateLimiter(),
		workloadSem: make(chan struct{}, maxWorkloadQueue),
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

// SetP2PMesh sets the small-world LAN discovery mesh (optional).
// Call this before Start so that /api/peers is served with live data.
func (s *Server) SetP2PMesh(m *p2p.Mesh) {
	s.p2pMesh = m
}

// SetPaymentLedger attaches the payment ledger for real payout dispatch.
// When set, POST /api/revenue/request-payout delegates to the ledger
// instead of returning a placeholder response.
func (s *Server) SetPaymentLedger(l *payment.Ledger) {
	s.paymentLedger = l
}

// SetServiceCatalog attaches the managed services catalog.
// When set, marketplace service endpoints become available.
func (s *Server) SetServiceCatalog(c *services.Catalog) {
	s.catalog = c
}

// SetHashChecker attaches the CSAM content hash checker.
// When set, storage upload endpoints check content SHA-256 against the blocklist.
func (s *Server) SetHashChecker(hc *moderation.CSAMHashChecker) {
	s.hashChecker = hc
}

// SetDIDBlocklist attaches the DID blocklist.
// When set, all authenticated API calls check the requester DID against the list.
func (s *Server) SetDIDBlocklist(bl *moderation.DIDBlocklist) {
	s.blocklist = bl
}

// SetSafetyPolicy attaches the OPA safety policy evaluator.
// When set, workload purchase requests are evaluated against safety prohibition rules.
func (s *Server) SetSafetyPolicy(sp *moderation.SafetyPolicy) {
	s.safetyPolicy = sp
}

// SetTLSConfig sets the TLS certificate and key paths.
// When both are non-empty, Start() uses ListenAndServeTLS instead of ListenAndServe.
func (s *Server) SetTLSConfig(certFile, keyFile string) {
	s.tlsCertFile = certFile
	s.tlsKeyFile = keyFile
}

// SetAllowedOrigins sets the list of CORS-allowed origins.
// Use ["*"] for open access (default when empty); restrict to specific domains in production.
func (s *Server) SetAllowedOrigins(origins []string) {
	s.allowedOrigins = origins
}

// SetStripeWebhookSecret sets the Stripe webhook signing secret used to verify
// the Stripe-Signature header on incoming webhook events.
func (s *Server) SetStripeWebhookSecret(secret string) {
	s.stripeWebhookSecret = secret
}

// limitBodySize is a middleware that caps request bodies at 4 MB, protecting
// all handlers from large-payload denial-of-service attacks.
func limitBodySize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<20) // 4 MB cap
		next.ServeHTTP(w, r)
	})
}

// Start begins listening for HTTP API requests.
func (s *Server) Start(ctx context.Context) error {
	// Ensure the owner Ed25519 keypair exists; log the private key once if new.
	s.ensureOwnerKeypairLogged(ctx)

	mux := http.NewServeMux()

	// ── Public (no auth required) ──────────────────────────────────────────
	// Rate-limit challenge to 10 req/min per IP to mitigate brute-force.
	mux.HandleFunc("/api/auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		if !s.rateLimiter.Allow(r, 10) {
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}
		s.handleAuthChallenge(w, r)
	})
	mux.HandleFunc("/api/auth/connect", s.handleAuthConnect)

	// Stripe webhook — public (verified by Stripe-Signature, not device token)
	mux.HandleFunc("/api/webhooks/stripe", s.handleStripeWebhook)

	// Health, version, and discovery
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/resources/discover", s.handleDiscoverResources)
	mux.HandleFunc("/api/status", s.handleStatus)

	// LBTAS reputation
	mux.HandleFunc("/api/lbtas/score/", s.handleGetScore)
	mux.HandleFunc("/api/lbtas/rate-provider", s.handleRateProvider)
	mux.HandleFunc("/api/lbtas/rate-user", s.handleRateUser)

	// Mobile-app unified revenue endpoint: GET /api/revenue
	// Must be registered before the sub-path routes so ServeMux exact-matches it.
	mux.HandleFunc("/api/revenue", s.handleMobileRevenue)

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
	mux.HandleFunc("/api/workloads/validate-placement", s.handleValidatePlacement) // Capability validation
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

	// Topology endpoints (Sprint 3.5 - two-tier federation)
	mux.HandleFunc("/api/topology/cluster/members", s.handleGetClusterMembers)
	mux.HandleFunc("/api/topology/mesh/peers", s.handleGetMeshPeers)
	mux.HandleFunc("/api/topology/routing-table", s.handleGetRoutingTable)

	// Reputation ledger endpoints (Sprint 3.6)
	mux.HandleFunc("/api/reputation/ledger", s.handleGetReputationLedger)
	mux.HandleFunc("/api/reputation/stats", s.handleGetReputationStats)
	mux.HandleFunc("/api/reputation/nodes/", s.routeReputationNode) // Handles /nodes/{node_did}, /nodes/{node_did}/pricing
	mux.HandleFunc("/api/reputation/verify/", s.handleVerifyChain)

	// Governance endpoints (Phase 2)
	mux.HandleFunc("/api/governance/proposals", s.handleGovernanceProposals)
	mux.HandleFunc("/api/governance/proposals/", s.handleGovernanceProposal)
	mux.HandleFunc("/api/governance/vote", s.handleGovernanceVote)

	// Mobile node endpoints (v0.2)
	mux.HandleFunc("/ws/nodes", s.handleMobileWS)
	mux.HandleFunc("/api/v1/nodes/mobile/register", s.handleMobileRegister)
	mux.HandleFunc("/api/v1/nodes/mobile", s.handleListMobileNodes)

	// P2P LAN peer discovery (v0.2.0)
	mux.HandleFunc("/api/peers", s.handlePeers)

	// Federation endpoints (public — provider nodes use Ed25519 signatures, not device tokens)
	mux.HandleFunc("/api/federation/info", s.handleFederationInfo)
	mux.HandleFunc("/api/federation/peers", s.handleFederationPeers)
	mux.HandleFunc("/api/federation/announce", s.handleFederationAnnounce)
	mux.HandleFunc("/api/federation/heartbeat", s.handleFederationHeartbeat)
	mux.HandleFunc("/api/federation/deregister", s.handleFederationDeregister)

	// Marketplace endpoints (buyer-side: browse, estimate, purchase, orders)
	mux.HandleFunc("/api/marketplace/nodes", s.handleMarketplaceNodes)
	mux.HandleFunc("/api/marketplace/estimate", s.handleMarketplaceEstimate)
	mux.HandleFunc("/api/marketplace/services", s.handleMarketplaceServices)
	mux.HandleFunc("/api/marketplace/purchase", s.handleMarketplacePurchase)
	mux.HandleFunc("/api/marketplace/purchase-service", s.handleMarketplacePurchaseService)
	mux.HandleFunc("/api/orders", s.handleListOrders)
	mux.HandleFunc("/api/orders/", s.handleOrderByID) // GET /{id} and POST /{id}/cancel

	// Wallet endpoints (prepaid sats balance, topup, history)
	mux.HandleFunc("/api/wallet/balance", s.handleWalletBalance)
	mux.HandleFunc("/api/wallet/topup", s.handleWalletTopup)
	mux.HandleFunc("/api/wallet/topups", s.handleWalletTopups)
	mux.HandleFunc("/api/wallet/confirm-topup", s.handleConfirmTopup)

	// Prometheus metrics — public, no auth required.
	// GET /metrics returns the default Prometheus registry in text exposition format.
	mux.Handle("/metrics", promhttp.Handler())

	// Content safety admin + federation blocklist pull (Item 1 & 2)
	s.setupAdminRoutes(mux)

	s.server = &http.Server{
		Addr: s.listenAddr,
		// errorScrubMiddleware (outermost) prevents 5xx bodies from leaking
		// internal errors to clients (T-005).
		// metricsMiddleware instruments all requests for Prometheus.
		// limitBodySize caps request bodies at 4 MB (DoS protection).
		// authMiddleware enforces device token authentication.
		Handler:        errorScrubMiddleware(metricsMiddleware(limitBodySize(s.authMiddleware(mux)))),
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB header cap
	}

	go func() { // #nosec G118 -- context.Background() intentional: shutdown must complete regardless of parent context cancellation
		<-ctx.Done()
		s.server.Shutdown(context.Background()) // nolint:errcheck
	}()

	// Create the TCP listener explicitly so we can cap concurrent connections
	// with netutil.LimitListener (T-003: API goroutine exhaustion DoS).
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("API server: listen %s: %w", s.listenAddr, err)
	}
	ln = netutil.LimitListener(ln, maxConnections)

	if s.tlsCertFile != "" && s.tlsKeyFile != "" {
		log.Printf("[httpapi] TLS enabled; API server listening on %s (max %d connections)", s.listenAddr, maxConnections)
		if err := s.server.ServeTLS(ln, s.tlsCertFile, s.tlsKeyFile); err != http.ErrServerClosed {
			return fmt.Errorf("API server TLS error: %w", err)
		}
		return nil
	}
	log.Printf("[httpapi] WARNING: TLS disabled — set tls_cert_file/tls_key_file for production")
	log.Printf("[httpapi] API server listening on %s (max %d connections)", s.listenAddr, maxConnections)
	if err := s.server.Serve(ln); err != http.ErrServerClosed {
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

// handlePeers serves GET /api/peers.
// Returns the list of live LAN-discovered peers from the small-world mesh.
// If the mesh is not configured, returns an empty list (404-free degradation).
func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.p2pMesh == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
			"count": 0,
			"peers": []interface{}{},
		})
		return
	}
	s.p2pMesh.HandlePeers(w, r)
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

	// S1-3: Workload queue depth cap — prevent unbounded goroutine accumulation
	// when many submissions arrive simultaneously (T-003).
	select {
	case s.workloadSem <- struct{}{}:
		defer func() { <-s.workloadSem }()
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"server_busy","message":"workload queue is full — retry after a moment"}`)
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
