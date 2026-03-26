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
	cmd.Stderr = os.Stderr

	log.Printf("[agent] starting %q in %s (prompt: %d bytes)", r.Command, repoDir, len(promptContent))

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
			return "", fmt.Errorf("agent failed (exit %d)", exitErr.ExitCode())
		}
		return "", fmt.Errorf("running agent: %w", err)
	}

	log.Printf("[agent] completed (%d bytes output)", buf.Len())
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
