package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/allbin/agentique/backend/internal/auth"
	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/ws"
)

// Config holds server configuration.
type Config struct {
	AuthEnabled bool
	RPID        string
	RPOrigins   []string
}

// Server is the main HTTP server for the Agentique backend.
type Server struct {
	mux            *http.ServeMux
	mgr            *session.Manager
	authSvc        *auth.Service
	allowedOrigins map[string]bool
}

// New creates a new Server with all routes registered.
func New(queries *store.Queries, cfg Config) (*Server, error) {
	mux := http.NewServeMux()
	hub := ws.NewHub()
	mgr := session.NewManager(queries, hub)
	mgr.RecoverStaleSessions(context.Background())
	svc := session.NewService(mgr, queries, hub)
	gitSvc := session.NewGitService(mgr, queries, hub)

	ph := &project.Handler{Queries: queries}

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/projects", ph.HandleList)
	mux.HandleFunc("POST /api/projects", ph.HandleCreate)
	mux.HandleFunc("PATCH /api/projects/{id}", ph.HandleUpdate)
	mux.HandleFunc("DELETE /api/projects/{id}", ph.HandleDelete)

	subscribe := func() (<-chan session.SSEEvent, func()) {
		ch := make(chan session.SSEEvent, 64)
		listener := &sseListener{ch: ch}
		hub.AddListener(listener)
		return ch, func() {
			hub.RemoveListener(listener)
			close(ch)
		}
	}
	sh := session.NewHandler(svc, subscribe)
	mux.HandleFunc("GET /api/sessions", sh.HandleList)
	mux.HandleFunc("GET /api/sessions/events", sh.HandleEvents)
	mux.HandleFunc("GET /api/sessions/{id}", sh.HandleGet)
	mux.HandleFunc("GET /api/sessions/{id}/history", sh.HandleHistory)
	mux.HandleFunc("POST /api/sessions/{id}/stop", sh.HandleStop)
	mux.HandleFunc("POST /api/sessions/{id}/query", sh.HandleQuery)
	mux.HandleFunc("DELETE /api/sessions/{id}", sh.HandleDelete)

	projectGitSvc := project.NewGitService(queries, hub)
	wsh := &ws.Handler{Service: svc, GitService: gitSvc, ProjectGitService: projectGitSvc, Queries: queries, Hub: hub}
	mux.Handle("GET /ws", wsh)

	frontendSub, _ := fs.Sub(frontendFS, "frontend_dist")
	mux.Handle("GET /", &spaHandler{fs: frontendSub})

	s := &Server{mux: mux, mgr: mgr}

	if cfg.AuthEnabled {
		authSvc, err := auth.NewService(queries, cfg.RPID, cfg.RPOrigins)
		if err != nil {
			return nil, fmt.Errorf("auth service: %w", err)
		}
		authSvc.RegisterRoutes(mux)
		s.authSvc = authSvc
		s.allowedOrigins = make(map[string]bool, len(cfg.RPOrigins))
		for _, o := range cfg.RPOrigins {
			s.allowedOrigins[o] = true
		}
	} else {
		// When auth is disabled, serve a static status endpoint.
		mux.HandleFunc("GET /api/auth/status", func(w http.ResponseWriter, r *http.Request) {
			respondJSON(w, http.StatusOK, map[string]any{
				"authEnabled":   false,
				"authenticated": true,
				"userCount":     0,
			})
		})
	}

	return s, nil
}

// Shutdown gracefully closes all live sessions.
func (s *Server) Shutdown() {
	if s.mgr != nil {
		s.mgr.CloseAll()
	}
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var chain http.Handler = requestLogger(s.mux)
	if s.authSvc != nil {
		chain = s.authSvc.Middleware(chain)
	}
	s.corsMiddleware(chain).ServeHTTP(w, r)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		if origin != "" {
			// When auth is enabled, only allow configured origins.
			if len(s.allowedOrigins) > 0 && !s.allowedOrigins[origin] {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip logging for WebSocket upgrades (logged in ws package) and static assets.
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		duration := time.Since(start)

		level := slog.LevelDebug
		if sw.status >= 500 {
			level = slog.LevelError
		} else if sw.status >= 400 {
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", duration,
		)
	})
}
