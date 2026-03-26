package cmd

import (
	"strings"
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

	err := runFix("issue-1", "svc-a", cfg, mgr, nil)
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
	err := runFix("issue-1", "svc-a", cfg, mgr, nil)
	if err == nil {
		t.Error("expected error when no investigation.md exists")
	}
}

func TestRunFixIterate_RequiresResolve(t *testing.T) {
	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "**Service:** svc-a\nerror content")
	mgr.WriteInvestigation("issue-1", "investigation content")
	mgr.WriteFix("issue-1", "previous fix content")
	// No resolve.json

	cfg := &config.Config{
		Agent: config.AgentConfig{Fix: "echo"},
	}

	err := runFixIterate("issue-1", "svc-a", cfg, mgr, nil)
	if err == nil {
		t.Error("expected error when resolve.json is missing")
	}
	if !strings.Contains(err.Error(), "resolve") {
		t.Errorf("expected error to mention resolve, got: %v", err)
	}
}

func TestRunFixIterate_RequiresPreviousFix(t *testing.T) {
	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "**Service:** svc-a\nerror content")
	mgr.WriteInvestigation("issue-1", "investigation content")
	// No fix.md
	mgr.WriteResolve("issue-1", &reports.ResolveData{
		Branch: "fix/issue-1-foo",
		MRURL:  "https://gitlab.com/org/repo/-/merge_requests/1",
	})

	cfg := &config.Config{
		Agent: config.AgentConfig{Fix: "echo"},
	}

	err := runFixIterate("issue-1", "svc-a", cfg, mgr, nil)
	if err == nil {
		t.Error("expected error when no fix.md exists")
	}
	if !strings.Contains(err.Error(), "fix") {
		t.Errorf("expected error to mention fix, got: %v", err)
	}
}
