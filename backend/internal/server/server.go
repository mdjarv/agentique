package server

import (
	"io/fs"
	"net/http"

	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/ws"
)

// Server is the main HTTP server for the Agentique backend.
type Server struct {
	mux *http.ServeMux
	mgr *session.Manager
}

// New creates a new Server with all routes registered.
func New(queries *store.Queries) *Server {
	mux := http.NewServeMux()
	hub := ws.NewHub()
	mgr := session.NewManager(queries, hub)
	svc := session.NewService(mgr, queries, hub)
	gitSvc := session.NewGitService(mgr, queries, hub)

	ph := &project.Handler{Queries: queries}

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/projects", ph.HandleList)
	mux.HandleFunc("POST /api/projects", ph.HandleCreate)
	mux.HandleFunc("DELETE /api/projects/{id}", ph.HandleDelete)

	wsh := &ws.Handler{Service: svc, GitService: gitSvc, Hub: hub}
	mux.Handle("GET /ws", wsh)

	frontendSub, _ := fs.Sub(frontendFS, "frontend_dist")
	mux.Handle("GET /", &spaHandler{fs: frontendSub})

	s := &Server{mux: mux, mgr: mgr}
	return s
}

// Shutdown gracefully closes all live sessions.
func (s *Server) Shutdown() {
	if s.mgr != nil {
		s.mgr.CloseAll()
	}
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	corsMiddleware(s.mux).ServeHTTP(w, r)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
