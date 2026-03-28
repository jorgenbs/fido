# Single Binary Shipping + SSE Notifications + Dashboard Improvements

**Date:** 2026-03-28
**Status:** Approved

## Motivation

Fido currently ships as three Docker Compose services (API, daemon, web). This poses issues for agent access and adds unnecessary deployment complexity for what is a dev sidecar tool. The web dashboard also lacks reactivity — state changes (e.g., investigating → investigated) require manual re-navigation to pick up.

This spec covers:
1. Collapsing to a single binary
2. Real-time updates via SSE event bus
3. Browser notifications on key events
4. Dashboard expanded row improvements

---

## 1. Single Binary Architecture

### Collapse three processes into `fido serve`

- **API server** — unchanged, serves on configured port (default 8080).
- **Daemon scanner** — becomes an internal goroutine ticker inside `serve`, controlled by existing `scan.interval` config. The separate `daemon` command is removed.
- **Embedded frontend** — `//go:embed web/dist` serves the built React app at `/`. API routes remain at `/api/*`. SPA catch-all returns `index.html` for non-API, non-asset routes.

### API client change

Drop `VITE_API_URL` environment variable. The frontend uses relative `/api/...` paths since it is served from the same origin.

### Development workflow

- `fido serve` — runs backend with embedded frontend (if built) + daemon ticker.
- `cd web && npm run dev` — Vite dev server for frontend hot-reload, proxies `/api` to `localhost:8080`.
- Docker Compose is removed.

### Running Fido

```bash
# Build frontend + backend
cd web && npm run build && cd ..
go build -o fido .

# Run everything
./fido serve
```

---

## 2. Global SSE Event Bus

### New endpoint: `GET /api/events`

A single persistent SSE connection per browser tab. The backend publishes events whenever state changes.

### Event types

| Event | Payload | When |
|-------|---------|------|
| `scan:complete` | `{ count, issueIds }` | Scan finishes, new issues found |
| `issue:updated` | `{ id, field, oldValue, newValue }` | Stage transition, CI status change, ignore/unignore |
| `issue:progress` | `{ id, action, status }` | Investigation/fix started or completed |

### Backend implementation

- Central in-process event hub (pub/sub pattern). A `Hub` struct holds a set of subscriber channels. Goroutines that run scan/investigate/fix publish events to the hub. The SSE handler registers a subscriber on connect and fans out events.
- The existing per-issue `GET /api/issues/{id}/progress` endpoint is kept for detail page log streaming. The global stream carries lightweight state-change notifications only.
- `EventSource` handles client reconnection natively (auto-reconnects).

### Frontend subscription

- Dashboard subscribes on mount, unsubscribes on unmount.
- On `issue:updated` — refetch that single issue via `GET /api/issues/{id}` and merge into state (no full list reload).
- On `scan:complete` — refetch full issue list (new issues appeared).
- On `issue:progress` with `status: "started"` — show inline spinner on the affected row.
- On `issue:progress` with `status: "complete"` — the subsequent `issue:updated` event triggers the data refresh.

---

## 3. Browser Notifications

### Permission

On first visit, show a small non-intrusive banner asking for notification permission. Not a browser popup on page load.

### Events that trigger notifications

| Event | Notification |
|-------|-------------|
| New issues from scan | Yes — "{count} new issues discovered" |
| investigating → investigated | Yes — "{title}: investigation complete" |
| investigated → fixed | Yes — "{title}: fix applied" |
| CI pipeline pass | Yes — "{title}: CI passed" |
| CI pipeline fail | Yes — "{title}: CI failed" |
| Fix iteration needed | Yes — "{title}: CI failed, iteration needed" |
| Ignore/unignore | No |
| Other silent updates | No |

### Behavior

- Clicking a notification opens/focuses the Fido tab and navigates to the issue detail page.
- Notifications only fire when the tab is not focused (no point notifying about something you're looking at).
- Respects browser notification permission — if denied, falls back to UI-only updates.

---

## 4. UI Highlights for Silent Updates

When an SSE event updates an issue in the dashboard:

- **Row highlight** — the changed row gets a brief background flash animation (subtle, fades over ~2 seconds).
- **Cell highlight** — specifically changed cells (stage badge, CI status) get a color pulse or brief underline so the user can scan the table and see what moved.
- Highlights clear after timeout. No persistent "unread" state.

---

## 5. Expanded Row Improvements

### Current state

Expanded row shows last seen, occurrence count, and buttons that navigate to the detail page.

### New content

- **Datadog issue URL** — clickable link, sourced from `meta.json`.
- **Service + environment** — from `meta.json`.
- **Stack trace preview** — first 15 lines from `error.md`, rendered in a monospace code block. "Show more" button expands to full trace inline.

### Inline actions

- **Investigate** button (stage: `scanned`) — triggers `POST /api/issues/{id}/investigate` directly. Shows inline spinner + "Investigating..." status text.
- **Fix** button (stage: `investigated`) — triggers `POST /api/issues/{id}/fix` directly. Shows inline spinner + "Fixing..." status text.
- **Ignore/Unignore** — already works inline, unchanged.

When an action completes, the SSE event bus updates the row automatically — stage badge changes, highlight animation fires. For full progress logs, the user clicks through to the detail page.

---

## API Changes Summary

| Change | Type |
|--------|------|
| `GET /api/events` | New — global SSE event stream |
| `GET /api/issues/{id}` | Modified — response includes `stack_trace` and `datadog_url` fields |
| `POST /api/scan` | Modified — publishes `scan:complete` event |
| `POST /api/issues/{id}/investigate` | Modified — publishes `issue:progress` events |
| `POST /api/issues/{id}/fix` | Modified — publishes `issue:progress` events |
| `POST /api/issues/{id}/ignore` | Modified — publishes `issue:updated` event |
| `POST /api/issues/{id}/unignore` | Modified — publishes `issue:updated` event |
| `fido daemon` command | Removed — merged into `fido serve` |

---

## Out of Scope

- WebSocket support (SSE is sufficient for unidirectional push)
- Persistent notification preferences / mute settings
- Multi-user awareness (who triggered what)
- Mobile / PWA push notifications
