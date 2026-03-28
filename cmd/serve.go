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

		// Start daemon scanner in background
		intervalStr := cfg.Scan.Interval
		if intervalStr == "" {
			intervalStr = "15m"
		}
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return fmt.Errorf("invalid scan interval %q: %w", intervalStr, err)
		}

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		go func() {
			fmt.Printf("Background scanner started (interval: %s)\n", interval)
			scanFn := func() error {
				count, scanErr := runScan(cfg, ddClient, mgr)
				if scanErr != nil {
					fmt.Printf("Background scan error: %v\n", scanErr)
					return scanErr
				}
				fmt.Printf("Background scan complete: %d new issues\n", count)
				hub.Publish(api.Event{Type: "scan:complete", Payload: map[string]any{"count": count}})
				return nil
			}
			runDaemonLoop(ctx, interval, scanFn)
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
