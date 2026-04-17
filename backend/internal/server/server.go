package server

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/browser"
	"github.com/mdjarv/agentique/backend/internal/claudeaccount"
	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/devurls"
	"github.com/mdjarv/agentique/backend/internal/filebrowser"
	"github.com/mdjarv/agentique/backend/internal/filesystem"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/persona"
	"github.com/mdjarv/agentique/backend/internal/prompttemplate"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/team"
	"github.com/mdjarv/agentique/backend/internal/testmode"
	"github.com/mdjarv/agentique/backend/internal/ws"
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

	// ExperimentalTeams enables persistent agent profiles, teams, and personas.
	ExperimentalTeams bool
	// ExperimentalBrowser enables the per-session Chrome browser panel.
	ExperimentalBrowser bool

	// DevURLSlots is the configured pool of leasable dev URL slots. Empty
	// disables the AcquireDevUrl tool path (slots will report all-busy).
	DevURLSlots []config.DevURLSlot

	// MCPInternalURL is the URL spawned Claude subprocesses use to reach the
	// agentique HTTP MCP endpoint (e.g. "http://localhost:19201/mcp"). Must
	// be reachable from the local machine; not exposed publicly.
	MCPInternalURL string
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
	svc            *session.Service
	browserSvc     *session.BrowserService
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

	devStore := devurls.NewStore(cfg.DevURLSlots)
	mcpTokens := mcphttp.NewTokenStore()

	mgr := session.NewManager(cfg.DB, queries, hub, connector)
	mgr.SetMCPHTTP(mcpTokens, cfg.MCPInternalURL)
	mgr.SetDevURLStore(devStore)
	if cfg.DevMode && cfg.DBPath != "" {
		mgr.GlobalPreamble = devModePreamble(cfg.DBPath)
	}
	mgr.RecoverStaleSessions(context.Background())
	svc := session.NewService(mgr, queries, hub, runner)
	gitSvc := session.NewGitService(mgr, queries, hub, runner)
	svc.SetGitService(gitSvc)

	var browserSvc *session.BrowserService
	if cfg.ExperimentalBrowser {
		browserMgr := browser.NewManager()
		browserSvc = session.NewBrowserService(mgr, browserMgr, hub)
		svc.SetBrowserService(browserSvc)
		slog.Info("experimental browser panel enabled")
	}

	ph := &project.Handler{Queries: queries}

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		httperror.JSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"features": map[string]bool{
				"browser": cfg.ExperimentalBrowser,
				"teams":   cfg.ExperimentalTeams,
			},
		})
	})
	mux.HandleFunc("GET /api/projects", ph.HandleList)
	mux.HandleFunc("POST /api/projects", ph.HandleCreate)
	mux.HandleFunc("PATCH /api/projects/{id}", ph.HandleUpdate)
	mux.HandleFunc("DELETE /api/projects/{id}", ph.HandleDelete)
	mux.HandleFunc("GET /api/preset-definitions", ph.HandleListPresetDefinitions)

	pth := &prompttemplate.Handler{Queries: queries}
	mux.HandleFunc("GET /api/templates", pth.HandleList)
	mux.HandleFunc("POST /api/templates", pth.HandleCreate)
	mux.HandleFunc("GET /api/templates/{id}", pth.HandleGet)
	mux.HandleFunc("PUT /api/templates/{id}", pth.HandleUpdate)
	mux.HandleFunc("DELETE /api/templates/{id}", pth.HandleDelete)

	cah := claudeaccount.NewHandler()
	mux.HandleFunc("GET /api/claude-account", cah.HandleStatus)
	mux.HandleFunc("POST /api/claude-account/logout", cah.HandleLogout)
	mux.HandleFunc("POST /api/claude-account/login", cah.HandleLogin)
	mux.HandleFunc("POST /api/claude-account/login/cancel", cah.HandleLoginCancel)
	mux.HandleFunc("POST /api/claude-account/login/code", cah.HandleLoginCode)

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

	fh := &session.FilesHandler{}
	mux.HandleFunc("GET /api/sessions/{id}/files/{filepath...}", fh.HandleServe)

	projectGitSvc := project.NewGitService(queries, hub, project.RealGitOps(), runner)

	var teamSvc *team.Service
	var personaSvc *persona.Service
	if cfg.ExperimentalTeams {
		teamSvc = team.NewService(queries, hub)
		personaSvc = persona.NewService(runner, queries, hub)
		svc.SetPersonaQuerier(personaSvc)
		slog.Info("experimental teams feature enabled")
	}

	wsh := &ws.Handler{Service: svc, GitService: gitSvc, ProjectGitService: projectGitSvc, Queries: queries, Hub: hub, TeamService: teamSvc, PersonaService: personaSvc, BrowserService: browserSvc}
	mux.Handle("GET /ws", wsh)

	mcpHandler := mcphttp.NewHandler(mcpTokens, devStore)
	// Register explicit methods so the pattern doesn't conflict with the SPA
	// catch-all "GET /". The handler dispatches on method internally.
	mux.Handle("POST /mcp", mcpHandler)
	mux.Handle("GET /mcp", mcpHandler)
	mux.Handle("DELETE /mcp", mcpHandler)

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

	s := &Server{mux: mux, mgr: mgr, svc: svc, browserSvc: browserSvc}

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
		wsh.AllowedOrigins = s.allowedOrigins
	} else {
		// When auth is disabled, serve a static status endpoint.
		mux.HandleFunc("GET /api/auth/status", func(w http.ResponseWriter, r *http.Request) {
			httperror.JSON(w, http.StatusOK, map[string]any{
				"authEnabled":   false,
				"authenticated": true,
				"userCount":     0,
			})
		})
	}

	return s, nil
}

// Shutdown gracefully closes all live sessions and browser instances.
func (s *Server) Shutdown() {
	if s.svc != nil {
		s.svc.Close()
	}
	if s.browserSvc != nil {
		s.browserSvc.StopAll()
	}
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
	chain = maxBodySize(chain)
	s.corsMiddleware(chain).ServeHTTP(w, r)
}

// maxBodySize limits request body reads to 2 MB, preventing OOM from oversized payloads.
// WebSocket upgrades are excluded since they don't have a traditional request body.
func maxBodySize(next http.Handler) http.Handler {
	const maxBytes = 2 << 20 // 2 MB
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
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
