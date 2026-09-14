package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// The migrated schema is built once per test binary and copied into each test.
// Running every migration per test cost ~2s under -race (modernc sqlite is
// transpiled C), which put a package of two hundred DB tests past go test's
// default ten-minute timeout — a stall that read like a deadlock.
var (
	templateOnce  sync.Once
	templateBytes []byte
	templateErr   error
)

// OpenMigratedDB opens a fresh database in t.TempDir() holding the fully
// migrated schema, closed when the test finishes. Every call gets its own file,
// so tests share nothing but the starting bytes.
func OpenMigratedDB(t testing.TB) *sql.DB {
	t.Helper()
	templateOnce.Do(func() { templateBytes, templateErr = buildTemplate() })
	if templateErr != nil {
		t.Fatalf("build migrated template: %v", templateErr)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(dbPath, templateBytes, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// buildTemplate migrates a scratch database and returns it as one standalone
// file's bytes. VACUUM INTO writes a self-contained copy with no WAL sidecar,
// so the bytes are the whole database; store.Open re-enables WAL on each copy.
func buildTemplate() ([]byte, error) {
	dir, err := os.MkdirTemp("", "agentique-testdb-")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	db, err := store.Open(filepath.Join(dir, "migrate.db"))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	snapshot := filepath.Join(dir, "template.db")
	if _, err := db.Exec("VACUUM INTO ?", snapshot); err != nil {
		return nil, fmt.Errorf("vacuum into: %w", err)
	}
	b, err := os.ReadFile(snapshot)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	return b, nil
}
