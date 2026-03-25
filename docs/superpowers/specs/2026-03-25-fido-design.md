# Fido — Design Spec

> A developer sidecar that fetches errors from Datadog, investigates them in code, and proposes fixes as draft GitLab MRs.

## Overview

Fido is a CLI tool and local development environment that automates the pipeline from production error discovery to code fix proposal. It is designed as a modular, markdown-driven pipeline where each stage produces a standalone report that any agent or human can consume.

Fido is **not** a fully automated solution. It is a developer tool that surfaces errors, provides AI-assisted investigation, and drafts fixes for human review.

## Architecture

### Components

1. **`fido` CLI** (Go) — the core binary. Handles Datadog API interaction, report management, agent orchestration, HTTP API, and daemon mode.
2. **Fido Web** (TypeScript) — a thin presentation layer. Reads reports via the Go HTTP API and provides a browser-based dashboard.

### Deployment

Local deployment via `docker-compose`:

```
docker-compose.yml
├── fido          (Go: CLI + HTTP API + daemon/cron)
├── fido-web      (TypeScript: serves UI, talks to fido API)
└── volumes:
    └── ~/.fido/  (shared reports directory)
```

The Go binary serves multiple roles via subcommands — CLI, HTTP API server (`fido serve`), and daemon (`fido daemon`). The CLI also works standalone outside Docker.

## Data Pipeline

### Stages

```
Datadog Error Tracking
        │
        ▼
   fido scan          → error.md
        │
        ▼
   fido investigate   → investigation.md
        │
        ▼
   fido fix           → fix.md + GitLab draft MR
```

Each stage produces a markdown report. The presence of a file indicates stage completion. Stages are independent — a developer can intervene at any point, skip stages, or manually author reports.

### Report Structure

```
~/.fido/
├── config.yml
└── reports/
    └── <dd-issue-id>/
        ├── error.md
        ├── investigation.md
        ├── fix.md
        └── resolve.json
```

### Report Contents

**`error.md`** (from `fido scan`):
- Error type, message, fingerprint
- Stack trace (from Datadog Error Tracking)
- Occurrence count, first/last seen
- Service name, environment
- Surrounding logs (fetched by trace ID)
- Link to Datadog issue

**`investigation.md`** (from `fido investigate`):
- Resolved repository (from config mapping)
- Root cause analysis (agent output)
- Affected files and code paths
- Suggested fix approach
- Confidence level / complexity estimate

**`fix.md`** (from `fido fix`):
- Summary of changes made
- Reasoning behind the approach
- Files changed (diff summary)
- Test results if applicable

**`resolve.json`** (written by Fido after `fido fix` completes):
Structured metadata for the web UI and downstream tooling:
```json
{
  "branch": "fix/abc123-null-pointer-in-handler",
  "mr_url": "https://gitlab.com/ruter-as/.../merge_requests/42",
  "mr_status": "draft",
  "service": "drt-services",
  "datadog_issue_id": "abc123",
  "datadog_url": "https://app.datadoghq.eu/...",
  "created_at": "2026-03-25T10:30:00Z"
}
```
This file is written by Fido core (not the agent) by parsing the agent's output and `glab` results. It serves as the machine-readable record of the fix, keeping `fix.md` as a pure agent narrative.

## CLI Commands

```
fido scan [--service <name>] [--since <duration>] [--limit <n>]
```
Polls Datadog Error Tracking API. Filters by service or configured defaults. Writes `error.md` for each new issue. Skips issues that already have a report.

```
fido investigate <issue-id> [--agent <command>]
```
Reads `error.md`, resolves the repo from config, clones it (or uses local path), and passes context to an agent. Writes `investigation.md`.

```
fido fix <issue-id> [--agent <command>]
```
Reads `investigation.md`, passes it to an agent with repo context. Agent creates branch, makes changes, pushes, creates draft MR. Writes `fix.md`.

```
fido list [--status scan|investigated|fixed] [--service <name>]
```
Lists known issues and their pipeline stage based on which report files exist.

```
fido show <issue-id>
```
Prints the reports for a given issue to stdout.

```
fido daemon [--interval <duration>]
```
Runs `fido scan` on a recurring interval. Used by the docker-compose setup.

```
fido serve [--port <port>]
```
Starts the HTTP API server for the web UI.

```
fido config
```
Interactive setup for Datadog keys, service filters, repo mappings, and agent commands.

## Configuration

```yaml
# ~/.fido/config.yml
datadog:
  api_key: "..."
  app_key: "..."
  site: "datadoghq.eu"
  services:
    - "ondemand-planning"
    - "drt-services"

scan:
  interval: "15m"
  since: "24h"

repositories:
  drt-services:
    local: "/path/to/drt-services"
  vehicle-position:
    git: "https://gitlab.com/ruter-as/systems/ondemand-planning/vehicle-position.git"

agent:
  investigate: "claude -p"
  fix: "claude -p"
```

### Repository Mapping

The `repositories` section maps Datadog service names to code locations — either a `local` filesystem path or a `git` URL (which Fido clones to a temp directory).

This is a simple, explicit mapping. Future enhancement: auto-populate from the `tet-organization` service catalog.

## Agent Integration

Fido is agent-agnostic. The `investigate` and `fix` stages shell out to a configurable command.

### Invocation Contract

Fido invokes agents using a simple contract:

1. Fido writes a **prompt file** (markdown) to a temp location containing all context the agent needs
2. Fido executes the agent command as: `<agent-command> <prompt-file-path> --cwd <repo-path>`
3. Fido captures the agent's **stdout** as the stage report (written to `investigation.md` or `fix.md`)

The agent command in `config.yml` is a command prefix. Fido appends the prompt file path as an argument and sets the **working directory of the subprocess** to the repo path (not passed as a CLI flag):

```bash
# Config: agent.investigate = "claude -p"
# Fido runs (cwd set to /path/to/repo):
cd /path/to/repo && claude -p /tmp/fido-prompt-abc123.md > ~/.fido/reports/abc123/investigation.md

# Config: agent.investigate = "python my_agent.py"
# Fido runs (cwd set to /path/to/repo):
cd /path/to/repo && python my_agent.py /tmp/fido-prompt-abc123.md > ~/.fido/reports/abc123/investigation.md
```

The repo path is also included in the prompt file for agents that need it explicitly.

The `--agent` CLI flag overrides the config and takes a full command prefix (same semantics).

### Prompt Templates

Fido uses built-in prompt templates for each stage. The prompt file includes:

**Investigate prompt:**
- The full `error.md` content
- Repo path
- Instructions: analyze the error, identify root cause, list affected files, suggest fix approach
- Output format specification (what `investigation.md` should contain)

**Fix prompt:**
- The full `error.md` and `investigation.md` content
- Repo path
- Instructions: create branch (`fix/<issue-id>-<desc>`), implement the fix, push, create draft MR via `glab`
- MR conventions: title as `fix(<service>): <description>`, draft, assigned to configured user
- Output format specification (what `fix.md` should contain)
- Prerequisites note: agent environment must have `git` and `glab` (authenticated) available

### Swappable Agents

The agent commands in `config.yml` can be any executable that accepts a prompt file path as an argument and writes its report to stdout:

```yaml
agent:
  investigate: "claude -p"        # Claude Code (default)
  fix: "claude -p"                # Claude Code (default)
  # investigate: "aider --message-file"  # Aider
  # fix: "python my_agent.py"            # Custom agent
```

Agents that don't follow the `<cmd> <file> --cwd <dir>` convention can be wrapped in a shell script.

## GitLab MR Creation

MR creation is handled by the fix agent, not Fido core. Fido provides instructions in the prompt:

- **Branch naming:** `fix/<issue-id>-<short-description>`
- **MR title:** `fix(fido): <short description>` (conventional commits)
- **MR description:** populated from `investigation.md` summary + Datadog link
- **Draft:** always true
- **Assignee:** the developer running Fido (from git config or `config.yml`)

The agent uses `glab` (GitLab CLI) to create the MR. Swapping to GitHub later means changing the prompt to use `gh` — no Go code changes needed.

## Web UI

### Views

- **Dashboard** — list of all issues, filterable by service and pipeline stage (scanned / investigated / fixed). Shows occurrence count, last seen, service name.
- **Issue Detail** — renders the markdown reports for an issue. Buttons to advance to the next stage ("Investigate this", "Fix this").
- **Settings** — edit `config.yml` (services, repos, scan interval).

### HTTP API

The Go binary exposes a REST API via `fido serve`:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/issues` | List all issues with stage status. Supports `?service=` and `?status=` filters |
| `GET` | `/api/issues/:id` | Get issue detail including all stage reports and resolve.json |
| `POST` | `/api/issues/:id/investigate` | Trigger `fido investigate` for an issue |
| `POST` | `/api/issues/:id/fix` | Trigger `fido fix` for an issue |
| `GET` | `/api/issues/:id/progress` | SSE stream of agent output for a running action |
| `GET` | `/api/config` | Get current configuration |
| `PUT` | `/api/config` | Update configuration |
| `POST` | `/api/scan` | Trigger an immediate scan |

**`GET /api/issues/:id` response shape:**

```json
{
  "id": "abc123",
  "stage": "fixed",
  "service": "drt-services",
  "error": "... error.md content ...",
  "investigation": "... investigation.md content or null ...",
  "fix": "... fix.md content or null ...",
  "resolve": {
    "branch": "fix/abc123-null-pointer-in-handler",
    "mr_url": "https://gitlab.com/ruter-as/.../merge_requests/42",
    "mr_status": "draft",
    "service": "drt-services",
    "datadog_issue_id": "abc123",
    "datadog_url": "https://app.datadoghq.eu/...",
    "created_at": "2026-03-25T10:30:00Z"
  }
}
```

The `stage` field is derived from which files exist: `"scanned"` (only error.md), `"investigated"` (+ investigation.md), `"fixed"` (+ fix.md + resolve.json). The `resolve` field is `null` unless the issue has reached the fixed stage.

Long-running actions (`investigate`, `fix`, `scan`) return immediately with `202 Accepted`. The client subscribes to `/api/issues/:id/progress` (Server-Sent Events) keyed by issue ID to stream agent output in real time. Only one action per issue can run at a time — a second request returns `409 Conflict`.

Error responses use standard HTTP status codes with a JSON body: `{ "error": "<message>" }`.

## Datadog Integration

### Primary Data Source: Error Tracking API

- `POST /api/v2/error-tracking/issues/search` — list grouped error issues
- `GET /api/v2/error-tracking/issues/{issue_id}` — get issue details

Error Tracking provides pre-grouped issues with deduplicated stack traces, occurrence counts, and lifecycle status (open/ignored/resolved). This eliminates the need for Fido to build its own deduplication logic.

### Supplementary: Logs Search API

- `POST /api/v2/logs/events/search` — pull surrounding logs by trace ID

Used to enrich `error.md` with contextual log lines around the error.

### Authentication

API key + Application key, passed via headers. Configured in `config.yml`.

### Polling

Datadog APIs are poll-based (no streaming). `fido daemon` polls on a configurable interval (default: 15 minutes). Rate limits (~300 req/hour) are sufficient for a developer sidecar.

## State Management

No database. State is derived from:

1. **Filesystem** — which report files exist determines pipeline stage
2. **Datadog Error Tracking** — issue lifecycle (open/ignored/resolved) is managed in Datadog

If a richer query/filtering layer is needed later, a lightweight database index can be added on top without changing the report structure.

## Future Enhancements

- **tet-organization integration** — auto-populate repo mappings from the service catalog
- **Database index** — SQLite/Postgres for richer querying in the web UI
- **GitHub support** — swap `glab` for `gh` in fix prompts
- **Webhook triggers** — Datadog Monitor webhooks for near-real-time scanning instead of polling
- **Multi-user** — shared Fido instance with per-user MR assignment

## Tech Stack

| Component | Technology |
|-----------|-----------|
| CLI / Core | Go |
| Datadog SDK | `github.com/DataDog/datadog-api-client-go` (v2) |
| Web UI | TypeScript |
| Deployment | Docker Compose |
| GitLab interaction | `glab` CLI (via agent) |
| Default AI agent | Claude Code (`claude` CLI) |
