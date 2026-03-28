package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRun(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	cfg := Config{DB: db, Dir: dir, Retain: 10}
	run(cfg)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(entries))
	}
	if ext := filepath.Ext(entries[0].Name()); ext != ".db" {
		t.Fatalf("expected .db extension, got %s", ext)
	}
}

func TestPrune(t *testing.T) {
	dir := t.TempDir()

	// Create 5 fake backup files with ordered timestamps.
	for i := range 5 {
		name := "agentique-20260101-" + time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("150405") + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	prune(dir, 3)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 backups after prune, got %d", len(entries))
	}

	// Oldest should be pruned, newest kept.
	if entries[0].Name() != "agentique-20260101-020000.db" {
		t.Fatalf("expected oldest remaining to be 020000, got %s", entries[0].Name())
	}
}

func TestPruneIgnoresNonBackupFiles(t *testing.T) {
	dir := t.TempDir()

	// Create 3 backups + 1 unrelated file.
	for i := range 3 {
		name := "agentique-20260101-" + time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("150405") + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	prune(dir, 2)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 2 backups + 1 unrelated = 3 total.
	if len(entries) != 3 {
		t.Fatalf("expected 3 files, got %d", len(entries))
	}
}
