<p align="center">
  <img src="docs/fido-banner.png" alt="Fido — go fetch fido" />
</p>

# Fido

A developer sidecar that fetches errors from Datadog, investigates them in code, and proposes fixes as draft GitLab MRs — all powered by AI.

## The Three Phases

Fido processes errors through a pipeline of three independent phases. Each phase produces a markdown report in `~/.fido/reports/<issue-id>/`. You can intervene, skip, or re-run any phase at any time.

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
   fido scan          → error.md + CI status refresh
        │
        ▼
   fido investigate   → investigation.md
        │
        ▼
   fido fix           → fix.md + draft MR
        │
        ▼ (if CI fails)
   fido fix --iterate → fix-2.md (pushes to existing branch)
```

## Getting Started

### Prerequisites

- Datadog Personal Access Token with `error_tracking_read` and `logs_read_data` scopes
- `glab` CLI authenticated (for MR creation)
- An AI agent CLI (default: `claude`)

### Install & Configure

```bash
# Build from source
go build -o fido .

# Create config
mkdir -p ~/.fido
cp config.example.yml ~/.fido/config.yml
```

Edit `~/.fido/config.yml` with your Datadog token, services, and repository paths. See `config.example.yml` for a fully documented template.

## CLI

```bash
# Scan Datadog for new errors
fido scan [--service <name>] [--since <duration>]

# Investigate an error (runs AI agent)
fido investigate <issue-id> [--service <name>]

# Fix an error (creates draft MR)
fido fix <issue-id> [--service <name>]

# Iterate on a fix when CI is failing (pushes to existing branch, no new MR)
fido fix <issue-id> --iterate [--service <name>]

# List tracked issues
fido list [--status scanned|investigated|fixed]

# Show all reports for an issue
fido show <issue-id>

# Run scan on a recurring interval
fido daemon [--interval <duration>]

# Start the API server (for the web dashboard)
fido serve [--port <port>]
```

### Typical workflow

```bash
fido scan                        # pull latest errors
fido list --status scanned       # see what's new
fido investigate <issue-id>      # AI root-cause analysis
fido show <issue-id>             # review the investigation
fido fix <issue-id>              # AI implements fix + draft MR
fido fix <issue-id> --iterate   # re-fix if CI is failing (uses CI logs as context)
```

## Web Dashboard

The web dashboard gives your team a shared view of all tracked errors and lets anyone trigger investigations or fixes without touching the CLI.

Start the full stack with Docker Compose:

```bash
docker compose up
```

| Service | Port | Description |
|---------|------|-------------|
| `fido` | 8080 | HTTP API |
| `fido-daemon` | — | Background scanner |
| `fido-web` | 3000 | Web dashboard |

Open **http://localhost:3000** to access the dashboard.

### Dashboard features

- **Issue list** — all tracked errors with stage indicators (scanned → investigated → fixed), service name, occurrence count, and last-seen timestamp
- **Filter by stage** — focus on what needs attention
- **Scan Now** — trigger a fresh Datadog scan from the UI
- **Issue detail view** — read the error report, investigation, and fix side-by-side
- **One-click investigate / fix** — kick off AI phases with progress streaming
- **MR links** — jump straight to the draft Merge Request when a fix is ready
- **CI status** — pipeline status badge (passed/failed/running/pending) shown for all fixed issues, updated on every scan
- **Re-fix (CI failing)** — when CI is red, trigger a new fix iteration directly from the UI; the agent receives the CI failure logs and previous fix as context
- **Ignore / unignore** — dismiss noisy issues without losing them

## Configuration

All configuration lives in `~/.fido/config.yml`.

| Field | Description | Default |
|-------|-------------|---------|
| `datadog.token` | Datadog PAT | (required) |
| `datadog.site` | Datadog site domain (e.g. `datadoghq.eu`) | `datadoghq.eu` |
| `datadog.services` | Service names to monitor | `[]` |
| `scan.interval` | Daemon poll interval | `15m` |
| `scan.since` | How far back to look | `24h` |
| `repositories.<name>.local` | Local path to repo | |
| `repositories.<name>.git` | Git clone URL | |
| `agent.investigate` | Agent command for investigation | `claude -p` |
| `agent.fix` | Agent command for fixes | `claude -p` |

> **Note:** The `site` field is the Datadog site domain, not your org subdomain. If your URL is `https://myorg.datadoghq.eu`, set `site: "datadoghq.eu"`.
