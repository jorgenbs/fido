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

func TestTriggerIgnore_PublishesEvent(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})

	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	h := NewHandlers(mgr, nil)
	h.hub = hub

	r := httptest.NewRequest(http.MethodPost, "/api/issues/issue-1/ignore", nil)
	r = withURLParam(r, "id", "issue-1")
	w := httptest.NewRecorder()
	h.TriggerIgnore(w, r)

	select {
	case evt := <-ch:
		if evt.Type != "issue:updated" {
			t.Errorf("Type: got %q, want issue:updated", evt.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("no event published")
	}
}

func TestTriggerScan_PublishesEvent(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	h := NewHandlers(mgr, nil)
	h.hub = hub
	h.SetScanFunc(func() error { return nil })

	r := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	w := httptest.NewRecorder()
	h.TriggerScan(w, r)

	select {
	case evt := <-ch:
		if evt.Type != "scan:complete" {
			t.Errorf("Type: got %q, want scan:complete", evt.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no event published")
	}
}

func TestTriggerInvestigate_PublishesProgressEvents(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	h := NewHandlers(mgr, nil)
	h.hub = hub
	h.SetInvestigateFunc(func(id string, w io.Writer) error {
		return nil
	})

	r := httptest.NewRequest(http.MethodPost, "/api/issues/issue-1/investigate", nil)
	w := httptest.NewRecorder()
	h.TriggerInvestigate(w, withURLParam(r, "id", "issue-1"))

	var events []Event
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case evt := <-ch:
			events = append(events, evt)
			if evt.Type == "issue:progress" {
				p := evt.Payload.(map[string]any)
				if p["status"] == "complete" {
					goto done
				}
			}
		case <-timeout:
			goto done
		}
	}
done:
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (started + complete), got %d", len(events))
	}
	if events[0].Type != "issue:progress" {
		t.Errorf("first event: got %q, want issue:progress", events[0].Type)
	}
}

func TestListIssues_IncludesStackTraceAndDatadogURL(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "# Error\n\n## Stack Trace\n```\npanic: runtime error\ngoroutine 1:\nmain.go:42\nmain.go:10\n```")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:    "svc-a",
		DatadogURL: "https://app.datadoghq.eu/error-tracking/issue/123",
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
	if resp[0].DatadogURL != "https://app.datadoghq.eu/error-tracking/issue/123" {
		t.Errorf("DatadogURL: got %q", resp[0].DatadogURL)
	}
	if resp[0].StackTrace == "" {
		t.Error("expected non-empty StackTrace")
	}
}

func TestListIssues_IncludesTimeseries(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	issueID := "test-ts-issue"
	mgr.WriteError(issueID, "## Error\ntest error")
	mgr.WriteMetadata(issueID, &reports.MetaData{
		Title:   "Test Error",
		Service: "test-svc",
	})
	mgr.WriteTimeSeries(issueID, &reports.TimeSeries{
		Buckets: []reports.Bucket{
			{Timestamp: "2026-04-05T00:00:00Z", Count: 5},
			{Timestamp: "2026-04-05T01:00:00Z", Count: 10},
		},
		Window:      "24h",
		LastFetched: "2026-04-05T02:00:00Z",
	})

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues?window=24h", nil)
	w := httptest.NewRecorder()
	h.ListIssues(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var items []IssueListItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	if len(items[0].Timeseries) != 2 {
		t.Errorf("timeseries len = %d, want 2", len(items[0].Timeseries))
	}
	if items[0].Stats == nil {
		t.Fatal("stats is nil")
	}
	if items[0].Stats.Total != 15 {
		t.Errorf("stats.Total = %d, want 15", items[0].Stats.Total)
	}
}

func TestGetTimeseries(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	issueID := "ts-test-1"
	mgr.WriteError(issueID, "test error")
	mgr.WriteMetadata(issueID, &reports.MetaData{Service: "svc", Env: "prod"})
	mgr.WriteTimeSeries(issueID, &reports.TimeSeries{
		Buckets: []reports.Bucket{
			{Timestamp: "2026-04-03T10:00:00Z", Count: 12},
			{Timestamp: "2026-04-03T11:00:00Z", Count: 8},
		},
		Window:      "24h",
		LastFetched: "2026-04-03T14:00:00Z",
	})

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues/ts-test-1/timeseries?window=24h", nil)
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()

	h.GetTimeseries(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp reports.TimeSeries
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Buckets) != 2 {
		t.Errorf("expected 2 buckets, got %d", len(resp.Buckets))
	}
}

func TestGetTimeseries_NotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	h := NewHandlers(mgr, nil)

	req := httptest.NewRequest("GET", "/api/issues/nonexistent/timeseries", nil)
	req = withURLParam(req, "id", "nonexistent")
	w := httptest.NewRecorder()

	h.GetTimeseries(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
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
