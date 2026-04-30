package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/allbin/agentkit/sqliteops"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/doctor"
	"github.com/mdjarv/agentique/backend/internal/logging"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/server"
	"github.com/mdjarv/agentique/backend/internal/store"
)

var (
	dbPath      string
	disableAuth bool
	logLevel    string
	rpID        string
	rpOrigin    string
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

	cfg := server.Config{
		AuthEnabled:       !disableAuth,
		TestMode:          testMode,
		DevMode:           !isRelease(),
		DBPath:            dbFile,
		DB:                db,
		ExperimentalTeams:   fileCfg.Experimental.Teams,
		ExperimentalBrowser: fileCfg.Experimental.Browser,
		DevURLSlots:         fileCfg.DevURLs,
		MCPInternalURL:      mcpInternalURL,
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

	authStatus := "enabled"
	if disableAuth {
		authStatus = "disabled"
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv,
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
