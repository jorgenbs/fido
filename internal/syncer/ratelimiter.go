// internal/syncer/ratelimiter.go
package syncer

import (
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
func NewRateLimiter(maxPerMinute int) *RateLimiter {
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
	for {
		r.mu.Lock()
		r.refill()
		if r.tokens >= 1 {
			r.tokens--
			r.mu.Unlock()
			return
		}
		// Calculate wait time for next token
		deficit := 1 - r.tokens
		wait := time.Duration(deficit / r.refillRate)
		r.mu.Unlock()
		time.Sleep(wait)
	}
}
