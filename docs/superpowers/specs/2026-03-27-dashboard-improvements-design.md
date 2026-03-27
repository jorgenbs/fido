# Dashboard Improvements — Design Spec

## Overview

Four improvements to the Fido dashboard and backend:

1. **Bulk selection & actions** — Gmail-style checkboxes with bulk ignore/unignore
2. **Service filter** — dropdown filter populated from current issues
3. **Investigation tags** — parse Confidence, Complexity, Code Fixable from investigation reports, store in meta.json, display as columns, and allow filtering
4. **Prompt update** — add `## Code Fixable: Yes/No` to investigation prompt template

## Backend Changes

### Investigation Prompt Template (`cmd/investigate.go`)

Add a new field to the prompt template:

```
- **Code Fixable**: Yes/No (is this a code defect that can be fixed with a code change?)
```

This goes alongside the existing Confidence and Complexity fields.

### Tag Parsing (`cmd/investigate.go`)

After `mgr.WriteInvestigation()` completes, parse three tags from the investigation markdown:

- `## Confidence: <value>` → `High`, `Medium`, `Low`
- `## Complexity: <value>` → `Simple`, `Moderate`, `Complex`
- `## Code Fixable: <value>` → `Yes`, `No`

Parsing is case-insensitive, extracts the first word after the colon. If a tag is missing, store empty string.

### Metadata Storage (`internal/reports/manager.go`)

Add three fields to `MetaData`:

```go
Confidence string `json:"confidence"`
Complexity string `json:"complexity"`
CodeFixable string `json:"code_fixable"`
```

Add `UpdateMetadata(issueID string, fn func(*MetaData))` method that reads, applies mutation, and writes back.

### API Response (`internal/api/handlers.go`)

Add to `IssueListItem` JSON response:

```go
Confidence  string `json:"confidence"`
Complexity  string `json:"complexity"`
CodeFixable string `json:"code_fixable"`
```

Populated from meta.json when listing issues.

## Frontend Changes

### New Columns (`Dashboard.tsx`)

Add three columns after Stage:

| Column | Display | Empty state |
|--------|---------|-------------|
| Confidence | Colored badge: green=High, yellow=Medium, red=Low | `—` |
| Complexity | Colored badge: green=Simple, yellow=Moderate, red=Complex | `—` |
| Fixable | Green `✓` for Yes, red `✗` for No | `—` |

### Filters (`Dashboard.tsx`)

Add to the toolbar alongside existing stage filter:

- **Service** dropdown — dynamically populated from unique `service` values in current issue list. "All services" default.
- **Confidence** dropdown — All / High / Medium / Low
- **Complexity** dropdown — All / Simple / Moderate / Complex
- **Code fixable only** checkbox — same pattern as "Show ignored"

All filtering is client-side. Issues are filtered after fetch, before render.

### Selection & Bulk Actions (`Dashboard.tsx`)

- **Checkbox column** (32px) as first column in header and each row
- **Select all** checkbox in header — toggles all currently visible (filtered) issues
- **Selected state** tracked as `Set<string>` of issue IDs
- **Bulk action bar** appears between toolbar and table when ≥1 item selected:
  - Shows count: "N selected"
  - "Ignore" button — calls `ignoreIssue()` for each selected non-ignored issue
  - "Unignore" button — calls `unignoreIssue()` for each selected ignored issue
  - "Clear selection" link
- Selection clears after bulk action completes and issues re-fetch
- Selected rows get subtle blue background highlight (`bg-blue-950/30`)

### API Client (`api/client.ts`)

Add fields to `IssueListItem` interface:

```typescript
confidence: string;
complexity: string;
code_fixable: string;
```

## Grid Layout

Current: `grid-cols-[2.5fr_1fr_1fr_0.8fr_0.8fr_60px]`

New: `grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.6fr_0.8fr_0.8fr]`

Columns: Checkbox | Issue | Service | Stage | Confidence | Complexity | Fixable | CI | MR

The expand/collapse `···` column is removed — row expansion still works by clicking anywhere on the row.
