package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBackup(t *testing.T, dir, name string, size int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func names(files []BackupFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Name)
	}
	return out
}

func TestClassifyBackup(t *testing.T) {
	tests := []struct {
		name     string
		ok       bool
		snapshot bool
		stamp    string
	}{
		{name: "agentique-20260828-032820.db", ok: true, stamp: "20260828-032820"},
		{name: "agentique-pre-20260828-032820.db", ok: true, snapshot: true, stamp: "20260828-032820"},
		// Anything a trim cannot name, a trim must not remove.
		{name: "agentique.db", ok: false},
		{name: "agentique-.db", ok: false},
		{name: "agentique-20260828-032820.db.tmp", ok: false},
		{name: "somethingelse-20260828-032820.db", ok: false},
		{name: "README", ok: false},
	}
	for _, tt := range tests {
		got, ok := classifyBackup(tt.name)
		if ok != tt.ok {
			t.Errorf("classifyBackup(%q) ok = %v, want %v", tt.name, ok, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Snapshot != tt.snapshot || got.Stamp != tt.stamp {
			t.Errorf("classifyBackup(%q) = {snapshot:%v stamp:%q}, want {snapshot:%v stamp:%q}",
				tt.name, got.Snapshot, got.Stamp, tt.snapshot, tt.stamp)
		}
	}
}

func TestListBackupsSortsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	writeBackup(t, dir, "agentique-20260101-000000.db", 1)
	writeBackup(t, dir, "agentique-20260828-120000.db", 2)
	writeBackup(t, dir, "agentique-20260501-060000.db", 3)
	writeBackup(t, dir, "unrelated.txt", 9)

	files, err := listBackups(dir)
	if err != nil {
		t.Fatalf("listBackups: %v", err)
	}
	want := []string{
		"agentique-20260828-120000.db",
		"agentique-20260501-060000.db",
		"agentique-20260101-000000.db",
	}
	got := names(files)
	if len(got) != len(want) {
		t.Fatalf("listBackups = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("listBackups = %v, want %v", got, want)
		}
	}
}

func TestListBackupsMissingDirIsNotAnError(t *testing.T) {
	files, err := listBackups(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("listBackups on a missing dir: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files, got %d", len(files))
	}
}

// The whole point of the two namespaces: a snapshot taken before a migration is
// not disk to be reclaimed, however old it is.
func TestPlanBackupTrimNeverTouchesSnapshots(t *testing.T) {
	files := []BackupFile{
		{Name: "agentique-20260828-120000.db", Stamp: "20260828-120000"},
		{Name: "agentique-20260827-120000.db", Stamp: "20260827-120000"},
		{Name: "agentique-20260826-120000.db", Stamp: "20260826-120000"},
		{Name: "agentique-20260825-120000.db", Stamp: "20260825-120000"},
		{Name: "agentique-pre-20260101-000000.db", Stamp: "20260101-000000", Snapshot: true},
		{Name: "agentique-pre-20250101-000000.db", Stamp: "20250101-000000", Snapshot: true},
	}
	got := names(planBackupTrim(files, 2))
	want := []string{"agentique-20260826-120000.db", "agentique-20260825-120000.db"}
	if len(got) != len(want) {
		t.Fatalf("planBackupTrim = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("planBackupTrim = %v, want %v", got, want)
		}
	}
}

// keep is a promise about periodic backups. Snapshots must not be counted
// toward it, or a directory full of snapshots would let a trim delete every
// periodic backup while believing it kept some.
func TestPlanBackupTrimKeepCountsPeriodicOnly(t *testing.T) {
	files := []BackupFile{
		{Name: "agentique-pre-a.db", Stamp: "a", Snapshot: true},
		{Name: "agentique-pre-b.db", Stamp: "b", Snapshot: true},
		{Name: "agentique-pre-c.db", Stamp: "c", Snapshot: true},
		{Name: "agentique-3.db", Stamp: "3"},
		{Name: "agentique-2.db", Stamp: "2"},
	}
	if got := planBackupTrim(files, 3); len(got) != 0 {
		t.Fatalf("expected nothing to trim with 2 periodic and keep=3, got %v", names(got))
	}
}

// A client cannot ask for an empty backup directory.
func TestPlanBackupTrimClampsKeepToTheFloor(t *testing.T) {
	files := []BackupFile{
		{Name: "agentique-4.db", Stamp: "4"},
		{Name: "agentique-3.db", Stamp: "3"},
		{Name: "agentique-2.db", Stamp: "2"},
		{Name: "agentique-1.db", Stamp: "1"},
	}
	for _, keep := range []int{-5, 0, 1} {
		got := planBackupTrim(files, keep)
		if len(got) != len(files)-minBackupsKept {
			t.Fatalf("keep=%d removed %d files, want %d", keep, len(got), len(files)-minBackupsKept)
		}
		// The newest survivors are the ones kept, never the oldest.
		for _, f := range got {
			if f.Stamp > "2" {
				t.Fatalf("keep=%d would remove %q, which is among the newest", keep, f.Name)
			}
		}
	}
}

func TestPlanBackupTrimNothingToDo(t *testing.T) {
	if got := planBackupTrim(nil, 3); got != nil {
		t.Fatalf("expected nil for an empty directory, got %v", names(got))
	}
	files := []BackupFile{{Name: "agentique-1.db", Stamp: "1"}}
	if got := planBackupTrim(files, 3); got != nil {
		t.Fatalf("expected nil when under the keep count, got %v", names(got))
	}
}

func TestSummarizeBackupsSplitsNamespaces(t *testing.T) {
	dir := t.TempDir()
	writeBackup(t, dir, "agentique-20260828-120000.db", 100)
	writeBackup(t, dir, "agentique-20260827-120000.db", 100)
	writeBackup(t, dir, "agentique-20260826-120000.db", 100)
	writeBackup(t, dir, "agentique-20260101-000000.db", 100)
	writeBackup(t, dir, "agentique-pre-20260501-000000.db", 50)

	got := summarizeBackups(dir)
	if got == nil {
		t.Fatal("summarizeBackups returned nil")
	}
	if got.PeriodicCount != 4 || got.PeriodicBytes != 400 {
		t.Errorf("periodic = %d/%d, want 4/400", got.PeriodicCount, got.PeriodicBytes)
	}
	if got.SnapshotCount != 1 || got.SnapshotBytes != 50 {
		t.Errorf("snapshots = %d/%d, want 1/50", got.SnapshotCount, got.SnapshotBytes)
	}
	if got.OldestPeriodic != "20260101-000000" {
		t.Errorf("oldest = %q, want the earliest periodic stamp", got.OldestPeriodic)
	}
	// The page offers the verb only when it would do something.
	if got.Trimmable != 1 {
		t.Errorf("trimmable = %d, want 1 (4 periodic, default keep %d)", got.Trimmable, DefaultBackupsKept)
	}
}

func TestSummarizeBackupsEmpty(t *testing.T) {
	if got := summarizeBackups(t.TempDir()); got != nil {
		t.Fatalf("expected nil for an empty directory, got %+v", got)
	}
}
