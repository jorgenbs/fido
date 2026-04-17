package cmd

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jorgenbs/fido/internal/agent"
	"github.com/jorgenbs/fido/internal/config"
	"github.com/jorgenbs/fido/internal/datadog"
	"github.com/jorgenbs/fido/internal/reports"
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
		var ddClient *datadog.Client
		if len(cfg.Datadog) > 0 && cfg.Datadog[0].Token != "" {
			if c, err := datadog.NewClient(cfg.Datadog[0].Token, cfg.Datadog[0].Site, cfg.Datadog[0].OrgSubdomain); err == nil {
				c.SetVerbose(verbose)
				ddClient = c
			}
		}
		return runInvestigate(issueID, service, cfg, mgr, ddClient, nil)
	},
}

const investigatePromptTemplate = `You are investigating a production error. Analyze the error below, look through the codebase, and produce a root cause analysis.

## Error Report

%s
%s
## Instructions

0. Ensure you are on the latest on main branch
1. Analyze the error and stack trace
2. Find the relevant code in the repository
3. Identify the root cause
4. List all affected files and code paths
5. Suggest a fix approach
6. Estimate confidence and complexity
7. Determine whether this is a code-fixable defect

## Output Format

Write your analysis as markdown with these sections:
- **Root Cause**: What is causing this error
- **Affected Files**: List of files involved
- **Suggested Fix**: How to fix this

## Confidence: High/Medium/Low
## Complexity: Simple/Moderate/Complex
## Code Fixable: Yes/No (is this a code defect that can be fixed with a code change?)
`

func runInvestigate(issueID, service string, cfg *config.Config, mgr *reports.Manager, ddClient *datadog.Client, progress io.Writer) error {
	log.Printf("[investigate] %s: reading error report", issueID)
	errorContent, err := mgr.ReadError(issueID)
	if err != nil {
		return fmt.Errorf("no error report for issue %s: %w", issueID, err)
	}

	var issueCtx *datadog.IssueContext

	needsContext := strings.Contains(errorContent, "<!-- TRACES_PENDING -->") || strings.Contains(errorContent, "<!-- STACK_TRACE_PENDING -->")
	alreadyMissing := strings.Contains(errorContent, "_No stack trace available_")
	if verbose {
		log.Printf("[investigate] %s: needsContext=%v alreadyMissing=%v ddClient=%v", issueID, needsContext, alreadyMissing, ddClient != nil)
	}
	if needsContext || (verbose && alreadyMissing) {
		tracesSection := ""
		stackTraceSection := "_No stack trace available_"
		if ddClient == nil {
			log.Printf("[investigate] %s: skipping context fetch — no Datadog client (token missing?)", issueID)
		} else {
			meta, metaErr := mgr.ReadMetadata(issueID)
			if metaErr != nil {
				log.Printf("[investigate] %s: reading metadata failed (non-fatal): %v", issueID, metaErr)
			} else {
				if verbose {
					log.Printf("[investigate] %s: fetching issue context for service=%q env=%q firstSeen=%q lastSeen=%q",
						issueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen)
				} else {
					log.Printf("[investigate] %s: fetching issue context (traces/stack trace)", issueID)
				}
				if fetchedCtx, err := ddClient.FetchIssueContext(issueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen); err == nil {
					issueCtx = &fetchedCtx
					if len(fetchedCtx.Traces) > 0 {
						var sb strings.Builder
						sb.WriteString("## Related Traces\n\n")
						for _, t := range fetchedCtx.Traces {
							sb.WriteString(fmt.Sprintf("- [Trace %s](%s)\n", t.TraceID, t.URL))
						}
						tracesSection = sb.String()
					}
					if fetchedCtx.StackTrace != "" {
						stackTraceSection = "```\n" + fetchedCtx.StackTrace + "\n```"
					}
					if verbose {
						log.Printf("[investigate] %s: context fetch result: traces=%d stackTrace=%v",
							issueID, len(fetchedCtx.Traces), fetchedCtx.StackTrace != "")
					}
				} else {
					log.Printf("[investigate] %s: fetching issue context (non-fatal): %v", issueID, err)
				}
			}
		}
		if needsContext {
			errorContent = strings.Replace(errorContent, "<!-- TRACES_PENDING -->", tracesSection, 1)
			errorContent = strings.Replace(errorContent, "<!-- STACK_TRACE_PENDING -->", stackTraceSection, 1)
			if err := mgr.WriteError(issueID, errorContent); err != nil {
				log.Printf("[investigate] %s: updating error.md with context (non-fatal): %v", issueID, err)
			}
		}
	}

	// Build enrichment sections
	var enrichment strings.Builder

	// Trace details — reuse existing FetchIssueContext result, or fetch if not done yet
	if issueCtx == nil && ddClient != nil {
		if meta, err := mgr.ReadMetadata(issueID); err == nil {
			if ctx, err := ddClient.FetchIssueContext(issueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen); err == nil {
				issueCtx = &ctx
			}
		}
	}
	if issueCtx != nil {
		if td := formatTraceDetails(issueCtx.TraceDetails); td != "" {
			enrichment.WriteString(td)
			enrichment.WriteString("\n")
		}
	}

	// Error frequency
	if ddClient != nil {
		if meta, err := mgr.ReadMetadata(issueID); err == nil {
			if freq, err := ddClient.FetchErrorFrequency(issueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen); err == nil {
				// Version filtering: only include if first == last and non-empty
				version := ""
				if meta.FirstSeenVersion != "" && meta.FirstSeenVersion == meta.LastSeenVersion {
					version = meta.FirstSeenVersion
				}
				if fd := formatFrequency(freq, version); fd != "" {
					enrichment.WriteString(fd)
					enrichment.WriteString("\n")
				}
			}
		}
	}

	// Co-occurring errors (local data only, no API call needed)
	if meta, err := mgr.ReadMetadata(issueID); err == nil {
		if allIssues, err := mgr.ListIssues(false); err == nil {
			if re := formatRelatedErrors(issueID, meta, allIssues); re != "" {
				enrichment.WriteString(re)
				enrichment.WriteString("\n")
			}
		}
	}

	log.Printf("[investigate] %s: resolving repo for service %q", issueID, service)
	repoPath, err := resolveRepoPath(service, cfg)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf(investigatePromptTemplate, errorContent, enrichment.String())

	log.Printf("[investigate] %s: starting agent %q (repo=%s, error=%d bytes, prompt=%d bytes total)", issueID, cfg.Agent.Investigate, repoPath, len(errorContent), len(prompt))
	runner := &agent.Runner{Command: cfg.Agent.Investigate, Progress: progress}
	output, err := runner.Run(prompt, repoPath)
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	output = stripPreamble(output)

	log.Printf("[investigate] %s: writing investigation report (%d bytes)", issueID, len(output))
	if err := mgr.WriteInvestigation(issueID, output); err != nil {
		return fmt.Errorf("writing investigation report: %w", err)
	}

	confidence, complexity, codeFixable := parseInvestigationTags(output)
	if err := mgr.SetInvestigationTags(issueID, confidence, complexity, codeFixable); err != nil {
		log.Printf("[investigate] %s: storing investigation tags (non-fatal): %v", issueID, err)
	}

	fmt.Printf("Investigation complete for %s\n", issueID)
	return nil
}

// stripPreamble removes agent reasoning text that appears before the
// structured report. It looks for the first markdown heading (## ) and
// drops everything above it.
func stripPreamble(output string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			return strings.Join(lines[i:], "\n")
		}
	}
	return output // no heading found — return as-is
}

func parseInvestigationTags(content string) (confidence, complexity, codeFixable string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "## confidence:") {
			confidence = titleCase(stripMarkdown(firstWord(strings.TrimSpace(line[len("## confidence:"):]))))
		} else if strings.HasPrefix(lower, "## complexity:") {
			complexity = titleCase(stripMarkdown(firstWord(strings.TrimSpace(line[len("## complexity:"):]))))
		} else if strings.HasPrefix(lower, "## code fixable:") {
			codeFixable = titleCase(stripMarkdown(firstWord(strings.TrimSpace(line[len("## code fixable:"):]))))
		}
	}
	return
}

func stripMarkdown(s string) string {
	return strings.Trim(s, "*_")
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func firstWord(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
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

// formatTraceDetails formats a TraceDetails struct into a markdown section.
// Returns empty string if neither ErrorName nor HTTPMethod is set.
func formatTraceDetails(td datadog.TraceDetails) string {
	if td.ErrorName == "" && td.HTTPMethod == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Trace Details\n\n")

	// Error line
	if td.ErrorName != "" {
		sb.WriteString("**Error:** ")
		sb.WriteString(td.ErrorName)
		if td.ErrorMessage != "" {
			sb.WriteString(fmt.Sprintf(" — %q", td.ErrorMessage))
		}
		if td.ErrorType != "" {
			sb.WriteString(fmt.Sprintf(" (type: %s)", td.ErrorType))
		}
		sb.WriteString("\n")
	}

	// HTTP line
	if td.HTTPMethod != "" {
		sb.WriteString("**HTTP:** ")
		sb.WriteString(fmt.Sprintf("%s %s", td.HTTPMethod, td.HTTPURL))
		if td.HTTPStatusCode > 0 {
			sb.WriteString(fmt.Sprintf(" → %d", td.HTTPStatusCode))
		}
		sb.WriteString("\n")
	}

	// Response body line
	if td.ResponseBody != "" {
		sb.WriteString(fmt.Sprintf("**Response Body:** %s\n", td.ResponseBody))
	}

	return sb.String()
}

// formatFrequency formats a FrequencyData struct into a markdown section.
// Returns empty string if there are no buckets.
func formatFrequency(fd datadog.FrequencyData, version string) string {
	if len(fd.Buckets) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Error Frequency\n\n")
	sb.WriteString("_This data shows when and how often the error occurs. Use it to narrow the time window for git investigation, but only if the pattern suggests a recent onset. A long-lived error with steady occurrences is unlikely to correlate with recent commits._\n\n")

	onsetDate := fd.OnsetTime.Format("2006-01-02")
	daySpan := int(time.Since(fd.OnsetTime).Hours()/24) + 1
	sb.WriteString(fmt.Sprintf("**Summary:** First appeared %s, %d occurrences over %d days, trend: %s\n",
		onsetDate, fd.TotalCount, daySpan, fd.TrendDirection))

	if version != "" {
		sb.WriteString(fmt.Sprintf("**Version:** First seen in %s\n", version))
	}

	sb.WriteString("**Buckets:**\n")
	parts := make([]string, 0, len(fd.Buckets))
	for _, b := range fd.Buckets {
		parts = append(parts, fmt.Sprintf("%s: %d", b.Date, b.Count))
	}
	sb.WriteString(strings.Join(parts, " | "))
	sb.WriteString("\n")

	return sb.String()
}

// formatRelatedErrors formats related issues (same service, overlapping timeline) into a markdown section.
// Returns empty string if current is nil or no matching issues found.
func formatRelatedErrors(currentIssueID string, current *reports.MetaData, allIssues []reports.IssueSummary) string {
	if current == nil {
		return ""
	}
	currentTime, err := time.Parse(time.RFC3339, current.FirstSeen)
	if err != nil {
		return ""
	}

	var matches []string
	for _, issue := range allIssues {
		if issue.ID == currentIssueID {
			continue
		}
		if issue.Meta == nil {
			continue
		}
		if issue.Meta.Service != current.Service {
			continue
		}
		issueTime, err := time.Parse(time.RFC3339, issue.Meta.FirstSeen)
		if err != nil {
			continue
		}
		diff := currentTime.Sub(issueTime)
		if diff < 0 {
			diff = -diff
		}
		if diff > time.Hour {
			continue
		}
		firstSeenFmt := issueTime.Format("2006-01-02 15:04")
		line := fmt.Sprintf("- %s: %s (%s) — first seen %s, %d occurrences",
			issue.Meta.Title, issue.Meta.Message, issue.Meta.Service, firstSeenFmt, issue.Meta.Count)
		matches = append(matches, line)
	}

	if len(matches) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Potentially Related Errors\n\n")
	sb.WriteString("_These errors were tracked in the same service with overlapping timelines. They may share a root cause, or they may be entirely unrelated. Only pursue connections if the error types or stack traces suggest a shared code path._\n\n")
	for _, m := range matches {
		sb.WriteString(m)
		sb.WriteString("\n")
	}
	return sb.String()
}

func init() {
	investigateCmd.Flags().String("agent", "", "override agent command")
	investigateCmd.Flags().String("service", "", "service name (auto-detected from error report if omitted)")
	rootCmd.AddCommand(investigateCmd)
}
