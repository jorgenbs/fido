package cmd

import (
	"testing"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

func TestFix_LaunchesInteractiveAgentAndChecksOutput(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "# Error\nNullPointerException")
	mgr.WriteInvestigation("issue-1", "# Investigation\nRoot cause: missing null check")

	// Pre-write fix.md and resolve.json to simulate the agent having run.
	mgr.WriteFix("issue-1", "**Summary**: Added null check")
	mgr.WriteResolve("issue-1", &reports.ResolveData{
		Branch:         "fix/issue-1-null-check",
		MRURL:          "https://gitlab.example.com/mr/1",
		MRStatus:       "draft",
		Service:        "svc-a",
		DatadogIssueID: "issue-1",
	})

	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: repoDir},
		},
		Agent: config.AgentConfig{
			Fix: "true", // no-op; files already written above
		},
	}

	err := runFix("issue-1", "svc-a", cfg, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
