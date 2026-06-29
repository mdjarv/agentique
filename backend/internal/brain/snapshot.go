package brain

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Snapshot / rollback (RFC-LD reversibility, brain-evolution Band 1 M1). The markdown
// brain dir is the source of truth, so a churn is made reversible by a one-shot
// filesystem copy taken before it runs. Snapshots live in a sibling .snapshots/<ts>/
// dir that is invisible to recall/consolidation: filestore.List is non-recursive and
// reads only direct *.md of each top-level scope dir, so .snapshots (which holds no
// direct *.md) yields zero records and never enters ListScopes/Recall. This is the
// SINGLE snapshot mechanism for the whole band — the label backfill (M6) and the CLI
// reuse brain.Snapshot rather than inventing their own.
const (
	snapshotsDir = ".snapshots"
	// snapshotTSFormat is UTC, filesystem-safe, and lexically == chronological so a
	// sort by name newest-first is a sort by time newest-first.
	snapshotTSFormat      = "20060102T150405Z"
	defaultSnapshotRetain = 7
)

// Snapshot writes a pre-churn snapshot of this service's brain directory, retaining the
// configured number of snapshots. It is the single reversibility hook the M5 archive
// churn (automation.runOnce) and the M6 label backfill both rely on.
func (s *Service) Snapshot() (SnapshotInfo, error) { return Snapshot(s.dir, s.snapshotRetain) }

// ListSnapshots returns this service's snapshots newest-first (wraps the package-level
// ListSnapshots over the service's brain dir). A missing .snapshots dir is not an error.
func (s *Service) ListSnapshots() ([]SnapshotInfo, error) { return ListSnapshots(s.dir) }

// RestoreSnapshot rolls the ENTIRE brain back to snapshot id: it takes a fresh pre-restore
// safety snapshot (so the restore is itself reversible), makes the markdown tree match id,
// then INVALIDATES the read-through cache so the running server reflects the restored
// corpus immediately. Without that invalidation the cache would keep serving the
// pre-restore corpus until the next write — the M1 "restore is offline-only" caveat, lifted
// here for the live UI path (brain-ui-spec.md F4, the load-bearing fix). Held under s.mu so
// the file rewrite can't race a concurrent single-fact write (mutate funnels through s.mu).
//
// NOTE: in semantic mode the chroma vector index is NOT reindexed here — its vectors
// reconcile lazily on the next write touching each fact (and fully on a Reindex / restart
// warm). The memory LIST the UI shows is correct immediately; only semantic recall ranking
// may be briefly stale. A full post-restore Reindex is a deliberate follow-up.
func (s *Service) RestoreSnapshot(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := Restore(s.dir, id, s.snapshotRetain); err != nil {
		return err
	}
	s.cache.Invalidate()
	return nil
}

// SnapshotInfo describes a single snapshot directory.
type SnapshotInfo struct {
	ID        string    // the <ts> directory name (also the restore handle)
	Path      string    // absolute path to the snapshot directory
	CreatedAt time.Time // parsed from ID
	Files     int       // regular files copied/contained
	Bytes     int64     // total bytes of those files
}

// Snapshot copies the brain tree (every top-level entry EXCEPT .snapshots, INCLUDING
// the .fingerprints.json / .global-manifest.json dotfiles) into brain/.snapshots/<ts>/,
// then prunes to the newest `retain` snapshots. retain<=0 uses defaultSnapshotRetain
// (7). An empty brain yields Files==0 and no error. IDs have 1-second resolution; a
// same-second re-snapshot overwrites (O_TRUNC) — acceptable, tests avoid it via snapshotAt.
func Snapshot(brainDir string, retain int) (SnapshotInfo, error) {
	return snapshotAt(brainDir, retain, time.Now().UTC())
}

// snapshotAt is the time-injectable core: tests pass deterministic, distinct `now`s to
// build retention/round-trip fixtures without sleeping; production passes time.Now().UTC().
func snapshotAt(brainDir string, retain int, now time.Time) (SnapshotInfo, error) {
	if retain <= 0 {
		retain = defaultSnapshotRetain
	}
	id := now.Format(snapshotTSFormat)
	dst := filepath.Join(brainDir, snapshotsDir, id)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return SnapshotInfo{}, fmt.Errorf("brain: snapshot mkdir %q: %w", dst, err)
	}
	// Skip the top-level .snapshots so a snapshot never copies prior snapshots (no
	// exponential blowup) and never copies dst into itself.
	files, bytes, err := copyTree(brainDir, dst, map[string]struct{}{snapshotsDir: {}})
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("brain: snapshot copy: %w", err)
	}
	if err := pruneSnapshots(brainDir, retain); err != nil {
		return SnapshotInfo{}, fmt.Errorf("brain: snapshot prune: %w", err)
	}
	return SnapshotInfo{ID: id, Path: dst, CreatedAt: now, Files: files, Bytes: bytes}, nil
}

// ListSnapshots returns the snapshots newest-first. A missing .snapshots dir is not an
// error (returns nil, nil). Non-snapshot directory names are ignored.
func ListSnapshots(brainDir string) ([]SnapshotInfo, error) {
	dir := filepath.Join(brainDir, snapshotsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("brain: list snapshots: %w", err)
	}
	var infos []SnapshotInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ts, perr := time.Parse(snapshotTSFormat, e.Name())
		if perr != nil {
			continue // not a snapshot directory
		}
		path := filepath.Join(dir, e.Name())
		files, bytes, err := countTree(path)
		if err != nil {
			return nil, fmt.Errorf("brain: stat snapshot %q: %w", e.Name(), err)
		}
		infos = append(infos, SnapshotInfo{ID: e.Name(), Path: path, CreatedAt: ts, Files: files, Bytes: bytes})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID > infos[j].ID }) // newest-first
	return infos, nil
}

// Restore makes the brain tree exactly match snapshot `id`. It FIRST writes a fresh
// pre-restore safety snapshot (so a restore is itself reversible), then removes every
// live top-level entry except .snapshots and copies the snapshot back over it. A
// missing id is os.ErrNotExist. Restore is offline-only for M1 (it rewrites files
// underneath a running server's read-through cache).
func Restore(brainDir, id string, retain int) error {
	snapPath := filepath.Join(brainDir, snapshotsDir, id)
	if _, err := os.Stat(snapPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("brain: snapshot %q not found: %w", id, os.ErrNotExist)
		}
		return fmt.Errorf("brain: stat snapshot %q: %w", id, err)
	}
	if _, err := Snapshot(brainDir, retain); err != nil {
		return fmt.Errorf("brain: pre-restore snapshot: %w", err)
	}
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return fmt.Errorf("brain: restore read live tree: %w", err)
	}
	for _, e := range entries {
		if e.Name() == snapshotsDir {
			continue
		}
		if err := os.RemoveAll(filepath.Join(brainDir, e.Name())); err != nil {
			return fmt.Errorf("brain: restore clear %q: %w", e.Name(), err)
		}
	}
	if _, _, err := copyTree(snapPath, brainDir, map[string]struct{}{}); err != nil {
		return fmt.Errorf("brain: restore copy: %w", err)
	}
	return nil
}

// copyTree copies every entry under src into dst, skipping the configured top-level
// names (and their subtrees). Directories are made 0o755, files written O_TRUNC 0o644.
func copyTree(src, dst string, skipTop map[string]struct{}) (files int, bytes int64, err error) {
	walkErr := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return fmt.Errorf("brain: rel %q: %w", path, rerr)
		}
		if rel == "." {
			return nil
		}
		top := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if _, skip := skipTop[top]; skip {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if mErr := os.MkdirAll(target, 0o755); mErr != nil {
				return fmt.Errorf("brain: mkdir %q: %w", target, mErr)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // skip symlinks/sockets/etc — the brain holds only regular files
		}
		n, cErr := copyFile(path, target)
		if cErr != nil {
			return cErr
		}
		files++
		bytes += n
		return nil
	})
	return files, bytes, walkErr
}

// copyFile copies a single regular file, returning the number of bytes written.
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("brain: open %q: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, fmt.Errorf("brain: mkdir for %q: %w", dst, err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("brain: create %q: %w", dst, err)
	}
	n, cErr := io.Copy(out, in)
	if closeErr := out.Close(); closeErr != nil && cErr == nil {
		cErr = closeErr
	}
	if cErr != nil {
		return 0, fmt.Errorf("brain: copy to %q: %w", dst, cErr)
	}
	return n, nil
}

// countTree returns the regular-file count and total bytes under root.
func countTree(root string) (files int, bytes int64, err error) {
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, iErr := d.Info()
		if iErr != nil {
			return iErr
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes, walkErr
}

// pruneSnapshots keeps the newest `retain` snapshots and RemoveAll-prunes the rest.
func pruneSnapshots(brainDir string, retain int) error {
	if retain <= 0 {
		retain = defaultSnapshotRetain
	}
	infos, err := ListSnapshots(brainDir)
	if err != nil {
		return err
	}
	if len(infos) <= retain {
		return nil
	}
	for _, info := range infos[retain:] { // infos newest-first
		if err := os.RemoveAll(info.Path); err != nil {
			return fmt.Errorf("brain: prune snapshot %q: %w", info.ID, err)
		}
	}
	return nil
}
