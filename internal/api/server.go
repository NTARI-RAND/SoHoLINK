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

// APIServer is the SoHoLINK control plane HTTP server. All connections use
// mTLS via SPIRE SVIDs; every request must carry a valid SPIFFE identity.
type APIServer struct {
	srv        *http.Server
	metricsSrv *http.Server
	db         *store.DB
	registry   *orchestrator.NodeRegistry
	idSource   *identity.Source
}

// New constructs an APIServer. It registers all routes on a single mux,
// wraps the mux with RequireSPIFFE middleware, and configures the TLS
// settings from the SPIRE identity source. metricsAddr is the address for
// the plain HTTP metrics server (e.g. ":9091") — not wrapped with mTLS.
func New(db *store.DB, registry *orchestrator.NodeRegistry, idSource *identity.Source, addr string, metricsAddr string) *APIServer {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	})

	registerNodeRoutes(mux, db, registry)

	s := &APIServer{
		db:       db,
		registry: registry,
		idSource: idSource,
	}
	s.srv = &http.Server{
		Addr:         addr,
		Handler:      identity.RequireSPIFFE(mux),
		TLSConfig:    identity.TLSServerConfig(idSource),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
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

// Start begins accepting TLS connections. Empty strings are passed to
// ListenAndServeTLS because the TLS config supplies the certificates directly
// from SPIRE — no certificate files are used.
func (s *APIServer) Start(ctx context.Context) error {
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
