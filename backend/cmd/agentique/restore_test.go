package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/allbin/agentkit/sqliteops"
	_ "modernc.org/sqlite"
)

// writeFile drops a placeholder backup file into dir.
func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// names lists dir's files, sorted.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func has(list []string, want string) bool {
	for _, n := range list {
		if n == want {
			return true
		}
	}
	return false
}

// safetyName builds a restore safety copy name offset by n minutes from a
// fixed base, newest at n = 0.
func safetyName(n int) string {
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	return restoreSafetyPrefix + base.Add(-time.Duration(n)*time.Minute).Format(restoreSafetyTimeLayout) + ".db"
}

// newTestDB opens a tiny on-disk SQLite database that VACUUM INTO can copy.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

// A restore safety copy is the only copy of a database the operator just
// overwrote. Startup snapshot pruning keeps five entries in sqliteops'
// "agentique-pre-" namespace; while safety copies were named
// "agentique-pre-restore-*" they matched that prefix, so the two kinds
// competed for the same five slots — an unrelated restart could evict a
// safety copy, and enough safety copies wiped out every startup snapshot.
func TestRestoreSafetyCopiesDoNotShareTheSnapshotPool(t *testing.T) {
	dir := t.TempDir()
	db := newTestDB(t)

	// Fill the safety-copy pool to its own quota.
	var safety []string
	for i := 0; i < restoreSafetyRetain; i++ {
		n := safetyName(i)
		safety = append(safety, n)
		writeFile(t, dir, n)
	}

	// Four existing startup snapshots; the fifth is the one Snapshot writes.
	snapshots := []string{
		"agentique-pre-20260824-080000.db",
		"agentique-pre-20260824-090000.db",
		"agentique-pre-20260824-100000.db",
		"agentique-pre-20260824-110000.db",
	}
	for _, n := range snapshots {
		writeFile(t, dir, n)
	}

	created, err := sqliteops.Snapshot(db, dir, "agentique", 5)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	got := names(t, dir)

	// The snapshot pool must hold its full quota: the four prior snapshots
	// plus the new one. Under the shared-pool bug all five were deleted,
	// including the snapshot that had just been written.
	for _, n := range append(snapshots, filepath.Base(created)) {
		if !has(got, n) {
			t.Errorf("startup snapshot %s was evicted by restore safety copies; dir = %v", n, got)
		}
	}

	// And pruning the snapshot pool must not reach into the safety copies.
	for _, n := range safety {
		if !has(got, n) {
			t.Errorf("restore safety copy %s was evicted by an unrelated startup snapshot; dir = %v", n, got)
		}
	}
}

// The tiered periodic prune keys on a parseable timestamp, so a safety copy
// must never look like a periodic backup either.
func TestRestoreSafetyCopyIsNotAPeriodicBackupName(t *testing.T) {
	name := restoreSafetyPrefix + "20260824-120000.db"
	rest := strings.TrimSuffix(strings.TrimPrefix(name, "agentique-"), ".db")
	if _, err := time.Parse(restoreSafetyTimeLayout, rest); err == nil {
		t.Fatalf("%s parses as a periodic backup timestamp (%q); the tiered prune would own it", name, rest)
	}
}

func TestPruneRestoreSafetyCopiesKeepsNewest(t *testing.T) {
	dir := t.TempDir()

	// One more than the quota, oldest last.
	var all []string
	for i := 0; i <= restoreSafetyRetain; i++ {
		n := safetyName(i)
		all = append(all, n)
		writeFile(t, dir, n)
	}
	oldest := all[len(all)-1]

	pruneRestoreSafetyCopies(dir)

	got := names(t, dir)
	if has(got, oldest) {
		t.Errorf("oldest safety copy %s survived the quota; dir = %v", oldest, got)
	}
	for _, n := range all[:restoreSafetyRetain] {
		if !has(got, n) {
			t.Errorf("safety copy %s within quota was pruned; dir = %v", n, got)
		}
	}
}

// Safety copies written by an older build carry the legacy prefix. They share
// the new pool — ordered by timestamp, not filename, so the two prefixes
// interleave — rather than being orphaned on disk forever.
func TestPruneRestoreSafetyCopiesAdoptsLegacyNames(t *testing.T) {
	dir := t.TempDir()

	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	legacyNewest := legacyRestoreSafetyPrefix + base.Format(restoreSafetyTimeLayout) + ".db"
	writeFile(t, dir, legacyNewest)

	// Current-prefix copies, all older than the legacy one. Lexicographically
	// they sort before it ("2" < "r"), so a name-ordered prune would drop the
	// newest current copy and keep the oldest legacy one.
	var current []string
	for i := 1; i <= restoreSafetyRetain; i++ {
		n := restoreSafetyPrefix + base.Add(-time.Duration(i)*time.Hour).Format(restoreSafetyTimeLayout) + ".db"
		current = append(current, n)
		writeFile(t, dir, n)
	}
	oldestCurrent := current[len(current)-1]

	pruneRestoreSafetyCopies(dir)

	got := names(t, dir)
	if !has(got, legacyNewest) {
		t.Errorf("newest copy %s was pruned; dir = %v", legacyNewest, got)
	}
	if has(got, oldestCurrent) {
		t.Errorf("oldest copy %s survived the quota; dir = %v", oldestCurrent, got)
	}
	if len(got) != restoreSafetyRetain {
		t.Errorf("pool holds %d copies, want %d; dir = %v", len(got), restoreSafetyRetain, got)
	}
}

// Pruning is scoped to the safety-copy namespace: periodic backups, startup
// snapshots and unrelated files are never candidates.
func TestPruneRestoreSafetyCopiesTouchesNothingElse(t *testing.T) {
	dir := t.TempDir()

	bystanders := []string{
		"agentique-20260824-120000.db",     // periodic
		"agentique-pre-20260824-120000.db", // startup snapshot
		"agentique-restore-not-a-time.db",  // right prefix, unparseable
		"agentique-pre-restore-garbage.db", // legacy prefix, unparseable
		"unrelated.db",
	}
	for _, n := range bystanders {
		writeFile(t, dir, n)
	}
	// Enough safety copies to force a prune.
	for i := 0; i <= restoreSafetyRetain; i++ {
		writeFile(t, dir, safetyName(i))
	}

	pruneRestoreSafetyCopies(dir)

	got := names(t, dir)
	for _, n := range bystanders {
		if !has(got, n) {
			t.Errorf("prune removed %s, which is not a safety copy; dir = %v", n, got)
		}
	}
}

// Safety copies stay out of the restore listing, whichever prefix they carry —
// the list is for backups you would restore FROM.
func TestListBackupsHidesSafetyCopies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agentique-20260824-120000.db")
	writeFile(t, dir, "agentique-pre-20260824-120000.db")
	writeFile(t, dir, safetyName(0))
	writeFile(t, dir, legacyRestoreSafetyPrefix+"20260824-110000.db")

	entries, err := listBackups(dir)
	if err != nil {
		t.Fatalf("listBackups: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("listed %d backups, want 2: %v", len(entries), entries)
	}
	for _, e := range entries {
		if isRestoreSafetyCopy(e.name) {
			t.Errorf("safety copy %s appeared in the restore listing", e.name)
		}
	}
}

func TestParseTimestampFromNameHandlesEveryNamespace(t *testing.T) {
	want := "2026-08-24 12:00:00"
	for _, name := range []string{
		"agentique-20260824-120000.db",
		"agentique-pre-20260824-120000.db",
		restoreSafetyPrefix + "20260824-120000.db",
		legacyRestoreSafetyPrefix + "20260824-120000.db",
	} {
		if got := parseTimestampFromName(name); got != want {
			t.Errorf("parseTimestampFromName(%q) = %q, want %q", name, got, want)
		}
	}
}
