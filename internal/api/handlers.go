package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

type IssueListItem struct {
	ID    string `json:"id"`
	Stage string `json:"stage"`
}

type IssueDetail struct {
	ID            string               `json:"id"`
	Stage         string               `json:"stage"`
	Error         string               `json:"error"`
	Investigation *string              `json:"investigation"`
	Fix           *string              `json:"fix"`
	Resolve       *reports.ResolveData `json:"resolve"`
}

type ScanFunc func() error
type InvestigateFunc func(issueID string) error
type FixFunc func(issueID string) error

type Handlers struct {
	reports       *reports.Manager
	config        *config.Config
	scanFn        ScanFunc
	investigateFn InvestigateFunc
	fixFn         FixFunc
	running       sync.Map
}

func NewHandlers(mgr *reports.Manager, cfg *config.Config) *Handlers {
	return &Handlers{reports: mgr, config: cfg}
}

func (h *Handlers) SetScanFunc(fn ScanFunc)              { h.scanFn = fn }
func (h *Handlers) SetInvestigateFunc(fn InvestigateFunc) { h.investigateFn = fn }
func (h *Handlers) SetFixFunc(fn FixFunc)                { h.fixFn = fn }

func (h *Handlers) ListIssues(w http.ResponseWriter, r *http.Request) {
	issues, err := h.reports.ListIssues()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	statusFilter := r.URL.Query().Get("status")

	var items []IssueListItem
	for _, issue := range issues {
		if statusFilter != "" && string(issue.Stage) != statusFilter {
			continue
		}
		items = append(items, IssueListItem{
			ID:    issue.ID,
			Stage: string(issue.Stage),
		})
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
	if fix, err := h.reports.ReadFix(id); err == nil {
		detail.Fix = &fix
	}
	if resolve, err := h.reports.ReadResolve(id); err == nil {
		detail.Resolve = resolve
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
	go func() {
		defer h.running.Delete(id)
		h.investigateFn(id)
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
	go func() {
		defer h.running.Delete(id)
		h.fixFn(id)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
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

	for {
		if _, running := h.running.Load(id); !running {
			fmt.Fprintf(w, "data: {\"status\": \"complete\"}\n\n")
			flusher.Flush()
			return
		}
		fmt.Fprintf(w, "data: {\"status\": \"running\"}\n\n")
		flusher.Flush()
		time.Sleep(2 * time.Second)
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
