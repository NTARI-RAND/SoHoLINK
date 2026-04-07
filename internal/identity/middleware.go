package identity

import (
	"context"
	"net/http"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

type contextKey struct{}

// RequireSPIFFE rejects any request whose TLS connection does not carry a peer
// certificate with a valid SPIFFE URI SAN. On success it stores the parsed
// spiffeid.ID in the request context; retrieve it with SPIFFEIDFromContext.
func RequireSPIFFE(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "mTLS required", http.StatusUnauthorized)
			return
		}
		for _, cert := range r.TLS.PeerCertificates {
			for _, uri := range cert.URIs {
				if uri.Scheme != "spiffe" {
					continue
				}
				id, err := spiffeid.FromURI(uri)
				if err != nil {
					continue
				}
				ctx := context.WithValue(r.Context(), contextKey{}, id)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		http.Error(w, "SPIFFE identity required", http.StatusUnauthorized)
	})
}

// SPIFFEIDFromContext retrieves the SPIFFE ID stored by RequireSPIFFE.
// Returns false if the context carries no SPIFFE ID.
func SPIFFEIDFromContext(ctx context.Context) (spiffeid.ID, bool) {
	id, ok := ctx.Value(contextKey{}).(spiffeid.ID)
	return id, ok
}
