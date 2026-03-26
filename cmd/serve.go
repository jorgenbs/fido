package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

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
		)
		if err != nil {
			return err
		}
		ddClient.SetVerbose(verbose)

		server := api.NewServer(mgr, cfg)
		handlers := api.GetHandlers(server)
		handlers.SetScanFunc(func() error {
			count, err := runScan(cfg, ddClient, mgr)
			if err != nil {
				return err
			}
			fmt.Printf("API scan complete: %d new issues\n", count)
			return nil
		})
		handlers.SetInvestigateFunc(func(issueID string) error {
			service := ""
			if meta, err := mgr.ReadMetadata(issueID); err == nil {
				service = meta.Service
			}
			if service == "" {
				errorContent, _ := mgr.ReadError(issueID)
				service = extractServiceFromReport(errorContent)
			}
			return runInvestigate(issueID, service, cfg, mgr)
		})
		handlers.SetFixFunc(func(issueID string) error {
			service := ""
			if meta, err := mgr.ReadMetadata(issueID); err == nil {
				service = meta.Service
			}
			if service == "" {
				errorContent, _ := mgr.ReadError(issueID)
				service = extractServiceFromReport(errorContent)
			}
			return runFix(issueID, service, cfg, mgr)
		})

		fmt.Printf("Fido API server listening on :%s\n", port)
		return http.ListenAndServe(":"+port, server)
	},
}

func init() {
	serveCmd.Flags().String("port", "8080", "port to listen on")
	rootCmd.AddCommand(serveCmd)
}
