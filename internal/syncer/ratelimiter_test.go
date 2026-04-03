// internal/syncer/ratelimiter_test.go
package syncer

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowsUpToMaxPerMinute(t *testing.T) {
	rl := NewRateLimiter(5) // 5 per minute

	allowed := 0
	for i := 0; i < 10; i++ {
		if rl.TryAcquire() {
			allowed++
		}
	}

	if allowed != 5 {
		t.Errorf("expected 5 allowed, got %d", allowed)
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	rl := NewRateLimiter(60) // 60 per minute = 1 per second

	// Drain all tokens
	for rl.TryAcquire() {
	}

	// Wait for one refill
	time.Sleep(1100 * time.Millisecond)

	if !rl.TryAcquire() {
		t.Error("expected token to be available after refill")
	}
}

func TestRateLimiter_WaitBlocks(t *testing.T) {
	rl := NewRateLimiter(600) // 10 per second

	// Drain all tokens
	for rl.TryAcquire() {
	}

	start := time.Now()
	rl.Wait()
	elapsed := time.Since(start)

	if elapsed < 90*time.Millisecond {
		t.Errorf("Wait should have blocked, elapsed: %v", elapsed)
	}
}
