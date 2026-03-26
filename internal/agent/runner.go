package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Runner struct {
	Command string
}

func (r *Runner) Run(promptContent, repoDir string) (string, error) {
	parts := strings.Fields(r.Command)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(promptContent)

	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("agent failed (exit %d)", exitErr.ExitCode())
		}
		return "", fmt.Errorf("running agent: %w", err)
	}

	return buf.String(), nil
}

func (r *Runner) RunInteractive(promptContent, repoDir string) error {
	parts := strings.Fields(r.Command)
	parts = append(parts, promptContent)

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = repoDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("agent failed (exit %d)", exitErr.ExitCode())
		}
		return fmt.Errorf("running agent: %w", err)
	}
	return nil
}
