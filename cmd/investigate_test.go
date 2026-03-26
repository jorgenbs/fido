package cmd

import (
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
