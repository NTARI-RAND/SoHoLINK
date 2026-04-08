package portal

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

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

// New constructs a PortalServer. It walks templatesDir recursively to collect
// all .html file paths (not parsed yet — see renderTemplate), registers routes,
// and builds the http.Server.
func New(db *store.DB, addr string, sessionSecret []byte, templatesDir string, paymentClient *payment.Client, baseURL string) (*PortalServer, error) {
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
	mux.Handle("GET /dispute/queue",
		RequireAuth(sm, RequireRole("ntari_staff", http.HandlerFunc(ps.handleDisputeQueue))))

	mux.Handle("GET /provider/onboarding",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderOnboardingPage))))
	mux.Handle("POST /provider/onboarding",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderOnboarding))))
	mux.Handle("GET /provider/onboarding/return",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderOnboardingReturn))))
	mux.Handle("GET /provider/provision",
		RequireAuth(sm, RequireRole("provider", http.HandlerFunc(ps.handleProviderProvision))))

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
	ps.renderTemplate(w, "consumer_marketplace.html", claims)
}

func (ps *PortalServer) handleDisputeQueue(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	ps.renderTemplate(w, "dispute_queue.html", claims)
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
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
