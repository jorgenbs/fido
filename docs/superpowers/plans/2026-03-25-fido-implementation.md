# Fido Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Fido — a CLI tool that fetches Datadog errors, investigates them via AI agents, and proposes fixes as draft GitLab MRs.

**Architecture:** Go CLI with subcommands (scan, investigate, fix, list, show, daemon, serve). Markdown reports on filesystem as pipeline artifacts. HTTP API for web UI consumption. TypeScript web dashboard. Docker Compose for local deployment.

**Tech Stack:** Go (cobra CLI, chi router, datadog-api-client-go v2), TypeScript (Vite + React), Docker Compose

**Spec:** `docs/superpowers/specs/2026-03-25-fido-design.md`

---

## File Structure

```
fido/
├── cmd/
│   ├── root.go              # Cobra root command + global flags
│   ├── scan.go              # fido scan command
│   ├── investigate.go       # fido investigate command
│   ├── fix.go               # fido fix command
│   ├── list.go              # fido list command
│   ├── show.go              # fido show command
│   ├── daemon.go            # fido daemon command
│   └── serve.go             # fido serve command
├── internal/
│   ├── config/
│   │   └── config.go        # Config loading and validation
│   ├── datadog/
│   │   └── client.go        # Datadog API wrapper (Error Tracking + Logs)
│   ├── reports/
│   │   └── manager.go       # Filesystem report CRUD + stage derivation
│   ├── agent/
│   │   └── runner.go        # Agent invocation (subprocess + prompt assembly)
│   └── api/
│       ├── server.go        # HTTP API server setup
│       └── handlers.go      # API route handlers
├── templates/
│   ├── investigate.md.tmpl  # Prompt template for investigate stage
│   └── fix.md.tmpl          # Prompt template for fix stage
├── web/                     # TypeScript web UI (separate package)
│   ├── package.json
│   ├── vite.config.ts
│   ├── index.html
│   └── src/
│       ├── main.tsx
│       ├── App.tsx
│       ├── api/
│       │   └── client.ts    # HTTP client for Fido API
│       ├── pages/
│       │   ├── Dashboard.tsx
│       │   ├── IssueDetail.tsx
│       │   └── Settings.tsx
│       └── components/
│           ├── IssueList.tsx
│           ├── StageIndicator.tsx
│           └── MarkdownViewer.tsx
├── main.go                  # Entry point
├── go.mod
├── go.sum
├── docker-compose.yml
├── Dockerfile               # Multi-stage Go build
└── web/Dockerfile           # Web UI build
```

---

### Task 1: Go Project Scaffolding + Config

**Files:**
- Create: `go.mod`, `main.go`, `cmd/root.go`, `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Initialize Go module**

Run: `go mod init github.com/ruter-as/fido`

- [ ] **Step 2: Install dependencies**

Run: `go get github.com/spf13/cobra github.com/spf13/viper gopkg.in/yaml.v3`

- [ ] **Step 3: Write config test**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  api_key: "test-api-key"
  app_key: "test-app-key"
  site: "datadoghq.eu"
  services:
    - "svc-a"
    - "svc-b"

scan:
  interval: "15m"
  since: "24h"

repositories:
  svc-a:
    local: "/path/to/svc-a"
  svc-b:
    git: "https://gitlab.com/org/svc-b.git"

agent:
  investigate: "claude -p"
  fix: "claude -p"
`
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Datadog.APIKey != "test-api-key" {
		t.Errorf("expected api_key 'test-api-key', got %q", cfg.Datadog.APIKey)
	}
	if len(cfg.Datadog.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(cfg.Datadog.Services))
	}
	if cfg.Repositories["svc-a"].Local != "/path/to/svc-a" {
		t.Errorf("expected local path, got %q", cfg.Repositories["svc-a"].Local)
	}
	if cfg.Repositories["svc-b"].Git != "https://gitlab.com/org/svc-b.git" {
		t.Errorf("expected git url, got %q", cfg.Repositories["svc-b"].Git)
	}
	if cfg.Agent.Investigate != "claude -p" {
		t.Errorf("expected agent investigate command, got %q", cfg.Agent.Investigate)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yml")
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestLoadConfig_DefaultValues(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  api_key: "key"
  app_key: "app"
`
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Scan.Interval != "15m" {
		t.Errorf("expected default interval '15m', got %q", cfg.Scan.Interval)
	}
	if cfg.Scan.Since != "24h" {
		t.Errorf("expected default since '24h', got %q", cfg.Scan.Since)
	}
	if cfg.Datadog.Site != "datadoghq.eu" {
		t.Errorf("expected default site 'datadoghq.eu', got %q", cfg.Datadog.Site)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `Load` function not defined

- [ ] **Step 5: Implement config**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Datadog      DatadogConfig            `yaml:"datadog"`
	Scan         ScanConfig               `yaml:"scan"`
	Repositories map[string]RepoConfig    `yaml:"repositories"`
	Agent        AgentConfig              `yaml:"agent"`
}

type DatadogConfig struct {
	APIKey   string   `yaml:"api_key"`
	AppKey   string   `yaml:"app_key"`
	Site     string   `yaml:"site"`
	Services []string `yaml:"services"`
}

type ScanConfig struct {
	Interval string `yaml:"interval"`
	Since    string `yaml:"since"`
}

type RepoConfig struct {
	Local string `yaml:"local"`
	Git   string `yaml:"git"`
}

type AgentConfig struct {
	Investigate string `yaml:"investigate"`
	Fix         string `yaml:"fix"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		Datadog: DatadogConfig{
			Site: "datadoghq.eu",
		},
		Scan: ScanConfig{
			Interval: "15m",
			Since:    "24h",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 7: Create root command and main.go**

```go
// cmd/root.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruter-as/fido/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "fido",
	Short: "Fetch errors from Datadog, investigate, and propose fixes",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cfgFile == "" {
			home, _ := os.UserHomeDir()
			cfgFile = filepath.Join(home, ".fido", "config.yml")
		}
		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.fido/config.yml)")
}
```

```go
// main.go
package main

import "github.com/ruter-as/fido/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 8: Verify it builds**

Run: `go build -o fido .`
Expected: Binary builds successfully

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum main.go cmd/root.go internal/config/
git commit -m "feat(fido): add project scaffolding and config loading"
```

---

### Task 2: Report Manager

**Files:**
- Create: `internal/reports/manager.go`
- Test: `internal/reports/manager_test.go`

- [ ] **Step 1: Write report manager tests**

```go
// internal/reports/manager_test.go
package reports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestManager_WriteAndReadError(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	content := "# Error Report\nNullPointerException in handler"
	err := m.WriteError("issue-123", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := m.ReadError("issue-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("content mismatch: got %q", got)
	}
}

func TestManager_Stage(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	// No files = unknown
	stage := m.Stage("issue-123")
	if stage != StageUnknown {
		t.Errorf("expected unknown, got %s", stage)
	}

	// Only error.md = scanned
	m.WriteError("issue-123", "error")
	stage = m.Stage("issue-123")
	if stage != StageScanned {
		t.Errorf("expected scanned, got %s", stage)
	}

	// + investigation.md = investigated
	m.WriteInvestigation("issue-123", "investigation")
	stage = m.Stage("issue-123")
	if stage != StageInvestigated {
		t.Errorf("expected investigated, got %s", stage)
	}

	// + fix.md + resolve.json = fixed
	m.WriteFix("issue-123", "fix")
	resolve := &ResolveData{
		Branch:         "fix/issue-123-null-pointer",
		MRURL:          "https://gitlab.com/org/repo/-/merge_requests/1",
		MRStatus:       "draft",
		Service:        "svc-a",
		DatadogIssueID: "issue-123",
		DatadogURL:     "https://app.datadoghq.eu/...",
	}
	m.WriteResolve("issue-123", resolve)
	stage = m.Stage("issue-123")
	if stage != StageFixed {
		t.Errorf("expected fixed, got %s", stage)
	}
}

func TestManager_ListIssues(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	m.WriteError("issue-1", "error 1")
	m.WriteError("issue-2", "error 2")
	m.WriteInvestigation("issue-2", "investigation 2")

	issues, err := m.ListIssues()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestManager_Exists(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	if m.Exists("issue-123") {
		t.Error("expected issue to not exist")
	}

	m.WriteError("issue-123", "error")
	if !m.Exists("issue-123") {
		t.Error("expected issue to exist")
	}
}

func TestManager_ReadResolve(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	resolve := &ResolveData{
		Branch:         "fix/issue-123-test",
		MRURL:          "https://gitlab.com/merge/1",
		MRStatus:       "draft",
		Service:        "svc-a",
		DatadogIssueID: "issue-123",
	}
	m.WriteResolve("issue-123", resolve)

	got, err := m.ReadResolve("issue-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Branch != resolve.Branch {
		t.Errorf("branch mismatch: got %q", got.Branch)
	}
	if got.MRURL != resolve.MRURL {
		t.Errorf("mr_url mismatch: got %q", got.MRURL)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/reports/ -v`
Expected: FAIL — types and functions not defined

- [ ] **Step 3: Implement report manager**

```go
// internal/reports/manager.go
package reports

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Stage string

const (
	StageUnknown      Stage = "unknown"
	StageScanned      Stage = "scanned"
	StageInvestigated Stage = "investigated"
	StageFixed        Stage = "fixed"
)

type ResolveData struct {
	Branch         string `json:"branch"`
	MRURL          string `json:"mr_url"`
	MRStatus       string `json:"mr_status"`
	Service        string `json:"service"`
	DatadogIssueID string `json:"datadog_issue_id"`
	DatadogURL     string `json:"datadog_url"`
	CreatedAt      string `json:"created_at"`
}

type IssueSummary struct {
	ID    string
	Stage Stage
}

type Manager struct {
	baseDir string
}

func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
}

func (m *Manager) issueDir(issueID string) string {
	return filepath.Join(m.baseDir, issueID)
}

func (m *Manager) writeFile(issueID, filename, content string) error {
	dir := m.issueDir(issueID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating issue dir: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644)
}

func (m *Manager) readFile(issueID, filename string) (string, error) {
	data, err := os.ReadFile(filepath.Join(m.issueDir(issueID), filename))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *Manager) fileExists(issueID, filename string) bool {
	_, err := os.Stat(filepath.Join(m.issueDir(issueID), filename))
	return err == nil
}

func (m *Manager) WriteError(issueID, content string) error {
	return m.writeFile(issueID, "error.md", content)
}

func (m *Manager) ReadError(issueID string) (string, error) {
	return m.readFile(issueID, "error.md")
}

func (m *Manager) WriteInvestigation(issueID, content string) error {
	return m.writeFile(issueID, "investigation.md", content)
}

func (m *Manager) ReadInvestigation(issueID string) (string, error) {
	return m.readFile(issueID, "investigation.md")
}

func (m *Manager) WriteFix(issueID, content string) error {
	return m.writeFile(issueID, "fix.md", content)
}

func (m *Manager) ReadFix(issueID string) (string, error) {
	return m.readFile(issueID, "fix.md")
}

func (m *Manager) WriteResolve(issueID string, data *ResolveData) error {
	if data.CreatedAt == "" {
		data.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling resolve data: %w", err)
	}
	return m.writeFile(issueID, "resolve.json", string(b))
}

func (m *Manager) ReadResolve(issueID string) (*ResolveData, error) {
	content, err := m.readFile(issueID, "resolve.json")
	if err != nil {
		return nil, err
	}
	var data ResolveData
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, fmt.Errorf("parsing resolve.json: %w", err)
	}
	return &data, nil
}

func (m *Manager) Stage(issueID string) Stage {
	if !m.fileExists(issueID, "error.md") {
		return StageUnknown
	}
	if m.fileExists(issueID, "resolve.json") && m.fileExists(issueID, "fix.md") {
		return StageFixed
	}
	if m.fileExists(issueID, "investigation.md") {
		return StageInvestigated
	}
	return StageScanned
}

func (m *Manager) Exists(issueID string) bool {
	return m.fileExists(issueID, "error.md")
}

func (m *Manager) ListIssues() ([]IssueSummary, error) {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var issues []IssueSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if m.fileExists(id, "error.md") {
			issues = append(issues, IssueSummary{
				ID:    id,
				Stage: m.Stage(id),
			})
		}
	}
	return issues, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/reports/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/reports/
git commit -m "feat(fido): add filesystem report manager with stage derivation"
```

---

### Task 3: Datadog Client Wrapper

**Files:**
- Create: `internal/datadog/client.go`
- Test: `internal/datadog/client_test.go`

- [ ] **Step 1: Install Datadog SDK**

Run: `go get github.com/DataDog/datadog-api-client-go/v2`

- [ ] **Step 2: Write Datadog client tests**

Tests use a mock HTTP server to simulate Datadog API responses. This avoids needing real credentials for unit tests.

```go
// internal/datadog/client_test.go
package datadog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_SearchErrorIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/error-tracking/issues" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("DD-API-KEY") != "test-key" {
			t.Error("missing API key header")
		}

		resp := SearchIssuesResponse{
			Data: []ErrorIssue{
				{
					ID: "issue-1",
					Attributes: ErrorIssueAttributes{
						Title:      "NullPointerException",
						Message:    "null pointer in handleRequest",
						Service:    "svc-a",
						Env:        "prod",
						FirstSeen:  "2026-03-25T08:00:00Z",
						LastSeen:   "2026-03-25T09:00:00Z",
						Count:      42,
						Status:     "open",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", "test-app-key", server.URL)
	issues, err := client.SearchErrorIssues([]string{"svc-a"}, "24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].ID != "issue-1" {
		t.Errorf("expected issue-1, got %s", issues[0].ID)
	}
	if issues[0].Attributes.Count != 42 {
		t.Errorf("expected count 42, got %d", issues[0].Attributes.Count)
	}
}

func TestClient_SearchLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := SearchLogsResponse{
			Data: []LogEntry{
				{
					Attributes: LogAttributes{
						Message:   "Processing request for user 123",
						Timestamp: "2026-03-25T08:59:50Z",
						Service:   "svc-a",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", "test-app-key", server.URL)
	logs, err := client.SearchLogs("trace_id:abc123", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/datadog/ -v`
Expected: FAIL — types not defined

- [ ] **Step 4: Implement Datadog client**

Note: We use a custom HTTP client instead of the Datadog SDK directly. This makes testing simpler (mock HTTP server) and avoids tight coupling to the SDK's generated types. The SDK's generated client configures its base URL via environment variables and context, making it awkward to point at a test server. A thin HTTP wrapper gives us the same API coverage with better testability.

```go
// internal/datadog/client.go
package datadog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	apiKey  string
	appKey  string
	baseURL string
	http    *http.Client
}

type ErrorIssue struct {
	ID         string               `json:"id"`
	Attributes ErrorIssueAttributes `json:"attributes"`
}

type ErrorIssueAttributes struct {
	Title     string `json:"title"`
	Message   string `json:"message"`
	Service   string `json:"service"`
	Env       string `json:"env"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Count     int64  `json:"count"`
	Status    string `json:"status"`
	StackTrace string `json:"stack_trace,omitempty"`
}

type SearchIssuesResponse struct {
	Data []ErrorIssue `json:"data"`
}

type LogEntry struct {
	Attributes LogAttributes `json:"attributes"`
}

type LogAttributes struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Service   string `json:"service"`
	Status    string `json:"status"`
}

type SearchLogsResponse struct {
	Data []LogEntry `json:"data"`
}

func NewClient(apiKey, appKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		appKey:  appKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, query url.Values) ([]byte, error) {
	u := fmt.Sprintf("%s%s", c.baseURL, path)
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("DD-API-KEY", c.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", c.appKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *Client) SearchErrorIssues(services []string, since string) ([]ErrorIssue, error) {
	query := url.Values{}
	if len(services) > 0 {
		query.Set("filter[services]", strings.Join(services, ","))
	}
	query.Set("filter[since]", since)

	body, err := c.do("GET", "/api/v2/error-tracking/issues", query)
	if err != nil {
		return nil, err
	}

	var resp SearchIssuesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return resp.Data, nil
}

func (c *Client) SearchLogs(queryStr, since string) ([]LogEntry, error) {
	query := url.Values{}
	query.Set("filter[query]", queryStr)
	query.Set("filter[from]", since)

	body, err := c.do("GET", "/api/v2/logs/events", query)
	if err != nil {
		return nil, err
	}

	var resp SearchLogsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return resp.Data, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/datadog/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/datadog/
git commit -m "feat(fido): add Datadog API client for error tracking and logs"
```

---

### Task 4: Scan Command

**Files:**
- Create: `cmd/scan.go`, `templates/error.md.tmpl`
- Test: `cmd/scan_test.go`

- [ ] **Step 1: Create error report template**

```
// templates/error.md.tmpl
# Error Report: {{.Title}}

**Issue ID:** {{.ID}}
**Service:** {{.Service}}
**Environment:** {{.Env}}
**Status:** {{.Status}}

## Occurrences

- **Count:** {{.Count}}
- **First seen:** {{.FirstSeen}}
- **Last seen:** {{.LastSeen}}

## Error

**Type:** {{.Title}}
**Message:** {{.Message}}

## Stack Trace

{{if .StackTrace}}
```
{{.StackTrace}}
```
{{else}}
_No stack trace available_
{{end}}

## Surrounding Logs

{{if .Logs}}
{{range .Logs}}
- `{{.Timestamp}}` [{{.Status}}] {{.Message}}
{{end}}
{{else}}
_No surrounding logs found_
{{end}}

## Links

- [Datadog Issue]({{.DatadogURL}})
```

- [ ] **Step 2: Write scan command test**

```go
// cmd/scan_test.go
package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
)

func TestScanCommand_CreatesErrorReports(t *testing.T) {
	// Mock Datadog API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "error-tracking"):
			resp := datadog.SearchIssuesResponse{
				Data: []datadog.ErrorIssue{
					{
						ID: "issue-1",
						Attributes: datadog.ErrorIssueAttributes{
							Title:   "NullPointerException",
							Message: "null in handler",
							Service: "svc-a",
							Env:     "prod",
							Count:   10,
							Status:  "open",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case strings.Contains(r.URL.Path, "logs"):
			resp := datadog.SearchLogsResponse{Data: []datadog.LogEntry{}}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := datadog.NewClient("key", "app", server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services: []string{"svc-a"},
			Site:     server.URL,
		},
		Scan: config.ScanConfig{Since: "24h"},
	}

	count, err := runScan(cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 new issue, got %d", count)
	}

	// Verify report was created
	if !mgr.Exists("issue-1") {
		t.Error("expected issue-1 report to exist")
	}
	content, _ := mgr.ReadError("issue-1")
	if !strings.Contains(content, "NullPointerException") {
		t.Error("expected error report to contain error title")
	}
}

func TestScanCommand_SkipsExistingIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := datadog.SearchIssuesResponse{
			Data: []datadog.ErrorIssue{
				{ID: "issue-1", Attributes: datadog.ErrorIssueAttributes{Title: "Err", Service: "svc-a"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := datadog.NewClient("key", "app", server.URL)

	// Pre-create the issue
	mgr.WriteError("issue-1", "existing report")

	cfg := &config.Config{
		Datadog: config.DatadogConfig{Services: []string{"svc-a"}},
		Scan:    config.ScanConfig{Since: "24h"},
	}

	count, err := runScan(cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 new issues (existing skipped), got %d", count)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./cmd/ -v -run TestScan`
Expected: FAIL — `runScan` not defined

- [ ] **Step 4: Implement scan command**

```go
// cmd/scan.go
package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan Datadog for new error issues",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		ddClient := datadog.NewClient(
			cfg.Datadog.APIKey,
			cfg.Datadog.AppKey,
			fmt.Sprintf("https://api.%s", cfg.Datadog.Site),
		)

		services, _ := cmd.Flags().GetStringSlice("service")
		if len(services) == 0 {
			services = cfg.Datadog.Services
		}

		since, _ := cmd.Flags().GetString("since")
		if since == "" {
			since = cfg.Scan.Since
		}

		scanCfg := &config.Config{
			Datadog: config.DatadogConfig{Services: services, Site: cfg.Datadog.Site},
			Scan:    config.ScanConfig{Since: since},
		}

		count, err := runScan(scanCfg, ddClient, mgr)
		if err != nil {
			return err
		}
		fmt.Printf("Found %d new error issues\n", count)
		return nil
	},
}

type errorReportData struct {
	ID         string
	Title      string
	Message    string
	Service    string
	Env        string
	FirstSeen  string
	LastSeen   string
	Count      int64
	Status     string
	StackTrace string
	Logs       []datadog.LogAttributes
	DatadogURL string
}

func runScan(cfg *config.Config, ddClient *datadog.Client, mgr *reports.Manager) (int, error) {
	issues, err := ddClient.SearchErrorIssues(cfg.Datadog.Services, cfg.Scan.Since)
	if err != nil {
		return 0, fmt.Errorf("searching error issues: %w", err)
	}

	tmpl, err := loadErrorTemplate()
	if err != nil {
		return 0, fmt.Errorf("loading template: %w", err)
	}

	count := 0
	for _, issue := range issues {
		if mgr.Exists(issue.ID) {
			continue
		}

		data := errorReportData{
			ID:         issue.ID,
			Title:      issue.Attributes.Title,
			Message:    issue.Attributes.Message,
			Service:    issue.Attributes.Service,
			Env:        issue.Attributes.Env,
			FirstSeen:  issue.Attributes.FirstSeen,
			LastSeen:   issue.Attributes.LastSeen,
			Count:      issue.Attributes.Count,
			Status:     issue.Attributes.Status,
			StackTrace: issue.Attributes.StackTrace,
			DatadogURL: fmt.Sprintf("https://app.%s/error-tracking/issue/%s", cfg.Datadog.Site, issue.ID),
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return count, fmt.Errorf("rendering error report: %w", err)
		}

		if err := mgr.WriteError(issue.ID, buf.String()); err != nil {
			return count, fmt.Errorf("writing error report: %w", err)
		}
		count++
	}

	return count, nil
}

func loadErrorTemplate() (*template.Template, error) {
	// Embedded template as fallback
	const defaultTemplate = `# Error Report: {{.Title}}

**Issue ID:** {{.ID}}
**Service:** {{.Service}}
**Environment:** {{.Env}}
**Status:** {{.Status}}

## Occurrences

- **Count:** {{.Count}}
- **First seen:** {{.FirstSeen}}
- **Last seen:** {{.LastSeen}}

## Error

**Type:** {{.Title}}
**Message:** {{.Message}}

## Stack Trace

{{if .StackTrace}}` + "```" + `
{{.StackTrace}}
` + "```" + `
{{else}}
_No stack trace available_
{{end}}

## Surrounding Logs

{{if .Logs}}
{{range .Logs}}
- ` + "`{{.Timestamp}}`" + ` [{{.Status}}] {{.Message}}
{{end}}
{{else}}
_No surrounding logs found_
{{end}}

## Links

- [Datadog Issue]({{.DatadogURL}})
`
	return template.New("error").Parse(defaultTemplate)
}

func init() {
	scanCmd.Flags().StringSlice("service", nil, "filter by service name(s)")
	scanCmd.Flags().String("since", "", "how far back to look (default: config value)")
	scanCmd.Flags().Int("limit", 0, "max number of issues to fetch")
	rootCmd.AddCommand(scanCmd)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/ -v -run TestScan`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/scan.go cmd/scan_test.go templates/
git commit -m "feat(fido): add scan command with error report generation"
```

---

### Task 5: Agent Runner

**Files:**
- Create: `internal/agent/runner.go`
- Test: `internal/agent/runner_test.go`

- [ ] **Step 1: Write agent runner tests**

```go
// internal/agent/runner_test.go
package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunner_BuildPromptFile(t *testing.T) {
	r := &Runner{}

	content := "# Error\nSomething broke"
	path, err := r.WritePromptFile("issue-123", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading prompt file: %v", err)
	}
	if !strings.Contains(string(data), "Something broke") {
		t.Error("prompt file should contain the content")
	}
}

func TestRunner_Run(t *testing.T) {
	repoDir := t.TempDir()

	r := &Runner{
		Command: echoCommand(),
	}

	output, err := r.Run("Hello from prompt", repoDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "Hello from prompt") {
		t.Errorf("expected output to contain prompt content, got: %s", output)
	}
}

func TestRunner_Run_CommandFails(t *testing.T) {
	r := &Runner{
		Command: "false",
	}

	_, err := r.Run("prompt", t.TempDir())
	if err == nil {
		t.Error("expected error for failing command")
	}
}

// echoCommand returns a command that cats the prompt file to stdout.
// This simulates an agent that reads the prompt and writes output.
func echoCommand() string {
	if runtime.GOOS == "windows" {
		return "type"
	}
	return "cat"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -v`
Expected: FAIL — `Runner` not defined

- [ ] **Step 3: Implement agent runner**

```go
// internal/agent/runner.go
package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Runner struct {
	Command string // Command prefix, e.g. "claude -p"
}

func (r *Runner) WritePromptFile(issueID, content string) (string, error) {
	tmpDir := os.TempDir()
	path := filepath.Join(tmpDir, fmt.Sprintf("fido-prompt-%s.md", issueID))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing prompt file: %w", err)
	}
	return path, nil
}

func (r *Runner) Run(promptContent, repoDir string) (string, error) {
	promptFile, err := r.WritePromptFile("tmp", promptContent)
	if err != nil {
		return "", err
	}
	defer os.Remove(promptFile)

	parts := strings.Fields(r.Command)
	parts = append(parts, promptFile)

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("agent failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return "", fmt.Errorf("running agent: %w", err)
	}

	return string(output), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/
git commit -m "feat(fido): add agent runner for subprocess invocation"
```

---

### Task 6: Investigate Command

**Files:**
- Create: `cmd/investigate.go`, `templates/investigate.md.tmpl`
- Test: `cmd/investigate_test.go`

- [ ] **Step 1: Write investigate command test**

```go
// cmd/investigate_test.go
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruter-as/fido/internal/agent"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

func TestInvestigate_ProducesInvestigationReport(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	// Pre-create error report
	mgr.WriteError("issue-1", "# Error\nNullPointerException in handler")

	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: repoDir},
		},
		Agent: config.AgentConfig{
			Investigate: "cat", // cat will just echo the prompt file
		},
	}

	err := runInvestigate("issue-1", "svc-a", cfg, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := mgr.ReadInvestigation("issue-1")
	if err != nil {
		t.Fatalf("reading investigation: %v", err)
	}
	if !strings.Contains(content, "NullPointerException") {
		t.Error("investigation should contain error context from prompt")
	}
}

func TestInvestigate_FailsWithoutErrorReport(t *testing.T) {
	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	cfg := &config.Config{}
	err := runInvestigate("issue-1", "svc-a", cfg, mgr)
	if err == nil {
		t.Error("expected error when no error.md exists")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -v -run TestInvestigate`
Expected: FAIL — `runInvestigate` not defined

- [ ] **Step 3: Implement investigate command**

```go
// cmd/investigate.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ruter-as/fido/internal/agent"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var investigateCmd = &cobra.Command{
	Use:   "investigate <issue-id>",
	Short: "Investigate an error issue using an AI agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		agentCmd, _ := cmd.Flags().GetString("agent")
		if agentCmd != "" {
			cfg.Agent.Investigate = agentCmd
		}

		service, _ := cmd.Flags().GetString("service")
		if service == "" {
			// Try to extract service from error.md metadata
			errorContent, err := mgr.ReadError(issueID)
			if err != nil {
				return fmt.Errorf("no error report found for %s: %w", issueID, err)
			}
			service = extractServiceFromReport(errorContent)
			if service == "" {
				return fmt.Errorf("could not determine service — use --service flag")
			}
		}
		return runInvestigate(issueID, service, cfg, mgr)
	},
}

const investigatePromptTemplate = `You are investigating a production error. Analyze the error below, look through the codebase, and produce a root cause analysis.

## Error Report

%s

## Instructions

1. Analyze the error and stack trace
2. Find the relevant code in the repository
3. Identify the root cause
4. List all affected files and code paths
5. Suggest a fix approach
6. Estimate confidence and complexity

## Output Format

Write your analysis as markdown with these sections:
- **Root Cause**: What is causing this error
- **Affected Files**: List of files involved
- **Suggested Fix**: How to fix this
- **Confidence**: High/Medium/Low
- **Complexity**: Simple/Moderate/Complex
`

func runInvestigate(issueID, service string, cfg *config.Config, mgr *reports.Manager) error {
	errorContent, err := mgr.ReadError(issueID)
	if err != nil {
		return fmt.Errorf("no error report for issue %s: %w", issueID, err)
	}

	// Resolve repo path
	repoPath, err := resolveRepoPath(service, cfg)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf(investigatePromptTemplate, errorContent)

	runner := &agent.Runner{Command: cfg.Agent.Investigate}
	output, err := runner.Run(prompt, repoPath)
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	if err := mgr.WriteInvestigation(issueID, output); err != nil {
		return fmt.Errorf("writing investigation report: %w", err)
	}

	fmt.Printf("Investigation complete for %s\n", issueID)
	return nil
}

func resolveRepoPath(service string, cfg *config.Config) (string, error) {
	repo, ok := cfg.Repositories[service]
	if !ok {
		return "", fmt.Errorf("no repository configured for service %q", service)
	}

	if repo.Local != "" {
		return repo.Local, nil
	}

	if repo.Git != "" {
		tmpDir, err := os.MkdirTemp("", "fido-repo-*")
		if err != nil {
			return "", err
		}
		cmd := exec.Command("git", "clone", "--depth", "1", repo.Git, tmpDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("git clone failed: %s: %w", string(output), err)
		}
		return tmpDir, nil
	}

	return "", fmt.Errorf("repository %q has no local or git path configured", service)
}

// extractServiceFromReport parses the **Service:** field from an error.md report.
func extractServiceFromReport(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "**Service:**") {
			return strings.TrimSpace(strings.TrimPrefix(line, "**Service:**"))
		}
	}
	return ""
}

func init() {
	investigateCmd.Flags().String("agent", "", "override agent command")
	investigateCmd.Flags().String("service", "", "service name (auto-detected from error report if omitted)")
	rootCmd.AddCommand(investigateCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -v -run TestInvestigate`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/investigate.go cmd/investigate_test.go
git commit -m "feat(fido): add investigate command with agent invocation"
```

---

### Task 7: Fix Command

**Files:**
- Create: `cmd/fix.go`
- Test: `cmd/fix_test.go`

- [ ] **Step 1: Write fix command test**

```go
// cmd/fix_test.go
package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

func TestFix_ProducesFixReportAndResolve(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	mgr.WriteError("issue-1", "# Error\nNullPointerException")
	mgr.WriteInvestigation("issue-1", "# Investigation\nRoot cause: missing null check")

	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: repoDir},
		},
		Agent: config.AgentConfig{
			Fix: "cat", // echoes the prompt
		},
	}

	err := runFix("issue-1", "svc-a", cfg, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fix, err := mgr.ReadFix("issue-1")
	if err != nil {
		t.Fatalf("reading fix: %v", err)
	}
	if !strings.Contains(fix, "NullPointerException") {
		t.Error("fix report should contain error context")
	}

	stage := mgr.Stage("issue-1")
	if stage != reports.StageFixed {
		t.Errorf("expected stage fixed, got %s", stage)
	}
}

func TestFix_FailsWithoutInvestigation(t *testing.T) {
	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	mgr.WriteError("issue-1", "error")

	cfg := &config.Config{}
	err := runFix("issue-1", "svc-a", cfg, mgr)
	if err == nil {
		t.Error("expected error when no investigation.md exists")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -v -run TestFix`
Expected: FAIL — `runFix` not defined

- [ ] **Step 3: Implement fix command**

```go
// cmd/fix.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruter-as/fido/internal/agent"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var fixCmd = &cobra.Command{
	Use:   "fix <issue-id>",
	Short: "Fix an investigated issue using an AI agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		agentCmd, _ := cmd.Flags().GetString("agent")
		if agentCmd != "" {
			cfg.Agent.Fix = agentCmd
		}

		service, _ := cmd.Flags().GetString("service")
		if service == "" {
			errorContent, err := mgr.ReadError(issueID)
			if err != nil {
				return fmt.Errorf("no error report found for %s: %w", issueID, err)
			}
			service = extractServiceFromReport(errorContent)
			if service == "" {
				return fmt.Errorf("could not determine service — use --service flag")
			}
		}
		return runFix(issueID, service, cfg, mgr)
	},
}

const fixPromptTemplate = `You are fixing a production error. Use the error report and investigation below to implement a fix.

## Error Report

%s

## Investigation

%s

## Instructions

1. Create a new branch: fix/%s-<short-description>
2. Implement the fix described in the investigation
3. Commit with conventional commit message: fix(<service>): <description>
4. Push the branch
5. Create a draft MR using glab:
   - Title: fix(<service>): <short description>
   - Description: Include investigation summary and Datadog link
   - Draft: yes

## Output Format

Write a summary of what you did:
- **Summary**: What was changed and why
- **Files Changed**: List of modified files
- **Branch**: The branch name
- **MR URL**: The merge request URL (if created)
- **Tests**: Any test results
`

func runFix(issueID, service string, cfg *config.Config, mgr *reports.Manager) error {
	errorContent, err := mgr.ReadError(issueID)
	if err != nil {
		return fmt.Errorf("no error report for issue %s: %w", issueID, err)
	}

	investigationContent, err := mgr.ReadInvestigation(issueID)
	if err != nil {
		return fmt.Errorf("no investigation report for issue %s — run 'fido investigate %s' first: %w", issueID, issueID, err)
	}

	repoPath, err := resolveRepoPath(service, cfg)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf(fixPromptTemplate, errorContent, investigationContent, issueID)

	runner := &agent.Runner{Command: cfg.Agent.Fix}
	output, err := runner.Run(prompt, repoPath)
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	if err := mgr.WriteFix(issueID, output); err != nil {
		return fmt.Errorf("writing fix report: %w", err)
	}

	// Parse branch and MR URL from agent output using known patterns
	resolve := &reports.ResolveData{
		Branch:         parseField(output, "Branch"),
		MRURL:          parseField(output, "MR URL"),
		MRStatus:       "draft",
		Service:        service,
		DatadogIssueID: issueID,
		DatadogURL:     fmt.Sprintf("https://app.%s/error-tracking/issue/%s", cfg.Datadog.Site, issueID),
	}
	if err := mgr.WriteResolve(issueID, resolve); err != nil {
		return fmt.Errorf("writing resolve data: %w", err)
	}

	fmt.Printf("Fix complete for %s\n", issueID)
	return nil
}

// parseField extracts a value from agent output like "- **Branch:** fix/issue-123-desc"
func parseField(content, field string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		// Match "- **Field:** value" or "**Field:** value"
		prefix := "**" + field + ":**"
		if idx := strings.Index(trimmed, prefix); idx != -1 {
			return strings.TrimSpace(trimmed[idx+len(prefix):])
		}
	}
	return ""
}

func init() {
	fixCmd.Flags().String("agent", "", "override agent command")
	fixCmd.Flags().String("service", "", "service name (auto-detected from error report if omitted)")
	rootCmd.AddCommand(fixCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -v -run TestFix`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/fix.go cmd/fix_test.go
git commit -m "feat(fido): add fix command with agent invocation and resolve.json"
```

---

### Task 8: List and Show Commands

**Files:**
- Create: `cmd/list.go`, `cmd/show.go`
- Test: `cmd/list_test.go`, `cmd/show_test.go`

- [ ] **Step 1: Write list command test**

```go
// cmd/list_test.go
package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ruter-as/fido/internal/reports"
)

func TestList_ShowsIssuesWithStages(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	mgr.WriteError("issue-1", "error 1")
	mgr.WriteError("issue-2", "error 2")
	mgr.WriteInvestigation("issue-2", "investigation 2")

	var buf bytes.Buffer
	err := runList(mgr, "", "", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "issue-1") {
		t.Error("expected issue-1 in output")
	}
	if !strings.Contains(output, "issue-2") {
		t.Error("expected issue-2 in output")
	}
	if !strings.Contains(output, "scanned") {
		t.Error("expected 'scanned' stage in output")
	}
	if !strings.Contains(output, "investigated") {
		t.Error("expected 'investigated' stage in output")
	}
}

func TestList_FilterByStatus(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	mgr.WriteError("issue-1", "error 1")
	mgr.WriteError("issue-2", "error 2")
	mgr.WriteInvestigation("issue-2", "investigation 2")

	var buf bytes.Buffer
	err := runList(mgr, "investigated", "", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "issue-1") {
		t.Error("issue-1 should be filtered out")
	}
	if !strings.Contains(output, "issue-2") {
		t.Error("expected issue-2 in output")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -v -run TestList`
Expected: FAIL

- [ ] **Step 3: Implement list and show commands**

```go
// cmd/list.go
package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List known error issues and their pipeline stage",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		status, _ := cmd.Flags().GetString("status")
		service, _ := cmd.Flags().GetString("service")

		return runList(mgr, status, service, os.Stdout)
	},
}

func runList(mgr *reports.Manager, statusFilter, serviceFilter string, w io.Writer) error {
	issues, err := mgr.ListIssues()
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ISSUE ID\tSTAGE")
	fmt.Fprintln(tw, "--------\t-----")

	for _, issue := range issues {
		if statusFilter != "" && string(issue.Stage) != statusFilter {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\n", issue.ID, issue.Stage)
	}

	return tw.Flush()
}

func init() {
	listCmd.Flags().String("status", "", "filter by stage: scanned, investigated, fixed")
	listCmd.Flags().String("service", "", "filter by service name")
	rootCmd.AddCommand(listCmd)
}
```

```go
// cmd/show.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <issue-id>",
	Short: "Show reports for an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		return runShow(issueID, mgr)
	},
}

func runShow(issueID string, mgr *reports.Manager) error {
	if !mgr.Exists(issueID) {
		return fmt.Errorf("no reports found for issue %s", issueID)
	}

	stage := mgr.Stage(issueID)
	fmt.Printf("=== Issue: %s (stage: %s) ===\n\n", issueID, stage)

	if content, err := mgr.ReadError(issueID); err == nil {
		fmt.Println("--- error.md ---")
		fmt.Println(content)
		fmt.Println()
	}

	if content, err := mgr.ReadInvestigation(issueID); err == nil {
		fmt.Println("--- investigation.md ---")
		fmt.Println(content)
		fmt.Println()
	}

	if content, err := mgr.ReadFix(issueID); err == nil {
		fmt.Println("--- fix.md ---")
		fmt.Println(content)
		fmt.Println()
	}

	if resolve, err := mgr.ReadResolve(issueID); err == nil {
		fmt.Println("--- resolve.json ---")
		fmt.Printf("Branch:    %s\n", resolve.Branch)
		fmt.Printf("MR URL:    %s\n", resolve.MRURL)
		fmt.Printf("MR Status: %s\n", resolve.MRStatus)
		fmt.Printf("Service:   %s\n", resolve.Service)
		fmt.Printf("Created:   %s\n", resolve.CreatedAt)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(showCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -v -run "TestList|TestShow"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/list.go cmd/show.go cmd/list_test.go
git commit -m "feat(fido): add list and show commands"
```

---

### Task 9: HTTP API Server

**Files:**
- Create: `internal/api/server.go`, `internal/api/handlers.go`, `cmd/serve.go`
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Install chi router**

Run: `go get github.com/go-chi/chi/v5`

- [ ] **Step 2: Write API handler tests**

```go
// internal/api/handlers_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruter-as/fido/internal/reports"
)

func TestListIssuesHandler(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error 1")
	mgr.WriteError("issue-2", "error 2")

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues", nil)
	w := httptest.NewRecorder()

	h.ListIssues(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp []IssueListItem
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 2 {
		t.Errorf("expected 2 issues, got %d", len(resp))
	}
}

func TestGetIssueHandler(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "# Error\ntest error")
	mgr.WriteInvestigation("issue-1", "# Investigation\nroot cause found")

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues/issue-1", nil)
	w := httptest.NewRecorder()

	// Simulate chi URL param
	h.GetIssue(w, withURLParam(req, "id", "issue-1"))

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp IssueDetail
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID != "issue-1" {
		t.Errorf("expected issue-1, got %s", resp.ID)
	}
	if resp.Stage != "investigated" {
		t.Errorf("expected investigated, got %s", resp.Stage)
	}
	if resp.Investigation == nil {
		t.Error("expected investigation to be present")
	}
	if resp.Resolve != nil {
		t.Error("expected resolve to be nil")
	}
}

func TestGetIssueHandler_NotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues/nonexistent", nil)
	w := httptest.NewRecorder()

	h.GetIssue(w, withURLParam(req, "id", "nonexistent"))

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/ -v`
Expected: FAIL

- [ ] **Step 4: Implement handlers**

```go
// internal/api/handlers.go
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

type IssueListItem struct {
	ID    string `json:"id"`
	Stage string `json:"stage"`
}

type IssueDetail struct {
	ID            string              `json:"id"`
	Stage         string              `json:"stage"`
	Error         string              `json:"error"`
	Investigation *string             `json:"investigation"`
	Fix           *string             `json:"fix"`
	Resolve       *reports.ResolveData `json:"resolve"`
}

type Handlers struct {
	reports *reports.Manager
	config  *config.Config
}

func NewHandlers(mgr *reports.Manager, cfg *config.Config) *Handlers {
	return &Handlers{reports: mgr, config: cfg}
}

func (h *Handlers) ListIssues(w http.ResponseWriter, r *http.Request) {
	issues, err := h.reports.ListIssues()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	statusFilter := r.URL.Query().Get("status")

	var items []IssueListItem
	for _, issue := range issues {
		if statusFilter != "" && string(issue.Stage) != statusFilter {
			continue
		}
		items = append(items, IssueListItem{
			ID:    issue.ID,
			Stage: string(issue.Stage),
		})
	}

	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) GetIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	errorContent, _ := h.reports.ReadError(id)

	detail := IssueDetail{
		ID:    id,
		Stage: string(h.reports.Stage(id)),
		Error: errorContent,
	}

	if inv, err := h.reports.ReadInvestigation(id); err == nil {
		detail.Investigation = &inv
	}
	if fix, err := h.reports.ReadFix(id); err == nil {
		detail.Fix = &fix
	}
	if resolve, err := h.reports.ReadResolve(id); err == nil {
		detail.Resolve = resolve
	}

	writeJSON(w, http.StatusOK, detail)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// withURLParam is a test helper to inject chi URL params
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
```

- [ ] **Step 5: Implement server setup**

```go
// internal/api/server.go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

type Server struct {
	handler  http.Handler
	handlers *Handlers
}

func NewServer(mgr *reports.Manager, cfg *config.Config) *Server {
	h := NewHandlers(mgr, cfg)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Route("/api", func(r chi.Router) {
		r.Get("/issues", h.ListIssues)
		r.Get("/issues/{id}", h.GetIssue)
		r.Post("/issues/{id}/investigate", h.TriggerInvestigate)
		r.Post("/issues/{id}/fix", h.TriggerFix)
		r.Get("/issues/{id}/progress", h.StreamProgress)
		r.Post("/scan", h.TriggerScan)
	})

	return &Server{handler: r, handlers: h}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func GetHandlers(s *Server) *Handlers {
	return s.handlers
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

Note: `TriggerInvestigate`, `TriggerFix`, `StreamProgress`, and `TriggerScan` handlers should be stubbed initially (returning `501 Not Implemented`) and implemented in a follow-up task when the action execution layer is built. The core read endpoints (`ListIssues`, `GetIssue`) are the priority for web UI integration.

- [ ] **Step 6: Create serve command**

```go
// cmd/serve.go
package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ruter-as/fido/internal/api"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetString("port")
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		ddClient := datadog.NewClient(
			cfg.Datadog.APIKey,
			cfg.Datadog.AppKey,
			fmt.Sprintf("https://api.%s", cfg.Datadog.Site),
		)

		server := api.NewServer(mgr, cfg)
		// Inject action functions so API handlers can trigger CLI operations
		handlers := api.GetHandlers(server)
		handlers.SetScanFunc(func() error {
			count, err := runScan(cfg, ddClient, mgr)
			if err != nil {
				return err
			}
			fmt.Printf("API scan complete: %d new issues\n", count)
			return nil
		})
		handlers.SetInvestigateFunc(func(issueID string) error {
			errorContent, _ := mgr.ReadError(issueID)
			service := extractServiceFromReport(errorContent)
			return runInvestigate(issueID, service, cfg, mgr)
		})
		handlers.SetFixFunc(func(issueID string) error {
			errorContent, _ := mgr.ReadError(issueID)
			service := extractServiceFromReport(errorContent)
			return runFix(issueID, service, cfg, mgr)
		})

		fmt.Printf("Fido API server listening on :%s\n", port)
		return http.ListenAndServe(":"+port, server)
	},
}

func init() {
	serveCmd.Flags().String("port", "8080", "port to listen on")
	rootCmd.AddCommand(serveCmd)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/api/ cmd/serve.go
git commit -m "feat(fido): add HTTP API server with issue list and detail endpoints"
```

---

### Task 10: Daemon Command

**Files:**
- Create: `cmd/daemon.go`
- Test: `cmd/daemon_test.go`

- [ ] **Step 1: Write daemon test**

```go
// cmd/daemon_test.go
package cmd

import (
	"context"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"15m", 15 * time.Minute},
		{"1h", 1 * time.Hour},
		{"30s", 30 * time.Second},
	}

	for _, tt := range tests {
		d, err := time.ParseDuration(tt.input)
		if err != nil {
			t.Errorf("failed to parse %q: %v", tt.input, err)
		}
		if d != tt.expected {
			t.Errorf("expected %v, got %v", tt.expected, d)
		}
	}
}

func TestDaemonLoop_RunsAndCancels(t *testing.T) {
	callCount := 0
	scanFn := func() error {
		callCount++
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	runDaemonLoop(ctx, 50*time.Millisecond, scanFn)

	if callCount < 2 {
		t.Errorf("expected at least 2 scan calls, got %d", callCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -v -run TestDaemon`
Expected: FAIL

- [ ] **Step 3: Implement daemon command**

```go
// cmd/daemon.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run fido scan on a recurring interval",
	RunE: func(cmd *cobra.Command, args []string) error {
		intervalStr, _ := cmd.Flags().GetString("interval")
		if intervalStr == "" {
			intervalStr = cfg.Scan.Interval
		}

		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return fmt.Errorf("invalid interval %q: %w", intervalStr, err)
		}

		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)
		ddClient := datadog.NewClient(
			cfg.Datadog.APIKey,
			cfg.Datadog.AppKey,
			fmt.Sprintf("https://api.%s", cfg.Datadog.Site),
		)

		scanFn := func() error {
			count, err := runScan(cfg, ddClient, mgr)
			if err != nil {
				fmt.Printf("Scan error: %v\n", err)
				return err
			}
			fmt.Printf("Scan complete: %d new issues\n", count)
			return nil
		}

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		fmt.Printf("Fido daemon started (interval: %s)\n", interval)
		runDaemonLoop(ctx, interval, scanFn)
		fmt.Println("Fido daemon stopped")
		return nil
	},
}

func runDaemonLoop(ctx context.Context, interval time.Duration, scanFn func() error) {
	// Run immediately on start
	scanFn()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanFn()
		}
	}
}

func init() {
	daemonCmd.Flags().String("interval", "", "scan interval (default: config value)")
	rootCmd.AddCommand(daemonCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -v -run TestDaemon`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/daemon.go cmd/daemon_test.go
git commit -m "feat(fido): add daemon command for recurring scans"
```

---

### Task 11: Web UI Scaffolding

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/api/client.ts`, `web/tsconfig.json`

- [ ] **Step 1: Initialize web project**

```bash
cd web
npm create vite@latest . -- --template react-ts
npm install
npm install react-router-dom react-markdown
```

- [ ] **Step 2: Create API client**

```typescript
// web/src/api/client.ts
const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export interface IssueListItem {
  id: string;
  stage: string;
}

export interface ResolveData {
  branch: string;
  mr_url: string;
  mr_status: string;
  service: string;
  datadog_issue_id: string;
  datadog_url: string;
  created_at: string;
}

export interface IssueDetail {
  id: string;
  stage: string;
  error: string;
  investigation: string | null;
  fix: string | null;
  resolve: ResolveData | null;
}

export async function listIssues(status?: string): Promise<IssueListItem[]> {
  const params = new URLSearchParams();
  if (status) params.set('status', status);
  const res = await fetch(`${API_BASE}/api/issues?${params}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export async function getIssue(id: string): Promise<IssueDetail> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export async function triggerInvestigate(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/investigate`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export async function triggerFix(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/fix`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export async function triggerScan(): Promise<void> {
  const res = await fetch(`${API_BASE}/api/scan`, { method: 'POST' });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export function subscribeProgress(id: string, onMessage: (data: string) => void): EventSource {
  const es = new EventSource(`${API_BASE}/api/issues/${encodeURIComponent(id)}/progress`);
  es.onmessage = (event) => onMessage(event.data);
  return es;
}
```

- [ ] **Step 3: Create App with routing**

```tsx
// web/src/App.tsx
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { IssueDetail } from './pages/IssueDetail';

export function App() {
  return (
    <BrowserRouter>
      <div style={{ maxWidth: '1200px', margin: '0 auto', padding: '1rem' }}>
        <h1>Fido</h1>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/issues/:id" element={<IssueDetail />} />
        </Routes>
      </div>
    </BrowserRouter>
  );
}
```

- [ ] **Step 4: Verify it builds**

Run: `cd web && npm run build`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add web/
git commit -m "feat(fido): scaffold web UI with Vite, React, and API client"
```

---

### Task 12: Web UI — Dashboard and Issue Detail Pages

**Files:**
- Create: `web/src/pages/Dashboard.tsx`, `web/src/pages/IssueDetail.tsx`, `web/src/components/StageIndicator.tsx`, `web/src/components/MarkdownViewer.tsx`

- [ ] **Step 1: Create StageIndicator component**

```tsx
// web/src/components/StageIndicator.tsx
interface Props {
  stage: string;
}

const stageColors: Record<string, string> = {
  scanned: '#f59e0b',
  investigated: '#3b82f6',
  fixed: '#10b981',
};

export function StageIndicator({ stage }: Props) {
  return (
    <span
      style={{
        padding: '2px 8px',
        borderRadius: '4px',
        backgroundColor: stageColors[stage] || '#6b7280',
        color: 'white',
        fontSize: '0.85em',
        fontWeight: 500,
      }}
    >
      {stage}
    </span>
  );
}
```

- [ ] **Step 2: Create MarkdownViewer component**

```tsx
// web/src/components/MarkdownViewer.tsx
import ReactMarkdown from 'react-markdown';

interface Props {
  content: string;
  title: string;
}

export function MarkdownViewer({ content, title }: Props) {
  return (
    <div style={{ border: '1px solid #e5e7eb', borderRadius: '8px', padding: '1rem', marginBottom: '1rem' }}>
      <h3 style={{ marginTop: 0 }}>{title}</h3>
      <ReactMarkdown>{content}</ReactMarkdown>
    </div>
  );
}
```

- [ ] **Step 3: Create Dashboard page**

```tsx
// web/src/pages/Dashboard.tsx
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { listIssues, triggerScan, type IssueListItem } from '../api/client';
import { StageIndicator } from '../components/StageIndicator';

export function Dashboard() {
  const [issues, setIssues] = useState<IssueListItem[]>([]);
  const [filter, setFilter] = useState('');
  const [loading, setLoading] = useState(true);

  const fetchIssues = async () => {
    setLoading(true);
    try {
      const data = await listIssues(filter || undefined);
      setIssues(data);
    } catch (err) {
      console.error('Failed to fetch issues:', err);
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchIssues();
  }, [filter]);

  const handleScan = async () => {
    await triggerScan();
    fetchIssues();
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
        <h2>Issues</h2>
        <div>
          <select value={filter} onChange={(e) => setFilter(e.target.value)} style={{ marginRight: '0.5rem' }}>
            <option value="">All stages</option>
            <option value="scanned">Scanned</option>
            <option value="investigated">Investigated</option>
            <option value="fixed">Fixed</option>
          </select>
          <button onClick={handleScan}>Scan Now</button>
        </div>
      </div>

      {loading ? (
        <p>Loading...</p>
      ) : issues.length === 0 ? (
        <p>No issues found. Run a scan to get started.</p>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '2px solid #e5e7eb' }}>
              <th style={{ textAlign: 'left', padding: '0.5rem' }}>Issue ID</th>
              <th style={{ textAlign: 'left', padding: '0.5rem' }}>Stage</th>
            </tr>
          </thead>
          <tbody>
            {issues.map((issue) => (
              <tr key={issue.id} style={{ borderBottom: '1px solid #e5e7eb' }}>
                <td style={{ padding: '0.5rem' }}>
                  <Link to={`/issues/${issue.id}`}>{issue.id}</Link>
                </td>
                <td style={{ padding: '0.5rem' }}>
                  <StageIndicator stage={issue.stage} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Create IssueDetail page**

```tsx
// web/src/pages/IssueDetail.tsx
import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getIssue, triggerInvestigate, triggerFix, type IssueDetail as IssueDetailType } from '../api/client';
import { StageIndicator } from '../components/StageIndicator';
import { MarkdownViewer } from '../components/MarkdownViewer';

export function IssueDetail() {
  const { id } = useParams<{ id: string }>();
  const [issue, setIssue] = useState<IssueDetailType | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchIssue = async () => {
    if (!id) return;
    setLoading(true);
    try {
      const data = await getIssue(id);
      setIssue(data);
    } catch (err) {
      console.error('Failed to fetch issue:', err);
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchIssue();
  }, [id]);

  if (loading) return <p>Loading...</p>;
  if (!issue) return <p>Issue not found</p>;

  const handleInvestigate = async () => {
    await triggerInvestigate(issue.id);
    fetchIssue();
  };

  const handleFix = async () => {
    await triggerFix(issue.id);
    fetchIssue();
  };

  return (
    <div>
      <Link to="/">&larr; Back to dashboard</Link>
      <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', margin: '1rem 0' }}>
        <h2 style={{ margin: 0 }}>{issue.id}</h2>
        <StageIndicator stage={issue.stage} />
      </div>

      <MarkdownViewer title="Error Report" content={issue.error} />

      {issue.investigation ? (
        <MarkdownViewer title="Investigation" content={issue.investigation} />
      ) : (
        <button onClick={handleInvestigate}>Investigate this issue</button>
      )}

      {issue.fix ? (
        <MarkdownViewer title="Fix" content={issue.fix} />
      ) : issue.investigation ? (
        <button onClick={handleFix}>Fix this issue</button>
      ) : null}

      {issue.resolve && (
        <div style={{ border: '1px solid #10b981', borderRadius: '8px', padding: '1rem', marginTop: '1rem' }}>
          <h3 style={{ marginTop: 0 }}>Resolution</h3>
          <p><strong>Branch:</strong> {issue.resolve.branch}</p>
          <p><strong>MR:</strong> <a href={issue.resolve.mr_url} target="_blank" rel="noreferrer">{issue.resolve.mr_url}</a></p>
          <p><strong>Status:</strong> {issue.resolve.mr_status}</p>
          <p><strong>Created:</strong> {issue.resolve.created_at}</p>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Verify it builds**

Run: `cd web && npm run build`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add web/src/
git commit -m "feat(fido): add dashboard and issue detail pages"
```

---

### Task 13: Docker Compose Setup

**Files:**
- Create: `Dockerfile`, `web/Dockerfile`, `docker-compose.yml`

- [ ] **Step 1: Create Go Dockerfile**

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o fido .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates git
COPY --from=builder /app/fido /usr/local/bin/fido
ENTRYPOINT ["fido"]
```

- [ ] **Step 2: Create web Dockerfile**

```dockerfile
# web/Dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

- [ ] **Step 3: Create nginx config for web**

```nginx
# web/nginx.conf
server {
    listen 80;
    root /usr/share/nginx/html;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    location /api/ {
        proxy_pass http://fido:8080;
        proxy_http_version 1.1;
        proxy_set_header Connection '';
        proxy_buffering off;
    }
}
```

- [ ] **Step 4: Create docker-compose.yml**

```yaml
# docker-compose.yml
services:
  fido:
    build: .
    command: ["serve", "--port", "8080", "--config", "/root/.fido/config.yml"]
    volumes:
      - ~/.fido:/root/.fido
    ports:
      - "8080:8080"

  fido-daemon:
    build: .
    command: ["daemon", "--config", "/root/.fido/config.yml"]
    volumes:
      - ~/.fido:/root/.fido

  fido-web:
    build: ./web
    ports:
      - "3000:80"
    depends_on:
      - fido

```

The `~/.fido/config.yml` file must exist on the host before running `docker compose up`. This is the same config file the CLI uses outside Docker — no separate env var mechanism needed.

- [ ] **Step 5: Create setup instructions**

Add to the project README or print from `fido config`:

```
# Prerequisites for Docker Compose:
# 1. Create ~/.fido/config.yml (run `fido config` or copy from config.example.yml)
# 2. Run: docker compose up
```

- [ ] **Step 6: Verify docker-compose config is valid**

Run: `docker compose config`
Expected: Config prints without errors

- [ ] **Step 7: Commit**

```bash
git add Dockerfile web/Dockerfile web/nginx.conf docker-compose.yml
git commit -m "feat(fido): add Docker Compose setup with Go API, daemon, and web UI"
```

---

### Task 14: API Action Handlers (Investigate, Fix, Scan triggers)

**Files:**
- Modify: `internal/api/handlers.go`
- Test: `internal/api/handlers_test.go`

The stubbed handlers (`TriggerInvestigate`, `TriggerFix`, `TriggerScan`, `StreamProgress`) need to be implemented so the web UI action buttons work.

- [ ] **Step 1: Write tests for action handlers**

```go
// Add to internal/api/handlers_test.go

func TestTriggerScanHandler(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	scanCalled := false
	h := NewHandlers(mgr, nil)
	h.SetScanFunc(func() error {
		scanCalled = true
		return nil
	})

	req := httptest.NewRequest("POST", "/api/scan", nil)
	w := httptest.NewRecorder()

	h.TriggerScan(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}
	// Give goroutine time to run
	time.Sleep(50 * time.Millisecond)
	if !scanCalled {
		t.Error("expected scan function to be called")
	}
}

func TestTriggerInvestigateHandler(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "# Error\ntest")

	investigateCalled := ""
	h := NewHandlers(mgr, nil)
	h.SetInvestigateFunc(func(issueID string) error {
		investigateCalled = issueID
		return nil
	})

	req := httptest.NewRequest("POST", "/api/issues/issue-1/investigate", nil)
	w := httptest.NewRecorder()

	h.TriggerInvestigate(w, withURLParam(req, "id", "issue-1"))

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}
	time.Sleep(50 * time.Millisecond)
	if investigateCalled != "issue-1" {
		t.Errorf("expected investigate for issue-1, got %q", investigateCalled)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -v -run "TestTrigger"`
Expected: FAIL

- [ ] **Step 3: Implement action handlers**

Add to `internal/api/handlers.go`:

```go
import (
	"sync"
	"time"
)

// Action function types — injected by the serve command
type ScanFunc func() error
type InvestigateFunc func(issueID string) error
type FixFunc func(issueID string) error

// Add fields to Handlers struct:
// scanFn        ScanFunc
// investigateFn InvestigateFunc
// fixFn         FixFunc
// running       sync.Map  // tracks running actions by issue ID

func (h *Handlers) SetScanFunc(fn ScanFunc)             { h.scanFn = fn }
func (h *Handlers) SetInvestigateFunc(fn InvestigateFunc) { h.investigateFn = fn }
func (h *Handlers) SetFixFunc(fn FixFunc)                 { h.fixFn = fn }

func (h *Handlers) TriggerScan(w http.ResponseWriter, r *http.Request) {
	if h.scanFn == nil {
		writeError(w, http.StatusNotImplemented, "scan not configured")
		return
	}
	go h.scanFn()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) TriggerInvestigate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, loaded := h.running.LoadOrStore(id, true); loaded {
		writeError(w, http.StatusConflict, "action already running for this issue")
		return
	}
	if h.investigateFn == nil {
		h.running.Delete(id)
		writeError(w, http.StatusNotImplemented, "investigate not configured")
		return
	}
	go func() {
		defer h.running.Delete(id)
		h.investigateFn(id)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

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
	go func() {
		defer h.running.Delete(id)
		h.fixFn(id)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) StreamProgress(w http.ResponseWriter, r *http.Request) {
	// SSE streaming — placeholder that reports completion status
	// Full implementation would capture and stream agent stdout
	id := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Poll until the action completes (running map entry is removed)
	for {
		if _, running := h.running.Load(id); !running {
			fmt.Fprintf(w, "data: {\"status\": \"complete\"}\n\n")
			flusher.Flush()
			return
		}
		fmt.Fprintf(w, "data: {\"status\": \"running\"}\n\n")
		flusher.Flush()
		time.Sleep(2 * time.Second)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat(fido): implement API action handlers for investigate, fix, and scan"
```

---

### Task 15: Integration Smoke Test

**Files:**
- Create: `scripts/smoke-test.sh`

- [ ] **Step 1: Create smoke test script**

```bash
#!/usr/bin/env bash
# scripts/smoke-test.sh
# Verifies Fido builds and basic commands work without real Datadog credentials.
set -euo pipefail

echo "=== Building fido ==="
go build -o ./bin/fido .

echo "=== Testing: fido --help ==="
./bin/fido --help

echo "=== Testing: fido list (with temp config) ==="
TMPDIR=$(mktemp -d)
mkdir -p "$TMPDIR/reports"
cat > "$TMPDIR/config.yml" <<EOF
datadog:
  api_key: "fake"
  app_key: "fake"
  site: "datadoghq.eu"
scan:
  interval: "15m"
  since: "24h"
EOF

./bin/fido --config "$TMPDIR/config.yml" list

echo "=== Testing: fido show (nonexistent issue) ==="
./bin/fido --config "$TMPDIR/config.yml" show nonexistent 2>&1 || true

echo "=== Running all unit tests ==="
go test ./... -v

echo "=== Building web UI ==="
cd web && npm run build

echo "=== All smoke tests passed ==="
rm -rf "$TMPDIR" ./bin/fido
```

- [ ] **Step 2: Run smoke test**

Run: `chmod +x scripts/smoke-test.sh && ./scripts/smoke-test.sh`
Expected: All steps pass

- [ ] **Step 3: Commit**

```bash
git add scripts/smoke-test.sh
git commit -m "test(fido): add integration smoke test script"
```
