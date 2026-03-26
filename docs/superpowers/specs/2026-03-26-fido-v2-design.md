# fido v2 Design Spec

**Date:** 2026-03-26
**Status:** Approved

## Overview

This iteration improves fido across five areas: richer Datadog context during investigation, structured issue metadata, a redesigned web UI (shadcn/ui, Slate/blue, dark mode), an ignore feature, and a bug fix for investigate not triggering properly via the API.

---

## 1. Data Model

### `meta.json` (new — written at scan time)

Stored alongside `error.md` in `~/.fido/reports/<issueID>/meta.json`.

```json
{
  "title": "NullPointerException in PaymentService",
  "service": "payment-svc",
  "env": "production",
  "first_seen": "2026-03-25T10:00:00Z",
  "last_seen": "2026-03-26T09:00:00Z",
  "count": 47,
  "datadog_url": "https://ruter.datadoghq.eu/error-tracking/issue/<id>",
  "datadog_events_url": "https://ruter.datadoghq.eu/event/explorer?query=service:payment-svc&from=...&to=...",
  "datadog_trace_url": "https://ruter.datadoghq.eu/apm/traces?query=service:payment-svc env:production&start=...&end=...",
  "ignored": false
}
```

Deep-link URLs are pre-computed from known fields at scan time — no extra API call required.

### `resolve.json` (unchanged)

Written after fix. Contains `branch`, `mr_url`, `mr_status`, `service`, `datadog_issue_id`, `datadog_url`, `created_at`.

### `ignored` field

`ignored` is orthogonal to stage — an issue keeps its pipeline stage (`scanned` / `investigated` / `fixed`) and also carries `ignored: bool`. The stage machine in `reports.Manager` is unchanged.

### `IssueListItem` API response (enriched)

```json
{
  "id": "28b7e936-...",
  "stage": "investigated",
  "title": "NullPointerException in PaymentService",
  "service": "payment-svc",
  "last_seen": "2026-03-26T09:00:00Z",
  "count": 47,
  "mr_url": null
}
```

`mr_url` is populated from `resolve.json` when it exists, `null` otherwise.

---

## 2. Backend: Datadog Enrichment at Investigate Time

### New method: `client.FetchIssueContext`

```go
type IssueContext struct {
    Traces     []TraceRef
    EventsURL  string
    TracesURL  string
}

type TraceRef struct {
    TraceID string
    URL     string
}

func (c *Client) FetchIssueContext(service, env, firstSeen, lastSeen string) (IssueContext, error)
```

Called by `runInvestigate` before building the prompt. Uses `SpansApi` to search for spans matching `service:<svc> env:<env>` in the issue's time window (up to 3 results). If the API call fails, returns an empty `IssueContext` — enrichment is best-effort and never blocks investigation.

### Prompt augmentation

The investigation prompt gains two sections when context is available:

```
## Related Traces
- [Trace abc123](https://ruter.datadoghq.eu/apm/trace/abc123)

## Useful Links
- [Events Timeline](https://ruter.datadoghq.eu/event/explorer?...)
- [Trace Waterfall](https://ruter.datadoghq.eu/apm/traces?...)
```

Deep-link URLs are always appended even when the Spans API returns nothing.

---

## 3. Backend: Ignore Feature

### New API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/issues/{id}/ignore` | Sets `ignored: true` in `meta.json` |
| `POST` | `/api/issues/{id}/unignore` | Sets `ignored: false` in `meta.json` |

### `reports.Manager` additions

```go
func (m *Manager) WriteMetadata(issueID string, data *MetaData) error
func (m *Manager) ReadMetadata(issueID string) (*MetaData, error)
func (m *Manager) SetIgnored(issueID string, ignored bool) error
```

`WriteMetadata` is called by `runScan` alongside `WriteError`.

### `ListIssues` change

Reads `meta.json` to enrich each `IssueSummary`. Filters out ignored issues by default. Accepts `?show_ignored=true` to include them.

---

## 4. Bug Fix: Investigate via API

### Root causes

1. **Fragile service extraction** — `serve.go` calls `extractServiceFromReport(errorContent)` which parses `**Service:** svc-name` from `error.md` text. If the format doesn't match, `service` is empty and `resolveRepoPath` fails silently inside a goroutine.
2. **Silent goroutine errors** — `TriggerInvestigate` fires the investigation in a goroutine with no error logging. Any failure is invisible.
3. **No UI feedback** — `IssueDetail.tsx` calls `fetchIssue()` immediately after triggering, before Claude finishes. The SSE `/progress` endpoint exists but is never subscribed to.

### Fixes

**1. Read service from `meta.json`** — `serve.go` reads `MetaData` instead of parsing `error.md`. Falls back to `extractServiceFromReport` for issues scanned before v2 (no `meta.json`):
```go
handlers.SetInvestigateFunc(func(issueID string) error {
    service := ""
    if meta, err := mgr.ReadMetadata(issueID); err == nil {
        service = meta.Service
    }
    if service == "" {
        errorContent, _ := mgr.ReadError(issueID)
        service = extractServiceFromReport(errorContent)
    }
    return runInvestigate(issueID, service, cfg, mgr)
})
```

**2. Log goroutine errors** — wrap the goroutine body:
```go
go func() {
    defer h.running.Delete(id)
    if err := h.investigateFn(id); err != nil {
        log.Printf("investigate %s failed: %v", id, err)
    }
}()
```

**3. SSE error event** — add error status to the stream so the UI can surface failures:
```
data: {"status":"error","message":"agent failed: ..."}
```

**4. UI: SSE subscription** — `IssueDetail.tsx` subscribes to `subscribeProgress(id, ...)` when triggering investigate or fix. Shows "Running…" state (pulsing indicator) until SSE emits `complete` or `error`, then calls `fetchIssue()`.

---

## 5. Frontend: Redesign

### Stack

- **shadcn/ui** — component library (Button, Badge, Table, Select, Checkbox, Separator)
- **Tailwind CSS** — utility classes (already used by shadcn)
- **Color palette** — Slate/blue accent (shadcn `slate` base, `blue-500` primary action)
- **Dark mode** — CSS variable toggle, default dark; persisted to `localStorage`

### Dashboard

Table with expandable rows:

**Columns:** Issue title · Service · Stage badge · MR link · Actions (···)

**Expandable row** — click any row to reveal:
- Last seen, occurrence count
- Links: Datadog ↗, Events ↗, Traces ↗
- Actions: Investigate (if stage=scanned) / Fix (if stage=investigated) · View · Ignore

Expanded row style: blue left border (`border-l-2 border-blue-500`) + slightly darker blue-tinted background to distinguish from the header row.

**Toolbar:** stage filter dropdown · "Show ignored" checkbox · Scan Now button · dark mode toggle

### Issue Detail

- Breadcrumb back to dashboard
- Title + stage badge + metadata row (service, last seen, count, Datadog/Events/Traces links)
- Error report section (collapsible markdown)
- Investigation section:
  - When not started: "Investigate" button
  - When running: pulsing blue dot + "Running…" label, greyed-out Fix section below
  - When complete: markdown viewer
- Fix section: enabled only after investigation exists
- Resolution panel: shown when `resolve.json` exists (branch, MR link, status)

---

## 6. File Changes Summary

### Go (backend)

| File | Change |
|------|--------|
| `internal/reports/manager.go` | Add `MetaData` struct, `WriteMetadata`, `ReadMetadata`, `SetIgnored`; enrich `ListIssues` |
| `internal/datadog/client.go` | Add `FetchIssueContext`, `IssueContext`, `TraceRef` |
| `internal/api/handlers.go` | Add `TriggerIgnore`, `TriggerUnignore`; log goroutine errors; SSE error event; enrich `IssueListItem` Go struct with `title`, `service`, `last_seen`, `count`, `mr_url` |
| `internal/api/server.go` | Register new ignore/unignore routes |
| `cmd/scan.go` | Call `WriteMetadata` with structured data from Datadog response |
| `cmd/investigate.go` | Call `FetchIssueContext`; append context to prompt |
| `cmd/serve.go` | Read service from `meta.json` instead of parsing `error.md` |

### TypeScript (frontend)

| File | Change |
|------|--------|
| `web/package.json` | Add shadcn/ui, tailwind, class-variance-authority, clsx |
| `web/src/api/client.ts` | Add `ignoreIssue`, `unignoreIssue`; update `IssueListItem` type |
| `web/src/pages/Dashboard.tsx` | Rewrite with shadcn Table, expandable rows, toolbar |
| `web/src/pages/IssueDetail.tsx` | Rewrite with SSE subscription, running state, shadcn components |
| `web/src/components/StageIndicator.tsx` | Update to shadcn Badge with Slate palette |
| `web/src/index.css` | Replace with Tailwind + shadcn CSS variables, dark mode support |
