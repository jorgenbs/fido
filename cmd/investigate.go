package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ruter-as/fido/internal/agent"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var investigateCmd = &cobra.Command{
	Use:   "investigate <issue-id>",
	Short: "Investigate an error issue using an AI agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		agentCmd, _ := cmd.Flags().GetString("agent")
		if agentCmd != "" {
			cfg.Agent.Investigate = agentCmd
		}

		service, _ := cmd.Flags().GetString("service")
		if service == "" {
			errorContent, err := mgr.ReadError(issueID)
			if err != nil {
				return fmt.Errorf("no error report found for %s: %w", issueID, err)
			}
			service = extractServiceFromReport(errorContent)
			if service == "" {
				return fmt.Errorf("could not determine service — use --service flag")
			}
		}
		ddClient, _ := datadog.NewClient(cfg.Datadog.Token, cfg.Datadog.Site, cfg.Datadog.OrgSubdomain)
		return runInvestigate(issueID, service, cfg, mgr, ddClient)
	},
}

const investigatePromptTemplate = `You are investigating a production error. Analyze the error below, look through the codebase, and produce a root cause analysis.

## Error Report

%s

## Instructions

1. Analyze the error and stack trace
2. Find the relevant code in the repository
3. Identify the root cause
4. List all affected files and code paths
5. Suggest a fix approach
6. Estimate confidence and complexity

## Output Format

Write your analysis as markdown with these sections:
- **Root Cause**: What is causing this error
- **Affected Files**: List of files involved
- **Suggested Fix**: How to fix this
- **Confidence**: High/Medium/Low
- **Complexity**: Simple/Moderate/Complex
`

func runInvestigate(issueID, service string, cfg *config.Config, mgr *reports.Manager, ddClient *datadog.Client) error {
	errorContent, err := mgr.ReadError(issueID)
	if err != nil {
		return fmt.Errorf("no error report for issue %s: %w", issueID, err)
	}

	repoPath, err := resolveRepoPath(service, cfg)
	if err != nil {
		return err
	}

	// Build context section from meta.json + optional Datadog traces
	var contextSection string
	if meta, err := mgr.ReadMetadata(issueID); err == nil {
		var issueCtx datadog.IssueContext
		if ddClient != nil {
			issueCtx, _ = ddClient.FetchIssueContext(meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen)
		} else {
			// Use pre-computed deep links from meta.json
			issueCtx = datadog.IssueContext{
				EventsURL: meta.DatadogEventsURL,
				TracesURL: meta.DatadogTraceURL,
			}
		}

		var lines []string
		if len(issueCtx.Traces) > 0 {
			lines = append(lines, "\n## Related Traces\n")
			for _, tr := range issueCtx.Traces {
				lines = append(lines, fmt.Sprintf("- [Trace %s](%s)", tr.TraceID, tr.URL))
			}
		}
		lines = append(lines, "\n## Useful Links\n")
		if issueCtx.EventsURL != "" {
			lines = append(lines, fmt.Sprintf("- [Events Timeline](%s)", issueCtx.EventsURL))
		}
		if issueCtx.TracesURL != "" {
			lines = append(lines, fmt.Sprintf("- [Trace Waterfall](%s)", issueCtx.TracesURL))
		}
		contextSection = strings.Join(lines, "\n")
	}

	prompt := fmt.Sprintf(investigatePromptTemplate, errorContent) + contextSection

	runner := &agent.Runner{Command: cfg.Agent.Investigate}
	output, err := runner.Run(prompt, repoPath)
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	if err := mgr.WriteInvestigation(issueID, output); err != nil {
		return fmt.Errorf("writing investigation report: %w", err)
	}

	fmt.Printf("Investigation complete for %s\n", issueID)
	return nil
}

func resolveRepoPath(service string, cfg *config.Config) (string, error) {
	repo, ok := cfg.Repositories[service]
	if !ok {
		return "", fmt.Errorf("no repository configured for service %q", service)
	}

	if repo.Local != "" {
		return repo.Local, nil
	}

	if repo.Git != "" {
		tmpDir, err := os.MkdirTemp("", "fido-repo-*")
		if err != nil {
			return "", err
		}
		cmd := exec.Command("git", "clone", "--depth", "1", repo.Git, tmpDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("git clone failed: %s: %w", string(output), err)
		}
		return tmpDir, nil
	}

	return "", fmt.Errorf("repository %q has no local or git path configured", service)
}

func extractServiceFromReport(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "**Service:**") {
			return strings.TrimSpace(strings.TrimPrefix(line, "**Service:**"))
		}
	}
	return ""
}

func init() {
	investigateCmd.Flags().String("agent", "", "override agent command")
	investigateCmd.Flags().String("service", "", "service name (auto-detected from error report if omitted)")
	rootCmd.AddCommand(investigateCmd)
}
