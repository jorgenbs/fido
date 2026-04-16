package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jorgenbs/fido/internal/reports"
)

func TestList_ShowsIssuesWithStages(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	mgr.WriteError("issue-1", "error 1")
	mgr.WriteError("issue-2", "error 2")
	mgr.WriteInvestigation("issue-2", "investigation 2")

	var buf bytes.Buffer
	err := runList(mgr, "", "", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "issue-1") {
		t.Error("expected issue-1 in output")
	}
	if !strings.Contains(output, "issue-2") {
		t.Error("expected issue-2 in output")
	}
	if !strings.Contains(output, "scanned") {
		t.Error("expected 'scanned' stage in output")
	}
	if !strings.Contains(output, "investigated") {
		t.Error("expected 'investigated' stage in output")
	}
}

func TestList_FilterByStatus(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	mgr.WriteError("issue-1", "error 1")
	mgr.WriteError("issue-2", "error 2")
	mgr.WriteInvestigation("issue-2", "investigation 2")

	var buf bytes.Buffer
	err := runList(mgr, "investigated", "", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "issue-1") {
		t.Error("issue-1 should be filtered out")
	}
	if !strings.Contains(output, "issue-2") {
		t.Error("expected issue-2 in output")
	}
}
