package agent

import (
	"runtime"
	"strings"
	"testing"
)

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

func TestRunner_RunInteractive(t *testing.T) {
	r := &Runner{
		Command: "true",
	}

	err := r.RunInteractive("Hello from prompt", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_RunInteractive_CommandFails(t *testing.T) {
	r := &Runner{
		Command: "false",
	}

	err := r.RunInteractive("prompt", t.TempDir())
	if err == nil {
		t.Error("expected error for failing command")
	}
}

func echoCommand() string {
	if runtime.GOOS == "windows" {
		return "type"
	}
	return "cat"
}
