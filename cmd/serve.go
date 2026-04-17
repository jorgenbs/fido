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

	"github.com/jorgenbs/fido/internal/api"
	"github.com/jorgenbs/fido/internal/config"
	"github.com/jorgenbs/fido/internal/datadog"
	"github.com/jorgenbs/fido/internal/reports"
	"github.com/jorgenbs/fido/internal/syncer"
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

		if len(cfg.Datadog) == 0 {
			return fmt.Errorf("no datadog config found")
		}

		// Build a client for each unique Datadog config and a service→client map.
		type ddEntry struct {
			cfg    *config.DatadogConfig
			client *datadog.Client
		}
		var ddEntries []ddEntry
		clientMap := make(map[string]*datadog.Client) // service → client

		for i := range cfg.Datadog {
			ddCfg := &cfg.Datadog[i]
			client, err := datadog.NewClient(ddCfg.Token, ddCfg.Site, ddCfg.OrgSubdomain)
			if err != nil {
				return fmt.Errorf("creating Datadog client for %q: %w", ddCfg.Name, err)
			}
			client.SetVerbose(verbose)
			ddEntries = append(ddEntries, ddEntry{cfg: ddCfg, client: client})
			for _, svc := range ddCfg.Services {
				clientMap[svc] = client
			}
		}

		defaultClient := ddEntries[0].client

		resolveClient := syncer.ClientResolver(func(service string) *datadog.Client {
			if c, ok := clientMap[service]; ok {
				return c
			}
			return defaultClient
		})

		hub := api.NewHub()

		server := api.NewServer(mgr, cfg, hub)
		handlers := api.GetHandlers(server)
		handlers.SetScanFunc(func() error {
			totalCount := 0
			for _, entry := range ddEntries {
				if len(entry.cfg.Services) == 0 {
					continue
				}
				count, err := runScan(cfg, entry.cfg, entry.client, mgr)
				if err != nil {
					return err
				}
				totalCount += count
			}
			fmt.Printf("API scan complete: %d new issues\n", totalCount)
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
			return runInvestigate(issueID, service, cfg, mgr, resolveClient(service), progress)
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
		totalCount := 0
		for _, entry := range ddEntries {
			if len(entry.cfg.Services) == 0 {
				continue
			}
			count, _, scanErr := runScanWithResults(cfg, entry.cfg, entry.client, mgr)
			if scanErr != nil {
				return fmt.Errorf("initial scan failed for %q (check your Datadog token): %w", entry.cfg.Name, scanErr)
			}
			totalCount += count
		}
		fmt.Printf("Initial scan complete: %d issues updated\n", totalCount)
		hub.Publish(api.Event{Type: "scan:complete", Payload: map[string]any{"count": totalCount}})

		adapter := syncer.NewAdapter(resolveClient, defaultClient, mgr, hub, func() ([]syncer.ScanResult, error) {
			var allResults []syncer.ScanResult
			totalCount := 0
			for _, entry := range ddEntries {
				if len(entry.cfg.Services) == 0 {
					continue
				}
				c, results, err := runScanWithResults(cfg, entry.cfg, entry.client, mgr)
				if err != nil {
					return nil, err
				}
				totalCount += c
				allResults = append(allResults, results...)
			}
			fmt.Printf("Background scan complete: %d issues updated\n", totalCount)
			return allResults, nil
		})

		engine := syncer.NewEngine(adapter, syncer.EngineConfig{
			Interval:  interval,
			RateLimit: rateLimit,
		})

		// Wire import to enqueue stacktrace fetch immediately
		allClients := make([]*datadog.Client, 0, len(ddEntries))
		for _, entry := range ddEntries {
			allClients = append(allClients, entry.client)
		}
		handlers.SetImportFunc(func(issueID string) error {
			if err := runImport(issueID, cfg, allClients, mgr); err != nil {
				return err
			}
			// Enqueue stacktrace fetch in the sync engine
			meta, _ := mgr.ReadMetadata(issueID)
			if meta != nil {
				engine.EnqueueIssue(syncer.ScanResult{
					IssueID:   issueID,
					Service:   meta.Service,
					Env:       meta.Env,
					FirstSeen: meta.FirstSeen,
					LastSeen:  meta.LastSeen,
				})
			}
			return nil
		})

		// Wire Datadog response headers into the engine's rate limiter.
		// Deduplicate clients (multiple services may share one client).
		limiter := engine.Limiter()
		seenClients := make(map[*datadog.Client]bool)
		for _, entry := range ddEntries {
			if seenClients[entry.client] {
				continue
			}
			seenClients[entry.client] = true
			entry.client.SetRateLimitCallback(func(info datadog.RateLimitInfo) {
				limiter.Update(
					info.Limit,
					info.Remaining,
					time.Duration(info.Period)*time.Second,
					info.Reset,
				)
			})
		}

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		go func() {
			fmt.Printf("Sync engine started (interval: %s, rate limit: adaptive from Datadog headers)\n", interval)
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
