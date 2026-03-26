# GitLab CI Status Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Worktree:** Use superpowers:using-git-worktrees to create an isolated worktree before starting.

**Goal:** Add GitLab CI pipeline status to Fido — fetched during scan, shown in the web UI, and used to drive `fix --iterate` when CI is failing.

**Architecture:** `glab` CLI subprocess (same pattern as existing agent runner) fetches CI status and logs. Status stored in `meta.json`. The `fix` command gains an `--iterate` flag that feeds CI failure logs + previous fix content into a different agent prompt. The web API exposes CI fields on existing response types; the frontend shows a badge and a "Re-fix (CI failing)" button.

**Tech Stack:** Go (cobra, chi), `glab` CLI subprocess, React/TypeScript (Vite, Tailwind, shadcn/ui)

**Spec:** `docs/superpowers/specs/2026-03-26-gitlab-ci-status-design.md`

---

## File Map

| File | Change |
|------|--------|
| `internal/gitlab/ci.go` | **Create** — `FetchCIStatus`, `FetchCIFailureLogs` |
| `internal/gitlab/ci_test.go` | **Create** — tests for both functions |
| `internal/reports/manager.go` | **Modify** — add `CIStatus`/`CIURL` to `MetaData`; add `ReadLatestFix` |
| `internal/reports/manager_test.go` | **Modify** — tests for new methods |
| `cmd/scan.go` | **Modify** — add `refreshCIStatuses`; include `Repositories` in `scanCfg` |
| `cmd/fix.go` | **Modify** — add `--iterate` flag and `runFixIterate` function |
| `internal/api/handlers.go` | **Modify** — CI fields on response types; `FixFunc` signature; `TriggerFix` reads body |
| `cmd/serve.go` | **Modify** — update `SetFixFunc` lambda to route iterate flag |
| `web/src/api/client.ts` | **Modify** — CI fields on interfaces; `triggerFix` accepts iterate option |
| `web/src/components/CIStatusBadge.tsx` | **Create** — shared CI status badge component |
| `web/src/pages/Dashboard.tsx` | **Modify** — CI badge column |
| `web/src/pages/IssueDetail.tsx` | **Modify** — CI badge + "Re-fix" button in Resolution section |

---

## Task 1: Data model — `MetaData` CI fields and `ReadLatestFix`

**Files:**
- Modify: `internal/reports/manager.go`
- Modify: `internal/reports/manager_test.go`

- [ ] **Step 1.1: Write the failing tests**

Add to `internal/reports/manager_test.go`:

```go
func TestManager_MetaData_CIStatus(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	meta := &MetaData{
		Title:    "SomeError",
		Service:  "svc-a",
		CIStatus: "failed",
		CIURL:    "https://gitlab.com/org/repo/-/pipelines/42",
	}
	if err := m.WriteMetadata("issue-1", meta); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}
	got, err := m.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if got.CIStatus != "failed" {
		t.Errorf("CIStatus: got %q, want %q", got.CIStatus, "failed")
	}
	if got.CIURL != meta.CIURL {
		t.Errorf("CIURL: got %q, want %q", got.CIURL, meta.CIURL)
	}
}

func TestManager_ReadLatestFix_FirstIteration(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteFix("issue-1", "first fix content")

	content, iter, err := m.ReadLatestFix("issue-1")
	if err != nil {
		t.Fatalf("ReadLatestFix: %v", err)
	}
	if iter != 1 {
		t.Errorf("iter: got %d, want 1", iter)
	}
	if content != "first fix content" {
		t.Errorf("content: got %q", content)
	}
}

func TestManager_ReadLatestFix_SecondIteration(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteFix("issue-1", "first fix")
	m.writeFile("issue-1", "fix-2.md", "second fix content")

	content, iter, err := m.ReadLatestFix("issue-1")
	if err != nil {
		t.Fatalf("ReadLatestFix: %v", err)
	}
	if iter != 2 {
		t.Errorf("iter: got %d, want 2", iter)
	}
	if content != "second fix content" {
		t.Errorf("content: got %q", content)
	}
}

func TestManager_ReadLatestFix_NoFix(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	_, _, err := m.ReadLatestFix("issue-1")
	if err == nil {
		t.Error("expected error when no fix exists")
	}
}
```

- [ ] **Step 1.2: Run tests to verify they fail**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./internal/reports/... -run "TestManager_MetaData_CIStatus|TestManager_ReadLatestFix" -v
```

Expected: FAIL — `CIStatus` field undefined, `ReadLatestFix` undefined.

- [ ] **Step 1.3: Add `CIStatus`/`CIURL` to `MetaData` struct**

In `internal/reports/manager.go`, replace the `MetaData` struct:

```go
type MetaData struct {
	Title            string `json:"title"`
	Service          string `json:"service"`
	Env              string `json:"env"`
	FirstSeen        string `json:"first_seen"`
	LastSeen         string `json:"last_seen"`
	Count            int64  `json:"count"`
	DatadogURL       string `json:"datadog_url"`
	DatadogEventsURL string `json:"datadog_events_url"`
	DatadogTraceURL  string `json:"datadog_trace_url"`
	Ignored          bool   `json:"ignored"`
	CIStatus         string `json:"ci_status,omitempty"`
	CIURL            string `json:"ci_url,omitempty"`
}
```

- [ ] **Step 1.4: Add `ReadLatestFix` method**

Add after the existing `ReadFix` method in `internal/reports/manager.go`:

```go
// ReadLatestFix returns the content and iteration number of the most recent fix file.
// Iteration 1 = fix.md, iteration 2 = fix-2.md, etc.
// Returns an error if no fix file exists.
func (m *Manager) ReadLatestFix(issueID string) (string, int, error) {
	// Walk down from high iterations to find the latest
	for n := 10; n >= 2; n-- {
		filename := fmt.Sprintf("fix-%d.md", n)
		if m.fileExists(issueID, filename) {
			content, err := m.readFile(issueID, filename)
			return content, n, err
		}
	}
	// Fall back to fix.md (iteration 1)
	content, err := m.readFile(issueID, "fix.md")
	if err != nil {
		return "", 0, fmt.Errorf("no fix file found for %s", issueID)
	}
	return content, 1, nil
}
```

- [ ] **Step 1.5: Run tests to verify they pass**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./internal/reports/... -v
```

Expected: all PASS.

- [ ] **Step 1.6: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add internal/reports/manager.go internal/reports/manager_test.go
git commit -m "feat(reports): add CIStatus/CIURL to MetaData and ReadLatestFix method"
```

---

## Task 2: `internal/gitlab` package — CI status and log fetching

**Files:**
- Create: `internal/gitlab/ci.go`
- Create: `internal/gitlab/ci_test.go`

- [ ] **Step 2.1: Write the failing tests**

Create `internal/gitlab/ci_test.go`:

```go
package gitlab

import (
	"os"
	"os/exec"
	"testing"
)

func TestParseCIStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"✓ Pipeline passed after 2m 30s", "passed"},
		{"✗ Pipeline failed after 1m 15s", "failed"},
		{"Pipeline running", "running"},
		{"Pipeline pending", "pending"},
		{"Pipeline canceled", "canceled"},
		{"no known status here", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := parseCIStatus(tt.input)
		if got != tt.expected {
			t.Errorf("parseCIStatus(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractPipelineURL(t *testing.T) {
	output := "Pipeline #42 passed\nhttps://gitlab.com/org/repo/-/pipelines/42\nsome other line"
	got := extractPipelineURL(output)
	if got != "https://gitlab.com/org/repo/-/pipelines/42" {
		t.Errorf("extractPipelineURL = %q", got)
	}
}

func TestExtractPipelineURL_NoURL(t *testing.T) {
	got := extractPipelineURL("no url here")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestFetchCIStatus_GlabNotFound verifies graceful error when glab not in PATH.
func TestFetchCIStatus_GlabNotFound(t *testing.T) {
	if _, err := exec.LookPath("glab"); err == nil {
		t.Skip("glab is installed; skipping not-found test")
	}
	dir := t.TempDir()
	// Create a minimal git repo so glab doesn't fail for wrong reason
	os.MkdirAll(dir+"/.git", 0755)

	_, _, err := FetchCIStatus("main", dir)
	if err == nil {
		t.Error("expected error when glab not found")
	}
}
```

- [ ] **Step 2.2: Run tests to verify they fail**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./internal/gitlab/... -v 2>&1 | head -20
```

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 2.3: Create `internal/gitlab/ci.go`**

```go
package gitlab

import (
	"fmt"
	"os/exec"
	"strings"
)

// FetchCIStatus runs `glab ci status --branch <branch>` in repoPath and returns
// the pipeline status ("passed", "failed", "running", "pending", "canceled", or "")
// and the pipeline URL. Returns ("", "", nil) if no pipeline found.
func FetchCIStatus(branch, repoPath string) (status, pipelineURL string, err error) {
	cmd := exec.Command("glab", "ci", "status", "--branch", branch)
	cmd.Dir = repoPath
	out, execErr := cmd.CombinedOutput()
	output := string(out)

	// glab exits non-zero for failed pipelines but still outputs useful info.
	// Only treat as error if there's no output at all.
	if execErr != nil && len(strings.TrimSpace(output)) == 0 {
		return "", "", fmt.Errorf("glab ci status: %w", execErr)
	}

	status = parseCIStatus(output)
	pipelineURL = extractPipelineURL(output)
	return status, pipelineURL, nil
}

// FetchCIFailureLogs runs `glab ci view --branch <branch>` in repoPath and
// returns the output as a string for use in fix iteration prompts.
func FetchCIFailureLogs(branch, repoPath string) (string, error) {
	cmd := exec.Command("glab", "ci", "view", "--branch", branch)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil && len(strings.TrimSpace(output)) == 0 {
		return "", fmt.Errorf("glab ci view: %w", err)
	}
	return output, nil
}

// parseCIStatus searches output for known GitLab pipeline status strings.
func parseCIStatus(output string) string {
	lower := strings.ToLower(output)
	for _, s := range []string{"passed", "failed", "running", "pending", "canceled"} {
		if strings.Contains(lower, s) {
			return s
		}
	}
	return ""
}

// extractPipelineURL finds the first https://gitlab.com/... URL in the output.
func extractPipelineURL(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") && strings.Contains(line, "pipelines") {
			return line
		}
	}
	return ""
}
```

- [ ] **Step 2.4: Run tests to verify they pass**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./internal/gitlab/... -v
```

Expected: all PASS.

- [ ] **Step 2.5: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add internal/gitlab/
git commit -m "feat(gitlab): add FetchCIStatus and FetchCIFailureLogs using glab CLI"
```

---

## Task 3: CI status refresh during scan

**Files:**
- Modify: `cmd/scan.go`
- Modify: `cmd/scan_test.go`

- [ ] **Step 3.1: Write the failing test**

Add to `cmd/scan_test.go`:

```go
func TestRunScan_CIRefresh_SkipsWhenNoResolve(t *testing.T) {
	// An issue with no resolve.json should not cause any CI-related errors.
	server := newTestScanServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	// Pre-create issue so scan skips it (it already exists)
	mgr.WriteError("issue-1", "existing")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})
	// No resolve.json → CI refresh should be a no-op

	ddClient := newTestDDClient(t, server.URL)
	cfg := &config.Config{
		Datadog: config.DatadogConfig{Services: []string{"svc-a"}},
		Scan:    config.ScanConfig{Since: "24h"},
	}

	_, err := runScan(cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// meta.json should still have empty CIStatus
	meta, _ := mgr.ReadMetadata("issue-1")
	if meta.CIStatus != "" {
		t.Errorf("expected empty CIStatus, got %q", meta.CIStatus)
	}
}
```

- [ ] **Step 3.2: Run test to verify it fails**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./cmd/... -run TestRunScan_CIRefresh_SkipsWhenNoResolve -v
```

Expected: FAIL — compile error (function signature will change in next step) or logic issue.

- [ ] **Step 3.3: Add `refreshCIStatuses` to `cmd/scan.go` and include Repositories in scanCfg**

First, update the scan CLI command to include `Repositories` in `scanCfg` (so CI refresh can resolve repo paths). In `scanCmd.RunE`, replace the `scanCfg` construction:

```go
scanCfg := &config.Config{
    Datadog:      config.DatadogConfig{Services: services, Site: cfg.Datadog.Site, OrgSubdomain: cfg.Datadog.OrgSubdomain},
    Scan:         config.ScanConfig{Since: since},
    Repositories: cfg.Repositories,
}
```

Then add the `refreshCIStatuses` function at the bottom of `cmd/scan.go`. Add import `"log"` and `"github.com/ruter-as/fido/internal/gitlab"` to the import block:

```go
// refreshCIStatuses updates ci_status and ci_url in meta.json for all issues
// that have a resolve.json (i.e. a branch and MR). Non-fatal: logs and skips on error.
func refreshCIStatuses(cfg *config.Config, mgr *reports.Manager) {
	issues, err := mgr.ListIssues(true) // include ignored
	if err != nil {
		log.Printf("CI refresh: listing issues: %v", err)
		return
	}
	for _, issue := range issues {
		resolve, err := mgr.ReadResolve(issue.ID)
		if err != nil || resolve.Branch == "" {
			continue
		}
		repoPath, err := resolveRepoPath(resolve.Service, cfg)
		if err != nil {
			log.Printf("CI refresh: no repo for service %q (issue %s): %v", resolve.Service, issue.ID, err)
			continue
		}
		status, ciURL, err := gitlab.FetchCIStatus(resolve.Branch, repoPath)
		if err != nil {
			log.Printf("CI refresh: glab failed for %s: %v", issue.ID, err)
			continue
		}
		meta, err := mgr.ReadMetadata(issue.ID)
		if err != nil {
			log.Printf("CI refresh: reading meta for %s: %v", issue.ID, err)
			continue
		}
		meta.CIStatus = status
		meta.CIURL = ciURL
		if err := mgr.WriteMetadata(issue.ID, meta); err != nil {
			log.Printf("CI refresh: writing meta for %s: %v", issue.ID, err)
		}
	}
}
```

Then call `refreshCIStatuses` at the end of `runScan`, after the main loop:

```go
func runScan(cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) (int, error) {
	// ... existing code unchanged ...

	// After processing new issues, refresh CI statuses for all fixed issues.
	refreshCIStatuses(cfg, mgr)

	return count, nil
}
```

- [ ] **Step 3.4: Run tests**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./cmd/... -v
```

Expected: all PASS. (The CI refresh test passes because there's no resolve.json on the pre-created issue, so `ReadResolve` returns error and the loop skips it.)

- [ ] **Step 3.5: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add cmd/scan.go cmd/scan_test.go
git commit -m "feat(scan): refresh GitLab CI status for fixed issues during scan"
```

---

## Task 4: `fix --iterate` command

**Files:**
- Modify: `cmd/fix.go`
- Modify: `cmd/fix_test.go`

- [ ] **Step 4.1: Write the failing tests**

Read `cmd/fix_test.go` first to understand patterns, then add:

```go
func TestRunFixIterate_RequiresResolve(t *testing.T) {
	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "**Service:** svc-a\nerror content")
	mgr.WriteInvestigation("issue-1", "investigation content")
	mgr.WriteFix("issue-1", "previous fix content")
	// No resolve.json

	cfg := &config.Config{
		Agent: config.AgentConfig{Fix: "echo"},
	}

	err := runFixIterate("issue-1", "svc-a", cfg, mgr, nil)
	if err == nil {
		t.Error("expected error when resolve.json is missing")
	}
	if !strings.Contains(err.Error(), "resolve") {
		t.Errorf("expected error to mention resolve, got: %v", err)
	}
}

func TestRunFixIterate_RequiresPreviousFix(t *testing.T) {
	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "**Service:** svc-a\nerror content")
	mgr.WriteInvestigation("issue-1", "investigation content")
	// No fix.md
	mgr.WriteResolve("issue-1", &reports.ResolveData{
		Branch: "fix/issue-1-foo",
		MRURL:  "https://gitlab.com/org/repo/-/merge_requests/1",
	})

	cfg := &config.Config{
		Agent: config.AgentConfig{Fix: "echo"},
	}

	err := runFixIterate("issue-1", "svc-a", cfg, mgr, nil)
	if err == nil {
		t.Error("expected error when no fix.md exists")
	}
	if !strings.Contains(err.Error(), "fix") {
		t.Errorf("expected error to mention fix, got: %v", err)
	}
}
```

- [ ] **Step 4.2: Run tests to verify they fail**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./cmd/... -run "TestRunFixIterate" -v 2>&1 | head -20
```

Expected: FAIL — `runFixIterate` undefined.

- [ ] **Step 4.3: Add `--iterate` flag and `runFixIterate` to `cmd/fix.go`**

Add imports `"github.com/ruter-as/fido/internal/gitlab"` and `"log"` to `cmd/fix.go`.

Add the iterate prompt template constant after `fixPromptTemplate`:

```go
const fixIteratePromptTemplate = `You are iterating on a fix for a production error. A previous fix attempt was made but CI is failing.

## Error Report

%s

## Investigation

%s

## Previous Fix Attempt

%s

## CI Failure Output

%s

## Instructions

The branch ` + "`%s`" + ` already exists with an open MR at %s.

1. Review the CI failure output above to understand what is failing
2. Make the necessary code changes to fix the CI failures
3. Commit your changes to the existing branch ` + "`%s`" + ` with a conventional commit message: fix(<service>): <description>
4. Push the branch — do NOT create a new branch or new MR
5. Write a summary to %s with these sections:
   - **Summary**: What CI was failing and what was changed
   - **Files Changed**: List of modified files
   - **Tests**: Any test results
`
```

Add the `runFixIterate` function after `runFix`:

```go
func runFixIterate(issueID, service string, cfg *config.Config, mgr *reports.Manager, progress io.Writer) error {
	errorContent, err := mgr.ReadError(issueID)
	if err != nil {
		return fmt.Errorf("no error report for issue %s: %w", issueID, err)
	}

	investigationContent, err := mgr.ReadInvestigation(issueID)
	if err != nil {
		return fmt.Errorf("no investigation for issue %s — run 'fido investigate %s' first: %w", issueID, issueID, err)
	}

	resolve, err := mgr.ReadResolve(issueID)
	if err != nil {
		return fmt.Errorf("no resolve.json for issue %s — run 'fido fix %s' first: %w", issueID, issueID, err)
	}

	fixContent, currentIter, err := mgr.ReadLatestFix(issueID)
	if err != nil {
		return fmt.Errorf("no fix file for issue %s — run 'fido fix %s' first: %w", issueID, issueID, err)
	}

	repoPath, err := resolveRepoPath(service, cfg)
	if err != nil {
		return err
	}

	ciLogs, err := gitlab.FetchCIFailureLogs(resolve.Branch, repoPath)
	if err != nil {
		log.Printf("warning: could not fetch CI logs for %s: %v", issueID, err)
		ciLogs = "(CI logs unavailable — check GitLab pipeline manually)"
	}

	home, _ := os.UserHomeDir()
	issueReportsDir := filepath.Join(home, ".fido", "reports", issueID)
	nextFixFilename := fmt.Sprintf("fix-%d.md", currentIter+1)
	nextFixPath := filepath.Join(issueReportsDir, nextFixFilename)

	prompt := fmt.Sprintf(fixIteratePromptTemplate,
		errorContent, investigationContent, fixContent, ciLogs,
		resolve.Branch, resolve.MRURL,
		resolve.Branch,
		nextFixPath,
	)

	runner := &agent.Runner{Command: cfg.Agent.Fix, Progress: progress}
	if err := runner.RunInteractive(prompt, repoPath); err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	if _, _, err := mgr.ReadLatestFix(issueID); err == nil {
		fmt.Printf("Fix iteration complete for %s (wrote %s)\n", issueID, nextFixFilename)
	} else {
		fmt.Fprintf(os.Stderr, "Warning: %s was not written — the session may have exited early.\n", nextFixFilename)
	}

	return nil
}
```

Update `fixCmd.RunE` to handle the `--iterate` flag (replace the final `return runFix(...)` line):

```go
iterate, _ := cmd.Flags().GetBool("iterate")
if iterate {
    return runFixIterate(issueID, service, cfg, mgr, nil)
}
return runFix(issueID, service, cfg, mgr, nil)
```

Add the flag to `init()`:

```go
fixCmd.Flags().Bool("iterate", false, "iterate on an existing fix using CI failure logs")
```

- [ ] **Step 4.4: Run tests**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./cmd/... -run "TestRunFixIterate" -v
```

Expected: `TestRunFixIterate_RequiresResolve` and `TestRunFixIterate_RequiresPreviousFix` both PASS.

- [ ] **Step 4.5: Run all tests**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./...
```

Expected: all PASS.

- [ ] **Step 4.6: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add cmd/fix.go cmd/fix_test.go
git commit -m "feat(fix): add --iterate flag with CI failure context prompt"
```

---

## Task 5: API — CI fields on response types and iterate support

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go` (if tests exist for affected methods)

- [ ] **Step 5.1: Read `internal/api/handlers_test.go` to understand test patterns**

Read the file before making changes.

- [ ] **Step 5.2: Write the failing tests**

Add to `internal/api/handlers_test.go`:

```go
func TestTriggerFix_IterateFlag(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	var gotIterate bool
	h := NewHandlers(mgr, &config.Config{})
	h.SetFixFunc(func(id string, iterate bool, progress io.Writer) error {
		gotIterate = iterate
		return nil
	})

	body := strings.NewReader(`{"iterate":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/issues/issue-1/fix", body)
	req = withURLParam(req, "id", "issue-1")
	w := httptest.NewRecorder()
	h.TriggerFix(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusAccepted)
	}

	// Give goroutine time to run
	time.Sleep(50 * time.Millisecond)
	if !gotIterate {
		t.Error("expected iterate=true to be passed to FixFunc")
	}
}

func TestListIssues_IncludesCIStatus(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:  "svc-a",
		CIStatus: "failed",
		CIURL:    "https://gitlab.com/org/repo/-/pipelines/42",
	})

	h := NewHandlers(mgr, &config.Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	w := httptest.NewRecorder()
	h.ListIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var items []IssueListItem
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].CIStatus != "failed" {
		t.Errorf("CIStatus: got %q, want %q", items[0].CIStatus, "failed")
	}
}
```

- [ ] **Step 5.3: Run tests to verify they fail**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./internal/api/... -run "TestTriggerFix_IterateFlag|TestListIssues_IncludesCIStatus" -v 2>&1 | head -30
```

Expected: FAIL — `FixFunc` signature mismatch, `CIStatus` field undefined.

- [ ] **Step 5.4: Update `internal/api/handlers.go`**

Update `FixFunc` type:

```go
type FixFunc func(issueID string, iterate bool, progress io.Writer) error
```

Update `IssueListItem` struct (add two fields):

```go
type IssueListItem struct {
	ID       string  `json:"id"`
	Stage    string  `json:"stage"`
	Title    string  `json:"title,omitempty"`
	Service  string  `json:"service,omitempty"`
	LastSeen string  `json:"last_seen,omitempty"`
	Count    int64   `json:"count,omitempty"`
	MRURL    *string `json:"mr_url"`
	Ignored  bool    `json:"ignored"`
	CIStatus string  `json:"ci_status,omitempty"`
	CIURL    string  `json:"ci_url,omitempty"`
}
```

Update `IssueDetail` struct (add two fields):

```go
type IssueDetail struct {
	ID            string               `json:"id"`
	Stage         string               `json:"stage"`
	Error         string               `json:"error"`
	Investigation *string              `json:"investigation"`
	Fix           *string              `json:"fix"`
	Resolve       *reports.ResolveData `json:"resolve"`
	CIStatus      string               `json:"ci_status,omitempty"`
	CIURL         string               `json:"ci_url,omitempty"`
}
```

In `ListIssues`, after setting `item.Ignored`, add:

```go
item.CIStatus = issue.Meta.CIStatus
item.CIURL = issue.Meta.CIURL
```

In `GetIssue`, after setting `detail.Resolve`, add:

```go
if meta, err := h.reports.ReadMetadata(id); err == nil {
    detail.CIStatus = meta.CIStatus
    detail.CIURL = meta.CIURL
}
```

Also in `GetIssue`, replace `h.reports.ReadFix(id)` with `ReadLatestFix` to show the most recent fix:

```go
if fix, _, err := h.reports.ReadLatestFix(id); err == nil {
    detail.Fix = &fix
}
```

Update `TriggerFix` to parse the request body for the iterate flag. Add a struct and update the handler:

```go
func (h *Handlers) TriggerFix(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, loaded := h.running.LoadOrStore(id, true); loaded {
		writeError(w, http.StatusConflict, "action already running for this issue")
		return
	}
	if h.fixFn == nil {
		h.running.Delete(id)
		writeError(w, http.StatusNotImplemented, "fix not configured")
		return
	}

	var req struct {
		Iterate bool `json:"iterate"`
	}
	json.NewDecoder(r.Body).Decode(&req) // ignore parse error; defaults to iterate=false

	pbuf := &progressBuf{}
	h.progressBufs.Store(id, pbuf)
	go func() {
		defer h.running.Delete(id)
		if err := h.fixFn(id, req.Iterate, pbuf); err != nil {
			log.Printf("fix %s failed: %v", id, err)
			h.running.Store(id+"_error", err.Error())
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}
```

- [ ] **Step 5.5: Run tests**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./internal/api/... -v
```

Expected: all PASS.

- [ ] **Step 5.6: Fix compile errors in `cmd/serve.go` (FixFunc signature change)**

The `SetFixFunc` call in `serve.go` will no longer compile due to the signature change. Update the lambda in `cmd/serve.go` to accept `iterate bool`:

```go
handlers.SetFixFunc(func(issueID string, iterate bool, progress io.Writer) error {
    service := ""
    if meta, err := mgr.ReadMetadata(issueID); err == nil {
        service = meta.Service
    }
    if service == "" {
        errorContent, _ := mgr.ReadError(issueID)
        service = extractServiceFromReport(errorContent)
    }
    if iterate {
        return runFixIterate(issueID, service, cfg, mgr, progress)
    }
    return runFix(issueID, service, cfg, mgr, progress)
})
```

- [ ] **Step 5.7: Run all tests**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./...
```

Expected: all PASS.

- [ ] **Step 5.8: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add internal/api/handlers.go internal/api/handlers_test.go cmd/serve.go
git commit -m "feat(api): add CI status fields to responses and iterate support in TriggerFix"
```

---

## Task 6: Frontend API client types

**Files:**
- Modify: `web/src/api/client.ts`

- [ ] **Step 6.1: Update `web/src/api/client.ts`**

Add `ci_status` and `ci_url` fields to both interfaces, and update `triggerFix` to accept an optional `iterate` option:

```typescript
export interface IssueListItem {
  id: string;
  stage: string;
  title: string;
  service: string;
  last_seen: string;
  count: number;
  mr_url: string | null;
  ignored: boolean;
  ci_status: string;
  ci_url: string;
}

export interface IssueDetail {
  id: string;
  stage: string;
  error: string;
  investigation: string | null;
  fix: string | null;
  resolve: ResolveData | null;
  ci_status: string;
  ci_url: string;
}
```

Replace the `triggerFix` function:

```typescript
export async function triggerFix(id: string, iterate = false): Promise<void> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/fix`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ iterate }),
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}
```

- [ ] **Step 6.2: Run frontend verification**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg/web && npm run build 2>&1 | tail -20
```

Expected: builds successfully with no TypeScript errors.

- [ ] **Step 6.3: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add web/src/api/client.ts
git commit -m "feat(web): add CI status fields to API types and iterate option to triggerFix"
```

---

## Task 7: `CIStatusBadge` component

**Files:**
- Create: `web/src/components/CIStatusBadge.tsx`

- [ ] **Step 7.1: Create `web/src/components/CIStatusBadge.tsx`**

```tsx
interface CIStatusBadgeProps {
  status: string;
  url?: string;
}

const STATUS_STYLES: Record<string, string> = {
  passed: 'bg-green-900/40 text-green-400 border-green-800',
  failed: 'bg-red-900/40 text-red-400 border-red-800',
  running: 'bg-yellow-900/40 text-yellow-400 border-yellow-800',
  pending: 'bg-muted text-muted-foreground border-border',
  canceled: 'bg-muted text-muted-foreground border-border',
};

export function CIStatusBadge({ status, url }: CIStatusBadgeProps) {
  if (!status) return <span className="text-muted-foreground text-xs">—</span>;

  const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLES[status] ?? 'bg-muted text-muted-foreground border-border'}`;

  if (url) {
    return (
      <a href={url} target="_blank" rel="noreferrer" className={classes} onClick={(e) => e.stopPropagation()}>
        {status}
      </a>
    );
  }
  return <span className={classes}>{status}</span>;
}
```

- [ ] **Step 7.2: Run frontend build**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg/web && npm run build 2>&1 | tail -10
```

Expected: builds successfully.

- [ ] **Step 7.3: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add web/src/components/CIStatusBadge.tsx
git commit -m "feat(web): add CIStatusBadge component"
```

---

## Task 8: Dashboard — CI badge column

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 8.1: Update `web/src/pages/Dashboard.tsx`**

Add `CIStatusBadge` to the import at the top:

```tsx
import { CIStatusBadge } from '../components/CIStatusBadge';
```

Update the grid column definition (add a CI column between Stage and MR). Replace the header row `grid-cols` class and column header:

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

Update the main row `grid-cols` class to match, and add the CI badge cell after the Stage cell. Replace the full issue row `<div>`:

```tsx
<div
  className="grid grid-cols-[2.5fr_1fr_1fr_0.8fr_0.8fr_60px] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-colors"
  onClick={() => toggleRow(issue.id)}
>
  <span className="font-medium text-sm truncate pr-2">
    {issue.title || issue.id}
    {expandedId === issue.id && (
      <span className="ml-1.5 text-blue-400 text-xs">▾</span>
    )}
  </span>
  <span className="text-xs text-muted-foreground">{issue.service}</span>
  <span>
    <StageIndicator stage={issue.stage} />
  </span>
  <span>
    {issue.mr_url ? (
      <CIStatusBadge status={issue.ci_status} url={issue.ci_url || undefined} />
    ) : (
      <span className="text-muted-foreground text-xs">—</span>
    )}
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
  <span className="text-muted-foreground text-center text-sm">···</span>
</div>
```

- [ ] **Step 8.2: Run frontend verification**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg/web && npm run build 2>&1 | tail -10
```

Expected: builds with no errors.

- [ ] **Step 8.3: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add web/src/pages/Dashboard.tsx
git commit -m "feat(web): add CI status badge column to Dashboard"
```

---

## Task 9: IssueDetail — CI badge and Re-fix button

**Files:**
- Modify: `web/src/pages/IssueDetail.tsx`

- [ ] **Step 9.1: Update `web/src/pages/IssueDetail.tsx`**

Add `CIStatusBadge` import:

```tsx
import { CIStatusBadge } from '../components/CIStatusBadge';
```

Add `triggerFix` is already imported; update its usage by adding a `handleRefix` handler. Add after the existing `handleFix` function:

```tsx
const handleRefix = async () => {
  if (!id) return;
  setErrorMsg(null);
  setFixState('running');
  try {
    await triggerFix(id, true);
    startSSE(() => {
      setFixState('idle');
      fetchIssue();
    });
  } catch (err) {
    setFixState('error');
    setErrorMsg(String(err));
  }
};
```

Update the Resolution section to show CI status and the Re-fix button. Replace the existing `{issue.resolve && ...}` block:

```tsx
{issue.resolve && (
  <Section title="Resolution">
    <div className="p-4 space-y-2 text-sm">
      <p><span className="text-muted-foreground">Branch:</span> <code className="text-xs">{issue.resolve.branch}</code></p>
      <p>
        <span className="text-muted-foreground">MR:</span>{' '}
        <a href={issue.resolve.mr_url} target="_blank" rel="noreferrer" className="text-blue-400 hover:underline">
          {issue.resolve.mr_url}
        </a>
      </p>
      <p><span className="text-muted-foreground">MR Status:</span> {issue.resolve.mr_status}</p>
      {issue.ci_status && (
        <p className="flex items-center gap-2">
          <span className="text-muted-foreground">CI:</span>
          <CIStatusBadge status={issue.ci_status} url={issue.ci_url || undefined} />
        </p>
      )}
      <p><span className="text-muted-foreground">Created:</span> {new Date(issue.resolve.created_at).toLocaleString()}</p>
      {issue.ci_status === 'failed' && fixState !== 'running' && (
        <div className="pt-2">
          <Button size="sm" variant="outline" onClick={handleRefix} className="border-red-800 text-red-400 hover:bg-red-950/30">
            Re-fix (CI failing)
          </Button>
        </div>
      )}
      {fixState === 'running' && progressLog && (
        <pre className="mt-2 p-4 text-xs font-mono text-muted-foreground whitespace-pre-wrap overflow-auto max-h-96">
          {progressLog}
        </pre>
      )}
    </div>
  </Section>
)}
```

- [ ] **Step 9.2: Run frontend verification**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg/web && npm run build 2>&1 | tail -10
```

Expected: builds with no errors.

- [ ] **Step 9.3: Run Playwright verification (requires dev server)**

In one terminal: `cd web && npm run dev`

In another:
```bash
cd /Users/jorgenbs/dev/ruter/claudedawg/web && node verify.mjs
```

Expected: exits 0, no React errors.

- [ ] **Step 9.4: Commit**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && git add web/src/pages/IssueDetail.tsx
git commit -m "feat(web): add CI status badge and Re-fix button to IssueDetail resolution section"
```

---

## Final verification

- [ ] **Run all Go tests**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go test ./...
```

Expected: all PASS.

- [ ] **Run full frontend build**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg/web && npm run build
```

Expected: no errors.

- [ ] **Build the binary**

```bash
cd /Users/jorgenbs/dev/ruter/claudedawg && go build -o fido .
```

Expected: builds successfully.
