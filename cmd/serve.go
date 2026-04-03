package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ruter-as/fido/internal/api"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/ruter-as/fido/internal/syncer"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetString("port")
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		ddClient, err := datadog.NewClient(
			cfg.Datadog.Token,
			cfg.Datadog.Site,
			cfg.Datadog.OrgSubdomain,
		)
		if err != nil {
			return err
		}
		ddClient.SetVerbose(verbose)

		hub := api.NewHub()

		server := api.NewServer(mgr, cfg, hub)
		handlers := api.GetHandlers(server)
		handlers.SetScanFunc(func() error {
			count, err := runScan(cfg, ddClient, mgr)
			if err != nil {
				return err
			}
			fmt.Printf("API scan complete: %d new issues\n", count)
			return nil
		})
		handlers.SetInvestigateFunc(func(issueID string, progress io.Writer) error {
			service := ""
			if meta, err := mgr.ReadMetadata(issueID); err == nil {
				service = meta.Service
			}
			if service == "" {
				errorContent, _ := mgr.ReadError(issueID)
				service = extractServiceFromReport(errorContent)
			}
			return runInvestigate(issueID, service, cfg, mgr, ddClient, progress)
		})
		handlers.SetFixFunc(func(issueID string, iterate bool, progress io.Writer) error {
			service := ""
			if meta, err := mgr.ReadMetadata(issueID); err == nil {
				service = meta.Service
			}
			if service == "" {
				errorContent, _ := mgr.ReadError(issueID)
				service = extractServiceFromReport(errorContent)
			}
			if iterate {
				return runFixIterate(issueID, service, cfg, mgr, progress)
			}
			return runFix(issueID, service, cfg, mgr, progress)
		})

		// Start sync engine in background
		intervalStr := cfg.Scan.Interval
		if intervalStr == "" {
			intervalStr = "15m"
		}
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return fmt.Errorf("invalid scan interval %q: %w", intervalStr, err)
		}

		rateLimit := cfg.Scan.RateLimit
		if rateLimit <= 0 {
			rateLimit = 30
		}

		// Run initial scan synchronously to validate config/credentials
		fmt.Println("Running initial scan...")
		count, _, scanErr := runScanWithResults(cfg, ddClient, mgr)
		if scanErr != nil {
			return fmt.Errorf("initial scan failed (check your Datadog token): %w", scanErr)
		}
		fmt.Printf("Initial scan complete: %d new issues\n", count)
		hub.Publish(api.Event{Type: "scan:complete", Payload: map[string]any{"count": count}})

		adapter := syncer.NewAdapter(ddClient, mgr, hub, func() ([]syncer.ScanResult, error) {
			c, results, err := runScanWithResults(cfg, ddClient, mgr)
			if err != nil {
				return nil, err
			}
			fmt.Printf("Background scan complete: %d new issues\n", c)
			return results, nil
		})

		engine := syncer.NewEngine(adapter, syncer.EngineConfig{
			Interval:  interval,
			Window:    cfg.Scan.Since,
			RateLimit: rateLimit,
		})

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		go func() {
			fmt.Printf("Sync engine started (interval: %s, rate limit: %d/min)\n", interval, rateLimit)
			engine.Run(ctx)
		}()

		fmt.Printf("Fido server listening on :%s\n", port)

		srv := &http.Server{Addr: ":" + port, Handler: server}
		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()
		return srv.ListenAndServe()
	},
}

func init() {
	serveCmd.Flags().String("port", "8080", "port to listen on")
	rootCmd.AddCommand(serveCmd)
}
