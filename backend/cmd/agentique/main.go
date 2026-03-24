package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	dbpkg "github.com/allbin/agentique/backend/db"
	"github.com/allbin/agentique/backend/internal/logging"
	"github.com/allbin/agentique/backend/internal/server"
	"github.com/allbin/agentique/backend/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

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
	srv := server.New(queries)

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: srv,
	}

	// Graceful shutdown on interrupt or SIGTERM.
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	listenErr := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", *addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

	// Close all live sessions before shutting down HTTP.
	srv.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
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
// if no projects exist. This gives a ready-to-use experience on first launch.
func ensureDefaultProject(q *store.Queries) {
	projects, err := q.ListProjects(context.Background())
	if err != nil || len(projects) > 0 {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	// Prefer the git root so the project covers the whole repo.
	projectDir := cwd
	if root := findGitRoot(cwd); root != "" {
		projectDir = root
	}

	name := filepath.Base(projectDir)
	_, err = q.CreateProject(context.Background(), store.CreateProjectParams{
		ID:   uuid.NewString(),
		Name: name,
		Path: projectDir,
	})
	if err != nil {
		slog.Warn("failed to create default project", "error", err)
		return
	}
	slog.Info("created default project", "name", name, "path", projectDir)
}
