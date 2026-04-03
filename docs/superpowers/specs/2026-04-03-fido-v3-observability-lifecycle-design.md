# Fido v3: Observability, Lifecycle & Agent Intelligence

**Date:** 2026-04-03
**Status:** Approved

## Overview

Evolve Fido from a scan-and-fix pipeline into a closed-loop error management system. Six components:

1. **Daemon Sync Engine** — refactor the background scanner into a rate-limited job queue that consolidates all Datadog API interactions
2. **Resolution Lifecycle** — close the loop: resolve errors in Datadog, monitor for regressions, confirm fixes
3. **Dashboard Enrichment** — time-series sparklines, selectable time windows, trend indicators
4. **Agent Investigation Enrichment** — feed richer context (git correlation, frequency patterns, co-occurring errors) into the investigation agent
5. **Notification Service** — extensible notification abstraction with browser push as first implementation
6. **Config UI** — settings page for managing config.yml from the browser

## Architecture Approach

**Hybrid: foundation first, then feature slices.**

The daemon sync engine is built first as a shared foundation. Then each feature (resolution lifecycle, sparklines, agent enrichment, config UI) is implemented as an independent vertical slice on top of it.

---

## 1. Daemon Sync Engine

### Current State

The daemon is a simple timer that calls `scanFn()` every interval (default 15m). All Datadog API calls happen in a burst during the scan. Stack traces are only fetched at investigation time.

### Design

Refactor into a job-based sync engine with a priority queue and rate limiter.

### Job Types

- **`sync_issues`** — existing scan logic (search for error issues). Runs first each cycle. Enqueues follow-up jobs for each issue.
- **`fetch_buckets`** — fetch time-series occurrence data for an issue within the current time window. Stores bucketed counts locally.
- **`fetch_stacktrace`** — fetch sample error spans/stack traces. Pre-fetches for issues that don't have a stack trace yet (currently only done at investigation time).
- **`resolve_check`** — for resolved issues in the observation window, check if new occurrences appeared since resolution.

### Rate Limiter

Token-bucket rate limiter wrapping all Datadog API calls. Shared across all job types. On 429 response, back off and re-enqueue the job. Jobs are spread across the scan interval rather than bursting at scan time.

Configurable: max requests/minute in config.yml (default: 30/min).

### Time-Series Storage

Per-issue file `timeseries.json` alongside `meta.json`:

```json
{
  "buckets": [
    {"timestamp": "2026-04-03T10:00:00Z", "count": 12},
    {"timestamp": "2026-04-03T11:00:00Z", "count": 8}
  ],
  "window": "7d",
  "last_fetched": "2026-04-03T14:00:00Z"
}
```

- Hourly granularity for windows <= 7d, daily for longer.
- Cache refreshed when dashboard requests a window not covered, or on regular daemon cycle.

### Event Flow

```
Scan interval fires
  -> sync_issues job runs (Datadog search API)
  -> For each issue:
      -> enqueue fetch_buckets (if stale or window changed)
      -> enqueue fetch_stacktrace (if missing)
      -> enqueue resolve_check (if in observation window)
  -> Worker drains queue at rate limit
  -> Each completed job writes to local files + publishes SSE event
```

---

## 2. Resolution Lifecycle

### New Issue States

Extend the current stage model with resolution tracking:

| Stage | Meaning |
|-------|---------|
| scanned | Error captured from Datadog |
| investigated | AI root-cause analysis complete |
| fixed | Fix implemented, MR opened |
| **resolved** | MR merged + error marked resolved in Datadog. Observation window active. |
| **confirmed** | Observation window passed with zero recurrences. Terminal happy state. |
| **regression** | New occurrences detected during observation window. Needs human attention. |

### State Transitions

```
scanned -> investigated -> fixed -> resolved -> confirmed
                                       |
                                       v
                                    regression -> investigated -> fixed -> resolved -> ...
```

### Resolution Trigger

When the scanner detects an issue's MR status is `merged`:
1. Call Datadog API to mark the error issue as resolved
2. Record `resolved_at` in `resolve.json`
3. Transition to `resolved` state
4. Start observation window

### Observation Window

Configurable in config.yml (default `24h`). The `resolve_check` daemon job checks resolved issues:
- Query Datadog for occurrence count since `resolved_at`
- Configured duration has elapsed since `resolved_at` with zero occurrences during that period -> transition to `confirmed`
- New occurrences detected -> transition to `regression`, fire notification

### Regression Handling

- Issue moves to `regression` state (distinct from `scanned`)
- Browser push notification via notification service
- Issue surfaces prominently in dashboard (red badge, sorted to top)
- Previous investigation + fix context preserved
- Human decides whether to re-investigate or take a different approach

### Data Changes

`resolve.json` gains:
```json
{
  "resolved_at": "2026-04-03T16:00:00Z",
  "observation_window": "24h",
  "confirmed_at": null,
  "regression_at": null,
  "regression_count": 0
}
```

`meta.json` gains explicit `stage` field:
```json
{
  "stage": "resolved"
}
```

File-based stage detection remains as fallback for the original three stages. New states (`resolved`, `confirmed`, `regression`) require the explicit field.

---

## 3. Notification Service

### Interface

```go
type Notification struct {
    Type    string   // "regression", "confirmed", "scan_complete", etc.
    IssueID string
    Title   string
    Message string
    URL     string   // deep link to issue in Fido UI
}

type Notifier interface {
    Send(ctx context.Context, n Notification) error
}
```

### Dispatcher

Holds a list of `Notifier` implementations, fans out to all. Adding Slack later is registering a new `Notifier`.

```go
type Dispatcher struct {
    notifiers []Notifier
}
```

### Browser Push Implementation

First `Notifier` uses the existing SSE hub. Publishes notification events that the frontend picks up via the browser Notification API.

### Trigger Points

- Regression detected
- Confirmed fixed
- Scan complete (existing event, routed through new service)

---

## 4. Dashboard Enrichment

### Time Window Selector

Toolbar control with presets: **1h, 6h, 24h, 7d, 30d**. Default: **24h**.

Changing the window:
1. Updates the issue list API call with the new time range
2. Triggers bucket data fetch if local cache doesn't cover the requested window
3. Re-renders sparklines

### Sparklines

Inline SVG sparkline per dashboard row, rendered from `timeseries.json` bucket data. Approximately 80x20px, no axes, shape only. Color-coded:
- Rising trend: red/warm
- Stable/declining: neutral/green

Click navigates to issue detail page.

### Issue Detail Chart

Larger time-series chart on the issue detail page showing occurrence frequency over the selected window. Simple bar or area chart, SVG-based or lightweight lib.

### Enriched Stats in Dashboard Table

- **Count** within selected window (not just all-time)
- **Trend indicator** — up/down/stable based on comparing recent vs older half of window
- **Peak** — highest hourly count in window

### API Changes

- `GET /api/issues/:id/timeseries?window=24h` — returns cached bucket data, triggers fetch if stale
- `GET /api/issues?window=24h` — filters to errors seen within window, includes summary stats (count, trend direction)

---

## 5. Agent Investigation Enrichment

### Richer Context for Investigation Prompt

Three new data sources appended to the investigation prompt after the error report.

### Git Correlation (conditionally included)

Relevance filter — only include when signal is strong:
1. Only include commits within `first_seen - 48h` to `first_seen + 24h`. If the error has been around for months, skip git context entirely.
2. Only blame lines directly referenced in the stack trace, not whole files.
3. If no commits fall within the relevant window, omit the section entirely (no empty section that primes the agent to think about git history).

Prompt framing: "The following recent changes were made to files in the stack trace around the time this error first appeared. They may or may not be related."

### Frequency Pattern Analysis

Include time-series bucket data in the prompt:
- Compact raw bucket data
- Pre-computed summary: onset time, spike patterns, trend direction, total occurrences
- Note sudden appearances with exact onset time

### Co-occurring Errors

Check other tracked issues in the same service:
- Compare time-series data for overlapping `first_seen` or spike times (tolerance: +/- 1h)
- Append "Related Errors" section listing co-occurring issues with titles and timing

### Prompt Structure

```markdown
## Error Frequency
<bucket summary + raw data>

## Recent Git Changes (affected files)
<git log + blame output>
(only included when relevant — see relevance filter)

## Potentially Related Errors
<co-occurring issues in same service>
```

Agent instructed: "If the error onset correlates with a specific commit, prioritize that as a likely root cause."

### No Agent Command Changes

Purely richer context passed to the existing agent invocation.

---

## 6. Config UI

### Settings Page

New route `/settings` in the frontend. Form-based page reading/writing `~/.fido/config.yml` through API endpoints.

### Sections

Mirror the config file structure:

- **Datadog** — site, org subdomain, services list (add/remove), token (masked, editable)
- **Scan** — interval, since, observation window (duration inputs)
- **Repositories** — service -> path mappings (add/remove/edit)
- **Agent** — investigate command, fix command (text inputs)
- **Rate Limiting** — max requests/minute for Datadog API

### API Endpoints

- `GET /api/config` — returns current config (token masked)
- `PUT /api/config` — validates and writes config, restarts daemon with new settings
- `PUT /api/config/token` — separate endpoint for token updates (never returned in full via GET)

### Validation

Backend validates before writing:
- Duration fields parse correctly
- Repository paths exist (for local repos)
- Services list is non-empty
- Rate limit within sane bounds

Invalid fields return specific error messages, surfaced inline in the form.

### Daemon Reload

Config save signals the daemon to pick up new config on next cycle. No process restart needed.

---

## Implementation Order

1. **Daemon Sync Engine** — foundation for everything else
2. **Resolution Lifecycle** — depends on daemon (resolve_check jobs)
3. **Notification Service** — depends on resolution lifecycle (regression triggers)
4. **Dashboard Enrichment** — depends on daemon (time-series data)
5. **Agent Investigation Enrichment** — depends on daemon (bucket data) + independent git logic
6. **Config UI** — independent, can be built in parallel with later slices
