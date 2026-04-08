package portal

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// PortalServer is the SoHoLINK marketplace portal HTTP server. It sits behind
// NGINX, which handles TLS termination, so ListenAndServe (not TLS) is used.
type PortalServer struct {
	srv           *http.Server
	db            *store.DB
	sm            *SessionManager
	payment       *payment.Client
	orch          *orchestrator.Orchestrator
	baseURL       string
	templatePaths []string
}

// onboardingData is the template data for provider_onboarding.html.
type onboardingData struct {
	Email              string
	ISPTier            string
	DisclosureAccepted bool
	StripeComplete     bool
}

// ProfileRow is a single resource profile row for provider_provision.html.
type ProfileRow struct {
	ID            string
	Name          string
	IsDefault     bool
	CPUEnabled    bool
	GPUPct        int
	RAMPct        int
	StorageGB     int
	BandwidthMbps int
}

// provisionData is the template data for provider_provision.html.
type provisionData struct {
	Email    string
	NodeID   string
	Profiles []ProfileRow
}

// DisputeRow is a single row in the dispute queue table.
type DisputeRow struct {
	ID                string
	Status            string
	Reason            string
	ConsumerRefundPct int
	CreatedAt         time.Time
	PaymentIntentID   string
	JobID             string
	ConsumerEmail     string
	NodeClass         string
	CountryCode       string
}

// DisputeQueueData is the template data for dispute_queue.html.
type DisputeQueueData struct {
	Email    string
	Disputes []DisputeRow
}

// NodeListing is a single marketplace node entry.
type NodeListing struct {
	ID            string
	CountryCode   string
	CPUCores      int
	RAMGB         int64
	StorageGB     int
	BandwidthMbps int
	EstHrRate     float64
}

// ClassGroup groups NodeListings by node class for the marketplace template.
type ClassGroup struct {
	Class     string
	ClassName string
	Nodes     []NodeListing
	MinRate   float64
	MaxRate   float64
}

// MarketplaceData is the template data for consumer_marketplace.html.
type MarketplaceData struct {
	Email     string
	Classes   []ClassGroup
	CPURateHr float64
	RAMRateHr float64
}

// JobStatusData is the template data for consumer_job_status.html.
type JobStatusData struct {
	JobID     string
	Status    string
	NodeID    string
	CreatedAt time.Time
	Email     string
}

// New constructs a PortalServer. It walks templatesDir recursively to collect
// all .html file paths (not parsed yet — see renderTemplate), registers routes,
// and builds the http.Server.
func New(db *store.DB, addr string, sessionSecret []byte, templatesDir string, paymentClient *payment.Client, baseURL string, orch *orchestrator.Orchestrator) (*PortalServer, error) {
	var paths []string
	if err := filepath.WalkDir(templatesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".html" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("new portal: walk templates: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("new portal: no templates found in %s", templatesDir)
	}

	sm := NewSessionManager(sessionSecret)
	ps := &PortalServer{
		db:            db,
		sm:            sm,
		payment:       paymentClient,
		orch:          orch,
		baseURL:       baseURL,
		templatePaths: paths,
	}

	mux := http.NewServeMux()

	// Public routes.
	mux.HandleFunc("GET /", ps.handleIndex)
	mux.HandleFunc("GET /login", ps.handleLoginPage)
	mux.HandleFunc("POST /login", ps.handleLogin)

	// Protected routes — auth middleware wraps role middleware.
	mux.Handle("GET /provider/dashboard",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderDashboard))))
	mux.Handle("GET /consumer/marketplace",
		RequireAuth(sm, RequireRole("consumer", http.HandlerFunc(ps.handleConsumerMarketplace))))
	mux.Handle("POST /consumer/job",
		RequireAuth(sm, RequireRole("consumer", http.HandlerFunc(ps.handleSubmitJob))))
	mux.Handle("GET /consumer/job/{id}",
		RequireAuth(sm, RequireRole("consumer", http.HandlerFunc(ps.handleJobStatus))))
	mux.Handle("GET /dispute/queue",
		RequireAuth(sm, RequireRole("ntari_staff", http.HandlerFunc(ps.handleDisputeQueue))))
	mux.Handle("POST /dispute/{id}/resolve",
		RequireAuth(sm, RequireRole("ntari_staff", http.HandlerFunc(ps.handleDisputeResolve))))
	mux.Handle("POST /dispute/{id}/review",
		RequireAuth(sm, RequireRole("ntari_staff", http.HandlerFunc(ps.handleDisputeReview))))

	mux.Handle("GET /provider/onboarding",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderOnboardingPage))))
	mux.Handle("POST /provider/onboarding",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderOnboarding))))
	mux.Handle("GET /provider/onboarding/return",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderOnboardingReturn))))
	mux.Handle("GET /provider/provision",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderProvision))))
	mux.Handle("POST /provider/provision/profile",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleAddProfile))))

	ps.srv = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return ps, nil
}

// renderTemplate builds a fresh template set from layout.html and the requested
// page file, then executes the named page template. Parsing per-request avoids
// the shared-set redefinition problem: every page defines {{define "content"}},
// which would collide if all files were parsed into one set at startup.
func (ps *PortalServer) renderTemplate(w http.ResponseWriter, page string, data any) {
	var layoutPath, pagePath string
	for _, p := range ps.templatePaths {
		switch filepath.Base(p) {
		case "layout.html":
			layoutPath = p
		case page:
			pagePath = p
		}
	}
	if layoutPath == "" || pagePath == "" {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.ParseFiles(layoutPath, pagePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, page, data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// Start begins accepting HTTP connections. Blocks until the server is shut down.
func (ps *PortalServer) Start(_ context.Context) error {
	return ps.srv.ListenAndServe()
}

// Shutdown gracefully drains active connections within the context deadline.
func (ps *PortalServer) Shutdown(ctx context.Context) error {
	return ps.srv.Shutdown(ctx)
}

func (ps *PortalServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	ps.renderTemplate(w, "index.html", nil)
}

func (ps *PortalServer) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	ps.renderTemplate(w, "login.html", nil)
}

func (ps *PortalServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email    := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	role     := r.FormValue("role")

	if email == "" || password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}
	validRoles := map[string]bool{"provider": true, "consumer": true, "ntari_staff": true}
	if !validRoles[role] {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}

	var userID, hash string
	var err error

	switch role {
	case "provider":
		err = ps.db.Pool.QueryRow(r.Context(),
			`SELECT id, COALESCE(password_hash, '') FROM providers WHERE email = $1`,
			email,
		).Scan(&userID, &hash)
	case "ntari_staff":
		err = ps.db.Pool.QueryRow(r.Context(),
			`SELECT id, COALESCE(password_hash, '') FROM providers WHERE email = $1 AND is_staff = TRUE`,
			email,
		).Scan(&userID, &hash)
	case "consumer":
		err = ps.db.Pool.QueryRow(r.Context(),
			`SELECT id, COALESCE(password_hash, '') FROM consumers WHERE email = $1`,
			email,
		).Scan(&userID, &hash)
	}
	if err != nil {
		// Row not found or DB error — return 401 to avoid account enumeration.
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := ps.sm.CreateToken(SessionClaims{
		UserID:    userID,
		Email:     email,
		Role:      role,
		ExpiresAt: time.Now().Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   900,
	})

	switch role {
	case "provider":
		http.Redirect(w, r, "/provider/dashboard", http.StatusSeeOther)
	case "consumer":
		http.Redirect(w, r, "/consumer/marketplace", http.StatusSeeOther)
	case "ntari_staff":
		http.Redirect(w, r, "/dispute/queue", http.StatusSeeOther)
	}
}

func (ps *PortalServer) handleProviderDashboard(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	ps.renderTemplate(w, "provider_dashboard.html", claims)
}

func (ps *PortalServer) handleConsumerMarketplace(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())

	// Step 1 — fetch current platform rates.
	rateRows, err := ps.db.Pool.Query(r.Context(),
		`SELECT resource_type, base_rate
		 FROM resource_pricing
		 WHERE (effective_until IS NULL OR effective_until > NOW())
		 ORDER BY resource_type, effective_from DESC`,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rateRows.Close()

	rates := make(map[string]float64)
	for rateRows.Next() {
		var rt string
		var rate float64
		if err := rateRows.Scan(&rt, &rate); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// Keep only the first (most recent) row per resource_type.
		if _, seen := rates[rt]; !seen {
			rates[rt] = rate
		}
	}
	if err := rateRows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Step 2 — fetch online nodes with their default resource profile.
	nodeRows, err := ps.db.Pool.Query(r.Context(),
		`SELECT
		     n.id, n.node_class, n.country_code,
		     (n.hardware_profile->>'CPUCores')::int        AS cpu_cores,
		     (n.hardware_profile->>'RAMMB')::bigint / 1024 AS ram_gb,
		     COALESCE(rp.ram_pct, 100)                     AS ram_pct,
		     COALESCE(rp.cpu_enabled, true)                AS cpu_enabled,
		     COALESCE(rp.storage_gb, 0)                    AS storage_gb,
		     COALESCE(rp.bandwidth_mbps, 0)                AS bandwidth_mbps,
		     COALESCE(rp.price_multiplier, 1.0)            AS price_multiplier
		 FROM nodes n
		 LEFT JOIN resource_profiles rp
		     ON rp.node_id = n.id AND rp.is_default = TRUE
		 WHERE n.status = 'online'
		 ORDER BY n.node_class, n.country_code`,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer nodeRows.Close()

	classMap := make(map[string][]NodeListing)
	classOrder := []string{}

	for nodeRows.Next() {
		var (
			id, nodeClass, country string
			cpuCores               int
			ramGB                  int64
			ramPct                 int
			cpuEnabled             bool
			storageGB, bwMbps      int
			priceMultiplier        float64
		)
		if err := nodeRows.Scan(&id, &nodeClass, &country, &cpuCores, &ramGB,
			&ramPct, &cpuEnabled, &storageGB, &bwMbps, &priceMultiplier); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Step 3 — compute available resources and estimated hourly rate.
		availCPU := float64(cpuCores)
		if !cpuEnabled {
			availCPU = 0
		}
		availRAM := float64(ramGB) * float64(ramPct) / 100.0
		estHrRate := (availCPU*rates["cpu_core_hr"] + availRAM*rates["ram_gb_hr"]) * priceMultiplier

		listing := NodeListing{
			ID:            id,
			CountryCode:   country,
			CPUCores:      cpuCores,
			RAMGB:         ramGB,
			StorageGB:     storageGB,
			BandwidthMbps: bwMbps,
			EstHrRate:     estHrRate,
		}

		if _, exists := classMap[nodeClass]; !exists {
			classOrder = append(classOrder, nodeClass)
		}
		classMap[nodeClass] = append(classMap[nodeClass], listing)
	}
	if err := nodeRows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Step 4 — build ClassGroup slice in stable order.
	classNames := map[string]string{
		"A": "Class A — Dedicated Server",
		"B": "Class B — Mobile GPU",
		"C": "Class C — Smart TV",
		"D": "Class D — NAS / Storage",
	}

	groups := make([]ClassGroup, 0, len(classOrder))
	for _, cls := range classOrder {
		nodes := classMap[cls]
		minRate, maxRate := nodes[0].EstHrRate, nodes[0].EstHrRate
		for _, n := range nodes[1:] {
			if n.EstHrRate < minRate {
				minRate = n.EstHrRate
			}
			if n.EstHrRate > maxRate {
				maxRate = n.EstHrRate
			}
		}
		name, ok := classNames[cls]
		if !ok {
			name = "Class " + cls
		}
		groups = append(groups, ClassGroup{
			Class:     cls,
			ClassName: name,
			Nodes:     nodes,
			MinRate:   minRate,
			MaxRate:   maxRate,
		})
	}

	ps.renderTemplate(w, "consumer_marketplace.html", MarketplaceData{
		Email:     claims.Email,
		Classes:   groups,
		CPURateHr: rates["cpu_core_hr"],
		RAMRateHr: rates["ram_gb_hr"],
	})
}

func (ps *PortalServer) handleDisputeQueue(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())

	rows, err := ps.db.Pool.Query(r.Context(),
		`SELECT
		     d.id, d.status, d.reason, d.consumer_refund_pct,
		     d.created_at, d.payment_intent_id,
		     j.id AS job_id,
		     c.email AS consumer_email,
		     n.node_class, n.country_code
		 FROM disputes d
		 JOIN jobs j ON j.id = d.job_id
		 JOIN consumers c ON c.id = d.consumer_id
		 JOIN nodes n ON n.id = d.node_id
		 WHERE d.status IN ('open', 'under_review')
		 ORDER BY d.created_at ASC`,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var disputes []DisputeRow
	for rows.Next() {
		var d DisputeRow
		if err := rows.Scan(
			&d.ID, &d.Status, &d.Reason, &d.ConsumerRefundPct,
			&d.CreatedAt, &d.PaymentIntentID,
			&d.JobID, &d.ConsumerEmail,
			&d.NodeClass, &d.CountryCode,
		); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		disputes = append(disputes, d)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ps.renderTemplate(w, "dispute_queue.html", DisputeQueueData{
		Email:    claims.Email,
		Disputes: disputes,
	})
}

func (ps *PortalServer) handleSubmitJob(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	nodeID := r.FormValue("node_id")
	if nodeID == "" {
		http.Error(w, "node_id is required", http.StatusBadRequest)
		return
	}

	var status string
	err := ps.db.Pool.QueryRow(r.Context(),
		`SELECT status FROM nodes WHERE id = $1`,
		nodeID,
	).Scan(&status)
	if err != nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if status != "online" {
		http.Error(w, "node is not online", http.StatusConflict)
		return
	}

	resp, err := ps.orch.SubmitJob(r.Context(), orchestrator.SubmitJobRequest{
		ConsumerID:   claims.UserID,
		WorkloadType: "app_hosting",
		CPUCores:     2,
		RAMMB:        4096,
	})
	if err != nil {
		http.Error(w, "failed to submit job", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/consumer/job/"+resp.JobID, http.StatusSeeOther)
}

func (ps *PortalServer) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	jobID := r.PathValue("id")

	var data JobStatusData
	data.Email = claims.Email
	data.JobID = jobID

	err := ps.db.Pool.QueryRow(r.Context(),
		`SELECT status, COALESCE(node_id::text, ''), created_at
		 FROM jobs WHERE id = $1 AND consumer_id = $2`,
		jobID, claims.UserID,
	).Scan(&data.Status, &data.NodeID, &data.CreatedAt)
	if err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	ps.renderTemplate(w, "consumer_job_status.html", data)
}

func (ps *PortalServer) handleDisputeResolve(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	disputeID := r.PathValue("id")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	consumerRefundPct, err := strconv.Atoi(r.FormValue("consumer_refund_pct"))
	if err != nil || consumerRefundPct < 0 || consumerRefundPct > 100 {
		http.Error(w, "consumer_refund_pct must be between 0 and 100", http.StatusBadRequest)
		return
	}

	var (
		paymentIntentID string
		jobID           string
	)
	err = ps.db.Pool.QueryRow(r.Context(),
		`SELECT d.payment_intent_id, j.id
		 FROM disputes d
		 JOIN jobs j ON j.id = d.job_id
		 WHERE d.id = $1 AND d.status IN ('open', 'under_review')`,
		disputeID,
	).Scan(&paymentIntentID, &jobID)
	if err != nil {
		http.Error(w, "dispute not found or already resolved", http.StatusNotFound)
		return
	}

	if consumerRefundPct > 0 {
		var amountCents int64
		err = ps.db.Pool.QueryRow(r.Context(),
			`SELECT amount_cents FROM jobs WHERE id = $1`,
			jobID,
		).Scan(&amountCents)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		refundAmount := amountCents * int64(consumerRefundPct) / 100
		if refundAmount > 0 {
			if err := ps.payment.CreateRefund(r.Context(), paymentIntentID, refundAmount); err != nil {
				http.Error(w, "failed to issue refund", http.StatusInternalServerError)
				return
			}
		}
	}

	_, err = ps.db.Pool.Exec(r.Context(),
		`UPDATE disputes
		 SET status = 'resolved', consumer_refund_pct = $1, arbiter_id = $2,
		     arbiter_notes = 'resolved via terminal', resolved_at = NOW(), updated_at = NOW()
		 WHERE id = $3`,
		consumerRefundPct, claims.UserID, disputeID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dispute/queue", http.StatusSeeOther)
}

func (ps *PortalServer) handleDisputeReview(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	disputeID := r.PathValue("id")

	_, err := ps.db.Pool.Exec(r.Context(),
		`UPDATE disputes SET status = 'under_review', arbiter_id = $1, updated_at = NOW()
		 WHERE id = $2 AND status = 'open'`,
		claims.UserID, disputeID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dispute/queue", http.StatusSeeOther)
}

func (ps *PortalServer) handleProviderOnboardingPage(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())

	var ispTier        string
	var disclosureAt   *time.Time
	var stripeComplete bool
	err := ps.db.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(isp_tier, ''), disclosure_accepted_at, stripe_onboarding_complete
		 FROM providers WHERE id = $1`,
		claims.UserID,
	).Scan(&ispTier, &disclosureAt, &stripeComplete)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if disclosureAt != nil && stripeComplete {
		http.Redirect(w, r, "/provider/provision", http.StatusSeeOther)
		return
	}

	ps.renderTemplate(w, "provider_onboarding.html", onboardingData{
		Email:              claims.Email,
		ISPTier:            ispTier,
		DisclosureAccepted: disclosureAt != nil,
		StripeComplete:     stripeComplete,
	})
}

func (ps *PortalServer) handleProviderOnboarding(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ispTier    := r.FormValue("isp_tier")
	disclosure := r.FormValue("disclosure_accepted")

	validTiers := map[string]bool{"business": true, "residential": true, "cellular": true}
	if !validTiers[ispTier] {
		http.Error(w, "invalid ISP tier", http.StatusBadRequest)
		return
	}
	if ispTier == "cellular" {
		http.Error(w, "Cellular connections are not eligible for Class A node listings", http.StatusBadRequest)
		return
	}
	if disclosure != "true" {
		http.Error(w, "disclosure acceptance is required", http.StatusBadRequest)
		return
	}

	_, err := ps.db.Pool.Exec(r.Context(),
		`UPDATE providers SET isp_tier = $1, disclosure_accepted_at = NOW(), updated_at = NOW() WHERE id = $2`,
		ispTier, claims.UserID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	accountID, err := ps.payment.CreateConnectedAccount(r.Context(), claims.Email, claims.Email)
	if err != nil {
		http.Error(w, "failed to create Stripe account", http.StatusInternalServerError)
		return
	}

	_, err = ps.db.Pool.Exec(r.Context(),
		`UPDATE providers SET stripe_account_id = $1, updated_at = NOW() WHERE id = $2`,
		accountID, claims.UserID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	onboardingURL, err := ps.payment.CreateOnboardingLink(
		r.Context(), accountID,
		ps.baseURL+"/provider/onboarding",
		ps.baseURL+"/provider/onboarding/return",
	)
	if err != nil {
		http.Error(w, "failed to create onboarding link", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, onboardingURL, http.StatusSeeOther)
}

func (ps *PortalServer) handleProviderOnboardingReturn(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())

	var stripeAccountID string
	err := ps.db.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(stripe_account_id, '') FROM providers WHERE id = $1`,
		claims.UserID,
	).Scan(&stripeAccountID)
	if err != nil || stripeAccountID == "" {
		http.Redirect(w, r, "/provider/onboarding", http.StatusSeeOther)
		return
	}

	status, err := ps.payment.CheckOnboardingStatus(r.Context(), stripeAccountID)
	if err != nil {
		http.Redirect(w, r, "/provider/onboarding", http.StatusSeeOther)
		return
	}

	if status.TransfersActive && !status.RequirementsPending {
		_, err = ps.db.Pool.Exec(r.Context(),
			`UPDATE providers SET stripe_onboarding_complete = TRUE, onboarding_complete = TRUE, updated_at = NOW() WHERE id = $1`,
			claims.UserID,
		)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/provider/provision", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/provider/onboarding", http.StatusSeeOther)
}

func (ps *PortalServer) handleProviderProvision(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())

	var onboardingComplete bool
	err := ps.db.Pool.QueryRow(r.Context(),
		`SELECT onboarding_complete FROM providers WHERE id = $1`,
		claims.UserID,
	).Scan(&onboardingComplete)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !onboardingComplete {
		http.Redirect(w, r, "/provider/onboarding", http.StatusSeeOther)
		return
	}

	// Get the provider's first node ID (used as target for new profiles).
	var nodeID string
	_ = ps.db.Pool.QueryRow(r.Context(),
		`SELECT id FROM nodes WHERE provider_id = $1 ORDER BY created_at ASC LIMIT 1`,
		claims.UserID,
	).Scan(&nodeID)

	rows, err := ps.db.Pool.Query(r.Context(),
		`SELECT id, name, is_default, cpu_enabled, gpu_pct, ram_pct, storage_gb, bandwidth_mbps
		 FROM resource_profiles
		 WHERE node_id IN (SELECT id FROM nodes WHERE provider_id = $1)
		 ORDER BY is_default DESC, created_at ASC`,
		claims.UserID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var profiles []ProfileRow
	for rows.Next() {
		var p ProfileRow
		if err := rows.Scan(&p.ID, &p.Name, &p.IsDefault, &p.CPUEnabled,
			&p.GPUPct, &p.RAMPct, &p.StorageGB, &p.BandwidthMbps); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ps.renderTemplate(w, "provider_provision.html", provisionData{
		Email:    claims.Email,
		NodeID:   nodeID,
		Profiles: profiles,
	})
}

func (ps *PortalServer) handleAddProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	nodeID     := r.FormValue("node_id")
	name       := strings.TrimSpace(r.FormValue("name"))
	isDefault  := r.FormValue("is_default") == "true"
	cpuEnabled := r.FormValue("cpu_enabled") == "true"

	if nodeID == "" {
		http.Error(w, "node_id is required", http.StatusBadRequest)
		return
	}
	if name == "" {
		http.Error(w, "profile name is required", http.StatusBadRequest)
		return
	}

	ramPct, err := strconv.Atoi(r.FormValue("ram_pct"))
	if err != nil || ramPct < 1 || ramPct > 100 {
		http.Error(w, "ram_pct must be between 1 and 100", http.StatusBadRequest)
		return
	}
	storageGB, err := strconv.Atoi(r.FormValue("storage_gb"))
	if err != nil || storageGB < 0 {
		http.Error(w, "storage_gb must be 0 or greater", http.StatusBadRequest)
		return
	}
	bandwidthMbps, err := strconv.Atoi(r.FormValue("bandwidth_mbps"))
	if err != nil || bandwidthMbps < 0 {
		http.Error(w, "bandwidth_mbps must be 0 or greater", http.StatusBadRequest)
		return
	}
	priceMultiplier, err := strconv.ParseFloat(r.FormValue("price_multiplier"), 64)
	if err != nil || priceMultiplier < 0.5 || priceMultiplier > 2.0 {
		http.Error(w, "price_multiplier must be between 0.50 and 2.00", http.StatusBadRequest)
		return
	}

	// Verify the node belongs to the authenticated provider.
	var providerID string
	err = ps.db.Pool.QueryRow(r.Context(),
		`SELECT provider_id FROM nodes WHERE id = $1`,
		nodeID,
	).Scan(&providerID)
	if err != nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if providerID != claims.UserID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Clear any existing default for this node before inserting a new one.
	if isDefault {
		_, err = ps.db.Pool.Exec(r.Context(),
			`UPDATE resource_profiles SET is_default = FALSE WHERE node_id = $1`,
			nodeID,
		)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	_, err = ps.db.Pool.Exec(r.Context(),
		`INSERT INTO resource_profiles
		 (node_id, name, is_default, cpu_enabled, gpu_pct, ram_pct, storage_gb, bandwidth_mbps, price_multiplier)
		 VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8)`,
		nodeID, name, isDefault, cpuEnabled, ramPct, storageGB, bandwidthMbps, priceMultiplier,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/provider/provision", http.StatusSeeOther)
}
