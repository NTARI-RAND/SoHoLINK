package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// This file implements per-IP rate limiting for the PUBLIC operator endpoints,
// scoped narrowly to AUTHENTICATION FAILURES (401 responses). It exists so a
// single source cannot cheaply brute-force operator auth or hammer the onboarding
// funnel, without ever locking out a legitimate operator.
//
// CRITICAL DESIGN CONSTRAINT (task + design §13 item on per-IP limiting):
// the limiter is NEVER an IP-scoped LOCKOUT. Cloudy's whole fleet shares ONE
// egress IP, so a hard block on an IP would deny the entire fleet on the strength
// of a few bad requests from one misbehaving member. Instead the limiter is a
// soft throttle: once an IP exceeds its 401 budget it receives 429 with a
// Retry-After, the bucket REFILLS over time (so the IP always recovers on its
// own), and a SUCCESSFUL request is never throttled and never counted against the
// budget. There is no permanent state, no manual "clear lockout", and no denial
// of a well-formed authenticated request.

// authFailureLimiter is a per-IP token-bucket keyed to 401 responses. Each IP
// starts with `burst` tokens; each 401 consumes one; tokens refill at `refill`
// per second up to `burst`. When an IP has no tokens, further requests from it
// are answered 429 BEFORE reaching the wrapped handler — but only 401-producing
// traffic ever depletes tokens, so ordinary and successful traffic is unaffected.
//
// The limiter is safe for concurrent use. It is deliberately in-memory and
// process-local: the :8090 governance surface and the public portal are separate
// processes, and this guards the PUBLIC endpoints only.
type authFailureLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	burst   float64 // maximum tokens (initial budget of 401s before throttling)
	refill  float64 // tokens added per second
	now     func() time.Time
}

// tokenBucket is one IP's replenishing 401 budget.
type tokenBucket struct {
	tokens   float64
	lastSeen time.Time
}

// newAuthFailureLimiter constructs a limiter. burst is the number of 401s an IP
// may incur before being throttled; refillPerSec is how fast the budget recovers.
// Sensible pilot defaults: burst=10, refillPerSec=0.2 (one token every 5s, full
// recovery from empty in ~50s). Never a lockout: the bucket always refills.
func newAuthFailureLimiter(burst, refillPerSec float64) *authFailureLimiter {
	if burst <= 0 {
		burst = 10
	}
	if refillPerSec <= 0 {
		refillPerSec = 0.2
	}
	return &authFailureLimiter{
		buckets: make(map[string]*tokenBucket),
		burst:   burst,
		refill:  refillPerSec,
		now:     time.Now,
	}
}

// allow reports whether the IP currently has budget to make another request that
// MIGHT 401. It refills the bucket based on elapsed time but does NOT consume a
// token here — a token is consumed only when the wrapped handler actually
// produces a 401 (see Wrap). Returns the seconds until at least one token is
// available (for Retry-After) when the IP is over budget.
func (l *authFailureLimiter) allow(ip string) (ok bool, retryAfter int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refillLocked(ip)
	if b.tokens >= 1 {
		return true, 0
	}
	// Over budget: compute when one token will be available.
	need := 1 - b.tokens
	secs := int(need/l.refill) + 1
	if secs < 1 {
		secs = 1
	}
	return false, secs
}

// penalize consumes one token from the IP's bucket. Called only after the wrapped
// handler produced a 401, so ordinary/successful traffic is never charged.
func (l *authFailureLimiter) penalize(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refillLocked(ip)
	b.tokens--
	if b.tokens < 0 {
		b.tokens = 0
	}
}

// refillLocked returns the IP's bucket, creating it (full) on first sight and
// crediting elapsed-time refill up to burst. Caller holds l.mu.
func (l *authFailureLimiter) refillLocked(ip string) *tokenBucket {
	now := l.now()
	b, ok := l.buckets[ip]
	if !ok {
		b = &tokenBucket{tokens: l.burst, lastSeen: now}
		l.buckets[ip] = b
		return b
	}
	elapsed := now.Sub(b.lastSeen).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.refill
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.lastSeen = now
	}
	return b
}

// Wrap returns a handler that applies the per-IP 401 budget in front of next.
// When the IP is over budget it answers 429 with Retry-After WITHOUT invoking
// next (cheap rejection of a brute-forcing source). Otherwise it invokes next and
// charges a token only if next produced a 401. It NEVER blocks a successful
// request and NEVER holds permanent state — the budget always refills.
func (l *authFailureLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

		if ok, retryAfter := l.allow(ip); !ok {
			w.Header().Set("Retry-After", itoa(retryAfter))
			// Soft throttle, not a lockout: the caller may retry once the budget
			// refills. A well-formed authenticated request from the same IP is
			// only ever delayed by this window, never permanently denied.
			writeError(w, http.StatusTooManyRequests, "too many failed authentication attempts; retry after the indicated delay")
			return
		}

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		if rec.status == http.StatusUnauthorized {
			l.penalize(ip)
		}
	})
}

// statusRecorder captures the response status so Wrap can tell whether next
// produced a 401 without buffering the body.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// An implicit 200 (handler wrote body without WriteHeader).
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// clientIP extracts the caller's IP for bucketing. It prefers the leftmost
// X-Forwarded-For entry (the public portal sits behind NGINX/Cloudflare), then
// X-Real-IP, then the transport RemoteAddr. Bucketing by IP is intentionally
// coarse — the whole point is that the shared-egress fleet is NEVER hard-blocked,
// only softly throttled and always allowed to recover.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Leftmost is the original client.
		if i := indexByte(xff, ','); i >= 0 {
			return trimSpace(xff[:i])
		}
		return trimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return trimSpace(xr)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// The three tiny string helpers below avoid pulling in strconv/strings solely
// for one call each in this file's hot path.

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
