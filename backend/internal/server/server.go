package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/allbin/agentkit/devurls"
	"github.com/allbin/agentkit/eventbus"
	"github.com/allbin/agentkit/runtime"
	claudeadapter "github.com/allbin/agentkit/runtime/cli/claude"
	codexadapter "github.com/allbin/agentkit/runtime/cli/codex"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/brain"
	"github.com/mdjarv/agentique/backend/internal/browser"
	"github.com/mdjarv/agentique/backend/internal/claudeaccount"
	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/filebrowser"
	"github.com/mdjarv/agentique/backend/internal/filesystem"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/httpsecurity"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/persona"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/prompttemplate"
	"github.com/mdjarv/agentique/backend/internal/schedule"
	"github.com/mdjarv/agentique/backend/internal/service"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/storage"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/team"
	"github.com/mdjarv/agentique/backend/internal/testmode"
	"github.com/mdjarv/agentique/backend/internal/update"
	"github.com/mdjarv/agentique/backend/internal/voice"
	"github.com/mdjarv/agentique/backend/internal/ws"
)

// Config holds server configuration.
type Config struct {
	AuthEnabled bool
	RPID        string
	RPOrigins   []string

	// MachineID is this server's stable identity (internal/machine), served
	// unauthenticated at /.well-known/agentique/environment so clients can
	// probe and pin it. Empty in tests that don't care about identity.
	MachineID string
	// MachineIdentity signs fresh client challenges. It is loaded from the data
	// directory by serve.go and pinned by clients during pairing.
	MachineIdentity *machine.SigningIdentity
	// MachineHTTPClient performs identity verification and bearer revocation
	// when a remote machine is removed. Nil uses a bounded no-redirect client.
	MachineHTTPClient *http.Client
	// MachineLabel is the human-friendly machine name shown in clients — the
	// boot default (env, else config, else hostname). A name set from the UI
	// is stored in host_presentation and wins over it; see effectiveLabel.
	MachineLabel string
	// MachineLabelPinned reports that MachineLabel came from
	// AGENTIQUE_MACHINE_LABEL. An operator who set the env var meant it, so
	// the UI can neither override it nor pretend it did.
	MachineLabelPinned bool
	// ListenPort is this server's listen port — the first port candidate when
	// probing tailnet peers for discovery (peers tend to mirror each other's
	// setup).
	ListenPort string
	// Version is the build version string ("dev" for non-release builds).
	Version string
	// Update configures the in-app upgrade check (docs/upgrades.md). The
	// checker is constructed here but never started here — serve.go's
	// production block runs the poll loop.
	Update config.UpdateConfig
	// AdminSecret arms the data-dir-secret auth path for the pairing and
	// session-management endpoints (CLI `agentique pair`). Empty = disabled.
	AdminSecret string

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
	// ExperimentalVoice enables the live spoken-dialog composer mode and mounts
	// its socket. Voice below selects which speech backend that socket talks to.
	ExperimentalVoice bool
	// Voice configures the speech backend behind ExperimentalVoice. With the
	// flag on and no credentials configured, the socket serves the loopback echo
	// used to verify the audio path and contacts nothing.
	Voice config.VoiceConfig

	// IdleEvictTimeout, when > 0, stops a session idle at least this long to
	// reclaim its CLI process and browser subtree (it resumes on the next
	// message). 0 disables idle eviction. Resolved from [session]
	// idle-evict-timeout (or AGENTIQUE_SESSION_IDLE_EVICT_TIMEOUT).
	IdleEvictTimeout time.Duration

	// Claude carries connector-wide flags for the claude provider, resolved
	// from the [claude] config section with AGENTIQUE_CLAUDE_* env overrides.
	Claude config.ClaudeConfig

	// SchedulerDisabled turns scheduled loops off entirely (schedules persist
	// but never fire). Resolved from [scheduler] disabled
	// (or AGENTIQUE_SCHEDULER_DISABLED).
	SchedulerDisabled bool
	// SchedulerOptions tunes the scheduled-loop service; zero values use the
	// documented defaults (docs/scheduled-loops.md).
	SchedulerOptions schedule.Options

	// DevURLSlots is the configured pool of leasable dev URL slots. Empty
	// disables the AcquireDevUrl tool path (slots will report all-busy).
	DevURLSlots []config.DevURLSlot

	// ModelOverrides replaces the auto-detected model catalog for a provider,
	// keyed by provider name. Empty leaves auto-detection in charge.
	ModelOverrides map[string][]config.ModelOverride

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
	// BrainSemanticThreshold overrides the cosine link threshold for semantic area
	// clustering (model-specific; 0 = default). Inert without an embedder.
	BrainSemanticThreshold float64
	// BrainVectorVeto overrides the hybrid-recall vector veto floor (model-specific;
	// 0 = default). Inert without an embedder.
	BrainVectorVeto float64
	// BrainCalibrate derives the semantic thresholds from the live corpus's own cosine
	// distribution at boot instead of the hand-set defaults (model-specific auto-
	// calibration). An explicit BrainSemanticThreshold/BrainVectorVeto still wins.
	// Inert without an embedder.
	BrainCalibrate bool
	// BrainRecall toggles auto-recall (pinned facts + per-turn task-relevant facts in the
	// preamble). Default on; resolved from AGENTIQUE_BRAIN_RECALL (env, wins) or [brain]
	// recall (config file), off only when explicitly disabled.
	BrainRecall bool
	// BrainConsolidateInterval enables scheduled (automatic) consolidation across all
	// scopes when set to a positive duration (e.g. "6h"); empty disables it. Resolved from
	// the AGENTIQUE_BRAIN_CONSOLIDATE_INTERVAL env var (preferred) or the [brain]
	// consolidate-interval config-file value.
	BrainConsolidateInterval string
	// BrainConsolidateModel is the model scheduled consolidation uses for LLM
	// reorganization; empty = deterministic dedup/decay only. From
	// AGENTIQUE_BRAIN_CONSOLIDATE_MODEL or [brain] consolidate-model.
	BrainConsolidateModel string
	// BrainLearnModel enables session-end auto-encode (distill durable facts from a
	// finished transcript) when set to haiku|sonnet|opus; empty disables it. From
	// AGENTIQUE_BRAIN_LEARN_MODEL or [brain] learn-model.
	BrainLearnModel string
	// BrainOutcomeModel enables the session-end automatic outcome emitter — judge from the
	// transcript whether recalled facts helped or were contradicted, feeding
	// MarkAutoHelped/Flag — when set to haiku|sonnet|opus; empty disables it. From
	// AGENTIQUE_BRAIN_OUTCOME_MODEL or [brain] outcome-model.
	BrainOutcomeModel string
	// BrainGraph tunes the knowledge-graph view: semantic kNN edge density (backend) and the
	// force-layout curves (frontend). Resolved from [brain.graph] with AGENTIQUE_BRAIN_GRAPH_*
	// env overrides; zero fields take brain's built-in defaults.
	BrainGraph config.BrainGraphConfig
	// BrainSnapshotRetain bounds how many pre-churn brain snapshots are kept. From
	// AGENTIQUE_BRAIN_SNAPSHOT_RETAIN or [brain] snapshot-retain; 0 = brain's default (7).
	BrainSnapshotRetain int
	// BrainArchiveAfter enables disuse-aging archival when set to a positive duration string
	// (e.g. "720h"); "" disables it. From AGENTIQUE_BRAIN_ARCHIVE_AFTER or [brain] archive-after.
	BrainArchiveAfter string
	// BrainArchiveFloor is the effective-confidence floor below which a faded fact is archived/
	// faded from recall. From AGENTIQUE_BRAIN_ARCHIVE_FLOOR or [brain] archive-confidence-floor;
	// 0 = brain's default (0.35).
	BrainArchiveFloor float64
	// BrainRetryMax bounds session-end learn/outcome job retries before dead-lettering. From
	// AGENTIQUE_BRAIN_RETRY_MAX or [brain] retry-max; 0 = brain's default (5).
	BrainRetryMax int
}

// serviceInstalled reports whether a service manager would bring agentique
// back after a restart. Without one, replacing the binary would just stop it.
func serviceInstalled() bool {
	st, err := service.GetStatus()
	return err == nil && st.Installed
}

// parseUpdateInterval reads the [update] interval; an empty value means "take
// the default" and a bad one is reported but never fatal — a mistyped check
// interval must not stop the server from serving.
func parseUpdateInterval(v string) (time.Duration, error) {
	if v == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("interval must be positive, got %s", v)
	}
	return d, nil
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
	scheduler      *schedule.Scheduler
	updateChecker  *update.Checker
	updateApplier  *update.Applier
	updateCLIs     *update.CLIProbe
	allowedOrigins map[string]bool
	authEnabled    bool
	// csp is the SPA document policy, computed once from the embedded bundle
	// (the inline bootstrap script is allowed by hash, not by 'unsafe-inline').
	csp string
}

// UpdateChecker exposes the version checker so serve.go can start its poll
// loop (same precedent as Scheduler — no network or filesystem work runs from
// a constructor a test might call). Nil when checking is disabled.
func (s *Server) UpdateChecker() *update.Checker { return s.updateChecker }

// UpdateCLIProbe exposes the provider-CLI probe so serve.go can start its poll
// loop, for the same reason UpdateChecker is exposed: detection spawns
// `--version`, and nothing that touches a subprocess may run from a
// constructor a test might call. Nil when update checking is disabled.
func (s *Server) UpdateCLIProbe() *update.CLIProbe { return s.updateCLIs }

// Scheduler exposes the scheduled-loop service so serve.go can run the boot
// sweep and start the tick loop (deliberately not started in New — see the
// SweepOrphans precedent). Nil when the scheduler is disabled.
func (s *Server) Scheduler() *schedule.Scheduler { return s.scheduler }

// New creates a new Server with all routes registered.
func New(queries *store.Queries, cfg Config) (*Server, error) {
	mux := http.NewServeMux()
	bus := eventbus.New()
	allowedOrigins := make(map[string]bool, len(cfg.RPOrigins))
	for _, origin := range cfg.RPOrigins {
		allowedOrigins[origin] = true
	}
	machineHTTPClient := cfg.MachineHTTPClient
	if machineHTTPClient == nil {
		machineHTTPClient = &http.Client{Timeout: 10 * time.Second}
	} else {
		copy := *machineHTTPClient
		machineHTTPClient = &copy
		if machineHTTPClient.Timeout == 0 {
			machineHTTPClient.Timeout = 10 * time.Second
		}
	}
	machineHTTPClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	var connector runtime.CLIConnector
	var runner session.BlockingRunner
	var testConnector *testmode.Connector

	if cfg.TestMode {
		testConnector = testmode.NewConnector()
		connector = testConnector
		runner = testmode.NewBlockingRunner()
		slog.Info("test mode enabled: using mock CLI connector")
	} else {
		claudeOpts := []claudecli.Option{
			claudecli.WithIncludePartialMessages(),
			claudecli.WithReplayUserMessages(),
		}
		// [claude] flags. Both are additive and default to the CLI's own
		// behavior, so an unset section leaves the connector exactly as it was.
		if cfg.Claude.ExcludeDynamicSystemPromptSections {
			claudeOpts = append(claudeOpts, claudecli.WithExcludeDynamicSystemPromptSections())
		}
		if cfg.Claude.AutoCompact != "" {
			claudeOpts = append(claudeOpts, claudecli.WithAutoCompact(cfg.Claude.AutoCompact))
		}
		if cfg.Claude.ForwardSubagentText {
			claudeOpts = append(claudeOpts, claudecli.WithForwardSubagentText())
		}
		connector = claudeadapter.NewConnector(claudeOpts...)
		runner = session.RealBlockingRunner()
	}

	devStore := devurls.NewStore(toAgentkitSlots(cfg.DevURLSlots))
	mcpTokens := mcphttp.NewTokenStore()

	// The connectors double as the answer to "which CLI would this machine
	// actually spawn": each one owns its client options, so it is the only
	// thing that stays correct if a binary path is ever overridden. Collected
	// here rather than re-derived, so detection and execution cannot drift
	// apart (docs/upgrades.md C13). A connector that cannot answer is simply
	// absent — the test-mode connector never implements this.
	cliInspectors := map[string]runtime.InstallInspectable{}
	if in, ok := connector.(runtime.InstallInspectable); ok {
		cliInspectors["claude"] = in
	}
	mgr := session.NewManager(cfg.DB, queries, bus, connector)
	if !cfg.TestMode {
		codexConnector := codexadapter.NewConnector()
		mgr.SetProviderConnector("codex", codexConnector)
		if in, ok := codexConnector.(runtime.InstallInspectable); ok {
			cliInspectors["codex"] = in
		}
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
	if cfg.IdleEvictTimeout > 0 {
		svc.SetIdleEvictTimeout(cfg.IdleEvictTimeout)
	}

	// The agent browser (headless Playwright MCP) is always available — it
	// launches lazily on first tool use. The experimental flag only gates the
	// human-facing browser panel (the live screencast view).
	browserMgr := browser.NewManager()
	browserSvc := session.NewBrowserService(mgr, browserMgr, bus)
	svc.SetBrowserService(browserSvc)
	svc.SetBrowserPanelEnabled(cfg.ExperimentalBrowser)
	if cfg.ExperimentalBrowser {
		slog.Info("experimental browser panel enabled")
	}

	// Scheduled loops (docs/scheduled-loops.md): agentique-owned recurring
	// prompts fired into sessions as fresh turns. Constructed and wired here;
	// the boot sweep and tick loop start from serve.go (never in New).
	var sched *schedule.Scheduler
	if !cfg.SchedulerDisabled && cfg.DB != nil {
		sched = schedule.NewScheduler(cfg.DB, queries, schedule.NewSessionGateway(svc, queries), bus.Broadcast, cfg.SchedulerOptions)
		svc.SetOnSessionFinished(sched.OnSessionFinished)
		mgr.OnSessionIdle = sched.OnSessionIdle
	}

	ph := &project.Handler{Queries: queries}

	// How this host presents itself (docs/multi-machine.md). Presentation is
	// local: the name and icon are what THIS host shows, stored in its own DB
	// and never pushed to or pulled from a peer. Read per request rather than
	// captured at boot, so a rename takes effect without a restart.
	//
	// Precedence: AGENTIQUE_MACHINE_LABEL (pinned by an operator who meant it)
	// → the name set from the UI → config file → hostname. A pinned label is
	// reported as such, so the UI can show that an edit would do nothing
	// rather than accepting a write that silently loses.
	hostPresentation := func(ctx context.Context) (label string, icon string) {
		label = cfg.MachineLabel
		if cfg.MachineLabelPinned {
			return label, ""
		}
		row, err := queries.GetHostPresentation(ctx)
		if err != nil {
			return label, "" // unset (no row) or unreadable — the boot default stands
		}
		if row.Label != "" {
			label = row.Label
		}
		return label, row.Icon
	}

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		machineLabel, machineIcon := hostPresentation(r.Context())
		httperror.JSON(w, http.StatusOK, map[string]any{
			"status":             "ok",
			"machineId":          cfg.MachineID,
			"machineLabel":       machineLabel,
			"machineIcon":        machineIcon,
			"machineLabelPinned": cfg.MachineLabelPinned,
			"version":            cfg.Version,
			"features": map[string]bool{
				"browser": cfg.ExperimentalBrowser,
				"teams":   cfg.ExperimentalTeams,
				"voice":   cfg.ExperimentalVoice,
			},
		})
	})
	requireFullAccess := func(w http.ResponseWriter, r *http.Request) bool {
		if !cfg.AuthEnabled {
			return true
		}
		session := auth.UserFromContext(r.Context())
		if session == nil || session.IsAdmin == 0 {
			httperror.RespondError(w, httperror.Forbidden("full-access operator required"))
			return false
		}
		return true
	}

	// In-app upgrades (docs/upgrades.md): each server checks for ITSELF —
	// only it knows its platform, its install method and whether it is busy.
	// Constructed here so the route exists; the poll loop starts from serve.go.
	var updateChecker *update.Checker
	var updateApplier *update.Applier
	var updateCLIs *update.CLIProbe
	if !cfg.Update.Disabled {
		interval, ierr := parseUpdateInterval(cfg.Update.Interval)
		if ierr != nil {
			slog.Warn("update: bad [update] interval, using default", "value", cfg.Update.Interval, "error", ierr)
		}
		updateChecker = update.NewChecker(update.Options{
			Version:  cfg.Version,
			APIURL:   cfg.Update.APIURL,
			Interval: interval,
		})
		// Applying is the machine's own business: it replaces its own binary
		// and restarts its own service. Busy comes from the turn registry —
		// a restart is not a pause (docs/upgrades.md, docs/process-lifecycle.md).
		armDeadline, aerr := parseUpdateInterval(cfg.Update.ArmDeadline)
		if aerr != nil {
			slog.Warn("update: bad [update] arm-deadline, using default", "value", cfg.Update.ArmDeadline, "error", aerr)
		}
		applier := update.NewApplier(updateChecker, update.Deps{
			BinaryPath:       service.BinaryPath,
			Restart:          service.Restart,
			ServiceInstalled: serviceInstalled,
			BusyTurns:        mgr.BusyTurns,
			Publish:          bus.Broadcast,
			MachineID:        cfg.MachineID,
			ArmDeadline:      armDeadline,
		})
		// The drain gate fires on turn END, not on the idle transition: the
		// runtime flips Idle before the pipeline drains the completion, so an
		// idle-time check would still see that very turn as in flight.
		mgr.AddTurnEndListener(applier.OnTurnEnd)
		updateApplier = applier
		// CLI detection follows the same switch as the release check: turning
		// [update] off silences all of it, not just the part about ourselves.
		updateCLIs = update.NewCLIProbe(cliInspectors, interval)
		// The version a CLI reports when a session starts is the only account of
		// it that comes from something that happened rather than from inspecting
		// a binary — the one check on detection being right.
		mgr.SetOnCLIVersion(updateCLIs.RecordRan)
		uh := &update.Handler{Checker: updateChecker, Applier: applier, CLIs: updateCLIs}
		mux.HandleFunc("GET /api/update/status", uh.HandleStatus)
		// Applying replaces this machine's binary and restarts its service, and
		// `force` ends every turn in flight (docs/process-lifecycle.md). That is
		// at least as privileged as reading the machine catalog, so it takes the
		// same guard — reading the status does not.
		mux.HandleFunc("POST /api/update/apply", func(w http.ResponseWriter, r *http.Request) {
			if !requireFullAccess(w, r) {
				return
			}
			uh.HandleApply(w, r)
		})
		mux.HandleFunc("DELETE /api/update/apply", func(w http.ResponseWriter, r *http.Request) {
			if !requireFullAccess(w, r) {
				return
			}
			uh.HandleCancel(w, r)
		})
	}

	// Machine catalog (multi-machine): paired machines are ACCOUNT state, not
	// device state — a phone PWA and a desktop logging into this primary see
	// the same machines. Clients cache public metadata locally but reload bearer
	// credentials from this full-access endpoint into memory.
	// Auth-guarded like all /api routes; the rows carry each remote's bearer
	// token, peer material to the auth sessions already stored in this DB.
	mux.HandleFunc("GET /api/machines", func(w http.ResponseWriter, r *http.Request) {
		if !requireFullAccess(w, r) {
			return
		}
		rows, err := queries.ListMachines(r.Context())
		if err != nil {
			httperror.RespondError(w, httperror.Internal("list machines", err))
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, m := range rows {
			out = append(out, map[string]any{
				"machineId":   m.MachineID,
				"label":       m.Label,
				"baseUrl":     m.BaseUrl,
				"token":       m.Token,
				"addedAt":     m.AddedAt,
				"icon":        m.Icon,
				"sessionId":   m.SessionID,
				"identityKey": m.IdentityKey,
			})
		}
		httperror.JSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("PUT /api/machines/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireFullAccess(w, r) {
			return
		}
		var req struct {
			Label       string `json:"label"`
			BaseURL     string `json:"baseUrl"`
			Token       string `json:"token"`
			SessionID   string `json:"sessionId"`
			IdentityKey string `json:"identityKey"`
			AddedAt     string `json:"addedAt"`
			Icon        string `json:"icon"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httperror.RespondError(w, httperror.BadRequest("invalid request body"))
			return
		}
		machineID := r.PathValue("id")
		if uuid.Validate(machineID) != nil || machineID == cfg.MachineID {
			httperror.RespondError(w, httperror.BadRequest("a remote machine UUID is required"))
			return
		}
		if req.BaseURL == "" || req.Token == "" || req.SessionID == "" || req.IdentityKey == "" {
			httperror.RespondError(w, httperror.BadRequest("baseUrl, token, sessionId, and identityKey are required"))
			return
		}
		baseURL, err := machine.NormalizeRemoteBaseURL(req.BaseURL)
		if err != nil {
			httperror.RespondError(w, httperror.BadRequest(err.Error()))
			return
		}
		if len([]rune(req.Label)) > 64 || len([]rune(req.Icon)) > 64 || len(req.Token) > 512 || len(req.SessionID) > 128 || len(req.IdentityKey) > 1024 {
			httperror.RespondError(w, httperror.BadRequest("machine catalog field is too long"))
			return
		}
		if err := machine.ValidateIdentityPublicKey(req.IdentityKey); err != nil {
			httperror.RespondError(w, httperror.BadRequest("identityKey is invalid"))
			return
		}
		if req.AddedAt == "" {
			req.AddedAt = time.Now().UTC().Format(time.RFC3339)
		} else if _, err := time.Parse(time.RFC3339, req.AddedAt); err != nil {
			httperror.RespondError(w, httperror.BadRequest("addedAt must be RFC3339"))
			return
		}
		if err := queries.UpsertMachine(r.Context(), store.UpsertMachineParams{
			MachineID:   machineID,
			Label:       req.Label,
			BaseUrl:     baseURL,
			Token:       req.Token,
			AddedAt:     req.AddedAt,
			Icon:        req.Icon,
			SessionID:   req.SessionID,
			IdentityKey: req.IdentityKey,
		}); err != nil {
			httperror.RespondError(w, httperror.Internal("save machine", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PATCH /api/machines/{id}/presentation", func(w http.ResponseWriter, r *http.Request) {
		if !requireFullAccess(w, r) {
			return
		}
		var req struct {
			Label string `json:"label"`
			Icon  string `json:"icon"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httperror.RespondError(w, httperror.BadRequest("invalid request body"))
			return
		}
		if len([]rune(req.Label)) > 64 || len([]rune(req.Icon)) > 64 {
			httperror.RespondError(w, httperror.BadRequest("label and icon must be 64 characters or fewer"))
			return
		}
		n, err := queries.UpdateMachinePresentation(r.Context(), store.UpdateMachinePresentationParams{
			MachineID: r.PathValue("id"), Label: req.Label, Icon: req.Icon,
		})
		if err != nil {
			httperror.RespondError(w, httperror.Internal("save machine presentation", err))
			return
		}
		if n == 0 {
			httperror.RespondError(w, httperror.NotFound("machine not found"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// This host's own name and icon. Deliberately NOT a row in `machines`:
	// every client consumer of that catalog treats an entry as a reachable
	// remote (opens a socket to base_url, fetches with its token), so self
	// belongs beside the catalog, not in it.
	mux.HandleFunc("PUT /api/machine/presentation", func(w http.ResponseWriter, r *http.Request) {
		// This rewrites how the host identifies itself to every client, so it
		// takes the same guard as its /api/machines/{id}/presentation sibling.
		if !requireFullAccess(w, r) {
			return
		}
		var req struct {
			Label string `json:"label"`
			Icon  string `json:"icon"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httperror.RespondError(w, httperror.BadRequest("label/icon body required"))
			return
		}
		req.Label = strings.TrimSpace(req.Label)
		if len([]rune(req.Label)) > 64 || len([]rune(req.Icon)) > 64 {
			httperror.RespondError(w, httperror.BadRequest("label and icon must be 64 characters or fewer"))
			return
		}
		if err := queries.SetHostPresentation(r.Context(), store.SetHostPresentationParams{
			Label: req.Label,
			Icon:  req.Icon,
		}); err != nil {
			httperror.RespondError(w, httperror.Internal("save machine presentation", err))
			return
		}
		label, icon := hostPresentation(r.Context())
		httperror.JSON(w, http.StatusOK, map[string]any{
			"machineLabel":       label,
			"machineIcon":        icon,
			"machineLabelPinned": cfg.MachineLabelPinned,
		})
	})
	mux.HandleFunc("DELETE /api/machines/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireFullAccess(w, r) {
			return
		}
		machineID := r.PathValue("id")
		entry, err := queries.GetMachine(r.Context(), machineID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				httperror.RespondError(w, httperror.NotFound("machine not found"))
				return
			}
			httperror.RespondError(w, httperror.Internal("load machine", err))
			return
		}
		if err := machine.RevokeRemoteBearer(r.Context(), machineHTTPClient, entry.BaseUrl, entry.MachineID, entry.IdentityKey, entry.Token); err != nil {
			httperror.RespondError(w, httperror.BadGateway("remote credential could not be revoked; the machine was not removed", err))
			return
		}
		if err := queries.DeleteMachine(r.Context(), machineID); err != nil {
			httperror.RespondError(w, httperror.Internal("delete machine", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Tailnet peer discovery (multi-machine): probe online tailnet peers
	// for agentique descriptors so Add-machine can suggest them. A hint layer
	// only — pairing still authorizes. Auth-guarded like all /api routes.
	mux.HandleFunc("GET /api/machines/discover", func(w http.ResponseWriter, r *http.Request) {
		if !requireFullAccess(w, r) {
			return
		}
		peers := machine.DiscoverPeers(r.Context(), cfg.MachineID, cfg.ListenPort)
		if peers == nil {
			peers = []machine.DiscoveredPeer{}
		}
		httperror.JSON(w, http.StatusOK, peers)
	})

	// Machine descriptor (docs/multi-machine.md): unauthenticated by
	// design — it is the universal "is an agentique server here, and which
	// one" probe. Clients pin MachineID and the signing key, then verify a
	// fresh signed challenge before sending credentials on every connect.
	// Capabilities are optional booleans; clients treat a missing key as
	// unsupported, so features degrade per-feature without version checks.
	mux.HandleFunc("GET /.well-known/agentique/environment", func(w http.ResponseWriter, r *http.Request) {
		label, _ := hostPresentation(r.Context())
		identityKey := ""
		if cfg.MachineIdentity != nil {
			identityKey = cfg.MachineIdentity.PublicKey()
		}
		httperror.JSON(w, http.StatusOK, map[string]any{
			"machineId":   cfg.MachineID,
			"identityKey": identityKey,
			"label":       label,
			"version":     cfg.Version,
			"platform": map[string]string{
				"os":   goruntime.GOOS,
				"arch": goruntime.GOARCH,
			},
			"capabilities": map[string]bool{
				"pairing": cfg.AuthEnabled,
				"browser": cfg.ExperimentalBrowser,
				"teams":   cfg.ExperimentalTeams,
				"voice":   cfg.ExperimentalVoice,
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

	wsh := &ws.Handler{Service: svc, GitService: gitSvc, ProjectGitService: projectGitSvc, Queries: queries, Bus: bus, TeamService: teamSvc, PersonaService: personaSvc, BrowserService: browserSvc, ScheduleService: sched, Catalog: modelCatalog(queries, cfg.ModelOverrides), AllowedOrigins: allowedOrigins, AllowTicketOrigin: cfg.AuthEnabled}
	mux.Handle("GET /ws", wsh)

	// Live voice. Mounted under /api/ deliberately: the auth middleware
	// protects the /api/ prefix and the exact string "/ws", so a socket at
	// /ws/voice would fall through as an SPA asset and stream a live microphone
	// with no credential. auth.wsUpgradePaths must list this path too, or a
	// cross-origin paired machine — which has no cookie — cannot connect.
	//
	// A misconfigured [voice] section disables the feature; it never takes down
	// the server, on the same principle as the brain below.
	//
	// voiceRegistry routes a worker's progress reports into the calls following
	// it, and is shared with the MCP handler below so VoiceReport can reach it.
	// It stays nil when voice is off, which is what omits the tool entirely.
	var voiceRegistry *voice.Registry
	if cfg.ExperimentalVoice {
		voiceRegistry = voice.NewRegistry()
		if vh, err := newVoiceHandler(cfg, allowedOrigins, voiceRegistry); err != nil {
			slog.Error("live voice disabled: bad configuration", "error", err)
			voiceRegistry = nil
		} else {
			mux.Handle("GET /api/voice/live", vh)
			slog.Info("live voice enabled", "backend", vh.Backend())
		}
	}

	// Persistent agent memory ("the brain"). Optional: enabled when BrainDir is
	// set. Failure to initialize must not take down the server — memory is an
	// enhancement, so we log and continue without it.
	var memProvider mcphttp.MemoryStore
	var brainAuto *brain.Automation
	if cfg.BrainDir != "" {
		// Couple the read-time recall fade to archiving being ENABLED: it activates only when
		// archive-after parses to a positive duration, so a stray archive-confidence-floor can
		// never silently evict live facts from recall while the churn isn't archiving (the
		// deploy-safety contract). When enabled, an unset floor takes the built-in default.
		recallArchiveFloor := 0.0
		if d, perr := time.ParseDuration(cfg.BrainArchiveAfter); cfg.BrainArchiveAfter != "" && perr == nil && d > 0 {
			recallArchiveFloor = cfg.BrainArchiveFloor
			if recallArchiveFloor <= 0 {
				recallArchiveFloor = memory.DefaultArchiveConfidenceFloor
			}
		}
		brainSvc, err := brain.New(context.Background(), brain.Config{
			Dir:               cfg.BrainDir,
			ChromaURL:         cfg.BrainChromaURL,
			EmbedURL:          cfg.BrainEmbedURL,
			EmbedModel:        cfg.BrainEmbedModel,
			EmbedAPIKey:       cfg.BrainEmbedKey,
			SemanticThreshold: cfg.BrainSemanticThreshold,
			VectorVetoScore:   cfg.BrainVectorVeto,
			Calibrate:         cfg.BrainCalibrate,
			SnapshotRetain:    cfg.BrainSnapshotRetain,
			ArchiveFloor:      recallArchiveFloor,
			Graph: brain.GraphConfig{
				EdgeCap:          cfg.BrainGraph.EdgeCap,
				EdgeThreshold:    cfg.BrainGraph.EdgeThreshold,
				LinkStrengthBase: cfg.BrainGraph.LinkStrengthBase,
				LinkStrengthSpan: cfg.BrainGraph.LinkStrengthSpan,
				LinkDistanceBase: cfg.BrainGraph.LinkDistanceBase,
				LinkDistanceSpan: cfg.BrainGraph.LinkDistanceSpan,
				Gravity:          cfg.BrainGraph.Gravity,
			},
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
			mux.Handle("POST /api/brain/memories/{id}/restore", httperror.HandlerFunc(bh.HandleRestore))
			mux.Handle("GET /api/brain/search", httperror.HandlerFunc(bh.HandleSearch))
			mux.Handle("GET /api/brain/graph", httperror.HandlerFunc(bh.HandleGraph))
			mux.Handle("POST /api/brain/consolidate", httperror.HandlerFunc(bh.HandleConsolidate))
			mux.Handle("POST /api/brain/consolidate/preview", httperror.HandlerFunc(bh.HandlePreviewConsolidate))
			mux.Handle("POST /api/brain/consolidate/apply", httperror.HandlerFunc(bh.HandleApplyConsolidate))
			mux.Handle("POST /api/brain/consolidate/global/preview", httperror.HandlerFunc(bh.HandlePreviewGlobal))
			mux.Handle("POST /api/brain/consolidate/global/apply", httperror.HandlerFunc(bh.HandleApplyGlobal))
			mux.Handle("POST /api/brain/consolidate/all", httperror.HandlerFunc(bh.HandleConsolidateAll))
			mux.Handle("GET /api/brain/consolidate/job", httperror.HandlerFunc(bh.HandleConsolidateJob))
			mux.Handle("GET /api/brain/status", httperror.HandlerFunc(bh.HandleStatus))
			mux.Handle("GET /api/brain/snapshots", httperror.HandlerFunc(bh.HandleListSnapshots))
			mux.Handle("POST /api/brain/snapshots", httperror.HandlerFunc(bh.HandleCreateSnapshot))
			mux.Handle("POST /api/brain/snapshots/{id}/restore", httperror.HandlerFunc(bh.HandleRestoreSnapshot))
			slog.Info("brain: enabled", "dir", cfg.BrainDir, "semantic", brainSvc.SemanticEnabled())

			// --- Memory automation (the recall → encode → consolidate loop) ---

			// Auto-recall (default on): inject pinned facts into every session's
			// system preamble so the brain shapes behaviour without the agent having
			// to call MemorySearch. Resolved from AGENTIQUE_BRAIN_RECALL (env) or
			// [brain] recall (config); disable with either set to off/false/0/no.
			if cfg.BrainRecall {
				mgr.MemoryPreambleFn = brainSvc.PinnedPreamble
				mgr.MemoryRecallFn = brainSvc.RecallBlock
				// Explain the <brain> recall envelope once in the system preamble so the
				// per-turn recall block stays a compact tagged block (see RecallBlock).
				mgr.MemoryRecallPreamble = brain.RecallPreamble
				// Operating contract: high-confidence preferences become acted-on standing
				// instructions in the preamble, not just soft context (brain.md#the-outcome-signal).
				mgr.MemoryContractFn = brainSvc.OperatingContract
				slog.Info("brain: auto-recall enabled (pinned facts + operating contract in preamble + task-relevant recall on the first turn)")
			}

			// Session-end learning (opt-in): when a session is deleted, run up to two
			// best-effort transcript passes — auto-encode (distill durable facts) and the
			// automatic outcome emitter (judge whether the facts recall surfaced this session
			// actually helped or were contradicted, feeding MarkAutoHelped/Flag). Each is gated
			// by its own model (env wins over the [brain] config value, resolved in serve.go);
			// the two share the single captured transcript through one onSessionEnd hook.
			var encodeEx *brain.ClaudeExtractor
			if lm := cfg.BrainLearnModel; lm != "" {
				if m, perr := brain.ParseModel(lm); perr != nil {
					slog.Warn("brain: auto-encode disabled (bad model)", "model", lm, "error", perr)
				} else {
					encodeEx = brain.NewClaudeExtractor(runner, m)
					slog.Info("brain: auto-encode enabled", "model", lm)
				}
			}
			var outcomeJudge *brain.ClaudeOutcomeJudge
			if om := cfg.BrainOutcomeModel; om != "" {
				if m, perr := brain.ParseModel(om); perr != nil {
					slog.Warn("brain: auto-outcome emitter disabled (bad model)", "model", om, "error", perr)
				} else {
					outcomeJudge = brain.NewClaudeOutcomeJudge(runner, m)
					slog.Info("brain: auto-outcome emitter enabled", "model", om)
				}
			}
			// Durable retry queue (M7): the session-end learn/outcome passes run as durable,
			// idempotent jobs (brain_jobs) so a restart mid-extraction never loses the work —
			// drained on startup + on enqueue, retried then dead-lettered. The handler bodies call
			// the exact same Service methods as before, just routed through durability. Two
			// INDEPENDENT jobs so a learn failure can't force an outcome re-run.
			handlers := map[string]brain.JobHandler{}
			if encodeEx != nil {
				handlers[brain.JobKindLearn] = func(ctx context.Context, j brain.Job) (bool, error) {
					n, err := brainSvc.LearnFromTranscript(ctx, j.Scope, j.Events, encodeEx)
					return n > 0, err
				}
			}
			if outcomeJudge != nil {
				handlers[brain.JobKindOutcome] = func(ctx context.Context, j brain.Job) (bool, error) {
					rep, err := brainSvc.ApplyOutcomesFromTranscript(ctx, j.Scope, j.Events, outcomeJudge)
					return rep.Helped > 0 || rep.Flagged > 0, err
				}
			}
			if len(handlers) > 0 {
				jq := brain.NewJobQueue(queries, bus, cfg.BrainRetryMax, handlers)
				go jq.Drain(context.Background()) // startup recovery: resume crash-left jobs
				svc.SetOnSessionEnd(func(projectID string, events []store.SessionEvent) {
					tevents := make([]brain.TranscriptEvent, len(events))
					for i, e := range events {
						tevents[i] = brain.TranscriptEvent{Type: e.Type, Data: e.Data}
					}
					if _, ok := handlers[brain.JobKindLearn]; ok {
						if err := jq.Enqueue(context.Background(), brain.JobKindLearn, projectID, tevents); err != nil {
							slog.Warn("brain: enqueue learn job failed", "project", projectID, "error", err)
						}
					}
					if _, ok := handlers[brain.JobKindOutcome]; ok {
						if err := jq.Enqueue(context.Background(), brain.JobKindOutcome, projectID, tevents); err != nil {
							slog.Warn("brain: enqueue outcome job failed", "project", projectID, "error", err)
						}
					}
				})
				// Learn-on-completion (M3): also fire the ingest sink when a session cleanly
				// completes (StateDone), not only on delete. MUST be retained, or learn-on-
				// completion is silently disabled. M3's completion ingest flows through the same
				// svc.onSessionEnd, so it gains queue durability automatically.
				mgr.OnSessionComplete = svc.HandleSessionComplete
			}

			// Scheduled consolidation (opt-in): automatic consolidation across all scopes on
			// a timer. Resolved from AGENTIQUE_BRAIN_CONSOLIDATE_INTERVAL (env, preferred) or
			// [brain] consolidate-interval (config file); empty = off. Same for the model.
			if iv := cfg.BrainConsolidateInterval; iv != "" {
				if d, derr := time.ParseDuration(iv); derr != nil || d <= 0 {
					slog.Warn("brain: scheduled consolidation off (bad interval)", "value", iv, "error", derr)
				} else {
					var sm claudecli.Model
					if smName := cfg.BrainConsolidateModel; smName != "" {
						if m, merr := brain.ParseModel(smName); merr == nil {
							sm = m
						} else {
							slog.Warn("brain: consolidation model invalid; deterministic dedup only", "model", smName, "error", merr)
						}
					}
					// Disuse-aging archival (M5): "" archive-after = off (inert policy). A bad
					// duration logs a warning and leaves archiving disabled.
					archiveAfter := time.Duration(0)
					if aa := cfg.BrainArchiveAfter; aa != "" {
						if ad, aerr := time.ParseDuration(aa); aerr == nil && ad > 0 {
							archiveAfter = ad
						} else {
							slog.Warn("brain: archiving disabled (bad archive-after)", "value", aa, "error", aerr)
						}
					}
					brainAuto = brain.NewAutomation(brainSvc, runner, bus, d, sm, archiveAfter, cfg.BrainArchiveFloor)
					brainAuto.Start()
				}
			}
		}
	}

	// A typed-nil *Scheduler must not become a non-nil interface.
	var schedCreator mcphttp.ScheduleCreator
	if sched != nil {
		schedCreator = sched
	}
	// Same trap: a typed-nil *voice.Registry would register VoiceReport and
	// then panic on the first call.
	var voiceReporter mcphttp.VoiceReporter
	if voiceRegistry != nil {
		voiceReporter = voiceRegistry
	}
	mcpHandler := mcphttp.NewHandler(mcpTokens, devStore, svc, memProvider, schedCreator, voiceReporter)
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

	s := &Server{
		mux:            mux,
		mgr:            mgr,
		svc:            svc,
		browserSvc:     browserSvc,
		brainAuto:      brainAuto,
		scheduler:      sched,
		updateChecker:  updateChecker,
		updateApplier:  updateApplier,
		updateCLIs:     updateCLIs,
		allowedOrigins: allowedOrigins,
		authEnabled:    cfg.AuthEnabled,
		csp:            spaCSP(frontendSub),
	}

	if cfg.AuthEnabled {
		authSvc, err := auth.NewService(queries, cfg.RPID, cfg.RPOrigins)
		if err != nil {
			return nil, fmt.Errorf("auth service: %w", err)
		}
		authSvc.SetAdminSecret(cfg.AdminSecret)
		authSvc.SetMachineIdentity(cfg.MachineID, cfg.MachineIdentity)
		authSvc.RegisterRoutes(mux)
		authSvc.RegisterUserRoutes(mux)
		s.authSvc = authSvc
		wsh.SessionTracker = authSvc
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
// SweepOrphans reclaims orphaned worktrees and /tmp artifacts left by sessions
// that no longer exist (crashes, force-quits, DB resets). Orphans-only and
// self-guarding (it reaps nothing when the session table is empty), so it is
// safe to run unattended. Invoked once from the serve command at production
// startup — deliberately not from New, so unit tests that construct a server
// never trigger filesystem removals against the developer's real data dir.
func (s *Server) SweepOrphans(ctx context.Context) {
	if s.svc != nil {
		s.svc.SweepOrphans(ctx)
	}
}

func (s *Server) Shutdown() {
	if s.scheduler != nil {
		s.scheduler.Stop()
	}
	if s.updateChecker != nil {
		s.updateChecker.Stop()
	}
	if s.updateApplier != nil {
		s.updateApplier.StopArmWatch()
	}
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
	chain = requireJSONBodies(chain)
	chain = preventSensitiveCaching(chain)
	chain = securityHeaders(s.csp, chain)
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

// requireJSONBodies stops browsers from smuggling JSON through a CORS-simple
// text/plain request. Bodyless action endpoints remain valid without a
// Content-Type header.
func requireJSONBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.ContentLength == 0 {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
		default:
			next.ServeHTTP(w, r)
			return
		}

		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			httperror.RespondError(w, httperror.UnsupportedMediaType("request body must use application/json"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func preventSensitiveCaching(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/auth/") || r.URL.Path == "/api/machines" || strings.HasPrefix(r.URL.Path, "/api/machines/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authEnabled && !httpsecurity.RequestHostIsLoopback(r) {
			httperror.RespondError(w, httperror.Forbidden("auth-disabled access requires a loopback Host"))
			return
		}
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		w.Header().Add("Vary", "Origin")
		originAllowed := httpsecurity.OriginAllowed(r, s.allowedOrigins)
		publicCrossOrigin := crossOriginPublicPath(r.URL.Path)
		bearerRequest := httpsecurity.RequestsBearer(r)
		if r.Method == http.MethodOptions {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			bearerRequest = httpsecurity.PreflightRequestsBearer(r)
		}

		if !originAllowed && !publicCrossOrigin && !bearerRequest {
			httperror.RespondError(w, httperror.Forbidden("origin is not allowed"))
			return
		}

		switch {
		case origin != "" && originAllowed:
			// Configured (RP) origins get credentialed CORS — cookies work.
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		default:
			// An untrusted origin reaches this branch only for the explicit
			// public surface or when it proposes bearer authority. Omitting
			// Allow-Credentials prevents ambient cookies from riding along;
			// auth still validates any bearer before the handler runs.
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func crossOriginPublicPath(path string) bool {
	switch path {
	case "/.well-known/agentique/environment", "/api/health", "/api/auth/pair", "/api/auth/identity-proof", "/api/auth/status":
		return true
	default:
		return false
	}
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
