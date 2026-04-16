package pidfile

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	if err := Write(path, 12345); err != nil {
		t.Fatalf("Write: %v", err)
	}

	pid, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected 12345, got %d", pid)
	}
}

func TestRead_Missing(t *testing.T) {
	_, err := Read("/nonexistent/path.pid")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRead_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pid")
	os.WriteFile(path, []byte("not-a-number\n"), 0644)

	_, err := Read(path)
	if err == nil {
		t.Error("expected error for invalid content")
	}
}

func TestIsRunning_CurrentProcess(t *testing.T) {
	if !IsRunning(os.Getpid()) {
		t.Error("expected current process to be running")
	}
}

func TestIsRunning_DeadProcess(t *testing.T) {
	if IsRunning(99999999) {
		t.Error("expected non-existent PID to not be running")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)

	Remove(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}
}
