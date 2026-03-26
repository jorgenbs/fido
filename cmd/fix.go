package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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
6. Write a summary to %s/fix.md with these sections:
   - **Summary**: What was changed and why
   - **Files Changed**: List of modified files
   - **Tests**: Any test results
7. Write %s/resolve.json with this exact JSON structure:
   {"branch":"<branch-name>","mr_url":"<mr-url>","mr_status":"draft","service":"%s","datadog_issue_id":"%s","datadog_url":"%s","created_at":"<RFC3339 timestamp>"}
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

	home, _ := os.UserHomeDir()
	issueReportsDir := filepath.Join(home, ".fido", "reports", issueID)
	datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issueID)

	prompt := fmt.Sprintf(fixPromptTemplate,
		errorContent, investigationContent,
		issueID,
		issueReportsDir, issueReportsDir,
		service, issueID, datadogURL,
	)

	runner := &agent.Runner{Command: cfg.Agent.Fix}
	if err := runner.RunInteractive(prompt, repoPath); err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	if !mgr.Exists(issueID) || mgr.Stage(issueID) != reports.StageFixed {
		fmt.Fprintf(os.Stderr, "Warning: fix.md or resolve.json was not written — the session may have exited early.\n")
	} else {
		fmt.Printf("Fix complete for %s\n", issueID)
	}

	return nil
}

func init() {
	fixCmd.Flags().String("agent", "", "override agent command")
	fixCmd.Flags().String("service", "", "service name (auto-detected from error report if omitted)")
	rootCmd.AddCommand(fixCmd)
}
