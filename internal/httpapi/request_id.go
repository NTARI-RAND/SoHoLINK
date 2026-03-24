package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/logging"
)

// requestIDMiddleware assigns a unique request ID to every incoming request.
// The ID is propagated via context (for structured logging) and returned
// in the X-Request-ID response header (for client-side correlation).
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the client sent a request ID (e.g. from a load balancer)
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = generateRequestID()
		}

		// Set the request ID in context for structured logging
		ctx := logging.ContextWithRequestID(r.Context(), rid)

		// Set the response header so the client can correlate
		w.Header().Set("X-Request-ID", rid)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// generateRequestID creates a short, unique request ID (16 hex chars).
func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}
