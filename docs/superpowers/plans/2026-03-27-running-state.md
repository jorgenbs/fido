# Running State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface in-flight operation state (investigate/fix) through the API so the dashboard shows a pulsing badge and IssueDetail auto-reconnects to a running operation after navigate/refresh.

**Architecture:** Three backend changes (store op-type string in `h.running`, expose `running_op` on both API responses, fix `StreamProgress` idle vs complete distinction) followed by three frontend changes (TS types + idle SSE handling, IssueDetail auto-reconnect, Dashboard pulsing badge).

**Tech Stack:** Go 1.25, chi router, sync.Map, React 19, TypeScript, Tailwind CSS.

---

## File Map

**Modified (backend):**
- `internal/api/handlers.go` — TriggerInvestigate/TriggerFix store op string; IssueDetail/IssueListItem get RunningOp field; GetIssue/ListIssues populate it; StreamProgress checks stage for idle vs complete
- `internal/api/handlers_test.go` — tests for RunningOp in responses, StreamProgress idle/complete

**Modified (frontend):**
- `web/src/api/client.ts` — add `running_op: string` to IssueDetail and IssueListItem interfaces
- `web/src/pages/IssueDetail.tsx` — handle `"idle"` SSE status; auto-reconnect in initial useEffect
- `web/src/pages/Dashboard.tsx` — pulsing badge in stage column

---

## Task 1: Store op-type in running map + `running_op` in API responses

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`

The `h.running` sync.Map currently stores `(issueID → bool)`. This task changes the value to a string (`"investigate"` or `"fix"`) and surfaces it as `running_op` in both API response structs.

- [ ] **Step 1: Write failing tests in `internal/api/handlers_test.go`**

Append after the existing tests:

```go
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/api/... -v -run "TestGetIssue_RunningOp|TestListIssues_RunningOp"
```

Expected: FAIL — `resp.RunningOp` field does not exist on `IssueDetail` / `IssueListItem`.

- [ ] **Step 3: Add `RunningOp` to both structs in `internal/api/handlers.go`**

In the `IssueDetail` struct (around line 37), add:
```go
type IssueDetail struct {
	ID            string               `json:"id"`
	Stage         string               `json:"stage"`
	Error         string               `json:"error"`
	Investigation *string              `json:"investigation"`
	Fix           *string              `json:"fix"`
	Resolve       *reports.ResolveData `json:"resolve"`
	CIStatus      string               `json:"ci_status,omitempty"`
	CIURL         string               `json:"ci_url,omitempty"`
	RunningOp     string               `json:"running_op,omitempty"`
}
```

In the `IssueListItem` struct (around line 20), add `RunningOp`:
```go
type IssueListItem struct {
	ID          string  `json:"id"`
	Stage       string  `json:"stage"`
	Title       string  `json:"title,omitempty"`
	Message     string  `json:"message,omitempty"`
	Service     string  `json:"service,omitempty"`
	LastSeen    string  `json:"last_seen,omitempty"`
	Count       int64   `json:"count,omitempty"`
	MRURL       *string `json:"mr_url"`
	Ignored     bool    `json:"ignored"`
	CIStatus    string  `json:"ci_status,omitempty"`
	CIURL       string  `json:"ci_url,omitempty"`
	Confidence  string  `json:"confidence,omitempty"`
	Complexity  string  `json:"complexity,omitempty"`
	CodeFixable string  `json:"code_fixable,omitempty"`
	RunningOp   string  `json:"running_op,omitempty"`
}
```

- [ ] **Step 4: Change TriggerInvestigate to store op-type string**

Find this line in `TriggerInvestigate` (around line 176):
```go
if _, loaded := h.running.LoadOrStore(id, true); loaded {
```
Change to:
```go
if _, loaded := h.running.LoadOrStore(id, "investigate"); loaded {
```

- [ ] **Step 5: Change TriggerFix to store op-type string**

Find this line in `TriggerFix` (around line 203):
```go
if _, loaded := h.running.LoadOrStore(id, true); loaded {
```
Change to:
```go
if _, loaded := h.running.LoadOrStore(id, "fix"); loaded {
```

- [ ] **Step 6: Populate RunningOp in GetIssue**

In `GetIssue`, after the line `detail.CIURL = meta.CIURL` (around line 156), add:

```go
if op, ok := h.running.Load(id); ok {
    detail.RunningOp = op.(string)
}
```

- [ ] **Step 7: Populate RunningOp in ListIssues**

In `ListIssues`, after `items = append(items, item)` is built but before the append (inside the for loop, after `item.MRURL` is set), add:

```go
if op, ok := h.running.Load(issue.ID); ok {
    item.RunningOp = op.(string)
}
```

The full item block in `ListIssues` should now end:
```go
		if issue.MRURL != "" {
			item.MRURL = &issue.MRURL
		}
		if op, ok := h.running.Load(issue.ID); ok {
			item.RunningOp = op.(string)
		}
		items = append(items, item)
```

- [ ] **Step 8: Run tests to confirm they pass**

```bash
go test ./internal/api/... -v -run "TestGetIssue_RunningOp|TestListIssues_RunningOp"
```

Expected: all 3 new tests PASS.

- [ ] **Step 9: Run full test suite**

```bash
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat(api): expose running_op in IssueDetail and IssueListItem responses"
```

---

## Task 2: Fix StreamProgress idle vs complete

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`

Currently `StreamProgress` returns `"complete"` whenever nothing is in `h.running`, even for issues that never had an operation. Fix: if stage is `investigated` or `fixed`, return `"complete"` (something did finish); otherwise return `"idle"`.

- [ ] **Step 1: Add flushRecorder helper and failing tests in `internal/api/handlers_test.go`**

Append:

```go
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/api/... -v -run "TestStreamProgress"
```

Expected: FAIL — `TestStreamProgress_IdleWhenNothingRan` gets `"complete"` but wants `"idle"`.

- [ ] **Step 3: Update StreamProgress in `internal/api/handlers.go`**

Find the not-running branch (around line 345):
```go
		if _, running := h.running.Load(id); !running {
			drainLog() // flush remaining output before complete
			fmt.Fprintf(w, "data: {\"status\":\"complete\"}\n\n")
			flusher.Flush()
			return
		}
```

Replace with:
```go
		if _, running := h.running.Load(id); !running {
			drainLog()
			stage := h.reports.Stage(id)
			if stage == reports.StageInvestigated || stage == reports.StageFixed {
				fmt.Fprintf(w, "data: {\"status\":\"complete\"}\n\n")
			} else {
				fmt.Fprintf(w, "data: {\"status\":\"idle\"}\n\n")
			}
			flusher.Flush()
			return
		}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/api/... -v -run "TestStreamProgress"
```

Expected: both tests PASS.

- [ ] **Step 5: Run full test suite**

```bash
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "fix(api): StreamProgress returns idle instead of complete when no operation has run"
```

---

## Task 3: TypeScript types + idle SSE handling

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/IssueDetail.tsx`

Add `running_op` to TS interfaces and handle the new `"idle"` SSE status so it doesn't leave the UI stuck in a running state after a server restart.

- [ ] **Step 1: Add `running_op` to both interfaces in `web/src/api/client.ts`**

In `IssueListItem`, add after `code_fixable`:
```typescript
export interface IssueListItem {
  id: string;
  stage: string;
  title: string;
  message: string;
  service: string;
  last_seen: string;
  count: number;
  mr_url: string | null;
  ignored: boolean;
  ci_status: string;
  ci_url: string;
  confidence: string;
  complexity: string;
  code_fixable: string;
  running_op: string;
}
```

In `IssueDetail`, add after `ci_url`:
```typescript
export interface IssueDetail {
  id: string;
  stage: string;
  error: string;
  investigation: string | null;
  fix: string | null;
  resolve: ResolveData | null;
  ci_status: string;
  ci_url: string;
  running_op: string;
}
```

- [ ] **Step 2: Handle `"idle"` status in `startSSE` in `web/src/pages/IssueDetail.tsx`**

Find `startSSE` (around line 79). The current callback handles `complete` and `error`. Add an `idle` branch:

```tsx
  const startSSE = (onComplete: () => void) => {
    if (!id) return;
    sseRef.current?.close();
    setProgressLog('');
    sseRef.current = subscribeProgress(id, (data) => {
      if (data.log) {
        setProgressLog((prev) => prev + data.log);
      }
      if (data.status === 'complete') {
        sseRef.current?.close();
        setProgressLog('');
        onComplete();
      } else if (data.status === 'error') {
        sseRef.current?.close();
        setErrorMsg(data.message ?? 'Unknown error');
        setInvestigateState('error');
        setFixState('idle');
      } else if (data.status === 'idle') {
        sseRef.current?.close();
        setInvestigateState('idle');
        setFixState('idle');
      }
    });
  };
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts web/src/pages/IssueDetail.tsx
git commit -m "feat(frontend): add running_op to TS types; handle idle SSE status"
```

---

## Task 4: IssueDetail auto-reconnect on load

**Files:**
- Modify: `web/src/pages/IssueDetail.tsx`

When the page loads and `running_op` is set (an operation is already in flight), automatically set the appropriate running state and connect to the SSE stream. The buffered output from `progressBuf` will be replayed from the start (`lastSent` resets to 0 on each new connection).

- [ ] **Step 1: Replace the initial `useEffect` in `web/src/pages/IssueDetail.tsx`**

Find the first `useEffect` (around line 44):
```tsx
  useEffect(() => {
    fetchIssue();
    return () => {
      sseRef.current?.close();
      if (copyTimeoutRef.current) clearTimeout(copyTimeoutRef.current);
    };
  }, [id]);
```

Replace with:
```tsx
  useEffect(() => {
    if (!id) return;
    setLoading(true);
    getIssue(id)
      .then((data) => {
        setIssue(data);
        setLoading(false);
        if (data.running_op === 'investigate') {
          setInvestigateState('running');
          startSSE(() => { setInvestigateState('idle'); fetchIssue(); });
        } else if (data.running_op === 'fix') {
          setFixState('running');
          startSSE(() => { setFixState('idle'); fetchIssue(); });
        }
      })
      .catch((err) => {
        console.error('Failed to fetch issue:', err);
        setLoading(false);
      });
    return () => {
      sseRef.current?.close();
      if (copyTimeoutRef.current) clearTimeout(copyTimeoutRef.current);
    };
  }, [id]);
```

Note: `fetchIssue` is still used by the MR polling effect and SSE-complete callbacks — leave it unchanged.

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Verify frontend with Playwright**

```bash
npm run dev > /tmp/vite.log 2>&1 &
sleep 4 && node verify.mjs
kill %1
```

Expected: exits 0, no React errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/IssueDetail.tsx
git commit -m "feat: auto-reconnect SSE on IssueDetail load when operation is running"
```

---

## Task 5: Dashboard pulsing badge

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

Replace the static `<StageIndicator>` in the stage column with a pulsing "Investigating…" / "Fixing…" badge when `running_op` is set.

- [ ] **Step 1: Update the stage column in `web/src/pages/Dashboard.tsx`**

Find the stage column span in the row grid (around line 276):
```tsx
                <span>
                  <StageIndicator stage={issue.stage} />
                </span>
```

Replace with:
```tsx
                <span>
                  {issue.running_op ? (
                    <span className="inline-flex items-center gap-1 text-blue-400 text-xs font-medium">
                      <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
                      {issue.running_op === 'investigate' ? 'Investigating…' : 'Fixing…'}
                    </span>
                  ) : (
                    <StageIndicator stage={issue.stage} />
                  )}
                </span>
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Verify frontend with Playwright**

```bash
npm run dev > /tmp/vite.log 2>&1 &
sleep 4 && node verify.mjs
kill %1
```

Expected: exits 0, no React errors.

- [ ] **Step 4: Rebuild binary and verify live API**

```bash
cd .. && go build -o fido .
kill $(pgrep -f './fido serve') 2>/dev/null; ./fido serve &
sleep 1
# With a real running issue, running_op should appear:
curl -s localhost:8080/api/issues | jq '.[0].running_op'
# For an idle issue, should be null/omitted:
curl -s localhost:8080/api/issues/<real-id> | jq '.running_op'
# Progress on a scanned-only issue should now return idle:
curl -s localhost:8080/api/issues/<real-id>/progress
```

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/Dashboard.tsx
git commit -m "feat: pulsing Investigating/Fixing badge in dashboard stage column"
```

---

## Self-Review

**Spec coverage:**
1. ✅ `StreamProgress` idle vs complete — Task 2
2. ✅ `running_op` in `/api/issues` list — Task 1 (IssueListItem)
3. ✅ `running_op` in `/api/issues/{id}` detail — Task 1 (IssueDetail)
4. ✅ IssueDetail auto-reconnect — Task 4
5. ✅ Dashboard pulsing badge — Task 5
6. ✅ TS types updated — Task 3
7. ✅ Idle SSE status handled — Task 3

**Placeholder scan:** No TBDs. All code blocks complete.

**Type consistency:**
- `RunningOp string` in Go struct → `json:"running_op,omitempty"` → `running_op: string` in TS — consistent across Tasks 1, 3, 4, 5.
- `h.running.Store(id, "investigate")` in Task 1 → `op.(string)` cast in GetIssue/ListIssues in Task 1 — consistent.
- `reports.StageInvestigated`, `reports.StageFixed` in Task 2 — match constants in `internal/reports/manager.go`.
- `startSSE` modified in Task 3, called in Task 4 — same signature.
