// internal/syncer/ratelimiter_test.go
package syncer

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_StartsConservative(t *testing.T) {
	rl := NewRateLimiter(30)

	// Before any Update(), only 1 request should be allowed (conservative start).
	if !rl.TryAcquire() {
		t.Error("expected first request to be allowed")
	}
	if rl.TryAcquire() {
		t.Error("expected second request to be blocked before server feedback")
	}
}

func TestRateLimiter_UpdateFromHeaders(t *testing.T) {
	rl := NewRateLimiter(30)

	// Simulate server telling us: limit=5, remaining=4, period=60s, reset=50s
	rl.Update(5, 4, 60*time.Second, 50*time.Second)

	allowed := 0
	for i := 0; i < 10; i++ {
		if rl.TryAcquire() {
			allowed++
		}
	}
	if allowed != 4 {
		t.Errorf("expected 4 allowed after Update(remaining=4), got %d", allowed)
	}
}

func TestRateLimiter_ResetsAfterPeriod(t *testing.T) {
	rl := NewRateLimiter(30)

	// Simulate: limit=5, remaining=0, period=60s, reset in 100ms
	rl.Update(5, 0, 60*time.Second, 100*time.Millisecond)

	if rl.TryAcquire() {
		t.Error("expected request to be blocked when remaining=0")
	}

	// Wait for reset
	time.Sleep(150 * time.Millisecond)

	if !rl.TryAcquire() {
		t.Error("expected request to be allowed after reset")
	}
}

func TestRateLimiter_WaitContextRespectsCancel(t *testing.T) {
	rl := NewRateLimiter(30)

	// Simulate: remaining=0, reset far in the future
	rl.Update(5, 0, 60*time.Second, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := rl.WaitContext(ctx)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestRateLimiter_WaitBlocksUntilReset(t *testing.T) {
	rl := NewRateLimiter(30)

	// Simulate: remaining=0, reset in 100ms
	rl.Update(5, 0, 60*time.Second, 100*time.Millisecond)

	start := time.Now()
	rl.Wait()
	elapsed := time.Since(start)

	if elapsed < 80*time.Millisecond {
		t.Errorf("Wait should have blocked ~100ms, elapsed: %v", elapsed)
	}
}

func TestRateLimiter_PanicsOnZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on maxPerMinute=0")
		}
	}()
	NewRateLimiter(0)
}
