package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
)

func TestInvestigate_ProducesInvestigationReport(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "# Error\nNullPointerException in handler")

	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: repoDir},
		},
		Agent: config.AgentConfig{
			Investigate: "cat",
		},
	}

	err := runInvestigate("issue-1", "svc-a", cfg, mgr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := mgr.ReadInvestigation("issue-1")
	if err != nil {
		t.Fatalf("reading investigation: %v", err)
	}
	if !strings.Contains(content, "NullPointerException") {
		t.Error("investigation should contain error context from prompt")
	}
}

func TestInvestigate_FailsWithoutErrorReport(t *testing.T) {
	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	cfg := &config.Config{}
	err := runInvestigate("issue-1", "svc-a", cfg, mgr, nil, nil)
	if err == nil {
		t.Error("expected error when no error.md exists")
	}
}

func TestParseInvestigationTags_AllPresent(t *testing.T) {
	content := `## Root Cause
Some analysis here.

## Confidence: High

The stack trace points precisely to the issue.

## Complexity: Simple

No code changes required.

## Code Fixable: No
`
	conf, comp, fix := parseInvestigationTags(content)
	if conf != "High" {
		t.Errorf("confidence: got %q, want %q", conf, "High")
	}
	if comp != "Simple" {
		t.Errorf("complexity: got %q, want %q", comp, "Simple")
	}
	if fix != "No" {
		t.Errorf("codeFixable: got %q, want %q", fix, "No")
	}
}

func TestParseInvestigationTags_CaseInsensitive(t *testing.T) {
	content := "## confidence: medium\n## COMPLEXITY: Complex\n## Code Fixable: Yes\n"
	conf, comp, fix := parseInvestigationTags(content)
	if conf != "Medium" {
		t.Errorf("confidence: got %q, want %q", conf, "Medium")
	}
	if comp != "Complex" {
		t.Errorf("complexity: got %q, want %q", comp, "Complex")
	}
	if fix != "Yes" {
		t.Errorf("codeFixable: got %q, want %q", fix, "Yes")
	}
}

func TestParseInvestigationTags_AsteriskWrapped(t *testing.T) {
	content := "## Confidence: **Medium**\n## Complexity: **Moderate**\n## Code Fixable: **Yes**\n"
	conf, comp, fix := parseInvestigationTags(content)
	if conf != "Medium" {
		t.Errorf("confidence: got %q, want Medium", conf)
	}
	if comp != "Moderate" {
		t.Errorf("complexity: got %q, want Moderate", comp)
	}
	if fix != "Yes" {
		t.Errorf("codeFixable: got %q, want Yes", fix)
	}
}

func TestParseInvestigationTags_Partially(t *testing.T) {
	content := "## Code Fixable: Partially\n"
	_, _, fix := parseInvestigationTags(content)
	if fix != "Partially" {
		t.Errorf("codeFixable: got %q, want Partially", fix)
	}
}

func TestParseInvestigationTags_MissingTags(t *testing.T) {
	content := "## Root Cause\nSome text only."
	conf, comp, fix := parseInvestigationTags(content)
	if conf != "" {
		t.Errorf("confidence: got %q, want empty", conf)
	}
	if comp != "" {
		t.Errorf("complexity: got %q, want empty", comp)
	}
	if fix != "" {
		t.Errorf("codeFixable: got %q, want empty", fix)
	}
}

func TestStripPreamble_RemovesThinkingText(t *testing.T) {
	input := "Let me analyze this.\nLooking at the code...\n\n---\n\n## Root Cause\nThe bug is here."
	got := stripPreamble(input)
	if !strings.HasPrefix(got, "## Root Cause") {
		t.Errorf("expected to start with '## Root Cause', got %q", got[:min(len(got), 40)])
	}
	if strings.Contains(got, "Let me analyze") {
		t.Error("expected thinking text to be stripped")
	}
}

func TestStripPreamble_NoHeading(t *testing.T) {
	input := "Just plain text with no headings"
	got := stripPreamble(input)
	if got != input {
		t.Errorf("expected unchanged output, got %q", got)
	}
}

func TestStripPreamble_HeadingOnFirstLine(t *testing.T) {
	input := "## Root Cause\nThe bug is here."
	got := stripPreamble(input)
	if got != input {
		t.Errorf("expected unchanged output, got %q", got)
	}
}

func TestInvestigate_ParsesAndStoresTags(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "# Error\nNullPointerException")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})

	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: repoDir},
		},
		Agent: config.AgentConfig{
			Investigate: "bash -c",
		},
	}

	// Override to use a mock that outputs structured tags
	// We can't easily pass args with spaces through strings.Fields splitting,
	// so use a wrapper script approach or use printf via shell.
	// Instead, write a temp script file.
	scriptFile := filepath.Join(t.TempDir(), "mock-agent.sh")
	os.WriteFile(scriptFile, []byte("#!/bin/sh\nprintf '## Confidence: High\n## Complexity: Simple\n## Code Fixable: Yes\n'"), 0755)
	cfg.Agent.Investigate = scriptFile

	err := runInvestigate("issue-1", "svc-a", cfg, mgr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta, err := mgr.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Confidence != "High" {
		t.Errorf("Confidence: got %q, want High", meta.Confidence)
	}
	if meta.Complexity != "Simple" {
		t.Errorf("Complexity: got %q, want Simple", meta.Complexity)
	}
	if meta.CodeFixable != "Yes" {
		t.Errorf("CodeFixable: got %q, want Yes", meta.CodeFixable)
	}
}

func TestRunInvestigate_IncludesContextLinks(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	// URLs are now embedded in error.md by the scan step
	mgr.WriteError("issue-1", "# Error\nNullPointerException\n\n## Links\n\n- [Events Timeline](https://example.com/events)\n- [Trace Waterfall](https://example.com/traces)\n")

	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{"svc-a": {Local: repoDir}},
		Agent:        config.AgentConfig{Investigate: "cat"},
	}

	err := runInvestigate("issue-1", "svc-a", cfg, mgr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv, _ := mgr.ReadInvestigation("issue-1")
	if !strings.Contains(inv, "https://example.com/events") {
		t.Error("expected investigation prompt to contain events URL")
	}
	if !strings.Contains(inv, "https://example.com/traces") {
		t.Error("expected investigation prompt to contain traces URL")
	}
}

// --- formatTraceDetails ---

func TestFormatTraceDetails_WithData(t *testing.T) {
	td := datadog.TraceDetails{
		ErrorName:      "ServiceNotAvailableError",
		ErrorMessage:   "Service is not available at the requested time",
		ErrorType:      "DRTException",
		HTTPMethod:     "POST",
		HTTPURL:        "https://api.sparelabs.com/v1/rides",
		HTTPStatusCode: 400,
		ResponseBody:   `{"name":"ServiceNotAvailableError"}`,
	}
	got := formatTraceDetails(td)
	if !strings.Contains(got, "ServiceNotAvailableError") {
		t.Error("expected ErrorName in output")
	}
	if !strings.Contains(got, "POST") {
		t.Error("expected HTTPMethod in output")
	}
	if !strings.Contains(got, "400") {
		t.Error("expected status code in output")
	}
	if !strings.Contains(got, "## Trace Details") {
		t.Error("expected section heading")
	}
}

func TestFormatTraceDetails_Empty(t *testing.T) {
	got := formatTraceDetails(datadog.TraceDetails{})
	if got != "" {
		t.Errorf("expected empty string for zero-value TraceDetails, got %q", got)
	}
}

// --- formatFrequency ---

func TestFormatFrequency_WithBuckets(t *testing.T) {
	onset := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	fd := datadog.FrequencyData{
		Buckets: []datadog.FrequencyBucket{
			{Date: "2026-03-26", Count: 1},
			{Date: "2026-04-07", Count: 5},
		},
		OnsetTime:      onset,
		TotalCount:     6,
		TrendDirection: "increasing",
	}
	got := formatFrequency(fd, "v2.4.1")
	if !strings.Contains(got, "## Error Frequency") {
		t.Error("expected section heading")
	}
	if !strings.Contains(got, "2026-03-26") {
		t.Error("expected onset date in output")
	}
	if !strings.Contains(got, "6 occurrences") {
		t.Error("expected total count in output")
	}
	if !strings.Contains(got, "increasing") {
		t.Error("expected trend in output")
	}
	if !strings.Contains(got, "v2.4.1") {
		t.Error("expected version in output")
	}
	if !strings.Contains(got, "2026-03-26: 1") {
		t.Error("expected bucket entries in output")
	}
}

func TestFormatFrequency_EmptyBuckets(t *testing.T) {
	got := formatFrequency(datadog.FrequencyData{}, "v1.0")
	if got != "" {
		t.Errorf("expected empty string for no buckets, got %q", got)
	}
}

func TestFormatFrequency_OmitsEmptyVersion(t *testing.T) {
	onset := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	fd := datadog.FrequencyData{
		Buckets:        []datadog.FrequencyBucket{{Date: "2026-03-26", Count: 1}},
		OnsetTime:      onset,
		TotalCount:     1,
		TrendDirection: "stable",
	}
	got := formatFrequency(fd, "")
	if strings.Contains(got, "**Version:**") {
		t.Error("expected no Version line when version is empty")
	}
}

// --- formatRelatedErrors ---

func TestFormatRelatedErrors_WithMatches(t *testing.T) {
	now := time.Now().UTC()
	currentFirstSeen := now.Add(-2 * time.Hour).Format(time.RFC3339)
	matchFirstSeen := now.Add(-2*time.Hour + 30*time.Minute).Format(time.RFC3339) // ±30min — within 1h
	tooOldFirstSeen := now.Add(-5 * time.Hour).Format(time.RFC3339)

	current := &reports.MetaData{
		Service:   "svc-a",
		FirstSeen: currentFirstSeen,
		Title:     "NullPointerException",
		Count:     3,
	}

	allIssues := []reports.IssueSummary{
		{
			ID: "issue-current",
			Meta: &reports.MetaData{
				Service:   "svc-a",
				FirstSeen: currentFirstSeen,
				Title:     "NullPointerException",
				Count:     3,
			},
		},
		{
			ID: "issue-match",
			Meta: &reports.MetaData{
				Service:   "svc-a",
				FirstSeen: matchFirstSeen,
				Title:     "TimeoutException",
				Message:   "Connection timed out",
				Count:     5,
			},
		},
		{
			ID: "issue-diff-service",
			Meta: &reports.MetaData{
				Service:   "svc-b",
				FirstSeen: matchFirstSeen,
				Title:     "OtherError",
				Count:     1,
			},
		},
		{
			ID: "issue-too-old",
			Meta: &reports.MetaData{
				Service:   "svc-a",
				FirstSeen: tooOldFirstSeen,
				Title:     "AncientError",
				Count:     1,
			},
		},
	}

	got := formatRelatedErrors("issue-current", current, allIssues)
	if !strings.Contains(got, "TimeoutException") {
		t.Error("expected matching issue in output")
	}
	if strings.Contains(got, "OtherError") {
		t.Error("expected different-service issue excluded")
	}
	if strings.Contains(got, "AncientError") {
		t.Error("expected too-old issue excluded")
	}
	if strings.Contains(got, "NullPointerException") {
		t.Error("expected current issue excluded from related errors")
	}
}

func TestInvestigate_IncludesEnrichmentSections(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	// Create the issue under investigation
	mgr.WriteError("issue-1", "# Error\n**Service:** svc-a\nDRTException in handler")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:   "svc-a",
		FirstSeen: "2026-03-26T16:00:00Z",
		LastSeen:  "2026-03-26T17:00:00Z",
	})

	// Create a co-occurring issue in the same service
	mgr.WriteError("issue-2", "# Error\n**Service:** svc-a\nTimeoutException")
	mgr.WriteMetadata("issue-2", &reports.MetaData{
		Title:     "TimeoutException",
		Message:   "Connection timed out",
		Service:   "svc-a",
		FirstSeen: "2026-03-26T15:30:00Z",
		LastSeen:  "2026-03-26T17:00:00Z",
		Count:     3,
	})

	// Use "cat" as agent so the prompt becomes the investigation output
	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{"svc-a": {Local: repoDir}},
		Agent:        config.AgentConfig{Investigate: "cat"},
	}

	err := runInvestigate("issue-1", "svc-a", cfg, mgr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv, _ := mgr.ReadInvestigation("issue-1")

	// Should include related errors section (issue-2 is same service, within ±1h)
	if !strings.Contains(inv, "TimeoutException") {
		t.Error("expected related error TimeoutException in investigation prompt")
	}
	if !strings.Contains(inv, "Potentially Related Errors") {
		t.Error("expected Related Errors section header")
	}
}

func TestFormatRelatedErrors_NoMatches(t *testing.T) {
	got := formatRelatedErrors("issue-1", nil, nil)
	if got != "" {
		t.Errorf("expected empty string for nil current, got %q", got)
	}
}
