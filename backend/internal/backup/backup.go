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

const (
	periodicPrefix = "agentique-"
	prePrefix      = "agentique-pre-"
	suffix         = ".db"
	timeLayout     = "20060102-150405"
)

// Config holds backup configuration.
type Config struct {
	DB          *sql.DB
	Dir         string
	Interval    time.Duration
	DailyRetain int // days to keep daily backups (tiered retention)
}

// TieredConfig controls tiered retention policy.
type TieredConfig struct {
	Dir            string
	RecentDuration time.Duration
	DailyDays      int
	Now            func() time.Time // injectable clock for testing
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

// Snapshot creates a pre-startup backup before migrations run.
// Uses a separate "agentique-pre-" prefix so periodic prune never touches these.
func Snapshot(db *sql.DB, dir string, retain int) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("backup: snapshot: failed to create directory", "dir", dir, "error", err)
		return
	}

	filename := fmt.Sprintf("%s%s%s", prePrefix, time.Now().UTC().Format(timeLayout), suffix)
	dest := filepath.Join(dir, filename)

	if _, err := db.Exec("VACUUM INTO ?", dest); err != nil {
		slog.Error("backup: snapshot: VACUUM INTO failed", "dest", dest, "error", err)
		return
	}

	if err := verifyBackup(dest); err != nil {
		slog.Error("backup: snapshot: integrity check failed, removing", "dest", dest, "error", err)
		os.Remove(dest)
		return
	}

	slog.Info("backup: pre-startup snapshot created", "file", dest)
	pruneByPrefix(dir, prePrefix, nil, retain)
}

// Metadata holds summary counts from a backup database.
type Metadata struct {
	Projects int64
	Sessions int64
	Events   int64
}

// BackupMetadata opens a backup DB read-only and returns row counts.
// Returns zero Metadata and an error if the DB cannot be read.
func BackupMetadata(path string) (Metadata, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return Metadata{}, err
	}
	defer db.Close()

	var m Metadata
	row := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM projects),
		(SELECT COUNT(*) FROM sessions),
		(SELECT COUNT(*) FROM session_events)`)
	if err := row.Scan(&m.Projects, &m.Sessions, &m.Events); err != nil {
		return Metadata{}, err
	}
	return m, nil
}

// parseBackupTime extracts the UTC timestamp from a periodic backup filename.
// Expected format: agentique-YYYYMMDD-HHMMSS.db
// Returns zero time and false for non-matching names (including agentique-pre-*).
func parseBackupTime(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, periodicPrefix) || strings.HasPrefix(name, prePrefix) {
		return time.Time{}, false
	}
	if !strings.HasSuffix(name, suffix) {
		return time.Time{}, false
	}
	ts := name[len(periodicPrefix) : len(name)-len(suffix)]
	t, err := time.Parse(timeLayout, ts)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// parsePreBackupTime extracts the UTC timestamp from a pre-startup backup filename.
func parsePreBackupTime(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, prePrefix) || !strings.HasSuffix(name, suffix) {
		return time.Time{}, false
	}
	ts := name[len(prePrefix) : len(name)-len(suffix)]
	t, err := time.Parse(timeLayout, ts)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// verifyBackup opens a backup file and runs a quick integrity check.
func verifyBackup(path string) error {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()

	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("quick_check: %s", result)
	}
	return nil
}

func run(cfg Config) {
	filename := fmt.Sprintf("%s%s%s", periodicPrefix, time.Now().UTC().Format(timeLayout), suffix)
	dest := filepath.Join(cfg.Dir, filename)

	if _, err := cfg.DB.Exec("VACUUM INTO ?", dest); err != nil {
		slog.Error("backup: VACUUM INTO failed", "dest", dest, "error", err)
		return
	}

	if err := verifyBackup(dest); err != nil {
		slog.Error("backup: integrity check failed, removing", "dest", dest, "error", err)
		os.Remove(dest)
		return
	}

	slog.Info("backup: created", "file", dest)
	pruneTiered(TieredConfig{
		Dir:            cfg.Dir,
		RecentDuration: 2 * time.Hour,
		DailyDays:      cfg.DailyRetain,
		Now:            time.Now,
	})
}

// pruneTiered applies tiered retention to periodic backups:
//   - Keep all backups within RecentDuration
//   - Keep 1 per calendar day (newest) for the last DailyDays
//   - Delete everything older
//
// Pre-startup snapshots (agentique-pre-*) are never touched.
func pruneTiered(cfg TieredConfig) {
	entries, err := os.ReadDir(cfg.Dir)
	if err != nil {
		slog.Error("backup: failed to read directory", "dir", cfg.Dir, "error", err)
		return
	}

	now := cfg.Now()
	recentCutoff := now.Add(-cfg.RecentDuration)
	dailyCutoff := now.AddDate(0, 0, -cfg.DailyDays)

	type backup struct {
		name string
		t    time.Time
	}

	// daily candidates grouped by calendar day (UTC)
	dailyGroups := make(map[string][]backup) // key: "YYYY-MM-DD"
	var toDelete []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		t, ok := parseBackupTime(e.Name())
		if !ok {
			continue
		}

		switch {
		case t.After(recentCutoff):
			// recent — keep
		case t.After(dailyCutoff):
			// daily candidate
			day := t.Format("2006-01-02")
			dailyGroups[day] = append(dailyGroups[day], backup{e.Name(), t})
		default:
			// ancient — delete
			toDelete = append(toDelete, e.Name())
		}
	}

	// For each day, keep only the newest backup.
	for _, group := range dailyGroups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].t.After(group[j].t)
		})
		for _, b := range group[1:] {
			toDelete = append(toDelete, b.name)
		}
	}

	for _, name := range toDelete {
		path := filepath.Join(cfg.Dir, name)
		if err := os.Remove(path); err != nil {
			slog.Error("backup: failed to remove old backup", "file", path, "error", err)
		} else {
			slog.Info("backup: pruned", "file", path)
		}
	}
}

// pruneByPrefix removes oldest files matching prefix (excluding any excludePrefixes)
// in dir, keeping at most retain files.
func pruneByPrefix(dir string, prefix string, excludePrefixes []string, retain int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Error("backup: failed to read directory", "dir", dir, "error", err)
		return
	}

	var matches []os.DirEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		excluded := false
		for _, ep := range excludePrefixes {
			if strings.HasPrefix(e.Name(), ep) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		matches = append(matches, e)
	}

	if len(matches) <= retain {
		return
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Name() < matches[j].Name()
	})

	for _, b := range matches[:len(matches)-retain] {
		path := filepath.Join(dir, b.Name())
		if err := os.Remove(path); err != nil {
			slog.Error("backup: failed to remove old backup", "file", path, "error", err)
		} else {
			slog.Info("backup: pruned", "file", path)
		}
	}
}
