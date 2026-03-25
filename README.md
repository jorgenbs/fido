# Fido

A developer sidecar that fetches errors from Datadog, investigates them in code, and proposes fixes as draft GitLab MRs.

## How it works

```
Datadog Error Tracking
        |
        v
   fido scan          -> error.md
        |
        v
   fido investigate   -> investigation.md
        |
        v
   fido fix           -> fix.md + draft MR
```

Each stage produces a markdown report in `~/.fido/reports/<issue-id>/`. Stages are independent — you can intervene, skip, or run any step manually.

## Prerequisites

- Go 1.22+
- Node.js 20+ (for web UI)
- Datadog API key and Application key
- `glab` CLI authenticated (for MR creation via the fix agent)
- An AI agent CLI (default: `claude`)

## Setup

```bash
# Build
go build -o fido .

# Create config
mkdir -p ~/.fido
cp config.example.yml ~/.fido/config.yml
# Edit ~/.fido/config.yml with your Datadog keys, services, and repo paths
```

## Configuration

All configuration lives in `~/.fido/config.yml`. See `config.example.yml` for a documented template.

| Field | Description | Default |
|-------|-------------|---------|
| `datadog.api_key` | Datadog API key | (required) |
| `datadog.app_key` | Datadog Application key | (required) |
| `datadog.site` | Datadog site | `datadoghq.eu` |
| `datadog.services` | Service names to monitor | `[]` |
| `scan.interval` | Daemon poll interval | `15m` |
| `scan.since` | How far back to look | `24h` |
| `repositories.<name>.local` | Local path to repo | |
| `repositories.<name>.git` | Git clone URL | |
| `agent.investigate` | Agent command for investigation | `claude -p` |
| `agent.fix` | Agent command for fixes | `claude -p` |

No environment variables are needed — everything is configured via the YAML file.

## CLI Usage

```bash
# Scan Datadog for new errors
fido scan [--service <name>] [--since <duration>]

# Investigate an error (runs AI agent)
fido investigate <issue-id> [--service <name>] [--agent <command>]

# Fix an error (runs AI agent, creates draft MR)
fido fix <issue-id> [--service <name>] [--agent <command>]

# List all tracked issues
fido list [--status scanned|investigated|fixed]

# Show reports for an issue
fido show <issue-id>

# Run scan on a recurring interval
fido daemon [--interval <duration>]

# Start the HTTP API server (for web UI)
fido serve [--port <port>]
```

## Docker Compose

Run the full stack (API server + daemon + web UI):

```bash
# Make sure ~/.fido/config.yml exists first
docker compose up
```

| Service | Port | Description |
|---------|------|-------------|
| `fido` | 8080 | HTTP API server |
| `fido-daemon` | — | Background scanner |
| `fido-web` | 3000 | Web dashboard |

Open `http://localhost:3000` for the web UI.

## Development

```bash
# Run tests
go test ./...

# Run smoke test
./scripts/smoke-test.sh

# Build web UI
cd web && npm run build
```
