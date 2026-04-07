package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import <issue-id>",
	Short: "Import a Datadog error tracking issue into Fido",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
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

		if err := runImport(issueID, cfg, ddClient, mgr); err != nil {
			return err
		}
		fmt.Printf("Successfully imported issue %s\n", issueID)
		return nil
	},
}

func runImport(issueID string, cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) error {
	if mgr.Exists(issueID) {
		return fmt.Errorf("issue %s is already imported", issueID)
	}

	// Fetch all issues for configured services, then find the one we want.
	// The Datadog error tracking API searches by service, not by issue ID directly.
	issues, err := ddClient.SearchErrorIssues(cfg.Datadog.Services, cfg.Scan.Since)
	if err != nil {
		return fmt.Errorf("searching Datadog: %w", err)
	}

	var found *datadog.ErrorIssue
	for i := range issues {
		if issues[i].ID == issueID {
			found = &issues[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("issue %s not found on Datadog (searched services: %v)", issueID, cfg.Datadog.Services)
	}

	service := found.Attributes.Service
	if _, ok := cfg.Repositories[service]; !ok {
		return fmt.Errorf("service %q is not configured in repositories — add it to your config.yml", service)
	}

	// Build URLs
	eventsURL := buildEventsURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, found.Attributes.Service, found.Attributes.Env, found.Attributes.FirstSeen, found.Attributes.LastSeen)
	tracesURL := buildTracesURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, found.Attributes.Service, found.Attributes.Env, found.Attributes.FirstSeen, found.Attributes.LastSeen)
	datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, found.ID)

	// Render error report using existing template
	tmpl, err := loadErrorTemplate()
	if err != nil {
		return fmt.Errorf("loading template: %w", err)
	}

	data := errorReportData{
		ID:         found.ID,
		Title:      found.Attributes.Title,
		Message:    found.Attributes.Message,
		Service:    found.Attributes.Service,
		Env:        found.Attributes.Env,
		FirstSeen:  found.Attributes.FirstSeen,
		LastSeen:   found.Attributes.LastSeen,
		Count:      found.Attributes.Count,
		StackTrace: found.Attributes.StackTrace,
		DatadogURL: datadogURL,
		EventsURL:  eventsURL,
		TracesURL:  tracesURL,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("rendering error report: %w", err)
	}

	if err := mgr.WriteError(found.ID, buf.String()); err != nil {
		return fmt.Errorf("writing error report: %w", err)
	}

	meta := &reports.MetaData{
		Title:            found.Attributes.Title,
		Message:          found.Attributes.Message,
		Service:          found.Attributes.Service,
		Env:              found.Attributes.Env,
		FirstSeen:        found.Attributes.FirstSeen,
		LastSeen:         found.Attributes.LastSeen,
		Count:            found.Attributes.Count,
		DatadogURL:       datadogURL,
		DatadogEventsURL: eventsURL,
		DatadogTraceURL:  tracesURL,
	}
	if err := mgr.WriteMetadata(found.ID, meta); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(importCmd)
}
