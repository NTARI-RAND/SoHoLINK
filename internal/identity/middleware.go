package identity

import (
	"context"
	"net/http"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

type contextKey struct{}

// RequireSPIFFE rejects any request whose TLS connection does not carry a
// client certificate that cryptographically VERIFIES against the SPIRE trust
// bundle, and stores the authenticated spiffeid.ID in the request context
// (retrieve it with SPIFFEIDFromContext).
//
// Verification happens here, at the HTTP layer, on purpose: TLSServerConfigOptional
// only *requests* the client certificate (tls.RequestClientCert) so that
// /health and /allowlist stay reachable without one. That means the TLS stack
// does NOT validate the presented cert — so trusting its URI SAN directly would
// let any client on the network present a self-signed certificate asserting an
// arbitrary spiffe://.../node/<id> and be believed. x509svid.Verify validates
// the chain against the bundle AND returns the authenticated SPIFFE ID, closing
// that hole.
func RequireSPIFFE(bundle x509bundle.Source, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "mTLS required", http.StatusUnauthorized)
			return
		}
		id, _, err := x509svid.Verify(r.TLS.PeerCertificates, bundle)
		if err != nil {
			http.Error(w, "SPIFFE identity verification failed", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), contextKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SPIFFEIDFromContext retrieves the SPIFFE ID stored by RequireSPIFFE.
// Returns false if the context carries no SPIFFE ID.
func SPIFFEIDFromContext(ctx context.Context) (spiffeid.ID, bool) {
	id, ok := ctx.Value(contextKey{}).(spiffeid.ID)
	return id, ok
}

// WithSPIFFEID stores id in ctx using the same key as RequireSPIFFE.
// Intended for tests that call handlers directly, bypassing the middleware.
func WithSPIFFEID(ctx context.Context, id spiffeid.ID) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// UnavailableHandler returns an http.Handler that responds 503 to every
// request with a JSON body explaining that the SPIFFE identity source is
// not available. Used when the SPIRE Workload API socket cannot be reached
// and the orchestrator runs in degraded mode without mTLS — see TODO 12.
func UnavailableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"identity unavailable","detail":"SPIRE workload API socket not reachable; SPIFFE-protected routes are disabled"}`))
	})
}
