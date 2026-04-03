package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
	fidoweb "github.com/ruter-as/fido/web"
)

type Server struct {
	handler  http.Handler
	handlers *Handlers
}

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
		r.Get("/issues/{id}/timeseries", h.GetTimeseries)
		r.Post("/scan", h.TriggerScan)
		r.Get("/events", h.StreamEvents)
		r.Post("/debug/event", h.DebugPublishEvent)
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

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func GetHandlers(s *Server) *Handlers {
	return s.handlers
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
