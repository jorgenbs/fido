# Investigation Progress, Datadog Links, Badge Contrast — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Three independent improvements: real-time investigation progress via stream-json parsing, fix malformed Datadog links, fix badge contrast in light mode.

**Architecture:** Task 1 adds a best-effort JSON line filter in the agent runner that extracts text from stream-json events (falling back to raw passthrough for non-JSON). Task 2 fixes URL construction with proper encoding. Task 3 adds `dark:` Tailwind prefixes to badge components.

**Tech Stack:** Go (runner, URL building), React/TypeScript + Tailwind (badges)

---

### Task 1: Stream filter writer for investigation progress

**Files:**
- Create: `internal/agent/streamfilter.go`
- Create: `internal/agent/streamfilter_test.go`
- Modify: `internal/agent/runner.go:19-30`
- Modify: `internal/agent/runner_test.go`
- Modify: `config.example.yml:26`

The runner currently pipes raw stdout to both the progress buffer (SSE to frontend) and a result buffer (saved as investigation.md). When the agent command uses `--output-format stream-json`, stdout contains JSON event lines — not clean text. This filter intercepts stdout, tries to parse each line as JSON, extracts text content from `assistant` message events, and writes the clean text to the downstream writers. Non-JSON lines pass through unchanged.

- [ ] **Step 1: Write the failing test for streamFilterWriter**

Create `internal/agent/streamfilter_test.go`:

```go
package agent

import (
	"bytes"
	"testing"
)

func TestStreamFilterWriter_ExtractsAssistantText(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	// Simulate a stream-json assistant message with text content
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello world"}]}}` + "\n"
	w.Write([]byte(line))

	if got := buf.String(); got != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", got)
	}
}

func TestStreamFilterWriter_PassesThroughPlainText(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	w.Write([]byte("plain text output\n"))

	if got := buf.String(); got != "plain text output\n" {
		t.Errorf("expected %q, got %q", "plain text output\n", got)
	}
}

func TestStreamFilterWriter_IgnoresSystemEvents(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	w.Write([]byte(`{"type":"system","subtype":"init","cwd":"/tmp"}` + "\n"))

	if got := buf.String(); got != "" {
		t.Errorf("expected empty output for system event, got %q", got)
	}
}

func TestStreamFilterWriter_HandlesPartialLines(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	// Write in two chunks that split a line
	w.Write([]byte("first part "))
	w.Write([]byte("second part\n"))

	if got := buf.String(); got != "first part second part\n" {
		t.Errorf("expected %q, got %q", "first part second part\n", got)
	}
}

func TestStreamFilterWriter_ExtractsResultText(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	line := `{"type":"result","subtype":"success","result":"Final answer text"}` + "\n"
	w.Write([]byte(line))

	if got := buf.String(); got != "Final answer text" {
		t.Errorf("expected %q, got %q", "Final answer text", got)
	}
}

func TestStreamFilterWriter_MultipleAssistantMessages(t *testing.T) {
	var buf bytes.Buffer
	w := newStreamFilterWriter(&buf)

	w.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Part 1"}]}}` + "\n"))
	w.Write([]byte(`{"type":"system","subtype":"tool_use"}` + "\n"))
	w.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Part 2"}]}}` + "\n"))

	if got := buf.String(); got != "Part 1Part 2" {
		t.Errorf("expected %q, got %q", "Part 1Part 2", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/agent/ -run TestStreamFilter -v`
Expected: FAIL — `newStreamFilterWriter` not defined.

- [ ] **Step 3: Implement streamFilterWriter**

Create `internal/agent/streamfilter.go`:

```go
package agent

import (
	"bytes"
	"encoding/json"
	"io"
)

// streamFilterWriter is a best-effort filter that extracts text from
// Claude CLI stream-json events. Non-JSON lines pass through unchanged.
// This keeps the runner format-agnostic: if the agent command doesn't
// output stream-json, everything passes through as-is.
type streamFilterWriter struct {
	dst     io.Writer
	lineBuf bytes.Buffer
}

func newStreamFilterWriter(dst io.Writer) *streamFilterWriter {
	return &streamFilterWriter{dst: dst}
}

func (w *streamFilterWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.lineBuf.Write(p)

	for {
		line, err := w.lineBuf.ReadBytes('\n')
		if err != nil {
			// No complete line yet — put the partial back
			w.lineBuf.Write(line)
			break
		}
		w.processLine(line[:len(line)-1]) // strip trailing \n
	}
	return n, nil
}

func (w *streamFilterWriter) processLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}

	// Try JSON parse
	var event struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Message *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		Result string `json:"result"`
	}

	if json.Unmarshal(line, &event) != nil {
		// Not JSON — pass through as plain text
		w.dst.Write(line)
		w.dst.Write([]byte("\n"))
		return
	}

	switch event.Type {
	case "assistant":
		if event.Message != nil {
			for _, block := range event.Message.Content {
				if block.Type == "text" && block.Text != "" {
					w.dst.Write([]byte(block.Text))
				}
			}
		}
	case "result":
		if event.Result != "" {
			w.dst.Write([]byte(event.Result))
		}
	// system, tool_use, etc. — silently skip
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/agent/ -run TestStreamFilter -v`
Expected: All 6 tests PASS.

- [ ] **Step 5: Wire the filter into Runner.Run**

Modify `internal/agent/runner.go`. Replace the stdout writer setup (lines 25-30) to wrap the progress writer with the stream filter:

Current code:
```go
var buf bytes.Buffer
writers := []io.Writer{os.Stdout, &buf}
if r.Progress != nil {
    writers = append(writers, r.Progress)
}
cmd.Stdout = io.MultiWriter(writers...)
```

New code:
```go
var buf bytes.Buffer
var filteredBuf bytes.Buffer
filter := newStreamFilterWriter(&filteredBuf)
writers := []io.Writer{os.Stdout, filter}
if r.Progress != nil {
    progressFilter := newStreamFilterWriter(r.Progress)
    writers = append(writers, progressFilter)
}
cmd.Stdout = io.MultiWriter(writers...)
```

Also change the return value (line 77) from `buf.String()` to `filteredBuf.String()`:
```go
log.Printf("[agent] completed (stdout: %d bytes, stderr: %d bytes)", filteredBuf.Len(), stderrBuf.Len())
return filteredBuf.String(), nil
```

Note: `os.Stdout` still gets raw output (for server logs), the filtered buffers get clean text.

- [ ] **Step 6: Update existing runner tests**

The existing `TestRunner_Run` uses `cat` which echoes stdin to stdout as plain text. The filter passes plain text through unchanged, so existing tests should still pass. Verify:

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/agent/ -v`
Expected: All tests PASS (both existing and new).

- [ ] **Step 7: Update config.example.yml**

Change line 26 in `config.example.yml`:

From:
```yaml
  investigate: "claude -p --dangerously-skip-permissions"
```

To:
```yaml
  investigate: "claude -p --dangerously-skip-permissions --output-format stream-json --verbose"
```

- [ ] **Step 8: Commit**

```bash
git add internal/agent/streamfilter.go internal/agent/streamfilter_test.go internal/agent/runner.go config.example.yml
git commit -m "feat: add stream filter for real-time investigation progress"
```

---

### Task 2: Fix Datadog Events/Traces URL construction

**Files:**
- Modify: `cmd/scan.go:215-243`
- Modify: `cmd/scan_test.go`

Two bugs: (1) spaces in the query parameter break markdown link parsing, (2) empty `env` produces `env:&from=` in the URL.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/scan_test.go`:

```go
func TestBuildEventsURL_EncodesQuery(t *testing.T) {
	url := buildEventsURL("myorg", "datadoghq.eu", "my-service", "prod", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
	// Query must not contain unescaped spaces
	if strings.Contains(url, "query=service:my-service env:") {
		t.Error("query parameter contains unescaped space")
	}
	// Must contain encoded query
	if !strings.Contains(url, "query=") {
		t.Error("expected query= in URL")
	}
	if !strings.Contains(url, "my-service") {
		t.Error("expected service name in URL")
	}
	if !strings.Contains(url, "prod") {
		t.Error("expected env in URL")
	}
}

func TestBuildEventsURL_EmptyEnv(t *testing.T) {
	url := buildEventsURL("myorg", "datadoghq.eu", "my-service", "", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
	if strings.Contains(url, "env:") {
		t.Error("URL should omit env: when env is empty")
	}
}

func TestBuildTracesURL_EncodesQuery(t *testing.T) {
	url := buildTracesURL("myorg", "datadoghq.eu", "my-service", "prod", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
	if strings.Contains(url, "query=service:my-service env:") {
		t.Error("query parameter contains unescaped space")
	}
}

func TestBuildTracesURL_EmptyEnv(t *testing.T) {
	url := buildTracesURL("myorg", "datadoghq.eu", "my-service", "", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
	if strings.Contains(url, "env:") {
		t.Error("URL should omit env: when env is empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./cmd/ -run TestBuild -v`
Expected: FAIL — unescaped spaces and env: present with empty env.

- [ ] **Step 3: Fix buildEventsURL and buildTracesURL**

Replace both functions in `cmd/scan.go` (lines 215-243). Add `"net/url"` to the imports if not already present.

```go
func buildEventsURL(org, site, service, env, firstSeen, lastSeen string) string {
	from, err := time.Parse(time.RFC3339, firstSeen)
	if err != nil {
		return ""
	}
	to, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return ""
	}
	query := "service:" + service
	if env != "" {
		query += " env:" + env
	}
	return fmt.Sprintf(
		"https://%s.%s/event/explorer?query=%s&from=%d&to=%d",
		org, site, url.QueryEscape(query), from.UnixMilli(), to.UnixMilli(),
	)
}

func buildTracesURL(org, site, service, env, firstSeen, lastSeen string) string {
	from, err := time.Parse(time.RFC3339, firstSeen)
	if err != nil {
		return ""
	}
	to, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return ""
	}
	query := "service:" + service
	if env != "" {
		query += " env:" + env
	}
	return fmt.Sprintf(
		"https://%s.%s/apm/traces?query=%s&start=%d&end=%d",
		org, site, url.QueryEscape(query), from.UnixMilli(), to.UnixMilli(),
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./cmd/ -run TestBuild -v`
Expected: All 4 new tests PASS.

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./cmd/ -v`
Expected: All existing tests still PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/scan.go cmd/scan_test.go
git commit -m "fix: URL-encode Datadog query params and omit empty env"
```

---

### Task 3: Fix badge contrast in light mode

**Files:**
- Modify: `web/src/components/CIStatusBadge.tsx`
- Modify: `web/src/components/InvestigationBadge.tsx`
- Modify: `web/src/components/StageIndicator.tsx`

All three components hardcode dark-mode Tailwind colors (e.g. `bg-green-900/40 text-green-400`) without `dark:` prefixes. Fix: add light-mode defaults with `dark:` overrides.

- [ ] **Step 1: Update CIStatusBadge.tsx**

Replace the `STATUS_STYLES` object:

```typescript
const STATUS_STYLES: Record<string, string> = {
  passed: 'bg-green-100 text-green-800 border-green-300 dark:bg-green-900/40 dark:text-green-400 dark:border-green-800',
  merged: 'bg-purple-100 text-purple-800 border-purple-300 dark:bg-purple-900/40 dark:text-purple-400 dark:border-purple-800',
  failed: 'bg-red-100 text-red-800 border-red-300 dark:bg-red-900/40 dark:text-red-400 dark:border-red-800',
  running: 'bg-yellow-100 text-yellow-800 border-yellow-300 dark:bg-yellow-900/40 dark:text-yellow-400 dark:border-yellow-800',
  pending: 'bg-muted text-muted-foreground border-border',
  canceled: 'bg-muted text-muted-foreground border-border',
};
```

- [ ] **Step 2: Update InvestigationBadge.tsx**

Replace both `CONFIDENCE_STYLES` and `COMPLEXITY_STYLES`:

```typescript
const CONFIDENCE_STYLES: Record<string, string> = {
  high: 'bg-green-100 text-green-800 border-green-300 dark:bg-green-900/40 dark:text-green-400 dark:border-green-800',
  medium: 'bg-yellow-100 text-yellow-800 border-yellow-300 dark:bg-yellow-900/40 dark:text-yellow-400 dark:border-yellow-800',
  low: 'bg-red-100 text-red-800 border-red-300 dark:bg-red-900/40 dark:text-red-400 dark:border-red-800',
};

const COMPLEXITY_STYLES: Record<string, string> = {
  simple: 'bg-green-100 text-green-800 border-green-300 dark:bg-green-900/40 dark:text-green-400 dark:border-green-800',
  moderate: 'bg-yellow-100 text-yellow-800 border-yellow-300 dark:bg-yellow-900/40 dark:text-yellow-400 dark:border-yellow-800',
  complex: 'bg-red-100 text-red-800 border-red-300 dark:bg-red-900/40 dark:text-red-400 dark:border-red-800',
};
```

- [ ] **Step 3: Update StageIndicator.tsx**

Replace the `stageColors` object:

```typescript
const stageColors: Record<string, string> = {
  scanned: 'bg-indigo-100 text-indigo-800 border-indigo-300 dark:bg-indigo-950 dark:text-indigo-300 dark:border-indigo-800',
  investigated: 'bg-amber-100 text-amber-800 border-amber-300 dark:bg-amber-950 dark:text-amber-300 dark:border-amber-800',
  fixed: 'bg-emerald-100 text-emerald-800 border-emerald-300 dark:bg-emerald-950 dark:text-emerald-300 dark:border-emerald-800',
};
```

Also update the fallback in `StageIndicator`:

```typescript
const colorClass = stageColors[stage] ?? 'bg-slate-100 text-slate-600 border-slate-300 dark:bg-slate-800 dark:text-slate-400 dark:border-slate-700';
```

- [ ] **Step 4: Verify with Playwright**

Run: `cd /Users/jorgenbs/dev/ruter/fido/web && npm run dev` (in background)
Run: `cd /Users/jorgenbs/dev/ruter/fido/web && node verify.mjs`
Expected: No React errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/CIStatusBadge.tsx web/src/components/InvestigationBadge.tsx web/src/components/StageIndicator.tsx
git commit -m "fix: badge contrast in light mode with dark: prefix overrides"
```
