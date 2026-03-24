package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ---------------------------------------------------------------------------
// Live Prometheus counters (registered at init via promauto)
// ---------------------------------------------------------------------------

// httpRequestsTotal counts every HTTP request handled by the API server,
// labelled by HTTP method, normalised path, and response status code.
var httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "soholink_http_requests_total",
	Help: "Total HTTP requests handled by the API server, by method, path, and status code.",
}, []string{"method", "path", "status"})

// httpRequestDuration tracks the latency distribution of HTTP requests.
var httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "soholink_http_request_duration_seconds",
	Help:    "HTTP request latency in seconds.",
	Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
}, []string{"method", "path", "status"})

// httpActiveRequests tracks the number of in-flight HTTP requests.
var httpActiveRequests = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "soholink_http_active_requests",
	Help: "Number of HTTP requests currently being processed.",
})

// walletTopupTotal counts successfully initiated wallet topup requests.
var walletTopupTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "soholink_wallet_topup_total",
	Help: "Total wallet topup requests successfully initiated.",
})

// workloadPurchaseTotal counts workload purchase attempts, labelled by result.
var workloadPurchaseTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "soholink_workload_purchase_total",
	Help: "Workload purchase attempts by result label.",
}, []string{"result"})

// ---------------------------------------------------------------------------
// Business metrics — earnings, jobs, federation
// ---------------------------------------------------------------------------

// jobsScheduledTotal counts workloads dispatched by the orchestrator.
var jobsScheduledTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "soholink_jobs_scheduled_total",
	Help: "Total jobs scheduled by the orchestrator, by status.",
}, []string{"status"}) // "dispatched", "completed", "failed", "timeout"

// earningsTotal tracks cumulative earnings in satoshis.
var earningsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "soholink_earnings_sats_total",
	Help: "Cumulative earnings in satoshis.",
})

// federationPeers tracks the current number of connected federation peers.
var federationPeers = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "soholink_federation_peers",
	Help: "Current number of connected federation peers.",
})

// nodeUptimeSeconds reports how long the node has been running.
var nodeUptimeSeconds = promauto.NewGaugeFunc(prometheus.GaugeOpts{
	Name: "soholink_node_uptime_seconds",
	Help: "Seconds since node process started.",
}, func() float64 {
	return time.Since(nodeStartTime).Seconds()
})

// authAttemptsTotal counts authentication attempts by result.
var authAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "soholink_auth_attempts_total",
	Help: "Authentication attempts by result.",
}, []string{"result"}) // "success", "failed", "rate_limited"

// ---------------------------------------------------------------------------
// HTTP status-recording middleware
// ---------------------------------------------------------------------------

// statusRecorder is a minimal http.ResponseWriter wrapper that captures the
// HTTP status code written by the downstream handler, used by metricsMiddleware.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// metricsMiddleware wraps next and records request count, duration, and
// active request gauge for every HTTP request.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		httpActiveRequests.Inc()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		httpActiveRequests.Dec()
		elapsed := time.Since(start).Seconds()
		path := normalisePath(r.URL.Path)
		status := strconv.Itoa(rw.status)

		httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path, status).Observe(elapsed)
	})
}

// normalisePath reduces high-cardinality path labels by replacing resource IDs
// with placeholders. e.g. /api/lbtas/score/did:key:abc → /api/lbtas/score/:id
func normalisePath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		// Replace DID-like segments
		if strings.HasPrefix(part, "did:") {
			parts[i] = ":id"
		}
		// Replace hex strings longer than 16 chars (hashes, tokens)
		if len(part) > 16 && isHexLike(part) {
			parts[i] = ":id"
		}
	}
	return strings.Join(parts, "/")
}

// isHexLike returns true if s looks like a hex string or base64 token.
func isHexLike(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Metric recording helpers (called from handlers)
// ---------------------------------------------------------------------------

// RecordJobScheduled increments the job counter for a given status.
func RecordJobScheduled(status string) {
	jobsScheduledTotal.WithLabelValues(status).Inc()
}

// RecordEarnings adds sats to the cumulative earnings counter.
func RecordEarnings(sats float64) {
	earningsTotal.Add(sats)
}

// RecordFederationPeers updates the peer gauge.
func RecordFederationPeers(count int) {
	federationPeers.Set(float64(count))
}

// RecordAuthAttempt increments auth counter by result.
func RecordAuthAttempt(result string) {
	authAttemptsTotal.WithLabelValues(result).Inc()
}

// ---------------------------------------------------------------------------
// Metric name catalogue (kept for reference / dashboard generation)
// ---------------------------------------------------------------------------

// MetricNames holds the full set of metric names used by the system.
var MetricNames = struct {
	ComputeJobsTotal        string
	ComputeQueueSize        string
	ComputeCPUSecondsTotal  string
	StorageUploadsTotal     string
	StorageBytesStored      string
	StorageMalwareBlocked   string
	PrintJobsTotal          string
	PrintFilamentGramsTotal string
	PortalSessionsActive    string
	PortalAuthFailuresTotal string
	PortalBandwidthBytes    string
	PaymentsPending         string
	PaymentsSettledTotal    string
	PaymentsFailedTotal     string
	LBTASRatingsTotal       string
	LBTASDisputesTotal      string
	LBTASAutoResolvedTotal  string
	HTTPRequestsTotal       string
	HTTPRequestDuration     string
	HTTPActiveRequests      string
	WalletTopupTotal        string
	WorkloadPurchaseTotal   string
}{
	ComputeJobsTotal:        "compute_jobs_total",
	ComputeQueueSize:        "compute_queue_size",
	ComputeCPUSecondsTotal:  "compute_cpu_seconds_total",
	StorageUploadsTotal:     "storage_uploads_total",
	StorageBytesStored:      "storage_bytes_stored",
	StorageMalwareBlocked:   "storage_malware_blocked",
	PrintJobsTotal:          "print_jobs_total",
	PrintFilamentGramsTotal: "print_filament_grams_total",
	PortalSessionsActive:    "portal_sessions_active",
	PortalAuthFailuresTotal: "portal_auth_failures_total",
	PortalBandwidthBytes:    "portal_bandwidth_bytes",
	PaymentsPending:         "payments_pending",
	PaymentsSettledTotal:    "payments_settled_total",
	PaymentsFailedTotal:     "payments_failed_total",
	LBTASRatingsTotal:       "lbtas_ratings_total",
	LBTASDisputesTotal:      "lbtas_disputes_total",
	LBTASAutoResolvedTotal:  "lbtas_auto_resolved_total",
	HTTPRequestsTotal:       "soholink_http_requests_total",
	HTTPRequestDuration:     "soholink_http_request_duration_seconds",
	HTTPActiveRequests:      "soholink_http_active_requests",
	WalletTopupTotal:        "soholink_wallet_topup_total",
	WorkloadPurchaseTotal:   "soholink_workload_purchase_total",
}
