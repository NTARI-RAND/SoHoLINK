package httpapi

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// dashboardFS holds the embedded ui/dashboard/ asset tree.
// Set via SetDashboardFS before Server.Start() is called.
var dashboardFS fs.FS

// SetDashboardFS registers the embedded dashboard asset tree.
// Call this in main() before Server.Start():
//
//	sub, _ := fs.Sub(soholink.DashboardFS, "ui/dashboard")
//	server.SetDashboardFS(sub)
func (s *Server) SetDashboardFS(fsys fs.FS) {
	dashboardFS = fsys
}

// nodeStartTime is set once at process start and used to report uptime.
var nodeStartTime = time.Now()

// handleDashboard serves the embedded SPA at /dashboard and /dashboard/*.
// Unknown paths fall through to index.html so the JS hash-router works.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if dashboardFS == nil {
		http.Error(w, "dashboard not available", http.StatusNotFound)
		return
	}

	// Strip the /dashboard prefix to obtain the relative asset path.
	path := strings.TrimPrefix(r.URL.Path, "/dashboard")
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		path = "index.html"
	}

	// If the asset doesn't exist (e.g. a hash-route URL), serve index.html.
	if _, err := fs.Stat(dashboardFS, path); err != nil {
		path = "index.html"
	}

	// Explicit content-type so browsers don't sniff incorrectly.
	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	}

	if path == "index.html" {
		// Prevent stale SPA from persisting in browser cache.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}

	// http.ServeFileFS handles Range, If-Modified-Since, and ETags automatically.
	// Requires Go 1.22+; go.mod enforces 1.25.7.
	http.ServeFileFS(w, r, dashboardFS, path)
}

// handleStatus returns a JSON snapshot of node health and resource usage.
// This is the primary data source for the Dashboard screen radial dials.
// Only GET is accepted.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	uptime := time.Since(nodeStartTime)

	// Active rentals — use existing store method if available.
	activeRentals := 0
	if s.store != nil {
		if rentals, err := s.store.GetActiveRentals(ctx); err == nil {
			activeRentals = len(rentals)
		}
	}

	// Federation nodes — mobile hub gives connected mobile count.
	federationNodes := 0
	mobileNodes := 0
	if s.mobileHub != nil {
		mobileNodes = len(s.mobileHub.ActiveNodes())
		federationNodes = mobileNodes
	}

	// Earnings today — revenue events since midnight UTC.
	earnedSatsToday := int64(0)
	if s.store != nil {
		midnight := time.Now().UTC().Truncate(24 * time.Hour)
		if earned, err := s.store.GetRevenueSince(ctx, midnight); err == nil {
			earnedSatsToday = earned
		}
	}

	// Resource utilisation fields remain zero until Phase 2 wires live
	// gopsutil readings. The frontend handles zero gracefully (dials at 0%).
	type statusResponse struct {
		UptimeSeconds    int64   `json:"uptime_seconds"`
		OS               string  `json:"os"`
		ActiveRentals    int     `json:"active_rentals"`
		FederationNodes  int     `json:"federation_nodes"`
		MobileNodes      int     `json:"mobile_nodes"`
		EarnedSatsToday  int64   `json:"earned_sats_today"`
		CPUOfferedPct    int     `json:"cpu_offered_pct"`
		CPUUsedPct       float64 `json:"cpu_used_pct"`
		RAMOfferedGB     float64 `json:"ram_offered_gb"`
		RAMUsedPct       float64 `json:"ram_used_pct"`
		StorageOfferedGB float64 `json:"storage_offered_gb"`
		StorageUsedPct   float64 `json:"storage_used_pct"`
		NetOfferedMbps   int     `json:"net_offered_mbps"`
		NetUsedPct       float64 `json:"net_used_pct"`
	}

	resp := statusResponse{
		UptimeSeconds:    int64(uptime.Seconds()),
		OS:               runtime.GOOS + "/" + runtime.GOARCH,
		ActiveRentals:    activeRentals,
		FederationNodes:  federationNodes,
		MobileNodes:      mobileNodes,
		EarnedSatsToday:  earnedSatsToday,
		CPUOfferedPct:    50,
		CPUUsedPct:       0,
		RAMOfferedGB:     0,
		RAMUsedPct:       0,
		StorageOfferedGB: 0,
		StorageUsedPct:   0,
		NetOfferedMbps:   0,
		NetUsedPct:       0,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // #nosec G104 -- response write errors are non-actionable
}
