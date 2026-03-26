package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run fido scan on a recurring interval",
	RunE: func(cmd *cobra.Command, args []string) error {
		intervalStr, _ := cmd.Flags().GetString("interval")
		if intervalStr == "" {
			intervalStr = cfg.Scan.Interval
		}

		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return fmt.Errorf("invalid interval %q: %w", intervalStr, err)
		}

		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)
		ddClient := datadog.NewClient(
			cfg.Datadog.Token,
			fmt.Sprintf("https://api.%s", cfg.Datadog.Site),
		)

		scanFn := func() error {
			count, err := runScan(cfg, ddClient, mgr)
			if err != nil {
				fmt.Printf("Scan error: %v\n", err)
				return err
			}
			fmt.Printf("Scan complete: %d new issues\n", count)
			return nil
		}

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		fmt.Printf("Fido daemon started (interval: %s)\n", interval)
		runDaemonLoop(ctx, interval, scanFn)
		fmt.Println("Fido daemon stopped")
		return nil
	},
}

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

func init() {
	daemonCmd.Flags().String("interval", "", "scan interval (default: config value)")
	rootCmd.AddCommand(daemonCmd)
}
