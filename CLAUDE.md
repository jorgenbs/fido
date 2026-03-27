# Fido

Go backend + React/TypeScript frontend (Vite, shadcn/ui, Tailwind).

## Project

- Error triage sidecar: Datadog → scan → investigate → fix → draft GitLab MR
- Reports live in `~/.fido/reports/<issue-id>/` (error.md, meta.json, investigation.md, fix.md, resolve.json)
- Config: `~/.fido/config.yml` (see `config.example.yml`)

## Commands

- `go build -o fido .` — build
- `go test ./...` — run tests
- `cd web && npm run dev` — start frontend dev server (port 5174)
- `docker compose up` — full stack (API :8080, daemon, web :3000)

## Backend verification

- Be diligent of gitlab cli and datadog api usage - run a test before accepting the code.
- After implementing or changing any API endpoint, verify it against the **running server** with curl. Build the binary, restart the server, then curl the affected endpoint with a real issue ID and confirm the response contains the expected data — not just empty/stored values.

```bash
# Rebuild and restart
go build -o fido . && kill $(pgrep -f './fido serve') ; ./fido serve &

# Curl the endpoint (use a real issue ID from ~/.fido/reports/)
curl -s localhost:8080/api/issues/<id>/mr-status
curl -s localhost:8080/api/issues/<id>
curl -s localhost:8080/api/issues
```

Passing unit tests and TypeScript compilation are not sufficient — they do not catch integration issues like wrong CLI flags, stderr pollution, or config path mismatches.

## Frontend verification

Before committing frontend changes, verify with Playwright headless browser:

```bash
# Terminal 1: start dev server
cd web && npm run dev

# Terminal 2: run verification (exits 1 if React errors found)
cd web && node verify.mjs
```

`verify.mjs` checks Dashboard and IssueDetail for console errors, ignoring expected backend-unavailable errors. Fix all errors before committing.

## Conventions

- Use conventional commits (`feat:`, `fix:`, `docs:`, `refactor:`, etc.)
- Commit when finishing a task
- Use the superpower skills to their extent
