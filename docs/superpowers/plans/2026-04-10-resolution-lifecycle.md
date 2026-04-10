# Resolution Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the error management loop — resolve errors in Datadog when MRs merge, continuously monitor Datadog status for all tracked issues, detect regressions, and surface status in the dashboard.

**Architecture:** Three new Datadog client methods (`ResolveIssue`, `GetIssueStatus`, `GetIssue`), three new MetaData fields (`DatadogStatus`, `ResolvedAt`, `RegressionCount`), implementation of the `resolve_check` engine job, and a `DatadogStatusBadge` frontend component. The sync engine enqueues a `resolve_check` job for every tracked non-ignored issue each cycle. The resolve_check job handles both MR-merge-triggered resolution and continuous status monitoring.

**Tech Stack:** Go (Datadog SDK v2 `datadogV2.ErrorTrackingApi`, `datadogV2.IssueState`), React/TypeScript, Tailwind CSS.

---

## File Structure

| Action | Path | Responsibility |
|--------|------|---------------|
| Modify | `internal/datadog/client.go` | Add `ResolveIssue()`, `GetIssueStatus()` methods |
| Modify | `internal/datadog/client_test.go` | Tests for new Datadog client methods |
| Modify | `internal/reports/manager.go` | Add `DatadogStatus`, `ResolvedAt`, `RegressionCount` to MetaData |
| Modify | `internal/reports/manager_test.go` | Tests for new MetaData fields |
| Modify | `internal/syncer/engine.go` | Implement `resolve_check` job, add `ResolveIssue`/`GetIssueStatus`/`ReadResolve`/`ReadMetadata`/`SetDatadogStatus` to `Deps` interface |
| Modify | `internal/syncer/engine_test.go` | Tests for resolve_check logic |
| Modify | `internal/syncer/adapter.go` | Implement new `Deps` methods on Adapter |
| Modify | `internal/api/handlers.go` | Add `DatadogStatus`/`RegressionCount` to `IssueListItem` and `IssueDetail` |
| Modify | `web/src/api/client.ts` | Add `datadog_status`/`regression_count` to types |
| Create | `web/src/components/DatadogStatusBadge.tsx` | Badge component for Datadog status |
| Modify | `web/src/pages/Dashboard.tsx` | Render `DatadogStatusBadge`, sort regressions to top, handle new SSE events |

---

### Task 1: MetaData Fields

**Files:**
- Modify: `internal/reports/manager.go:30-49`
- Modify: `internal/reports/manager_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/reports/manager_test.go`:

```go
func TestManager_MetaData_ResolutionFields(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	m.WriteError("issue-1", "error")

	meta := &MetaData{
		Title:           "TestError",
		Service:         "svc-a",
		DatadogStatus:   "for_review",
		ResolvedAt:      "",
		RegressionCount: 0,
	}
	if err := m.WriteMetadata("issue-1", meta); err != nil {
		t.Fatal(err)
	}

	got, err := m.ReadMetadata("issue-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DatadogStatus != "for_review" {
		t.Errorf("expected for_review, got %s", got.DatadogStatus)
	}
	if got.RegressionCount != 0 {
		t.Errorf("expected 0, got %d", got.RegressionCount)
	}

	// Update resolution fields
	got.DatadogStatus = "resolved"
	got.ResolvedAt = "2026-04-10T12:00:00Z"
	got.RegressionCount = 1
	if err := m.WriteMetadata("issue-1", got); err != nil {
		t.Fatal(err)
	}

	got2, err := m.ReadMetadata("issue-1")
	if err != nil {
		t.Fatal(err)
	}
	if got2.DatadogStatus != "resolved" {
		t.Errorf("expected resolved, got %s", got2.DatadogStatus)
	}
	if got2.ResolvedAt != "2026-04-10T12:00:00Z" {
		t.Errorf("expected resolved_at, got %s", got2.ResolvedAt)
	}
	if got2.RegressionCount != 1 {
		t.Errorf("expected 1, got %d", got2.RegressionCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/reports/ -run TestManager_MetaData_ResolutionFields -v`
Expected: FAIL — `DatadogStatus`, `ResolvedAt`, `RegressionCount` fields don't exist on MetaData.

- [ ] **Step 3: Add fields to MetaData struct**

In `internal/reports/manager.go`, add three fields to the `MetaData` struct (after `LastSeenVersion`):

```go
	DatadogStatus   string `json:"datadog_status,omitempty"`
	ResolvedAt      string `json:"resolved_at,omitempty"`
	RegressionCount int    `json:"regression_count,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/reports/ -run TestManager_MetaData_ResolutionFields -v`
Expected: PASS

- [ ] **Step 5: Add SetDatadogStatus convenience method and test**

Add test to `internal/reports/manager_test.go`:

```go
func TestManager_SetDatadogStatus(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	m.WriteError("issue-1", "error")
	m.WriteMetadata("issue-1", &MetaData{Service: "svc-a"})

	if err := m.SetDatadogStatus("issue-1", "resolved", "2026-04-10T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	meta, _ := m.ReadMetadata("issue-1")
	if meta.DatadogStatus != "resolved" {
		t.Errorf("expected resolved, got %s", meta.DatadogStatus)
	}
	if meta.ResolvedAt != "2026-04-10T12:00:00Z" {
		t.Errorf("expected resolved_at set, got %s", meta.ResolvedAt)
	}
}

func TestManager_IncrementRegressionCount(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	m.WriteError("issue-1", "error")
	m.WriteMetadata("issue-1", &MetaData{Service: "svc-a", RegressionCount: 0})

	if err := m.IncrementRegressionCount("issue-1"); err != nil {
		t.Fatal(err)
	}
	meta, _ := m.ReadMetadata("issue-1")
	if meta.RegressionCount != 1 {
		t.Errorf("expected 1, got %d", meta.RegressionCount)
	}

	if err := m.IncrementRegressionCount("issue-1"); err != nil {
		t.Fatal(err)
	}
	meta, _ = m.ReadMetadata("issue-1")
	if meta.RegressionCount != 2 {
		t.Errorf("expected 2, got %d", meta.RegressionCount)
	}
}
```

Add methods to `internal/reports/manager.go`:

```go
func (m *Manager) SetDatadogStatus(issueID, status, resolvedAt string) error {
	meta, err := m.ReadMetadata(issueID)
	if err != nil {
		return fmt.Errorf("reading metadata: %w", err)
	}
	meta.DatadogStatus = status
	if resolvedAt != "" {
		meta.ResolvedAt = resolvedAt
	}
	return m.WriteMetadata(issueID, meta)
}

func (m *Manager) IncrementRegressionCount(issueID string) error {
	meta, err := m.ReadMetadata(issueID)
	if err != nil {
		return fmt.Errorf("reading metadata: %w", err)
	}
	meta.RegressionCount++
	return m.WriteMetadata(issueID, meta)
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/reports/ -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/reports/manager.go internal/reports/manager_test.go
git commit -m "feat(reports): add DatadogStatus, ResolvedAt, RegressionCount to MetaData"
```

---

### Task 2: Datadog Client — ResolveIssue and GetIssueStatus

**Files:**
- Modify: `internal/datadog/client.go`
- Modify: `internal/datadog/client_test.go`

- [ ] **Step 1: Write the failing test for ResolveIssue**

Add to `internal/datadog/client_test.go`:

```go
func TestClient_ResolveIssue(t *testing.T) {
	var receivedMethod, receivedPath string
	var receivedBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"data":{"id":"abc123","type":"error_tracking_issue","attributes":{"state":"RESOLVED"}}}`)
	}))
	defer ts.Close()

	client, err := NewClient("test-token", "datadoghq.com", "myorg")
	if err != nil {
		t.Fatal(err)
	}
	client.OverrideServers(datadog.ServerConfigurations{
		{URL: ts.URL, Description: "test"},
	})

	if err := client.ResolveIssue("abc123"); err != nil {
		t.Fatalf("ResolveIssue failed: %v", err)
	}

	if receivedMethod != "PUT" {
		t.Errorf("expected PUT, got %s", receivedMethod)
	}
	if !strings.Contains(receivedPath, "/abc123/state") {
		t.Errorf("expected path containing /abc123/state, got %s", receivedPath)
	}
	if !strings.Contains(string(receivedBody), `"RESOLVED"`) {
		t.Errorf("expected body to contain RESOLVED, got %s", receivedBody)
	}
}
```

Add imports needed at top of test file: `"io"`, `"strings"` (if not already present).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/datadog/ -run TestClient_ResolveIssue -v`
Expected: FAIL — `ResolveIssue` method doesn't exist.

- [ ] **Step 3: Implement ResolveIssue**

Add to `internal/datadog/client.go`:

```go
// ResolveIssue marks an error tracking issue as resolved in Datadog.
func (c *Client) ResolveIssue(issueID string) error {
	attrs := datadogV2.NewIssueUpdateStateRequestDataAttributes(datadogV2.ISSUESTATE_RESOLVED)
	data := datadogV2.NewIssueUpdateStateRequestData(*attrs, issueID, datadogV2.ISSUEUPDATESTATEREQUESTDATATYPE_ERROR_TRACKING_ISSUE)
	body := datadogV2.IssueUpdateStateRequest{Data: *data}

	_, _, err := c.api.UpdateIssueState(c.ctx(), issueID, body)
	if err != nil {
		return fmt.Errorf("resolving issue %s: %w", issueID, err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/datadog/ -run TestClient_ResolveIssue -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for GetIssueStatus**

Add to `internal/datadog/client_test.go`:

```go
func TestClient_GetIssueStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"id":"abc123","type":"error_tracking_issue","attributes":{"state":"OPEN","error_type":"RuntimeError","service":"my-svc"}}}`)
	}))
	defer ts.Close()

	client, err := NewClient("test-token", "datadoghq.com", "myorg")
	if err != nil {
		t.Fatal(err)
	}
	client.OverrideServers(datadog.ServerConfigurations{
		{URL: ts.URL, Description: "test"},
	})

	status, err := client.GetIssueStatus("abc123")
	if err != nil {
		t.Fatalf("GetIssueStatus failed: %v", err)
	}
	if status != "OPEN" {
		t.Errorf("expected OPEN, got %s", status)
	}
}
```

- [ ] **Step 6: Implement GetIssueStatus**

Add to `internal/datadog/client.go`:

```go
// GetIssueStatus fetches the current state of a Datadog error tracking issue.
// Returns the state string (e.g. "OPEN", "RESOLVED", "ACKNOWLEDGED").
func (c *Client) GetIssueStatus(issueID string) (string, error) {
	resp, _, err := c.api.GetIssue(c.ctx(), issueID)
	if err != nil {
		return "", fmt.Errorf("getting issue %s: %w", issueID, err)
	}
	data := resp.GetData()
	attrs := data.GetAttributes()
	return string(attrs.GetState()), nil
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/datadog/ -run TestClient_GetIssueStatus -v`
Expected: PASS

- [ ] **Step 8: Run all Datadog client tests**

Run: `go test ./internal/datadog/ -v`
Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add internal/datadog/client.go internal/datadog/client_test.go
git commit -m "feat(datadog): add ResolveIssue and GetIssueStatus API methods"
```

---

### Task 3: Sync Engine — Implement resolve_check Job

**Files:**
- Modify: `internal/syncer/engine.go`
- Modify: `internal/syncer/engine_test.go`

- [ ] **Step 1: Extend the Deps interface**

Add the new methods the engine needs to the `Deps` interface in `internal/syncer/engine.go`:

```go
type Deps interface {
	ScanIssues() ([]ScanResult, error)
	FetchStacktrace(issueID, service, env, firstSeen, lastSeen string) (string, error)
	SaveStacktrace(issueID, stacktrace string) error
	HasStacktrace(issueID string) bool
	Publish(eventType string, payload map[string]any)
	// Resolution lifecycle
	ListTrackedIssues() ([]TrackedIssue, error)
	ResolveIssue(datadogIssueID string) error
	GetIssueStatus(datadogIssueID string) (string, error)
	SetDatadogStatus(issueID, status, resolvedAt string) error
	IncrementRegressionCount(issueID string) error
}
```

Add the `TrackedIssue` struct (contains the fields the engine needs for resolve_check):

```go
// TrackedIssue is a summary of a tracked issue for resolve_check jobs.
type TrackedIssue struct {
	IssueID        string
	DatadogIssueID string // the Datadog-side issue ID (from resolve.json or the issue ID itself)
	MRStatus       string
	DatadogStatus  string
	ResolvedAt     string
	Ignored        bool
}
```

- [ ] **Step 2: Write the failing test for resolve_check — MR merge triggers resolve**

Add to `internal/syncer/engine_test.go`:

```go
type resolveCheckDeps struct {
	trackedIssues      []TrackedIssue
	resolvedIssueIDs   []string
	statusByIssueID    map[string]string
	setStatusCalls     []setStatusCall
	regressionCounts   map[string]int
	publishedEvents    []publishedEvent
}

type setStatusCall struct {
	IssueID    string
	Status     string
	ResolvedAt string
}

type publishedEvent struct {
	EventType string
	Payload   map[string]any
}

func (d *resolveCheckDeps) ScanIssues() ([]ScanResult, error)                          { return nil, nil }
func (d *resolveCheckDeps) FetchStacktrace(_, _, _, _, _ string) (string, error)        { return "", nil }
func (d *resolveCheckDeps) SaveStacktrace(_, _ string) error                            { return nil }
func (d *resolveCheckDeps) HasStacktrace(_ string) bool                                 { return true }
func (d *resolveCheckDeps) ListTrackedIssues() ([]TrackedIssue, error)                  { return d.trackedIssues, nil }

func (d *resolveCheckDeps) ResolveIssue(datadogIssueID string) error {
	d.resolvedIssueIDs = append(d.resolvedIssueIDs, datadogIssueID)
	return nil
}

func (d *resolveCheckDeps) GetIssueStatus(datadogIssueID string) (string, error) {
	if s, ok := d.statusByIssueID[datadogIssueID]; ok {
		return s, nil
	}
	return "OPEN", nil
}

func (d *resolveCheckDeps) SetDatadogStatus(issueID, status, resolvedAt string) error {
	d.setStatusCalls = append(d.setStatusCalls, setStatusCall{issueID, status, resolvedAt})
	return nil
}

func (d *resolveCheckDeps) IncrementRegressionCount(issueID string) error {
	if d.regressionCounts == nil {
		d.regressionCounts = map[string]int{}
	}
	d.regressionCounts[issueID]++
	return nil
}

func (d *resolveCheckDeps) Publish(eventType string, payload map[string]any) {
	d.publishedEvents = append(d.publishedEvents, publishedEvent{eventType, payload})
}

func TestEngine_ResolveCheck_MRMergedTriggersResolve(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "merged",
				DatadogStatus:  "",
				ResolvedAt:     "",
				Ignored:        false,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "RESOLVED"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	if len(deps.resolvedIssueIDs) != 1 || deps.resolvedIssueIDs[0] != "dd-abc" {
		t.Errorf("expected ResolveIssue called with dd-abc, got %v", deps.resolvedIssueIDs)
	}
	if len(deps.setStatusCalls) < 1 {
		t.Fatal("expected SetDatadogStatus to be called")
	}
	call := deps.setStatusCalls[0]
	if call.Status != "resolved" {
		t.Errorf("expected status=resolved, got %s", call.Status)
	}
	if call.ResolvedAt == "" {
		t.Error("expected ResolvedAt to be set")
	}
}
```

- [ ] **Step 2b: Run test to verify it fails**

Run: `go test ./internal/syncer/ -run TestEngine_ResolveCheck_MRMergedTriggersResolve -v`
Expected: FAIL — `executeResolveCheck` doesn't exist, `Deps` interface missing methods.

- [ ] **Step 3: Write test for regression detection**

Add to `internal/syncer/engine_test.go`:

```go
func TestEngine_ResolveCheck_DetectsRegression(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "merged",
				DatadogStatus:  "resolved",
				ResolvedAt:     "2026-04-09T12:00:00Z",
				Ignored:        false,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "OPEN"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	// Should NOT call ResolveIssue again (already resolved, ResolvedAt is set)
	if len(deps.resolvedIssueIDs) != 0 {
		t.Errorf("expected no ResolveIssue calls, got %v", deps.resolvedIssueIDs)
	}

	// Should detect regression
	if deps.regressionCounts["issue-1"] != 1 {
		t.Errorf("expected regression count 1, got %d", deps.regressionCounts["issue-1"])
	}

	// Should update status to open
	found := false
	for _, c := range deps.setStatusCalls {
		if c.IssueID == "issue-1" && c.Status == "open" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SetDatadogStatus(issue-1, open), got %v", deps.setStatusCalls)
	}

	// Should publish regression event
	hasRegressionEvent := false
	for _, e := range deps.publishedEvents {
		if e.EventType == "issue:regression" {
			hasRegressionEvent = true
		}
	}
	if !hasRegressionEvent {
		t.Errorf("expected issue:regression event, got %v", deps.publishedEvents)
	}
}
```

- [ ] **Step 4: Write test for loop prevention**

Add to `internal/syncer/engine_test.go`:

```go
func TestEngine_ResolveCheck_NoReResolveWhenAlreadyResolved(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "merged",
				DatadogStatus:  "resolved",
				ResolvedAt:     "2026-04-09T12:00:00Z", // already resolved
				Ignored:        false,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "RESOLVED"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	// Should NOT call ResolveIssue (already resolved)
	if len(deps.resolvedIssueIDs) != 0 {
		t.Errorf("expected no ResolveIssue calls, got %v", deps.resolvedIssueIDs)
	}

	// Status unchanged, no calls expected
	if len(deps.setStatusCalls) != 0 {
		t.Errorf("expected no status updates, got %v", deps.setStatusCalls)
	}
}
```

- [ ] **Step 5: Write test for skipping ignored issues**

Add to `internal/syncer/engine_test.go`:

```go
func TestEngine_ResolveCheck_SkipsIgnoredIssues(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "merged",
				DatadogStatus:  "",
				ResolvedAt:     "",
				Ignored:        true,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "OPEN"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	if len(deps.resolvedIssueIDs) != 0 {
		t.Errorf("expected no API calls for ignored issue, got %v", deps.resolvedIssueIDs)
	}
}
```

- [ ] **Step 6: Write test for general status sync (non-regression divergence)**

Add to `internal/syncer/engine_test.go`:

```go
func TestEngine_ResolveCheck_StatusSyncNonRegression(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "",
				DatadogStatus:  "open",
				ResolvedAt:     "",
				Ignored:        false,
			},
		},
		// Datadog says ACKNOWLEDGED, Fido says open
		statusByIssueID: map[string]string{"dd-abc": "ACKNOWLEDGED"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	// Should update status
	found := false
	for _, c := range deps.setStatusCalls {
		if c.IssueID == "issue-1" && c.Status == "acknowledged" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected status update to acknowledged, got %v", deps.setStatusCalls)
	}

	// Should publish status_changed, not regression
	for _, e := range deps.publishedEvents {
		if e.EventType == "issue:regression" {
			t.Error("should not fire regression event for non-regression status change")
		}
	}
}
```

- [ ] **Step 7: Implement executeResolveCheck**

Add to `internal/syncer/engine.go`:

```go
// executeResolveCheck runs the resolution lifecycle logic for all tracked issues.
// For each non-ignored issue:
// 1. If MR is merged and ResolvedAt is empty, resolve in Datadog
// 2. Check Datadog status and detect divergence/regression
func (e *Engine) executeResolveCheck() {
	issues, err := e.deps.ListTrackedIssues()
	if err != nil {
		log.Printf("syncer: ListTrackedIssues error: %v", err)
		return
	}

	for _, issue := range issues {
		if issue.Ignored {
			continue
		}
		if issue.DatadogIssueID == "" {
			continue
		}

		// Step 1: MR merge -> resolve in Datadog (once per fix cycle)
		if issue.MRStatus == "merged" && issue.ResolvedAt == "" {
			if err := e.deps.ResolveIssue(issue.DatadogIssueID); err != nil {
				log.Printf("syncer: ResolveIssue error for %s: %v", issue.IssueID, err)
				continue
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if err := e.deps.SetDatadogStatus(issue.IssueID, "resolved", now); err != nil {
				log.Printf("syncer: SetDatadogStatus error for %s: %v", issue.IssueID, err)
			}
			e.deps.Publish("issue:resolved", map[string]any{"id": issue.IssueID})
			continue // skip status check this cycle, we just set it
		}

		// Step 2: Check current Datadog status
		ddStatus, err := e.deps.GetIssueStatus(issue.DatadogIssueID)
		if err != nil {
			log.Printf("syncer: GetIssueStatus error for %s: %v", issue.IssueID, err)
			continue
		}

		normalizedStatus := strings.ToLower(ddStatus)
		storedStatus := strings.ToLower(issue.DatadogStatus)

		if normalizedStatus == storedStatus {
			continue // no change
		}

		// Status diverged
		isRegression := storedStatus == "resolved" && (normalizedStatus == "open" || normalizedStatus == "for_review")
		if isRegression {
			if err := e.deps.IncrementRegressionCount(issue.IssueID); err != nil {
				log.Printf("syncer: IncrementRegressionCount error for %s: %v", issue.IssueID, err)
			}
			if err := e.deps.SetDatadogStatus(issue.IssueID, normalizedStatus, ""); err != nil {
				log.Printf("syncer: SetDatadogStatus error for %s: %v", issue.IssueID, err)
			}
			e.deps.Publish("issue:regression", map[string]any{"id": issue.IssueID})
		} else {
			if err := e.deps.SetDatadogStatus(issue.IssueID, normalizedStatus, ""); err != nil {
				log.Printf("syncer: SetDatadogStatus error for %s: %v", issue.IssueID, err)
			}
			e.deps.Publish("issue:status_changed", map[string]any{"id": issue.IssueID, "status": normalizedStatus})
		}
	}
}
```

Add `"strings"` to the imports in `engine.go`.

Update the worker switch case to call `executeResolveCheck` instead of the log stub:

Replace:
```go
		case JobResolveCheck:
			if err := e.limiter.WaitContext(ctx); err != nil {
				return
			}
			log.Printf("syncer: resolve_check for issue %s: not yet implemented", job.IssueID)
```

With:
```go
		case JobResolveCheck:
			// resolve_check doesn't use individual job.IssueID — it processes all tracked issues
			e.executeResolveCheck()
```

- [ ] **Step 8: Run all engine tests**

Run: `go test ./internal/syncer/ -v`
Expected: All PASS. Existing tests may fail if they use a mock that doesn't implement the new `Deps` methods — see next step.

- [ ] **Step 9: Fix existing engine_test.go mocks**

The existing engine tests use a mock `Deps`. Add the new interface methods to any existing mock structs in `engine_test.go`:

```go
func (d *existingMockDeps) ListTrackedIssues() ([]TrackedIssue, error)         { return nil, nil }
func (d *existingMockDeps) ResolveIssue(_ string) error                        { return nil }
func (d *existingMockDeps) GetIssueStatus(_ string) (string, error)            { return "", nil }
func (d *existingMockDeps) SetDatadogStatus(_, _, _ string) error              { return nil }
func (d *existingMockDeps) IncrementRegressionCount(_ string) error            { return nil }
```

(Use the actual mock struct name from the existing test file.)

- [ ] **Step 10: Run all syncer tests**

Run: `go test ./internal/syncer/ -v`
Expected: All PASS

- [ ] **Step 11: Commit**

```bash
git add internal/syncer/engine.go internal/syncer/engine_test.go
git commit -m "feat(syncer): implement resolve_check job with regression detection"
```

---

### Task 4: Sync Engine — Enqueue resolve_check Each Cycle + Wire Adapter

**Files:**
- Modify: `internal/syncer/engine.go:149-165` (executeSyncIssues)
- Modify: `internal/syncer/adapter.go`

- [ ] **Step 1: Enqueue resolve_check after sync_issues**

In `internal/syncer/engine.go`, add a `resolve_check` job at the end of `executeSyncIssues()`:

```go
func (e *Engine) executeSyncIssues() {
	results, err := e.deps.ScanIssues()
	if err != nil {
		log.Printf("syncer: ScanIssues error: %v", err)
		return
	}

	for _, r := range results {
		e.scanMeta[r.IssueID] = r

		if !e.deps.HasStacktrace(r.IssueID) {
			e.queue.Push(Job{Type: JobFetchStacktrace, IssueID: r.IssueID, Priority: 3})
		}
	}

	// Enqueue resolve_check to run after stacktrace fetches
	e.queue.Push(Job{Type: JobResolveCheck, Priority: 4})

	e.deps.Publish("scan:complete", map[string]any{"count": len(results)})
}
```

- [ ] **Step 2: Implement new Deps methods on Adapter**

Add to `internal/syncer/adapter.go`:

```go
func (a *Adapter) ListTrackedIssues() ([]TrackedIssue, error) {
	issues, err := a.mgr.ListIssues(false) // exclude ignored
	if err != nil {
		return nil, err
	}

	var tracked []TrackedIssue
	for _, issue := range issues {
		ti := TrackedIssue{
			IssueID: issue.ID,
		}
		if issue.Meta != nil {
			ti.DatadogStatus = issue.Meta.DatadogStatus
			ti.ResolvedAt = issue.Meta.ResolvedAt
			ti.Ignored = issue.Meta.Ignored
		}
		if resolve, err := a.mgr.ReadResolve(issue.ID); err == nil {
			ti.MRStatus = resolve.MRStatus
			ti.DatadogIssueID = resolve.DatadogIssueID
		}
		// Fall back to using the issue ID as the Datadog issue ID
		if ti.DatadogIssueID == "" {
			ti.DatadogIssueID = issue.ID
		}
		tracked = append(tracked, ti)
	}
	return tracked, nil
}

func (a *Adapter) ResolveIssue(datadogIssueID string) error {
	return a.ddClient.ResolveIssue(datadogIssueID)
}

func (a *Adapter) GetIssueStatus(datadogIssueID string) (string, error) {
	return a.ddClient.GetIssueStatus(datadogIssueID)
}

func (a *Adapter) SetDatadogStatus(issueID, status, resolvedAt string) error {
	return a.mgr.SetDatadogStatus(issueID, status, resolvedAt)
}

func (a *Adapter) IncrementRegressionCount(issueID string) error {
	return a.mgr.IncrementRegressionCount(issueID)
}
```

- [ ] **Step 3: Build to verify compilation**

Run: `go build ./...`
Expected: Success

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/syncer/engine.go internal/syncer/adapter.go
git commit -m "feat(syncer): enqueue resolve_check each cycle, wire adapter methods"
```

---

### Task 5: API — Surface DatadogStatus and RegressionCount

**Files:**
- Modify: `internal/api/handlers.go:20-39` (IssueListItem), `internal/api/handlers.go:41-51` (IssueDetail)
- Modify: `internal/api/handlers.go:113-163` (ListIssues), `internal/api/handlers.go:165-201` (GetIssue)

- [ ] **Step 1: Add fields to IssueListItem and IssueDetail**

Add to `IssueListItem` struct in `internal/api/handlers.go`:

```go
	DatadogStatus   string `json:"datadog_status,omitempty"`
	RegressionCount int    `json:"regression_count,omitempty"`
```

Add to `IssueDetail` struct:

```go
	DatadogStatus   string `json:"datadog_status,omitempty"`
	RegressionCount int    `json:"regression_count,omitempty"`
```

- [ ] **Step 2: Populate fields in ListIssues handler**

In the `ListIssues` method, inside the `if issue.Meta != nil` block, add:

```go
				item.DatadogStatus   = issue.Meta.DatadogStatus
				item.RegressionCount = issue.Meta.RegressionCount
```

- [ ] **Step 3: Populate fields in GetIssue handler**

In the `GetIssue` method, inside the `if meta, err := h.reports.ReadMetadata(id); err == nil` block, add:

```go
		detail.DatadogStatus = meta.DatadogStatus
		detail.RegressionCount = meta.RegressionCount
```

- [ ] **Step 4: Build to verify**

Run: `go build ./...`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers.go
git commit -m "feat(api): expose datadog_status and regression_count in issue endpoints"
```

---

### Task 6: Frontend — DatadogStatusBadge Component

**Files:**
- Create: `web/src/components/DatadogStatusBadge.tsx`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add fields to TypeScript types**

In `web/src/api/client.ts`, add to the `IssueListItem` interface:

```typescript
  datadog_status: string;
  regression_count: number;
```

Add to the `IssueDetail` interface:

```typescript
  datadog_status: string;
  regression_count: number;
```

Add new SSE event types to the `SSEEvent` type:

```typescript
export interface SSEEvent {
  type: 'scan:complete' | 'issue:updated' | 'issue:progress' | 'issue:imported' | 'issue:resolved' | 'issue:regression' | 'issue:status_changed';
  payload: Record<string, any>;
}
```

- [ ] **Step 2: Create DatadogStatusBadge component**

Create `web/src/components/DatadogStatusBadge.tsx`:

```tsx
interface DatadogStatusBadgeProps {
  status: string;
  regressionCount: number;
}

const STATUS_STYLES: Record<string, string> = {
  resolved: 'bg-green-100 text-green-800 border-green-300 dark:bg-green-900/40 dark:text-green-400 dark:border-green-800',
  regression: 'bg-red-100 text-red-800 border-red-300 dark:bg-red-900/40 dark:text-red-400 dark:border-red-800',
  acknowledged: 'bg-muted text-muted-foreground border-border',
  ignored: 'bg-muted text-muted-foreground border-border',
};

export function DatadogStatusBadge({ status, regressionCount }: DatadogStatusBadgeProps) {
  if (!status) return null;

  const isRegression = (status === 'open' || status === 'for_review') && regressionCount > 0;

  if (isRegression) {
    const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLES.regression}`;
    return <span className={classes}>Regression{regressionCount > 1 ? ` (${regressionCount})` : ''}</span>;
  }

  if (status === 'resolved') {
    const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLES.resolved}`;
    return <span className={classes}>Resolved</span>;
  }

  if (status === 'acknowledged' || status === 'ignored') {
    const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLES[status]}`;
    return <span className={classes}>{status}</span>;
  }

  // open/for_review with no regressions — no badge (default state)
  return null;
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: Success (or only pre-existing errors)

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts web/src/components/DatadogStatusBadge.tsx
git commit -m "feat(web): add DatadogStatusBadge component and update API types"
```

---

### Task 7: Frontend — Dashboard Integration

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: Import DatadogStatusBadge**

Add import at top of `web/src/pages/Dashboard.tsx`:

```typescript
import { DatadogStatusBadge } from '../components/DatadogStatusBadge';
```

- [ ] **Step 2: Add DD Status column to grid**

Update the grid template from:
```
grid-cols-[32px_2fr_1fr_0.6fr_0.6fr_0.5fr_0.5fr_0.6fr_0.6fr]
```
to:
```
grid-cols-[32px_2fr_1fr_0.6fr_0.6fr_0.5fr_0.5fr_0.6fr_0.6fr_0.6fr]
```

This applies to:
1. The header row div
2. Each issue row div

Add a new header column after "MR":
```tsx
<span>DD Status</span>
```

Add the badge in each issue row after the MR column:
```tsx
<span>
  <DatadogStatusBadge status={issue.datadog_status} regressionCount={issue.regression_count} />
</span>
```

- [ ] **Step 3: Sort regressions to top**

In the `filteredIssues` computation, add sorting after filtering. Replace:
```typescript
  const filteredIssues = issues.filter(issue => {
```

With a sorted version:
```typescript
  const filteredIssues = issues.filter(issue => {
    if (serviceFilter !== 'all' && issue.service !== serviceFilter) return false;
    if (confidenceFilter !== 'all' && issue.confidence.toLowerCase() !== confidenceFilter.toLowerCase()) return false;
    if (complexityFilter !== 'all' && issue.complexity.toLowerCase() !== complexityFilter.toLowerCase()) return false;
    if (codeFixableOnly && issue.code_fixable !== 'Yes') return false;
    return true;
  }).sort((a, b) => {
    const aRegression = a.regression_count > 0 && (a.datadog_status === 'open' || a.datadog_status === 'for_review') ? 1 : 0;
    const bRegression = b.regression_count > 0 && (b.datadog_status === 'open' || b.datadog_status === 'for_review') ? 1 : 0;
    return bRegression - aRegression;
  });
```

- [ ] **Step 4: Handle new SSE events**

In the `useEventStream` callback, add cases for the new events:

```typescript
      case 'issue:resolved':
      case 'issue:regression':
      case 'issue:status_changed': {
        if (id) {
          fetchIssues(true);
          setHighlightedIds(prev => new Set(prev).add(id));
          setTimeout(() => {
            setHighlightedIds(prev => {
              const next = new Set(prev);
              next.delete(id);
              return next;
            });
          }, 3000);

          const goToIssue = () => navigate(`/issues/${id}`);
          if (event.type === 'issue:regression') {
            notify('Regression detected', { body: `Issue ${id} has regressed`, onClick: goToIssue });
          } else if (event.type === 'issue:resolved') {
            notify('Issue resolved', { body: `Issue ${id} marked resolved in Datadog`, onClick: goToIssue });
          }
        }
        break;
      }
```

- [ ] **Step 5: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: Success

- [ ] **Step 6: Run Playwright verification**

Terminal 1: `cd web && npm run dev`
Terminal 2: `cd web && node verify.mjs`
Expected: No React errors

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/Dashboard.tsx
git commit -m "feat(web): show Datadog status badge on dashboard, sort regressions to top"
```

---

### Task 8: Build + Integration Verification

**Files:** None (verification only)

- [ ] **Step 1: Full build**

Run: `make build`
Expected: Success

- [ ] **Step 2: Run all Go tests**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 3: Start server and verify API**

```bash
go build -o fido . && kill $(pgrep -f './fido serve') 2>/dev/null ; ./fido serve &
```

Wait for "Fido server listening on :8080", then:

```bash
curl -s localhost:8080/api/issues | jq '.[0] | {datadog_status, regression_count}'
```

Expected: Response includes `datadog_status` and `regression_count` fields (values may be empty/0 for existing issues).

Pick a real issue ID from the output:
```bash
curl -s localhost:8080/api/issues/<id> | jq '{datadog_status, regression_count}'
```

Expected: Same fields present in detail response.

- [ ] **Step 4: Verify frontend**

Open `http://localhost:8080` in browser. Verify:
- Dashboard loads without errors
- DD Status column is visible in the header
- Issues with no `datadog_status` show no badge (expected for existing issues)

- [ ] **Step 5: Clean up**

```bash
kill $(pgrep -f './fido serve') 2>/dev/null
```
