<p align="center">
  <img src="docs/fido-banner.png" alt="Fido — go fetch fido" />
</p>

# Fido

An AI powered developer workflow for importing errors from Datadog, investigates them in code, and proposes fixes as draft GitLab MRs.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/jorgenbs/fido/main/install.sh | sh
```

Or with Go:

```bash
go install github.com/jorgenbs/fido@latest
```

To update an existing installation:

```bash
fido upgrade
```

## Quick Start

```bash
# Create config
mkdir -p ~/.fido
cp config.example.yml ~/.fido/config.yml
# Edit ~/.fido/config.yml with your Datadog token, services, and repository paths

# Start Fido (dashboard + background sync)
fido
```

Open **http://localhost:8080** to access the dashboard.

### Prerequisites

- Datadog Personal Access Token with `error_tracking_read`, `apm_read`, and `logs_read_data` scopes
- `glab` CLI authenticated (for MR creation)
- An AI agent CLI (default: `claude`)

## How It Works

Fido processes errors through a pipeline of three phases. Each phase produces a markdown report in `~/.fido/reports/<issue-id>/`. You can intervene, skip, or re-run any phase at any time.

### Phase 1: Scan

Fido queries Datadog Error Tracking for new unresolved errors across your monitored services. For each new error it stores a report with the title, stack trace, timestamps, occurrence count, and links back to Datadog.

### Phase 2: Investigate

An AI agent (Claude by default) receives the error report together with your repository and produces a root-cause analysis: affected files, suggested approach, confidence level, and relevant Datadog trace links.

### Phase 3: Fix

The AI agent takes both the error and investigation reports, implements the fix on a feature branch, and opens a **draft GitLab Merge Request** for your team to review.

```
Datadog Error Tracking
        │
        ▼
   fido scan          → error.md + stacktrace enrichment
        │
        ▼
   fido investigate   → investigation.md
        │
        ▼
   fido fix           → fix.md + draft MR
        │
        ▼ (if CI fails)
   fido fix --iterate → re-fix using CI failure logs
```

## CLI

### Daemon

```bash
fido                  # start dashboard + background sync (daemonized)
fido status           # check if the daemon is running
fido stop             # stop the daemon
fido serve [--port]   # run in foreground (for development)
```

### Error Pipeline

```bash
# Scan Datadog for new errors
fido scan [--service <name>...] [--since <duration>] [--limit <n>]

# Import a specific Datadog issue by ID
fido import <issue-id>

# Investigate an error (runs AI agent)
fido investigate <issue-id> [--service <name>]

# Fix an error (creates draft MR)
fido fix <issue-id> [--service <name>]

# Iterate on a fix when CI is failing
fido fix <issue-id> --iterate [--service <name>]
```

### Browsing

```bash
# List tracked issues
fido list [--status scanned|investigated|fixed] [--service <name>]

# Show all reports for an issue
fido show <issue-id>
```

### Utilities

```bash
fido version          # print version
fido upgrade          # self-update to latest release
```

### Typical workflow

```bash
fido                             # start the daemon
fido scan                        # pull latest errors (also runs automatically)
fido list --status scanned       # see what's new
fido investigate <issue-id>      # AI root-cause analysis
fido show <issue-id>             # review the investigation
fido fix <issue-id>              # AI implements fix + draft MR
fido fix <issue-id> --iterate    # re-fix if CI is failing
```

## Web Dashboard

The dashboard gives your team a shared view of all tracked errors and lets anyone trigger investigations or fixes without touching the CLI.

- **Issue list** — all tracked errors with stage indicators (scanned → investigated → fixed), service name, occurrence count, and last-seen timestamp
- **Filter by stage** — focus on what needs attention
- **Scan Now** — trigger a fresh Datadog scan from the UI
- **Issue detail view** — read the error report, investigation, and fix side-by-side
- **One-click investigate / fix** — kick off AI phases with progress streaming
- **Datadog status** — live issue status from Datadog (open/resolved/ignored/regressed)
- **MR links** — jump straight to the draft Merge Request when a fix is ready
- **CI status** — pipeline status badge (passed/failed/running/pending) for fixed issues
- **Re-fix (CI failing)** — trigger a new fix iteration directly from the UI; the agent receives CI failure logs and previous fix as context
- **Ignore / unignore** — dismiss noisy issues without losing them

## Configuration

All configuration lives in `~/.fido/config.yml`. See `config.example.yml` for a fully documented template.

| Field | Description | Default |
|-------|-------------|---------|
| `datadog.token` | Datadog PAT | (required) |
| `datadog.site` | Datadog site domain (e.g. `datadoghq.eu`) | `datadoghq.eu` |
| `datadog.services` | Service names to monitor | `[]` |
| `scan.interval` | Daemon poll interval | `15m` |
| `scan.since` | How far back to look | `24h` |
| `scan.rate_limit` | Max Datadog API requests/minute | `30` |
| `scan.observation_window` | How long to watch resolved issues for regressions | `24h` |
| `repositories.<name>.local` | Local path to repo | |
| `repositories.<name>.git` | Git clone URL | |
| `agent.investigate` | Agent command for investigation | `claude -p` |
| `agent.fix` | Agent command for fixes | `claude -p` |

> **Note:** The `site` field is the Datadog site domain, not your org subdomain. If your URL is `https://myorg.datadoghq.eu`, set `site: "datadoghq.eu"`.
