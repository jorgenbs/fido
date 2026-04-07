# Fido v3 — Import-driven workflow

**Date:** 2026-04-07
**Supersedes:** 2026-04-03-fido-v3-observability-lifecycle-design.md (timeseries/dashboard enrichment parts)

## Problem

Fido v2 drifted toward replicating Datadog's UI — timeseries charts, sparklines, time window selectors, trend indicators. This is the wrong direction. Developers already use Datadog to view and get notified about errors. Fido's value is as a sidecar for _resolving_ issues: investigate with agents, push MRs, monitor resolution.

The auto-scan model (discover all issues, pull them in automatically) doesn't match how developers actually work. They pick a specific issue from Datadog and want to resolve it.

## Design

### 1. Remove timeseries/sparkline/time-window features

**Backend removals:**
- `internal/reports/timeseries.go` + `timeseries_test.go` — `Bucket`, `TimeSeries` types, read/write methods
- `internal/api/timeseries_stats.go` + `timeseries_stats_test.go` — stats computation
- `GetTimeseries` handler from `handlers.go`
- `/api/issues/{id}/timeseries` route from `server.go`
- `Timeseries` and `Stats` fields from `IssueListItem` struct
- Timeseries reading from `ListIssues` handler (the `ReadTimeSeries` block)
- `window` query parameter from `ListIssues`
- `FetchErrorTimeline`, `parseComputeRaw`, `parseComputeTyped`, `TimelineBucket` from `datadog/client.go`
- `ObservationWindow` from `ScanConfig`

**Syncer removals:**
- `FetchBuckets`, `SaveBuckets`, `IsBucketStale` from `Deps` interface
- `BucketData` type from engine
- `JobFetchBuckets` handling from engine worker
- Bucket-stale check from `executeSyncIssues`
- `FetchBuckets`, `SaveBuckets`, `IsBucketStale` implementations from adapter

**Frontend removals:**
- `web/src/components/Sparkline.tsx` — delete entire file
- `timeseries`, `stats` fields from `IssueListItem` type in `client.ts`
- `TimeseriesData` type and `fetchTimeseries` function from `client.ts`
- `window` parameter from `listIssues` function
- Time window selector (`Select` for `timeWindow`) from Dashboard toolbar
- `timeWindow` state variable from Dashboard
- Activity and Trend columns from Dashboard table (header + cells)
- Sparkline import from Dashboard
- Error frequency section from IssueDetail (the `timeseries` state, `fetchTimeseries` useEffect, and the chart section)
- Sparkline import from IssueDetail

### 2. Change scan to update-only

In `runScan`: remove the `else` branch that creates new reports for issues not yet in Fido. Only process issues where `mgr.Exists(issue.ID)` is true — update their metadata fields (count, last_seen, etc.) and continue.

In `runScanWithResults`: same change — skip creation, only update existing issues and build the `ScanResult` slice from existing issues.

The syncer's `executeSyncIssues` still enqueues `JobFetchStacktrace` for imported issues missing stack traces. The `JobFetchBuckets` path is removed entirely.

### 3. Add import functionality

#### CLI: `fido import <issue-id>`

New command in `cmd/import.go`:
1. Takes a single positional arg: the Datadog issue ID
2. Calls `ddClient.SearchErrorIssues` (or a new single-issue fetch) to get issue details
3. Validates: issue's service must have a matching entry in `cfg.Repositories`
4. Creates error report + metadata using the existing template (reuse `loadErrorTemplate` and the report-writing logic from scan)
5. Prints success with issue title, service, and ID — or a clear error message

Validation errors:
- "issue not found on Datadog" — if the API returns no results for that ID
- "service 'X' is not configured — add it to repositories in config.yml" — if service has no repo mapping

#### API: `POST /api/import`

New handler `ImportIssue` in `handlers.go`:
- Request body: `{"issue_id": "string"}`
- Calls the same import logic as the CLI
- Returns 200 with the created issue on success
- Returns 400 with error message on validation failure (service not configured)
- Returns 404 if issue not found on Datadog
- Returns 409 if issue already imported

Route: `r.Post("/api/import", h.ImportIssue)` in `server.go`

The handler needs access to the Datadog client and config for validation. Add an `ImportFunc` similar to `ScanFunc`.

#### Web: Import input in Dashboard toolbar

Simple inline form in the toolbar:
- Text input with placeholder "Datadog issue ID"
- "Import" button next to it
- On submit: call `POST /api/import`, show inline error toast on failure, refresh issue list on success
- Disable input + button while request is in flight

### 4. What stays unchanged

- Investigate, fix, resolve flow
- CI status polling and refresh
- SSE events and notifications
- All filters except time window (stage, service, confidence, complexity, code fixable, show ignored)
- Bulk selection and ignore/unignore
- Stack trace enrichment via syncer
- Rate limiting

### 5. Config changes

Remove `observation_window` from `ScanConfig`. Keep `rate_limit` and `interval` — the syncer still runs to refresh metadata and fetch missing stacktraces for imported issues.
