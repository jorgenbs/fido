# IssueDetail Page Improvements — Design Spec

## Overview

Five improvements to the IssueDetail page and related backend:

1. **Markdown formatting** — styled headings, links, code blocks in MarkdownViewer
2. **Stack trace truncation** — collapsible code blocks in MarkdownViewer
3. **Fix command display** — replace "Fix this issue" button with CLI command + copy
4. **Re-fix button guard** — only show when stage=fixed AND ci_status=failed
5. **CI status polling endpoint** — live CI status updates from GitLab
6. **Investigation tag prompt fix** — align prompt output format with parser expectations

---

## 1. Markdown Formatting

`web/src/components/MarkdownViewer.tsx` currently uses `react-markdown` with inline styles and no custom component overrides. Headings render at browser-default sizes with no dark-theme styling, and links are invisible.

Replace the inline-style wrapper div with a Tailwind `prose`-free approach using explicit custom `components` prop:

| Element | Tailwind classes |
|---------|-----------------|
| `h1` | `text-lg font-semibold text-foreground mt-4 mb-2` |
| `h2` | `text-base font-semibold text-foreground mt-3 mb-1.5` |
| `h3` | `text-sm font-semibold text-foreground mt-2 mb-1` |
| `a` | `text-blue-400 hover:underline` (opens in new tab) |
| `code` (inline) | `bg-muted text-xs font-mono px-1 py-0.5 rounded` |
| `ul` / `ol` | `pl-5 space-y-1 my-2` with `list-disc` / `list-decimal` |
| `p` | `my-1.5 text-sm` |
| `strong` | `font-semibold text-foreground` |

The outer wrapper div uses `px-4 py-3 text-sm text-foreground` with no external border (the `Section` component already provides the border).

---

## 2. Stack Trace Truncation

Code blocks (`pre` elements) in error reports often contain long stack traces. Custom `pre` component in `MarkdownViewer`:

- If the content has ≤ 12 lines: render normally, no truncation
- If > 12 lines: show first 12 lines, then a "Show more (N lines)" link
- Clicking expands to full content with a "Show less" link
- State (`expanded: boolean`) lives inside the custom `pre` component as local state
- Styling: `bg-muted/50 rounded p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap`

---

## 3. Fix Command Display

The "Fix this issue" button in the Fix section is replaced with a read-only command display:

```
fido fix <issue-id>
```

Rendered as a styled code block with a copy-to-clipboard button on the right. On click, copies `fido fix <id>` to clipboard using `navigator.clipboard.writeText`. Button label toggles briefly to "Copied!" then back to "Copy".

The Fix section only shows this when `issue.investigation` exists and `!issue.fix` (same condition that currently gates the Fix button).

---

## 4. Re-fix Button Guard + CI Running Indicator

**Re-fix guard:**

Current condition: `issue.ci_status === 'failed' && fixState !== 'running'`

New condition: `issue.stage === 'fixed' && issue.ci_status === 'failed' && fixState !== 'running'`

The `stage` field is already present on `IssueDetail` from the API. No backend changes needed.

**CI running indicator:**

When `issue.ci_status === 'running' || issue.ci_status === 'pending'`, the Resolution `<Section>` header shows a pulsing dot with label "CI pipeline running…" — reusing the existing `running` and `runningLabel` props already supported by the `Section` component. The `CIStatusBadge` in the section body continues to show the yellow badge as before.

---

## 5. MR Status Polling Endpoint

A single endpoint that refreshes both CI pipeline status and MR merge status, so the UI can reflect the full lifecycle of a merge request without a full page reload.

### Backend

New handler `RefreshMRStatus` on `GET /api/issues/{id}/mr-status`:

1. Read `resolve.json` for the issue — if absent, return `{"ci_status": "", "ci_url": "", "mr_status": ""}` (200)
2. Look up repo path via `cfg.Repositories[service]` (service from `meta.json`)
3. Call `gitlab.FetchCIStatus(resolve.Branch, repoPath)` — returns `(status, ciURL, err)`
4. Call `gitlab.FetchMRStatus(resolve.Branch, repoPath)` — returns `(mrStatus, err)` using `glab mr view --branch <branch>`
5. On success: update `meta.json` via `mgr.SetCIStatus(issueID, status, ciURL)` and update `resolve.json` via `mgr.SetMRStatus(issueID, mrStatus)`; return `{"ci_status": status, "ci_url": ciURL, "mr_status": mrStatus}`
6. On any error: return 200 with current values from `meta.json` / `resolve.json` (don't fail the request)

New `gitlab.FetchMRStatus(branch, repoPath string) (status string, err error)`:
- Runs `glab mr view --branch <branch>` in `repoPath`
- Parses the output for state keywords: `merged`, `opened`, `closed`
- Returns `""` if no MR found (non-fatal)

New `SetCIStatus(issueID, status, ciURL string) error` on `Manager` — read-modify-write on `meta.json` (same pattern as `SetIgnored`).

New `SetMRStatus(issueID, mrStatus string) error` on `Manager` — read-modify-write on `resolve.json`. `ResolveData` already has the `MRStatus string` field.

Register route in `server.go`: `r.Get("/api/issues/{id}/mr-status", handlers.RefreshMRStatus)`

### Frontend

In `IssueDetail.tsx`, add a polling effect:

```typescript
useEffect(() => {
  if (!id || !issue?.resolve) return; // only poll when MR exists
  const ciTerminal = ['passed', 'failed', 'canceled'];
  const mrTerminal = ['merged', 'closed'];
  if (ciTerminal.includes(issue.ci_status) && mrTerminal.includes(issue.resolve.mr_status)) return;

  const interval = setInterval(async () => {
    const res = await fetch(`${API_BASE}/api/issues/${id}/mr-status`);
    if (!res.ok) return;
    const data = await res.json();
    if (data.ci_status !== issue.ci_status || data.mr_status !== issue.resolve?.mr_status) {
      fetchIssue(); // full refresh on change
    }
  }, 30_000);

  return () => clearInterval(interval);
}, [id, issue?.resolve, issue?.ci_status]);
```

Polling runs every 30 seconds. Stops when both CI is terminal AND MR is merged/closed.

The `IssueDetail` API response already includes `resolve.mr_status` via the existing `resolve` field on `IssueDetail`.

---

## 6. Investigation Tag Prompt Fix

The prompt's Output Format section uses bullet-list style:
```
- **Confidence**: High/Medium/Low
```

But `parseInvestigationTags` looks for H2 heading style:
```
## Confidence: High
```

Fix: update the prompt template in `cmd/investigate.go` to use heading format:

```
## Output Format

Write your analysis as markdown with these sections:
- **Root Cause**: What is causing this error
- **Affected Files**: List of files involved
- **Suggested Fix**: How to fix this

## Confidence: High/Medium/Low
## Complexity: Simple/Moderate/Complex
## Code Fixable: Yes/No (is this a code defect that can be fixed with a code change?)
```

The three structured tags are now H2 headings matching the parser, while the free-text sections remain as bullet descriptions.
