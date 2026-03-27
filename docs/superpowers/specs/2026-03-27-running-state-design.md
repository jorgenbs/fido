# Running State — Design Spec

**Date:** 2026-03-27
**Status:** Approved

## Problem

1. Navigating away or refreshing during an investigation loses the running state — the SSE stream is torn down client-side and the frontend has no way to reconnect on return.
2. Calling `GET /api/issues/{id}/progress` on an issue with nothing running returns `{"status":"complete"}`, which is misleading and causes the frontend to behave as if an operation just finished.
3. The `/api/issues` list has no indication of which issues have an active operation, so the dashboard can't show a loading indicator.

## Design

### Backend: `h.running` stores op-type instead of bool

Change `h.running` (sync.Map) value type from `bool` to `string`:

- `TriggerInvestigate`: `h.running.Store(id, "investigate")`
- `TriggerFix`: `h.running.Store(id, "fix")`
- `defer h.running.Delete(id)` unchanged in both handlers

All existing checks (`h.running.Load(id)`) continue to work — presence check is the same; callers that need the op-type cast the value to string.

### Backend: `StreamProgress` — idle vs complete

Current logic fires `"complete"` whenever `h.running` has no entry for the id, including for issues that were never running. New logic:

```
if not running:
    if stage == scanned:   → send {"status":"idle"}
    else:                  → send {"status":"complete"}
    return
```

`stage == scanned` means no `investigation.md` exists → nothing has completed yet (operation never ran, or server restarted before it could finish). Any stage beyond `scanned` means an operation did complete at some point → `"complete"` is accurate.

The file system is the source of truth; no in-memory progressBuf dependency.

Frontend must handle the new `"idle"` status: close the SSE connection and leave the UI in its current state (no refetch, no state change).

### Backend: `running_op` field on API responses

Add `RunningOp string \`json:"running_op,omitempty"\`` to both `IssueDetail` and `IssueListItem`.

Populated in `GetIssue` and `ListIssues` by checking `h.running.Load(id)`:
- `"investigate"` — investigation in progress
- `"fix"` — fix in progress
- omitted / `""` — nothing running

No new endpoints. No persistent state written to disk.

### Frontend: IssueDetail auto-reconnect

The initial `useEffect` currently calls `fetchIssue()` and does nothing else. After the change, the initial fetch is inlined so it can branch on `running_op`:

```
init():
  data = getIssue(id)
  setIssue(data)
  setLoading(false)
  if data.running_op === 'investigate':
    setInvestigateState('running')
    startSSE(onComplete: setInvestigateState('idle'), fetchIssue())
  else if data.running_op === 'fix':
    setFixState('running')
    startSSE(onComplete: setFixState('idle'), fetchIssue())
```

The regular `fetchIssue` (used from SSE-complete callbacks and MR polling) is unchanged — it does not attempt reconnect, since those call sites already have an SSE in flight or don't need one.

The `subscribeProgress` client already handles the full log replay naturally: `lastSent` starts at 0 on each new SSE connection, and `progressBuf` is never cleared on the backend, so the full buffered output is retransmitted on reconnect.

Handle the new `"idle"` SSE status in the client's `onmessage`: close the EventSource, do nothing else.

### Frontend: Dashboard pulsing badge

In the stage column of `Dashboard.tsx`, replace `<StageIndicator>` with:

```tsx
{issue.running_op ? (
    <span className="inline-flex items-center gap-1 text-blue-400 text-xs font-medium">
        <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
        {issue.running_op === 'investigate' ? 'Investigating…' : 'Fixing…'}
    </span>
) : (
    <StageIndicator stage={issue.stage} />
)}
```

## Affected Files

**Backend:**
- `internal/api/handlers.go` — TriggerInvestigate, TriggerFix, StreamProgress, GetIssue, ListIssues, IssueDetail struct, IssueListItem struct
- `internal/api/handlers_test.go` — tests for StreamProgress idle/complete, running_op in responses

**Frontend:**
- `web/src/api/client.ts` — add `running_op: string` to IssueDetail and IssueListItem interfaces; handle `"idle"` status in subscribeProgress
- `web/src/pages/IssueDetail.tsx` — auto-reconnect in initial useEffect
- `web/src/pages/Dashboard.tsx` — pulsing badge in stage column

## What this does NOT change

- The stage system (`scanned` / `investigated` / `fixed`) is file-based and unchanged.
- No persistent running state is written to disk. If the server restarts mid-operation, the operation is lost; the issue stays at its last committed stage.
- The `/api/issues/{id}/mr-status` polling is unchanged.
- The `progressBufs` cleanup (or lack thereof) is unchanged — out of scope.
