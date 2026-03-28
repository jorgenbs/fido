package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/gitlab"
	"github.com/ruter-as/fido/internal/reports"
)

type IssueListItem struct {
	ID          string  `json:"id"`
	Stage       string  `json:"stage"`
	Title       string  `json:"title,omitempty"`
	Message     string  `json:"message,omitempty"`
	Service     string  `json:"service,omitempty"`
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
	Env         string  `json:"env,omitempty"`
	DatadogURL  string  `json:"datadog_url,omitempty"`
	StackTrace  string  `json:"stack_trace,omitempty"`
}

type IssueDetail struct {
	ID            string               `json:"id"`
	Stage         string               `json:"stage"`
	Error         string               `json:"error"`
	Investigation *string              `json:"investigation"`
	Fix           *string              `json:"fix"`
	Resolve       *reports.ResolveData `json:"resolve"`
	CIStatus      string               `json:"ci_status,omitempty"`
	CIURL         string               `json:"ci_url,omitempty"`
	RunningOp     string               `json:"running_op,omitempty"`
}

type ScanFunc func() error
type InvestigateFunc func(issueID string, progress io.Writer) error
type FixFunc func(issueID string, iterate bool, progress io.Writer) error

// progressBuf is a thread-safe write buffer for streaming agent output to SSE clients.
type progressBuf struct {
	mu      sync.Mutex
	content strings.Builder
}

func (p *progressBuf) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.content.Write(b)
}

func (p *progressBuf) ReadAll() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.content.String()
}

type Handlers struct {
	reports       *reports.Manager
	config        *config.Config
	hub           *Hub
	scanFn        ScanFunc
	investigateFn InvestigateFunc
	fixFn         FixFunc
	running       sync.Map
	progressBufs  sync.Map // issueID -> *progressBuf
}

func NewHandlers(mgr *reports.Manager, cfg *config.Config) *Handlers {
	return &Handlers{reports: mgr, config: cfg}
}

func (h *Handlers) publish(evt Event) {
	if h.hub != nil {
		h.hub.Publish(evt)
	}
}

func (h *Handlers) SetScanFunc(fn ScanFunc)              { h.scanFn = fn }
func (h *Handlers) SetInvestigateFunc(fn InvestigateFunc) { h.investigateFn = fn }
func (h *Handlers) SetFixFunc(fn FixFunc)                { h.fixFn = fn }

func (h *Handlers) ListIssues(w http.ResponseWriter, r *http.Request) {
	showIgnored := r.URL.Query().Get("show_ignored") == "true"
	issues, err := h.reports.ListIssues(showIgnored)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	statusFilter := r.URL.Query().Get("status")

	items := []IssueListItem{}
	for _, issue := range issues {
		if statusFilter != "" && string(issue.Stage) != statusFilter {
			continue
		}
		item := IssueListItem{
			ID:    issue.ID,
			Stage: string(issue.Stage),
		}
		if issue.Meta != nil {
			item.Title    = issue.Meta.Title
			item.Message  = issue.Meta.Message
			item.Service  = issue.Meta.Service
			item.LastSeen = issue.Meta.LastSeen
			item.Count    = issue.Meta.Count
			item.Ignored  = issue.Meta.Ignored
			item.CIStatus    = issue.Meta.CIStatus
			item.CIURL       = issue.Meta.CIURL
			item.Confidence  = issue.Meta.Confidence
			item.Complexity  = issue.Meta.Complexity
			item.CodeFixable = issue.Meta.CodeFixable
			item.Env         = issue.Meta.Env
			item.DatadogURL  = issue.Meta.DatadogURL
		}
		if errContent, err := h.reports.ReadError(issue.ID); err == nil {
			item.StackTrace = extractStackTrace(errContent)
		}
		if issue.MRURL != "" {
			item.MRURL = &issue.MRURL
		}
		if op, ok := h.running.Load(issue.ID); ok {
			item.RunningOp = op.(string)
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) GetIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	errorContent, _ := h.reports.ReadError(id)

	detail := IssueDetail{
		ID:    id,
		Stage: string(h.reports.Stage(id)),
		Error: errorContent,
	}

	if inv, err := h.reports.ReadInvestigation(id); err == nil {
		detail.Investigation = &inv
	}
	if fix, _, err := h.reports.ReadLatestFix(id); err == nil {
		detail.Fix = &fix
	}
	if resolve, err := h.reports.ReadResolve(id); err == nil {
		detail.Resolve = resolve
	}
	if meta, err := h.reports.ReadMetadata(id); err == nil {
		detail.CIStatus = meta.CIStatus
		detail.CIURL = meta.CIURL
	}
	if op, ok := h.running.Load(id); ok {
		detail.RunningOp = op.(string)
	}

	writeJSON(w, http.StatusOK, detail)
}

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

func (h *Handlers) TriggerInvestigate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, loaded := h.running.LoadOrStore(id, "investigate"); loaded {
		writeError(w, http.StatusConflict, "action already running for this issue")
		return
	}
	if h.investigateFn == nil {
		h.running.Delete(id)
		writeError(w, http.StatusNotImplemented, "investigate not configured")
		return
	}
	pbuf := &progressBuf{}
	h.progressBufs.Store(id, pbuf)
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
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) TriggerFix(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, loaded := h.running.LoadOrStore(id, "fix"); loaded {
		writeError(w, http.StatusConflict, "action already running for this issue")
		return
	}
	if h.fixFn == nil {
		h.running.Delete(id)
		writeError(w, http.StatusNotImplemented, "fix not configured")
		return
	}

	var req struct {
		Iterate bool `json:"iterate"`
	}
	json.NewDecoder(r.Body).Decode(&req) // ignore parse error; defaults to iterate=false

	pbuf := &progressBuf{}
	h.progressBufs.Store(id, pbuf)
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
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) TriggerIgnore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if err := h.reports.SetIgnored(id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.publish(Event{Type: "issue:updated", Payload: map[string]any{"id": id, "field": "ignored", "newValue": true}})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
}

func (h *Handlers) TriggerUnignore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if err := h.reports.SetIgnored(id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.publish(Event{Type: "issue:updated", Payload: map[string]any{"id": id, "field": "ignored", "newValue": false}})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unignored"})
}

func (h *Handlers) RefreshMRStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	resolve, err := h.reports.ReadResolve(id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"ci_status": "",
			"ci_url":    "",
			"mr_status": "",
		})
		return
	}

	meta, err := h.reports.ReadMetadata(id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"ci_status": "",
			"ci_url":    "",
			"mr_status": resolve.MRStatus,
		})
		return
	}

	ciStatus := meta.CIStatus
	ciURL := meta.CIURL
	mrStatus := resolve.MRStatus

	if h.config != nil && resolve.Branch != "" {
		if repo, ok := h.config.Repositories[meta.Service]; ok && repo.Local != "" {
			if mr, ci, ciU, fetchErr := gitlab.FetchMRStatus(resolve.Branch, repo.Local); fetchErr == nil {
				mrStatus = mr
				_ = h.reports.SetMRStatus(id, mr)
				if ci != "" {
					ciStatus = ci
					ciURL = ciU
					_ = h.reports.SetCIStatus(id, ci, ciU)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"ci_status": ciStatus,
		"ci_url":    ciURL,
		"mr_status": mrStatus,
	})
}

func (h *Handlers) StreamProgress(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	var lastSent int

	sendLog := func(chunk string) {
		data, _ := json.Marshal(map[string]string{"status": "running", "log": chunk})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	drainLog := func() {
		if pbuf, ok := h.progressBufs.Load(id); ok {
			content := pbuf.(*progressBuf).ReadAll()
			if len(content) > lastSent {
				sendLog(content[lastSent:])
				lastSent = len(content)
			}
		}
	}

	for {
		// Stream any new log content
		drainLog()

		if errMsg, hasErr := h.running.Load(id + "_error"); hasErr {
			h.running.Delete(id + "_error")
			drainLog() // flush remaining output before error
			fmt.Fprintf(w, "data: {\"status\":\"error\",\"message\":%q}\n\n", errMsg)
			flusher.Flush()
			return
		}
		if _, running := h.running.Load(id); !running {
			drainLog()
			stage := h.reports.Stage(id)
			if stage == reports.StageInvestigated || stage == reports.StageFixed {
				fmt.Fprintf(w, "data: {\"status\":\"complete\"}\n\n")
			} else {
				fmt.Fprintf(w, "data: {\"status\":\"idle\"}\n\n")
			}
			flusher.Flush()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

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

func extractStackTrace(errorContent string) string {
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
					break
				}
				continue
			}
			if strings.HasPrefix(line, "## ") {
				break
			}
			trace = append(trace, line)
		}
	}
	return strings.TrimSpace(strings.Join(trace, "\n"))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
