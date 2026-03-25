package reports

import (
	"testing"
)

func TestManager_WriteAndReadError(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	content := "# Error Report\nNullPointerException in handler"
	err := m.WriteError("issue-123", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := m.ReadError("issue-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("content mismatch: got %q", got)
	}
}

func TestManager_Stage(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	stage := m.Stage("issue-123")
	if stage != StageUnknown {
		t.Errorf("expected unknown, got %s", stage)
	}

	m.WriteError("issue-123", "error")
	stage = m.Stage("issue-123")
	if stage != StageScanned {
		t.Errorf("expected scanned, got %s", stage)
	}

	m.WriteInvestigation("issue-123", "investigation")
	stage = m.Stage("issue-123")
	if stage != StageInvestigated {
		t.Errorf("expected investigated, got %s", stage)
	}

	m.WriteFix("issue-123", "fix")
	resolve := &ResolveData{
		Branch:         "fix/issue-123-null-pointer",
		MRURL:          "https://gitlab.com/org/repo/-/merge_requests/1",
		MRStatus:       "draft",
		Service:        "svc-a",
		DatadogIssueID: "issue-123",
		DatadogURL:     "https://app.datadoghq.eu/...",
	}
	m.WriteResolve("issue-123", resolve)
	stage = m.Stage("issue-123")
	if stage != StageFixed {
		t.Errorf("expected fixed, got %s", stage)
	}
}

func TestManager_ListIssues(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteError("issue-1", "error 1")
	m.WriteError("issue-2", "error 2")
	m.WriteInvestigation("issue-2", "investigation 2")

	issues, err := m.ListIssues()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestManager_Exists(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	if m.Exists("issue-123") {
		t.Error("expected issue to not exist")
	}

	m.WriteError("issue-123", "error")
	if !m.Exists("issue-123") {
		t.Error("expected issue to exist")
	}
}

func TestManager_ReadResolve(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	resolve := &ResolveData{
		Branch:         "fix/issue-123-test",
		MRURL:          "https://gitlab.com/merge/1",
		MRStatus:       "draft",
		Service:        "svc-a",
		DatadogIssueID: "issue-123",
	}
	m.WriteResolve("issue-123", resolve)

	got, err := m.ReadResolve("issue-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Branch != resolve.Branch {
		t.Errorf("branch mismatch: got %q", got.Branch)
	}
	if got.MRURL != resolve.MRURL {
		t.Errorf("mr_url mismatch: got %q", got.MRURL)
	}
}
