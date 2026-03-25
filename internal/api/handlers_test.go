package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruter-as/fido/internal/reports"
)

func TestListIssuesHandler(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error 1")
	mgr.WriteError("issue-2", "error 2")

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues", nil)
	w := httptest.NewRecorder()

	h.ListIssues(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp []IssueListItem
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 2 {
		t.Errorf("expected 2 issues, got %d", len(resp))
	}
}

func TestGetIssueHandler(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "# Error\ntest error")
	mgr.WriteInvestigation("issue-1", "# Investigation\nroot cause found")

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues/issue-1", nil)
	w := httptest.NewRecorder()

	h.GetIssue(w, withURLParam(req, "id", "issue-1"))

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp IssueDetail
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID != "issue-1" {
		t.Errorf("expected issue-1, got %s", resp.ID)
	}
	if resp.Stage != "investigated" {
		t.Errorf("expected investigated, got %s", resp.Stage)
	}
	if resp.Investigation == nil {
		t.Error("expected investigation to be present")
	}
	if resp.Resolve != nil {
		t.Error("expected resolve to be nil")
	}
}

func TestGetIssueHandler_NotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues/nonexistent", nil)
	w := httptest.NewRecorder()

	h.GetIssue(w, withURLParam(req, "id", "nonexistent"))

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
