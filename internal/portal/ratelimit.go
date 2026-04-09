package portal

import (
	"sync"
	"time"
)

// attemptRecord tracks failed login attempts for a single IP within a window.
type attemptRecord struct {
	Count       int
	WindowStart time.Time
}

// LoginRateLimiter tracks failed login attempts per IP and enforces a max
// attempts limit within a sliding window. Safe for concurrent use.
type LoginRateLimiter struct {
	m           sync.Map
	maxAttempts int
	window      time.Duration
}

// NewLoginRateLimiter returns a LoginRateLimiter that allows maxAttempts
// failures per IP within the given window before blocking further attempts.
func NewLoginRateLimiter(maxAttempts int, window time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{maxAttempts: maxAttempts, window: window}
}

// Allow returns false if the IP has exceeded maxAttempts failures within the
// current window. If the window has expired it resets the counter and allows.
func (rl *LoginRateLimiter) Allow(ip string) bool {
	v, ok := rl.m.Load(ip)
	if !ok {
		return true
	}
	rec := v.(attemptRecord)
	if time.Since(rec.WindowStart) > rl.window {
		rl.m.Delete(ip)
		return true
	}
	return rec.Count < rl.maxAttempts
}

// RecordFailure increments the failure count for the IP. If no record exists
// or the window has expired, it starts a fresh window.
func (rl *LoginRateLimiter) RecordFailure(ip string) {
	now := time.Now()
	v, loaded := rl.m.LoadOrStore(ip, attemptRecord{Count: 1, WindowStart: now})
	if !loaded {
		return
	}
	rec := v.(attemptRecord)
	if time.Since(rec.WindowStart) > rl.window {
		rec = attemptRecord{Count: 1, WindowStart: now}
	} else {
		rec.Count++
	}
	rl.m.Store(ip, rec)
}

// Reset clears the rate-limit record for the IP. Called on successful login.
func (rl *LoginRateLimiter) Reset(ip string) {
	rl.m.Delete(ip)
}
