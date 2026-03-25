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
