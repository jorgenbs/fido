# IssueDetail Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve the IssueDetail page with styled markdown, collapsible stack traces, a CLI fix command display, smarter re-fix guard, live MR+CI status polling, and a prompt fix that makes investigation tags actually get parsed.

**Architecture:** Six independent changes that can be implemented as separate tasks: (1) a Go prompt string fix, (2-4) a new `/api/issues/{id}/mr-status` endpoint backed by new `FetchMRStatus` in the gitlab package and two new manager methods, (5) a fully styled MarkdownViewer with collapsible pre blocks, and (6) IssueDetail UI improvements wiring everything together.

**Tech Stack:** Go 1.25, react-markdown v10, React 19, TypeScript, Tailwind CSS, glab CLI for GitLab API calls, chi router.

---

## File Map

**Modified (backend):**
- `cmd/investigate.go` — fix prompt Output Format to use `## Confidence:` headings
- `internal/gitlab/ci.go` — add `FetchMRStatus`, `parseMRStatus`
- `internal/gitlab/ci_test.go` — tests for `parseMRStatus`, `FetchMRStatus`
- `internal/reports/manager.go` — add `SetCIStatus`, `SetMRStatus`
- `internal/reports/manager_test.go` — tests for both new methods
- `internal/api/handlers.go` — add `RefreshMRStatus` handler; import gitlab package
- `internal/api/handlers_test.go` — two tests for `RefreshMRStatus`
- `internal/api/server.go` — register `GET /api/issues/{id}/mr-status`

**Modified (frontend):**
- `web/src/api/client.ts` — add `fetchMRStatus` function
- `web/src/components/MarkdownViewer.tsx` — full rewrite: styled headings/links/code, collapsible pre
- `web/src/pages/IssueDetail.tsx` — fix command display, re-fix guard, CI running indicator, MR polling

---

## Task 1: Fix Investigation Prompt Output Format

**Files:**
- Modify: `cmd/investigate.go` (lines 73–82)

The prompt tells the agent to output `- **Confidence**: High/Medium/Low` (bullet+bold style) but `parseInvestigationTags` looks for `## Confidence: High` (H2 heading prefix). The agent follows the prompt, so tags never match the parser. Fix the prompt to match the parser.

- [ ] **Step 1: Edit the prompt constant in `cmd/investigate.go`**

Replace the Output Format block. Current:
```
## Output Format

Write your analysis as markdown with these sections:
- **Root Cause**: What is causing this error
- **Affected Files**: List of files involved
- **Suggested Fix**: How to fix this
- **Confidence**: High/Medium/Low
- **Complexity**: Simple/Moderate/Complex
- **Code Fixable**: Yes/No (is this a code defect that can be fixed with a code change?)
```

Replace with:
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

In `cmd/investigate.go`, the `investigatePromptTemplate` const ends at line 82. Replace the last three bullet lines:

```go
const investigatePromptTemplate = `You are investigating a production error. Analyze the error below, look through the codebase, and produce a root cause analysis.

## Error Report

%s

## Instructions

0. Ensure you are on the latest on main branch
1. Analyze the error and stack trace
2. Find the relevant code in the repository
3. Identify the root cause
4. List all affected files and code paths
5. Suggest a fix approach
6. Estimate confidence and complexity
7. Determine whether this is a code-fixable defect

## Output Format

Write your analysis as markdown with these sections:
- **Root Cause**: What is causing this error
- **Affected Files**: List of files involved
- **Suggested Fix**: How to fix this

## Confidence: High/Medium/Low
## Complexity: Simple/Moderate/Complex
## Code Fixable: Yes/No (is this a code defect that can be fixed with a code change?)
`
```

- [ ] **Step 2: Verify existing tests still pass**

Run: `go test ./cmd/... -v -run TestParse`
Expected: `TestParseInvestigationTags_AllPresent`, `TestParseInvestigationTags_CaseInsensitive`, `TestParseInvestigationTags_MissingTags` all PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/investigate.go
git commit -m "fix: align investigate prompt output format with tag parser (heading style)"
```

---

## Task 2: Add FetchMRStatus to GitLab Package

**Files:**
- Modify: `internal/gitlab/ci.go`
- Modify: `internal/gitlab/ci_test.go`

Adds `FetchMRStatus(branch, repoPath string) (string, error)` which runs `glab mr view --branch <branch>` and parses the output for `merged`/`opened`/`closed`. Pattern mirrors `FetchCIStatus`.

- [ ] **Step 1: Write failing tests in `internal/gitlab/ci_test.go`**

Append to the file:

```go
func TestParseMRStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"State: merged\nTitle: Fix auth bug", "merged"},
		{"state:\tmerged", "merged"},
		{"State: opened", "opened"},
		{"State: closed", "closed"},
		{"No state here", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := parseMRStatus(tt.input)
		if got != tt.expected {
			t.Errorf("parseMRStatus(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFetchMRStatus_GlabNotFound(t *testing.T) {
	if _, err := exec.LookPath("glab"); err == nil {
		t.Skip("glab is installed; skipping not-found test")
	}
	dir := t.TempDir()
	os.MkdirAll(dir+"/.git", 0755)

	_, err := FetchMRStatus("main", dir)
	if err == nil {
		t.Error("expected error when glab not found")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./internal/gitlab/... -v -run TestParseMRStatus`
Expected: FAIL with "undefined: parseMRStatus"

- [ ] **Step 3: Add `parseMRStatus` and `FetchMRStatus` to `internal/gitlab/ci.go`**

Append to the end of the file (after `extractPipelineURL`):

```go
// FetchMRStatus runs `glab mr view --branch <branch>` in repoPath and returns
// the MR state ("merged", "opened", "closed", or ""). Returns ("", nil) if no MR found.
func FetchMRStatus(branch, repoPath string) (status string, err error) {
	cmd := exec.Command("glab", "mr", "view", "--branch", branch)
	cmd.Dir = repoPath
	out, execErr := cmd.CombinedOutput()
	output := string(out)

	if execErr != nil && len(strings.TrimSpace(output)) == 0 {
		return "", fmt.Errorf("glab mr view: %w", execErr)
	}

	return parseMRStatus(output), nil
}

// parseMRStatus searches output for known GitLab MR state strings.
func parseMRStatus(output string) string {
	lower := strings.ToLower(output)
	for _, s := range []string{"merged", "closed", "opened"} {
		if strings.Contains(lower, s) {
			return s
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/gitlab/... -v`
Expected: all tests PASS (FetchMRStatus_GlabNotFound skipped if glab installed)

- [ ] **Step 5: Commit**

```bash
git add internal/gitlab/ci.go internal/gitlab/ci_test.go
git commit -m "feat(gitlab): add FetchMRStatus using glab mr view"
```

---

## Task 3: Add SetCIStatus and SetMRStatus to Manager

**Files:**
- Modify: `internal/reports/manager.go`
- Modify: `internal/reports/manager_test.go`

Two new read-modify-write methods following the same pattern as `SetIgnored` and `SetInvestigationTags`.

- [ ] **Step 1: Write failing tests in `internal/reports/manager_test.go`**

Append to the file:

```go
func TestManager_SetCIStatus(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	m.WriteMetadata("issue-1", &MetaData{Service: "svc-a"})

	if err := m.SetCIStatus("issue-1", "failed", "https://gitlab.com/org/repo/-/pipelines/42"); err != nil {
		t.Fatalf("SetCIStatus: %v", err)
	}

	meta, _ := m.ReadMetadata("issue-1")
	if meta.CIStatus != "failed" {
		t.Errorf("CIStatus: got %q, want failed", meta.CIStatus)
	}
	if meta.CIURL != "https://gitlab.com/org/repo/-/pipelines/42" {
		t.Errorf("CIURL: got %q", meta.CIURL)
	}
}

func TestManager_SetCIStatus_PreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	m.WriteMetadata("issue-1", &MetaData{Service: "svc-a", Ignored: true, Confidence: "High"})

	m.SetCIStatus("issue-1", "passed", "https://gitlab.com/pipelines/1")

	meta, _ := m.ReadMetadata("issue-1")
	if !meta.Ignored {
		t.Error("Ignored flag was reset")
	}
	if meta.Confidence != "High" {
		t.Errorf("Confidence mutated: got %q", meta.Confidence)
	}
}

func TestManager_SetMRStatus(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	m.WriteResolve("issue-1", &ResolveData{
		Branch:   "fix/issue-1",
		MRURL:    "https://gitlab.com/mr/1",
		MRStatus: "opened",
	})

	if err := m.SetMRStatus("issue-1", "merged"); err != nil {
		t.Fatalf("SetMRStatus: %v", err)
	}

	resolve, _ := m.ReadResolve("issue-1")
	if resolve.MRStatus != "merged" {
		t.Errorf("MRStatus: got %q, want merged", resolve.MRStatus)
	}
	if resolve.Branch != "fix/issue-1" {
		t.Error("SetMRStatus should not change Branch field")
	}
}
```

- [ ] **Step 2: Run to confirm tests fail**

Run: `go test ./internal/reports/... -v -run TestManager_SetCIStatus`
Expected: FAIL with "m.SetCIStatus undefined"

- [ ] **Step 3: Add the two methods to `internal/reports/manager.go`**

After the `SetInvestigationTags` method (around line 194), add:

```go
func (m *Manager) SetCIStatus(issueID, status, ciURL string) error {
	meta, err := m.ReadMetadata(issueID)
	if err != nil {
		return fmt.Errorf("reading metadata: %w", err)
	}
	meta.CIStatus = status
	meta.CIURL = ciURL
	return m.WriteMetadata(issueID, meta)
}

func (m *Manager) SetMRStatus(issueID, mrStatus string) error {
	resolve, err := m.ReadResolve(issueID)
	if err != nil {
		return fmt.Errorf("reading resolve: %w", err)
	}
	resolve.MRStatus = mrStatus
	return m.WriteResolve(issueID, resolve)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/reports/... -v`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/reports/manager.go internal/reports/manager_test.go
git commit -m "feat(reports): add SetCIStatus and SetMRStatus manager methods"
```

---

## Task 4: Add RefreshMRStatus API Handler + Route

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`
- Modify: `internal/api/server.go`
- Modify: `web/src/api/client.ts`

New `GET /api/issues/{id}/mr-status` handler that fetches live CI and MR status (when a repo path is available) and returns `{ci_status, ci_url, mr_status}`. Falls back to stored values when config is nil or glab call fails.

- [ ] **Step 1: Write failing tests in `internal/api/handlers_test.go`**

Append to the file:

```go
func TestRefreshMRStatus_NoResolve(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	h := NewHandlers(mgr, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/issues/issue-1/mr-status", nil)
	r = withURLParam(r, "id", "issue-1")
	w := httptest.NewRecorder()
	h.RefreshMRStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ci_status"] != "" || resp["mr_status"] != "" {
		t.Errorf("expected empty statuses, got ci_status=%q mr_status=%q", resp["ci_status"], resp["mr_status"])
	}
}

func TestRefreshMRStatus_ReturnsStoredValuesWhenNoConfig(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:  "svc-a",
		CIStatus: "passed",
		CIURL:    "https://gitlab.com/org/repo/-/pipelines/1",
	})
	mgr.WriteResolve("issue-1", &reports.ResolveData{
		Branch:   "fix/issue-1",
		MRURL:    "https://gitlab.com/mr/1",
		MRStatus: "merged",
	})

	h := NewHandlers(mgr, nil) // nil config — no glab calls
	r := httptest.NewRequest(http.MethodGet, "/api/issues/issue-1/mr-status", nil)
	r = withURLParam(r, "id", "issue-1")
	w := httptest.NewRecorder()
	h.RefreshMRStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ci_status"] != "passed" {
		t.Errorf("ci_status: got %q, want passed", resp["ci_status"])
	}
	if resp["mr_status"] != "merged" {
		t.Errorf("mr_status: got %q, want merged", resp["mr_status"])
	}
}
```

- [ ] **Step 2: Run to confirm tests fail**

Run: `go test ./internal/api/... -v -run TestRefreshMRStatus`
Expected: FAIL with "h.RefreshMRStatus undefined"

- [ ] **Step 3: Add the handler to `internal/api/handlers.go`**

First, add the import for the gitlab package. Current imports block in handlers.go starts with `import (`. Add `"github.com/jorgenbs/fido/internal/gitlab"` to the import list:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jorgenbs/fido/internal/config"
	"github.com/jorgenbs/fido/internal/gitlab"
	"github.com/jorgenbs/fido/internal/reports"
)
```

Then append the handler after the existing `TriggerUnignore` method (around line 253):

```go
func (h *Handlers) RefreshMRStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	resolve, err := h.reports.ReadResolve(id)
	if err != nil {
		// No MR yet — return empty values, not an error
		writeJSON(w, http.StatusOK, map[string]string{
			"ci_status": "",
			"ci_url":    "",
			"mr_status": "",
		})
		return
	}

	meta, err := h.reports.ReadMetadata(id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"ci_status": "",
			"ci_url":    "",
			"mr_status": resolve.MRStatus,
		})
		return
	}

	ciStatus := meta.CIStatus
	ciURL := meta.CIURL
	mrStatus := resolve.MRStatus

	if h.config != nil && resolve.Branch != "" {
		if repo, ok := h.config.Repositories[meta.Service]; ok && repo.Local != "" {
			if status, url, fetchErr := gitlab.FetchCIStatus(resolve.Branch, repo.Local); fetchErr == nil {
				ciStatus = status
				ciURL = url
				_ = h.reports.SetCIStatus(id, status, url)
			}
			if status, fetchErr := gitlab.FetchMRStatus(resolve.Branch, repo.Local); fetchErr == nil {
				mrStatus = status
				_ = h.reports.SetMRStatus(id, status)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"ci_status": ciStatus,
		"ci_url":    ciURL,
		"mr_status": mrStatus,
	})
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/api/... -v -run TestRefreshMRStatus`
Expected: both tests PASS

- [ ] **Step 5: Register the route in `internal/api/server.go`**

In `server.go`, add the route inside the `/api` router block after the existing `progress` route:

```go
r.Route("/api", func(r chi.Router) {
    r.Get("/issues", h.ListIssues)
    r.Get("/issues/{id}", h.GetIssue)
    r.Post("/issues/{id}/investigate", h.TriggerInvestigate)
    r.Post("/issues/{id}/fix", h.TriggerFix)
    r.Post("/issues/{id}/ignore", h.TriggerIgnore)
    r.Post("/issues/{id}/unignore", h.TriggerUnignore)
    r.Get("/issues/{id}/progress", h.StreamProgress)
    r.Get("/issues/{id}/mr-status", h.RefreshMRStatus)
    r.Post("/scan", h.TriggerScan)
})
```

- [ ] **Step 6: Run full backend test suite**

Run: `go test ./...`
Expected: all tests PASS

- [ ] **Step 7: Add `fetchMRStatus` to `web/src/api/client.ts`**

Append to the end of `web/src/api/client.ts`:

```typescript
export async function fetchMRStatus(id: string): Promise<{ ci_status: string; ci_url: string; mr_status: string }> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/mr-status`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}
```

- [ ] **Step 8: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/server.go web/src/api/client.ts
git commit -m "feat: add GET /api/issues/{id}/mr-status endpoint for live MR+CI status"
```

---

## Task 5: Styled MarkdownViewer with Collapsible Pre Blocks

**Files:**
- Modify: `web/src/components/MarkdownViewer.tsx`

Complete rewrite. Removes the title prop (was always `""` in callers), removes the self-contained border (Section provides it), adds Tailwind-styled custom components for all markdown elements, and adds a collapsible `CollapsiblePre` component for code blocks with more than 12 lines.

- [ ] **Step 1: Rewrite `web/src/components/MarkdownViewer.tsx`**

Replace the entire file content:

```tsx
import { useState } from 'react';
import ReactMarkdown from 'react-markdown';

interface Props {
  content: string;
}

function CollapsiblePre({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = text.split('\n');
  const THRESHOLD = 12;
  const needsTruncation = lines.length > THRESHOLD;
  const displayText =
    !expanded && needsTruncation ? lines.slice(0, THRESHOLD).join('\n') : text;

  return (
    <pre className="bg-muted/50 rounded p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap my-2">
      {displayText}
      {needsTruncation && (
        <>
          {'\n'}
          <button
            className="text-blue-400 hover:underline text-xs font-sans cursor-pointer"
            onClick={() => setExpanded((e) => !e)}
          >
            {expanded ? 'Show less' : `Show more (${lines.length - THRESHOLD} lines)`}
          </button>
        </>
      )}
    </pre>
  );
}

export function MarkdownViewer({ content }: Props) {
  return (
    <div className="px-4 py-3 text-sm text-foreground">
      <ReactMarkdown
        components={{
          h1: ({ children }) => (
            <h1 className="text-lg font-semibold text-foreground mt-4 mb-2">{children}</h1>
          ),
          h2: ({ children }) => (
            <h2 className="text-base font-semibold text-foreground mt-3 mb-1.5">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-sm font-semibold text-foreground mt-2 mb-1">{children}</h3>
          ),
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noreferrer" className="text-blue-400 hover:underline">
              {children}
            </a>
          ),
          code: ({ children }) => (
            <code className="bg-muted text-xs font-mono px-1 py-0.5 rounded">{children}</code>
          ),
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          pre: ({ children }) => {
            const codeEl = children as React.ReactElement<{ children: any }>;
            const raw = codeEl?.props?.children;
            const text = Array.isArray(raw) ? raw.join('') : String(raw ?? '');
            return <CollapsiblePre text={text} />;
          },
          ul: ({ children }) => (
            <ul className="pl-5 space-y-1 my-2 list-disc">{children}</ul>
          ),
          ol: ({ children }) => (
            <ol className="pl-5 space-y-1 my-2 list-decimal">{children}</ol>
          ),
          p: ({ children }) => <p className="my-1.5 text-sm">{children}</p>,
          strong: ({ children }) => (
            <strong className="font-semibold text-foreground">{children}</strong>
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
```

- [ ] **Step 2: Update all MarkdownViewer usages in IssueDetail.tsx to remove `title=""`**

In `web/src/pages/IssueDetail.tsx`, change all three occurrences:

```tsx
// Line 158 — was: <MarkdownViewer title="" content={issue.error} />
<MarkdownViewer content={issue.error} />

// Line 168 — was: <MarkdownViewer title="" content={issue.investigation} />
<MarkdownViewer content={issue.investigation} />

// Line 192 — was: <MarkdownViewer title="" content={issue.fix} />
<MarkdownViewer content={issue.fix} />
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 4: Verify frontend with Playwright**

```bash
# Terminal 1 (background):
cd web && npm run dev &

# Terminal 2:
cd web && node verify.mjs
```
Expected: exits 0, no React errors reported.

Kill the dev server: `kill %1` (or use the job ID shown)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/MarkdownViewer.tsx web/src/pages/IssueDetail.tsx
git commit -m "feat: styled MarkdownViewer with collapsible code blocks"
```

---

## Task 6: IssueDetail — Fix Command, Re-fix Guard, CI Indicator, MR Polling

**Files:**
- Modify: `web/src/pages/IssueDetail.tsx`

Four changes in one file:
1. Replace "Fix this issue" button with a `fido fix <id>` command display + copy button
2. Add `stage === 'fixed'` guard to the Re-fix button
3. Show CI running indicator on Resolution section header
4. Add 30s polling effect for `/api/issues/{id}/mr-status`

- [ ] **Step 1: Add imports and state to `IssueDetail.tsx`**

At the top of `IssueDetail.tsx`, update the import from `../api/client` to include `fetchMRStatus`:

```tsx
import {
  getIssue,
  triggerInvestigate,
  triggerFix,
  subscribeProgress,
  fetchMRStatus,
  type IssueDetail as IssueDetailType,
} from '../api/client';
```

Inside the `IssueDetail` function, after the existing state declarations (after `const sseRef = ...`), add:

```tsx
const [copied, setCopied] = useState(false);
```

- [ ] **Step 2: Add the MR status polling effect**

In `IssueDetail.tsx`, after the existing `useEffect` that calls `fetchIssue()` and closes `sseRef` on cleanup (lines 41–44), add a second effect:

```tsx
useEffect(() => {
  if (!id || !issue?.resolve) return;
  const ciTerminal = ['passed', 'failed', 'canceled'];
  const mrTerminal = ['merged', 'closed'];
  if (
    ciTerminal.includes(issue.ci_status) &&
    mrTerminal.includes(issue.resolve.mr_status ?? '')
  )
    return;

  const interval = setInterval(async () => {
    try {
      const data = await fetchMRStatus(id);
      if (
        data.ci_status !== issue.ci_status ||
        data.mr_status !== issue.resolve?.mr_status
      ) {
        fetchIssue();
      }
    } catch {
      // non-fatal: polling errors are ignored
    }
  }, 30_000);

  return () => clearInterval(interval);
}, [id, issue?.resolve, issue?.ci_status]);
```

- [ ] **Step 3: Replace "Fix this issue" button with fix command display**

Find the Fix section in `IssueDetail.tsx`. The current content rendering for the fix button (when investigation exists but fix doesn't) is:

```tsx
) : issue.investigation ? (
  <div className="p-4">
    <Button size="sm" onClick={handleFix}>
      Fix this issue
    </Button>
  </div>
) : null}
```

Replace with:

```tsx
) : issue.investigation ? (
  <div className="p-4 space-y-2">
    <p className="text-xs text-muted-foreground">Run this command to fix the issue:</p>
    <div className="flex items-center gap-2 bg-muted/50 rounded px-3 py-2">
      <code className="flex-1 text-xs font-mono text-foreground">fido fix {issue.id}</code>
      <button
        onClick={() => {
          navigator.clipboard.writeText(`fido fix ${issue.id}`);
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        }}
        className="text-xs text-muted-foreground hover:text-foreground shrink-0"
      >
        {copied ? 'Copied!' : 'Copy'}
      </button>
    </div>
  </div>
) : null}
```

- [ ] **Step 4: Add `stage === 'fixed'` guard to Re-fix button**

In the Resolution section, find the Re-fix button condition:

```tsx
{issue.ci_status === 'failed' && fixState !== 'running' && (
```

Change to:

```tsx
{issue.stage === 'fixed' && issue.ci_status === 'failed' && fixState !== 'running' && (
```

- [ ] **Step 5: Add CI running indicator to Resolution section**

Find the Resolution section opening tag:

```tsx
{issue.resolve && (
  <Section title="Resolution">
```

Change to:

```tsx
{issue.resolve && (
  <Section
    title="Resolution"
    running={issue.ci_status === 'running' || issue.ci_status === 'pending'}
    runningLabel="CI pipeline running…"
  >
```

- [ ] **Step 6: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 7: Verify frontend with Playwright**

```bash
cd web && npm run dev &
cd web && node verify.mjs
kill %1
```
Expected: exits 0, no React errors

- [ ] **Step 8: Commit**

```bash
git add web/src/pages/IssueDetail.tsx
git commit -m "feat: fix command display, re-fix guard, CI indicator, MR status polling"
```

---

## Self-Review

**Spec coverage check:**

1. ✅ **Markdown formatting** — Task 5: styled h1/h2/h3, links (text-blue-400, new tab), inline code (bg-muted), ul/ol (list-disc/list-decimal), p, strong
2. ✅ **Stack trace truncation** — Task 5: `CollapsiblePre` with THRESHOLD=12, "Show more (N lines)" / "Show less" toggle
3. ✅ **Fix command display** — Task 6 Step 3: `fido fix <id>` code block with copy-to-clipboard; only shown when `issue.investigation && !issue.fix`
4. ✅ **Re-fix button guard** — Task 6 Step 4: `issue.stage === 'fixed' && issue.ci_status === 'failed'`
5. ✅ **CI running indicator** — Task 6 Step 5: `running={ci_status === 'running' || 'pending'}` on Resolution Section
6. ✅ **MR status polling endpoint** — Tasks 2, 3, 4: `FetchMRStatus`, `SetCIStatus`, `SetMRStatus`, `RefreshMRStatus`, route registered
7. ✅ **Investigation tag prompt fix** — Task 1: `## Confidence:` heading format in prompt

**Placeholder scan:** No TBDs, TODOs, or vague steps. All code complete.

**Type consistency:** `fetchMRStatus` added to `client.ts` (Task 4), imported in `IssueDetail.tsx` (Task 6). `SetCIStatus(issueID, status, ciURL string)` defined in Task 3, called with same signature in `RefreshMRStatus` (Task 4). `SetMRStatus(issueID, mrStatus string)` defined in Task 3, called in Task 4.
