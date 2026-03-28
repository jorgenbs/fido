package cmd

import (
	"context"
	"time"
)

func runDaemonLoop(ctx context.Context, interval time.Duration, scanFn func() error) {
	scanFn()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanFn()
		}
	}
}
