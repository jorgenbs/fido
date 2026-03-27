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

	issues, err := m.ListIssues(false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestManager_WriteAndReadMetadata(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	meta := &MetaData{
		Title:            "NullPointerException",
		Service:          "payment-svc",
		Env:              "production",
		FirstSeen:        "2026-03-25T10:00:00Z",
		LastSeen:         "2026-03-26T09:00:00Z",
		Count:            47,
		DatadogURL:       "https://ruter.datadoghq.eu/error-tracking/issue/abc",
		DatadogEventsURL: "https://ruter.datadoghq.eu/event/explorer?query=service:payment-svc",
		DatadogTraceURL:  "https://ruter.datadoghq.eu/apm/traces?query=service:payment-svc",
		Ignored:          false,
	}

	if err := m.WriteMetadata("issue-1", meta); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}
	got, err := m.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if got.Title != meta.Title {
		t.Errorf("title: got %q, want %q", got.Title, meta.Title)
	}
	if got.Service != meta.Service {
		t.Errorf("service: got %q, want %q", got.Service, meta.Service)
	}
}

func TestManager_SetIgnored(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteMetadata("issue-1", &MetaData{Service: "svc-a"})

	if err := m.SetIgnored("issue-1", true); err != nil {
		t.Fatalf("SetIgnored: %v", err)
	}
	meta, _ := m.ReadMetadata("issue-1")
	if !meta.Ignored {
		t.Error("expected ignored=true")
	}

	m.SetIgnored("issue-1", false)
	meta, _ = m.ReadMetadata("issue-1")
	if meta.Ignored {
		t.Error("expected ignored=false")
	}
}

func TestManager_ListIssues_FiltersIgnored(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteError("issue-1", "error")
	m.WriteMetadata("issue-1", &MetaData{Service: "svc-a", Ignored: false})
	m.WriteError("issue-2", "error")
	m.WriteMetadata("issue-2", &MetaData{Service: "svc-b", Ignored: true})

	visible, _ := m.ListIssues(false)
	if len(visible) != 1 {
		t.Errorf("expected 1 visible issue, got %d", len(visible))
	}

	all, _ := m.ListIssues(true)
	if len(all) != 2 {
		t.Errorf("expected 2 issues with show_ignored, got %d", len(all))
	}
}

func TestManager_ListIssues_EnrichesFromMeta(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteError("issue-1", "error")
	m.WriteMetadata("issue-1", &MetaData{Title: "NullPointer", Service: "svc-a"})

	issues, _ := m.ListIssues(false)
	if issues[0].Meta == nil {
		t.Fatal("expected Meta to be populated")
	}
	if issues[0].Meta.Service != "svc-a" {
		t.Errorf("service: got %q", issues[0].Meta.Service)
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

func TestManager_MetaData_CIStatus(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	meta := &MetaData{
		Title:    "SomeError",
		Service:  "svc-a",
		CIStatus: "failed",
		CIURL:    "https://gitlab.com/org/repo/-/pipelines/42",
	}
	if err := m.WriteMetadata("issue-1", meta); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}
	got, err := m.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if got.CIStatus != "failed" {
		t.Errorf("CIStatus: got %q, want %q", got.CIStatus, "failed")
	}
	if got.CIURL != meta.CIURL {
		t.Errorf("CIURL: got %q, want %q", got.CIURL, meta.CIURL)
	}
}

func TestManager_ReadLatestFix_FirstIteration(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteFix("issue-1", "first fix content")

	content, iter, err := m.ReadLatestFix("issue-1")
	if err != nil {
		t.Fatalf("ReadLatestFix: %v", err)
	}
	if iter != 1 {
		t.Errorf("iter: got %d, want 1", iter)
	}
	if content != "first fix content" {
		t.Errorf("content: got %q", content)
	}
}

func TestManager_ReadLatestFix_SecondIteration(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteFix("issue-1", "first fix")
	m.writeFile("issue-1", "fix-2.md", "second fix content")

	content, iter, err := m.ReadLatestFix("issue-1")
	if err != nil {
		t.Fatalf("ReadLatestFix: %v", err)
	}
	if iter != 2 {
		t.Errorf("iter: got %d, want 2", iter)
	}
	if content != "second fix content" {
		t.Errorf("content: got %q", content)
	}
}

func TestManager_ReadLatestFix_NoFix(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	_, _, err := m.ReadLatestFix("issue-1")
	if err == nil {
		t.Error("expected error when no fix exists")
	}
}

func TestManager_SetInvestigationTags(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteMetadata("issue-1", &MetaData{Service: "svc-a"})

	if err := m.SetInvestigationTags("issue-1", "High", "Simple", "Yes"); err != nil {
		t.Fatalf("SetInvestigationTags: %v", err)
	}

	meta, err := m.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Confidence != "High" {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, "High")
	}
	if meta.Complexity != "Simple" {
		t.Errorf("Complexity: got %q, want %q", meta.Complexity, "Simple")
	}
	if meta.CodeFixable != "Yes" {
		t.Errorf("CodeFixable: got %q, want %q", meta.CodeFixable, "Yes")
	}
}

func TestManager_SetInvestigationTags_PreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteMetadata("issue-1", &MetaData{Service: "svc-b", Ignored: true, CIStatus: "passed"})

	m.SetInvestigationTags("issue-1", "Medium", "Moderate", "No")

	meta, _ := m.ReadMetadata("issue-1")
	if meta.Service != "svc-b" {
		t.Errorf("Service mutated: got %q", meta.Service)
	}
	if !meta.Ignored {
		t.Error("Ignored flag was reset")
	}
	if meta.CIStatus != "passed" {
		t.Errorf("CIStatus mutated: got %q", meta.CIStatus)
	}
}
