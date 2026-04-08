package metrics

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTPRequestsTotal counts HTTP requests by handler, method, and response status code.
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "soholink_http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"handler", "method", "status_code"},
	)

	// ActiveJobsGauge tracks the number of currently active jobs.
	ActiveJobsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "soholink_active_jobs",
		Help: "Number of currently active jobs.",
	})

	// NodesOnlineGauge tracks the number of nodes currently online.
	NodesOnlineGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "soholink_nodes_online",
		Help: "Number of nodes currently online.",
	})

	// JobsSubmittedTotal counts job submissions by workload type.
	JobsSubmittedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "soholink_jobs_submitted_total",
			Help: "Total number of jobs submitted.",
		},
		[]string{"workload_type"},
	)

	// HeartbeatsTotal counts heartbeats received per node.
	HeartbeatsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "soholink_heartbeats_total",
			Help: "Total number of heartbeats received.",
		},
		[]string{"node_id"},
	)

	// UptimeScorerDuration measures the duration of uptime scorer runs in seconds.
	UptimeScorerDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "soholink_uptime_scorer_duration_seconds",
		Help:    "Duration of uptime scorer runs in seconds.",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	prometheus.MustRegister(
		HTTPRequestsTotal,
		ActiveJobsGauge,
		NodesOnlineGauge,
		JobsSubmittedTotal,
		HeartbeatsTotal,
		UptimeScorerDuration,
	)
}

// Handler returns the Prometheus HTTP handler for the /metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// responseWriter wraps http.ResponseWriter to capture the written status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// InstrumentHandler wraps next with Prometheus request counting, recording
// the handler name, HTTP method, and response status code as labels.
func InstrumentHandler(handler string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		HTTPRequestsTotal.WithLabelValues(handler, r.Method, strconv.Itoa(rw.statusCode)).Inc()
	})
}
