# Distribution & Daemon Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Fido installable via `curl | sh` with self-upgrade, and add daemon management so bare `fido` starts everything in the background.

**Architecture:** Two phases. Phase 1 adds daemon management commands (start/status/stop) using PID files in `~/.fido/`. Phase 2 migrates the module path, adds version embedding, GoReleaser config, GitHub Actions CI/CD, install script, and self-upgrade command.

**Tech Stack:** Go 1.25, Cobra CLI, GoReleaser, GitHub Actions, Node.js 22 (frontend build)

**Spec:** `docs/superpowers/specs/2026-04-16-distribution-daemon-management-design.md`

---

## Phase 1: Daemon Management

### Task 1: PID file utilities

Shared helpers for reading/writing PID and port files, and checking if a process is alive. Used by start, status, and stop commands.

**Files:**
- Create: `internal/pidfile/pidfile.go`
- Create: `internal/pidfile/pidfile_test.go`

- [ ] **Step 1: Write failing tests for pidfile package**

```go
// internal/pidfile/pidfile_test.go
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
	// Our own PID should be running
	if !IsRunning(os.Getpid()) {
		t.Error("expected current process to be running")
	}
}

func TestIsRunning_DeadProcess(t *testing.T) {
	// PID 0 is never a user process; signal check should fail
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pidfile/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement pidfile package**

```go
// internal/pidfile/pidfile.go
package pidfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Write writes a PID to the given file path.
func Write(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0644)
}

// Read reads a PID from the given file path.
func Read(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// IsRunning checks whether a process with the given PID is alive.
func IsRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// Remove deletes the file at path, ignoring errors if it doesn't exist.
func Remove(path string) {
	os.Remove(path)
}

// ReadPort reads a port string from the given file path.
func ReadPort(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	port := strings.TrimSpace(string(data))
	if port == "" {
		return "", fmt.Errorf("empty port file")
	}
	return port, nil
}

// WritePort writes a port string to the given file path.
func WritePort(path string, port string) error {
	return os.WriteFile(path, []byte(port+"\n"), 0644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pidfile/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pidfile/pidfile.go internal/pidfile/pidfile_test.go
git commit -m "feat: add pidfile utilities for daemon management"
```

---

### Task 2: `fido` bare command — background start

Set `rootCmd.RunE` so running `fido` with no subcommand forks `fido serve` in the background with PID/port tracking.

**Files:**
- Create: `cmd/start.go`
- Modify: `cmd/root.go`

- [ ] **Step 1: Create `cmd/start.go`**

```go
// cmd/start.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jorgenbs/fido/internal/pidfile"
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
```

- [ ] **Step 2: Create platform-specific detach helper**

```go
// cmd/start_unix.go
//go:build !windows

package cmd

import "syscall"

func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
```

- [ ] **Step 3: Wire start into rootCmd**

Add `RunE` to `rootCmd` in `cmd/root.go` and add the `--port` flag to the root command:

In `cmd/root.go`, add to the `init()` function:
```go
rootCmd.Flags().String("port", "8080", "port for the dashboard")
```

Set `RunE` on `rootCmd`:
```go
var rootCmd = &cobra.Command{
	Use:   "fido",
	Short: "Fetch errors from Datadog, investigate, and propose fixes",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetString("port")
		return runStart(port)
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// ... existing config loading unchanged ...
	},
}
```

And in `init()`, add:
```go
rootCmd.Flags().String("port", "8080", "port for the dashboard")
```

- [ ] **Step 4: Build and test manually**

```bash
go build -o fido . && ./fido
# Should print: Starting daemon... done / Dashboard on http://localhost:8080
# Verify PID file:
cat ~/.fido/fido.pid
# Verify process is running:
curl -s localhost:8080/api/issues | head -c 100
# Clean up:
kill $(cat ~/.fido/fido.pid)
```

- [ ] **Step 5: Commit**

```bash
git add cmd/start.go cmd/start_unix.go cmd/root.go
git commit -m "feat: add bare fido command for background daemon start"
```

---

### Task 3: `fido status` command

**Files:**
- Create: `cmd/status.go`

- [ ] **Step 1: Create `cmd/status.go`**

```go
// cmd/status.go
package cmd

import (
	"fmt"

	"github.com/jorgenbs/fido/internal/pidfile"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the Fido daemon is running",
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := pidfile.Read(pidPath())
		if err != nil {
			fmt.Println("Fido is not running")
			return nil
		}

		if !pidfile.IsRunning(pid) {
			// Stale PID file
			pidfile.Remove(pidPath())
			pidfile.Remove(portPath())
			fmt.Println("Fido is not running (cleaned up stale PID file)")
			return nil
		}

		port := "unknown"
		if p, err := pidfile.ReadPort(portPath()); err == nil {
			port = p
		}

		fmt.Printf("Fido is running (PID %d) on port %s\n", pid, port)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
```

- [ ] **Step 2: Build and test manually**

```bash
go build -o fido .
./fido          # start it
./fido status   # should say running with PID and port
kill $(cat ~/.fido/fido.pid)
./fido status   # should say not running, clean up stale PID
```

- [ ] **Step 3: Commit**

```bash
git add cmd/status.go
git commit -m "feat: add fido status command"
```

---

### Task 4: `fido stop` command

**Files:**
- Create: `cmd/stop.go`

- [ ] **Step 1: Create `cmd/stop.go`**

```go
// cmd/stop.go
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/jorgenbs/fido/internal/pidfile"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running Fido daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := pidfile.Read(pidPath())
		if err != nil {
			fmt.Println("Fido is not running")
			return nil
		}

		if !pidfile.IsRunning(pid) {
			pidfile.Remove(pidPath())
			pidfile.Remove(portPath())
			fmt.Println("Fido is not running (cleaned up stale PID file)")
			return nil
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("finding process %d: %w", pid, err)
		}

		fmt.Printf("Stopping Fido (PID %d)... ", pid)
		if err := proc.Signal(os.Interrupt); err != nil {
			return fmt.Errorf("sending signal: %w", err)
		}

		// Wait for process to exit with timeout
		deadline := time.After(10 * time.Second)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-deadline:
				// Force kill
				proc.Kill()
				fmt.Println("force killed")
				pidfile.Remove(pidPath())
				pidfile.Remove(portPath())
				return nil
			case <-ticker.C:
				if !pidfile.IsRunning(pid) {
					fmt.Println("done")
					pidfile.Remove(pidPath())
					pidfile.Remove(portPath())
					return nil
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
```

- [ ] **Step 2: Build and test the full lifecycle**

```bash
go build -o fido .
./fido          # start
./fido status   # running
./fido stop     # stop
./fido status   # not running
```

- [ ] **Step 3: Commit**

```bash
git add cmd/stop.go
git commit -m "feat: add fido stop command"
```

---

## Phase 2: Distribution

### Task 5: Module path migration

Change all imports from `github.com/jorgenbs/fido` to `github.com/jorgenbs/fido`.

**Files:**
- Modify: `go.mod`
- Modify: all `.go` files that import `github.com/jorgenbs/fido/...`

The full list of files with imports to change (29 files):
- `go.mod`
- `main.go`
- `cmd/root.go`, `cmd/serve.go`, `cmd/scan.go`, `cmd/list.go`, `cmd/show.go`, `cmd/fix.go`, `cmd/investigate.go`, `cmd/import.go`
- `cmd/scan_test.go`, `cmd/list_test.go`, `cmd/fix_test.go`, `cmd/investigate_test.go`, `cmd/import_test.go`
- `cmd/start.go`, `cmd/status.go`, `cmd/stop.go` (newly created — use new path from the start)
- `internal/api/server.go`, `internal/api/handlers.go`, `internal/api/handlers_test.go`, `internal/api/hub.go`, `internal/api/hub_test.go`
- `internal/syncer/adapter.go`, `internal/syncer/engine.go`

- [ ] **Step 1: Change module path in go.mod**

In `go.mod`, change line 1:
```
module github.com/jorgenbs/fido
```

- [ ] **Step 2: Find-and-replace all imports**

Run:
```bash
find . -name '*.go' -not -path './web/*' -exec sed -i '' 's|github.com/jorgenbs/fido|github.com/jorgenbs/fido|g' {} +
```

- [ ] **Step 3: Verify build**

```bash
go build -o fido .
go test ./...
```

Both should pass with no import errors.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor: migrate module path to github.com/jorgenbs/fido"
```

---

### Task 6: Version embedding + `fido version` command

**Files:**
- Create: `internal/version/version.go`
- Create: `cmd/version.go`
- Modify: `Makefile` (add ldflags to build target)

- [ ] **Step 1: Create version package**

```go
// internal/version/version.go
package version

// Version is set at build time via -ldflags.
var Version = "dev"
```

- [ ] **Step 2: Create version command**

```go
// cmd/version.go
package cmd

import (
	"fmt"

	"github.com/jorgenbs/fido/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Fido version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("fido %s\n", version.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
```

- [ ] **Step 3: Update Makefile to inject version**

Add ldflags to the `build` target:

```makefile
VERSION ?= dev

build: web
	go build -ldflags "-X github.com/jorgenbs/fido/internal/version.Version=$(VERSION)" -o fido .
```

- [ ] **Step 4: Build and verify**

```bash
make build VERSION=test-123
./fido version
# Expected: fido test-123

make build
./fido version
# Expected: fido dev
```

- [ ] **Step 5: Commit**

```bash
git add internal/version/version.go cmd/version.go Makefile
git commit -m "feat: add version embedding and fido version command"
```

---

### Task 7: `fido upgrade` command

**Files:**
- Create: `cmd/upgrade.go`

- [ ] **Step 1: Create `cmd/upgrade.go`**

```go
// cmd/upgrade.go
package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/jorgenbs/fido/internal/version"
	"github.com/spf13/cobra"
)

const repoAPI = "https://api.github.com/repos/jorgenbs/fido/releases/latest"

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Fido to the latest release",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Checking for updates...")

		resp, err := http.Get(repoAPI)
		if err != nil {
			return fmt.Errorf("checking latest release: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
		}

		var release ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return fmt.Errorf("parsing release: %w", err)
		}

		latest := strings.TrimPrefix(release.TagName, "v")
		current := strings.TrimPrefix(version.Version, "v")

		if current == latest {
			fmt.Printf("Already up to date (%s)\n", version.Version)
			return nil
		}

		// Find matching asset
		wantSuffix := fmt.Sprintf("_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
		var downloadURL string
		for _, a := range release.Assets {
			if strings.HasSuffix(a.Name, wantSuffix) {
				downloadURL = a.BrowserDownloadURL
				break
			}
		}
		if downloadURL == "" {
			return fmt.Errorf("no release asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
		}

		fmt.Printf("Downloading %s -> %s...\n", version.Version, release.TagName)

		// Download and extract
		dlResp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("downloading release: %w", err)
		}
		defer dlResp.Body.Close()

		tmpFile, err := extractBinaryFromTarGz(dlResp.Body)
		if err != nil {
			return fmt.Errorf("extracting binary: %w", err)
		}
		defer os.Remove(tmpFile)

		// Replace current binary
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding current executable: %w", err)
		}

		if err := replaceBinary(exe, tmpFile); err != nil {
			if os.IsPermission(err) {
				return fmt.Errorf("permission denied — try: sudo fido upgrade")
			}
			return fmt.Errorf("replacing binary: %w", err)
		}

		fmt.Printf("Upgraded: %s -> %s\n", version.Version, release.TagName)
		return nil
	},
}

func extractBinaryFromTarGz(r io.Reader) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag == tar.TypeReg && (hdr.Name == "fido" || strings.HasSuffix(hdr.Name, "/fido")) {
			tmp, err := os.CreateTemp("", "fido-upgrade-*")
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(tmp, tr); err != nil {
				tmp.Close()
				os.Remove(tmp.Name())
				return "", err
			}
			tmp.Close()
			os.Chmod(tmp.Name(), 0755)
			return tmp.Name(), nil
		}
	}
	return "", fmt.Errorf("fido binary not found in archive")
}

func replaceBinary(dst, src string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Write to a temp file next to the target, then rename (atomic on same filesystem)
	tmpPath := dst + ".new"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, srcFile); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	out.Close()

	return os.Rename(tmpPath, dst)
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
```

- [ ] **Step 2: Build and smoke test**

```bash
go build -o fido .
./fido upgrade
# Expected: "Checking for updates..." then either "Already up to date" or
# GitHub API 404 (no releases yet) — both are correct behavior at this stage
```

- [ ] **Step 3: Commit**

```bash
git add cmd/upgrade.go
git commit -m "feat: add fido upgrade command for self-updating"
```

---

### Task 8: GoReleaser config

**Files:**
- Create: `.goreleaser.yml`

- [ ] **Step 1: Create `.goreleaser.yml`**

The release workflow builds the frontend before invoking GoReleaser, so we skip the `before.hooks` here to avoid a redundant build. For local `goreleaser` runs, run `make web` first.

```yaml
version: 2

builds:
  - main: .
    binary: fido
    ldflags:
      - -X github.com/jorgenbs/fido/internal/version.Version={{.Version}}
    goos:
      - darwin
      - linux
    goarch:
      - amd64
      - arm64

archives:
  - format: tar.gz
    name_template: "fido_{{.Version}}_{{.Os}}_{{.Arch}}"

checksum:
  name_template: "checksums.txt"
  algorithm: sha256

release:
  github:
    owner: jorgenbs
    name: fido
```

- [ ] **Step 2: Validate config (if goreleaser is installed)**

```bash
goreleaser check 2>/dev/null || echo "goreleaser not installed — skipping local check"
```

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yml
git commit -m "chore: add GoReleaser config for cross-platform builds"
```

---

### Task 9: GitHub Actions workflows

**Files:**
- Create: `.github/workflows/release.yml`
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create `.github/workflows/release.yml`**

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: 22

      - name: Install frontend dependencies
        run: cd web && npm ci

      - name: Build frontend
        run: cd web && npm run build

      - uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 2: Create `.github/workflows/ci.yml`**

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build-and-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: 22

      - name: Install frontend dependencies
        run: cd web && npm ci

      - name: Build frontend
        run: cd web && npm run build

      - name: Run tests
        run: go test ./...
```

- [ ] **Step 3: Commit**

```bash
mkdir -p .github/workflows
git add .github/workflows/release.yml .github/workflows/ci.yml
git commit -m "ci: add GitHub Actions for CI and tagged releases"
```

---

### Task 10: Install script

**Files:**
- Create: `install.sh`

- [ ] **Step 1: Create `install.sh`**

```bash
#!/bin/sh
set -e

REPO="jorgenbs/fido"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin) ;;
  linux) ;;
  *)
    echo "Unsupported OS: $OS" >&2
    exit 1
    ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

# Fetch latest release tag
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
VERSION="${TAG#v}"

if [ -z "$VERSION" ]; then
  echo "Failed to determine latest version" >&2
  exit 1
fi

# Download
URL="https://github.com/${REPO}/releases/download/${TAG}/fido_${VERSION}_${OS}_${ARCH}.tar.gz"
echo "Downloading fido ${VERSION} for ${OS}/${ARCH}..."

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

curl -fsSL "$URL" | tar xz -C "$TMPDIR"

# Install
INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

mv "${TMPDIR}/fido" "${INSTALL_DIR}/fido"
chmod +x "${INSTALL_DIR}/fido"

echo "Installed fido ${VERSION} to ${INSTALL_DIR}/fido"
```

- [ ] **Step 2: Make executable and commit**

```bash
chmod +x install.sh
git add install.sh
git commit -m "feat: add install.sh for curl-pipe-sh installation"
```

---

### Task 11: Create GitHub repo and push

**Files:** None (git operations only)

- [ ] **Step 1: Create the GitHub repo**

```bash
gh repo create jorgenbs/fido --public --description "Error triage sidecar: Datadog → investigate → fix → draft MR" --source .
```

- [ ] **Step 2: Push all branches and tags**

```bash
git push -u origin main
git push origin --tags
```

- [ ] **Step 3: Verify the repo is live**

```bash
gh repo view jorgenbs/fido --web
```

- [ ] **Step 4: Tag a new release to trigger the first build**

```bash
git tag -a v3.1.0 -m "v3.1.0: distribution and daemon management"
git push origin v3.1.0
```

Wait for the GitHub Actions release workflow to complete, then verify:
```bash
gh release view v3.1.0 --repo jorgenbs/fido
```

Confirm the release has 4 `.tar.gz` assets (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64) and a `checksums.txt`.
