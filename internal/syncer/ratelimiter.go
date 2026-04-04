// internal/syncer/ratelimiter.go
package syncer

import (
	"context"
	"sync"
	"time"
)

// RateLimiter implements a rate limiter that adapts to server-reported limits.
// It starts with a conservative default and adjusts when it receives feedback
// from actual API response headers.
type RateLimiter struct {
	mu         sync.Mutex
	remaining  int           // requests remaining in current period (from server)
	resetAt    time.Time     // when the current period resets
	limit      int           // max requests per period (from server)
	period     time.Duration // period duration (from server)
	configured bool          // true once we've received server headers
}

// NewRateLimiter creates a rate limiter with a conservative default.
// The actual limit will be learned from response headers via Update().
func NewRateLimiter(defaultPerMinute int) *RateLimiter {
	if defaultPerMinute <= 0 {
		panic("syncer: maxPerMinute must be positive")
	}
	return &RateLimiter{
		remaining: 1, // start conservative: allow one request to learn the real limit
		resetAt:   time.Now().Add(time.Minute), // don't reset until we hear from the server
		limit:     1,
		period:    time.Minute,
	}
}

// Update feeds server-reported rate limit state into the limiter.
func (r *RateLimiter) Update(limit, remaining int, period, reset time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limit = limit
	r.remaining = remaining
	r.period = period
	r.resetAt = time.Now().Add(reset)
	r.configured = true
}

// TryAcquire attempts to consume one request slot. Returns false if none available.
func (r *RateLimiter) TryAcquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkReset()
	if r.remaining > 0 {
		r.remaining--
		return true
	}
	return false
}

// Wait blocks until a request slot is available, then consumes it.
func (r *RateLimiter) Wait() {
	r.WaitContext(context.Background()) //nolint:errcheck
}

// WaitContext blocks until a request slot is available or ctx is cancelled.
func (r *RateLimiter) WaitContext(ctx context.Context) error {
	for {
		r.mu.Lock()
		r.checkReset()
		if r.remaining > 0 {
			r.remaining--
			r.mu.Unlock()
			return nil
		}
		wait := time.Until(r.resetAt)
		if wait <= 0 {
			wait = time.Second // safety floor
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// checkReset checks if the current period has expired and refills tokens.
// Must be called with mu held.
func (r *RateLimiter) checkReset() {
	if time.Now().After(r.resetAt) {
		r.remaining = r.limit
		r.resetAt = time.Now().Add(r.period)
	}
}
