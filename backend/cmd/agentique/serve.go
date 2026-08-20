package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/allbin/agentkit/sqliteops"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/doctor"
	"github.com/mdjarv/agentique/backend/internal/logging"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/procctl"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/schedule"
	"github.com/mdjarv/agentique/backend/internal/server"
	"github.com/mdjarv/agentique/backend/internal/store"
)

var (
	dbPath         string
	disableAuth    bool
	logLevel       string
	rpID           string
	rpOrigin       string
	tlsCert        string
	tlsKey         string
	logOutput      string
	testMode       bool
	backupInterval string
	backupRetain   int
	disableBackup  bool
)

func init() {
	serveCmd.Flags().StringVar(&dbPath, "db", "", "database file path (default: platform data dir)")
	serveCmd.Flags().StringVar(&logLevel, "log-level", "", "log level: trace, debug, info (default), warn, error")
	serveCmd.Flags().BoolVar(&disableAuth, "disable-auth", false, "disable authentication (allow anonymous access)")
	serveCmd.Flags().StringVar(&rpID, "rp-id", "", "WebAuthn relying party ID (default: hostname from --addr)")
	serveCmd.Flags().StringVar(&rpOrigin, "rp-origin", "", "WebAuthn relying party origin (default: derived from --addr)")
	serveCmd.Flags().StringVar(&tlsCert, "tls-cert", "", "path to TLS certificate file")
	serveCmd.Flags().StringVar(&tlsKey, "tls-key", "", "path to TLS key file")
	serveCmd.Flags().StringVar(&logOutput, "log-output", "auto", "log output mode: auto, journald, file, stdout")
	serveCmd.Flags().BoolVar(&testMode, "test-mode", false, "enable test mode (mock CLI, test endpoints, no auth)")
	serveCmd.Flags().StringVar(&backupInterval, "backup-interval", "15m", "interval between database backups")
	serveCmd.Flags().IntVar(&backupRetain, "backup-retain", 7, "days to keep daily backups")
	serveCmd.Flags().BoolVar(&disableBackup, "disable-backup", false, "disable automatic database backups")
	rootCmd.AddCommand(serveCmd)
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Agentique server",
	RunE:  runServe,
	// Failures here are runtime conditions (port taken, data dir already
	// owned), not malformed invocations — a flag dump buries the message that
	// actually tells the operator what to do.
	SilenceUsage: true,
}

func preflight() error {
	checks := doctor.RunRequired()
	if doctor.HasFailures(checks) {
		return fmt.Errorf("%s", doctor.FormatError(checks))
	}
	// Log warnings for optional tools.
	for _, c := range checks {
		if c.Status == doctor.Warn {
			slog.Warn(c.Name+": "+c.Message, "fix", c.Fix)
		}
	}
	return nil
}

// isRelease reports whether this is a release build (version set via ldflags).
func isRelease() bool {
	return version != "" && version != "dev"
}

// envFloat parses a float from an env var, returning 0 when unset or unparseable.
func envFloat(name string) float64 {
	v := os.Getenv(name)
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		slog.Warn("ignoring unparseable float env var", "name", name, "value", v)
		return 0
	}
	return f
}

// envBool reads a boolean env var, treating the common truthy spellings as true and
// everything else (incl. unset) as false.
func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// envFloatOr layers an env var (preferred) over a config-file float: the env value wins
// when set and parseable, otherwise the file value is used. Mirrors firstNonEmpty for floats.
func envFloatOr(name string, fileVal float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return fileVal
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		slog.Warn("ignoring unparseable float env var; using config-file value", "name", name, "value", v)
		return fileVal
	}
	return f
}

// envIntOr layers an int env var (preferred) over a config-file int: the env value wins when
// set and parseable, otherwise the file value is used. Mirrors envFloatOr for ints.
func envIntOr(name string, fileVal int) int {
	v := os.Getenv(name)
	if v == "" {
		return fileVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("ignoring unparseable int env var; using config-file value", "name", name, "value", v)
		return fileVal
	}
	return n
}

// envBoolOr layers a boolean env var (preferred) over a config-file bool: when the env var
// is set its value wins, otherwise the file value is used (so an absent env doesn't force false).
func envBoolOr(name string, fileVal bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fileVal
	}
	switch strings.ToLower(v) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// resolveRecall resolves the default-ON auto-recall toggle: the AGENTIQUE_BRAIN_RECALL env
// wins when set, else the [brain] recall config value, else on. A value of off/false/0/no
// disables it; anything else (incl. empty/unset at both layers) leaves it on.
func resolveRecall(fileVal string) bool {
	if v := strings.TrimSpace(os.Getenv("AGENTIQUE_BRAIN_RECALL")); v != "" {
		return !brainToggleOff(v)
	}
	if v := strings.TrimSpace(fileVal); v != "" {
		return !brainToggleOff(v)
	}
	return true
}

func brainToggleOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "false", "0", "no":
		return true
	default:
		return false
	}
}

// firstNonEmpty returns the first non-empty string, used to layer an env var (preferred)
// over a config-file value: firstNonEmpty(os.Getenv(...), fileCfg....).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func resolveDBPath() string {
	if dbPath != "" {
		return dbPath
	}
	if v := os.Getenv("AGENTIQUE_DB"); v != "" {
		if err := os.MkdirAll(filepath.Dir(v), 0o755); err != nil {
			slog.Warn("cannot create directory for AGENTIQUE_DB, using default", "path", v, "error", err)
		} else {
			return v
		}
	}
	if !isRelease() {
		return "agentique.db"
	}
	p := paths.DBPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "agentique.db"
	}
	return p
}

// ownsDataDir reports whether a server that opened dbFile is the canonical
// instance for paths.DataDir() — i.e. it opened that data dir's own database
// rather than a scratch one beside it.
//
// This is the empty-authority guard for every destructive startup sweep. A
// server whose DB is not the data dir's DB has no picture of what that data dir
// legitimately contains: its (empty or stale) session set makes real worktrees,
// files, and CLI processes look unowned. Such a server must reclaim nothing.
// `--db /tmp/verify.db` with a production AGENTIQUE_HOME is exactly that shape.
func ownsDataDir(dbFile string) bool {
	abs, err := filepath.Abs(dbFile)
	if err != nil {
		return false
	}
	canonical, err := filepath.Abs(paths.DBPath())
	if err != nil {
		return false
	}
	return abs == canonical
}

func runServe(cmd *cobra.Command, args []string) error {
	// Load config file (missing file = defaults, not an error).
	fileCfg, err := config.Load(config.Path())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	applyConfig(cmd, fileCfg)

	lvl := logLevel
	if lvl == "" {
		lvl = os.Getenv("LOG_LEVEL")
	}
	jsonLog := os.Getenv("JSON_LOG")
	if jsonLog == "" {
		jsonLog = filepath.Join(paths.DataDir(), "agentique.log.jsonl")
	}
	logging.InitWithMode(lvl, jsonLog, logging.OutputMode(logOutput))

	// First-run hint.
	if !config.Exists() && !fileExists(paths.DBPath()) {
		slog.Info("first run detected — run 'agentique setup' for guided configuration")
	}

	if !testMode {
		if err := preflight(); err != nil {
			return err
		}
	}

	slog.Info("data directory", "path", paths.DataDir())
	dbFile := resolveDBPath()
	slog.Info("database", "path", dbFile)

	if testMode {
		absDB, _ := filepath.Abs(dbFile)
		prodDB := paths.DBPath()
		if absDB == prodDB {
			slog.Error("refusing to run in test mode against production database",
				"path", absDB,
				"hint", "set AGENTIQUE_DB or --db to an isolated path")
			os.Exit(1)
		}
	}

	// Refuse to start a second server against the same address — prevents two
	// servers fighting over the port/data dir (e.g. a tray-launched one plus a
	// manual start) and protects the DB from concurrent migrations below.
	if !testMode && isServerRunning() {
		return fmt.Errorf("agentique already running at %s — stop it first or use a different --addr", baseURL())
	}

	// Single-instance is a property of the DATA DIRECTORY, not the listen
	// address: two servers on different ports still share this dir's database,
	// worktrees, session files, and the CLI subprocesses the orphan reaper
	// claims authority over. The address probe above misses that case entirely
	// (it is how `just dev` on :9201 could start alongside the service on
	// :19201 and reap its live sessions), so take an exclusive lock on the dir.
	if !testMode {
		lock, err := procctl.AcquireInstanceLock(paths.DataDir())
		if err != nil {
			if errors.Is(err, procctl.ErrInstanceLocked) {
				return fmt.Errorf("%w\n  data dir: %s\n  hint: stop it first, or run against an isolated data dir with AGENTIQUE_HOME=<dir>", err, paths.DataDir())
			}
			return fmt.Errorf("instance lock: %w", err)
		}
		defer func() {
			if err := lock.Release(); err != nil {
				slog.Warn("releasing instance lock", "error", err)
			}
		}()
	}

	// Stamp this instance's identity onto every subprocess we spawn from here
	// on (ordinary env inheritance reaches the provider CLI and its subtree).
	// It is what scopes the orphan reaper to our own CLIs — see
	// procctl.OwnerEnvVar.
	if err := procctl.StampOwner(paths.DataDir()); err != nil {
		return fmt.Errorf("stamp owner identity: %w", err)
	}

	db, err := store.Open(dbFile)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if !testMode && !disableBackup {
		backupDir := filepath.Join(filepath.Dir(dbFile), "backups")
		sqliteops.Snapshot(db, backupDir, "agentique", 5)
	}

	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	queries := store.New(db)
	ensureDefaultProject(queries, fileCfg.Setup.InitialProject)
	seedBuiltinPersonas(queries)

	if !testMode && !disableBackup {
		interval, err := time.ParseDuration(backupInterval)
		if err != nil {
			slog.Error("invalid backup interval", "value", backupInterval)
			os.Exit(1)
		}
		backupDir := filepath.Join(filepath.Dir(dbFile), "backups")
		stopBackup := sqliteops.StartBackup(sqliteops.BackupConfig{
			DB:          db,
			Dir:         backupDir,
			Prefix:      "agentique",
			Interval:    interval,
			DailyRetain: backupRetain,
		})
		defer stopBackup()
		slog.Info("backup enabled", "interval", interval, "retain", backupRetain, "dir", backupDir)
	}

	tlsEnabled := tlsCert != "" && tlsKey != ""
	if (tlsCert != "") != (tlsKey != "") {
		slog.Error("--tls-cert and --tls-key must both be provided")
		os.Exit(1)
	}

	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	if testMode {
		disableAuth = true
		slog.Info("test mode: auth disabled, mock CLI enabled")
	}

	if err := config.ValidateDevURLs(fileCfg.DevURLs); err != nil {
		slog.Error("invalid dev-urls config", "error", err)
		os.Exit(1)
	}

	// MCP endpoint URL the spawned Claude subprocess uses to reach /mcp.
	// Always targets localhost — MCP is loopback-only for the local CLI.
	_, mcpPort, _ := net.SplitHostPort(addr)
	if mcpPort == "" {
		mcpPort = "9201"
	}
	mcpInternalURL := fmt.Sprintf("http://127.0.0.1:%s/mcp", mcpPort)

	// Idle-session eviction: env wins over the [session] config-file value; ""
	// disables it. An unparseable value is a hard config error rather than a
	// silent no-op.
	var idleEvictTimeout time.Duration
	if v := firstNonEmpty(os.Getenv("AGENTIQUE_SESSION_IDLE_EVICT_TIMEOUT"), fileCfg.Session.IdleEvictTimeout); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			slog.Error("invalid session idle-evict-timeout", "value", v, "error", err)
			os.Exit(1)
		}
		idleEvictTimeout = d
	}

	// [claude] connector flags: env wins over the config file. An out-of-range
	// autocompact window is a hard config error rather than a flag the CLI
	// rejects at spawn time, when the failure would surface as a dead session.
	claudeCfg := config.ClaudeConfig{
		ExcludeDynamicSystemPromptSections: envBoolOr(
			"AGENTIQUE_CLAUDE_EXCLUDE_DYNAMIC_SYSTEM_PROMPT_SECTIONS",
			fileCfg.Claude.ExcludeDynamicSystemPromptSections,
		),
		AutoCompact: firstNonEmpty(os.Getenv("AGENTIQUE_CLAUDE_AUTOCOMPACT"), fileCfg.Claude.AutoCompact),
		ForwardSubagentText: envBoolOr(
			"AGENTIQUE_CLAUDE_FORWARD_SUBAGENT_TEXT",
			fileCfg.Claude.ForwardSubagentText,
		),
	}
	if err := claudeCfg.Validate(); err != nil {
		slog.Error("invalid [claude] config", "error", err)
		os.Exit(1)
	}

	// Scheduled loops ([scheduler], docs/scheduled-loops.md): env wins over the
	// config file; unset fields take the scheduler's documented defaults. An
	// unparseable duration is a hard config error, same as idle-evict.
	schedDuration := func(key, fileVal string) time.Duration {
		v := firstNonEmpty(os.Getenv(key), fileVal)
		if v == "" {
			return 0 // scheduler default
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			slog.Error("invalid scheduler duration", "key", key, "value", v, "error", err)
			os.Exit(1)
		}
		return d
	}
	schedulerDisabled := envBoolOr("AGENTIQUE_SCHEDULER_DISABLED", fileCfg.Scheduler.Disabled)
	schedulerOpts := schedule.Options{
		TickInterval:           schedDuration("AGENTIQUE_SCHEDULER_TICK_INTERVAL", fileCfg.Scheduler.TickInterval),
		MinInterval:            schedDuration("AGENTIQUE_SCHEDULER_MIN_INTERVAL", fileCfg.Scheduler.MinInterval),
		MaxRunDuration:         schedDuration("AGENTIQUE_SCHEDULER_MAX_RUN_DURATION", fileCfg.Scheduler.MaxRunDuration),
		OnceCatchupWindow:      schedDuration("AGENTIQUE_SCHEDULER_ONCE_CATCHUP_WINDOW", fileCfg.Scheduler.OnceCatchupWindow),
		DynamicMaxDelay:        schedDuration("AGENTIQUE_SCHEDULER_DYNAMIC_MAX_DELAY", fileCfg.Scheduler.DynamicMaxDelay),
		DynamicFallback:        schedDuration("AGENTIQUE_SCHEDULER_DYNAMIC_FALLBACK", fileCfg.Scheduler.DynamicFallback),
		MaxConsecutiveFailures: envIntOr("AGENTIQUE_SCHEDULER_MAX_CONSECUTIVE_FAILURES", fileCfg.Scheduler.MaxConsecutiveFailures),
		RunHistory:             envIntOr("AGENTIQUE_SCHEDULER_RUN_HISTORY", fileCfg.Scheduler.RunHistory),
	}

	// Machine identity + pairing admin secret (docs/multi-machine-research.md
	// M0). Both are small read-or-create files in the data dir — created here
	// at serve startup, never in server.New (no filesystem side effects in
	// constructors), and skipped in test mode so test servers pointed at
	// scratch DBs never touch the real data dir.
	machineID := ""
	adminSecret := ""
	if !testMode {
		machineID, err = machine.LoadOrCreateID(paths.DataDir())
		if err != nil {
			slog.Error("failed to resolve machine identity", "error", err)
			os.Exit(1)
		}
		if !disableAuth {
			adminSecret, err = auth.LoadOrCreateAdminSecret(paths.DataDir())
			if err != nil {
				slog.Error("failed to resolve admin secret", "error", err)
				os.Exit(1)
			}
		}
	}
	machineLabel := machine.Label(firstNonEmpty(os.Getenv("AGENTIQUE_MACHINE_LABEL"), fileCfg.Server.MachineLabel))

	cfg := server.Config{
		AuthEnabled:         !disableAuth,
		MachineID:           machineID,
		MachineLabel:        machineLabel,
		Version:             version,
		AdminSecret:         adminSecret,
		TestMode:            testMode,
		DevMode:             !isRelease(),
		DBPath:              dbFile,
		DB:                  db,
		ExperimentalTeams:   fileCfg.Experimental.Teams,
		ExperimentalBrowser: fileCfg.Experimental.Browser,
		IdleEvictTimeout:    idleEvictTimeout,
		Claude:              claudeCfg,
		SchedulerDisabled:   schedulerDisabled,
		SchedulerOptions:    schedulerOpts,
		DevURLSlots:         fileCfg.DevURLs,
		ModelOverrides:      fileCfg.Models,
		MCPInternalURL:      mcpInternalURL,
		// Persistent agent memory ("brain"). Lives alongside the DB. Semantic
		// recall is opt-in via env (otherwise keyword recall over markdown files).
		BrainDir: filepath.Join(filepath.Dir(dbFile), "brain"),
		// Semantic recall: env var wins, else the [brain] config-file value, else off/default.
		BrainChromaURL:         firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_CHROMA_URL"), fileCfg.Brain.ChromaURL),
		BrainEmbedURL:          firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_EMBED_URL"), fileCfg.Brain.EmbedURL),
		BrainEmbedModel:        firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_EMBED_MODEL"), fileCfg.Brain.EmbedModel),
		BrainEmbedKey:          firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_EMBED_KEY"), fileCfg.Brain.EmbedKey),
		BrainSemanticThreshold: envFloatOr("AGENTIQUE_BRAIN_SEMANTIC_THRESHOLD", fileCfg.Brain.SemanticThreshold),
		BrainVectorVeto:        envFloatOr("AGENTIQUE_BRAIN_VECTOR_VETO", fileCfg.Brain.VectorVeto),
		BrainCalibrate:         envBoolOr("AGENTIQUE_BRAIN_AUTOCAL", fileCfg.Brain.Autocal),
		BrainRecall:            resolveRecall(fileCfg.Brain.Recall),
		// Scheduled consolidation: env var wins, else the [brain] config-file value, else off.
		BrainConsolidateInterval: firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_CONSOLIDATE_INTERVAL"), fileCfg.Brain.ConsolidateInterval),
		BrainConsolidateModel:    firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_CONSOLIDATE_MODEL"), fileCfg.Brain.ConsolidateModel),
		// Session-end learning: env wins over the [brain] config-file value, else off.
		BrainLearnModel:   firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_LEARN_MODEL"), fileCfg.Brain.LearnModel),
		BrainOutcomeModel: firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_OUTCOME_MODEL"), fileCfg.Brain.OutcomeModel),
		// Pre-churn snapshot retention: env wins over the [brain] value; 0 → brain's default (7).
		BrainSnapshotRetain: envIntOr("AGENTIQUE_BRAIN_SNAPSHOT_RETAIN", fileCfg.Brain.SnapshotRetain),
		// Disuse-aging archival: env wins; "" = off, 0 floor → brain's default (0.35).
		BrainArchiveAfter: firstNonEmpty(os.Getenv("AGENTIQUE_BRAIN_ARCHIVE_AFTER"), fileCfg.Brain.ArchiveAfter),
		BrainArchiveFloor: envFloatOr("AGENTIQUE_BRAIN_ARCHIVE_FLOOR", fileCfg.Brain.ArchiveConfidenceFloor),
		// Durable learn/outcome job retry budget: env wins; 0 → brain's default (5).
		BrainRetryMax: envIntOr("AGENTIQUE_BRAIN_RETRY_MAX", fileCfg.Brain.RetryMax),
		// Graph-view tuning: env wins over the [brain.graph] file value; 0 → brain's default.
		BrainGraph: config.BrainGraphConfig{
			EdgeCap:          envIntOr("AGENTIQUE_BRAIN_GRAPH_EDGE_CAP", fileCfg.Brain.Graph.EdgeCap),
			EdgeThreshold:    envFloatOr("AGENTIQUE_BRAIN_GRAPH_EDGE_THRESHOLD", fileCfg.Brain.Graph.EdgeThreshold),
			LinkStrengthBase: envFloatOr("AGENTIQUE_BRAIN_GRAPH_LINK_STRENGTH_BASE", fileCfg.Brain.Graph.LinkStrengthBase),
			LinkStrengthSpan: envFloatOr("AGENTIQUE_BRAIN_GRAPH_LINK_STRENGTH_SPAN", fileCfg.Brain.Graph.LinkStrengthSpan),
			LinkDistanceBase: envFloatOr("AGENTIQUE_BRAIN_GRAPH_LINK_DISTANCE_BASE", fileCfg.Brain.Graph.LinkDistanceBase),
			LinkDistanceSpan: envFloatOr("AGENTIQUE_BRAIN_GRAPH_LINK_DISTANCE_SPAN", fileCfg.Brain.Graph.LinkDistanceSpan),
			Gravity:          envFloatOr("AGENTIQUE_BRAIN_GRAPH_GRAVITY", fileCfg.Brain.Graph.Gravity),
		},
	}
	if cfg.AuthEnabled {
		cfg.RPID = rpID
		if cfg.RPID == "" {
			host, _, _ := net.SplitHostPort(addr)
			if host == "" || host == "0.0.0.0" {
				host = "localhost"
			}
			cfg.RPID = host
		}
		if rpOrigin == "" {
			host, port, _ := net.SplitHostPort(addr)
			if host == "" || host == "0.0.0.0" {
				host = "localhost"
			}
			rpOrigin = fmt.Sprintf("%s://%s:%s", scheme, host, port)
		}
		fileCfg.Server.RPOrigin = rpOrigin
		cfg.RPOrigins = fileCfg.AllRPOrigins()
	}
	srv, err := server.New(queries, cfg)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	// Reclaim orphaned worktrees, /tmp artifacts, and leaked CLI subprocesses.
	//
	// Both sweeps are destructive and both judge "orphan" against this server's
	// own state, so they run ONLY when this server is the canonical instance for
	// its data dir (see ownsDataDir): a server pointed at a scratch DB sees real
	// artifacts as unowned, which is precisely how a sandboxed verify run comes
	// to delete production state.
	// Scheduled loops: reconcile runs stranded by an ungraceful exit, then
	// start the tick loop. Sweep strictly before Start (never in server.New)
	// so the first tick can't misread a stale `queued` row as a live
	// predecessor. Runs in test mode too — the scheduler is provider-neutral
	// and the mock connector exercises it end-to-end.
	if sched := srv.Scheduler(); sched != nil {
		sched.BootSweep(context.Background())
		sched.Start()
	}

	if !testMode && ownsDataDir(dbFile) {
		go srv.SweepOrphans(context.Background())

		// Reap CLI subprocesses orphaned by a prior server that exited without a
		// clean shutdown (crash / OOM-kill / SIGKILL). Each session CLI runs in
		// its own process group and is only a child of the server, so an
		// ungraceful exit reparents it to init where nothing signals it — it
		// survives until reboot, leaking a claude process plus its Playwright MCP
		// subtree across every restart. Safe here: we hold this data dir's
		// instance lock, the scan is scoped to CLIs stamped with our own data dir
		// (procctl.OwnerEnvVar), and we have not resumed any sessions — so
		// nothing matched can belong to a live server. Kept out of server.New — a
		// constructor must have no destructive side effects (a stray sweep there
		// once nuked real worktrees in tests).
		switch n, err := procctl.ReapOrphanedCLIProcesses(paths.DataDir()); {
		case err != nil:
			slog.Warn("orphan CLI reap skipped", "error", err)
		case n > 0:
			slog.Info("reaped orphaned CLI process groups on startup", "count", n)
		}
	} else if !testMode {
		slog.Info("startup reclaim skipped — not the canonical server for this data dir",
			"db", dbFile, "canonical_db", paths.DBPath())
	}

	authStatus := "enabled"
	if disableAuth {
		authStatus = "disabled"
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv,
	}

	// Record our PID so the tray (or `agentique stop`) can find and stop us.
	if !testMode {
		if err := writePIDFile(); err != nil {
			slog.Warn("could not write pid file", "error", err)
		} else {
			defer removePIDFile()
		}
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	listenErr := make(chan error, 1)
	go func() {
		host, port, _ := net.SplitHostPort(addr)
		if host == "" || host == "0.0.0.0" {
			host = "localhost"
		}
		slog.Info("server listening", "url", fmt.Sprintf("%s://%s:%s", scheme, host, port), "tls", tlsEnabled, "auth", authStatus)
		var err error
		if tlsEnabled {
			err = httpServer.ListenAndServeTLS(tlsCert, tlsKey)
		} else {
			err = httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			listenErr <- err
		}
	}()

	select {
	case err := <-listenErr:
		slog.Error("server error", "error", err)
		removePIDFile() // os.Exit skips deferred cleanup
		os.Exit(1)
	case <-done:
	}
	slog.Info("shutting down")

	// Release the port first so a restart doesn't hit EADDRINUSE while
	// sessions are still draining.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("http shutdown failed", "error", err)
	}

	srv.Shutdown()

	// Backstop: srv.Shutdown() gracefully closes every session, but the
	// cooperative Close path can time out (a busy turn not exiting on stdin EOF
	// within the grace window) and leave a CLI subprocess mid-kill. Force-kill
	// any that are still our children so nothing is orphaned when we exit.
	if !testMode {
		switch n, err := procctl.KillCLIChildrenOf(os.Getpid(), paths.DataDir()); {
		case err != nil:
			slog.Warn("shutdown backstop skipped", "error", err)
		case n > 0:
			slog.Warn("shutdown backstop force-killed surviving CLI process groups", "count", n)
		}
	}

	slog.Info("server stopped")
	return nil
}

// applyConfig merges config file values into package-level vars.
// CLI flags that were explicitly set take precedence.
func applyConfig(cmd *cobra.Command, cfg *config.Config) {
	flags := cmd.Flags()

	if !flags.Changed("addr") && cfg.Server.Addr != "" {
		addr = cfg.Server.Addr
	}
	if !flags.Changed("disable-auth") && cfg.Server.DisableAuth {
		disableAuth = cfg.Server.DisableAuth
	}
	if !flags.Changed("tls-cert") && cfg.Server.TLSCert != "" {
		tlsCert = cfg.Server.TLSCert
	}
	if !flags.Changed("tls-key") && cfg.Server.TLSKey != "" {
		tlsKey = cfg.Server.TLSKey
	}
	if !flags.Changed("rp-id") && cfg.Server.RPID != "" {
		rpID = cfg.Server.RPID
	}
	if !flags.Changed("rp-origin") && cfg.Server.RPOrigin != "" {
		rpOrigin = cfg.Server.RPOrigin
	}
	if !flags.Changed("log-level") && cfg.Logging.Level != "" {
		logLevel = cfg.Logging.Level
	}
	if !flags.Changed("log-output") && cfg.Logging.Output != "" {
		logOutput = cfg.Logging.Output
	}
	if !flags.Changed("backup-interval") && cfg.Backup.Interval != "" {
		backupInterval = cfg.Backup.Interval
	}
	if !flags.Changed("backup-retain") && cfg.Backup.Retain != 0 {
		backupRetain = cfg.Backup.Retain
	}
	if !flags.Changed("disable-backup") && cfg.Backup.Disabled {
		disableBackup = cfg.Backup.Disabled
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// findGitRoot walks up from dir to find the nearest .git directory.
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ensureDefaultProject creates a project if none exist.
// Uses initialProject from config if set, otherwise falls back to git root or cwd.
func ensureDefaultProject(q *store.Queries, initialProject string) {
	projects, err := q.ListProjects(context.Background())
	if err != nil || len(projects) > 0 {
		return
	}

	var projectDir string
	if initialProject != "" {
		if info, err := os.Stat(initialProject); err == nil && info.IsDir() {
			projectDir = initialProject
		}
	}
	if projectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return
		}
		projectDir = cwd
		if root := findGitRoot(cwd); root != "" {
			projectDir = root
		}
	}

	name := filepath.Base(projectDir)
	_, err = q.CreateProject(context.Background(), store.CreateProjectParams{
		ID:   uuid.NewString(),
		Name: name,
		Path: projectDir,
		Slug: project.Slugify(name),
	})
	if err != nil {
		slog.Warn("failed to create default project", "error", err)
		return
	}
	slog.Info("created default project", "name", name, "path", projectDir)
}
