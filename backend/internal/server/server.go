package server

import (
	"io/fs"
	"net/http"

	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/ws"
)

// Server is the main HTTP server for the Agentique backend.
type Server struct {
	mux *http.ServeMux
}

// New creates a new Server with all routes registered.
func New(queries *store.Queries) *Server {
	mux := http.NewServeMux()

	// Project handler.
	ph := &project.Handler{Queries: queries}

	// API routes.
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/projects", ph.HandleList)
	mux.HandleFunc("POST /api/projects", ph.HandleCreate)
	mux.HandleFunc("DELETE /api/projects/{id}", ph.HandleDelete)

	// WebSocket endpoint.
	wsh := &ws.Handler{Queries: queries}
	mux.Handle("GET /ws", wsh)

	// SPA catch-all for frontend.
	frontendSub, _ := fs.Sub(frontendFS, "frontend_dist")
	mux.Handle("GET /", &spaHandler{fs: frontendSub})

	s := &Server{mux: mux}
	return s
}

// ServeHTTP implements the http.Handler interface, delegating to the internal mux
// with CORS middleware applied.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	corsMiddleware(s.mux).ServeHTTP(w, r)
}

// corsMiddleware adds permissive CORS headers for development.
// Skips WebSocket upgrade requests since CORS headers interfere with the handshake.
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
