package filestore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RewriteNormalized rewrites every record file whose on-disk bytes differ from its
// normalized canonical form (NormalizeConfidence + NormalizeLabels), persisting derived
// labels/confidence that were missing, AND — for the M5 migration grace — stamping
// LastUsedAt=now where it is currently zero (so the disuse clock starts at backfill, not the
// ancient UpdatedAt). Idempotent by byte-compare: a canonical, already-stamped file is
// skipped (a second pass finds no zero LastUsedAt and no missing labels → rewrites nothing).
// Never deletes, never mutates Text. Returns (scanned, rewritten).
//
// The LastUsedAt-where-zero stamp lives ONLY in this one-time engine, never in
// NormalizeLabels/toRecord — load-time stamping would corrupt every read. Walks the same
// non-recursive layout as List, so brain/.snapshots/ (no direct *.md) is never touched.
func (f *FileStore) RewriteNormalized(ctx context.Context, now time.Time, dryRun bool) (scanned, rewritten int, err error) {
	_ = ctx // reserved for cancellation; the FS pass is fast and synchronous
	f.mu.Lock()
	defer f.mu.Unlock()

	entries, err := os.ReadDir(f.root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("filestore: backfill read root: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(f.root, e.Name())
		files, derr := os.ReadDir(dir)
		if derr != nil {
			return scanned, rewritten, fmt.Errorf("filestore: backfill read %s: %w", dir, derr)
		}
		for _, fe := range files {
			if fe.IsDir() || !strings.HasSuffix(fe.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, fe.Name())
			orig, rerr := os.ReadFile(path)
			if rerr != nil {
				return scanned, rewritten, fmt.Errorf("filestore: backfill read %s: %w", path, rerr)
			}
			scanned++
			rec, decErr := decode(orig) // decode normalizes labels + confidence
			if decErr != nil {
				return scanned, rewritten, fmt.Errorf("filestore: backfill decode %s: %w", path, decErr)
			}
			if rec.LastUsedAt.IsZero() {
				rec.LastUsedAt = now // start the disuse clock at the migration boundary
			}
			next, encErr := encode(rec)
			if encErr != nil {
				return scanned, rewritten, fmt.Errorf("filestore: backfill encode %s: %w", path, encErr)
			}
			if bytes.Equal(orig, next) {
				continue
			}
			rewritten++
			if dryRun {
				continue
			}
			if werr := atomicWrite(path, next); werr != nil {
				return scanned, rewritten, fmt.Errorf("filestore: backfill write %s: %w", path, werr)
			}
		}
	}
	return scanned, rewritten, nil
}
