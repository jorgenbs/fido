# Distribution & Daemon Management Design

**Date:** 2026-04-16
**Status:** Approved

## Overview

Make Fido installable without cloning the repo. Ship pre-built binaries via GitHub Releases, provide a `curl | sh` installer and a self-upgrade command. As a pre-requisite, add daemon management so `fido` (bare) starts everything in the background.

## Pre-requisite: Daemon Management

### `fido` (no subcommand)

Running `fido` with no subcommand starts the server and sync engine as a background process:

- Forks `fido serve` as a detached subprocess (stdout/stderr redirected to `~/.fido/fido.log`)
- Writes PID to `~/.fido/fido.pid`
- Writes port to `~/.fido/fido.port`
- Polls briefly to confirm the process started (check PID is alive)
- Prints startup status:
  ```
  Starting daemon... done
  Starting dashboard on :8080... done
  ```
- If PID file exists and process is running, prints "Fido is already running (PID <pid>)" and exits

### `fido status`

- Reads `~/.fido/fido.pid`, checks if process is alive
- Prints: running/stopped, PID, port
- If stopped but stale PID file exists, cleans it up

### `fido stop`

- Reads `~/.fido/fido.pid`, sends SIGTERM
- Waits for process to exit (with timeout)
- Removes PID and port files
- Prints confirmation

### `fido serve`

Unchanged — foreground mode. Used for development and as the fork target for the background command.

## Distribution

### 1. Module path migration

- Change `go.mod` from `github.com/ruter-as/fido` to `github.com/jorgenbs/fido`
- Update all internal imports across the codebase
- Create repo on GitHub as `jorgenbs/fido` (public)
- Add `origin` remote

### 2. Version embedding

- New file: `internal/version/version.go`
  ```go
  package version
  var Version = "dev"
  ```
- Set via `-ldflags "-X github.com/jorgenbs/fido/internal/version.Version=<tag>"` at build time
- GoReleaser handles this automatically from the git tag

### 3. `fido version` command

- New `cmd/version.go` — prints the embedded version string

### 4. GoReleaser

`.goreleaser.yml` at repo root:

- **Frontend:** built by the GitHub Actions workflow before GoReleaser runs (no `before.hooks` needed)
- **Targets:** `darwin/arm64`, `darwin/amd64`, `linux/arm64`, `linux/amd64`
- **Ldflags:** `-X github.com/jorgenbs/fido/internal/version.Version={{.Version}}`
- **Archives:** `fido_{{.Version}}_{{.Os}}_{{.Arch}}.tar.gz`
- **Checksums:** SHA256

### 5. GitHub Actions

**`.github/workflows/release.yml`** — triggered on `v*` tag push:

1. Checkout code
2. Set up Go + Node.js
3. `cd web && npm ci && npm run build`
4. Run GoReleaser (builds, packages, publishes to GitHub Releases)

**`.github/workflows/ci.yml`** — triggered on push/PR to main:

1. Checkout code
2. Set up Go + Node.js
3. `cd web && npm ci && npm run build`
4. `go test ./...`

### 6. Install script

`install.sh` in repo root. Usage:

```bash
curl -fsSL https://raw.githubusercontent.com/jorgenbs/fido/main/install.sh | sh
```

The script:

1. Detects OS (`darwin`/`linux`) and arch (`arm64`/`amd64`)
2. Fetches latest release tag from GitHub API (`/repos/jorgenbs/fido/releases/latest`)
3. Downloads the matching `.tar.gz` from GitHub Releases
4. Extracts the `fido` binary
5. Installs to `/usr/local/bin` (falls back to `~/.local/bin` if no write access)
6. Prints installed version and path

No checksum verification — keeps the script simple. Users who care can verify manually.

### 7. Self-upgrade command

`fido upgrade` (`cmd/upgrade.go`):

1. Calls GitHub Releases API for latest version
2. Compares against embedded `version.Version` — exits early if current
3. Downloads matching archive for current `runtime.GOOS`/`runtime.GOARCH`
4. Extracts binary to temp file
5. Replaces running binary at `os.Executable()` path
6. Prints old → new version
7. If binary location requires elevated permissions, prints clear error suggesting `sudo fido upgrade`

## Target platforms

| OS | Arch |
|---|---|
| darwin | arm64 |
| darwin | amd64 |
| linux | arm64 |
| linux | amd64 |

## Install methods

| Method | Command |
|---|---|
| Install script | `curl -fsSL https://raw.githubusercontent.com/jorgenbs/fido/main/install.sh \| sh` |
| Go install | `go install github.com/jorgenbs/fido@latest` |
| Self-upgrade | `fido upgrade` |

## File layout (new/changed files)

```
.goreleaser.yml
.github/workflows/release.yml
.github/workflows/ci.yml
install.sh
internal/version/version.go
cmd/version.go
cmd/upgrade.go
cmd/start.go          # sets rootCmd.RunE for bare `fido` (background fork)
cmd/status.go
cmd/stop.go
```
