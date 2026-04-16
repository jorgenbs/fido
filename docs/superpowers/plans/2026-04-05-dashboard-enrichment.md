# Dashboard Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add sparklines, time window selector, trend indicators, and an issue detail chart so the dashboard shows error frequency at a glance.

**Architecture:** Backend includes timeseries data inline in the issues list response (approach C). Frontend computes sparkline paths and stats from the inline data. A new `Sparkline` component renders inline SVG. The time window selector lives in the toolbar and propagates via query param.

**Tech Stack:** Go (chi), React, TypeScript, Tailwind CSS, shadcn/ui, inline SVG

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `internal/api/handlers.go` | Add `window` query param to `ListIssues`, include inline timeseries + stats |
| Modify | `internal/api/handlers_test.go` | Test inline timeseries in list response |
| Create | `internal/api/timeseries_stats.go` | Pure functions: compute trend, total count, peak from buckets |
| Create | `internal/api/timeseries_stats_test.go` | Unit tests for stat computation |
| Modify | `web/src/api/client.ts` | Add `window` param to `listIssues`, extend `IssueListItem` type |
| Create | `web/src/components/Sparkline.tsx` | Inline SVG sparkline component (~80x20px) |
| Modify | `web/src/pages/Dashboard.tsx` | Add time window selector to toolbar, render sparklines + trend badges |
| Modify | `web/src/pages/IssueDetail.tsx` | Add larger timeseries chart section |

---

### Task 1: Backend — Timeseries Stats Computation

**Files:**
- Create: `internal/api/timeseries_stats.go`
- Create: `internal/api/timeseries_stats_test.go`

Pure functions that take a bucket slice and return computed stats. Isolated from HTTP concerns.

- [ ] **Step 1: Write the failing test**

Create `internal/api/timeseries_stats_test.go`:

```go
package api

import (
	"testing"

	"github.com/jorgenbs/fido/internal/reports"
)

func TestComputeTimeseriesStats_Rising(t *testing.T) {
	buckets := []reports.Bucket{
		{Timestamp: "2026-04-05T00:00:00Z", Count: 1},
		{Timestamp: "2026-04-05T01:00:00Z", Count: 2},
		{Timestamp: "2026-04-05T02:00:00Z", Count: 5},
		{Timestamp: "2026-04-05T03:00:00Z", Count: 10},
	}
	stats := computeTimeseriesStats(buckets)

	if stats.Total != 18 {
		t.Errorf("Total = %d, want 18", stats.Total)
	}
	if stats.Peak != 10 {
		t.Errorf("Peak = %d, want 10", stats.Peak)
	}
	if stats.Trend != "rising" {
		t.Errorf("Trend = %q, want \"rising\"", stats.Trend)
	}
}

func TestComputeTimeseriesStats_Declining(t *testing.T) {
	buckets := []reports.Bucket{
		{Timestamp: "2026-04-05T00:00:00Z", Count: 10},
		{Timestamp: "2026-04-05T01:00:00Z", Count: 5},
		{Timestamp: "2026-04-05T02:00:00Z", Count: 2},
		{Timestamp: "2026-04-05T03:00:00Z", Count: 1},
	}
	stats := computeTimeseriesStats(buckets)

	if stats.Trend != "declining" {
		t.Errorf("Trend = %q, want \"declining\"", stats.Trend)
	}
}

func TestComputeTimeseriesStats_Stable(t *testing.T) {
	buckets := []reports.Bucket{
		{Timestamp: "2026-04-05T00:00:00Z", Count: 5},
		{Timestamp: "2026-04-05T01:00:00Z", Count: 5},
		{Timestamp: "2026-04-05T02:00:00Z", Count: 5},
		{Timestamp: "2026-04-05T03:00:00Z", Count: 5},
	}
	stats := computeTimeseriesStats(buckets)

	if stats.Trend != "stable" {
		t.Errorf("Trend = %q, want \"stable\"", stats.Trend)
	}
	if stats.Total != 20 {
		t.Errorf("Total = %d, want 20", stats.Total)
	}
}

func TestComputeTimeseriesStats_Empty(t *testing.T) {
	stats := computeTimeseriesStats(nil)

	if stats.Total != 0 {
		t.Errorf("Total = %d, want 0", stats.Total)
	}
	if stats.Trend != "stable" {
		t.Errorf("Trend = %q, want \"stable\"", stats.Trend)
	}
}

func TestComputeTimeseriesStats_SingleBucket(t *testing.T) {
	buckets := []reports.Bucket{
		{Timestamp: "2026-04-05T00:00:00Z", Count: 42},
	}
	stats := computeTimeseriesStats(buckets)

	if stats.Total != 42 {
		t.Errorf("Total = %d, want 42", stats.Total)
	}
	if stats.Peak != 42 {
		t.Errorf("Peak = %d, want 42", stats.Peak)
	}
	if stats.Trend != "stable" {
		t.Errorf("Trend = %q, want \"stable\"", stats.Trend)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestComputeTimeseriesStats -v`
Expected: FAIL — `computeTimeseriesStats` undefined

- [ ] **Step 3: Write the implementation**

Create `internal/api/timeseries_stats.go`:

```go
package api

import "github.com/jorgenbs/fido/internal/reports"

type timeseriesStats struct {
	Total int64  `json:"total"`
	Peak  int64  `json:"peak"`
	Trend string `json:"trend"` // "rising", "declining", "stable"
}

func computeTimeseriesStats(buckets []reports.Bucket) timeseriesStats {
	if len(buckets) == 0 {
		return timeseriesStats{Trend: "stable"}
	}

	var total, peak int64
	for _, b := range buckets {
		total += b.Count
		if b.Count > peak {
			peak = b.Count
		}
	}

	trend := "stable"
	if len(buckets) >= 4 {
		half := len(buckets) / 2
		var firstHalf, secondHalf int64
		for _, b := range buckets[:half] {
			firstHalf += b.Count
		}
		for _, b := range buckets[half:] {
			secondHalf += b.Count
		}
		// Require >25% difference to classify as rising/declining
		if firstHalf > 0 {
			ratio := float64(secondHalf) / float64(firstHalf)
			if ratio > 1.25 {
				trend = "rising"
			} else if ratio < 0.75 {
				trend = "declining"
			}
		} else if secondHalf > 0 {
			trend = "rising"
		}
	}

	return timeseriesStats{Total: total, Peak: peak, Trend: trend}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestComputeTimeseriesStats -v`
Expected: PASS (all 5 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/api/timeseries_stats.go internal/api/timeseries_stats_test.go
git commit -m "feat: add timeseries stats computation (total, peak, trend)"
```

---

### Task 2: Backend — Inline Timeseries in ListIssues

**Files:**
- Modify: `internal/api/handlers.go:20-40` (IssueListItem struct)
- Modify: `internal/api/handlers.go:110-160` (ListIssues handler)

Add `window` query param support and include inline timeseries data + computed stats in the issues list response.

- [ ] **Step 1: Write the failing test**

Add to `internal/api/handlers_test.go`:

```go
func TestListIssues_IncludesTimeseries(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	// Create an issue with timeseries data
	issueID := "test-ts-issue"
	_ = mgr.WriteError(issueID, "## Error\ntest error")
	_ = mgr.WriteMetadata(issueID, &reports.Metadata{
		Title:   "Test Error",
		Service: "test-svc",
	})
	_ = mgr.WriteTimeSeries(issueID, &reports.TimeSeries{
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestListIssues_IncludesTimeseries -v`
Expected: FAIL — `Timeseries` and `Stats` fields don't exist on `IssueListItem`

- [ ] **Step 3: Add fields to IssueListItem**

In `internal/api/handlers.go`, add these fields to the `IssueListItem` struct (after `StackTrace`):

```go
	Timeseries []reports.Bucket  `json:"timeseries,omitempty"`
	Stats      *timeseriesStats  `json:"stats,omitempty"`
```

- [ ] **Step 4: Read window param and populate timeseries in ListIssues**

In `internal/api/handlers.go`, in the `ListIssues` method, after `showIgnored` line:

```go
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "24h"
	}
```

Then inside the `for _, issue := range issues` loop, after the `item.MRURL` assignment block and before `items = append(items, item)`:

```go
		if ts, err := h.reports.ReadTimeSeries(issue.ID); err == nil && ts.Window == window {
			item.Timeseries = ts.Buckets
			stats := computeTimeseriesStats(ts.Buckets)
			item.Stats = &stats
		}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestListIssues_IncludesTimeseries -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: include inline timeseries and stats in issues list response"
```

---

### Task 3: Frontend — API Client Updates

**Files:**
- Modify: `web/src/api/client.ts`

Add `window` parameter to `listIssues` and extend the `IssueListItem` interface with timeseries fields.

- [ ] **Step 1: Add types to IssueListItem**

In `web/src/api/client.ts`, add these fields to the `IssueListItem` interface (after `stack_trace`):

```typescript
  timeseries?: { timestamp: string; count: number }[];
  stats?: { total: number; peak: number; trend: 'rising' | 'declining' | 'stable' };
```

- [ ] **Step 2: Add window param to listIssues**

Update the `listIssues` function signature and body:

```typescript
export async function listIssues(status?: string, showIgnored?: boolean, window?: string): Promise<IssueListItem[]> {
  const params = new URLSearchParams();
  if (status) params.set('status', status);
  if (showIgnored) params.set('show_ignored', 'true');
  if (window) params.set('window', window);
  const res = await fetch(`${API_BASE}/api/issues?${params}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}
```

- [ ] **Step 3: Add fetchTimeseries function for detail page**

Add below the `listIssues` function:

```typescript
export interface TimeseriesData {
  buckets: { timestamp: string; count: number }[];
  window: string;
  last_fetched: string;
}

export async function fetchTimeseries(id: string, window?: string): Promise<TimeseriesData> {
  const params = new URLSearchParams();
  if (window) params.set('window', window);
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/timeseries?${params}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}
```

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat: add window param and timeseries types to API client"
```

---

### Task 4: Frontend — Sparkline Component

**Files:**
- Create: `web/src/components/Sparkline.tsx`

A small inline SVG component that renders a sparkline from bucket data.

- [ ] **Step 1: Create the Sparkline component**

Create `web/src/components/Sparkline.tsx`:

```tsx
interface SparklineProps {
  data: { count: number }[];
  width?: number;
  height?: number;
  trend?: 'rising' | 'declining' | 'stable';
}

export function Sparkline({ data, width = 80, height = 20, trend }: SparklineProps) {
  if (!data || data.length < 2) {
    return <svg width={width} height={height} className="inline-block" />;
  }

  const counts = data.map(d => d.count);
  const max = Math.max(...counts, 1);
  const padding = 1;

  const points = counts.map((count, i) => {
    const x = padding + (i / (counts.length - 1)) * (width - 2 * padding);
    const y = padding + (1 - count / max) * (height - 2 * padding);
    return `${x},${y}`;
  });

  const strokeColor =
    trend === 'rising'
      ? 'stroke-red-400'
      : trend === 'declining'
        ? 'stroke-green-400'
        : 'stroke-muted-foreground';

  return (
    <svg width={width} height={height} className="inline-block align-middle">
      <polyline
        points={points.join(' ')}
        fill="none"
        className={strokeColor}
        strokeWidth="1.5"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/Sparkline.tsx
git commit -m "feat: add Sparkline inline SVG component"
```

---

### Task 5: Frontend — Time Window Selector + Sparklines in Dashboard

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

Add a time window selector to the toolbar, pass `window` to the API, and render sparklines + trend indicators in the table.

- [ ] **Step 1: Add window state and import Sparkline**

At the top of `Dashboard.tsx`, add the import:

```typescript
import { Sparkline } from '../components/Sparkline';
```

Inside the `Dashboard` function, add state after the existing filter states:

```typescript
const [window, setWindow] = useState('24h');
```

- [ ] **Step 2: Pass window to fetchIssues**

Update the `fetchIssues` callback to pass `window`:

```typescript
const fetchIssues = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const data = await listIssues(filter === 'all' ? undefined : filter, showIgnored, window);
      setIssues(data);
      if (!silent) setSelectedIds(new Set());
    } catch (err) {
      console.error('Failed to fetch issues:', err);
    } finally {
      if (!silent) setLoading(false);
    }
  }, [filter, showIgnored, window]);
```

- [ ] **Step 3: Add time window selector to toolbar**

In the toolbar `<div>` that contains the filter dropdowns, add the window selector before the "Scan Now" button:

```tsx
<Select value={window} onValueChange={setWindow}>
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

- [ ] **Step 4: Add Sparkline and Trend columns to the table**

Update the grid template to add two columns for sparkline and trend. Change from:

```
grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.5fr_0.8fr_0.8fr]
```

To:

```
grid-cols-[32px_2fr_1fr_80px_0.6fr_0.6fr_0.6fr_0.5fr_0.5fr_0.6fr_0.6fr]
```

This applies to the header row and all data rows (3 occurrences total).

Update the header row to include Sparkline and Trend columns (insert after Service):

```tsx
<span>Issue</span>
<span>Service</span>
<span>Activity</span>
<span>Trend</span>
<span>Stage</span>
<span>Confidence</span>
<span>Complexity</span>
<span>Fixable</span>
<span>CI</span>
<span>MR</span>
```

In each data row, add after the Service column:

```tsx
<span>
  {issue.timeseries && issue.timeseries.length > 0 ? (
    <Sparkline data={issue.timeseries} trend={issue.stats?.trend} />
  ) : (
    <span className="text-muted-foreground text-xs">—</span>
  )}
</span>
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

- [ ] **Step 5: Verify with Playwright**

```bash
cd web && npm run dev &
cd web && node verify.mjs
```

Expected: No console errors on Dashboard page.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/Dashboard.tsx
git commit -m "feat: add time window selector, sparklines, and trend indicators to dashboard"
```

---

### Task 6: Frontend — Issue Detail Timeseries Chart

**Files:**
- Modify: `web/src/pages/IssueDetail.tsx`

Add a timeseries chart section to the issue detail page, showing a larger version of the sparkline with axis labels.

- [ ] **Step 1: Import fetchTimeseries and add chart state**

In `IssueDetail.tsx`, add the import:

```typescript
import { fetchTimeseries, type TimeseriesData } from '../api/client';
import { Sparkline } from '../components/Sparkline';
```

Add state inside the component (after the existing state declarations):

```typescript
const [timeseries, setTimeseries] = useState<TimeseriesData | null>(null);
```

- [ ] **Step 2: Fetch timeseries on load**

Add a `useEffect` after the existing ones:

```typescript
useEffect(() => {
  if (!id) return;
  fetchTimeseries(id).then(setTimeseries).catch(() => {});
}, [id]);
```

- [ ] **Step 3: Add the chart section**

Insert a new `Section` between "Error Report" and "Investigation":

```tsx
{timeseries && timeseries.buckets.length > 0 && (
  <Section title="Error Frequency">
    <div className="p-4">
      <div className="flex items-center justify-between mb-2 text-xs text-muted-foreground">
        <span>{new Date(timeseries.buckets[0].timestamp).toLocaleDateString()}</span>
        <span>{timeseries.window} window</span>
        <span>{new Date(timeseries.buckets[timeseries.buckets.length - 1].timestamp).toLocaleDateString()}</span>
      </div>
      <Sparkline
        data={timeseries.buckets}
        width={700}
        height={80}
      />
      <div className="flex gap-4 mt-2 text-xs text-muted-foreground">
        <span>Total: <strong className="text-foreground">{timeseries.buckets.reduce((s, b) => s + b.count, 0)}</strong></span>
        <span>Peak: <strong className="text-foreground">{Math.max(...timeseries.buckets.map(b => b.count))}</strong></span>
      </div>
    </div>
  </Section>
)}
```

- [ ] **Step 4: Verify with Playwright**

```bash
cd web && node verify.mjs
```

Expected: No console errors on IssueDetail page.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/IssueDetail.tsx
git commit -m "feat: add error frequency chart to issue detail page"
```

---

### Task 7: Backend Verification — Curl Test

**Files:** None (verification only)

Verify the enriched API response with a real issue against the running server.

- [ ] **Step 1: Build and restart**

```bash
cd web && npm run build && cd .. && go build -o fido . && kill $(pgrep -f './fido serve') ; ./fido serve &
```

- [ ] **Step 2: Curl the enriched endpoint**

```bash
curl -s localhost:8080/api/issues?window=24h | jq '.[0] | {id, timeseries: (.timeseries | length), stats}'
```

Expected: `timeseries` should be a non-zero number, `stats` should contain `total`, `peak`, `trend`.

- [ ] **Step 3: Curl the detail timeseries endpoint**

```bash
curl -s localhost:8080/api/issues/3b778680-22ef-11f1-ba65-da7ad0900005/timeseries?window=24h | jq '{window, buckets_count: (.buckets | length)}'
```

Expected: `window: "24h"`, `buckets_count` > 0.

- [ ] **Step 4: Commit any fixes if needed**

If curl reveals issues, fix them and add a targeted commit.

---

### Task 8: Frontend Verification — Full Playwright Check

**Files:** None (verification only)

- [ ] **Step 1: Start dev server and verify**

```bash
cd web && npm run dev &
sleep 3
cd web && node verify.mjs
```

Expected: All pages pass, no React errors.

- [ ] **Step 2: Stop dev server**

```bash
kill $(pgrep -f 'vite')
```
