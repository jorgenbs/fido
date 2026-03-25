package cmd

import (
	"strings"
	"testing"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

func TestFix_ProducesFixReportAndResolve(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "# Error\nNullPointerException")
	mgr.WriteInvestigation("issue-1", "# Investigation\nRoot cause: missing null check")

	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: repoDir},
		},
		Agent: config.AgentConfig{
			Fix: "cat",
		},
	}

	err := runFix("issue-1", "svc-a", cfg, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fix, err := mgr.ReadFix("issue-1")
	if err != nil {
		t.Fatalf("reading fix: %v", err)
	}
	if !strings.Contains(fix, "NullPointerException") {
		t.Error("fix report should contain error context")
	}

	stage := mgr.Stage("issue-1")
	if stage != reports.StageFixed {
		t.Errorf("expected stage fixed, got %s", stage)
	}
}

func TestFix_FailsWithoutInvestigation(t *testing.T) {
	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	mgr.WriteError("issue-1", "error")

	cfg := &config.Config{}
	err := runFix("issue-1", "svc-a", cfg, mgr)
	if err == nil {
		t.Error("expected error when no investigation.md exists")
	}
}
