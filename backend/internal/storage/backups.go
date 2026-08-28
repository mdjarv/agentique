package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

// Database backups are the one thing on the Storage page that exists to undo a
// disaster, so trimming them is the only cleanup verb here that can make a bad
// day worse. Two rules keep it honest.
//
// First, there are **two namespaces**, and only one of them is churn. Periodic
// backups (`agentique-<stamp>.db`) are written on a timer and pruned on a
// tiered schedule by agentkit's own retention. Pre-migration snapshots
// (`agentique-pre-<stamp>.db`) are taken deliberately, before something risky,
// and agentkit's periodic pruning never touches them. Neither does this: a
// snapshot somebody took because they were about to migrate a database is not
// disk to be reclaimed, and it is a fifth of the bytes here at most.
//
// Second, a trim can never empty the directory. `keep` is clamped up to
// minBackupsKept, because one surviving backup is one corrupt file away from
// none, and the request comes from a client that could ask for zero.
//
// Nothing here fights retention. Retention decides what is kept *over time*;
// trim is the operator saying "I want that week of history back as disk, now",
// and it can only ever keep fewer files than retention would.

const (
	backupSuffix = ".db"

	// backupPrefix must match the prefix serve.go hands sqliteops. A mismatch
	// makes trim a no-op rather than a hazard — it would simply match nothing.
	backupPrefix = "agentique-"

	// snapshotInfix marks a pre-migration safety snapshot. sqliteops writes
	// these as "<prefix>pre-<stamp>.db" and exempts them from periodic pruning.
	snapshotInfix = "pre-"

	// minBackupsKept is the floor a trim request is clamped up to. Keeping one
	// is not keeping a backup; it is keeping a single point of failure.
	minBackupsKept = 2
)

// BackupDir is where the timed database backups live, beside the database.
func BackupDir() string { return filepath.Join(paths.DataDir(), "backups") }

// BackupFile is one file in the backup directory.
type BackupFile struct {
	Name     string
	Path     string
	Bytes    int64
	Stamp    string // the "YYYYMMDD-HHMMSS" from the filename; sorts lexically
	Snapshot bool   // a pre-migration safety snapshot, never a trim candidate
}

// listBackups reads the backup directory, newest first. A missing directory is
// not an error — it means backups are disabled or none have been written yet.
func listBackups(dir string) ([]BackupFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read backup dir: %w", err)
	}
	out := make([]BackupFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		f, ok := classifyBackup(e.Name())
		if !ok {
			continue
		}
		f.Path = filepath.Join(dir, f.Name)
		if info, err := e.Info(); err == nil {
			f.Bytes = info.Size()
		}
		out = append(out, f)
	}
	// The stamp is a zero-padded UTC timestamp, so lexical order is chronological
	// and needs no parsing. Newest first.
	sort.Slice(out, func(i, j int) bool { return out[i].Stamp > out[j].Stamp })
	return out, nil
}

// classifyBackup names a file in the backup directory, or reports that it is
// none of ours. Anything unrecognised is left strictly alone — a trim must
// never remove a file it cannot name.
func classifyBackup(name string) (BackupFile, bool) {
	if !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, backupSuffix) {
		return BackupFile{}, false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(name, backupPrefix), backupSuffix)
	snapshot := strings.HasPrefix(rest, snapshotInfix)
	stamp := strings.TrimPrefix(rest, snapshotInfix)
	if stamp == "" {
		return BackupFile{}, false
	}
	return BackupFile{Name: name, Stamp: stamp, Snapshot: snapshot}, true
}

// summarizeBackups reports the two namespaces separately, because the row's
// detail line and the trim confirmation both have to say which of them a trim
// would touch. A nil result means there is nothing to report.
func summarizeBackups(dir string) *BackupSummary {
	files, err := listBackups(dir)
	if err != nil || len(files) == 0 {
		return nil
	}
	out := &BackupSummary{}
	for _, f := range files {
		if f.Snapshot {
			out.SnapshotCount++
			out.SnapshotBytes += f.Bytes
			continue
		}
		out.PeriodicCount++
		out.PeriodicBytes += f.Bytes
		// Files are newest-first, so the last periodic seen is the oldest.
		out.OldestPeriodic = f.Stamp
	}
	out.Trimmable = len(planBackupTrim(files, DefaultBackupsKept))
	return out
}

// DefaultBackupsKept is what the page's Trim button asks for. Enough that a
// single bad file is not the whole safety net, few enough to be worth clicking.
const DefaultBackupsKept = 3

// planBackupTrim picks the periodic backups to remove, keeping the `keep`
// newest. Pure and deterministic: given the same list it returns the same
// files, and it does no IO.
//
// Snapshots are filtered out before counting, so `keep` is a promise about
// periodic backups only and is not silently consumed by a namespace the trim
// cannot touch.
func planBackupTrim(files []BackupFile, keep int) []BackupFile {
	if keep < minBackupsKept {
		keep = minBackupsKept
	}
	periodic := make([]BackupFile, 0, len(files))
	for _, f := range files {
		if !f.Snapshot {
			periodic = append(periodic, f)
		}
	}
	sort.Slice(periodic, func(i, j int) bool { return periodic[i].Stamp > periodic[j].Stamp })
	if len(periodic) <= keep {
		return nil
	}
	return periodic[keep:]
}
