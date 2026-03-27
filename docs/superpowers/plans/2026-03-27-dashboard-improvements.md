# Dashboard Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add bulk selection + ignore actions, service/tag filters, and investigation tags (Confidence, Complexity, Code Fixable) parsed from investigation reports, stored in meta.json, and displayed as filterable columns in the dashboard.

**Architecture:** Backend parses three structured tags from investigation markdown and persists them to meta.json immediately after writing the report. The API exposes them in the issue list. The frontend applies all new filters client-side on the already-fetched issue list, keeping the existing API query pattern unchanged.

**Tech Stack:** Go (backend: reports, api, cmd/investigate), React + TypeScript + Tailwind + shadcn/ui (frontend)

---

## File Map

| File | Change |
|------|--------|
| `internal/reports/manager.go` | Add `Confidence`, `Complexity`, `CodeFixable` to `MetaData`; add `SetInvestigationTags` method |
| `internal/reports/manager_test.go` | Tests for new fields and `SetInvestigationTags` |
| `cmd/investigate.go` | Add `## Code Fixable:` to prompt template; add `parseInvestigationTags` + `firstWord` helpers; wire tag parsing after `WriteInvestigation` |
| `cmd/investigate_test.go` | Tests for `parseInvestigationTags` and tag persistence via `runInvestigate` |
| `internal/api/handlers.go` | Add `Confidence`, `Complexity`, `CodeFixable` to `IssueListItem`; populate from meta in `ListIssues` |
| `internal/api/handlers_test.go` | Test that tags are returned in list response |
| `web/src/api/client.ts` | Add `confidence`, `complexity`, `code_fixable` to `IssueListItem` interface |
| `web/src/components/InvestigationBadge.tsx` | New: colored badge for Confidence/Complexity values |
| `web/src/pages/Dashboard.tsx` | Selection state, bulk action bar, new filter dropdowns, new columns, updated grid layout |

---

## Task 1: Add investigation tag fields to MetaData and SetInvestigationTags

**Files:**
- Modify: `internal/reports/manager.go`
- Test: `internal/reports/manager_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/reports/manager_test.go`:

```go
func TestManager_SetInvestigationTags(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteMetadata("issue-1", &MetaData{Service: "svc-a"})

	if err := m.SetInvestigationTags("issue-1", "High", "Simple", "Yes"); err != nil {
		t.Fatalf("SetInvestigationTags: %v", err)
	}

	meta, err := m.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Confidence != "High" {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, "High")
	}
	if meta.Complexity != "Simple" {
		t.Errorf("Complexity: got %q, want %q", meta.Complexity, "Simple")
	}
	if meta.CodeFixable != "Yes" {
		t.Errorf("CodeFixable: got %q, want %q", meta.CodeFixable, "Yes")
	}
}

func TestManager_SetInvestigationTags_PreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteMetadata("issue-1", &MetaData{Service: "svc-b", Ignored: true, CIStatus: "passed"})

	m.SetInvestigationTags("issue-1", "Medium", "Moderate", "No")

	meta, _ := m.ReadMetadata("issue-1")
	if meta.Service != "svc-b" {
		t.Errorf("Service mutated: got %q", meta.Service)
	}
	if !meta.Ignored {
		t.Error("Ignored flag was reset")
	}
	if meta.CIStatus != "passed" {
		t.Errorf("CIStatus mutated: got %q", meta.CIStatus)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/reports/... -run TestManager_SetInvestigationTags -v
```

Expected: FAIL with `m.SetInvestigationTags undefined`

- [ ] **Step 3: Add fields to MetaData and implement SetInvestigationTags**

In `internal/reports/manager.go`, add three fields to `MetaData` (after the `CIURL` field):

```go
	Confidence  string `json:"confidence,omitempty"`
	Complexity  string `json:"complexity,omitempty"`
	CodeFixable string `json:"code_fixable,omitempty"`
```

Add the method after `SetIgnored`:

```go
func (m *Manager) SetInvestigationTags(issueID, confidence, complexity, codeFixable string) error {
	meta, err := m.ReadMetadata(issueID)
	if err != nil {
		return fmt.Errorf("reading metadata: %w", err)
	}
	meta.Confidence = confidence
	meta.Complexity = complexity
	meta.CodeFixable = codeFixable
	return m.WriteMetadata(issueID, meta)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/reports/... -v
```

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/reports/manager.go internal/reports/manager_test.go
git commit -m "feat: add Confidence/Complexity/CodeFixable fields to MetaData"
```

---

## Task 2: Add parseInvestigationTags and wire into runInvestigate

**Files:**
- Modify: `cmd/investigate.go`
- Test: `cmd/investigate_test.go`

- [ ] **Step 1: Write failing tests for parseInvestigationTags**

Add to `cmd/investigate_test.go`:

```go
func TestParseInvestigationTags_AllPresent(t *testing.T) {
	content := `## Root Cause
Some analysis here.

## Confidence: High

The stack trace points precisely to the issue.

## Complexity: Simple

No code changes required.

## Code Fixable: No
`
	conf, comp, fix := parseInvestigationTags(content)
	if conf != "High" {
		t.Errorf("confidence: got %q, want %q", conf, "High")
	}
	if comp != "Simple" {
		t.Errorf("complexity: got %q, want %q", comp, "Simple")
	}
	if fix != "No" {
		t.Errorf("codeFixable: got %q, want %q", fix, "No")
	}
}

func TestParseInvestigationTags_CaseInsensitive(t *testing.T) {
	content := "## confidence: medium\n## COMPLEXITY: Complex\n## Code Fixable: Yes\n"
	conf, comp, fix := parseInvestigationTags(content)
	if conf != "medium" {
		t.Errorf("confidence: got %q, want %q", conf, "medium")
	}
	if comp != "Complex" {
		t.Errorf("complexity: got %q, want %q", comp, "Complex")
	}
	if fix != "Yes" {
		t.Errorf("codeFixable: got %q, want %q", fix, "Yes")
	}
}

func TestParseInvestigationTags_MissingTags(t *testing.T) {
	content := "## Root Cause\nSome text only."
	conf, comp, fix := parseInvestigationTags(content)
	if conf != "" {
		t.Errorf("confidence: got %q, want empty", conf)
	}
	if comp != "" {
		t.Errorf("complexity: got %q, want empty", comp)
	}
	if fix != "" {
		t.Errorf("codeFixable: got %q, want empty", fix)
	}
}

func TestInvestigate_ParsesAndStoresTags(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "# Error\nNullPointerException")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})

	// Use echo to output a mock investigation with structured tags
	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: repoDir},
		},
		Agent: config.AgentConfig{
			// printf outputs the investigation report with tags
			Investigate: `bash -c printf '## Confidence: High\n## Complexity: Simple\n## Code Fixable: Yes\n'`,
		},
	}

	err := runInvestigate("issue-1", "svc-a", cfg, mgr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta, err := mgr.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Confidence != "High" {
		t.Errorf("Confidence: got %q, want High", meta.Confidence)
	}
	if meta.Complexity != "Simple" {
		t.Errorf("Complexity: got %q, want Simple", meta.Complexity)
	}
	if meta.CodeFixable != "Yes" {
		t.Errorf("CodeFixable: got %q, want Yes", meta.CodeFixable)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./cmd/... -run "TestParseInvestigationTags|TestInvestigate_ParsesAndStoresTags" -v
```

Expected: FAIL with `parseInvestigationTags undefined`

- [ ] **Step 3: Update the prompt template and add parsing helpers**

In `cmd/investigate.go`, update `investigatePromptTemplate` — add `Code Fixable` after `Complexity`:

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
- **Confidence**: High/Medium/Low
- **Complexity**: Simple/Moderate/Complex
- **Code Fixable**: Yes/No (is this a code defect that can be fixed with a code change?)
`
```

Add the two helpers after `extractServiceFromReport` in `cmd/investigate.go`:

```go
func parseInvestigationTags(content string) (confidence, complexity, codeFixable string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "## confidence:") {
			confidence = firstWord(strings.TrimSpace(line[len("## confidence:"):]))
		} else if strings.HasPrefix(lower, "## complexity:") {
			complexity = firstWord(strings.TrimSpace(line[len("## complexity:"):]))
		} else if strings.HasPrefix(lower, "## code fixable:") {
			codeFixable = firstWord(strings.TrimSpace(line[len("## code fixable:"):]))
		}
	}
	return
}

func firstWord(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
```

- [ ] **Step 4: Wire tag parsing in runInvestigate**

In `runInvestigate`, after `mgr.WriteInvestigation(...)` succeeds, add:

```go
	// Parse structured tags from the investigation and persist to metadata.
	// Non-fatal: old issues may not have meta.json.
	confidence, complexity, codeFixable := parseInvestigationTags(output)
	if err := mgr.SetInvestigationTags(issueID, confidence, complexity, codeFixable); err != nil {
		log.Printf("[investigate] %s: storing investigation tags (non-fatal): %v", issueID, err)
	}
```

The full `runInvestigate` tail (after the agent run) should read:

```go
	log.Printf("[investigate] %s: writing investigation report (%d bytes)", issueID, len(output))
	if err := mgr.WriteInvestigation(issueID, output); err != nil {
		return fmt.Errorf("writing investigation report: %w", err)
	}

	confidence, complexity, codeFixable := parseInvestigationTags(output)
	if err := mgr.SetInvestigationTags(issueID, confidence, complexity, codeFixable); err != nil {
		log.Printf("[investigate] %s: storing investigation tags (non-fatal): %v", issueID, err)
	}

	fmt.Printf("Investigation complete for %s\n", issueID)
	return nil
```

- [ ] **Step 5: Run all cmd tests**

```bash
go test ./cmd/... -v
```

Expected: All PASS

- [ ] **Step 6: Run full test suite**

```bash
go test ./...
```

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/investigate.go cmd/investigate_test.go
git commit -m "feat: parse and store investigation tags (Confidence, Complexity, Code Fixable)"
```

---

## Task 3: Expose investigation tags in API list response

**Files:**
- Modify: `internal/api/handlers.go`
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/api/handlers_test.go`:

```go
func TestListIssuesHandler_IncludesInvestigationTags(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:     "svc-a",
		Confidence:  "High",
		Complexity:  "Simple",
		CodeFixable: "Yes",
	})

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues", nil)
	w := httptest.NewRecorder()

	h.ListIssues(w, req)

	var resp []IssueListItem
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(resp))
	}
	if resp[0].Confidence != "High" {
		t.Errorf("Confidence: got %q, want High", resp[0].Confidence)
	}
	if resp[0].Complexity != "Simple" {
		t.Errorf("Complexity: got %q, want Simple", resp[0].Complexity)
	}
	if resp[0].CodeFixable != "Yes" {
		t.Errorf("CodeFixable: got %q, want Yes", resp[0].CodeFixable)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/api/... -run TestListIssuesHandler_IncludesInvestigationTags -v
```

Expected: FAIL — `IssueListItem` has no field `Confidence`

- [ ] **Step 3: Add fields to IssueListItem and populate in ListIssues**

In `internal/api/handlers.go`, add to the `IssueListItem` struct (after `CIURL`):

```go
	Confidence  string  `json:"confidence,omitempty"`
	Complexity  string  `json:"complexity,omitempty"`
	CodeFixable string  `json:"code_fixable,omitempty"`
```

In `ListIssues`, inside the `if issue.Meta != nil {` block, add after `item.CIURL = issue.Meta.CIURL`:

```go
		item.Confidence  = issue.Meta.Confidence
		item.Complexity  = issue.Meta.Complexity
		item.CodeFixable = issue.Meta.CodeFixable
```

- [ ] **Step 4: Run all API tests**

```bash
go test ./internal/api/... -v
```

Expected: All PASS

- [ ] **Step 5: Run full test suite**

```bash
go test ./...
```

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: expose investigation tags in API list response"
```

---

## Task 4: Add InvestigationBadge component and update API types

**Files:**
- Create: `web/src/components/InvestigationBadge.tsx`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add fields to IssueListItem in api/client.ts**

In `web/src/api/client.ts`, add to the `IssueListItem` interface (after `ci_url`):

```typescript
  confidence: string;
  complexity: string;
  code_fixable: string;
```

- [ ] **Step 2: Create InvestigationBadge component**

Create `web/src/components/InvestigationBadge.tsx`:

```tsx
const CONFIDENCE_STYLES: Record<string, string> = {
  high: 'bg-green-900/40 text-green-400 border-green-800',
  medium: 'bg-yellow-900/40 text-yellow-400 border-yellow-800',
  low: 'bg-red-900/40 text-red-400 border-red-800',
};

const COMPLEXITY_STYLES: Record<string, string> = {
  simple: 'bg-green-900/40 text-green-400 border-green-800',
  moderate: 'bg-yellow-900/40 text-yellow-400 border-yellow-800',
  complex: 'bg-red-900/40 text-red-400 border-red-800',
};

interface InvestigationBadgeProps {
  type: 'confidence' | 'complexity';
  value: string;
}

export function InvestigationBadge({ type, value }: InvestigationBadgeProps) {
  if (!value) return <span className="text-muted-foreground text-xs">—</span>;

  const styles = type === 'confidence' ? CONFIDENCE_STYLES : COMPLEXITY_STYLES;
  const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${
    styles[value.toLowerCase()] ?? 'bg-muted text-muted-foreground border-border'
  }`;

  return <span className={classes}>{value}</span>;
}
```

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts web/src/components/InvestigationBadge.tsx
git commit -m "feat: add InvestigationBadge component and investigation tag types"
```

---

## Task 5: Update Dashboard — new columns and grid layout

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: Add imports for InvestigationBadge**

At the top of `web/src/pages/Dashboard.tsx`, add `InvestigationBadge` to the imports:

```typescript
import { InvestigationBadge } from '../components/InvestigationBadge';
```

- [ ] **Step 2: Update grid layout and header row**

Replace the header row `<div>` in Dashboard.tsx. Change:

```tsx
          <div className="grid grid-cols-[2.5fr_1fr_1fr_0.8fr_0.8fr_60px] px-4 py-2 bg-muted/50 text-xs font-semibold text-muted-foreground tracking-wide uppercase border-b border-border">
            <span>Issue</span>
            <span>Service</span>
            <span>Stage</span>
            <span>CI</span>
            <span>MR</span>
            <span />
          </div>
```

To:

```tsx
          <div className="grid grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.5fr_0.8fr_0.8fr] px-4 py-2 bg-muted/50 text-xs font-semibold text-muted-foreground tracking-wide uppercase border-b border-border">
            <span />
            <span>Issue</span>
            <span>Service</span>
            <span>Stage</span>
            <span>Confidence</span>
            <span>Complexity</span>
            <span>Fixable</span>
            <span>CI</span>
            <span>MR</span>
          </div>
```

- [ ] **Step 3: Update main row grid and add new columns**

Replace the main row `<div>` (the one with `onClick={() => toggleRow(issue.id)}`). Change:

```tsx
              <div
                className="grid grid-cols-[2.5fr_1fr_1fr_0.8fr_0.8fr_60px] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-colors"
                onClick={() => toggleRow(issue.id)}
              >
```

To:

```tsx
              <div
                className="grid grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.5fr_0.8fr_0.8fr] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-colors"
                onClick={() => toggleRow(issue.id)}
              >
```

Inside the main row, after the opening `<div>`, add a placeholder checkbox cell (selection is wired in Task 6):

```tsx
                <span />
```

And replace the closing MR span and the `···` span. Remove the `···` span entirely. The new row body in full (replace everything between the two outermost `<span>` blocks):

```tsx
                <span />
                <span className="font-medium text-sm truncate pr-2">
                  {issue.title || issue.id}
                  {issue.message && (
                    <span className="ml-1.5 text-muted-foreground font-normal">
                      — {issue.message.length > 200 ? issue.message.slice(0, 200) + '…' : issue.message}
                    </span>
                  )}
                  {expandedId === issue.id && (
                    <span className="ml-1.5 text-blue-400 text-xs">▾</span>
                  )}
                </span>
                <span className="text-xs text-muted-foreground">{issue.service}</span>
                <span>
                  <StageIndicator stage={issue.stage} />
                </span>
                <span>
                  <InvestigationBadge type="confidence" value={issue.confidence} />
                </span>
                <span>
                  <InvestigationBadge type="complexity" value={issue.complexity} />
                </span>
                <span>
                  {issue.code_fixable === 'Yes' && <span className="text-green-400 text-sm">✓</span>}
                  {issue.code_fixable === 'No' && <span className="text-red-400 text-sm">✗</span>}
                  {!issue.code_fixable && <span className="text-muted-foreground text-xs">—</span>}
                </span>
                <span>
                  <CIStatusBadge status={issue.ci_status} url={issue.ci_url || undefined} />
                </span>
                <span>
                  {issue.mr_url ? (
                    <a
                      href={issue.mr_url}
                      target="_blank"
                      rel="noreferrer"
                      className="text-blue-400 text-xs hover:underline"
                      onClick={(e) => e.stopPropagation()}
                    >
                      MR ↗
                    </a>
                  ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                  )}
                </span>
```

Note: CI badge now shows regardless of whether `mr_url` exists (the old condition `{issue.mr_url ? <CIStatusBadge ...> : —}` is removed).

- [ ] **Step 4: Start dev server and run Playwright verification**

In terminal 1:
```bash
cd web && npm run dev
```

In terminal 2:
```bash
cd web && node verify.mjs
```

Expected: Exit 0 (no React errors)

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/Dashboard.tsx web/src/components/InvestigationBadge.tsx
git commit -m "feat: add Confidence/Complexity/Fixable columns to dashboard table"
```

---

## Task 6: Add selection state and bulk action bar to Dashboard

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: Add selectedIds state**

In `Dashboard.tsx`, add after the existing `useState` declarations:

```typescript
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
```

Also clear selection when issues re-fetch. In `fetchIssues`, after `setIssues(data)`:

```typescript
      setIssues(data);
      setSelectedIds(new Set());
```

- [ ] **Step 2: Add bulk action handlers**

Add after `handleIgnore`:

```typescript
  const handleBulkIgnore = async () => {
    const toIgnore = issues.filter(i => selectedIds.has(i.id) && !i.ignored);
    await Promise.all(toIgnore.map(i => ignoreIssue(i.id)));
    await fetchIssues();
  };

  const handleBulkUnignore = async () => {
    const toUnignore = issues.filter(i => selectedIds.has(i.id) && i.ignored);
    await Promise.all(toUnignore.map(i => unignoreIssue(i.id)));
    await fetchIssues();
  };
```

- [ ] **Step 3: Add select-all logic**

Add before the return statement:

```typescript
  const allSelected = issues.length > 0 && issues.every(i => selectedIds.has(i.id));
  const someSelected = selectedIds.size > 0;

  const toggleSelectAll = () => {
    if (allSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(issues.map(i => i.id)));
    }
  };

  const toggleSelectOne = (id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
```

- [ ] **Step 4: Wire select-all checkbox into header**

In the header row, replace the first `<span />` with:

```tsx
            <span>
              <Checkbox
                checked={allSelected}
                onCheckedChange={toggleSelectAll}
                className="w-3.5 h-3.5"
                onClick={(e) => e.stopPropagation()}
              />
            </span>
```

- [ ] **Step 5: Wire per-row checkbox**

In the main row, replace the placeholder `<span />` (first cell) with:

```tsx
                <span onClick={(e) => e.stopPropagation()}>
                  <Checkbox
                    checked={selectedIds.has(issue.id)}
                    onCheckedChange={() => toggleSelectOne(issue.id)}
                    className="w-3.5 h-3.5"
                  />
                </span>
```

Also add selected row highlight. Change the main row className from:

```tsx
                className="grid grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.5fr_0.8fr_0.8fr] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-colors"
```

To:

```tsx
                className={`grid grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.5fr_0.8fr_0.8fr] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-colors ${selectedIds.has(issue.id) ? 'bg-blue-950/30' : ''}`}
```

- [ ] **Step 6: Add bulk action bar between toolbar and table**

In the JSX, between the toolbar `<div>` and the table `<div>`, add:

```tsx
      {someSelected && (
        <div className="flex items-center gap-3 px-4 py-2 bg-blue-950/30 border-b border-blue-900 text-xs">
          <span className="text-blue-300 font-medium">{selectedIds.size} selected</span>
          <Button size="sm" variant="outline" className="h-6 text-xs" onClick={handleBulkIgnore}>
            Ignore
          </Button>
          <Button size="sm" variant="outline" className="h-6 text-xs" onClick={handleBulkUnignore}>
            Unignore
          </Button>
          <button
            className="ml-auto text-muted-foreground hover:text-foreground text-xs"
            onClick={() => setSelectedIds(new Set())}
          >
            Clear selection
          </button>
        </div>
      )}
```

- [ ] **Step 7: Run Playwright verification**

```bash
# Terminal 1 (if not already running)
cd web && npm run dev

# Terminal 2
cd web && node verify.mjs
```

Expected: Exit 0

- [ ] **Step 8: Commit**

```bash
git add web/src/pages/Dashboard.tsx
git commit -m "feat: add checkbox selection and bulk ignore/unignore to dashboard"
```

---

## Task 7: Add service, confidence, complexity, and code-fixable filters

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: Add filter state**

In `Dashboard.tsx`, add after existing `useState` declarations:

```typescript
  const [serviceFilter, setServiceFilter] = useState('all');
  const [confidenceFilter, setConfidenceFilter] = useState('all');
  const [complexityFilter, setComplexityFilter] = useState('all');
  const [codeFixableOnly, setCodeFixableOnly] = useState(false);
```

- [ ] **Step 2: Derive unique services and filtered issues**

Add before the return statement (after the existing `allSelected`/`someSelected` block):

```typescript
  const services = [...new Set(issues.map(i => i.service).filter(Boolean))].sort();

  const filteredIssues = issues.filter(issue => {
    if (serviceFilter !== 'all' && issue.service !== serviceFilter) return false;
    if (confidenceFilter !== 'all' && issue.confidence.toLowerCase() !== confidenceFilter.toLowerCase()) return false;
    if (complexityFilter !== 'all' && issue.complexity.toLowerCase() !== complexityFilter.toLowerCase()) return false;
    if (codeFixableOnly && issue.code_fixable !== 'Yes') return false;
    return true;
  });
```

- [ ] **Step 3: Replace issues.map with filteredIssues.map in the table**

In the JSX, change the table `issues.map(...)` to `filteredIssues.map(...)`:

```tsx
          {filteredIssues.map((issue) => (
```

Also update the `allSelected` / `toggleSelectAll` logic to operate on `filteredIssues` instead of `issues`:

```typescript
  const allSelected = filteredIssues.length > 0 && filteredIssues.every(i => selectedIds.has(i.id));

  const toggleSelectAll = () => {
    if (allSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(filteredIssues.map(i => i.id)));
    }
  };
```

And in the empty state check, update to show filtered count:

```tsx
      ) : filteredIssues.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No issues match the current filters.</p>
```

Keep the outer `issues.length === 0` check for the "no issues found at all" case. The condition chain becomes:

```tsx
      {loading ? (
        <p className="p-4 text-sm text-muted-foreground">Loading…</p>
      ) : issues.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No issues found. Run a scan to get started.</p>
      ) : filteredIssues.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No issues match the current filters.</p>
      ) : (
```

- [ ] **Step 4: Update issue count badge to show filtered/total**

In the toolbar, replace:

```tsx
          <span className="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs">
            {issues.length}
          </span>
```

With:

```tsx
          <span className="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs">
            {filteredIssues.length === issues.length
              ? issues.length
              : `${filteredIssues.length} / ${issues.length}`}
          </span>
```

- [ ] **Step 5: Add filter dropdowns to the toolbar**

In the toolbar's filter `<div>` (the one with `className="flex items-center gap-3"`), add the new filters after the existing stage Select and before the "Show ignored" checkbox:

```tsx
          {services.length > 0 && (
            <Select value={serviceFilter} onValueChange={setServiceFilter}>
              <SelectTrigger className="w-36 h-7 text-xs">
                <SelectValue placeholder="All services" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All services</SelectItem>
                {services.map(svc => (
                  <SelectItem key={svc} value={svc}>{svc}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Select value={confidenceFilter} onValueChange={setConfidenceFilter}>
            <SelectTrigger className="w-36 h-7 text-xs">
              <SelectValue placeholder="All confidence" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All confidence</SelectItem>
              <SelectItem value="High">High</SelectItem>
              <SelectItem value="Medium">Medium</SelectItem>
              <SelectItem value="Low">Low</SelectItem>
            </SelectContent>
          </Select>
          <Select value={complexityFilter} onValueChange={setComplexityFilter}>
            <SelectTrigger className="w-36 h-7 text-xs">
              <SelectValue placeholder="All complexity" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All complexity</SelectItem>
              <SelectItem value="Simple">Simple</SelectItem>
              <SelectItem value="Moderate">Moderate</SelectItem>
              <SelectItem value="Complex">Complex</SelectItem>
            </SelectContent>
          </Select>
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
            <Checkbox
              checked={codeFixableOnly}
              onCheckedChange={(v) => setCodeFixableOnly(!!v)}
              className="w-3.5 h-3.5"
            />
            Code fixable only
          </label>
```

- [ ] **Step 6: Run Playwright verification**

```bash
cd web && node verify.mjs
```

Expected: Exit 0

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/Dashboard.tsx
git commit -m "feat: add service, confidence, complexity, and code-fixable filters to dashboard"
```

---

## Task 8: Final verification and cleanup

- [ ] **Step 1: Run full Go test suite**

```bash
go test ./...
```

Expected: All PASS

- [ ] **Step 2: Build Go binary to check for compilation errors**

```bash
go build -o fido .
```

Expected: No errors, `fido` binary produced

- [ ] **Step 3: Run Playwright frontend verification**

```bash
# Terminal 1
cd web && npm run dev

# Terminal 2
cd web && node verify.mjs
```

Expected: Exit 0

- [ ] **Step 4: Clean up build artifact**

```bash
rm fido
```

- [ ] **Step 5: Final commit if any loose changes**

```bash
git status
# Only commit if there are uncommitted changes
```
