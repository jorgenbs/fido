package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruter-as/fido/internal/config"
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
