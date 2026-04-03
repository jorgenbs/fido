package gitlab

import (
	"bytes"
	"encoding/json"
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

// extractPipelineURL finds the first https://...pipelines/... URL in the output.
func extractPipelineURL(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") && strings.Contains(line, "pipelines") {
			return line
		}
	}
	return ""
}

type mrViewJSON struct {
	State    string `json:"state"`
	Pipeline *struct {
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	} `json:"pipeline"`
}

// FetchMRStatus runs `glab mr view <branch> --output json` in repoPath and returns
// the MR state ("merged", "opened", "closed"), CI status, and CI URL.
// Returns ("", "", "", nil) if no MR found.
func FetchMRStatus(branch, repoPath string) (mrStatus, ciStatus, ciURL string, err error) {
	cmd := exec.Command("glab", "mr", "view", branch, "--output", "json")
	cmd.Dir = repoPath
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	execErr := cmd.Run()
	output := stdout.String()

	if execErr != nil && len(strings.TrimSpace(output)) == 0 {
		// glab exits non-zero when no MR exists (e.g. branch deleted after merge).
		// Treat as "no MR found" rather than an error.
		return "", "", "", nil
	}

	mrStatus, ciStatus, ciURL = parseMRStatusJSON(output)
	return mrStatus, ciStatus, ciURL, nil
}

// parseMRStatusJSON parses JSON output from `glab mr view --output json`.
func parseMRStatusJSON(output string) (mrStatus, ciStatus, ciURL string) {
	var data mrViewJSON
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		return "", "", ""
	}
	mrStatus = data.State
	if data.Pipeline != nil {
		ciStatus = normalizeCIStatus(data.Pipeline.Status)
		ciURL = data.Pipeline.WebURL
	}
	return
}

// normalizeCIStatus maps GitLab pipeline status values to fido's internal status strings.
func normalizeCIStatus(s string) string {
	switch strings.ToLower(s) {
	case "success":
		return "passed"
	case "failed":
		return "failed"
	case "running":
		return "running"
	case "pending":
		return "pending"
	case "canceled", "cancelled":
		return "canceled"
	default:
		return s
	}
}
