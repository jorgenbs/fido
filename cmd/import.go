package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jorgenbs/fido/internal/config"
	"github.com/jorgenbs/fido/internal/datadog"
	"github.com/jorgenbs/fido/internal/reports"
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

		var ddClients []*datadog.Client
		for i := range cfg.Datadog {
			dd := &cfg.Datadog[i]
			c, err := datadog.NewClient(datadog.ClientConfig{
				Token: dd.Token, APIKey: dd.APIKey, AppKey: dd.AppKey,
				Site: dd.Site, OrgSubdomain: dd.OrgSubdomain,
			})
			if err != nil {
				return err
			}
			c.SetVerbose(verbose)
			ddClients = append(ddClients, c)
		}

		if err := runImport(issueID, cfg, ddClients, mgr); err != nil {
			return err
		}
		fmt.Printf("Successfully imported issue %s\n", issueID)
		return nil
	},
}

func runImport(issueID string, cfg *config.Config, ddClients []*datadog.Client, mgr *reports.Manager) error {
	if mgr.Exists(issueID) {
		return fmt.Errorf("issue %s is already imported", issueID)
	}

	// Search across all Datadog configs to find the issue.
	// Each client searches its own config's services.
	var found *datadog.ErrorIssue
	for i, ddClient := range ddClients {
		issues, err := ddClient.SearchErrorIssues(cfg.Datadog[i].Services, "8760h")
		if err != nil {
			return fmt.Errorf("searching Datadog (%s): %w", cfg.Datadog[i].Name, err)
		}
		for j := range issues {
			if issues[j].ID == issueID {
				found = &issues[j]
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return fmt.Errorf("issue %s not found on Datadog (searched services: %v)", issueID, cfg.Datadog.AllServices())
	}

	service := found.Attributes.Service
	if _, ok := cfg.Repositories[service]; !ok {
		return fmt.Errorf("service %q is not configured in repositories — add it to your config.yml", service)
	}

	// Determine which DD config owns this service for URL building
	var ddOrg, ddSite string
	if dd := cfg.Datadog.ForService(service); dd != nil {
		ddOrg, ddSite = dd.OrgSubdomain, dd.Site
	} else if len(cfg.Datadog) > 0 {
		ddOrg, ddSite = cfg.Datadog[0].OrgSubdomain, cfg.Datadog[0].Site
	}

	// Build URLs
	eventsURL := buildEventsURL(ddOrg, ddSite, found.Attributes.Service, found.Attributes.Env, found.Attributes.FirstSeen, found.Attributes.LastSeen)
	tracesURL := buildTracesURL(ddOrg, ddSite, found.Attributes.Service, found.Attributes.Env, found.Attributes.FirstSeen, found.Attributes.LastSeen)
	datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", ddOrg, ddSite, found.ID)

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
		FirstSeenVersion: found.Attributes.FirstSeenVersion,
		LastSeenVersion:  found.Attributes.LastSeenVersion,
	}
	if err := mgr.WriteMetadata(found.ID, meta); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(importCmd)
}
