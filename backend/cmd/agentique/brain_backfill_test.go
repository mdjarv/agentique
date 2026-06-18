package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

// writeBundle writes a brainBundle JSON fixture and returns its path.
func writeBundle(t *testing.T, dir string, mems []bundleMemory) string {
	t.Helper()
	b := brainBundle{Version: brainBundleVersion, Projects: map[string]bundleProject{}, Memories: mems}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	path := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return path
}

// seedTarget builds a target brain dir holding the given records.
func seedTarget(t *testing.T, recs ...memory.Record) (*filestore.FileStore, string) {
	t.Helper()
	dir := t.TempDir()
	fs := filestore.New(dir)
	for _, r := range recs {
		if err := fs.Put(context.Background(), r); err != nil {
			t.Fatalf("seed %s: %v", r.ID, err)
		}
	}
	return fs, dir
}

func TestComputeSubsumedBackfillFromBundle(t *testing.T) {
	ctx := context.Background()

	// Source bundle: the project originals (id-bearing), since deleted from the brain.
	bundleDir := t.TempDir()
	source := writeBundle(t, bundleDir, []bundleMemory{
		{ID: "src-a", Scope: "project:one", Text: "use just check", Category: "fact", Source: "human"},
		{ID: "src-b", Scope: "project:two", Text: "run go test -race", Category: "fact", Source: "agent"},
		// An id-less entry (old bundle shape) must be ignored, not panic.
		{Scope: "project:three", Text: "no id here", Category: "fact", Source: "human"},
	})

	// Target: a promoted global fact whose DerivedFrom points at the originals, plus
	// a fact already carrying provenance (must be left alone) and a project fact that
	// is out of scope for this global-only pass.
	promoted := memory.New(memory.ScopeGlobal, "Run quality gates before committing", memory.CategoryPreference, memory.SourceConsolidated)
	promoted.ID = "g-promoted"
	promoted.DerivedFrom = []string{"src-a", "src-b", "src-gone"}

	filled := memory.New(memory.ScopeGlobal, "Already has provenance", memory.CategoryFact, memory.SourceConsolidated)
	filled.ID = "g-filled"
	filled.DerivedFrom = []string{"src-a"}
	filled.Subsumed = []memory.SubsumedSource{{Scope: "project:one", Text: "use just check"}}

	projectFact := memory.New("project:one", "scoped fact", memory.CategoryFact, memory.SourceConsolidated)
	projectFact.ID = "p-fact"
	projectFact.DerivedFrom = []string{"src-a"}

	target, _ := seedTarget(t, promoted, filled, projectFact)

	stats, toWrite, err := computeSubsumedBackfill(ctx, target, source)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	if stats.SourceRecords != 2 {
		t.Errorf("SourceRecords = %d, want 2 (id-less entry ignored)", stats.SourceRecords)
	}
	// Only the unfilled global promotion is eligible: g-filled already has Subsumed,
	// p-fact is project-scoped (the pass is global-only).
	if stats.Eligible != 1 {
		t.Errorf("Eligible = %d, want 1", stats.Eligible)
	}
	if stats.PartialMatched != 1 || stats.FullyMatched != 0 {
		t.Errorf("match split = full %d / partial %d, want 0/1", stats.FullyMatched, stats.PartialMatched)
	}
	if stats.MatchedIDs != 2 || stats.DanglingIDs != 1 {
		t.Errorf("ids = %d matched / %d dangling, want 2/1", stats.MatchedIDs, stats.DanglingIDs)
	}
	if len(toWrite) != 1 {
		t.Fatalf("toWrite = %d, want 1", len(toWrite))
	}
	wantSub := []memory.SubsumedSource{
		{Scope: "project:one", Text: "use just check"},
		{Scope: "project:two", Text: "run go test -race"},
	}
	if got := toWrite[0].Record.Subsumed; !subsumedEqual(got, wantSub) {
		t.Errorf("Subsumed = %+v, want %+v", got, wantSub)
	}

	// Apply, then verify the record on disk carries the provenance.
	if n, err := writeSubsumedBackfill(ctx, target, toWrite); err != nil || n != 1 {
		t.Fatalf("write = %d, %v; want 1, nil", n, err)
	}
	got, err := target.Get(ctx, "g-promoted")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if !subsumedEqual(got.Subsumed, wantSub) {
		t.Errorf("persisted Subsumed = %+v, want %+v", got.Subsumed, wantSub)
	}
	// DerivedFrom is left intact — Subsumed is parallel provenance, not a replacement.
	if len(got.DerivedFrom) != 3 {
		t.Errorf("DerivedFrom mutated: %v", got.DerivedFrom)
	}

	// Idempotency: a second pass over the now-filled brain is a no-op.
	_, toWrite2, err := computeSubsumedBackfill(ctx, target, source)
	if err != nil {
		t.Fatalf("compute (2nd): %v", err)
	}
	if len(toWrite2) != 0 {
		t.Errorf("second pass not idempotent: %d facts to write", len(toWrite2))
	}
}

func TestComputeSubsumedBackfillFromMarkdownDir(t *testing.T) {
	ctx := context.Background()

	// Source is a brain markdown directory (the id-keyed source of truth, e.g. a
	// backup) rather than a JSON bundle.
	_, srcDir := seedTarget(t,
		mustRecord("src-a", "project:one", "first source"),
		mustRecord("src-b", "project:two", "second source"),
	)

	promoted := memory.New(memory.ScopeGlobal, "merged", memory.CategoryFact, memory.SourceConsolidated)
	promoted.ID = "g-1"
	promoted.DerivedFrom = []string{"src-a", "src-b"}
	target, _ := seedTarget(t, promoted)

	stats, toWrite, err := computeSubsumedBackfill(ctx, target, srcDir)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if stats.SourceRecords != 2 {
		t.Errorf("SourceRecords = %d, want 2", stats.SourceRecords)
	}
	if stats.FullyMatched != 1 {
		t.Errorf("FullyMatched = %d, want 1", stats.FullyMatched)
	}
	if len(toWrite) != 1 || len(toWrite[0].Record.Subsumed) != 2 {
		t.Fatalf("unexpected toWrite: %+v", toWrite)
	}
}

func TestComputeSubsumedBackfillIdlessBundle(t *testing.T) {
	ctx := context.Background()
	// A pre-id-field export: every entry lacks an id, so nothing is indexable.
	source := writeBundle(t, t.TempDir(), []bundleMemory{
		{Scope: "project:one", Text: "old shape", Category: "fact", Source: "human"},
	})
	promoted := memory.New(memory.ScopeGlobal, "merged", memory.CategoryFact, memory.SourceConsolidated)
	promoted.ID = "g-1"
	promoted.DerivedFrom = []string{"whatever"}
	target, _ := seedTarget(t, promoted)

	stats, toWrite, err := computeSubsumedBackfill(ctx, target, source)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if stats.SourceRecords != 0 {
		t.Errorf("SourceRecords = %d, want 0 (RunE surfaces this as a guidance error)", stats.SourceRecords)
	}
	if len(toWrite) != 0 {
		t.Errorf("toWrite = %d, want 0", len(toWrite))
	}
}

func mustRecord(id string, scope memory.Scope, text string) memory.Record {
	r := memory.New(scope, text, memory.CategoryFact, memory.SourceHuman)
	r.ID = id
	return r
}

func subsumedEqual(a, b []memory.SubsumedSource) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
