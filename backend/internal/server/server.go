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
	"github.com/mdjarv/agentique/backend/internal/assistant"
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
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
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
	"github.com/mdjarv/agentique/backend/internal/usage"
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
	// Commit is main.commit — the commit this binary was built from. It is what
	// the source channel compares the local checkout against (docs/upgrades.md).
	// Empty, "none" or "unknown" all mean "this build does not say", and the
	// source verdict is then withheld rather than guessed.
	Commit string
	// BuildOrigin is main.buildOrigin: "local" for `just build`, "release" for a
	// published asset, empty for a plain `go build`. Only a local build gets a
	// source verdict — nothing else can tell the two apart, since building at an
	// exact tag stamps the same bare tag CI does.
	BuildOrigin string
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
	// ExperimentalAssistant builds the assistant (docs/assistant.md): the
	// conversation, the journal, the verb table, the head, and the assistant.*
	// WS ops and AssistantReport tool that reach them. Off means UNBUILT, on
	// the brain's precedent — nothing below is constructed, and
	// `features.assistant` is false so a client never navigates to a surface
	// the server does not serve.
	ExperimentalAssistant bool

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

	// Brain (persistent agent memory). BrainEnabled is the master switch and is off by
	// default; BrainDir names the store. Both must hold or nothing below is constructed.
	// The optional Chroma/embed fields enable semantic recall (otherwise keyword recall
	// is used).
	BrainEnabled    bool
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
	// BrainConsolidateInterval enables scheduled (automatic) consolidation across all
	// scopes when set to a positive duration (e.g. "6h"); empty disables it. Resolved from
	// the AGENTIQUE_BRAIN_CONSOLIDATE_INTERVAL env var (preferred) or the [brain]
	// consolidate-interval config-file value.
	BrainConsolidateInterval string
	// BrainConsolidateModel is the model scheduled consolidation uses for LLM
	// reorganization; empty = deterministic dedup/decay only. From
	// AGENTIQUE_BRAIN_CONSOLIDATE_MODEL or [brain] consolidate-model.
	BrainConsolidateModel string
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

	// Assistant is the [assistant] section, resolved from the config file with
	// AGENTIQUE_ASSISTANT_* env overrides (docs/assistant.md, the M4 contract).
	// Raw strings: they are parsed in New, where a bad one is a warning and the
	// default rather than a server that will not start. Inert unless
	// ExperimentalAssistant is on.
	Assistant config.AssistantConfig

	// Peer is the [peer] section: what a paired server may do here
	// (docs/peers.md). The surface is mounted whatever it says — listing is
	// always served — and both opt-ins default to off.
	Peer config.PeerConfig
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
	mux        *http.ServeMux
	mgr        *session.Manager
	svc        *session.Service
	browserSvc *session.BrowserService
	authSvc    *auth.Service
	brainAuto  *brain.Automation
	// assistantSvc is nil when [experimental] assistant is off. Shutdown closes
	// it, which is what stops the head's subprocess and drops the credential
	// file behind it.
	assistantSvc *assistant.Service
	// peerPoller reads paired machines' news for this server's assistant or
	// live call (docs/peers.md). Nil when neither is on. Started by serve.go.
	peerPoller *peerPoller
	// stewardDeps is what this machine's steward reads (docs/peers.md). The
	// steward itself is built and started by serve.go, which knows where the
	// backups go.
	stewardDeps stewardDeps
	// assistantState is the session.state subscription behind the journal's
	// merge and archive entries, released on shutdown so the bus is not left
	// delivering into a closed server.
	assistantState *eventbus.Subscription
	// assistantHeartbeat is how often the assistant wakes on its own, resolved
	// from [assistant] heartbeat-interval. Zero disables it. Read by serve.go,
	// which starts the loop — New starts no timers.
	assistantHeartbeat time.Duration
	scheduler          *schedule.Scheduler
	updateChecker      *update.Checker
	updateApplier      *update.Applier
	updateCLIs         *update.CLIProbe
	updateSource       *update.SourceChecker
	usageCollector     *usage.Collector
	allowedOrigins     map[string]bool
	authEnabled        bool
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

// UpdateSourceChecker exposes the local-checkout watcher so serve.go can start
// its poll loop, for the same reason the other two are exposed: it shells out
// to git, and no constructor a test might call may do that. Nil when no
// [update] source-dir is configured.
func (s *Server) UpdateSourceChecker() *update.SourceChecker { return s.updateSource }

// UsageCollector exposes the subscription-usage collector so serve.go can start
// its poll loop. Same reason as the update checkers: it reaches the network and
// reads the CLI's credential store, and no constructor a test might call may do
// either.
func (s *Server) UsageCollector() *usage.Collector { return s.usageCollector }

// installPathOrEmpty resolves the binary the service would start. An empty
// answer disables only the staged-binary check, which is the right degradation:
// not knowing where the binary lives is not evidence that one is waiting.
func installPathOrEmpty() string {
	path, err := service.BinaryPath()
	if err != nil {
		return ""
	}
	return path
}

// Scheduler exposes the scheduled-loop service so serve.go can run the boot
// sweep and start the tick loop (deliberately not started in New — see the
// SweepOrphans precedent). Nil when the scheduler is disabled.
func (s *Server) Scheduler() *schedule.Scheduler { return s.scheduler }

// Assistant exposes the assistant so serve.go can start its heartbeat, on the
// same precedent as everything else in that block: the loop runs a model and
// starts head turns, and nothing a constructor a test might call may do either.
// Nil when [experimental] assistant is off.
func (s *Server) Assistant() *assistant.Service { return s.assistantSvc }

// PeerPoller exposes the paired-machine event poller so serve.go can start it:
// it dials other machines, which no constructor a test calls may do. Nil when
// neither the assistant nor voice is on.
func (s *Server) PeerPoller() *peerPoller { return s.peerPoller }

// RunSteward starts this machine's steward and blocks until ctx ends. From
// serve's production block, never a constructor: its passes write the findings
// table and publish to paired servers.
func (s *Server) RunSteward(ctx context.Context, backup StewardBackup) {
	newSteward(s.stewardDeps, backup).Run(ctx, stewardInterval)
}

// AssistantHeartbeat is how often that loop should tick, or 0 for never —
// resolved once in New from the config, so the parse and its boot warning live
// in one place rather than in the command.
func (s *Server) AssistantHeartbeat() time.Duration { return s.assistantHeartbeat }

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
		claudeOpts := session.ClaudeBaselineOptions()
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
	// The assistant head's own bearers, in their own store. A tool list is not
	// scoped to its caller, so the head's verbs live on their own endpoint, and
	// the only thing that can authenticate to it is a token minted for a head.
	assistantTokens := mcphttp.NewTokenStore()

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
	// The same seam, for "what is this account allowed to spend" (docs/usage.md).
	// A provider whose connector implements it needs no collector of its own;
	// Claude does not, because Anthropic exposes usage over HTTP rather than
	// through its CLI.
	accountInspectors := map[string]runtime.AccountInspectable{}
	if ai, ok := connector.(runtime.AccountInspectable); ok {
		accountInspectors["claude"] = ai
	}
	mgr := session.NewManager(cfg.DB, queries, bus, connector)
	if !cfg.TestMode {
		codexConnector := codexadapter.NewConnector()
		mgr.SetProviderConnector("codex", codexConnector)
		if in, ok := codexConnector.(runtime.InstallInspectable); ok {
			cliInspectors["codex"] = in
		}
		if ai, ok := codexConnector.(runtime.AccountInspectable); ok {
			accountInspectors["codex"] = ai
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
			// Same shape as the well-known descriptor's platform block, so a
			// client learns this host's OS from whichever it reads first.
			"platform": map[string]string{
				"os":   goruntime.GOOS,
				"arch": goruntime.GOARCH,
			},
			"features": map[string]bool{
				"browser":   cfg.ExperimentalBrowser,
				"teams":     cfg.ExperimentalTeams,
				"voice":     cfg.ExperimentalVoice,
				"assistant": cfg.ExperimentalAssistant,
				"brain":     cfg.BrainEnabled && cfg.BrainDir != "",
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
	var updateSource *update.SourceChecker
	var usageCollector *usage.Collector
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

		// The source channel (docs/upgrades.md). Two questions share one
		// watcher. "Is the binary at the install path the one running" needs no
		// checkout — `just install` without a restart leaves exactly that — so
		// any local build is watched for it. "Has the branch moved" needs a
		// named checkout. A release install answers neither: its updates come
		// from the release channel.
		origin := update.BuildOrigin(cfg.BuildOrigin)
		if cfg.Update.SourceDir != "" || origin == update.OriginLocal {
			updateSource = update.NewSourceChecker(update.SourceOptions{
				Dir:         cfg.Update.SourceDir,
				Branch:      cfg.Update.SourceBranch,
				BuiltFrom:   cfg.Commit,
				Origin:      origin,
				Version:     cfg.Version,
				InstallPath: installPathOrEmpty(),
				Interval:    interval,
			})
			// Knowing and acting are separate questions, the same split the CLI
			// rows draw. Restarting into a staged binary compiles nothing and
			// costs what a release apply costs, so it is always attached; the
			// build compiles in the operator's checkout, so it waits for
			// [update] source-apply.
			applier.SetSource(updateSource, cfg.Update.SourceApply)
			if cfg.Update.SourceDir != "" && !cfg.Update.SourceApply {
				slog.Info("update: source channel is reporting only — set [update] source-apply to enable the rebuild button",
					"dir", cfg.Update.SourceDir)
			}
		}

		uh := &update.Handler{Checker: updateChecker, Applier: applier, CLIs: updateCLIs, Source: updateSource}
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
				"platformOs":  m.PlatformOs,
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
			PlatformOs  string `json:"platformOs"`
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
		// platformOs is the remote's GOOS from its pairing descriptor. Empty is
		// allowed (an older client, or a descriptor that omitted it) and the
		// upsert then keeps any previously stored value.
		if !machine.ValidPlatformOS(req.PlatformOs) {
			httperror.RespondError(w, httperror.BadRequest("platformOs must be a lowercase OS name"))
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
			PlatformOs:  req.PlatformOs,
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
		// The peer credential first (docs/peers.md): it is the one an unattended
		// loop here presents, and forgetting the machine must not leave it alive
		// on the remote. A refusal already counts as revoked, the same rule as
		// the bearer's.
		if entry.PeerToken != "" {
			if err := machine.RevokeRemoteBearer(r.Context(), machineHTTPClient, entry.BaseUrl, entry.MachineID, entry.IdentityKey, entry.PeerToken); err != nil {
				httperror.RespondError(w, httperror.BadGateway("remote peer credential could not be revoked; the machine was not removed", err))
				return
			}
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
				"pairing":   cfg.AuthEnabled,
				"browser":   cfg.ExperimentalBrowser,
				"teams":     cfg.ExperimentalTeams,
				"voice":     cfg.ExperimentalVoice,
				"assistant": cfg.ExperimentalAssistant,
				"brain":     cfg.BrainEnabled && cfg.BrainDir != "",
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
	mux.HandleFunc("GET /api/sessions/{id}/model", sh.HandleModel)
	mux.HandleFunc("GET /api/sessions/{id}/history", sh.HandleHistory)
	mux.HandleFunc("POST /api/sessions/{id}/stop", sh.HandleStop)
	mux.HandleFunc("POST /api/sessions/{id}/query", sh.HandleQuery)
	mux.HandleFunc("DELETE /api/sessions/{id}", sh.HandleDelete)

	fh := &session.FilesHandler{}
	mux.HandleFunc("GET /api/sessions/{id}/files/{filepath...}", fh.HandleServe)
	eih := &session.EventImageHandler{Queries: queries}
	mux.HandleFunc("GET /api/sessions/{id}/events/{eventId}/images/{idx}", eih.HandleServe)

	sth := &storage.Handler{
		Queries: queries,
		LiveIDs: svc.LiveSessionIDs,
		Probe:   storage.RealSafetyProbe(),
		Reclaim: svc,
	}
	mux.HandleFunc("GET /api/storage/disk", sth.HandleDisk)
	mux.HandleFunc("GET /api/storage/usage", sth.HandleUsage)
	mux.HandleFunc("DELETE /api/storage/worktrees", sth.HandleDeleteWorktree)
	mux.HandleFunc("POST /api/storage/reclaim", sth.HandleReclaim)
	mux.HandleFunc("POST /api/storage/backups/trim", sth.HandleTrimBackups)
	mux.HandleFunc("DELETE /api/storage/scratchpads", sth.HandleDeleteForeignScratchpad)

	// Subscription usage: one probe per machine, shared by every client
	// (docs/usage.md). Deliberately NOT gated on [update] disabled — that
	// switch is about version checks, and a rate-limit window is a different
	// question. Constructed here, started from serve.go's production block.
	usageCollector = usage.New(usage.Options{
		Accounts: accountInspectors,
		Disk: func() (usage.DiskReading, bool) {
			st, err := storage.Stats()
			if err != nil {
				return usage.DiskReading{}, false
			}
			return usage.DiskReading{
				UsedPercent: st.UsagePercent,
				FreeBytes:   int64(st.FreeBytes),
			}, true
		},
		Today: func(ctx context.Context) (map[string]usage.Today, error) {
			rows, err := queries.TodaySpendByProvider(ctx)
			if err != nil {
				return nil, err
			}
			out := make(map[string]usage.Today, len(rows))
			for _, r := range rows {
				id := r.Provider
				if id == "" {
					id = "claude"
				}
				out[id] = usage.Today{Tokens: r.Tokens, Prompts: int(r.Prompts)}
			}
			return out, nil
		},
	})
	ush := &usage.Handler{Collector: usageCollector}
	mux.HandleFunc("GET /api/usage", ush.HandleStatus)

	projectGitSvc := project.NewGitService(queries, bus, project.RealGitOps(), runner)

	var teamSvc *team.Service
	var personaSvc *persona.Service
	if cfg.ExperimentalTeams {
		teamSvc = team.NewService(queries, bus)
		personaSvc = persona.NewService(runner, queries, bus)
		svc.SetPersonaQuerier(personaSvc)
		slog.Info("experimental teams feature enabled")
	}

	// One catalog for both the picker and the assistant: a model family someone
	// can choose on screen is one they can ask for out loud.
	catalog := modelCatalog(queries, cfg.ModelOverrides)

	// The owner's half of acting across machines (docs/peers.md). Mounted on
	// every server whatever its feature flags, because the machine being acted
	// on need not run an assistant itself; the auth middleware admits only a
	// peer credential here, and the guard inside decides what that credential
	// may actually do.
	//
	// The outbox is what a paired server following a session here reads back:
	// its reports and how its turns end. It listens to every turn end and keeps
	// only those of followed sessions.
	peerOutbox := peer.NewOutbox(queries, &assistantTurnFacts{svc: svc, queries: queries})
	mgr.AddTurnEndListener(peerOutbox.OnTurnEnd)
	peer.New(svc, queries,
		peer.WithSettings(peer.Settings{AcceptActions: cfg.Peer.AcceptActions, AcceptPolicies: cfg.Peer.AcceptPolicies}),
		peer.WithMachineID(cfg.MachineID),
		peer.WithCatalog(catalog),
		peer.WithOutbox(peerOutbox),
		peer.WithTranscripts(queries),
		peer.WithProposalActions(newAssistantActions(svc, gitSvc, mgr, queries, catalog)),
	).RegisterRoutes(mux)

	// Persistent memory ("the brain"). Opt-in: [brain] enabled is the master switch
	// and is off by default, so nothing below is constructed unless an operator asked
	// for it — no routes, no background loops. BrainDir must also be set, since it
	// names the store.
	//
	// It reaches no coding session. As of M2 the assistant is the brain's only
	// reader: memory is pulled through the head's own recall verb, never injected
	// into a session's preamble or turn, and no session tool writes a fact
	// (docs/assistant.md, the M2 contract).
	//
	// Failure to initialize must not take down the server — memory is an enhancement,
	// so we log and continue without it.
	//
	// It is constructed HERE, above the assistant, because the assistant takes it as a
	// collaborator through an option and options run in New: a brain built after that call
	// could only be handed over by a setter, and nothing else on this service arrives that
	// way. Nothing between the two positions reads either one.
	var (
		brainSvc  *brain.Service
		brainAuto *brain.Automation
	)
	if cfg.BrainEnabled && cfg.BrainDir != "" {
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
		newBrain, err := brain.New(context.Background(), brain.Config{
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
			brainSvc = newBrain
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

			// The assistant is the brain's only reader. With the store on and the
			// assistant off, memory is written and browsable and nothing recalls it —
			// say so, because a subsystem that is on and inert is the kind of thing
			// somebody re-derives at midnight. Never a refusal to boot: the store, the
			// routes and the page all work, and turning the assistant on is the fix.
			if !cfg.ExperimentalAssistant {
				slog.Info("brain: memory is stored and browsable, and nothing recalls it — " +
					"the assistant is the brain's only reader and [experimental] assistant is off " +
					"(set AGENTIQUE_EXPERIMENTAL_ASSISTANT=1 to turn it on)")
			}

			// The other half of that pairing, and the one that accumulates. With the
			// assistant on, every notable journal entry is staged as a capture — and
			// the ONLY path from a capture to a recallable fact is scheduled
			// consolidation with a model, because promotion is LLM-only. Without both
			// keys those sentences pile up forever, visible only behind the memory
			// page's capture toggle (docs/brain.md, "Automation").
			if cfg.ExperimentalAssistant && (cfg.BrainConsolidateInterval == "" || cfg.BrainConsolidateModel == "") {
				slog.Warn("brain: notable entries are staged as captures and nothing can promote them — " +
					"promotion is LLM-only, so set both [brain] consolidate-interval and " +
					"consolidate-model (or the captures accumulate unread)")
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

	// The assistant (docs/assistant.md), and the collaborators every surface on
	// it shares.
	//
	// The split here is the feature's gate. The COLLABORATORS — the directory,
	// the report registry, the dispatcher and the runtime facts — are built
	// whenever anything sits on them, because a live call is a head on this core
	// and needs all four whether or not the assistant itself is switched on. The
	// SERVICE is gated on [experimental] assistant, on the brain's precedent:
	// off means UNBUILT, so no conversation, no journal, no head, no assistant.*
	// WS ops and no assistant MCP tools, and `features.assistant` says so.
	var (
		assistantSvc       *assistant.Service
		assistantState     *eventbus.Subscription
		assistantHeartbeat time.Duration
		reportRegistry     *assistant.Registry
		assistantDir       *assistantDirectory
		assistantDisp      *assistantDispatcher
		assistantFacts     *assistantTurnFacts
		summarizer         *sessionSummarizer
		peerLink           *peerlink.Client
		peerSrc            *peerSessions
	)
	if cfg.ExperimentalVoice || cfg.ExperimentalAssistant {
		// The summariser keeps a session's transcript on this machine: it runs
		// through the provider CLI and only its paragraph leaves.
		summarizer = newSessionSummarizer(runner, queries, cfg.Voice.SummaryModel)
		// A cached summary describes a session as it was BEFORE the turn that
		// just ended, so it is stale the moment one does. Its own listener,
		// because dropping a cache entry is not a fact about the turn — it is
		// bookkeeping that has to happen whether or not anybody is following.
		mgr.AddTurnEndListener(summarizer.Forget)

		reportRegistry = assistant.NewRegistry()
		assistantDisp = &assistantDispatcher{svc: svc, queries: queries, summarizer: summarizer}
		assistantFacts = &assistantTurnFacts{svc: svc, queries: queries}
		// Machine presentation is read per answer rather than captured, so a
		// rename takes effect without a restart — the same rule the health
		// endpoint follows.
		assistantDir = newAssistantDirectory(svc, queries, summarizer, catalog, cfg.MachineID,
			func(ctx context.Context) string {
				label, _ := hostPresentation(ctx)
				return label
			})
		// Paired machines (docs/peers.md): their sessions and projects read
		// through each one's peer surface, and actions on them sent the same
		// way, with the peer credential peerLink mints and holds. Without it
		// the assistant knew only this machine's database, and a session the
		// sidebar showed on zbook did not exist for it. Lazy: nothing is
		// dialled until an assistant or a call asks.
		// A long poll holds its request open, so it gets a client whose timeout
		// outlives the wait; everything else keeps the machine client's.
		pollHTTPClient := *machineHTTPClient
		pollHTTPClient.Timeout = peerPollWait + 20*time.Second
		peerLink = peerlink.New(machineHTTPClient, queries,
			peerlink.WithPollClient(&pollHTTPClient),
			peerlink.WithLabel(func(ctx context.Context) string {
				label, _ := hostPresentation(ctx)
				return label
			}))
		peerSrc = newPeerSessions(queries, machineHTTPClient, peerLink, cfg.MachineID)
		assistantDir.peers, assistantDir.link = peerSrc, peerLink
		assistantDisp.peers, assistantDisp.link = peerSrc, peerLink
	}
	if cfg.ExperimentalAssistant {
		// Every collaborator below is non-nil by construction: the block above
		// runs whenever this one does. That matters because these are
		// interfaces, and a typed-nil pointer in one would look present and
		// panic on first use.
		opts := []assistant.Option{
			assistant.WithDirectory(assistantDir),
			assistant.WithDispatcher(assistantDisp),
			assistant.WithTurnFacts(assistantFacts),
			assistant.WithHeadManager(&assistantHeads{
				mgr:    mgr,
				tokens: assistantTokens,
				mcpURL: assistantMCPURL(cfg.MCPInternalURL),
			}),
			// The collector answers from cache and never touches the network,
			// which is what makes it safe to ask inside a verb.
			assistant.WithAllowances(usageCollector),
			// The uncontained tier's executor and its facts. Without it those
			// eight verbs refuse in words rather than proposing something
			// nothing has checked — so it is passed here, where every service
			// it needs already exists, and never constructed inside the core.
			assistant.WithActions(&routedActions{
				local: newAssistantActions(svc, gitSvc, mgr, queries, catalog),
				peers: peerSrc,
				link:  peerLink,
			}),
			assistant.WithRegistry(reportRegistry),
			assistant.WithBroadcaster(bus),
			// Autonomy (docs/assistant.md, the M4 contract). The triager is a
			// Haiku one-shot over the same blocking runner the auto-namer uses,
			// so the core never learns which provider CLI it is — and the
			// heartbeat's own timer is started from serve, not here.
			assistant.WithTriager(newAssistantTriager(runner, catalog, cfg.Assistant.TriageModel)),
			assistant.WithDigestAt(assistantDigestAt(cfg.Assistant.DigestAt)),
			// Compaction (docs/assistant.md, the M5 contract): the same one-shot
			// seam with a different prompt, and the reason it is wired here
			// rather than defaulted inside the core — without it the core folds
			// NOTHING, because deleting journal rows nothing has summarised is
			// losing them.
			assistant.WithSummarizer(newAssistantSummarizer(runner, catalog, cfg.Assistant.TriageModel)),
		}
		assistantHeartbeat = assistantHeartbeatInterval(cfg.Assistant.HeartbeatInterval)
		// The brain is the assistant's long-term memory and nothing else's, and
		// it is the one collaborator that is genuinely optional at runtime: the
		// two switches are independent, and `[brain] enabled` off means there is
		// no store to read. Only handed over when one was actually built, which
		// is what keeps the four memory verbs out of the table on a server with
		// no memory — a verb that cannot work must not be offered.
		//
		// Built from the pointer INSIDE the branch rather than narrowed to the
		// interface outside it: a typed-nil *brain.Service in an interface reads
		// as present, and the table would then carry four verbs that panic.
		if brainSvc != nil {
			opts = append(opts, assistant.WithMemory(newAssistantMemory(brainSvc, queries)))
		}
		a, err := assistant.New(queries, opts...)
		if err != nil {
			return nil, fmt.Errorf("assistant service: %w", err)
		}
		assistantSvc = a

		// ONE turn-end listener: it writes the journal and notifies the
		// followers, and a live call is one of those followers. Registered here
		// rather than from assistant.New, which by contract starts nothing.
		mgr.AddTurnEndListener(assistantSvc.OnTurnEnd)
		// A merge and an archive arrive on the state push rather than at a turn
		// ending, because neither of them is something a turn did. The baseline
		// is primed BEFORE the subscription, and that order is the whole point:
		// a push announces a write that has already happened, so a baseline
		// read after subscribing can already contain the transition it is
		// about to be told of. A read, not a side effect, which is why it may
		// live here; a failure is logged and the observer primes itself lazily,
		// fail-closed, on the first push.
		primeCtx, cancelPrime := context.WithTimeout(context.Background(), 15*time.Second)
		if err := assistantSvc.PrimeSessionStates(primeCtx); err != nil {
			slog.Warn("assistant: session state baseline not primed at boot", "error", err)
		}
		cancelPrime()
		assistantState = bus.SubscribeAll(&assistantStateObserver{svc: assistantSvc})
		slog.Info("assistant enabled", "heartbeat", assistantHeartbeat,
			"digest_at", cfg.Assistant.DigestAt)
	}

	wsh := &ws.Handler{Service: svc, GitService: gitSvc, ProjectGitService: projectGitSvc, Queries: queries, Bus: bus, TeamService: teamSvc, PersonaService: personaSvc, BrowserService: browserSvc, ScheduleService: sched, AssistantService: assistantSvc, Catalog: catalog, AllowedOrigins: allowedOrigins, AllowTicketOrigin: cfg.AuthEnabled}
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
	// The live handler outlives this block so the auth service — constructed
	// later — can be wired in as its session tracker.
	var liveVoice *voice.Handler
	if cfg.ExperimentalVoice {
		// Persona settings are read per call, so a change here takes effect on
		// the next call rather than the next restart.
		// Ignore the error here: newVoiceHandler below reports the same bad
		// configuration and disables the feature. A zero Options just means the
		// audition refuses, which is the right answer on a broken config.
		previewOpts, _ := resolveVoiceOptions(cfg)
		voiceSettings := &voiceSettingsHandler{
			queries:     queries,
			configModel: cfg.Voice.Model,
			previewOpts: previewOpts,
		}
		mux.HandleFunc("GET /api/voice/settings", voiceSettings.HandleGet)
		mux.HandleFunc("PUT /api/voice/settings", voiceSettings.HandlePut)
		mux.HandleFunc("POST /api/voice/preview", voiceSettings.HandlePreview)
		// Narrowed to the interface here rather than passed as a pointer: a
		// typed-nil *assistant.Service would arrive at the call looking present
		// and panic on the first turn it tried to mirror.
		var conversation voice.Conversation
		// The proposal half, narrowed for the same reason: it is also what
		// registers a live call as a surface, so a decision proposed mid-call
		// reaches the person on the line.
		var proposals voice.Proposals
		if assistantSvc != nil {
			conversation = assistantSvc
			proposals = assistantSvc
		}
		vh, err := newVoiceHandler(cfg, allowedOrigins, reportRegistry, assistantDisp,
			voiceSettings, assistantDir, conversation, proposals)
		if err != nil {
			slog.Error("live voice disabled: bad configuration", "error", err)
		} else {
			mux.Handle("GET /api/voice/live", vh)
			liveVoice = vh
			if assistantSvc == nil {
				// The runtime half of what a call hears — blocked, died,
				// finished — on a server whose assistant is off. With the core
				// built these come from its one turn-end listener, journal and
				// all; without it a call must not go deaf, so the same fact
				// reaches the same registry with nothing written down.
				watcher := newVoiceTurnWatcher(reportRegistry, assistantFacts)
				mgr.AddTurnEndListener(watcher.OnTurnEnd)
			}
			slog.Info("live voice enabled", "backend", vh.Backend())
		}
	}

	// A typed-nil *Scheduler must not become a non-nil interface.
	var schedCreator mcphttp.ScheduleCreator
	if sched != nil {
		schedCreator = sched
	}
	// Two halves, two gates. AssistantReport is registered on EVERY server
	// (docs/peers.md): a report reaches the local service when the assistant is
	// on (journal and followers), the bare registry when only a call is
	// following, and the peer outbox when a paired server is following — and
	// the machine a session runs on need not run an assistant for that last
	// one, so the tool cannot be gated on either local half. The verb table is
	// the head's and exists only with the service. Same typed-nil trap as above.
	var localReporter mcphttp.AssistantReporter
	switch {
	case assistantSvc != nil:
		localReporter = assistantSvc
	case reportRegistry != nil:
		localReporter = reportRegistry
	}
	mcpHandler := mcphttp.NewHandler(mcpTokens, devStore, svc, schedCreator,
		&peerAwareReporter{local: localReporter, outbox: peerOutbox}, sessionModelInspector{svc: svc})
	// Register explicit methods so the pattern doesn't conflict with the SPA
	// catch-all "GET /". The handler dispatches on method internally.
	mux.Handle("POST /mcp", mcpHandler)
	mux.Handle("GET /mcp", mcpHandler)
	mux.Handle("DELETE /mcp", mcpHandler)

	// The head's endpoint, mounted only with the service and reachable only with
	// a head's bearer. It is a separate endpoint because `tools/list` answers
	// from everything registered on a handler, whoever asks: sharing one would
	// hand the whole verb table — the uncontained tier included — to every
	// coding session on every turn.
	if assistantSvc != nil {
		assistantMCP := mcphttp.NewAssistantHandler(assistantTokens, assistantSvc)
		mux.Handle("POST "+assistantMCPPath, assistantMCP)
		mux.Handle("GET "+assistantMCPPath, assistantMCP)
		mux.Handle("DELETE "+assistantMCPPath, assistantMCP)
	}

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

	var poller *peerPoller
	if peerLink != nil {
		var sink peerEventSink
		switch {
		case assistantSvc != nil:
			sink = assistantPeerSink{svc: assistantSvc}
		case reportRegistry != nil:
			sink = registryPeerSink{reg: reportRegistry}
		}
		if sink != nil {
			poller = newPeerPoller(queries, peerLink, sink, cfg.MachineID)
			poller.onTurnEnd = func(machineID, sessionID string) {
				// What the session is doing just changed, and a summary of it
				// describes the turn before.
				peerSrc.Invalidate(machineID)
				summarizer.Forget(machineID + ":" + sessionID)
			}
		}
	}

	mux.HandleFunc("GET /api/steward/findings", handleFindings(queries))

	s := &Server{
		stewardDeps: stewardDeps{
			queries: queries, svc: svc, usage: usageCollector, updates: updateChecker,
			storage: sth, outbox: peerOutbox, assist: assistantSvc,
		},
		peerPoller:         poller,
		mux:                mux,
		mgr:                mgr,
		svc:                svc,
		browserSvc:         browserSvc,
		brainAuto:          brainAuto,
		assistantSvc:       assistantSvc,
		assistantHeartbeat: assistantHeartbeat,
		assistantState:     assistantState,
		scheduler:          sched,
		updateChecker:      updateChecker,
		updateApplier:      updateApplier,
		updateCLIs:         updateCLIs,
		updateSource:       updateSource,
		usageCollector:     usageCollector,
		allowedOrigins:     allowedOrigins,
		authEnabled:        cfg.AuthEnabled,
		csp:                spaCSP(frontendSub),
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
		// The voice socket is an authenticated WebSocket like /ws: revoking or
		// expiring an auth session must hang up its live calls, not just close
		// its subscriptions.
		if liveVoice != nil {
			liveVoice.SetSessionTracker(authSvc)
		}
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
	if s.usageCollector != nil {
		s.usageCollector.Stop()
	}
	if s.updateSource != nil {
		s.updateSource.Stop()
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
	if s.assistantState != nil {
		s.assistantState.Unsubscribe()
	}
	if s.assistantSvc != nil {
		if err := s.assistantSvc.Close(); err != nil {
			slog.Warn("assistant not closed cleanly", "error", err)
		}
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
