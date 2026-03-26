# GitLab CI Status Support — Design Spec

**Date:** 2026-03-26
**Status:** Approved

## Overview

Add GitLab CI pipeline status visibility to Fido. CI status is fetched during the existing scan step (CLI and daemon), displayed in the web UI, and when CI is failing the user can trigger a fix iteration that provides the agent with CI failure logs and the previous fix as context.

## Data Model

### `MetaData` (internal/reports/manager.go)

Two new fields added to the existing struct:

```go
CIStatus string `json:"ci_status,omitempty"` // "passed","failed","running","pending","canceled",""
CIURL    string `json:"ci_url,omitempty"`     // link to the GitLab pipeline
```

Written during scan if the issue has a `resolve.json` with a branch. Empty string means no pipeline found yet (issue hasn't been fixed, or pipeline not yet triggered).

### Versioned Fix Files

Fix iterations are stored as separate files rather than overwriting:

- First fix: `fix.md` (existing behavior, unchanged)
- Second fix: `fix-2.md`
- Third fix: `fix-3.md`
- etc.

New methods on `reports.Manager`:
- `WriteFixIteration(content string) (int, error)` — finds next available `fix-N.md` filename, writes it, returns iteration number
- `ReadLatestFix() (content string, iteration int, err error)` — returns content and number of the highest existing version

`resolve.json` is unchanged — always reflects the single MR and branch for the issue. A re-fix adds commits to the same branch.

## CI Status Fetch

### New Package: `internal/gitlab/ci.go`

Two functions using `glab` CLI as a subprocess (consistent with existing agent-runner pattern):

**`FetchCIStatus(branch, repoPath string) (status, pipelineURL string, err error)`**
- Shells out to: `glab ci status --branch <branch>` with `cwd: repoPath`
- Parses stdout for status: `"passed"`, `"failed"`, `"running"`, `"pending"`, `"canceled"`
- Returns `("", "", nil)` if no pipeline found
- Called during scan for every issue that has a `resolve.json`

**`FetchCIFailureLogs(branch, repoPath string) (string, error)`**
- Shells out to: `glab ci view --branch <branch> --log` with `cwd: repoPath`
- Returns raw log output from failing jobs
- Called only when building the `fix --iterate` prompt, not during scan

### Integration in `cmd/scan.go`

After writing `meta.json` for each issue:
1. Check if `resolve.json` exists
2. If yes, call `FetchCIStatus(resolve.Branch, repoPath)`
3. Update `meta.json` with `ci_status` and `ci_url`

Repo path is resolved from config by service name (same logic as investigate/fix — local path or temp git clone). If the service has no repo configured, CI fetch is skipped for that issue.

Failures in CI fetch are non-fatal — log a warning and continue scan.

## `fix --iterate` Command

`cmd/fix.go` gets an `--iterate` boolean flag. When set, the fix command runs in iteration mode:

### Iteration mode steps

1. Read `error.md` + `investigation.md` from reports manager
2. Read `resolve.json` for branch name and MR URL
3. Call `ReadLatestFix()` to get the most recent fix attempt
4. Call `FetchCIFailureLogs(branch, repoPath)` for failing job output
5. Build iteration prompt (see below)
6. Run agent interactively (same runner as initial fix)
7. Write result via `WriteFixIteration()` → `fix-2.md`, `fix-3.md`, etc.
8. Do NOT write a new `resolve.json` — branch and MR are unchanged

### Iteration prompt

Distinct from initial fix prompt. Key differences:
- States that branch `<branch>` already exists with open MR `<mr_url>`
- Includes CI failure logs
- Includes the previous `fix-N.md` as context
- Instructs agent: make necessary changes, commit to the existing branch, push
- Explicitly instructs: do NOT create a new branch or new MR

### API

`POST /api/issues/{id}/fix` accepts an optional JSON body:
```json
{ "iterate": true }
```

Handler reads this field and calls the fix function with iterate mode. No new endpoint needed.

## Web UI

### Dashboard

- Add CI status badge column next to the MR link column
- Badge only shown for issues that have an MR (`mr_url` present)
- Colors: `passed` → green, `failed` → red, `running` → yellow/amber, `pending` → gray
- Badge is a link to `ci_url` when present

### IssueDetail

- Resolution section shows CI status badge alongside MR URL
- When `ci_status === "failed"`: show **"Re-fix (CI failing)"** button
- Button calls `POST /api/issues/{id}/fix` with `{"iterate": true}`
- Progress streamed via existing SSE mechanism (`GET /api/issues/{id}/progress`)

### API response types

`IssueListItem` and `IssueDetail` both gain `ci_status` and `ci_url` string fields (nullable/omitempty). Frontend reads from these — no new endpoints needed.

## Error Handling

- `glab` not installed or not authenticated: `FetchCIStatus` returns an error; scan logs a warning and skips CI update for that issue. Does not abort scan.
- No pipeline found for branch: returns `("", "", nil)` — `ci_status` stays empty string, UI shows nothing.
- `fix --iterate` called when no `resolve.json` exists: return early with clear error message.
- `fix --iterate` called when `fix.md` doesn't exist: return early (can't iterate without a first fix).

## Out of Scope

- GitLab API direct calls (using `glab` CLI is sufficient)
- Auto-triggering re-fix on CI failure (always user-initiated from web UI)
- CI status for individual jobs/stages (just top-level pipeline status)
- Authentication setup for `glab` (assumed to be pre-configured in the environment)
