package portal

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// PortalServer is the SoHoLINK marketplace portal HTTP server. It sits behind
// NGINX, which handles TLS termination, so ListenAndServe (not TLS) is used.
type PortalServer struct {
	srv           *http.Server
	db            *store.DB
	sm            *SessionManager
	templatePaths []string
}

// New constructs a PortalServer. It walks templatesDir recursively to collect
// all .html file paths (not parsed yet — see renderTemplate), registers routes,
// and builds the http.Server.
func New(db *store.DB, addr string, sessionSecret []byte, templatesDir string) (*PortalServer, error) {
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
	ps := &PortalServer{db: db, sm: sm, templatePaths: paths}

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
//
// layout.html and the page file are located by base name within templatePaths.
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

// handleLogin authenticates the user and issues a session cookie.
// Full credential verification against the database is wired in Phase 3 Step 2.
func (ps *PortalServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
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
