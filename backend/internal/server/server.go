package server

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/allbin/agentique/backend/internal/auth"
	"github.com/allbin/agentique/backend/internal/claudeaccount"
	"github.com/allbin/agentique/backend/internal/filebrowser"
	"github.com/allbin/agentique/backend/internal/filesystem"
	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/respond"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/testmode"
	"github.com/allbin/agentique/backend/internal/ws"
)

// Config holds server configuration.
type Config struct {
	AuthEnabled bool
	RPID        string
	RPOrigins   []string

	// TestMode enables mock CLI connector and test-only HTTP routes.
	TestMode bool
	// DevMode indicates a non-release build. Injects safety instructions into session prompts.
	DevMode bool
	// DBPath is the resolved database file path. Used to generate dev-mode safety warnings.
	DBPath string
	// DB is required when TestMode is true (for raw SQL in reset).
	DB *sql.DB
}

func devModePreamble(dbPath string) string {
	return fmt.Sprintf(`## Live Database Warning

This Agentique instance is a development build. The live database is at:

    %s

This file is shared with the running server. Any command that writes to, overwrites, or deletes this file will cause data loss. If you cannot verify that a command is isolated from this database, confirm with the user before proceeding.`, dbPath)
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

	var connector session.CLIConnector
	var runner session.BlockingRunner
	var testConnector *testmode.Connector

	if cfg.TestMode {
		testConnector = testmode.NewConnector()
		connector = testConnector
		runner = testmode.NewBlockingRunner()
		slog.Info("test mode enabled: using mock CLI connector")
	} else {
		connector = session.RealConnector()
		runner = session.RealBlockingRunner()
	}

	mgr := session.NewManager(queries, hub, connector)
	if cfg.DevMode && cfg.DBPath != "" {
		mgr.GlobalPreamble = devModePreamble(cfg.DBPath)
	}
	mgr.RecoverStaleSessions(context.Background())
	svc := session.NewService(mgr, queries, hub, runner)
	gitSvc := session.NewGitService(mgr, queries, hub, runner)
	svc.SetGitService(gitSvc)

	ph := &project.Handler{Queries: queries}

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/projects", ph.HandleList)
	mux.HandleFunc("POST /api/projects", ph.HandleCreate)
	mux.HandleFunc("PATCH /api/projects/{id}", ph.HandleUpdate)
	mux.HandleFunc("DELETE /api/projects/{id}", ph.HandleDelete)
	mux.HandleFunc("GET /api/preset-definitions", ph.HandleListPresetDefinitions)

	cah := &claudeaccount.Handler{}
	mux.HandleFunc("GET /api/claude-account", cah.HandleStatus)
	mux.HandleFunc("POST /api/claude-account/logout", cah.HandleLogout)
	mux.HandleFunc("POST /api/claude-account/login", cah.HandleLogin)
	mux.HandleFunc("POST /api/claude-account/login/cancel", cah.HandleLoginCancel)

	fsh := &filesystem.Handler{}
	mux.HandleFunc("GET /api/filesystem/browse", fsh.HandleBrowse)
	mux.HandleFunc("GET /api/filesystem/validate", fsh.HandleValidate)

	fbh := &filebrowser.Handler{Queries: queries}
	mux.HandleFunc("GET /api/projects/{id}/files", fbh.HandleList)
	mux.HandleFunc("GET /api/projects/{id}/files/content", fbh.HandleContent)

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

	if cfg.TestMode && testConnector != nil {
		th := &testmode.Handler{
			Connector: testConnector,
			Manager:   mgr,
			Queries:   queries,
			DB:        cfg.DB,
		}
		th.RegisterRoutes(mux)
	}

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
			respond.JSON(w, http.StatusOK, map[string]any{
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
