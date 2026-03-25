package httpapi

import "net/http"

// securityHeadersMiddleware sets standard security headers on every HTTP response.
// These headers defend against common browser-based attacks (XSS, MIME sniffing,
// clickjacking, downgrade attacks) with zero config required.
//
// Header inventory:
//
//	X-Content-Type-Options: nosniff         — prevents MIME type sniffing
//	X-Frame-Options: DENY                   — blocks clickjacking via iframes
//	X-XSS-Protection: 0                     — disables legacy XSS filter (CSP preferred)
//	Referrer-Policy: strict-origin-when-cross-origin — limits referrer leakage
//	Permissions-Policy: camera=(), microphone=(), geolocation=() — restricts browser APIs
//	Content-Security-Policy: default-src 'self' — restricts resource loading to same origin
//
// When TLS is active (detected via r.TLS != nil or X-Forwarded-Proto: https),
// Strict-Transport-Security is also set with a 1-year max-age.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Prevent MIME type sniffing — stops browsers from interpreting
		// files as a different content type than declared.
		h.Set("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking — blocks this page from being embedded in iframes.
		h.Set("X-Frame-Options", "DENY")

		// Disable legacy XSS filter — modern browsers should use CSP instead.
		// Setting to "0" prevents the auditor from creating new vulnerabilities.
		h.Set("X-XSS-Protection", "0")

		// Control referrer information sent with requests.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Restrict access to powerful browser APIs.
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		// Content Security Policy — restrict resource loading to same origin.
		// API server only serves JSON, so this is conservative.
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")

		// HSTS — enforce HTTPS for future requests.
		// Only set when the connection is actually over TLS (direct or behind proxy).
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			// max-age=31536000 = 1 year; includeSubDomains for full coverage
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}
