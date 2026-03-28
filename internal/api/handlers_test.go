package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ruter-as/fido/internal/config"
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

func TestTriggerScanHandler(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	scanCalled := false
	h := NewHandlers(mgr, nil)
	h.SetScanFunc(func() error {
		scanCalled = true
		return nil
	})

	req := httptest.NewRequest("POST", "/api/scan", nil)
	w := httptest.NewRecorder()

	h.TriggerScan(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}
	// Give goroutine time to run
	time.Sleep(50 * time.Millisecond)
	if !scanCalled {
		t.Error("expected scan function to be called")
	}
}

func TestHandlers_TriggerIgnore(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/issues/issue-1/ignore", nil)
	r = withURLParam(r, "id", "issue-1")
	w := httptest.NewRecorder()
	h.TriggerIgnore(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	meta, _ := mgr.ReadMetadata("issue-1")
	if !meta.Ignored {
		t.Error("expected ignored=true after TriggerIgnore")
	}
}

func TestHandlers_TriggerUnignore(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a", Ignored: true})

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/issues/issue-1/unignore", nil)
	r = withURLParam(r, "id", "issue-1")
	w := httptest.NewRecorder()
	h.TriggerUnignore(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	meta, _ := mgr.ReadMetadata("issue-1")
	if meta.Ignored {
		t.Error("expected ignored=false after TriggerUnignore")
	}
}

func TestHandlers_TriggerIgnore_NotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/issues/missing/ignore", nil)
	r = withURLParam(r, "id", "missing")
	w := httptest.NewRecorder()
	h.TriggerIgnore(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandlers_TriggerUnignore_NotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/issues/missing/unignore", nil)
	r = withURLParam(r, "id", "missing")
	w := httptest.NewRecorder()
	h.TriggerUnignore(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestTriggerFix_IterateFlag(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	var gotIterate bool
	h := NewHandlers(mgr, &config.Config{})
	h.SetFixFunc(func(id string, iterate bool, progress io.Writer) error {
		gotIterate = iterate
		return nil
	})

	body := strings.NewReader(`{"iterate":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/issues/issue-1/fix", body)
	req = withURLParam(req, "id", "issue-1")
	w := httptest.NewRecorder()
	h.TriggerFix(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusAccepted)
	}

	time.Sleep(50 * time.Millisecond)
	if !gotIterate {
		t.Error("expected iterate=true to be passed to FixFunc")
	}
}

func TestListIssues_IncludesCIStatus(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:  "svc-a",
		CIStatus: "failed",
		CIURL:    "https://gitlab.com/org/repo/-/pipelines/42",
	})

	h := NewHandlers(mgr, &config.Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	w := httptest.NewRecorder()
	h.ListIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var items []IssueListItem
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].CIStatus != "failed" {
		t.Errorf("CIStatus: got %q, want %q", items[0].CIStatus, "failed")
	}
}

func TestListIssuesHandler_IncludesInvestigationTags(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:     "svc-a",
		Confidence:  "High",
		Complexity:  "Simple",
		CodeFixable: "Yes",
	})

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues", nil)
	w := httptest.NewRecorder()

	h.ListIssues(w, req)

	var resp []IssueListItem
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(resp))
	}
	if resp[0].Confidence != "High" {
		t.Errorf("Confidence: got %q, want High", resp[0].Confidence)
	}
	if resp[0].Complexity != "Simple" {
		t.Errorf("Complexity: got %q, want Simple", resp[0].Complexity)
	}
	if resp[0].CodeFixable != "Yes" {
		t.Errorf("CodeFixable: got %q, want Yes", resp[0].CodeFixable)
	}
}

func TestTriggerInvestigateHandler(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "# Error\ntest")

	investigateCalled := ""
	h := NewHandlers(mgr, nil)
	h.SetInvestigateFunc(func(issueID string, _ io.Writer) error {
		investigateCalled = issueID
		return nil
	})

	req := httptest.NewRequest("POST", "/api/issues/issue-1/investigate", nil)
	w := httptest.NewRecorder()

	h.TriggerInvestigate(w, withURLParam(req, "id", "issue-1"))

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}
	time.Sleep(50 * time.Millisecond)
	if investigateCalled != "issue-1" {
		t.Errorf("expected investigate for issue-1, got %q", investigateCalled)
	}
}

func TestRefreshMRStatus_NoResolve(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/issues/issue-1/mr-status", nil)
	r = withURLParam(r, "id", "issue-1")
	w := httptest.NewRecorder()
	h.RefreshMRStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ci_status"] != "" || resp["mr_status"] != "" {
		t.Errorf("expected empty statuses, got ci_status=%q mr_status=%q", resp["ci_status"], resp["mr_status"])
	}
}

func TestRefreshMRStatus_ReturnsStoredValuesWhenNoConfig(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:  "svc-a",
		CIStatus: "passed",
		CIURL:    "https://gitlab.com/org/repo/-/pipelines/1",
	})
	mgr.WriteResolve("issue-1", &reports.ResolveData{
		Branch:   "fix/issue-1",
		MRURL:    "https://gitlab.com/mr/1",
		MRStatus: "merged",
	})

	h := NewHandlers(mgr, nil) // nil config — no glab calls
	r := httptest.NewRequest(http.MethodGet, "/api/issues/issue-1/mr-status", nil)
	r = withURLParam(r, "id", "issue-1")
	w := httptest.NewRecorder()
	h.RefreshMRStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ci_status"] != "passed" {
		t.Errorf("ci_status: got %q, want passed", resp["ci_status"])
	}
	if resp["mr_status"] != "merged" {
		t.Errorf("mr_status: got %q, want merged", resp["mr_status"])
	}
}

func TestGetIssue_RunningOpWhenInvestigating(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	h := NewHandlers(mgr, nil)
	h.running.Store("issue-1", "investigate")

	r := httptest.NewRequest(http.MethodGet, "/api/issues/issue-1", nil)
	w := httptest.NewRecorder()
	h.GetIssue(w, withURLParam(r, "id", "issue-1"))

	var resp IssueDetail
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.RunningOp != "investigate" {
		t.Errorf("RunningOp: got %q, want investigate", resp.RunningOp)
	}
}

func TestGetIssue_RunningOpEmptyWhenIdle(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/issues/issue-1", nil)
	w := httptest.NewRecorder()
	h.GetIssue(w, withURLParam(r, "id", "issue-1"))

	var resp IssueDetail
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.RunningOp != "" {
		t.Errorf("RunningOp: got %q, want empty", resp.RunningOp)
	}
}

// flushRecorder wraps httptest.ResponseRecorder to satisfy http.Flusher.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

func TestStreamProgress_IdleWhenNothingRan(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error") // stage = scanned, no investigation

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/issues/issue-1/progress", nil)
	w := &flushRecorder{httptest.NewRecorder()}
	h.StreamProgress(w, withURLParam(r, "id", "issue-1"))

	body := w.Body.String()
	if !strings.Contains(body, `"status":"idle"`) {
		t.Errorf("expected idle status, got: %s", body)
	}
	if strings.Contains(body, `"status":"complete"`) {
		t.Errorf("should not return complete for unstarted issue, got: %s", body)
	}
}

func TestStreamProgress_CompleteWhenAlreadyInvestigated(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteInvestigation("issue-1", "root cause found") // stage = investigated

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/issues/issue-1/progress", nil)
	w := &flushRecorder{httptest.NewRecorder()}
	h.StreamProgress(w, withURLParam(r, "id", "issue-1"))

	body := w.Body.String()
	if !strings.Contains(body, `"status":"complete"`) {
		t.Errorf("expected complete status, got: %s", body)
	}
}

func TestStreamEvents_ReceivesPublishedEvent(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	hub := NewHub()
	h := NewHandlers(mgr, nil)
	h.hub = hub

	// Start handler in a goroutine (it blocks on SSE)
	r := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	ctx, cancel := context.WithCancel(r.Context())
	r = r.WithContext(ctx)
	w := &flushRecorder{httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		h.StreamEvents(w, r)
		close(done)
	}()

	// Give the goroutine time to subscribe and enter the select loop
	time.Sleep(50 * time.Millisecond)

	// Publish an event
	hub.Publish(Event{Type: "issue:updated", Payload: map[string]any{"id": "issue-1"}})

	// Give it time to write
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, `"type":"issue:updated"`) {
		t.Errorf("expected event in body, got: %s", body)
	}
}

func TestListIssues_RunningOpIncluded(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	h := NewHandlers(mgr, nil)
	h.running.Store("issue-1", "fix")

	r := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	w := httptest.NewRecorder()
	h.ListIssues(w, r)

	var resp []IssueListItem
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(resp))
	}
	if resp[0].RunningOp != "fix" {
		t.Errorf("RunningOp: got %q, want fix", resp[0].RunningOp)
	}
}
