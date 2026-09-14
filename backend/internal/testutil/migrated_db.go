package testutil

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// The migrated schema is built once and copied into each test. Running every
// migration per test cost ~2s under -race (modernc sqlite is transpiled C),
// which put a package of two hundred DB tests past go test's default ten-minute
// timeout — a stall that read like a deadlock.
//
// Building it once per test binary still cost ~3.5s in each of a dozen
// binaries, so the result is also cached on disk under a hash of the migration
// files, the way GOCACHE keys a build by its inputs: a changed or added
// migration is a different key, never a stale hit.
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
	templateOnce.Do(func() { templateBytes, templateErr = loadTemplate(dbpkg.Migrations) })
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

// loadTemplate returns the cached template for these migrations, building and
// caching it on a miss. The cache is an optimisation only: failing to read or
// write it falls back to the build.
func loadTemplate(migrations fs.FS) ([]byte, error) {
	key, err := migrationsKey(migrations)
	if err != nil {
		return nil, fmt.Errorf("hash migrations: %w", err)
	}
	cached := filepath.Join(templateCacheDir(), key+".db")
	if b, err := os.ReadFile(cached); err == nil {
		return b, nil
	}

	b, err := buildTemplate(migrations)
	if err != nil {
		return nil, err
	}
	storeTemplate(cached, b)
	return b, nil
}

// migrationsKey hashes every migration file's path and contents. fs.WalkDir
// visits in lexical order, so the key is stable across runs.
func migrationsKey(migrations fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(migrations, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(migrations, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", path, len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:32], nil
}

func templateCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "agentique", "test-db-templates")
}

// storeTemplate writes through a temp file and a rename, so a test binary
// running concurrently reads either nothing or a whole file. Best-effort.
func storeTemplate(path string, b []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
	}
}

// buildTemplate migrates a scratch database and returns it as one standalone
// file's bytes. VACUUM INTO writes a self-contained copy with no WAL sidecar,
// so the bytes are the whole database; store.Open re-enables WAL on each copy.
func buildTemplate(migrations fs.FS) ([]byte, error) {
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

	if err := store.RunMigrations(db, migrations); err != nil {
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
