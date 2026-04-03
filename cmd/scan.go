package cmd

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/gitlab"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/ruter-as/fido/internal/syncer"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan Datadog for new error issues",
	RunE: func(cmd *cobra.Command, args []string) error {
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

		services, _ := cmd.Flags().GetStringSlice("service")
		if len(services) == 0 {
			services = cfg.Datadog.Services
		}

		since, _ := cmd.Flags().GetString("since")
		if since == "" {
			since = cfg.Scan.Since
		}

		scanCfg := &config.Config{
			Datadog:      config.DatadogConfig{Services: services, Site: cfg.Datadog.Site, OrgSubdomain: cfg.Datadog.OrgSubdomain},
			Scan:         config.ScanConfig{Since: since},
			Repositories: cfg.Repositories,
		}

		count, err := runScan(scanCfg, ddClient, mgr)
		if err != nil {
			return err
		}
		fmt.Printf("Found %d new error issues\n", count)
		return nil
	},
}

type errorReportData struct {
	ID         string
	Title      string
	Message    string
	Service    string
	Env        string
	FirstSeen  string
	LastSeen   string
	Count      int64
	Status     string
	StackTrace string
	Logs       []datadog.LogAttributes
	DatadogURL string
	EventsURL  string
	TracesURL  string
}

func runScan(cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) (int, error) {
	issues, err := ddClient.SearchErrorIssues(cfg.Datadog.Services, cfg.Scan.Since)
	if err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}

	tmpl, err := loadErrorTemplate()
	if err != nil {
		return 0, fmt.Errorf("loading template: %w", err)
	}

	count := 0
	for _, issue := range issues {
		eventsURL := buildEventsURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.Attributes.Service, issue.Attributes.Env, issue.Attributes.FirstSeen, issue.Attributes.LastSeen)
		tracesURL := buildTracesURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.Attributes.Service, issue.Attributes.Env, issue.Attributes.FirstSeen, issue.Attributes.LastSeen)
		datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.ID)

		if mgr.Exists(issue.ID) {
			// Update Datadog-sourced fields for existing issues, preserving investigation tags and CI state
			meta, err := mgr.ReadMetadata(issue.ID)
			if err != nil {
				log.Printf("scan: reading meta for %s: %v", issue.ID, err)
				continue
			}
			meta.Title = issue.Attributes.Title
			meta.Message = issue.Attributes.Message
			meta.Service = issue.Attributes.Service
			meta.Env = issue.Attributes.Env
			meta.FirstSeen = issue.Attributes.FirstSeen
			meta.LastSeen = issue.Attributes.LastSeen
			meta.Count = issue.Attributes.Count
			meta.DatadogURL = datadogURL
			meta.DatadogEventsURL = eventsURL
			meta.DatadogTraceURL = tracesURL
			if err := mgr.WriteMetadata(issue.ID, meta); err != nil {
				log.Printf("scan: updating meta for %s: %v", issue.ID, err)
			}
			continue
		}

		data := errorReportData{
			ID:         issue.ID,
			Title:      issue.Attributes.Title,
			Message:    issue.Attributes.Message,
			Service:    issue.Attributes.Service,
			Env:        issue.Attributes.Env,
			FirstSeen:  issue.Attributes.FirstSeen,
			LastSeen:   issue.Attributes.LastSeen,
			Count:      issue.Attributes.Count,
			Status:     issue.Attributes.Status,
			StackTrace: issue.Attributes.StackTrace,
			DatadogURL: datadogURL,
			EventsURL:  eventsURL,
			TracesURL:  tracesURL,
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return count, fmt.Errorf("rendering error report: %w", err)
		}

		if err := mgr.WriteError(issue.ID, buf.String()); err != nil {
			return count, fmt.Errorf("writing error report: %w", err)
		}

		meta := &reports.MetaData{
			Title:            issue.Attributes.Title,
			Message:          issue.Attributes.Message,
			Service:          issue.Attributes.Service,
			Env:              issue.Attributes.Env,
			FirstSeen:        issue.Attributes.FirstSeen,
			LastSeen:         issue.Attributes.LastSeen,
			Count:            issue.Attributes.Count,
			DatadogURL:       datadogURL,
			DatadogEventsURL: eventsURL,
			DatadogTraceURL:  tracesURL,
		}
		if err := mgr.WriteMetadata(issue.ID, meta); err != nil {
			return count, fmt.Errorf("writing metadata: %w", err)
		}
		count++
	}

	refreshCIStatuses(cfg, mgr)

	return count, nil
}

// runScanWithResults runs a scan and returns both the count and structured results
// for the sync engine to enqueue follow-up jobs.
func runScanWithResults(cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) (int, []syncer.ScanResult, error) {
	issues, err := ddClient.SearchErrorIssues(cfg.Datadog.Services, cfg.Scan.Since)
	if err != nil {
		return 0, nil, fmt.Errorf("scan: %w", err)
	}

	tmpl, err := loadErrorTemplate()
	if err != nil {
		return 0, nil, fmt.Errorf("loading template: %w", err)
	}

	var results []syncer.ScanResult
	count := 0

	for _, issue := range issues {
		eventsURL := buildEventsURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.Attributes.Service, issue.Attributes.Env, issue.Attributes.FirstSeen, issue.Attributes.LastSeen)
		tracesURL := buildTracesURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.Attributes.Service, issue.Attributes.Env, issue.Attributes.FirstSeen, issue.Attributes.LastSeen)
		datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.ID)

		hasStack := issue.Attributes.StackTrace != ""

		if mgr.Exists(issue.ID) {
			meta, err := mgr.ReadMetadata(issue.ID)
			if err != nil {
				log.Printf("scan: reading meta for %s: %v", issue.ID, err)
				continue
			}
			meta.Title = issue.Attributes.Title
			meta.Message = issue.Attributes.Message
			meta.Service = issue.Attributes.Service
			meta.Env = issue.Attributes.Env
			meta.FirstSeen = issue.Attributes.FirstSeen
			meta.LastSeen = issue.Attributes.LastSeen
			meta.Count = issue.Attributes.Count
			meta.DatadogURL = datadogURL
			meta.DatadogEventsURL = eventsURL
			meta.DatadogTraceURL = tracesURL
			if err := mgr.WriteMetadata(issue.ID, meta); err != nil {
				log.Printf("scan: updating meta for %s: %v", issue.ID, err)
			}
		} else {
			data := errorReportData{
				ID: issue.ID, Title: issue.Attributes.Title, Message: issue.Attributes.Message,
				Service: issue.Attributes.Service, Env: issue.Attributes.Env,
				FirstSeen: issue.Attributes.FirstSeen, LastSeen: issue.Attributes.LastSeen,
				Count: issue.Attributes.Count, Status: issue.Attributes.Status,
				StackTrace: issue.Attributes.StackTrace,
				DatadogURL: datadogURL, EventsURL: eventsURL, TracesURL: tracesURL,
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err != nil {
				return count, results, fmt.Errorf("rendering error report: %w", err)
			}
			if err := mgr.WriteError(issue.ID, buf.String()); err != nil {
				return count, results, fmt.Errorf("writing error report: %w", err)
			}
			meta := &reports.MetaData{
				Title: issue.Attributes.Title, Message: issue.Attributes.Message,
				Service: issue.Attributes.Service, Env: issue.Attributes.Env,
				FirstSeen: issue.Attributes.FirstSeen, LastSeen: issue.Attributes.LastSeen,
				Count: issue.Attributes.Count, DatadogURL: datadogURL,
				DatadogEventsURL: eventsURL, DatadogTraceURL: tracesURL,
			}
			if err := mgr.WriteMetadata(issue.ID, meta); err != nil {
				return count, results, fmt.Errorf("writing metadata: %w", err)
			}
			count++
		}

		results = append(results, syncer.ScanResult{
			IssueID:       issue.ID,
			Service:       issue.Attributes.Service,
			Env:           issue.Attributes.Env,
			FirstSeen:     issue.Attributes.FirstSeen,
			LastSeen:      issue.Attributes.LastSeen,
			HasStacktrace: hasStack,
		})
	}

	refreshCIStatuses(cfg, mgr)
	return count, results, nil
}

func loadErrorTemplate() (*template.Template, error) {
	const defaultTemplate = `# Error Report: {{.Title}}

**Issue ID:** {{.ID}}
**Service:** {{.Service}}
**Environment:** {{.Env}}
**Status:** {{.Status}}

## Occurrences

- **Count:** {{.Count}}
- **First seen:** {{.FirstSeen}}
- **Last seen:** {{.LastSeen}}

## Error

**Type:** {{.Title}}
**Message:** {{.Message}}

## Stack Trace

{{if .StackTrace}}` + "```" + `
{{.StackTrace}}
` + "```" + `
{{else}}
<!-- STACK_TRACE_PENDING -->
{{end}}

## Surrounding Logs

{{if .Logs}}
{{range .Logs}}
- ` + "`{{.Timestamp}}`" + ` [{{.Status}}] {{.Message}}
{{end}}
{{else}}
_No surrounding logs found_
{{end}}
<!-- TRACES_PENDING -->

## Links

- [Datadog Issue]({{.DatadogURL}})
{{if .EventsURL}}- [Events Timeline]({{.EventsURL}})
{{end}}{{if .TracesURL}}- [Trace Waterfall]({{.TracesURL}})
{{end}}`
	return template.New("error").Parse(defaultTemplate)
}

func buildEventsURL(org, site, service, env, firstSeen, lastSeen string) string {
	from, err := time.Parse(time.RFC3339, firstSeen)
	if err != nil {
		return ""
	}
	to, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return ""
	}
	query := "service:" + service
	if env != "" {
		query += " env:" + env
	}
	return fmt.Sprintf(
		"https://%s.%s/event/explorer?query=%s&from=%d&to=%d",
		org, site, url.QueryEscape(query), from.UnixMilli(), to.UnixMilli(),
	)
}

func buildTracesURL(org, site, service, env, firstSeen, lastSeen string) string {
	from, err := time.Parse(time.RFC3339, firstSeen)
	if err != nil {
		return ""
	}
	to, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return ""
	}
	query := "service:" + service
	if env != "" {
		query += " env:" + env
	}
	return fmt.Sprintf(
		"https://%s.%s/apm/traces?query=%s&start=%d&end=%d",
		org, site, url.QueryEscape(query), from.UnixMilli(), to.UnixMilli(),
	)
}

// refreshCIStatuses updates ci_status and ci_url in meta.json for all issues
// that have a resolve.json (i.e. a branch and MR). Non-fatal: logs and skips on error.
func refreshCIStatuses(cfg *config.Config, mgr *reports.Manager) {
	issues, err := mgr.ListIssues(true) // include ignored
	if err != nil {
		log.Printf("CI refresh: listing issues: %v", err)
		return
	}
	for _, issue := range issues {
		resolve, err := mgr.ReadResolve(issue.ID)
		if err != nil || resolve.Branch == "" {
			continue
		}
		repoPath, err := resolveRepoPath(resolve.Service, cfg)
		if err != nil {
			log.Printf("CI refresh: no repo for service %q (issue %s): %v", resolve.Service, issue.ID, err)
			continue
		}
		mrStatus, ciStatus, ciURL, err := gitlab.FetchMRStatus(resolve.Branch, repoPath)
		if err != nil {
			log.Printf("CI refresh: glab failed for %s: %v", issue.ID, err)
			continue
		}
		meta, err := mgr.ReadMetadata(issue.ID)
		if err != nil {
			log.Printf("CI refresh: reading meta for %s: %v", issue.ID, err)
			continue
		}
		meta.CIStatus = ciStatus
		meta.CIURL = ciURL
		if err := mgr.WriteMetadata(issue.ID, meta); err != nil {
			log.Printf("CI refresh: writing meta for %s: %v", issue.ID, err)
		}
		if mrStatus != "" {
			resolve.MRStatus = mrStatus
			if err := mgr.WriteResolve(issue.ID, resolve); err != nil {
				log.Printf("CI refresh: writing resolve for %s: %v", issue.ID, err)
			}
		}
	}
}

func init() {
	scanCmd.Flags().StringSlice("service", nil, "filter by service name(s)")
	scanCmd.Flags().String("since", "", "how far back to look (default: config value)")
	scanCmd.Flags().Int("limit", 0, "max number of issues to fetch")
	rootCmd.AddCommand(scanCmd)
}
