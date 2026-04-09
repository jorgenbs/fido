# Agent Investigation Enrichment — Design Spec

**Date:** 2026-04-09
**Status:** Draft

## Problem

The investigation agent operates in the target repo and can navigate code, run git commands, and reason about errors. But it only receives the error report (stack trace, occurrence count, links). It lacks Datadog-side signals that would help it make better decisions about where to look and what kind of problem it's dealing with.

Two debug stories illustrate the gap:

1. **c97d40f6 (DRTException)** — The agent identified the root cause (missing error mapping in a `when` block) but couldn't determine the exact Spare error name string. That string was in the Datadog trace payload (the HTTP response body from the upstream API), which we never passed to the agent.

2. **52a6d316 (GraphQLError)** — A regression where fields were removed from a GraphQL schema. The error frequency, version info, and first_seen timestamp made it straightforward to correlate with a specific commit. But the agent had none of that temporal context.

## Approach

Pre-fetch richer context from Datadog and local report metadata, then inject it into the investigation prompt. The agent invocation itself doesn't change — just a richer prompt string.

The agent's own environment (MCP servers, additional tools) is outside fido's scope. Fido's job is to bridge Datadog data into a well-structured prompt.

## Enrichment Data Sources

### 1. Trace Payload Extraction

Extends the existing `FetchIssueContext` in `internal/datadog/client.go`.

Currently we fetch spans filtered by `@issue.id` and extract only `custom["error"]["stack"]`. We extend this to extract targeted fields from the first matching span:

- `custom["error"]` — full error object: name, message, type
- `custom["http"]` — method, URL, status code
- Response body if present in the span attributes

New struct:

```go
type TraceDetails struct {
    ErrorName      string
    ErrorMessage   string
    ErrorType      string
    HTTPMethod     string
    HTTPURL        string
    HTTPStatusCode int
    ResponseBody   string
}
```

Returned on `IssueContext` alongside existing `Traces` and `StackTrace` fields.

### 2. Error Frequency Buckets

New method `FetchErrorFrequency` in `internal/datadog/client.go`.

Uses the existing `SpansApi.AggregateSpans` with `SPANSCOMPUTETYPE_TIMESERIES`, filtered by `@issue.id:<id>`, bucketed by day.

```go
type FrequencyData struct {
    Buckets       []FrequencyBucket // date → count
    OnsetTime     time.Time         // earliest bucket with count > 0
    TotalCount    int64
    SpikeDates    []time.Time       // dates with count > 2× average
    TrendDirection string           // "increasing", "decreasing", "stable", "single_spike"
}

type FrequencyBucket struct {
    Date  string
    Count int64
}
```

### 3. Version Info

Extends `SearchErrorIssues` to extract `FirstSeenVersion` and `LastSeenVersion` from `IssueAttributes.AdditionalProperties` or the dedicated getters on the SDK model.

Added to `ErrorIssueAttributes`:

```go
FirstSeenVersion string
LastSeenVersion  string
```

Persisted in `MetaData` via scan and import flows.

**Filtering rule:** Only included in the prompt when `FirstSeenVersion` and `LastSeenVersion` are the same (error appeared and persists in one version). If they differ, the error spans a version upgrade and the version info is less likely to pinpoint the cause — omit it to avoid red herrings.

### 4. Co-occurring Errors

Computed locally from the reports manager — no extra Datadog API call.

Query `mgr.ListIssues` for issues in the same service, then compare `first_seen` timestamps with ±1h tolerance. Return a list of co-occurring issue summaries (title, message, first_seen, count).

## Prompt Structure

Enriched context is appended after the existing error report, before the instructions section.

```markdown
## Error Report
<existing error.md content — stacktrace, traces, links>

## Trace Details
**Error:** ServiceNotAvailableError — "Service is not available at the requested time"
**HTTP:** POST /api/v1/rides → 400
**Response Body:** {"name":"ServiceNotAvailableError","message":"..."}

## Error Frequency
**Summary:** First appeared 2026-03-26, 2 occurrences over 12 days, no spike pattern
**Version:** First seen in v2.4.1
**Buckets:**
2026-03-26: 1 | 2026-04-07: 1

## Potentially Related Errors
- GraphQLError: Cannot query field "bookingId" (bt-backend) — first seen 2025-06-27
```

### Conditional Inclusion Rules

- **Trace Details:** Omitted if no extra data beyond the stack trace already in the error report.
- **Error Frequency:** Always included when data is available (even low counts are signal).
- **Version Info:** Omitted when `FirstSeenVersion` differs from `LastSeenVersion`, or when either is empty.
- **Related Errors:** Omitted if no other issues exist in the same service.
- **No empty sections.** If a section has no data, it doesn't appear in the prompt at all — an empty section primes the agent to think about that topic when there's nothing there.

### Prompt Guidance

Added to the instructions section of the prompt:

> If the error frequency shows a sudden onset correlating with a version change, use git log and git blame to identify the introducing commit. The trace details may contain the exact error identifiers needed for the fix.

Red herring guardrails in section framing:

- **Frequency:** "This data shows when and how often the error occurs. Use it to narrow the time window for git investigation, but only if the pattern suggests a recent onset. A long-lived error with steady occurrences is unlikely to correlate with recent commits."
- **Related Errors:** "These errors were tracked in the same service with overlapping timelines. They may share a root cause, or they may be entirely unrelated. Only pursue connections if the error types or stack traces suggest a shared code path."

## Implementation Changes

All changes extend existing files. No new files.

### `internal/datadog/client.go`

- Add `TraceDetails` struct
- Extend `FetchIssueContext` to populate `TraceDetails` from span `custom` map (error object, HTTP fields, response body)
- Add `FrequencyData` and `FrequencyBucket` structs
- Add `FetchErrorFrequency(issueID, service, env, firstSeen, lastSeen)` method using `AggregateSpans` timeseries
- Extend `ErrorIssueAttributes` with `FirstSeenVersion`, `LastSeenVersion`
- Extract version fields in `SearchErrorIssues` from issue attributes

### `internal/reports/manager.go`

- Add `FirstSeenVersion`, `LastSeenVersion` to `MetaData` struct

### `cmd/scan.go`

- Persist `FirstSeenVersion`, `LastSeenVersion` in metadata during scan

### `cmd/investigate.go`

- After reading error report, before calling agent:
  1. Call `FetchErrorFrequency` → format frequency section with summary and guardrail framing
  2. Use enriched `TraceDetails` from `FetchIssueContext` → format trace details section
  3. Query `mgr.ListIssues` for same-service issues with `first_seen` within ±1h → format related errors section
  4. Apply version info filtering (omit if wide spread)
- Append enrichment sections + prompt guidance to the prompt string
- No changes to agent invocation itself

## Out of Scope

- **Agent environment configuration** (MCP servers, tools) — the agent's concern, not fido's
- **Git pre-computation** — the agent already has repo access and can run git commands based on the temporal signals we provide
- **Configurable field extraction** — targeted extraction covers known use cases; revisit if needed
