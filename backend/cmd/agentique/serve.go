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

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	dbpkg "github.com/allbin/agentique/backend/db"
	"github.com/allbin/agentique/backend/internal/logging"
	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/server"
	"github.com/allbin/agentique/backend/internal/store"
)

var (
	disableAuth bool
	rpID        string
	rpOrigin    string
	tlsCert     string
	tlsKey      string
)

func init() {
	serveCmd.Flags().BoolVar(&disableAuth, "disable-auth", false, "disable authentication (allow anonymous access)")
	serveCmd.Flags().StringVar(&rpID, "rp-id", "", "WebAuthn relying party ID (default: hostname from --addr)")
	serveCmd.Flags().StringVar(&rpOrigin, "rp-origin", "", "WebAuthn relying party origin (default: derived from --addr)")
	serveCmd.Flags().StringVar(&tlsCert, "tls-cert", "", "path to TLS certificate file")
	serveCmd.Flags().StringVar(&tlsKey, "tls-key", "", "path to TLS key file")
	rootCmd.AddCommand(serveCmd)
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Agentique server",
	RunE:  runServe,
}

func runServe(cmd *cobra.Command, args []string) error {
	logging.Init()

	db, err := store.Open("agentique.db")
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	queries := store.New(db)
	ensureDefaultProject(queries)

	tlsEnabled := tlsCert != "" && tlsKey != ""
	if (tlsCert != "") != (tlsKey != "") {
		slog.Error("--tls-cert and --tls-key must both be provided")
		os.Exit(1)
	}

	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	cfg := server.Config{
		AuthEnabled: !disableAuth,
	}
	if cfg.AuthEnabled {
		cfg.RPID = rpID
		cfg.RPOrigins = []string{rpOrigin}
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
			cfg.RPOrigins = []string{fmt.Sprintf("%s://%s:%s", scheme, host, port)}
		}
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

	listenAddr := addr
	if listenAddr == "localhost:9201" {
		// Default — use :9201 for binding (all interfaces in dev).
		listenAddr = ":9201"
	}

	httpServer := &http.Server{
		Addr:    listenAddr,
		Handler: srv,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	listenErr := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", listenAddr, "tls", tlsEnabled, "auth", authStatus)
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

	srv.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
	return nil
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

// ensureDefaultProject creates a project for the git root (or cwd)
// if no projects exist.
func ensureDefaultProject(q *store.Queries) {
	projects, err := q.ListProjects(context.Background())
	if err != nil || len(projects) > 0 {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	projectDir := cwd
	if root := findGitRoot(cwd); root != "" {
		projectDir = root
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
