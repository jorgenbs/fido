package cmd

import (
	"context"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"15m", 15 * time.Minute},
		{"1h", 1 * time.Hour},
		{"30s", 30 * time.Second},
	}

	for _, tt := range tests {
		d, err := time.ParseDuration(tt.input)
		if err != nil {
			t.Errorf("failed to parse %q: %v", tt.input, err)
		}
		if d != tt.expected {
			t.Errorf("expected %v, got %v", tt.expected, d)
		}
	}
}

func TestDaemonLoop_RunsAndCancels(t *testing.T) {
	callCount := 0
	scanFn := func() error {
		callCount++
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	runDaemonLoop(ctx, 50*time.Millisecond, scanFn)

	if callCount < 2 {
		t.Errorf("expected at least 2 scan calls, got %d", callCount)
	}
}
