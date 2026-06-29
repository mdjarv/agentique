package filestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func TestRoundTripLabels(t *testing.T) {
	ctx := context.Background()
	fs := New(t.TempDir())
	r := memory.New(memory.ScopeGlobal, "labeled fact", memory.CategoryFact, memory.SourceConsolidated)
	r.Evidence = memory.EvidenceCorroborated
	r.Volatility = memory.VolatilityEphemeral
	r.Lifecycle = memory.LifecycleSuperseded
	r.Relations = []memory.TypedRelation{
		{Type: memory.RelationSupersedes, Target: "a"},
		{Type: memory.RelationContradicts, Target: "b"},
	}
	r.Keywords = []string{"k1", "k2"}
	r.LastCurated = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	r.CuratorNote = "note"
	if err := fs.Put(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := fs.Get(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Evidence != r.Evidence || got.Volatility != r.Volatility || got.Lifecycle != r.Lifecycle {
		t.Fatalf("labels not round-tripped: %+v", got)
	}
	if len(got.Relations) != 2 ||
		got.Relations[0].Type != memory.RelationSupersedes || got.Relations[0].Target != "a" ||
		got.Relations[1].Type != memory.RelationContradicts || got.Relations[1].Target != "b" {
		t.Fatalf("relations not round-tripped in order: %+v", got.Relations)
	}
	if len(got.Keywords) != 2 || got.Keywords[0] != "k1" || got.Keywords[1] != "k2" {
		t.Fatalf("keywords not round-tripped: %+v", got.Keywords)
	}
	if !got.LastCurated.Equal(r.LastCurated) {
		t.Fatalf("LastCurated=%v, want %v", got.LastCurated, r.LastCurated)
	}
	if got.CuratorNote != "note" {
		t.Fatalf("CuratorNote=%q", got.CuratorNote)
	}
}

func TestDecodeBackfillsLabels(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A legacy (label-less) file written via raw bytes.
	legacy := "---\nid: legacy1\nscope: global\ncategory: identity\nsource: human\nuses: 0\n" +
		"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n\nUser's name is X.\n"
	if err := os.WriteFile(filepath.Join(dir, "global", "legacy1.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := New(dir).List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	r := recs[0]
	if r.Evidence != memory.EvidenceUserStated { // human
		t.Errorf("Evidence=%s, want user_stated", r.Evidence)
	}
	if r.Volatility != memory.VolatilityEvergreen { // identity
		t.Errorf("Volatility=%s, want evergreen", r.Volatility)
	}
	if r.Lifecycle != memory.LifecycleActive {
		t.Errorf("Lifecycle=%s, want active", r.Lifecycle)
	}
}
