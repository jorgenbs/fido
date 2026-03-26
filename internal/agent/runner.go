package agent

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Runner struct {
	Command  string
	Progress io.Writer // if set, agent stdout is also written here in real-time
}

func (r *Runner) Run(promptContent, repoDir string) (string, error) {
	parts := strings.Fields(r.Command)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(promptContent)

	var buf bytes.Buffer
	writers := []io.Writer{os.Stdout, &buf}
	if r.Progress != nil {
		writers = append(writers, r.Progress)
	}
	cmd.Stdout = io.MultiWriter(writers...)

	// Capture stderr separately so it appears with a prefix and is captured for debugging.
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	// Log first 500 chars of prompt to help verify correct input.
	preview := promptContent
	if len(preview) > 500 {
		preview = preview[:500] + "…"
	}
	log.Printf("[agent] starting %q in %s (prompt: %d bytes)\n--- prompt preview ---\n%s\n---", r.Command, repoDir, len(promptContent), preview)

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting agent: %w", err)
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		start := time.Now()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(start).Round(time.Second)
				log.Printf("[agent] still running... (%s elapsed)", elapsed)
				if r.Progress != nil {
					fmt.Fprintf(r.Progress, "\n[fido] agent running (%s elapsed)...\n", elapsed)
				}
			}
		}
	}()

	err := cmd.Wait()
	close(done)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("agent failed (exit %d); stderr: %s", exitErr.ExitCode(), stderrBuf.String())
		}
		return "", fmt.Errorf("running agent: %w; stderr: %s", err, stderrBuf.String())
	}

	log.Printf("[agent] completed (stdout: %d bytes, stderr: %d bytes)", buf.Len(), stderrBuf.Len())
	return buf.String(), nil
}

func (r *Runner) RunInteractive(promptContent, repoDir string) error {
	parts := strings.Fields(r.Command)
	parts = append(parts, promptContent)

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = repoDir
	cmd.Stdin = os.Stdin
	if r.Progress != nil {
		cmd.Stdout = io.MultiWriter(os.Stdout, r.Progress)
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr

	log.Printf("[agent] starting interactive %q in %s", r.Command, repoDir)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("agent failed (exit %d)", exitErr.ExitCode())
		}
		return fmt.Errorf("running agent: %w", err)
	}
	return nil
}
