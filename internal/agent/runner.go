package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Runner struct {
	Command string
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
