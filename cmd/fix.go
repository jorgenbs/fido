package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ruter-as/fido/internal/agent"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var fixCmd = &cobra.Command{
	Use:   "fix <issue-id>",
	Short: "Fix an investigated issue using an AI agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		agentCmd, _ := cmd.Flags().GetString("agent")
		if agentCmd != "" {
			cfg.Agent.Fix = agentCmd
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
		return runFix(issueID, service, cfg, mgr)
	},
}

const fixPromptTemplate = `You are fixing a production error. Use the error report and investigation below to implement a fix.

## Error Report

%s

## Investigation

%s

## Instructions

1. Create a new branch: fix/%s-<short-description>
2. Implement the fix described in the investigation
3. Commit with conventional commit message: fix(<service>): <description>
4. Push the branch
5. Create a draft MR using glab:
   - Title: fix(<service>): <short description>
   - Description: Include investigation summary and Datadog link
   - Draft: yes

## Output Format

Write a summary of what you did:
- **Summary**: What was changed and why
- **Files Changed**: List of modified files
- **Branch**: The branch name
- **MR URL**: The merge request URL (if created)
- **Tests**: Any test results
`

func runFix(issueID, service string, cfg *config.Config, mgr *reports.Manager) error {
	errorContent, err := mgr.ReadError(issueID)
	if err != nil {
		return fmt.Errorf("no error report for issue %s: %w", issueID, err)
	}

	investigationContent, err := mgr.ReadInvestigation(issueID)
	if err != nil {
		return fmt.Errorf("no investigation report for issue %s — run 'fido investigate %s' first: %w", issueID, issueID, err)
	}

	repoPath, err := resolveRepoPath(service, cfg)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf(fixPromptTemplate, errorContent, investigationContent, issueID)

	runner := &agent.Runner{Command: cfg.Agent.Fix}
	output, err := runner.Run(prompt, repoPath)
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	if err := mgr.WriteFix(issueID, output); err != nil {
		return fmt.Errorf("writing fix report: %w", err)
	}

	resolve := &reports.ResolveData{
		Branch:         parseField(output, "Branch"),
		MRURL:          parseField(output, "MR URL"),
		MRStatus:       "draft",
		Service:        service,
		DatadogIssueID: issueID,
		DatadogURL:     fmt.Sprintf("https://app.%s/error-tracking/issue/%s", cfg.Datadog.Site, issueID),
	}
	if err := mgr.WriteResolve(issueID, resolve); err != nil {
		return fmt.Errorf("writing resolve data: %w", err)
	}

	fmt.Printf("Fix complete for %s\n", issueID)
	return nil
}

// parseField extracts a value from agent output like "- **Branch:** fix/issue-123-desc"
func parseField(content, field string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		prefix := "**" + field + ":**"
		if idx := strings.Index(trimmed, prefix); idx != -1 {
			return strings.TrimSpace(trimmed[idx+len(prefix):])
		}
	}
	return ""
}

func init() {
	fixCmd.Flags().String("agent", "", "override agent command")
	fixCmd.Flags().String("service", "", "service name (auto-detected from error report if omitted)")
	rootCmd.AddCommand(fixCmd)
}
