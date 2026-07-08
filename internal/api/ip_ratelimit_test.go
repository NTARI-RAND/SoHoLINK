package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// handlerReturning always writes the given status.
func handlerReturning(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	})
}

func doReq(h http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/operators/apply", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A stream of 401s from one IP eventually gets throttled (429), and NEVER a
// permanent lockout: the throttle answers 429, not a silent block, and the
// bucket refills over time.
func TestAuthFailureLimiter_ThrottlesRepeated401(t *testing.T) {
	l := newAuthFailureLimiter(3, 0.2) // burst 3
	h := l.Wrap(handlerReturning(http.StatusUnauthorized))

	// First `burst` (3) requests reach the handler and get 401.
	for i := 0; i < 3; i++ {
		rec := doReq(h, "203.0.113.5:1000")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: got %d, want 401", i, rec.Code)
		}
	}
	// The next request is over budget: 429 with Retry-After, handler NOT invoked.
	rec := doReq(h, "203.0.113.5:1000")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-budget request: got %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on 429")
	}
}

// A SUCCESSFUL request is never counted against the budget and never throttled —
// the limiter charges only 401s.
func TestAuthFailureLimiter_SuccessNeverThrottled(t *testing.T) {
	l := newAuthFailureLimiter(2, 0.2)
	h := l.Wrap(handlerReturning(http.StatusOK))

	for i := 0; i < 50; i++ {
		rec := doReq(h, "203.0.113.9:2000")
		if rec.Code != http.StatusOK {
			t.Fatalf("success request %d throttled: got %d, want 200", i, rec.Code)
		}
	}
}

// The budget REFILLS: after exhausting it, advancing the clock restores capacity.
// This is the "never a lockout" property — the shared-egress fleet always recovers.
func TestAuthFailureLimiter_Refills(t *testing.T) {
	l := newAuthFailureLimiter(2, 1.0) // 1 token/sec
	now := time.Unix(1_700_000_000, 0)
	l.now = func() time.Time { return now }
	h := l.Wrap(handlerReturning(http.StatusUnauthorized))

	// Spend the budget (2), then get throttled on the 3rd.
	doReq(h, "198.51.100.7:3000")
	doReq(h, "198.51.100.7:3000")
	if rec := doReq(h, "198.51.100.7:3000"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after budget spent, got %d", rec.Code)
	}
	// Advance 2 seconds: 2 tokens refill, capacity restored.
	now = now.Add(2 * time.Second)
	if rec := doReq(h, "198.51.100.7:3000"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after refill expected handler reached (401), got %d", rec.Code)
	}
}

// Two different IPs have independent budgets — one IP's failures never throttle
// another. (The shared-egress caveat is about a hard block being unacceptable;
// per-IP independence is still correct behavior when sources differ.)
func TestAuthFailureLimiter_PerIPIndependent(t *testing.T) {
	l := newAuthFailureLimiter(1, 0.2)
	h := l.Wrap(handlerReturning(http.StatusUnauthorized))

	// Exhaust IP A.
	doReq(h, "203.0.113.1:100")
	if rec := doReq(h, "203.0.113.1:100"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A should be throttled, got %d", rec.Code)
	}
	// IP B is unaffected.
	if rec := doReq(h, "203.0.113.2:100"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("IP B should reach handler, got %d", rec.Code)
	}
}

// X-Forwarded-For leftmost is used for bucketing (portal sits behind a proxy).
func TestClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "10.0.0.1:5000"
	req.Header.Set("X-Forwarded-For", "203.0.113.44, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.44" {
		t.Fatalf("clientIP = %q, want 203.0.113.44", got)
	}
}
