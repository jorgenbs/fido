package cmd

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/jorgenbs/fido/internal/agent"
	"github.com/jorgenbs/fido/internal/config"
	"github.com/jorgenbs/fido/internal/gitlab"
	"github.com/jorgenbs/fido/internal/reports"
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
		iterate, _ := cmd.Flags().GetBool("iterate")
		if iterate {
			return runFixIterate(issueID, service, cfg, mgr, nil)
		}
		return runFix(issueID, service, cfg, mgr, nil)
	},
}

const fixPromptTemplate = `You are fixing a production error. Use the error report and investigation below to implement a fix.

## Error Report

%s

## Investigation

%s

## Instructions

0. Check that you are on the latest of the remote default branch of the repository
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

const fixIteratePromptTemplate = `You are iterating on a fix for a production error. A previous fix attempt was made but CI is failing.

## Error Report

%s

## Investigation

%s

## Previous Fix Attempt

%s

## CI Failure Output

%s

## Instructions

The branch ` + "`%s`" + ` already exists with an open MR at %s.

1. Review the CI failure output above to understand what is failing
2. Make the necessary code changes to fix the CI failures
3. Commit your changes to the existing branch ` + "`%s`" + ` with a conventional commit message: fix(<service>): <description>
4. Push the branch — do NOT create a new branch or new MR
5. Write a summary to %s with these sections:
   - **Summary**: What CI was failing and what was changed
   - **Files Changed**: List of modified files
   - **Tests**: Any test results
`

func runFix(issueID, service string, cfg *config.Config, mgr *reports.Manager, progress io.Writer) error {
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

	runner := &agent.Runner{Command: cfg.Agent.Fix, Progress: progress}
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

func runFixIterate(issueID, service string, cfg *config.Config, mgr *reports.Manager, progress io.Writer) error {
	errorContent, err := mgr.ReadError(issueID)
	if err != nil {
		return fmt.Errorf("no error report for issue %s: %w", issueID, err)
	}

	investigationContent, err := mgr.ReadInvestigation(issueID)
	if err != nil {
		return fmt.Errorf("no investigation for issue %s — run 'fido investigate %s' first: %w", issueID, issueID, err)
	}

	resolve, err := mgr.ReadResolve(issueID)
	if err != nil {
		return fmt.Errorf("no resolve.json for issue %s — run 'fido fix %s' first: %w", issueID, issueID, err)
	}

	fixContent, currentIter, err := mgr.ReadLatestFix(issueID)
	if err != nil {
		return fmt.Errorf("no fix file for issue %s — run 'fido fix %s' first: %w", issueID, issueID, err)
	}

	repoPath, err := resolveRepoPath(service, cfg)
	if err != nil {
		return err
	}

	ciLogs, err := gitlab.FetchCIFailureLogs(resolve.Branch, repoPath)
	if err != nil {
		log.Printf("warning: could not fetch CI logs for %s: %v", issueID, err)
		ciLogs = "(CI logs unavailable — check GitLab pipeline manually)"
	}

	home, _ := os.UserHomeDir()
	issueReportsDir := filepath.Join(home, ".fido", "reports", issueID)
	nextFixFilename := fmt.Sprintf("fix-%d.md", currentIter+1)
	nextFixPath := filepath.Join(issueReportsDir, nextFixFilename)

	prompt := fmt.Sprintf(fixIteratePromptTemplate,
		errorContent, investigationContent, fixContent, ciLogs,
		resolve.Branch, resolve.MRURL,
		resolve.Branch,
		nextFixPath,
	)

	runner := &agent.Runner{Command: cfg.Agent.Fix, Progress: progress}
	if err := runner.RunInteractive(prompt, repoPath); err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	if _, _, err := mgr.ReadLatestFix(issueID); err == nil {
		fmt.Printf("Fix iteration complete for %s (wrote %s)\n", issueID, nextFixFilename)
	} else {
		fmt.Fprintf(os.Stderr, "Warning: %s was not written — the session may have exited early.\n", nextFixFilename)
	}

	return nil
}

func init() {
	fixCmd.Flags().String("agent", "", "override agent command")
	fixCmd.Flags().String("service", "", "service name (auto-detected from error report if omitted)")
	fixCmd.Flags().Bool("iterate", false, "iterate on an existing fix using CI failure logs")
	rootCmd.AddCommand(fixCmd)
}
