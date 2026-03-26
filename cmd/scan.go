package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan Datadog for new error issues",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		ddClient := datadog.NewClient(
			cfg.Datadog.Token,
			fmt.Sprintf("https://api.%s", cfg.Datadog.Site),
		)

		services, _ := cmd.Flags().GetStringSlice("service")
		if len(services) == 0 {
			services = cfg.Datadog.Services
		}

		since, _ := cmd.Flags().GetString("since")
		if since == "" {
			since = cfg.Scan.Since
		}

		scanCfg := &config.Config{
			Datadog: config.DatadogConfig{Services: services, Site: cfg.Datadog.Site},
			Scan:    config.ScanConfig{Since: since},
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
}

func runScan(cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) (int, error) {
	issues, err := ddClient.SearchErrorIssues(cfg.Datadog.Services, cfg.Scan.Since)
	if err != nil {
		return 0, fmt.Errorf("searching error issues: %w", err)
	}

	tmpl, err := loadErrorTemplate()
	if err != nil {
		return 0, fmt.Errorf("loading template: %w", err)
	}

	count := 0
	for _, issue := range issues {
		if mgr.Exists(issue.ID) {
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
			DatadogURL: fmt.Sprintf("https://app.%s/error-tracking/issue/%s", cfg.Datadog.Site, issue.ID),
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return count, fmt.Errorf("rendering error report: %w", err)
		}

		if err := mgr.WriteError(issue.ID, buf.String()); err != nil {
			return count, fmt.Errorf("writing error report: %w", err)
		}
		count++
	}

	return count, nil
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
_No stack trace available_
{{end}}

## Surrounding Logs

{{if .Logs}}
{{range .Logs}}
- ` + "`{{.Timestamp}}`" + ` [{{.Status}}] {{.Message}}
{{end}}
{{else}}
_No surrounding logs found_
{{end}}

## Links

- [Datadog Issue]({{.DatadogURL}})
`
	return template.New("error").Parse(defaultTemplate)
}

func init() {
	scanCmd.Flags().StringSlice("service", nil, "filter by service name(s)")
	scanCmd.Flags().String("since", "", "how far back to look (default: config value)")
	scanCmd.Flags().Int("limit", 0, "max number of issues to fetch")
	rootCmd.AddCommand(scanCmd)
}
