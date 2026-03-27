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
	os.MkdirAll(dir+"/.git", 0755)

	_, _, err := FetchCIStatus("main", dir)
	if err == nil {
		t.Error("expected error when glab not found")
	}
}

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
