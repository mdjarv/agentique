package server

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/allbin/agentkit/devurls"
	"github.com/allbin/agentkit/eventbus"
	"github.com/allbin/agentkit/runtime"
	claudeadapter "github.com/allbin/agentkit/runtime/cli/claude"
	codexadapter "github.com/allbin/agentkit/runtime/cli/codex"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/brain"
	"github.com/mdjarv/agentique/backend/internal/browser"
	"github.com/mdjarv/agentique/backend/internal/claudeaccount"
	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/filebrowser"
	"github.com/mdjarv/agentique/backend/internal/filesystem"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/persona"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/prompttemplate"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/storage"
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

	// Brain (persistent agent memory). BrainDir enables the feature; the optional
	// Chroma/embed fields enable semantic recall (otherwise keyword recall is used).
	BrainDir        string
	BrainChromaURL  string
	BrainEmbedURL   string
	BrainEmbedModel string
	BrainEmbedKey   string
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
	brainAuto      *brain.Automation
	allowedOrigins map[string]bool
}

// New creates a new Server with all routes registered.
func New(queries *store.Queries, cfg Config) (*Server, error) {
	mux := http.NewServeMux()
	bus := eventbus.New()

	var connector runtime.CLIConnector
	var runner session.BlockingRunner
	var testConnector *testmode.Connector

	if cfg.TestMode {
		testConnector = testmode.NewConnector()
		connector = testConnector
		runner = testmode.NewBlockingRunner()
		slog.Info("test mode enabled: using mock CLI connector")
	} else {
		connector = claudeadapter.NewConnector(
			claudecli.WithIncludePartialMessages(),
			claudecli.WithReplayUserMessages(),
		)
		runner = session.RealBlockingRunner()
	}

	devStore := devurls.NewStore(toAgentkitSlots(cfg.DevURLSlots))
	mcpTokens := mcphttp.NewTokenStore()

	mgr := session.NewManager(cfg.DB, queries, bus, connector)
	if !cfg.TestMode {
		mgr.SetProviderConnector("codex", codexadapter.NewConnector())
	}
	mgr.SetMCPHTTP(mcpTokens, cfg.MCPInternalURL)
	mgr.SetDevURLStore(devStore)
	if cfg.DevMode && cfg.DBPath != "" {
		mgr.GlobalPreamble = devModePreamble(cfg.DBPath)
	}
	mgr.RecoverStaleSessions(context.Background())
	svc := session.NewService(mgr, queries, bus, runner)
	gitSvc := session.NewGitService(mgr, queries, bus, runner)
	svc.SetGitService(gitSvc)

	var browserSvc *session.BrowserService
	if cfg.ExperimentalBrowser {
		browserMgr := browser.NewManager()
		browserSvc = session.NewBrowserService(mgr, browserMgr, bus)
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

	subscribe := func() (<-chan eventbus.Event, func()) {
		ch := make(chan eventbus.Event, 64)
		sub := bus.SubscribeAll(eventbus.NewChannelSubscriber(ch))
		unsubscribe := func() {
			sub.Unsubscribe()
			close(ch)
		}
		return ch, unsubscribe
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

	sth := &storage.Handler{Queries: queries}
	mux.HandleFunc("GET /api/storage/disk", sth.HandleDisk)
	mux.HandleFunc("GET /api/storage/usage", sth.HandleUsage)
	mux.HandleFunc("DELETE /api/storage/worktrees", sth.HandleDeleteWorktree)

	projectGitSvc := project.NewGitService(queries, bus, project.RealGitOps(), runner)

	var teamSvc *team.Service
	var personaSvc *persona.Service
	if cfg.ExperimentalTeams {
		teamSvc = team.NewService(queries, bus)
		personaSvc = persona.NewService(runner, queries, bus)
		svc.SetPersonaQuerier(personaSvc)
		slog.Info("experimental teams feature enabled")
	}

	wsh := &ws.Handler{Service: svc, GitService: gitSvc, ProjectGitService: projectGitSvc, Queries: queries, Bus: bus, TeamService: teamSvc, PersonaService: personaSvc, BrowserService: browserSvc}
	mux.Handle("GET /ws", wsh)

	// Persistent agent memory ("the brain"). Optional: enabled when BrainDir is
	// set. Failure to initialize must not take down the server — memory is an
	// enhancement, so we log and continue without it.
	var memProvider mcphttp.MemoryStore
	var brainAuto *brain.Automation
	if cfg.BrainDir != "" {
		brainSvc, err := brain.New(context.Background(), brain.Config{
			Dir:         cfg.BrainDir,
			ChromaURL:   cfg.BrainChromaURL,
			EmbedURL:    cfg.BrainEmbedURL,
			EmbedModel:  cfg.BrainEmbedModel,
			EmbedAPIKey: cfg.BrainEmbedKey,
		})
		if err != nil {
			slog.Error("brain: disabled (init failed)", "error", err)
		} else {
			scopeResolver := func(ctx context.Context, sessionID string) memory.Scope {
				s, err := queries.GetSession(ctx, sessionID)
				if err != nil {
					return memory.ScopeGlobal
				}
				return brain.ScopeForProject(s.ProjectID)
			}
			mcpAdapter := brain.NewMCPAdapter(brainSvc, scopeResolver)
			mcpAdapter.SetBus(bus)
			memProvider = mcpAdapter

			bh := &brain.Handler{Service: brainSvc, Runner: runner, Bus: bus}
			mux.Handle("GET /api/brain/memories", httperror.HandlerFunc(bh.HandleList))
			mux.Handle("POST /api/brain/memories", httperror.HandlerFunc(bh.HandleCreate))
			mux.Handle("GET /api/brain/memories/{id}", httperror.HandlerFunc(bh.HandleGet))
			mux.Handle("PUT /api/brain/memories/{id}", httperror.HandlerFunc(bh.HandleUpdate))
			mux.Handle("DELETE /api/brain/memories/{id}", httperror.HandlerFunc(bh.HandleDelete))
			mux.Handle("POST /api/brain/memories/{id}/pin", httperror.HandlerFunc(bh.HandlePin))
			mux.Handle("POST /api/brain/memories/{id}/lock", httperror.HandlerFunc(bh.HandleLock))
			mux.Handle("POST /api/brain/memories/{id}/confirm", httperror.HandlerFunc(bh.HandleConfirm))
			mux.Handle("POST /api/brain/memories/{id}/flag", httperror.HandlerFunc(bh.HandleFlag))
			mux.Handle("POST /api/brain/memories/{id}/refine", httperror.HandlerFunc(bh.HandleRefine))
			mux.Handle("GET /api/brain/search", httperror.HandlerFunc(bh.HandleSearch))
			mux.Handle("GET /api/brain/graph", httperror.HandlerFunc(bh.HandleGraph))
			mux.Handle("POST /api/brain/consolidate", httperror.HandlerFunc(bh.HandleConsolidate))
			mux.Handle("POST /api/brain/consolidate/preview", httperror.HandlerFunc(bh.HandlePreviewConsolidate))
			mux.Handle("POST /api/brain/consolidate/apply", httperror.HandlerFunc(bh.HandleApplyConsolidate))
			mux.Handle("POST /api/brain/consolidate/global/preview", httperror.HandlerFunc(bh.HandlePreviewGlobal))
			mux.Handle("POST /api/brain/consolidate/global/apply", httperror.HandlerFunc(bh.HandleApplyGlobal))
			mux.Handle("POST /api/brain/consolidate/all", httperror.HandlerFunc(bh.HandleTidyAll))
			mux.Handle("GET /api/brain/consolidate/job", httperror.HandlerFunc(bh.HandleConsolidateJob))
			mux.Handle("GET /api/brain/status", httperror.HandlerFunc(bh.HandleStatus))
			slog.Info("brain: enabled", "dir", cfg.BrainDir, "semantic", brainSvc.SemanticEnabled())

			// --- Memory automation (the recall → encode → consolidate loop) ---

			// Auto-recall (default on): inject pinned facts into every session's
			// system preamble so the brain shapes behaviour without the agent having
			// to call MemorySearch. Disable with AGENTIQUE_BRAIN_RECALL=off.
			if v := os.Getenv("AGENTIQUE_BRAIN_RECALL"); v != "off" && v != "false" && v != "0" {
				mgr.MemoryPreambleFn = brainSvc.PinnedPreamble
				mgr.MemoryRecallFn = brainSvc.RecallBlock
				slog.Info("brain: auto-recall enabled (pinned facts in preamble + task-relevant recall on the first turn)")
			}

			// Auto-encode (opt-in): distill durable memories from a finished session's
			// transcript when it's deleted. AGENTIQUE_BRAIN_LEARN_MODEL=haiku|sonnet|opus.
			if lm := os.Getenv("AGENTIQUE_BRAIN_LEARN_MODEL"); lm != "" {
				if m, perr := brain.ParseModel(lm); perr != nil {
					slog.Warn("brain: auto-encode disabled (bad model)", "model", lm, "error", perr)
				} else {
					ex := brain.NewClaudeExtractor(runner, m)
					svc.SetOnSessionEnd(func(projectID string, events []store.SessionEvent) {
						tevents := make([]brain.TranscriptEvent, len(events))
						for i, e := range events {
							tevents[i] = brain.TranscriptEvent{Type: e.Type, Data: e.Data}
						}
						n, lerr := brainSvc.LearnFromTranscript(context.Background(), brain.ScopeForProject(projectID), tevents, ex)
						if lerr != nil {
							slog.Warn("brain: auto-encode failed", "project", projectID, "error", lerr)
							return
						}
						if n > 0 {
							slog.Info("brain: learned from ended session", "project", projectID, "facts", n)
							bus.Broadcast(brain.EventBrainUpdated, map[string]string{})
						}
					})
					slog.Info("brain: auto-encode enabled", "model", lm)
				}
			}

			// Scheduled sleep (opt-in): periodic consolidation across all scopes.
			// AGENTIQUE_BRAIN_SLEEP_INTERVAL=6h (+ optional AGENTIQUE_BRAIN_SLEEP_MODEL).
			if iv := os.Getenv("AGENTIQUE_BRAIN_SLEEP_INTERVAL"); iv != "" {
				if d, derr := time.ParseDuration(iv); derr != nil || d <= 0 {
					slog.Warn("brain: sleep scheduler off (bad interval)", "value", iv, "error", derr)
				} else {
					var sm claudecli.Model
					if smName := os.Getenv("AGENTIQUE_BRAIN_SLEEP_MODEL"); smName != "" {
						if m, merr := brain.ParseModel(smName); merr == nil {
							sm = m
						} else {
							slog.Warn("brain: sleep model invalid; deterministic dedup only", "model", smName, "error", merr)
						}
					}
					brainAuto = brain.NewAutomation(brainSvc, runner, bus, d, sm)
					brainAuto.Start()
				}
			}
		}
	}

	mcpHandler := mcphttp.NewHandler(mcpTokens, devStore, svc, memProvider)
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

	s := &Server{mux: mux, mgr: mgr, svc: svc, browserSvc: browserSvc, brainAuto: brainAuto}

	if cfg.AuthEnabled {
		authSvc, err := auth.NewService(queries, cfg.RPID, cfg.RPOrigins)
		if err != nil {
			return nil, fmt.Errorf("auth service: %w", err)
		}
		authSvc.RegisterRoutes(mux)
		authSvc.RegisterUserRoutes(mux)
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
	if s.brainAuto != nil {
		s.brainAuto.Stop()
	}
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
				httperror.RespondError(w, httperror.Forbidden("origin not allowed"))
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

// toAgentkitSlots converts agentique config slots to agentkit/devurls slots,
// decoupling agentkit from the agentique config package. Field shape is
// identical; this is a structural copy.
func toAgentkitSlots(in []config.DevURLSlot) []devurls.Slot {
	if len(in) == 0 {
		return nil
	}
	out := make([]devurls.Slot, len(in))
	for i, s := range in {
		out[i] = devurls.Slot{Slot: s.Slot, Port: s.Port, PublicHost: s.PublicHost}
	}
	return out
}

// requestLogger emits one access-log line per HTTP request at debug level.
// Status-based severity and error details are owned by httperror.RespondError
// — so migrated handlers produce a richer "http error" log at warn/error
// level alongside this trace.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip logging for WebSocket upgrades (logged in ws package).
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		slog.Log(r.Context(), slog.LevelDebug, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start),
		)
	})
}
