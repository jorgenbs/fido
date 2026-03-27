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
