package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	dbpkg "github.com/allbin/agentique/backend/db"
	"github.com/allbin/agentique/backend/internal/server"
	"github.com/allbin/agentique/backend/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	db, err := store.Open("agentique.db")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
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

	go func() {
		log.Printf("Agentique server listening on %s", *addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-done
	log.Println("shutting down server...")

	// Close all live sessions before shutting down HTTP.
	srv.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown error: %v", err)
	}

	log.Println("server stopped")
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
		log.Printf("failed to create default project: %v", err)
		return
	}
	log.Printf("created default project %q (%s)", name, projectDir)
}
