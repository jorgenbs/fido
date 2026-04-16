// cmd/start.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ruter-as/fido/internal/pidfile"
)

func fidoDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".fido")
}

func pidPath() string {
	return filepath.Join(fidoDir(), "fido.pid")
}

func portPath() string {
	return filepath.Join(fidoDir(), "fido.port")
}

func logPath() string {
	return filepath.Join(fidoDir(), "fido.log")
}

func runStart(port string) error {
	// Check if already running
	if pid, err := pidfile.Read(pidPath()); err == nil {
		if pidfile.IsRunning(pid) {
			fmt.Printf("Fido is already running (PID %d)\n", pid)
			return nil
		}
		// Stale PID file — clean up
		pidfile.Remove(pidPath())
		pidfile.Remove(portPath())
	}

	// Open log file for stdout/stderr redirection
	logFile, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	// Find our own executable
	exe, err := os.Executable()
	if err != nil {
		logFile.Close()
		return fmt.Errorf("finding executable: %w", err)
	}

	// Build args — forward config flag if set
	args := []string{"serve", "--port", port}
	if cfgFile != "" {
		args = append([]string{"--config", cfgFile}, args...)
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Detach from parent process group
	cmd.SysProcAttr = detachSysProcAttr()

	fmt.Print("Starting daemon... ")
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting fido serve: %w", err)
	}

	pid := cmd.Process.Pid

	// Write PID and port files
	if err := pidfile.Write(pidPath(), pid); err != nil {
		logFile.Close()
		return fmt.Errorf("writing pid file: %w", err)
	}
	if err := pidfile.WritePort(portPath(), port); err != nil {
		logFile.Close()
		return fmt.Errorf("writing port file: %w", err)
	}

	// Release the process so it runs independently
	cmd.Process.Release()
	logFile.Close()

	// Brief wait to check it didn't die immediately
	time.Sleep(500 * time.Millisecond)
	if !pidfile.IsRunning(pid) {
		pidfile.Remove(pidPath())
		pidfile.Remove(portPath())
		return fmt.Errorf("process exited immediately — check %s for details", logPath())
	}

	fmt.Println("done")
	fmt.Printf("Dashboard on http://localhost:%s\n", port)
	fmt.Printf("Logs: %s\n", logPath())
	return nil
}
