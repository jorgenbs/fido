# Resolution Lifecycle Design

**Date:** 2026-04-10
**Status:** Approved

## Overview

Close the loop on error management: when a fix MR merges, resolve the error in Datadog, then continuously monitor for regressions. Fido tracks Datadog's issue status on every sync cycle and fires notifications when status diverges from expectations.

## Core Model

Resolution status is a **separate axis** from the pipeline stage (`scanned → investigated → fixed`). The pipeline stage tracks where an issue is in the fix workflow; Datadog status tracks what Datadog thinks about the error. This mirrors how `Ignored` already works — orthogonal to stage.

### New fields on `MetaData`

| Field | Type | Description |
|-------|------|-------------|
| `DatadogStatus` | `string` | Current Datadog status: `"for_review"`, `"reviewed"`, `"resolved"`, `"ignored"`, `"excluded"` |
| `ResolvedAt` | `string` | RFC3339 timestamp, set when Fido resolves the issue in Datadog |
| `RegressionCount` | `int` | Number of times this issue has regressed after being resolved |

### Loop Prevention

Fido only calls the Datadog resolve API **once per fix cycle**. The `ResolvedAt` field acts as the guard:

1. MR merged + `ResolvedAt` empty → call Datadog resolve API → set `ResolvedAt`
2. Datadog flips to `for_review` → Fido records regression, does NOT re-resolve
3. Only a **new fix cycle** (new fix file, new MR merged) clears `ResolvedAt`, allowing another resolve call

Invariant: `ResolvedAt` non-empty means "we already resolved this, don't do it again."

## Datadog API Integration

Two new methods on the Datadog client:

### `ResolveIssue(issueID string) error`

`PUT /api/v2/error-tracking/issues/{issue_id}/state` with body:

```json
{
  "data": {
    "attributes": { "state": "RESOLVED" },
    "id": "<issue_id>",
    "type": "error_tracking_issue"
  }
}
```

### `GetIssueStatus(issueID string) (string, error)`

`GET /api/v2/error-tracking/issues/{issue_id}` — extract and return current status string.

Both go through the existing rate limiter.

## Sync Engine

The existing `resolve_check` job stub gets implemented. Each sync cycle, for every tracked non-ignored issue:

### Step 1: MR Merge Check

If `MRStatus == "merged"` and `ResolvedAt` is empty:
- Call `ResolveIssue(datadogIssueID)`
- Set `DatadogStatus = "resolved"`, set `ResolvedAt` to current time
- Fire SSE `issue:resolved` event

### Step 2: Status Sync

Call `GetIssueStatus(datadogIssueID)`, compare to stored `DatadogStatus`:
- If stored was `"resolved"` and Datadog says `"for_review"` → **regression**. Increment `RegressionCount`, update `DatadogStatus`, fire SSE `issue:regression` event
- Otherwise, if diverged → update `DatadogStatus`, fire SSE `issue:status_changed` event
- If same → no action

This means one API call per tracked issue per cycle (the status check). The resolve call only happens on MR merge, once per fix cycle.

## Dashboard UI

Badge/pill on dashboard rows showing Datadog status:

| Condition | Badge |
|-----------|-------|
| `DatadogStatus == "resolved"` | Green "Resolved" badge |
| `DatadogStatus == "for_review"` and `RegressionCount > 0` | Red "Regression" badge |
| `DatadogStatus == "for_review"` and `RegressionCount == 0` | No badge (default state) |
| `DatadogStatus == "reviewed"` or `"ignored"` | Neutral subtle badge |

Regressions sort to the top of the issue list.

## SSE Events

| Event | Trigger |
|-------|---------|
| `issue:resolved` | Fido marks an issue resolved in Datadog |
| `issue:regression` | A resolved issue flips back to `for_review` |
| `issue:status_changed` | Any other Datadog status divergence |

These follow the existing SSE pattern — the frontend already listens for `issue:*` events.

## What This Does NOT Include

- Manual resolve action from the UI (future work)
- Browser push notifications (separate Notification Service slice)
- Observation window / `confirmed` terminal state (replaced by continuous monitoring)
