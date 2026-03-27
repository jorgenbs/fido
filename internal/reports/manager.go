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

type MetaData struct {
	Title            string `json:"title"`
	Message          string `json:"message,omitempty"`
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
	Confidence       string `json:"confidence,omitempty"`
	Complexity       string `json:"complexity,omitempty"`
	CodeFixable      string `json:"code_fixable,omitempty"`
}

type IssueSummary struct {
	ID    string
	Stage Stage
	Meta  *MetaData // nil if meta.json not present (pre-v2 issue)
	MRURL string    // from resolve.json if present
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

func (m *Manager) WriteMetadata(issueID string, data *MetaData) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}
	return m.writeFile(issueID, "meta.json", string(b))
}

func (m *Manager) ReadMetadata(issueID string) (*MetaData, error) {
	content, err := m.readFile(issueID, "meta.json")
	if err != nil {
		return nil, err
	}
	var data MetaData
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, fmt.Errorf("parsing meta.json: %w", err)
	}
	return &data, nil
}

func (m *Manager) SetIgnored(issueID string, ignored bool) error {
	meta, err := m.ReadMetadata(issueID)
	if err != nil {
		return fmt.Errorf("reading metadata: %w", err)
	}
	meta.Ignored = ignored
	return m.WriteMetadata(issueID, meta)
}

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

func (m *Manager) ListIssues(showIgnored bool) ([]IssueSummary, error) {
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
		if !m.fileExists(id, "error.md") {
			continue
		}
		summary := IssueSummary{ID: id, Stage: m.Stage(id)}
		if meta, err := m.ReadMetadata(id); err == nil {
			summary.Meta = meta
			if !showIgnored && meta.Ignored {
				continue
			}
		}
		if resolve, err := m.ReadResolve(id); err == nil {
			summary.MRURL = resolve.MRURL
		}
		issues = append(issues, summary)
	}
	return issues, nil
}
