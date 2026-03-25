package agent

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestRunner_BuildPromptFile(t *testing.T) {
	r := &Runner{}

	content := "# Error\nSomething broke"
	path, err := r.WritePromptFile("issue-123", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading prompt file: %v", err)
	}
	if !strings.Contains(string(data), "Something broke") {
		t.Error("prompt file should contain the content")
	}
}

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

func echoCommand() string {
	if runtime.GOOS == "windows" {
		return "type"
	}
	return "cat"
}
