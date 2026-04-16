# Single Binary + SSE Notifications + Dashboard Improvements

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse Fido into a single binary (API + daemon + embedded frontend), add a global SSE event bus for real-time dashboard updates, browser notifications, and improved expanded rows.

**Architecture:** The Go server embeds the built frontend via `go:embed` and serves it alongside the API. A central `Hub` pub/sub fans events to SSE clients on `GET /api/events`. The daemon ticker runs as a goroutine inside `serve`. The frontend subscribes to the event stream for live updates, highlights changed rows, and fires browser notifications on key events.

**Tech Stack:** Go (chi router, go:embed), React 19 (TypeScript, Vite, Tailwind, shadcn/ui), SSE (EventSource API), Notifications API.

---

### Task 1: SSE Event Hub (Backend)

**Files:**
- Create: `internal/api/hub.go`
- Test: `internal/api/hub_test.go`

- [ ] **Step 1: Write failing tests for Hub**

```go
// internal/api/hub_test.go
package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHub_SubscribeReceivesPublishedEvents(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	hub.Publish(Event{Type: "issue:updated", Payload: map[string]any{"id": "issue-1"}})

	select {
	case evt := <-ch:
		if evt.Type != "issue:updated" {
			t.Errorf("Type: got %q, want issue:updated", evt.Type)
		}
		p, _ := json.Marshal(evt.Payload)
		if string(p) == "" {
			t.Error("expected non-empty payload")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestHub_UnsubscribeStopsDelivery(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	hub.Unsubscribe(ch)

	hub.Publish(Event{Type: "test", Payload: nil})

	select {
	case <-ch:
		t.Fatal("should not receive after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestHub_MultipleSubscribers(t *testing.T) {
	hub := NewHub()
	ch1 := hub.Subscribe()
	ch2 := hub.Subscribe()
	defer hub.Unsubscribe(ch1)
	defer hub.Unsubscribe(ch2)

	hub.Publish(Event{Type: "scan:complete", Payload: map[string]any{"count": 3}})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Type != "scan:complete" {
				t.Errorf("Type: got %q, want scan:complete", evt.Type)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -run TestHub -v`
Expected: compilation error — `NewHub` and `Event` not defined

- [ ] **Step 3: Implement Hub**

```go
// internal/api/hub.go
package api

import "sync"

// Event is a server-sent event published through the Hub.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// Hub is an in-process pub/sub for SSE events.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan Event]struct{})}
}

func (h *Hub) Subscribe() <-chan Event {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch <-chan Event) {
	// We need the send-capable channel to delete from the map.
	sendCh := ch.(chan Event)
	h.mu.Lock()
	delete(h.subscribers, sendCh)
	h.mu.Unlock()
	close(sendCh)
}

func (h *Hub) Publish(evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- evt:
		default:
			// slow subscriber — drop event
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -run TestHub -v`
Expected: all 3 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/hub.go internal/api/hub_test.go
git commit -m "feat: add in-process SSE event hub"
```

---

### Task 2: Wire Hub into Handlers + SSE Endpoint

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`

- [ ] **Step 1: Write failing test for the SSE events endpoint**

Add to `internal/api/handlers_test.go`:

```go
func TestStreamEvents_ReceivesPublishedEvent(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	hub := NewHub()
	h := NewHandlers(mgr, nil)
	h.hub = hub

	// Start handler in a goroutine (it blocks on SSE)
	r := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	ctx, cancel := context.WithCancel(r.Context())
	r = r.WithContext(ctx)
	w := &flushRecorder{httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		h.StreamEvents(w, r)
		close(done)
	}()

	// Publish an event
	hub.Publish(Event{Type: "issue:updated", Payload: map[string]any{"id": "issue-1"}})

	// Give it time to write
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, `"type":"issue:updated"`) {
		t.Errorf("expected event in body, got: %s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -run TestStreamEvents -v`
Expected: compilation error — `StreamEvents` method and `hub` field not defined

- [ ] **Step 3: Add hub field to Handlers and StreamEvents method**

In `internal/api/handlers.go`, add `hub` field to the `Handlers` struct:

```go
type Handlers struct {
	reports       *reports.Manager
	config        *config.Config
	hub           *Hub
	scanFn        ScanFunc
	investigateFn InvestigateFunc
	fixFn         FixFunc
	running       sync.Map
	progressBufs  sync.Map
}
```

Add the `StreamEvents` handler method at the end of handlers.go (before the helper functions):

```go
func (h *Handlers) StreamEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ch := h.hub.Subscribe()
	defer h.hub.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case evt := <-ch:
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
```

- [ ] **Step 4: Add route in server.go**

In `internal/api/server.go`, inside the `r.Route("/api", ...)` block, add after the `r.Post("/scan", h.TriggerScan)` line:

```go
r.Get("/events", h.StreamEvents)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -run TestStreamEvents -v`
Expected: PASS

- [ ] **Step 6: Run all existing tests to confirm no regressions**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -v`
Expected: all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/server.go internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: add GET /api/events SSE endpoint wired to hub"
```

---

### Task 3: Publish Events from Handlers

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`

- [ ] **Step 1: Write failing tests for event publishing**

Add to `internal/api/handlers_test.go`:

```go
func TestTriggerIgnore_PublishesEvent(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})

	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	h := NewHandlers(mgr, nil)
	h.hub = hub

	r := httptest.NewRequest(http.MethodPost, "/api/issues/issue-1/ignore", nil)
	r = withURLParam(r, "id", "issue-1")
	w := httptest.NewRecorder()
	h.TriggerIgnore(w, r)

	select {
	case evt := <-ch:
		if evt.Type != "issue:updated" {
			t.Errorf("Type: got %q, want issue:updated", evt.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("no event published")
	}
}

func TestTriggerScan_PublishesEvent(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)

	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	h := NewHandlers(mgr, nil)
	h.hub = hub
	h.SetScanFunc(func() error { return nil })

	r := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	w := httptest.NewRecorder()
	h.TriggerScan(w, r)

	select {
	case evt := <-ch:
		if evt.Type != "scan:complete" {
			t.Errorf("Type: got %q, want scan:complete", evt.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no event published")
	}
}

func TestTriggerInvestigate_PublishesProgressEvents(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "error")

	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	h := NewHandlers(mgr, nil)
	h.hub = hub
	h.SetInvestigateFunc(func(id string, w io.Writer) error {
		return nil
	})

	r := httptest.NewRequest(http.MethodPost, "/api/issues/issue-1/investigate", nil)
	w := httptest.NewRecorder()
	h.TriggerInvestigate(w, withURLParam(r, "id", "issue-1"))

	var events []Event
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case evt := <-ch:
			events = append(events, evt)
			if evt.Type == "issue:progress" {
				p := evt.Payload.(map[string]any)
				if p["status"] == "complete" {
					goto done
				}
			}
		case <-timeout:
			goto done
		}
	}
done:
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (started + complete), got %d", len(events))
	}
	if events[0].Type != "issue:progress" {
		t.Errorf("first event: got %q, want issue:progress", events[0].Type)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -run "TestTriggerIgnore_Publishes|TestTriggerScan_Publishes|TestTriggerInvestigate_Publishes" -v`
Expected: FAIL — no events published yet

- [ ] **Step 3: Add event publishing to handlers**

Helper method — add to `internal/api/handlers.go`:

```go
func (h *Handlers) publish(evt Event) {
	if h.hub != nil {
		h.hub.Publish(evt)
	}
}
```

Modify `TriggerScan` — replace the goroutine:

```go
func (h *Handlers) TriggerScan(w http.ResponseWriter, r *http.Request) {
	if h.scanFn == nil {
		writeError(w, http.StatusNotImplemented, "scan not configured")
		return
	}
	go func() {
		h.scanFn()
		h.publish(Event{Type: "scan:complete", Payload: map[string]any{}})
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}
```

Modify `TriggerInvestigate` — replace the goroutine:

```go
	go func() {
		defer h.running.Delete(id)
		h.publish(Event{Type: "issue:progress", Payload: map[string]any{"id": id, "action": "investigate", "status": "started"}})
		if err := h.investigateFn(id, pbuf); err != nil {
			log.Printf("investigate %s failed: %v", id, err)
			h.running.Store(id+"_error", err.Error())
			h.publish(Event{Type: "issue:progress", Payload: map[string]any{"id": id, "action": "investigate", "status": "error"}})
			return
		}
		h.publish(Event{Type: "issue:progress", Payload: map[string]any{"id": id, "action": "investigate", "status": "complete"}})
		h.publish(Event{Type: "issue:updated", Payload: map[string]any{"id": id, "field": "stage", "newValue": "investigated"}})
	}()
```

Modify `TriggerFix` — replace the goroutine:

```go
	go func() {
		defer h.running.Delete(id)
		h.publish(Event{Type: "issue:progress", Payload: map[string]any{"id": id, "action": "fix", "status": "started"}})
		if err := h.fixFn(id, req.Iterate, pbuf); err != nil {
			log.Printf("fix %s failed: %v", id, err)
			h.running.Store(id+"_error", err.Error())
			h.publish(Event{Type: "issue:progress", Payload: map[string]any{"id": id, "action": "fix", "status": "error"}})
			return
		}
		h.publish(Event{Type: "issue:progress", Payload: map[string]any{"id": id, "action": "fix", "status": "complete"}})
		h.publish(Event{Type: "issue:updated", Payload: map[string]any{"id": id, "field": "stage", "newValue": "fixed"}})
	}()
```

Modify `TriggerIgnore` — add after `SetIgnored` succeeds:

```go
	h.publish(Event{Type: "issue:updated", Payload: map[string]any{"id": id, "field": "ignored", "newValue": true}})
```

Modify `TriggerUnignore` — add after `SetIgnored` succeeds:

```go
	h.publish(Event{Type: "issue:updated", Payload: map[string]any{"id": id, "field": "ignored", "newValue": false}})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -run "TestTriggerIgnore_Publishes|TestTriggerScan_Publishes|TestTriggerInvestigate_Publishes" -v`
Expected: all 3 PASS

- [ ] **Step 5: Run all handler tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: publish SSE events from scan/investigate/fix/ignore handlers"
```

---

### Task 4: Collapse Daemon into Serve

**Files:**
- Modify: `cmd/serve.go`
- Modify: `cmd/daemon.go` (remove the cobra command registration, keep `runDaemonLoop` for reuse)
- Modify: `cmd/daemon_test.go`

- [ ] **Step 1: Write failing test for serve with daemon ticker**

Add to `cmd/daemon_test.go`:

```go
func TestRunDaemonLoop_ImmediateFirstScan(t *testing.T) {
	callCount := 0
	scanFn := func() error {
		callCount++
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — should still get the first scan

	runDaemonLoop(ctx, time.Hour, scanFn)

	if callCount != 1 {
		t.Errorf("expected exactly 1 call (immediate scan), got %d", callCount)
	}
}
```

- [ ] **Step 2: Run test to verify it passes (existing function still works)**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./cmd/ -run TestRunDaemonLoop -v`
Expected: PASS (the existing `runDaemonLoop` already does an immediate scan)

- [ ] **Step 3: Integrate daemon ticker into serve command**

Modify `cmd/serve.go` — add imports and integrate the daemon loop. Replace the `RunE` function body:

```go
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetString("port")
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		ddClient, err := datadog.NewClient(
			cfg.Datadog.Token,
			cfg.Datadog.Site,
			cfg.Datadog.OrgSubdomain,
		)
		if err != nil {
			return err
		}
		ddClient.SetVerbose(verbose)

		hub := api.NewHub()

		server := api.NewServer(mgr, cfg, hub)
		handlers := api.GetHandlers(server)
		handlers.SetScanFunc(func() error {
			count, err := runScan(cfg, ddClient, mgr)
			if err != nil {
				return err
			}
			fmt.Printf("API scan complete: %d new issues\n", count)
			return nil
		})
		handlers.SetInvestigateFunc(func(issueID string, progress io.Writer) error {
			service := ""
			if meta, err := mgr.ReadMetadata(issueID); err == nil {
				service = meta.Service
			}
			if service == "" {
				errorContent, _ := mgr.ReadError(issueID)
				service = extractServiceFromReport(errorContent)
			}
			return runInvestigate(issueID, service, cfg, mgr, ddClient, progress)
		})
		handlers.SetFixFunc(func(issueID string, iterate bool, progress io.Writer) error {
			service := ""
			if meta, err := mgr.ReadMetadata(issueID); err == nil {
				service = meta.Service
			}
			if service == "" {
				errorContent, _ := mgr.ReadError(issueID)
				service = extractServiceFromReport(errorContent)
			}
			if iterate {
				return runFixIterate(issueID, service, cfg, mgr, progress)
			}
			return runFix(issueID, service, cfg, mgr, progress)
		})

		// Start daemon scanner in background
		intervalStr := cfg.Scan.Interval
		if intervalStr == "" {
			intervalStr = "15m"
		}
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return fmt.Errorf("invalid scan interval %q: %w", intervalStr, err)
		}

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		go func() {
			fmt.Printf("Background scanner started (interval: %s)\n", interval)
			scanFn := func() error {
				count, scanErr := runScan(cfg, ddClient, mgr)
				if scanErr != nil {
					fmt.Printf("Background scan error: %v\n", scanErr)
					return scanErr
				}
				fmt.Printf("Background scan complete: %d new issues\n", count)
				hub.Publish(api.Event{Type: "scan:complete", Payload: map[string]any{"count": count}})
				return nil
			}
			runDaemonLoop(ctx, interval, scanFn)
		}()

		fmt.Printf("Fido server listening on :%s\n", port)

		srv := &http.Server{Addr: ":" + port, Handler: server}
		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()
		return srv.ListenAndServe()
	},
}
```

Add required imports to serve.go:

```go
import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jorgenbs/fido/internal/api"
	"github.com/jorgenbs/fido/internal/datadog"
	"github.com/jorgenbs/fido/internal/reports"
	"github.com/spf13/cobra"
)
```

- [ ] **Step 4: Update NewServer to accept Hub**

Modify `internal/api/server.go` — update `NewServer` signature:

```go
func NewServer(mgr *reports.Manager, cfg *config.Config, hub *Hub) *Server {
	h := NewHandlers(mgr, cfg)
	h.hub = hub
	// ... rest unchanged
}
```

- [ ] **Step 5: Remove daemon command registration**

Modify `cmd/daemon.go` — remove the `daemonCmd` cobra command and `init()` function. Keep only `runDaemonLoop`:

```go
package cmd

import (
	"context"
	"time"
)

func runDaemonLoop(ctx context.Context, interval time.Duration, scanFn func() error) {
	scanFn()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanFn()
		}
	}
}
```

- [ ] **Step 6: Run all tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./... 2>&1 | tail -20`
Expected: all PASS (daemon_test.go tests still pass since `runDaemonLoop` is preserved)

- [ ] **Step 7: Commit**

```bash
git add cmd/serve.go cmd/daemon.go cmd/daemon_test.go internal/api/server.go
git commit -m "feat: collapse daemon into serve command with background scanner"
```

---

### Task 5: Embed Frontend in Go Binary

**Files:**
- Create: `web/embed.go`
- Modify: `internal/api/server.go`
- Modify: `web/src/api/client.ts`
- Modify: `web/vite.config.ts`

- [ ] **Step 1: Change API client to use relative paths**

Replace line 1 of `web/src/api/client.ts`:

```typescript
const API_BASE = import.meta.env.VITE_API_URL || '';
```

When served from the same origin, empty string = relative path. For Vite dev server, configure a proxy instead.

- [ ] **Step 2: Add Vite proxy for development**

Replace `web/vite.config.ts`:

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5174,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
```

- [ ] **Step 3: Build the frontend**

Run: `cd /Users/jorgenbs/dev/ruter/fido/web && npm run build`
Expected: output in `web/dist/`

- [ ] **Step 4: Create embed.go**

```go
// web/embed.go
package web

import "embed"

//go:embed dist/*
var Assets embed.FS
```

- [ ] **Step 5: Add SPA file server to server.go**

Modify `internal/api/server.go` to serve the embedded frontend. Add imports and update `NewServer`:

```go
package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jorgenbs/fido/internal/config"
	"github.com/jorgenbs/fido/internal/reports"
	fidoweb "github.com/jorgenbs/fido/web"
)

func NewServer(mgr *reports.Manager, cfg *config.Config, hub *Hub) *Server {
	h := NewHandlers(mgr, cfg)
	h.hub = hub

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Route("/api", func(r chi.Router) {
		r.Get("/issues", h.ListIssues)
		r.Get("/issues/{id}", h.GetIssue)
		r.Post("/issues/{id}/investigate", h.TriggerInvestigate)
		r.Post("/issues/{id}/fix", h.TriggerFix)
		r.Post("/issues/{id}/ignore", h.TriggerIgnore)
		r.Post("/issues/{id}/unignore", h.TriggerUnignore)
		r.Get("/issues/{id}/progress", h.StreamProgress)
		r.Get("/issues/{id}/mr-status", h.RefreshMRStatus)
		r.Post("/scan", h.TriggerScan)
		r.Get("/events", h.StreamEvents)
	})

	// Serve embedded frontend (SPA)
	distFS, err := fs.Sub(fidoweb.Assets, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(distFS))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			// Try to serve the file directly; if not found, serve index.html (SPA routing)
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			if _, err := fs.Stat(distFS, path); err != nil {
				r.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	return &Server{handler: r, handlers: h}
}
```

- [ ] **Step 6: Build and verify**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go build -o fido .`
Expected: compiles successfully

- [ ] **Step 7: Run all Go tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./... 2>&1 | tail -20`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add web/embed.go web/src/api/client.ts web/vite.config.ts internal/api/server.go
git commit -m "feat: embed frontend in Go binary, serve SPA from single port"
```

---

### Task 6: Remove Docker Compose

**Files:**
- Delete: `docker-compose.yml`

- [ ] **Step 1: Remove docker-compose.yml**

Run: `cd /Users/jorgenbs/dev/ruter/fido && rm docker-compose.yml`

- [ ] **Step 2: Commit**

```bash
git rm docker-compose.yml
git commit -m "refactor: remove docker-compose in favor of single binary"
```

---

### Task 7: Frontend SSE Hook + Dashboard Subscription

**Files:**
- Create: `web/src/hooks/useEventStream.ts`
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: Add subscribeEvents to API client**

Add to the end of `web/src/api/client.ts`:

```typescript
export interface SSEEvent {
  type: 'scan:complete' | 'issue:updated' | 'issue:progress';
  payload: Record<string, any>;
}

export function subscribeEvents(onEvent: (event: SSEEvent) => void): EventSource {
  const es = new EventSource(`${API_BASE}/api/events`);
  es.onmessage = (msg) => {
    try {
      onEvent(JSON.parse(msg.data));
    } catch {
      // ignore malformed
    }
  };
  return es;
}
```

- [ ] **Step 2: Create useEventStream hook**

```typescript
// web/src/hooks/useEventStream.ts
import { useEffect, useRef } from 'react';
import { subscribeEvents, type SSEEvent } from '../api/client';

export function useEventStream(onEvent: (event: SSEEvent) => void) {
  const callbackRef = useRef(onEvent);
  callbackRef.current = onEvent;

  useEffect(() => {
    const es = subscribeEvents((evt) => callbackRef.current(evt));
    return () => es.close();
  }, []);
}
```

- [ ] **Step 3: Wire SSE into Dashboard for live updates**

In `web/src/pages/Dashboard.tsx`, add imports:

```typescript
import { useEventStream } from '../hooks/useEventStream';
import {
  listIssues,
  getIssue,
  triggerScan,
  triggerInvestigate as apiInvestigate,
  triggerFix as apiFix,
  ignoreIssue,
  unignoreIssue,
  type IssueListItem,
  type SSEEvent,
} from '../api/client';
```

Add state for tracking highlighted rows — after the existing `useState` declarations:

```typescript
const [highlightedIds, setHighlightedIds] = useState<Set<string>>(new Set());
```

Add the event stream hook — after `fetchIssues` useCallback:

```typescript
useEventStream((event: SSEEvent) => {
  const id = event.payload?.id as string | undefined;

  switch (event.type) {
    case 'scan:complete':
      fetchIssues();
      break;
    case 'issue:updated':
      if (id) {
        getIssue(id).then(() => fetchIssues()).catch(() => {});
        setHighlightedIds(prev => new Set(prev).add(id));
        setTimeout(() => {
          setHighlightedIds(prev => {
            const next = new Set(prev);
            next.delete(id);
            return next;
          });
        }, 2000);
      }
      break;
    case 'issue:progress':
      if (id) {
        // Update running_op in local state without full refetch
        setIssues(prev => prev.map(issue =>
          issue.id === id
            ? { ...issue, running_op: event.payload.status === 'started' ? event.payload.action as 'investigate' | 'fix' : undefined }
            : issue
        ));
        if (event.payload.status === 'complete') {
          fetchIssues();
          setHighlightedIds(prev => new Set(prev).add(id));
          setTimeout(() => {
            setHighlightedIds(prev => {
              const next = new Set(prev);
              next.delete(id);
              return next;
            });
          }, 2000);
        }
      }
      break;
  }
});
```

- [ ] **Step 4: Add highlight animation styles to rows**

In the main row `div`, add a conditional class for highlighting. Change the className:

```tsx
className={`grid grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.5fr_0.8fr_0.8fr] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-all duration-500 ${selectedIds.has(issue.id) ? 'bg-blue-950/30' : ''} ${highlightedIds.has(issue.id) ? 'bg-yellow-500/10' : ''}`}
```

- [ ] **Step 5: Verify frontend compiles**

Run: `cd /Users/jorgenbs/dev/ruter/fido/web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add web/src/hooks/useEventStream.ts web/src/api/client.ts web/src/pages/Dashboard.tsx
git commit -m "feat: SSE-driven live dashboard updates with row highlights"
```

---

### Task 8: Browser Notifications

**Files:**
- Create: `web/src/hooks/useNotifications.ts`
- Create: `web/src/components/NotificationBanner.tsx`
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: Create useNotifications hook**

```typescript
// web/src/hooks/useNotifications.ts
import { useState, useCallback, useEffect } from 'react';

type Permission = 'default' | 'granted' | 'denied';

export function useNotifications() {
  const [permission, setPermission] = useState<Permission>(
    typeof Notification !== 'undefined' ? Notification.permission as Permission : 'denied'
  );

  const requestPermission = useCallback(async () => {
    if (typeof Notification === 'undefined') return;
    const result = await Notification.requestPermission();
    setPermission(result as Permission);
  }, []);

  const notify = useCallback((title: string, options?: NotificationOptions) => {
    if (permission !== 'granted') return;
    if (document.hasFocus()) return; // don't notify if tab is focused
    const n = new Notification(title, { icon: '/favicon.svg', ...options });
    n.onclick = () => {
      window.focus();
      n.close();
    };
  }, [permission]);

  return { permission, requestPermission, notify };
}
```

- [ ] **Step 2: Create NotificationBanner component**

```tsx
// web/src/components/NotificationBanner.tsx
import { Button } from './ui/button';

interface Props {
  onAllow: () => void;
  onDismiss: () => void;
}

export function NotificationBanner({ onAllow, onDismiss }: Props) {
  return (
    <div className="flex items-center justify-between px-4 py-2 bg-blue-950/30 border-b border-blue-900 text-xs">
      <span className="text-blue-300">
        Enable browser notifications to get alerted when issues change state or CI completes.
      </span>
      <div className="flex gap-2">
        <Button size="sm" className="h-6 text-xs" onClick={onAllow}>
          Enable
        </Button>
        <button className="text-muted-foreground hover:text-foreground text-xs" onClick={onDismiss}>
          Dismiss
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Wire notifications into Dashboard**

In `web/src/pages/Dashboard.tsx`, add imports:

```typescript
import { useNotifications } from '../hooks/useNotifications';
import { NotificationBanner } from '../components/NotificationBanner';
```

Add inside the `Dashboard` component, after existing state declarations:

```typescript
const { permission, requestPermission, notify } = useNotifications();
const [bannerDismissed, setBannerDismissed] = useState(() =>
  localStorage.getItem('fido:notif-dismissed') === 'true'
);
const showBanner = permission === 'default' && !bannerDismissed;

const dismissBanner = () => {
  setBannerDismissed(true);
  localStorage.setItem('fido:notif-dismissed', 'true');
};
```

Update the `useEventStream` callback to fire notifications. Add notification calls inside the switch cases:

In the `issue:updated` case, after the highlight logic:

```typescript
case 'issue:updated': {
  const field = event.payload?.field as string;
  const newValue = event.payload?.newValue;
  if (id) {
    getIssue(id).then(() => fetchIssues()).catch(() => {});
    setHighlightedIds(prev => new Set(prev).add(id));
    setTimeout(() => {
      setHighlightedIds(prev => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }, 2000);

    // Notifications for stage changes and CI
    if (field === 'stage' && newValue === 'investigated') {
      notify('Investigation complete', { body: `Issue ${id} has been investigated` });
    } else if (field === 'stage' && newValue === 'fixed') {
      notify('Fix applied', { body: `Issue ${id} has been fixed` });
    } else if (field === 'ci_status' && newValue === 'passed') {
      notify('CI passed', { body: `Issue ${id}: pipeline passed` });
    } else if (field === 'ci_status' && newValue === 'failed') {
      notify('CI failed', { body: `Issue ${id}: pipeline failed` });
    }
  }
  break;
}
```

In the `scan:complete` case:

```typescript
case 'scan:complete': {
  const count = event.payload?.count as number;
  fetchIssues();
  if (count > 0) {
    notify('New issues found', { body: `Scan discovered ${count} new issue${count === 1 ? '' : 's'}` });
  }
  break;
}
```

Add the banner JSX right after the `{/* Nav */}` section:

```tsx
{showBanner && (
  <NotificationBanner onAllow={requestPermission} onDismiss={dismissBanner} />
)}
```

- [ ] **Step 4: Verify frontend compiles**

Run: `cd /Users/jorgenbs/dev/ruter/fido/web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add web/src/hooks/useNotifications.ts web/src/components/NotificationBanner.tsx web/src/pages/Dashboard.tsx
git commit -m "feat: browser notifications on scan results and stage/CI changes"
```

---

### Task 9: Expanded Row Improvements — Data

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`

- [ ] **Step 1: Write failing test for stack_trace and datadog_url in issue list**

Add to `internal/api/handlers_test.go`:

```go
func TestListIssues_IncludesStackTraceAndDatadogURL(t *testing.T) {
	dir := t.TempDir()
	mgr := reports.NewManager(dir)
	mgr.WriteError("issue-1", "# Error\n\n## Stack Trace\n```\npanic: runtime error\ngoroutine 1:\nmain.go:42\nmain.go:10\n```")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:    "svc-a",
		DatadogURL: "https://app.datadoghq.eu/error-tracking/issue/123",
	})

	h := NewHandlers(mgr, nil)
	req := httptest.NewRequest("GET", "/api/issues", nil)
	w := httptest.NewRecorder()
	h.ListIssues(w, req)

	var resp []IssueListItem
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(resp))
	}
	if resp[0].DatadogURL != "https://app.datadoghq.eu/error-tracking/issue/123" {
		t.Errorf("DatadogURL: got %q", resp[0].DatadogURL)
	}
	if resp[0].StackTrace == "" {
		t.Error("expected non-empty StackTrace")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -run TestListIssues_IncludesStackTraceAndDatadogURL -v`
Expected: FAIL — `DatadogURL` and `StackTrace` fields don't exist on `IssueListItem`

- [ ] **Step 3: Add fields to IssueListItem and populate them**

In `internal/api/handlers.go`, add fields to `IssueListItem`:

```go
type IssueListItem struct {
	ID          string  `json:"id"`
	Stage       string  `json:"stage"`
	Title       string  `json:"title,omitempty"`
	Message     string  `json:"message,omitempty"`
	Service     string  `json:"service,omitempty"`
	Env         string  `json:"env,omitempty"`
	LastSeen    string  `json:"last_seen,omitempty"`
	Count       int64   `json:"count,omitempty"`
	MRURL       *string `json:"mr_url"`
	Ignored     bool    `json:"ignored"`
	CIStatus    string  `json:"ci_status,omitempty"`
	CIURL       string  `json:"ci_url,omitempty"`
	Confidence  string  `json:"confidence,omitempty"`
	Complexity  string  `json:"complexity,omitempty"`
	CodeFixable string  `json:"code_fixable,omitempty"`
	RunningOp   string  `json:"running_op,omitempty"`
	DatadogURL  string  `json:"datadog_url,omitempty"`
	StackTrace  string  `json:"stack_trace,omitempty"`
}
```

In `ListIssues`, inside the `if issue.Meta != nil` block, add:

```go
item.Env = issue.Meta.Env
item.DatadogURL = issue.Meta.DatadogURL
```

After the meta block, read the error content and extract the stack trace:

```go
if errContent, err := h.reports.ReadError(issue.ID); err == nil {
	item.StackTrace = extractStackTrace(errContent)
}
```

Add the `extractStackTrace` helper function:

```go
func extractStackTrace(errorContent string) string {
	// Look for a fenced code block after "## Stack Trace" or "## Stacktrace"
	lines := strings.Split(errorContent, "\n")
	inTrace := false
	var trace []string
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "stack trace") || strings.Contains(strings.ToLower(line), "stacktrace") {
			inTrace = true
			continue
		}
		if inTrace {
			if strings.HasPrefix(line, "```") {
				if len(trace) > 0 {
					break // closing fence
				}
				continue // opening fence
			}
			if strings.HasPrefix(line, "## ") {
				break // next section
			}
			trace = append(trace, line)
		}
	}
	return strings.TrimSpace(strings.Join(trace, "\n"))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -run TestListIssues_IncludesStackTraceAndDatadogURL -v`
Expected: PASS

- [ ] **Step 5: Run all handler tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/api/ -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: include stack_trace, datadog_url, env in issue list API"
```

---

### Task 10: Expanded Row Improvements — Frontend

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: Add new fields to IssueListItem TypeScript interface**

In `web/src/api/client.ts`, update the `IssueListItem` interface:

```typescript
export interface IssueListItem {
  id: string;
  stage: string;
  title: string;
  message: string;
  service: string;
  env: string;
  last_seen: string;
  count: number;
  mr_url: string | null;
  ignored: boolean;
  ci_status: string;
  ci_url: string;
  confidence: string;
  complexity: string;
  code_fixable: string;
  running_op?: 'investigate' | 'fix';
  datadog_url: string;
  stack_trace: string;
}
```

- [ ] **Step 2: Add inline action handlers to Dashboard**

In `web/src/pages/Dashboard.tsx`, add handler functions after `handleIgnore`:

```typescript
const [actionLoading, setActionLoading] = useState<Record<string, string>>({});

const handleInvestigate = async (id: string) => {
  setActionLoading(prev => ({ ...prev, [id]: 'investigate' }));
  try {
    await apiInvestigate(id);
  } catch (err) {
    console.error('Failed to trigger investigate:', err);
  } finally {
    setActionLoading(prev => {
      const next = { ...prev };
      delete next[id];
      return next;
    });
  }
};

const handleFix = async (id: string) => {
  setActionLoading(prev => ({ ...prev, [id]: 'fix' }));
  try {
    await apiFix(id);
  } catch (err) {
    console.error('Failed to trigger fix:', err);
  } finally {
    setActionLoading(prev => {
      const next = { ...prev };
      delete next[id];
      return next;
    });
  }
};
```

- [ ] **Step 3: Replace expanded row content**

Replace the entire expanded row section (the `{expandedId === issue.id && (...)}` block) with:

```tsx
{expandedId === issue.id && (
  <div className="border-l-2 border-blue-500 bg-blue-950/20 px-4 py-3 space-y-3">
    {/* Metadata row */}
    <div className="flex flex-wrap gap-4 items-center text-xs">
      {issue.service && (
        <span className="text-muted-foreground">
          Service <strong className="text-foreground">{issue.service}</strong>
        </span>
      )}
      {issue.env && (
        <span className="text-muted-foreground">
          Env <strong className="text-foreground">{issue.env}</strong>
        </span>
      )}
      {issue.last_seen && (
        <span className="text-muted-foreground">
          Last seen <strong className="text-foreground">{new Date(issue.last_seen).toLocaleString()}</strong>
        </span>
      )}
      {issue.count > 0 && (
        <span className="text-muted-foreground">
          Occurrences <strong className="text-foreground">{issue.count}</strong>
        </span>
      )}
      {issue.datadog_url && (
        <a
          href={issue.datadog_url}
          target="_blank"
          rel="noreferrer"
          className="text-blue-400 hover:underline"
          onClick={(e) => e.stopPropagation()}
        >
          Datadog ↗
        </a>
      )}
      <Link
        to={`/issues/${issue.id}`}
        className="text-blue-400 hover:underline"
        onClick={(e) => e.stopPropagation()}
      >
        Full detail ↗
      </Link>
    </div>

    {/* Stack trace */}
    {issue.stack_trace && (
      <StackTracePreview trace={issue.stack_trace} />
    )}

    {/* Actions */}
    <div className="flex gap-2">
      {issue.stage === 'scanned' && !issue.running_op && (
        <Button
          size="sm"
          className="h-6 text-xs"
          disabled={!!actionLoading[issue.id]}
          onClick={(e) => {
            e.stopPropagation();
            handleInvestigate(issue.id);
          }}
        >
          {actionLoading[issue.id] === 'investigate' ? 'Starting...' : 'Investigate'}
        </Button>
      )}
      {issue.stage === 'investigated' && !issue.running_op && (
        <Button
          size="sm"
          className="h-6 text-xs"
          disabled={!!actionLoading[issue.id]}
          onClick={(e) => {
            e.stopPropagation();
            handleFix(issue.id);
          }}
        >
          {actionLoading[issue.id] === 'fix' ? 'Starting...' : 'Fix'}
        </Button>
      )}
      {issue.running_op && (
        <span className="inline-flex items-center gap-1 text-blue-400 text-xs">
          <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
          {issue.running_op === 'investigate' ? 'Investigating...' : 'Fixing...'}
        </span>
      )}
      <Button
        size="sm"
        variant="outline"
        className="h-6 text-xs"
        onClick={(e) => {
          e.stopPropagation();
          handleIgnore(issue.id, issue.ignored);
        }}
      >
        {issue.ignored ? 'Unignore' : 'Ignore'}
      </Button>
    </div>
  </div>
)}
```

- [ ] **Step 4: Add StackTracePreview component**

Add this component at the bottom of `Dashboard.tsx` (after the `Dashboard` function):

```tsx
function StackTracePreview({ trace }: { trace: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = trace.split('\n');
  const truncated = lines.length > 15;
  const displayLines = expanded ? lines : lines.slice(0, 15);

  return (
    <div className="text-xs">
      <pre className="p-3 bg-muted/30 rounded border border-border font-mono text-muted-foreground whitespace-pre-wrap overflow-auto max-h-80">
        {displayLines.join('\n')}
      </pre>
      {truncated && (
        <button
          className="mt-1 text-blue-400 hover:underline text-xs"
          onClick={(e) => {
            e.stopPropagation();
            setExpanded(!expanded);
          }}
        >
          {expanded ? 'Show less' : `Show more (${lines.length - 15} more lines)`}
        </button>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Verify frontend compiles**

Run: `cd /Users/jorgenbs/dev/ruter/fido/web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add web/src/api/client.ts web/src/pages/Dashboard.tsx
git commit -m "feat: expanded row with stack trace, datadog link, inline actions"
```

---

### Task 11: Build, Verify, and Clean Up

**Files:**
- Modify: `CLAUDE.md` (update commands section)

- [ ] **Step 1: Build frontend**

Run: `cd /Users/jorgenbs/dev/ruter/fido/web && npm run build`
Expected: successful build in `web/dist/`

- [ ] **Step 2: Build Go binary**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go build -o fido .`
Expected: compiles successfully

- [ ] **Step 3: Run all Go tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./...`
Expected: all PASS

- [ ] **Step 4: Verify frontend with Playwright**

Run the verification script:

```bash
# Terminal 1: start the server
cd /Users/jorgenbs/dev/ruter/fido && ./fido serve &
SERVER_PID=$!

# Terminal 2: verify
cd /Users/jorgenbs/dev/ruter/fido/web && node verify.mjs

kill $SERVER_PID
```

Expected: no React errors found

- [ ] **Step 5: Verify SSE endpoint with curl**

```bash
cd /Users/jorgenbs/dev/ruter/fido && ./fido serve &
SERVER_PID=$!
sleep 1

# Test SSE endpoint connects
timeout 3 curl -s -N http://localhost:8080/api/events &
CURL_PID=$!

# Trigger a scan to generate an event
curl -s -X POST http://localhost:8080/api/scan

sleep 2
kill $CURL_PID 2>/dev/null
kill $SERVER_PID
```

Expected: SSE endpoint returns `data: {"type":"scan:complete"...}` events

- [ ] **Step 6: Verify embedded frontend serves**

```bash
cd /Users/jorgenbs/dev/ruter/fido && ./fido serve &
SERVER_PID=$!
sleep 1

# Check that the frontend is served
curl -s http://localhost:8080/ | head -5

kill $SERVER_PID
```

Expected: HTML response containing the React app's `index.html`

- [ ] **Step 7: Update CLAUDE.md**

Replace the Commands section in `CLAUDE.md`:

```markdown
## Commands

- `go build -o fido .` — build (requires `cd web && npm run build` first for embedded frontend)
- `go test ./...` — run tests
- `cd web && npm run dev` — start frontend dev server with API proxy (port 5174)
- `./fido serve` — full stack: API + embedded frontend + background scanner (port 8080)
```

- [ ] **Step 8: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for single binary workflow"
```
