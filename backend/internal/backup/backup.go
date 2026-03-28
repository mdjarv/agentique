package backup

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Config holds backup configuration.
type Config struct {
	DB       *sql.DB
	Dir      string
	Interval time.Duration
	Retain   int
}

// Start begins periodic backups in a background goroutine.
// Returns a stop function for graceful shutdown.
func Start(cfg Config) (stop func()) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		slog.Error("backup: failed to create directory", "dir", cfg.Dir, "error", err)
		return func() {}
	}

	ticker := time.NewTicker(cfg.Interval)
	done := make(chan struct{})

	go func() {
		run(cfg)
		for {
			select {
			case <-ticker.C:
				run(cfg)
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	return func() { close(done) }
}

func run(cfg Config) {
	filename := fmt.Sprintf("agentique-%s.db", time.Now().UTC().Format("20060102-150405"))
	dest := filepath.Join(cfg.Dir, filename)

	if _, err := cfg.DB.Exec("VACUUM INTO ?", dest); err != nil {
		slog.Error("backup: VACUUM INTO failed", "dest", dest, "error", err)
		return
	}

	slog.Info("backup: created", "file", dest)
	prune(cfg.Dir, cfg.Retain)
}

func prune(dir string, retain int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Error("backup: failed to read directory", "dir", dir, "error", err)
		return
	}

	var backups []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "agentique-") && strings.HasSuffix(e.Name(), ".db") {
			backups = append(backups, e)
		}
	}

	if len(backups) <= retain {
		return
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Name() < backups[j].Name()
	})

	for _, b := range backups[:len(backups)-retain] {
		path := filepath.Join(dir, b.Name())
		if err := os.Remove(path); err != nil {
			slog.Error("backup: failed to remove old backup", "file", path, "error", err)
		} else {
			slog.Info("backup: pruned", "file", path)
		}
	}
}
