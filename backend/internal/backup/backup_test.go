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

// openTestDBWithSchema creates a DB that BackupMetadata can query.
func openTestDBWithSchema(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, stmt := range []string{
		"CREATE TABLE projects (id TEXT PRIMARY KEY)",
		"CREATE TABLE sessions (id TEXT PRIMARY KEY)",
		"CREATE TABLE session_events (id INTEGER PRIMARY KEY)",
		"INSERT INTO projects (id) VALUES ('p1'), ('p2')",
		"INSERT INTO sessions (id) VALUES ('s1'), ('s2'), ('s3')",
		"INSERT INTO session_events (id) VALUES (1), (2)",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return db, path
}

func TestRun(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	cfg := Config{DB: db, Dir: dir, DailyRetain: 7}
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

func TestPruneByPrefix(t *testing.T) {
	dir := t.TempDir()

	for i := range 5 {
		name := "agentique-20260101-" + time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("150405") + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pruneByPrefix(dir, periodicPrefix, []string{prePrefix}, 3)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 backups after prune, got %d", len(entries))
	}

	if entries[0].Name() != "agentique-20260101-020000.db" {
		t.Fatalf("expected oldest remaining to be 020000, got %s", entries[0].Name())
	}
}

func TestPruneByPrefixIgnoresNonBackupFiles(t *testing.T) {
	dir := t.TempDir()

	for i := range 3 {
		name := "agentique-20260101-" + time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("150405") + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	pruneByPrefix(dir, periodicPrefix, []string{prePrefix}, 2)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 files (2 backups + 1 other), got %d", len(entries))
	}
}

func TestPruneByPrefixExcludesPreSnapshots(t *testing.T) {
	dir := t.TempDir()

	// Create 3 periodic + 2 pre-startup backups.
	for i := range 3 {
		name := "agentique-20260101-" + time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("150405") + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 2 {
		name := "agentique-pre-20260101-" + time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("150405") + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Prune periodic to keep 1 — pre-snapshots must survive.
	pruneByPrefix(dir, periodicPrefix, []string{prePrefix}, 1)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 files (1 periodic + 2 pre), got %d", len(entries))
	}

	var preCount, periodicCount int
	for _, e := range entries {
		if isPreBackup(e.Name()) {
			preCount++
		} else {
			periodicCount++
		}
	}
	if preCount != 2 {
		t.Fatalf("expected 2 pre-snapshots, got %d", preCount)
	}
	if periodicCount != 1 {
		t.Fatalf("expected 1 periodic backup, got %d", periodicCount)
	}
}

func TestSnapshot(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	Snapshot(db, dir, 3)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(entries))
	}
	if name := entries[0].Name(); !isPreBackup(name) {
		t.Fatalf("expected agentique-pre- prefix, got %s", name)
	}
}

func TestSnapshotPrune(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	// Create 5 pre-snapshots + 2 periodic (should not be touched).
	for i := range 5 {
		name := "agentique-pre-20260101-" + time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("150405") + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 2 {
		name := "agentique-20260101-" + time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("150405") + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	Snapshot(db, dir, 3)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var preCount, periodicCount int
	for _, e := range entries {
		if isPreBackup(e.Name()) {
			preCount++
		} else if isPeriodicBackup(e.Name()) {
			periodicCount++
		}
	}

	if preCount != 3 {
		t.Fatalf("expected 3 pre-snapshots after prune, got %d", preCount)
	}
	if periodicCount != 2 {
		t.Fatalf("expected 2 periodic backups untouched, got %d", periodicCount)
	}
}

func TestParseBackupTime(t *testing.T) {
	tests := []struct {
		name    string
		wantOK  bool
		wantStr string
	}{
		{"agentique-20260329-085012.db", true, "2026-03-29 08:50:12"},
		{"agentique-pre-20260329-085012.db", false, ""},
		{"agentique-20260329-085012.txt", false, ""},
		{"other-20260329-085012.db", false, ""},
		{"agentique-.db", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseBackupTime(tt.name)
			if ok != tt.wantOK {
				t.Fatalf("parseBackupTime(%q) ok=%v, want %v", tt.name, ok, tt.wantOK)
			}
			if ok && got.Format("2006-01-02 15:04:05") != tt.wantStr {
				t.Fatalf("parseBackupTime(%q) = %v, want %s", tt.name, got, tt.wantStr)
			}
		})
	}
}

func TestParsePreBackupTime(t *testing.T) {
	got, ok := parsePreBackupTime("agentique-pre-20260329-085012.db")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.Format("2006-01-02 15:04:05") != "2026-03-29 08:50:12" {
		t.Fatalf("got %v, want 2026-03-29 08:50:12", got)
	}

	_, ok = parsePreBackupTime("agentique-20260329-085012.db")
	if ok {
		t.Fatal("expected ok=false for periodic backup name")
	}
}

func TestPruneTieredKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)

	// Create 5 backups all within the last 2 hours.
	for i := range 5 {
		ts := now.Add(-time.Duration(i*20) * time.Minute)
		name := "agentique-" + ts.Format(timeLayout) + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pruneTiered(TieredConfig{Dir: dir, RecentDuration: 2 * time.Hour, DailyDays: 7, Now: func() time.Time { return now }})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected all 5 recent backups kept, got %d", len(entries))
	}
}

func TestPruneTieredKeepsOneDailyPerDay(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)

	// Create 3 backups per day for 3 days (all older than 2h).
	for day := 1; day <= 3; day++ {
		for hour := range 3 {
			ts := now.AddDate(0, 0, -day).Add(time.Duration(hour) * time.Hour)
			name := "agentique-" + ts.Format(timeLayout) + ".db"
			if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	pruneTiered(TieredConfig{Dir: dir, RecentDuration: 2 * time.Hour, DailyDays: 7, Now: func() time.Time { return now }})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 daily backups (1 per day), got %d", len(entries))
	}
}

func TestPruneTieredDeletesAncient(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)

	// Create backups older than 7 days.
	for day := 8; day <= 10; day++ {
		ts := now.AddDate(0, 0, -day)
		name := "agentique-" + ts.Format(timeLayout) + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pruneTiered(TieredConfig{Dir: dir, RecentDuration: 2 * time.Hour, DailyDays: 7, Now: func() time.Time { return now }})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected all ancient backups deleted, got %d", len(entries))
	}
}

func TestPruneTieredIgnoresPreSnapshots(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)

	// Create 1 ancient periodic + 2 pre-snapshots (also ancient).
	ts := now.AddDate(0, 0, -10)
	for _, prefix := range []string{"agentique-", "agentique-pre-", "agentique-pre-"} {
		name := prefix + ts.Format(timeLayout) + ".db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
		ts = ts.Add(time.Second) // unique names
	}

	pruneTiered(TieredConfig{Dir: dir, RecentDuration: 2 * time.Hour, DailyDays: 7, Now: func() time.Time { return now }})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 pre-snapshots (periodic deleted), got %d", len(entries))
	}
}

func TestPruneTieredMixed(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)

	// Recent: 3 backups within 2h — all kept.
	for i := range 3 {
		ts := now.Add(-time.Duration(i*30) * time.Minute)
		name := "agentique-" + ts.Format(timeLayout) + ".db"
		os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644)
	}
	// Daily: 2 per day for 2 days — keep 1 per day = 2.
	for day := 1; day <= 2; day++ {
		for _, h := range []int{6, 18} {
			ts := now.AddDate(0, 0, -day).Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour)
			name := "agentique-" + ts.Format(timeLayout) + ".db"
			os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644)
		}
	}
	// Ancient: 2 backups older than 7 days — all deleted.
	for day := 8; day <= 9; day++ {
		ts := now.AddDate(0, 0, -day)
		name := "agentique-" + ts.Format(timeLayout) + ".db"
		os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644)
	}

	pruneTiered(TieredConfig{Dir: dir, RecentDuration: 2 * time.Hour, DailyDays: 7, Now: func() time.Time { return now }})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 3 recent + 2 daily keepers = 5
	if len(entries) != 5 {
		t.Fatalf("expected 5 backups (3 recent + 2 daily), got %d", len(entries))
	}
}

func TestBackupMetadata(t *testing.T) {
	db, path := openTestDBWithSchema(t)
	db.Close()

	m, err := BackupMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Projects != 2 {
		t.Fatalf("expected 2 projects, got %d", m.Projects)
	}
	if m.Sessions != 3 {
		t.Fatalf("expected 3 sessions, got %d", m.Sessions)
	}
	if m.Events != 2 {
		t.Fatalf("expected 2 events, got %d", m.Events)
	}
}

// helpers

func isPreBackup(name string) bool {
	_, ok := parsePreBackupTime(name)
	return ok
}

func isPeriodicBackup(name string) bool {
	_, ok := parseBackupTime(name)
	return ok
}
