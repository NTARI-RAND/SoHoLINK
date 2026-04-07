package httpapi

import (
	"log"
	"net/http"
	"strings"
)

// publicPaths are accessible without a device token.
// Federation endpoints are intentionally public: provider nodes authenticate
// via Ed25519 signatures embedded in the request body, not device tokens.
var publicPaths = map[string]bool{
	"/api/health":                    true,
	"/api/ready":                     true, // readiness probe — public for load balancers/k8s
	"/api/version":                   true, // build-time version info — public, no token required
	"/api/status":                    true, // node status snapshot — public for mobile dashboard
	"/api/auth/challenge":            true,
	"/api/auth/connect":              true,
	"/api/federation/info":           true,
	"/api/federation/peers":          true,
	"/api/federation/announce":       true,
	"/api/federation/heartbeat":      true,
	"/api/federation/deregister":     true,
	"/api/federation/blocklist":      true, // Item 2: public peer pull — no token required
	"/api/auth/owner-login":          true, // Browser seed login — no token needed to obtain a token
	"/api/webhooks/stripe":           true, // Stripe webhook: verified by Stripe-Signature, not device token
	"/metrics":                       true, // Prometheus scrape endpoint — no auth required
}

// authMiddleware wraps the entire ServeMux.  Every request whose path is NOT
// in publicPaths must carry a valid device token in the Authorization header:
//
//	Authorization: Bearer <64-char-hex-device-token>
//
// After token validation, the owner DID is checked against the platform DID
// blocklist (Item 2). Blocked DIDs receive 403 Forbidden.
//
// If the store is nil (e.g. during testing without a DB), authentication is
// skipped and all requests are allowed through.
// setCORSHeaders applies the Access-Control-Allow-Origin header using the
// server's configured allowedOrigins allowlist.  If the list is empty or
// contains "*", the wildcard is used for backward compatibility.
func (s *Server) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	if len(s.allowedOrigins) == 0 {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		return
	}
	origin := r.Header.Get("Origin")
	for _, allowed := range s.allowedOrigins {
		if allowed == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			return
		}
		if allowed == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			return
		}
	}
	// Origin not in allowlist — omit the header (browser will block the request).
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS pre-flight — let it through so the Flutter web app can reach us.
		if r.Method == http.MethodOptions {
			s.setCORSHeaders(w, r)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Set CORS headers on every real response.
		s.setCORSHeaders(w, r)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

		// Skip auth for public endpoints.
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for all static SPA assets (anything not under /api/ or /ws/).
		// The browser dashboard HTML/JS/CSS must load without a token; auth happens
		// at the API layer when the SPA makes authenticated API calls.
		if !strings.HasPrefix(r.URL.Path, "/api/") &&
			!strings.HasPrefix(r.URL.Path, "/ws/") &&
			!strings.HasPrefix(r.URL.Path, "/metrics") {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth when no store is wired (test / dev shortcut).
		if s.store == nil {
			next.ServeHTTP(w, r)
			return
		}

		token := bearerToken(r)
		if token == "" {
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		ok, err := s.store.ValidateDeviceToken(r.Context(), token)
		if err != nil || !ok {
			http.Error(w, "invalid or revoked token", http.StatusUnauthorized)
			return
		}

		// DID blocklist check (Item 2 — federation-level account suspension).
		// The owner DID represents this node's authenticated identity.
		if s.blocklist != nil {
			ownerDID, _ := s.store.GetNodeInfo(r.Context(), "owner_did")
			if ownerDID != "" {
				if blocked, reason, blErr := s.blocklist.IsBlocked(r.Context(), ownerDID); blErr == nil && blocked {
					log.Printf("[auth] blocked DID attempted access: did=%s path=%s reason=%s",
						ownerDID, r.URL.Path, reason)
					http.Error(w, "account suspended: "+reason, http.StatusForbidden)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// bearerToken extracts the token from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
