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
	"github.com/ruter-as/fido/internal/reports"
)

type IssueListItem struct {
	ID       string  `json:"id"`
	Stage    string  `json:"stage"`
	Title    string  `json:"title,omitempty"`
	Message  string  `json:"message,omitempty"`
	Service  string  `json:"service,omitempty"`
	LastSeen string  `json:"last_seen,omitempty"`
	Count    int64   `json:"count,omitempty"`
	MRURL    *string `json:"mr_url"`
	Ignored  bool    `json:"ignored"`
	CIStatus    string `json:"ci_status,omitempty"`
	CIURL       string `json:"ci_url,omitempty"`
	Confidence  string `json:"confidence,omitempty"`
	Complexity  string `json:"complexity,omitempty"`
	CodeFixable string `json:"code_fixable,omitempty"`
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
	scanFn        ScanFunc
	investigateFn InvestigateFunc
	fixFn         FixFunc
	running       sync.Map
	progressBufs  sync.Map // issueID -> *progressBuf
}

func NewHandlers(mgr *reports.Manager, cfg *config.Config) *Handlers {
	return &Handlers{reports: mgr, config: cfg}
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
		}
		if issue.MRURL != "" {
			item.MRURL = &issue.MRURL
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

	writeJSON(w, http.StatusOK, detail)
}

func (h *Handlers) TriggerScan(w http.ResponseWriter, r *http.Request) {
	if h.scanFn == nil {
		writeError(w, http.StatusNotImplemented, "scan not configured")
		return
	}
	go h.scanFn()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) TriggerInvestigate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, loaded := h.running.LoadOrStore(id, true); loaded {
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
		if err := h.investigateFn(id, pbuf); err != nil {
			log.Printf("investigate %s failed: %v", id, err)
			h.running.Store(id+"_error", err.Error())
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) TriggerFix(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.reports.Exists(id) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, loaded := h.running.LoadOrStore(id, true); loaded {
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
		if err := h.fixFn(id, req.Iterate, pbuf); err != nil {
			log.Printf("fix %s failed: %v", id, err)
			h.running.Store(id+"_error", err.Error())
		}
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "unignored"})
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
			drainLog() // flush remaining output before complete
			fmt.Fprintf(w, "data: {\"status\":\"complete\"}\n\n")
			flusher.Flush()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
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
