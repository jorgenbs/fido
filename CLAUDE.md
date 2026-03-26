# Fido

Go backend + React/TypeScript frontend (Vite, shadcn/ui, Tailwind).

## Project

- Error triage sidecar: Datadog → scan → investigate → fix → draft GitLab MR
- Reports live in `~/.fido/reports/<issue-id>/` (error.md, meta.json, investigation.md, fix.md, resolve.json)
- Config: `~/.fido/config.yml` (see `config.example.yml`)

## Commands

- `go build -o fido .` — build
- `go test ./...` — run tests
- `cd web && npm run dev` — start frontend dev server
- `docker compose up` — full stack (API :8080, daemon, web :3000)

## Conventions

- Use conventional commits (`feat:`, `fix:`, `docs:`, `refactor:`, etc.)
- Commit when finishing a task
- Use the superpower skills to their extent
