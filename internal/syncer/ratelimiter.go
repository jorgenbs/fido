// internal/syncer/ratelimiter.go
package syncer

import (
	"context"
	"sync"
	"time"
)

// RateLimiter implements a token-bucket rate limiter.
// Tokens refill at a steady rate up to maxPerMinute.
type RateLimiter struct {
	mu         sync.Mutex
	tokens     float64
	max        float64
	refillRate float64 // tokens per nanosecond
	lastRefill time.Time
}

// NewRateLimiter creates a rate limiter allowing maxPerMinute requests.
// Panics if maxPerMinute is not positive.
func NewRateLimiter(maxPerMinute int) *RateLimiter {
	if maxPerMinute <= 0 {
		panic("syncer: maxPerMinute must be positive")
	}
	max := float64(maxPerMinute)
	return &RateLimiter{
		tokens:     max,
		max:        max,
		refillRate: max / float64(time.Minute),
		lastRefill: time.Now(),
	}
}

func (r *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(r.lastRefill)
	r.tokens += float64(elapsed) * r.refillRate
	if r.tokens > r.max {
		r.tokens = r.max
	}
	r.lastRefill = now
}

// TryAcquire attempts to consume one token. Returns false if none available.
func (r *RateLimiter) TryAcquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refill()
	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}

// Wait blocks until a token is available, then consumes it.
func (r *RateLimiter) Wait() {
	r.WaitContext(context.Background()) //nolint:errcheck
}

// WaitContext blocks until a token is available or ctx is cancelled.
func (r *RateLimiter) WaitContext(ctx context.Context) error {
	for {
		r.mu.Lock()
		r.refill()
		if r.tokens >= 1 {
			r.tokens--
			r.mu.Unlock()
			return nil
		}
		deficit := 1 - r.tokens
		wait := time.Duration(deficit / r.refillRate)
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
