# Fido v3: Import-Driven Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refocus Fido from auto-scanning Datadog to manual import-driven workflow — remove timeseries/sparkline UI, make scan update-only, add `fido import <id>` CLI and web import.

**Architecture:** Three phases: (1) strip timeseries features from backend + frontend, (2) change scan to update-only, (3) add import command + API + web input. Each phase is independently committable.

**Tech Stack:** Go (cobra CLI, chi router), React/TypeScript (Vite, shadcn/ui), Datadog API client

**Spec:** `docs/superpowers/specs/2026-04-07-fido-v3-import-workflow-design.md`

---

### Task 1: Remove timeseries backend (reports, API handler, stats)

**Files:**
- Delete: `internal/reports/timeseries.go`
- Delete: `internal/reports/timeseries_test.go`
- Delete: `internal/api/timeseries_stats.go`
- Delete: `internal/api/timeseries_stats_test.go`
- Modify: `internal/api/handlers.go` — remove `Timeseries`/`Stats` fields from `IssueListItem`, remove `GetTimeseries` handler, remove timeseries reading from `ListIssues`, remove `window` param
- Modify: `internal/api/handlers_test.go` — remove `TestListIssues_IncludesTimeseries`, `TestGetTimeseries`, `TestGetTimeseries_NotFound`
- Modify: `internal/api/server.go` — remove `/api/issues/{id}/timeseries` route

- [ ] **Step 1: Delete timeseries files**

```bash
rm internal/reports/timeseries.go internal/reports/timeseries_test.go
rm internal/api/timeseries_stats.go internal/api/timeseries_stats_test.go
```

- [ ] **Step 2: Remove timeseries fields and handler from handlers.go**

In `internal/api/handlers.go`:

Remove these fields from `IssueListItem` struct:
```go
	Timeseries []reports.Bucket  `json:"timeseries,omitempty"`
	Stats      *timeseriesStats  `json:"stats,omitempty"`
```

Remove the `GetTimeseries` handler (lines 451-469, the entire function).

In `ListIssues`, remove the `window` query param parsing (lines 114-117):
```go
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "24h"
	}
```

And remove the timeseries reading block inside the issue loop (lines 162-166):
```go
		if ts, err := h.reports.ReadTimeSeries(issue.ID); err == nil && ts.Window == window {
			item.Timeseries = ts.Buckets
			stats := computeTimeseriesStats(ts.Buckets)
			item.Stats = &stats
		}
```

- [ ] **Step 3: Remove timeseries route from server.go**

In `internal/api/server.go`, remove this line:
```go
		r.Get("/issues/{id}/timeseries", h.GetTimeseries)
```

- [ ] **Step 4: Remove timeseries tests from handlers_test.go**

In `internal/api/handlers_test.go`, delete these three test functions entirely:
- `TestListIssues_IncludesTimeseries` (lines 597-641)
- `TestGetTimeseries` (lines 643-676)
- `TestGetTimeseries_NotFound` (lines 678-692)

- [ ] **Step 5: Run tests**

Run: `go test ./internal/api/... ./internal/reports/...`
Expected: All tests pass. The `reports` import in `handlers.go` is still used by `reports.Manager`, `reports.ResolveData`, `reports.MetaData` etc. so it stays.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor: remove timeseries backend (reports, API handler, stats)"
```

---

### Task 2: Remove timeseries from Datadog client

**Files:**
- Modify: `internal/datadog/client.go` — remove `FetchErrorTimeline`, `parseComputeRaw`, `parseComputeTyped`, `TimelineBucket`
- Modify: `internal/datadog/client_test.go` — remove any timeseries-related tests

- [ ] **Step 1: Check for timeseries tests in client_test.go**

Run: `grep -n "Timeline\|Timeseries\|timeseries\|FetchError" internal/datadog/client_test.go`

Remove any tests that reference `FetchErrorTimeline` or `TimelineBucket`.

- [ ] **Step 2: Remove from client.go**

In `internal/datadog/client.go`, remove:

The `TimelineBucket` type (lines 17-20):
```go
type TimelineBucket struct {
	Timestamp string
	Count     int64
}
```

The `FetchErrorTimeline` function (lines 338-400).

The `parseComputeRaw` function (lines 403-429).

The `parseComputeTyped` function (lines 432-448).

- [ ] **Step 3: Run tests**

Run: `go test ./internal/datadog/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "refactor: remove FetchErrorTimeline and timeseries types from Datadog client"
```

---

### Task 3: Remove timeseries from syncer (engine, adapter, deps)

**Files:**
- Modify: `internal/syncer/engine.go` — remove `BucketData`, `FetchBuckets`/`SaveBuckets`/`IsBucketStale` from `Deps`, remove `executeFetchBuckets`, remove bucket-stale enqueue from `executeSyncIssues`, remove `JobFetchBuckets` case from worker
- Modify: `internal/syncer/adapter.go` — remove `FetchBuckets`, `SaveBuckets`, `IsBucketStale` methods
- Modify: `internal/syncer/queue.go` — remove `JobFetchBuckets` constant
- Modify: `internal/syncer/engine_test.go` — remove bucket-related assertions and mock methods
- Modify: `internal/syncer/adapter.go` — remove bucket-related imports if unused

- [ ] **Step 1: Update Deps interface in engine.go**

In `internal/syncer/engine.go`:

Remove the `BucketData` type (lines 21-24):
```go
type BucketData struct {
	Timestamp string
	Count     int64
}
```

Remove these three methods from the `Deps` interface:
```go
	FetchBuckets(issueID, service, env, window string) ([]BucketData, error)
	SaveBuckets(issueID string, buckets []BucketData, window string) error
	IsBucketStale(issueID, window string, maxAge time.Duration) bool
```

Remove the `executeFetchBuckets` function (lines 176-195).

In the `worker` function, remove the `JobFetchBuckets` case (lines 133-136):
```go
		case JobFetchBuckets:
			if err := e.limiter.WaitContext(ctx); err != nil {
				return
			}
			e.executeFetchBuckets(job)
```

In `executeSyncIssues`, remove the bucket-stale check (lines 164-166):
```go
		if e.deps.IsBucketStale(r.IssueID, e.config.Window, 30*time.Minute) {
			e.queue.Push(Job{Type: JobFetchBuckets, IssueID: r.IssueID, Priority: 2})
		}
```

- [ ] **Step 2: Remove JobFetchBuckets from queue.go**

In `internal/syncer/queue.go`, remove:
```go
	JobFetchBuckets    JobType = "fetch_buckets"
```

- [ ] **Step 3: Remove bucket methods from adapter.go**

In `internal/syncer/adapter.go`, remove the `FetchBuckets` method (lines 41-65), the `SaveBuckets` method (lines 94-105), and the `IsBucketStale` method (lines 128-134).

Also remove the `"time"` import if it becomes unused after removing `SaveBuckets` (check — it's still used? No, `IsBucketStale` takes `time.Duration` but the method body uses `a.mgr.ReadTimeSeries`... actually the adapter implements the interface so if the interface method is removed, the implementation must be removed too). Remove the `"github.com/ruter-as/fido/internal/reports"` import only if no other method uses it — `SaveStacktrace` still uses `a.mgr` methods that don't need a `reports` import, but check. Actually `SaveBuckets` was the only method creating `reports.Bucket` and `reports.TimeSeries`. The `reports` package is still used via `a.mgr` which is `*reports.Manager`, but that's in the struct definition. Keep the import.

Remove `"time"` from adapter.go imports since `SaveBuckets` was the only user (it called `time.Now()`). The `IsBucketStale` method signature has `time.Duration` parameter but we're removing that too. Check if anything else uses `time` — no. Remove it.

- [ ] **Step 4: Update engine_test.go**

In `internal/syncer/engine_test.go`:

Remove these methods from `mockDeps`:
```go
func (m *mockDeps) FetchBuckets(issueID, service, env, window string) ([]BucketData, error) {
	m.bucketCount.Add(1)
	return []BucketData{{Timestamp: "2026-04-03T10:00:00Z", Count: 5}}, nil
}
func (m *mockDeps) SaveBuckets(issueID string, buckets []BucketData, window string) error { return nil }
func (m *mockDeps) IsBucketStale(issueID, window string, maxAge time.Duration) bool       { return true }
```

Remove the `bucketCount` field from `mockDeps`:
```go
	bucketCount atomic.Int32
```

In `TestEngine_RunsAndEnqueuesFollowUpJobs`, remove:
```go
	if deps.bucketCount.Load() < 1 {
		t.Errorf("expected at least 1 bucket fetch, got %d", deps.bucketCount.Load())
	}
```

In `TestEngine_RespectsRateLimit`, change the total calculation from:
```go
	total := deps.bucketCount.Load() + deps.stackCount.Load()
```
to:
```go
	total := deps.stackCount.Load()
```

And adjust the threshold — with only stacktrace jobs (1 per issue, 20 issues), the rate limit of 3 should still cap it. Change:
```go
	if total >= 20 {
		t.Errorf("rate limiter not working: %d follow-up jobs completed (expected < 20)", total)
	}
```
No change needed, the logic still holds.

Remove `"time"` from the test imports if unused — check: `time` is used by `time.Millisecond`, `time.Second`, `time.Minute` in tests. Keep it.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/syncer/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor: remove bucket/timeseries fetching from syncer"
```

---

### Task 4: Remove observation_window from config

**Files:**
- Modify: `internal/config/config.go` — remove `ObservationWindow` field from `ScanConfig`, remove default value
- Modify: `config.example.yml` — remove `observation_window` line

- [ ] **Step 1: Remove from config.go**

In `internal/config/config.go`, remove from `ScanConfig`:
```go
	ObservationWindow string `yaml:"observation_window"`
```

Remove from the defaults in `Load()`:
```go
			ObservationWindow: "24h",
```

- [ ] **Step 2: Remove from config.example.yml**

Remove the `observation_window` line from `config.example.yml`.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "refactor: remove observation_window from scan config"
```

---

### Task 5: Remove timeseries from frontend

**Files:**
- Delete: `web/src/components/Sparkline.tsx`
- Modify: `web/src/api/client.ts` — remove `timeseries`/`stats` from `IssueListItem`, remove `TimeseriesData`, `fetchTimeseries`, `window` param from `listIssues`
- Modify: `web/src/pages/Dashboard.tsx` — remove Sparkline import, time window selector, Activity/Trend columns, `timeWindow` state
- Modify: `web/src/pages/IssueDetail.tsx` — remove Sparkline import, `fetchTimeseries` import, timeseries state, frequency chart section

- [ ] **Step 1: Delete Sparkline component**

```bash
rm web/src/components/Sparkline.tsx
```

- [ ] **Step 2: Clean up client.ts**

In `web/src/api/client.ts`:

Remove `timeseries` and `stats` from `IssueListItem`:
```typescript
  timeseries?: { timestamp: string; count: number }[];
  stats?: { total: number; peak: number; trend: 'rising' | 'declining' | 'stable' };
```

Remove `TimeseriesData` type (lines 58-63):
```typescript
export interface TimeseriesData {
  buckets: { timestamp: string; count: number }[];
  window: string;
  last_fetched: string;
}
```

Remove `fetchTimeseries` function (lines 64-70):
```typescript
export async function fetchTimeseries(id: string, window?: string): Promise<TimeseriesData> {
  const params = new URLSearchParams();
  if (window) params.set('window', window);
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/timeseries?${params}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}
```

Remove the `window` parameter from `listIssues`:
```typescript
export async function listIssues(status?: string, showIgnored?: boolean): Promise<IssueListItem[]> {
  const params = new URLSearchParams();
  if (status) params.set('status', status);
  if (showIgnored) params.set('show_ignored', 'true');
  const res = await fetch(`${API_BASE}/api/issues?${params}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}
```

- [ ] **Step 3: Clean up Dashboard.tsx**

In `web/src/pages/Dashboard.tsx`:

Remove the `Sparkline` import:
```typescript
import { Sparkline } from '../components/Sparkline';
```

Remove `timeWindow` state:
```typescript
  const [timeWindow, setTimeWindow] = useState('24h');
```

Update `fetchIssues` to not pass `timeWindow`:
```typescript
  const fetchIssues = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const data = await listIssues(filter === 'all' ? undefined : filter, showIgnored);
      setIssues(data);
      if (!silent) setSelectedIds(new Set());
    } catch (err) {
      console.error('Failed to fetch issues:', err);
    } finally {
      if (!silent) setLoading(false);
    }
  }, [filter, showIgnored]);
```

Remove the time window `Select` from the toolbar (the block with `value={timeWindow}`):
```tsx
          <Select value={timeWindow} onValueChange={setTimeWindow}>
            <SelectTrigger className="w-24 h-7 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1h">1h</SelectItem>
              <SelectItem value="6h">6h</SelectItem>
              <SelectItem value="24h">24h</SelectItem>
              <SelectItem value="7d">7d</SelectItem>
              <SelectItem value="30d">30d</SelectItem>
            </SelectContent>
          </Select>
```

Update the grid template to remove Activity and Trend columns. Change from 11 columns:
```
grid-cols-[32px_2fr_1fr_80px_0.6fr_0.6fr_0.6fr_0.5fr_0.5fr_0.6fr_0.6fr]
```
to 9 columns:
```
grid-cols-[32px_2fr_1fr_0.6fr_0.6fr_0.5fr_0.5fr_0.6fr_0.6fr]
```

Apply this to all three places: header row, main row, and the expanded row div.

Remove the Activity and Trend header cells:
```tsx
            <span>Activity</span>
            <span>Trend</span>
```

Remove the Activity cell (the Sparkline rendering):
```tsx
                <span>
                  {issue.timeseries && issue.timeseries.length > 0 ? (
                    <Sparkline data={issue.timeseries} trend={issue.stats?.trend} />
                  ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                  )}
                </span>
```

Remove the Trend cell:
```tsx
                <span>
                  {issue.stats ? (
                    <span className={`text-xs font-medium ${
                      issue.stats.trend === 'rising' ? 'text-red-400' :
                      issue.stats.trend === 'declining' ? 'text-green-400' :
                      'text-muted-foreground'
                    }`}>
                      {issue.stats.trend === 'rising' ? '↑' :
                       issue.stats.trend === 'declining' ? '↓' : '→'}
                      {' '}{issue.stats.total}
                    </span>
                  ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                  )}
                </span>
```

- [ ] **Step 4: Clean up IssueDetail.tsx**

In `web/src/pages/IssueDetail.tsx`:

Remove `fetchTimeseries` and `TimeseriesData` from imports:
```typescript
import {
  getIssue,
  triggerInvestigate,
  subscribeProgress,
  fetchMRStatus,
  type IssueDetail as IssueDetailType,
} from '../api/client';
```

Remove the `Sparkline` import:
```typescript
import { Sparkline } from '../components/Sparkline';
```

Remove `timeseries` state:
```typescript
  const [timeseries, setTimeseries] = useState<TimeseriesData | null>(null);
```

Remove the `fetchTimeseries` useEffect (lines 93-96):
```typescript
  useEffect(() => {
    if (!id) return;
    fetchTimeseries(id).then(setTimeseries).catch(() => {});
  }, [id]);
```

Remove the Error Frequency section (lines 187-206):
```tsx
        {timeseries && timeseries.buckets.length > 0 && (
          <Section title="Error Frequency">
            ...
          </Section>
        )}
```

- [ ] **Step 5: Build frontend**

Run: `cd web && npm run build`
Expected: Build succeeds with no TypeScript errors.

- [ ] **Step 6: Run frontend verification**

Start the dev server and run verify.mjs:
```bash
cd web && npm run dev &
sleep 3
node verify.mjs
kill %1
```
Expected: No React errors.

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "refactor: remove timeseries, sparklines, and time window from frontend"
```

---

### Task 6: Change scan to update-only (no auto-creation)

**Files:**
- Modify: `cmd/scan.go` — change `runScan` to skip creation of new issues, change `runScanWithResults` similarly
- Modify: `cmd/scan_test.go` — update `TestScanCommand_CreatesErrorReports` to expect 0 new issues (nothing gets created), add test that scan updates existing issues

- [ ] **Step 1: Write test for update-only scan behavior**

In `cmd/scan_test.go`, rename `TestScanCommand_CreatesErrorReports` to `TestScanCommand_DoesNotCreateNewIssues` and change it to:

```go
func TestScanCommand_DoesNotCreateNewIssues(t *testing.T) {
	server := newTestScanServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := newTestDDClient(t, server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		},
		Scan: config.ScanConfig{Since: "24h"},
	}

	count, err := runScan(cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 new issues (scan should not create), got %d", count)
	}
	if mgr.Exists("issue-1") {
		t.Error("scan should not create new issue reports")
	}
}
```

Add a new test that scan updates existing issues:

```go
func TestScanCommand_UpdatesExistingIssues(t *testing.T) {
	server := newTestScanServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := newTestDDClient(t, server.URL)

	// Pre-create the issue with stale metadata
	mgr.WriteError("issue-1", "existing error report")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service: "svc-a",
		Title:   "OldTitle",
		Count:   1,
	})

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		},
		Scan: config.ScanConfig{Since: "24h"},
	}

	_, err := runScan(cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta, err := mgr.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Title != "NullPointerException" {
		t.Errorf("expected title updated to NullPointerException, got %q", meta.Title)
	}
	if meta.Count != 10 {
		t.Errorf("expected count updated to 10, got %d", meta.Count)
	}
}
```

- [ ] **Step 2: Run tests to see them fail**

Run: `go test ./cmd/ -run "TestScanCommand_DoesNotCreate|TestScanCommand_Updates" -v`
Expected: `TestScanCommand_DoesNotCreateNewIssues` FAILS (scan currently creates issues).

- [ ] **Step 3: Modify runScan to skip creation**

In `cmd/scan.go`, change `runScan` (line 81+). Remove the `else` branch that creates new issues. The function should only update existing issues:

```go
func runScan(cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) (int, error) {
	issues, err := ddClient.SearchErrorIssues(cfg.Datadog.Services, cfg.Scan.Since)
	if err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}

	updated := 0
	for _, issue := range issues {
		if !mgr.Exists(issue.ID) {
			continue
		}

		eventsURL := buildEventsURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.Attributes.Service, issue.Attributes.Env, issue.Attributes.FirstSeen, issue.Attributes.LastSeen)
		tracesURL := buildTracesURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.Attributes.Service, issue.Attributes.Env, issue.Attributes.FirstSeen, issue.Attributes.LastSeen)
		datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.ID)

		meta, err := mgr.ReadMetadata(issue.ID)
		if err != nil {
			log.Printf("scan: reading meta for %s: %v", issue.ID, err)
			continue
		}
		meta.Title = issue.Attributes.Title
		meta.Message = issue.Attributes.Message
		meta.Service = issue.Attributes.Service
		meta.Env = issue.Attributes.Env
		meta.FirstSeen = issue.Attributes.FirstSeen
		meta.LastSeen = issue.Attributes.LastSeen
		meta.Count = issue.Attributes.Count
		meta.DatadogURL = datadogURL
		meta.DatadogEventsURL = eventsURL
		meta.DatadogTraceURL = tracesURL
		if err := mgr.WriteMetadata(issue.ID, meta); err != nil {
			log.Printf("scan: updating meta for %s: %v", issue.ID, err)
		}
		updated++
	}

	refreshCIStatuses(cfg, mgr)
	return updated, nil
}
```

- [ ] **Step 4: Modify runScanWithResults similarly**

In `cmd/scan.go`, simplify `runScanWithResults` to match — only process existing issues:

```go
func runScanWithResults(cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) (int, []syncer.ScanResult, error) {
	issues, err := ddClient.SearchErrorIssues(cfg.Datadog.Services, cfg.Scan.Since)
	if err != nil {
		return 0, nil, fmt.Errorf("scan: %w", err)
	}

	var results []syncer.ScanResult
	updated := 0

	for _, issue := range issues {
		if !mgr.Exists(issue.ID) {
			continue
		}

		eventsURL := buildEventsURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.Attributes.Service, issue.Attributes.Env, issue.Attributes.FirstSeen, issue.Attributes.LastSeen)
		tracesURL := buildTracesURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.Attributes.Service, issue.Attributes.Env, issue.Attributes.FirstSeen, issue.Attributes.LastSeen)
		datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issue.ID)

		meta, err := mgr.ReadMetadata(issue.ID)
		if err != nil {
			log.Printf("scan: reading meta for %s: %v", issue.ID, err)
			continue
		}
		meta.Title = issue.Attributes.Title
		meta.Message = issue.Attributes.Message
		meta.Service = issue.Attributes.Service
		meta.Env = issue.Attributes.Env
		meta.FirstSeen = issue.Attributes.FirstSeen
		meta.LastSeen = issue.Attributes.LastSeen
		meta.Count = issue.Attributes.Count
		meta.DatadogURL = datadogURL
		meta.DatadogEventsURL = eventsURL
		meta.DatadogTraceURL = tracesURL
		if err := mgr.WriteMetadata(issue.ID, meta); err != nil {
			log.Printf("scan: updating meta for %s: %v", issue.ID, err)
		}
		updated++

		results = append(results, syncer.ScanResult{
			IssueID:       issue.ID,
			Service:       issue.Attributes.Service,
			Env:           issue.Attributes.Env,
			FirstSeen:     issue.Attributes.FirstSeen,
			LastSeen:      issue.Attributes.LastSeen,
			HasStacktrace: false, // we don't get stacktrace from search
		})
	}

	refreshCIStatuses(cfg, mgr)
	return updated, results, nil
}
```

The `loadErrorTemplate` function and `errorReportData` struct can stay — they'll be reused by the import command.

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/ -v`
Expected: All pass, including `TestScanCommand_SkipsExistingIssues` (which now tests that scan updates but returns 0 because the count semantics changed — actually this test pre-creates the issue, so `updated` will be 1 now). 

Wait — `TestScanCommand_SkipsExistingIssues` expects `count == 0` because the old code counted only _new_ issues. With the new code, it will count _updated_ issues, so the pre-existing issue-1 will be counted. Update this test to expect `count == 1` (it's an update):

```go
	if count != 1 {
		t.Errorf("expected 1 updated issue, got %d", count)
	}
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor: change scan to update-only, no auto-creation of new issues"
```

---

### Task 7: Add import CLI command

**Files:**
- Create: `cmd/import.go`
- Create: `cmd/import_test.go`

- [ ] **Step 1: Write failing test for import**

Create `cmd/import_test.go`:

```go
package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

func newTestImportServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "spans") {
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}})
			return
		}
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "issue-abc",
					"type": "issues_search_result",
					"attributes": map[string]interface{}{
						"total_count": 42,
					},
				},
			},
			"included": []map[string]interface{}{
				{
					"id":   "issue-abc",
					"type": "issue",
					"attributes": map[string]interface{}{
						"error_type":    "TimeoutError",
						"error_message": "request timed out",
						"service":       "svc-a",
						"state":         "OPEN",
						"first_seen":    int64(1711324800000),
						"last_seen":     int64(1711411200000),
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestImportIssue_Success(t *testing.T) {
	server := newTestImportServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := newTestDDClient(t, server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		},
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: "/tmp/svc-a"},
		},
	}

	err := runImport("issue-abc", cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mgr.Exists("issue-abc") {
		t.Fatal("expected issue to be created")
	}

	meta, err := mgr.ReadMetadata("issue-abc")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Title != "TimeoutError" {
		t.Errorf("title: got %q, want TimeoutError", meta.Title)
	}
	if meta.Service != "svc-a" {
		t.Errorf("service: got %q, want svc-a", meta.Service)
	}
}

func TestImportIssue_ServiceNotConfigured(t *testing.T) {
	server := newTestImportServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := newTestDDClient(t, server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		},
		Repositories: map[string]config.RepoConfig{}, // no repos configured
	}

	err := runImport("issue-abc", cfg, ddClient, mgr)
	if err == nil {
		t.Fatal("expected error for unconfigured service")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}
}

func TestImportIssue_AlreadyExists(t *testing.T) {
	server := newTestImportServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	mgr.WriteError("issue-abc", "existing")
	ddClient := newTestDDClient(t, server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		},
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: "/tmp/svc-a"},
		},
	}

	err := runImport("issue-abc", cfg, ddClient, mgr)
	if err == nil {
		t.Fatal("expected error for already-existing issue")
	}
	if !strings.Contains(err.Error(), "already imported") {
		t.Errorf("expected 'already imported' error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run "TestImport" -v`
Expected: FAIL — `runImport` not defined.

- [ ] **Step 3: Implement import command**

Create `cmd/import.go`:

```go
package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import <issue-id>",
	Short: "Import a Datadog error tracking issue into Fido",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		ddClient, err := datadog.NewClient(
			cfg.Datadog.Token,
			cfg.Datadog.Site,
			cfg.Datadog.OrgSubdomain,
		)
		if err != nil {
			return err
		}
		ddClient.SetVerbose(verbose)

		if err := runImport(issueID, cfg, ddClient, mgr); err != nil {
			return err
		}
		fmt.Printf("Successfully imported issue %s\n", issueID)
		return nil
	},
}

func runImport(issueID string, cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) error {
	if mgr.Exists(issueID) {
		return fmt.Errorf("issue %s is already imported", issueID)
	}

	// Fetch all issues for configured services, then find the one we want.
	// The Datadog error tracking API searches by service, not by issue ID directly.
	issues, err := ddClient.SearchErrorIssues(cfg.Datadog.Services, cfg.Scan.Since)
	if err != nil {
		return fmt.Errorf("searching Datadog: %w", err)
	}

	var found *datadog.ErrorIssue
	for i := range issues {
		if issues[i].ID == issueID {
			found = &issues[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("issue %s not found on Datadog (searched services: %v)", issueID, cfg.Datadog.Services)
	}

	service := found.Attributes.Service
	if _, ok := cfg.Repositories[service]; !ok {
		return fmt.Errorf("service %q is not configured in repositories — add it to your config.yml", service)
	}

	// Build URLs
	eventsURL := buildEventsURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, found.Attributes.Service, found.Attributes.Env, found.Attributes.FirstSeen, found.Attributes.LastSeen)
	tracesURL := buildTracesURL(cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, found.Attributes.Service, found.Attributes.Env, found.Attributes.FirstSeen, found.Attributes.LastSeen)
	datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, found.ID)

	// Render error report
	tmpl, err := loadErrorTemplate()
	if err != nil {
		return fmt.Errorf("loading template: %w", err)
	}

	data := errorReportData{
		ID:         found.ID,
		Title:      found.Attributes.Title,
		Message:    found.Attributes.Message,
		Service:    found.Attributes.Service,
		Env:        found.Attributes.Env,
		FirstSeen:  found.Attributes.FirstSeen,
		LastSeen:   found.Attributes.LastSeen,
		Count:      found.Attributes.Count,
		Status:     "",
		StackTrace: found.Attributes.StackTrace,
		DatadogURL: datadogURL,
		EventsURL:  eventsURL,
		TracesURL:  tracesURL,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("rendering error report: %w", err)
	}

	if err := mgr.WriteError(found.ID, buf.String()); err != nil {
		return fmt.Errorf("writing error report: %w", err)
	}

	meta := &reports.MetaData{
		Title:            found.Attributes.Title,
		Message:          found.Attributes.Message,
		Service:          found.Attributes.Service,
		Env:              found.Attributes.Env,
		FirstSeen:        found.Attributes.FirstSeen,
		LastSeen:         found.Attributes.LastSeen,
		Count:            found.Attributes.Count,
		DatadogURL:       datadogURL,
		DatadogEventsURL: eventsURL,
		DatadogTraceURL:  tracesURL,
	}
	if err := mgr.WriteMetadata(found.ID, meta); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(importCmd)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -run "TestImport" -v`
Expected: All 3 tests pass.

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/import.go cmd/import_test.go && git commit -m "feat: add fido import command for manual issue import"
```

---

### Task 8: Add import API endpoint

**Files:**
- Modify: `internal/api/handlers.go` — add `ImportFunc` type, `SetImportFunc`, `ImportIssue` handler
- Modify: `internal/api/server.go` — add `/api/import` route
- Modify: `internal/api/handlers_test.go` — add import handler tests
- Modify: `cmd/serve.go` — wire up import function

- [ ] **Step 1: Write failing tests for import handler**

Add to `internal/api/handlers_test.go`:

```go
func TestImportIssue_Success(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	h := NewHandlers(mgr, &config.Config{})
	h.SetImportFunc(func(issueID string) error {
		mgr.WriteError(issueID, "imported error")
		mgr.WriteMetadata(issueID, &reports.MetaData{Title: "TestError", Service: "svc-a"})
		return nil
	})

	body := strings.NewReader(`{"issue_id":"issue-new"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/import", body)
	w := httptest.NewRecorder()
	h.ImportIssue(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "imported" {
		t.Errorf("expected status=imported, got %q", resp["status"])
	}
}

func TestImportIssue_ValidationError(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	h := NewHandlers(mgr, &config.Config{})
	h.SetImportFunc(func(issueID string) error {
		return fmt.Errorf("service \"svc-x\" is not configured")
	})

	body := strings.NewReader(`{"issue_id":"issue-bad"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/import", body)
	w := httptest.NewRecorder()
	h.ImportIssue(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportIssue_MissingID(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	h := NewHandlers(mgr, &config.Config{})

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/import", body)
	w := httptest.NewRecorder()
	h.ImportIssue(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run "TestImportIssue" -v`
Expected: FAIL — `ImportIssue` not defined.

- [ ] **Step 3: Add import handler to handlers.go**

In `internal/api/handlers.go`, add the `ImportFunc` type near the other func types (after `FixFunc`):

```go
type ImportFunc func(issueID string) error
```

Add the field to the `Handlers` struct:

```go
	importFn      ImportFunc
```

Add the setter:

```go
func (h *Handlers) SetImportFunc(fn ImportFunc)            { h.importFn = fn }
```

Add the handler:

```go
func (h *Handlers) ImportIssue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IssueID string `json:"issue_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IssueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	if h.importFn == nil {
		writeError(w, http.StatusNotImplemented, "import not configured")
		return
	}
	if err := h.importFn(req.IssueID); err != nil {
		if strings.Contains(err.Error(), "already imported") {
			writeError(w, http.StatusConflict, err.Error())
		} else if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	h.publish(Event{Type: "issue:imported", Payload: map[string]any{"id": req.IssueID}})
	writeJSON(w, http.StatusOK, map[string]string{"status": "imported", "id": req.IssueID})
}
```

- [ ] **Step 4: Add route to server.go**

In `internal/api/server.go`, add inside the `/api` route group:

```go
		r.Post("/import", h.ImportIssue)
```

- [ ] **Step 5: Run handler tests**

Run: `go test ./internal/api/ -run "TestImportIssue" -v`
Expected: All 3 pass.

- [ ] **Step 6: Wire up import in serve.go**

In `cmd/serve.go`, after the `handlers.SetFixFunc(...)` block, add:

```go
		handlers.SetImportFunc(func(issueID string) error {
			return runImport(issueID, cfg, ddClient, mgr)
		})
```

- [ ] **Step 7: Run all tests**

Run: `go test ./...`
Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: add POST /api/import endpoint for web-based issue import"
```

---

### Task 9: Add import UI to Dashboard

**Files:**
- Modify: `web/src/api/client.ts` — add `importIssue` function
- Modify: `web/src/pages/Dashboard.tsx` — add import input in toolbar

- [ ] **Step 1: Add importIssue to client.ts**

In `web/src/api/client.ts`, add after the `triggerScan` function:

```typescript
export async function importIssue(issueId: string): Promise<{ status: string; id: string }> {
  const res = await fetch(`${API_BASE}/api/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ issue_id: issueId }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
    throw new Error(err.error || `API error: ${res.status}`);
  }
  return res.json();
}
```

- [ ] **Step 2: Add import input to Dashboard.tsx**

In `web/src/pages/Dashboard.tsx`:

Add `importIssue` to the imports from `../api/client`:
```typescript
import {
  listIssues,
  triggerScan,
  triggerInvestigate as apiInvestigate,
  ignoreIssue,
  unignoreIssue,
  importIssue,
  type IssueListItem,
  type SSEEvent,
} from '../api/client';
```

Add state for the import input after the existing state declarations:
```typescript
  const [importId, setImportId] = useState('');
  const [importLoading, setImportLoading] = useState(false);
  const [importError, setImportError] = useState<string | null>(null);
```

Add the import handler after `handleScan`:
```typescript
  const handleImport = async () => {
    const id = importId.trim();
    if (!id) return;
    setImportLoading(true);
    setImportError(null);
    try {
      await importIssue(id);
      setImportId('');
      await fetchIssues();
    } catch (err) {
      setImportError(err instanceof Error ? err.message : String(err));
    } finally {
      setImportLoading(false);
    }
  };
```

Add the `issue:imported` SSE event to the `useEventStream` handler (inside the `switch`):
```typescript
      case 'issue:imported' as any:
        fetchIssues(true);
        break;
```

In the toolbar div (the one with "Scan Now" button), add the import input before the Scan Now button:

```tsx
          <div className="flex items-center gap-1">
            <input
              type="text"
              placeholder="Datadog issue ID"
              value={importId}
              onChange={(e) => { setImportId(e.target.value); setImportError(null); }}
              onKeyDown={(e) => { if (e.key === 'Enter') handleImport(); }}
              disabled={importLoading}
              className="h-7 px-2 text-xs rounded border border-border bg-background text-foreground w-48 placeholder:text-muted-foreground"
            />
            <Button size="sm" onClick={handleImport} disabled={importLoading || !importId.trim()} className="h-7 text-xs">
              {importLoading ? 'Importing...' : 'Import'}
            </Button>
          </div>
```

Add error display right after the toolbar div (before the selection bar):

```tsx
      {importError && (
        <div className="px-4 py-2 bg-red-950/30 border-b border-red-900 text-red-400 text-xs flex items-center justify-between">
          <span>{importError}</span>
          <button onClick={() => setImportError(null)} className="text-red-400 hover:text-red-300 ml-2">✕</button>
        </div>
      )}
```

- [ ] **Step 3: Build frontend**

Run: `cd web && npm run build`
Expected: Build succeeds.

- [ ] **Step 4: Run frontend verification**

```bash
cd web && npm run dev &
sleep 3
node verify.mjs
kill %1
```
Expected: No React errors.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: add import input to Dashboard UI"
```

---

### Task 10: Update SSE event types and clean up serve.go

**Files:**
- Modify: `web/src/api/client.ts` — add `issue:imported` to `SSEEvent` type
- Modify: `cmd/serve.go` — update initial scan message (it now returns update count, not new count)

- [ ] **Step 1: Update SSEEvent type**

In `web/src/api/client.ts`, update the `SSEEvent` type:
```typescript
export interface SSEEvent {
  type: 'scan:complete' | 'issue:updated' | 'issue:progress' | 'issue:imported';
  payload: Record<string, any>;
}
```

- [ ] **Step 2: Update serve.go message**

In `cmd/serve.go`, change the initial scan message (line 99):
```go
		fmt.Printf("Initial scan complete: %d issues updated\n", count)
```

And the background scan message in the adapter callback (line 107):
```go
			fmt.Printf("Background scan complete: %d issues updated\n", c)
```

- [ ] **Step 3: Fix Dashboard SSE handler for issue:imported**

In `web/src/pages/Dashboard.tsx`, the `issue:imported` case we added in Task 9 used `as any` cast. Now that the type is updated, clean it up:
```typescript
      case 'issue:imported':
        fetchIssues(true);
        break;
```

- [ ] **Step 4: Build and verify**

```bash
cd web && npm run build
```
Expected: Build succeeds.

Run: `go build -o fido .`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "chore: update SSE event types and scan messages for import workflow"
```

---

### Task 11: Remove dead code and unused imports

**Files:**
- Modify: `cmd/scan.go` — remove `loadErrorTemplate`, `errorReportData` if only used by old scan creation (check: import.go reuses them, so keep)
- Modify: `internal/syncer/adapter.go` — verify no unused imports after bucket removal
- Run full test suite

- [ ] **Step 1: Check for unused imports and dead code**

Run: `go build ./... 2>&1` to catch any compile errors from unused imports.

Check `cmd/scan.go` — `loadErrorTemplate` and `errorReportData` are still used by `cmd/import.go` (same package). The `syncer` import in `scan.go` is still used by `runScanWithResults` returning `[]syncer.ScanResult`. Keep it.

Check `internal/syncer/adapter.go` — after removing bucket methods, verify `"time"` import is removed and `"github.com/ruter-as/fido/internal/reports"` is only used if needed. The `reports` package is used in `SaveStacktrace` (calls `a.mgr.ReadError` and `a.mgr.WriteError` — these are `*reports.Manager` methods). The `reports` import is used for the struct type. Actually check — `a.mgr` is `*reports.Manager` which is declared in the struct. The import for `reports` is needed. But `time` is only needed if any method references it — `SaveBuckets` was removed, check others. None use `time`. Remove `"time"`.

Fix any issues found.

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: All pass.

- [ ] **Step 3: Build binary**

Run: `go build -o fido .`
Expected: Builds successfully.

- [ ] **Step 4: Commit if any cleanup was needed**

```bash
git add -A && git commit -m "chore: remove unused imports and dead code after timeseries removal"
```

---

### Task 12: Integration verification

- [ ] **Step 1: Build and start server**

```bash
go build -o fido . && ./fido serve &
```

Wait for "Fido server listening on :8080".

- [ ] **Step 2: Verify import endpoint**

```bash
curl -s -X POST localhost:8080/api/import -H 'Content-Type: application/json' -d '{"issue_id":"test-nonexistent"}' | jq .
```

Expected: Error response (either "not found" or "not configured" depending on whether the ID exists in Datadog).

- [ ] **Step 3: Verify list endpoint has no timeseries**

```bash
curl -s localhost:8080/api/issues | jq '.[0] | keys'
```

Expected: No `timeseries` or `stats` keys in response.

- [ ] **Step 4: Verify timeseries endpoint is gone**

```bash
curl -s localhost:8080/api/issues/any-id/timeseries
```

Expected: 404 (route not found).

- [ ] **Step 5: Stop server and commit**

```bash
kill $(pgrep -f './fido serve')
```

No commit needed — this was verification only.
