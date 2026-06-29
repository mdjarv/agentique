package brain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

// seedBrain writes a couple of real records (via filestore) plus the two dotfiles into
// dir, and returns the live file set (rel path → bytes), excluding .snapshots.
func seedBrain(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	ctx := context.Background()
	fs := filestore.New(dir)
	if err := fs.Put(ctx, memory.New(memory.ScopeGlobal, "the brain is the source of truth", memory.CategoryFact, memory.SourceAgent)); err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(ctx, memory.New(memory.Scope("project-x"), "project x uses sqlc", memory.CategoryProject, memory.SourceConsolidated)); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		".fingerprints.json":    `{"global":"deadbeef"}`,
		".global-manifest.json": `{"project-x":"cafef00d"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return liveFiles(t, dir)
}

// liveFiles walks dir (excluding .snapshots) and returns rel path → bytes.
func liveFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == snapshotsDir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		b, rErr := os.ReadFile(path)
		if rErr != nil {
			return rErr
		}
		out[rel] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSnapshotCreatesCopy(t *testing.T) {
	dir := t.TempDir()
	live := seedBrain(t, dir)

	info, err := Snapshot(dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if info.Files != len(live) {
		t.Fatalf("Files=%d, want %d", info.Files, len(live))
	}
	var wantBytes int64
	for rel, content := range live {
		wantBytes += int64(len(content))
		got, err := os.ReadFile(filepath.Join(dir, snapshotsDir, info.ID, rel))
		if err != nil {
			t.Fatalf("snapshot missing %s: %v", rel, err)
		}
		if string(got) != string(content) {
			t.Fatalf("snapshot %s not byte-identical", rel)
		}
	}
	if info.Bytes != wantBytes {
		t.Fatalf("Bytes=%d, want %d", info.Bytes, wantBytes)
	}
}

func TestSnapshotExcludesSnapshotsDir(t *testing.T) {
	dir := t.TempDir()
	seedBrain(t, dir)
	first, err := snapshotAt(dir, 7, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second, err := snapshotAt(dir, 7, time.Date(2026, 6, 1, 0, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// The second snapshot must NOT contain a nested .snapshots (no exponential blowup),
	// and the two snapshots must have the same file count (the live tree, unchanged).
	if _, err := os.Stat(filepath.Join(second.Path, snapshotsDir)); !os.IsNotExist(err) {
		t.Fatalf("second snapshot contains a nested %s dir", snapshotsDir)
	}
	if first.Files != second.Files {
		t.Fatalf("snapshot file counts differ: %d vs %d", first.Files, second.Files)
	}
}

func TestListInvisibleToFilestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	seedBrain(t, dir)

	fs := filestore.New(dir)
	before, err := fs.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(dir, 7); err != nil {
		t.Fatal(err)
	}
	after, err := fs.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("filestore.List changed after snapshot: %d → %d (snapshot leaked into List)", len(before), len(after))
	}

	// ListScopes (brain) reads the same non-recursive List, so it must be unchanged too.
	svc, err := New(ctx, Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := svc.ListScopes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range scopes {
		if s == memory.Scope(snapshotsDir) {
			t.Fatalf("ListScopes surfaced the snapshots dir as a scope")
		}
	}
	if len(scopes) != 2 {
		t.Fatalf("ListScopes=%v, want 2 real scopes", scopes)
	}
}

func TestSnapshotRetention(t *testing.T) {
	dir := t.TempDir()
	seedBrain(t, dir)
	const retain = 3
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < retain+3; i++ {
		if _, err := snapshotAt(dir, retain, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListSnapshots(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != retain {
		t.Fatalf("retained %d snapshots, want %d", len(got), retain)
	}
	// Newest-first: the kept ones are the last `retain` timestamps.
	if got[0].ID != base.Add(time.Duration(retain+2)*time.Minute).Format(snapshotTSFormat) {
		t.Fatalf("newest retained = %s, expected the latest", got[0].ID)
	}

	// retain<=0 keeps the built-in default (7).
	for i := 0; i < 10; i++ {
		if _, err := snapshotAt(dir, 0, base.Add(time.Duration(100+i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got, err = ListSnapshots(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != defaultSnapshotRetain {
		t.Fatalf("retain<=0 kept %d, want %d", len(got), defaultSnapshotRetain)
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	seedBrain(t, dir)
	pre := liveFiles(t, dir)

	id := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Format(snapshotTSFormat)
	if _, err := snapshotAt(dir, 7, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	snapsBefore, _ := ListSnapshots(dir)

	// Mutate: add, delete, edit.
	fs := filestore.New(dir)
	if err := fs.Put(ctx, memory.New(memory.ScopeGlobal, "a brand new fact", memory.CategoryFact, memory.SourceAgent)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".fingerprints.json"), []byte(`{"global":"changed"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Restore(dir, id, 7); err != nil {
		t.Fatal(err)
	}

	post := liveFiles(t, dir)
	if len(post) != len(pre) {
		t.Fatalf("restored tree has %d files, want %d", len(post), len(pre))
	}
	for rel, content := range pre {
		if string(post[rel]) != string(content) {
			t.Fatalf("restored %s does not match pre-mutation content", rel)
		}
	}
	// One MORE snapshot than before (the pre-restore safety copy).
	snapsAfter, _ := ListSnapshots(dir)
	if len(snapsAfter) != len(snapsBefore)+1 {
		t.Fatalf("expected one extra (safety) snapshot: %d → %d", len(snapsBefore), len(snapsAfter))
	}
}

func TestRestoreUnknownIDError(t *testing.T) {
	dir := t.TempDir()
	before := seedBrain(t, dir)
	err := Restore(dir, "20990101T000000Z", 7)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
	after := liveFiles(t, dir)
	if len(after) != len(before) {
		t.Fatalf("live tree changed on failed restore: %d → %d", len(before), len(after))
	}
}

func TestSnapshotEmptyBrain(t *testing.T) {
	dir := t.TempDir()
	info, err := Snapshot(dir, 7)
	if err != nil {
		t.Fatalf("empty-brain snapshot errored: %v", err)
	}
	if info.Files != 0 {
		t.Fatalf("empty brain Files=%d, want 0", info.Files)
	}
}

func TestRunOnceSnapshotsBeforeChurn(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Add(ctx, memory.ScopeGlobal, "a durable fact to churn", memory.CategoryFact, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}

	auto := NewAutomation(svc, nil, nil, 0, "", 0, 0)
	auto.runOnce(ctx)

	snaps, err := ListSnapshots(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) < 1 {
		t.Fatalf("runOnce did not take a pre-churn snapshot")
	}
	if snaps[0].Files < 1 {
		t.Fatalf("pre-churn snapshot captured no files")
	}
}
