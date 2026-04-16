# fido v2 — Resume State

**Date:** 2026-03-26
**Branch:** main
**Last commit:** `9569f9c` fix(api): return empty array instead of null when no issues

---

## What We're Building

fido v2 — five improvement areas over a Go CLI tool that scans Datadog for production errors and manages an AI-driven fix pipeline:

1. Richer Datadog context at investigation time (trace/event deep-links + Spans API)
2. Structured `meta.json` at scan time (service, env, timestamps, count, Datadog URLs, ignored flag)
3. Frontend redesign with shadcn/ui, Slate/blue palette, dark mode
4. Ignore feature (hide unwanted issues from dashboard)
5. Bug fix: POST `/api/issues/{id}/investigate` returns 202 but silently fails

**Design spec:** `docs/superpowers/specs/2026-03-26-fido-v2-design.md`
**Implementation plan:** `docs/superpowers/plans/2026-03-26-fido-v2.md`

---

## Task Progress

| # | Task | Status |
|---|------|--------|
| 1 | MetaData model + Manager methods | ✅ Done (`64a5e9f`) |
| 2 | Fix ListIssues callers (cmd/list.go, handlers.go) | ✅ Done (`9569f9c`) |
| 3 | Scan writes meta.json + fix OrgSubdomain | ⚠️ Committed but has LSP warnings (see below) |
| 4 | Ignore/unignore API endpoints | ⏳ Pending |
| 5 | Fix goroutine logging + SSE error + serve.go bug | ⏳ Pending |
| 6 | Datadog FetchIssueContext | ⏳ Pending |
| 7 | Investigate uses FetchIssueContext | ⏳ Pending |
| 8 | shadcn/ui + Tailwind + dark mode setup | ⏳ Pending |
| 9 | Update API client types | ⏳ Pending |
| 10 | Dashboard redesign | ⏳ Pending |
| 11 | IssueDetail redesign with SSE | ⏳ Pending |

---

## Current Issue: Task 3 LSP Warnings

Task 3 was committed as `8639016` but LSP diagnostics show false positives that need investigation:

- `scan.go:9` — `"time" imported and not used` (UnusedImport compiler error)
- `scan.go:163` — `buildEventsURL` is unused
- `scan.go:172` — `buildTracesURL` is unused

**However:** `go build ./...` succeeds cleanly and `grep` shows the functions ARE called at lines 121-122 and `time` IS used inside the functions (lines 179-180, 188-189). The LSP diagnostics appear stale/incorrect.

**Verification command:**
```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go build ./... && go test ./...
```

Both should pass. If they do, the LSP warnings are stale and you can proceed to Task 4.

---

## How to Resume

### Process: Subagent-Driven Development

We're using `superpowers:subagent-driven-development`: fresh subagent per task, then spec compliance review, then code quality review, then mark complete.

### Next Action: Verify Task 3, then start Task 4

**Step 1:** Run build + tests to confirm Task 3 is clean:
```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go build ./... && go test ./...
```

**Step 2:** If clean, dispatch spec compliance reviewer for Task 3. Spec requirements:

1. `cmd/scan_test.go` — `TestScanCommand_CreatesErrorReports` has assertions for `mgr.ReadMetadata("issue-1")` returning `Service` and `Title`
2. `cmd/scan.go` — `scanCfg` includes `OrgSubdomain: cfg.Datadog.OrgSubdomain`
3. `cmd/scan.go` — `buildEventsURL` and `buildTracesURL` helper functions exist
4. `cmd/scan.go` — `runScan` calls `mgr.WriteMetadata(issue.ID, meta)` after `mgr.WriteError`
5. `meta` is populated with Title, Service, Env, FirstSeen, LastSeen, Count, DatadogURL, DatadogEventsURL, DatadogTraceURL
6. `go build ./...` succeeds, `go test ./...` passes

**Step 3:** If compliant, dispatch code quality review for Task 3.

**Step 4:** After Task 3 reviews pass, start Task 4 (Ignore/unignore API endpoints).

---

## Task 4 Full Spec (next up after Task 3)

From `docs/superpowers/plans/2026-03-26-fido-v2.md` around line 527:

**Files:**
- `internal/api/handlers.go` — add `TriggerIgnore`, `TriggerUnignore` methods
- `internal/api/handlers_test.go` — add tests for both
- `internal/api/server.go` — register `POST /api/issues/{id}/ignore` and `/api/issues/{id}/unignore`

**Tests to add in `handlers_test.go`:**

```go
func TestHandlers_TriggerIgnore(t *testing.T) {
    dir := t.TempDir()
    mgr := reports.NewManager(dir)
    mgr.WriteError("issue-1", "error")
    mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})

    h := NewHandlers(mgr, &config.Config{})
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

    h := NewHandlers(mgr, &config.Config{})
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
```

**Handlers to add in `handlers.go`:**

```go
func (h *Handlers) TriggerIgnore(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    if err := h.reports.SetIgnored(id, true); err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
}

func (h *Handlers) TriggerUnignore(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    if err := h.reports.SetIgnored(id, false); err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "unignored"})
}
```

**Routes to add in `server.go`:**

```go
r.Post("/api/issues/{id}/ignore", h.TriggerIgnore)
r.Post("/api/issues/{id}/unignore", h.TriggerUnignore)
```

**Commit message:** `feat(api): add ignore/unignore endpoints`

---

## Key Architecture Notes

- **State machine:** `~/.fido/reports/<issueID>/` — files: `error.md`, `meta.json`, `investigation.md`, `fix.md`, `resolve.json`
- **`meta.json`** — written at scan time, `ignored` field is orthogonal to stage
- **`reports.Manager`** — `SetIgnored` reads meta.json, flips flag, writes back
- **`IssueSummary`** — `{ID, Stage, Meta *MetaData, MRURL string}`
- **`IssueListItem`** (API) — `{ID, Stage, Title, Service, LastSeen, Count, MRURL *string, Ignored}`
- **SSE progress** — `/api/issues/{id}/progress` stream; UI subscribes when triggering investigate/fix
- **FetchIssueContext** — called at investigate time (not scan), best-effort Spans API, appends trace refs + deep-links to prompt

---

## Git Log (recent)

```
9569f9c fix(api): return empty array instead of null when no issues
c15b2a4 feat(api): enrich IssueListItem with title, service, last_seen, count, mr_url  (Task 2)
64a5e9f feat(reports): add MetaData model, WriteMetadata, ReadMetadata, SetIgnored, enrich ListIssues  (Task 1)
8639016 feat(scan): write meta.json at scan time with service, env, deep-link URLs  (Task 3 — verify first)
605a301 docs: add fido v2 implementation plan
0272fa9 docs: add fido v2 design spec
```

Note: git log may show these in a different order — run `git log --oneline -8` to see actual order.
