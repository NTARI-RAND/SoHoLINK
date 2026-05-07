package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/metrics"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// APIServer is the SoHoLINK control plane HTTP server. Node and job routes
// require mTLS via SPIRE SVIDs. A small set of plain routes (/health,
// /allowlist) sit outside the SPIFFE middleware so fresh agents and external
// monitors can reach them without an SVID.
type APIServer struct {
	srv        *http.Server
	metricsSrv *http.Server
	db         *store.DB
	registry   *orchestrator.NodeRegistry
	idSource   *identity.Source
}

// New constructs an APIServer. Node/job routes are registered on an inner mux
// wrapped with RequireSPIFFE. Plain routes (/health, /allowlist) are registered
// on the outer top-level mux. metricsAddr is the address for the separate plain
// HTTP metrics server — not wrapped with mTLS.
//
// If idSource is nil the server runs in degraded mode: it binds plain HTTP,
// SPIFFE-protected routes return 503, and /health reports
// "identity":"unavailable". This happens when the SPIRE Workload API socket
// cannot be reached at startup. See TODO 12.
func New(db *store.DB, registry *orchestrator.NodeRegistry, idSource *identity.Source, addr string, metricsAddr string, allowlistPath string) *APIServer {
	// authMux: all routes that require a valid SPIFFE SVID.
	authMux := http.NewServeMux()
	registerNodeRoutes(authMux, db, registry)

	// top: plain routes + SPIFFE-protected subtree.
	top := http.NewServeMux()
	top.HandleFunc("GET /health", healthHandler(idSource))
	top.HandleFunc("GET /allowlist", handleGetAllowlist(allowlistPath))
	if idSource != nil {
		top.Handle("/", identity.RequireSPIFFE(authMux))
	} else {
		// Degraded mode: SPIRE Workload API unreachable. SPIFFE-protected
		// routes return 503 with a descriptive body — see TODO 12.
		top.Handle("/", identity.UnavailableHandler())
	}

	s := &APIServer{
		db:       db,
		registry: registry,
		idSource: idSource,
	}
	s.srv = &http.Server{
		Addr:         addr,
		Handler:      top,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if idSource != nil {
		s.srv.TLSConfig = identity.TLSServerConfigOptional(idSource)
	}

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	s.metricsSrv = &http.Server{
		Addr:         metricsAddr,
		Handler:      metricsMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	return s
}

// Start begins accepting connections. With a non-nil idSource the server
// requires mTLS via SPIRE-issued SVIDs (HTTPS). In degraded mode (nil
// idSource) the server listens on plain HTTP — see New.
func (s *APIServer) Start(ctx context.Context) error {
	if s.idSource == nil {
		return s.srv.ListenAndServe()
	}
	return s.srv.ListenAndServeTLS("", "")
}

// StartMetrics starts a plain HTTP server on the metrics address serving only
// /metrics. It shuts down automatically when ctx is cancelled.
func (s *APIServer) StartMetrics(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.metricsSrv.Shutdown(context.Background())
	}()
	return s.metricsSrv.ListenAndServe()
}

// Shutdown gracefully drains active connections within the context deadline.
func (s *APIServer) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// writeError writes a JSON {"error":"..."} body with the given HTTP status.
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

// healthHandler returns a handler whose body reports SPIFFE identity status.
// Status is always 200 so the Compose healthcheck stays green in degraded
// mode — degraded state is surfaced in the body, not the status code.
func healthHandler(idSource *identity.Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identityStatus := "ready"
		if idSource == nil {
			identityStatus = "unavailable"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"status":   "ok",
			"identity": identityStatus,
		})
	}
}
