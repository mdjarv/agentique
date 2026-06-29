package filestore

import (
	"context"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func TestFilestoreCorroborationsRoundTrip(t *testing.T) {
	ctx := context.Background()
	fs := New(t.TempDir())

	r := memory.New(memory.ScopeGlobal, "reinforced fact", memory.CategoryFact, memory.SourceConsolidated)
	r.Corroborations = 3
	if err := fs.Put(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := fs.Get(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Corroborations != 3 {
		t.Fatalf("Corroborations=%d, want 3 (round-trip)", got.Corroborations)
	}

	// Zero round-trips to zero, and omitempty suppresses the key on disk.
	r0 := memory.New(memory.ScopeGlobal, "fresh fact", memory.CategoryFact, memory.SourceConsolidated)
	if err := fs.Put(ctx, r0); err != nil {
		t.Fatal(err)
	}
	got0, err := fs.Get(ctx, r0.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got0.Corroborations != 0 {
		t.Fatalf("Corroborations=%d, want 0", got0.Corroborations)
	}
	b, err := encode(r0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "corroborations:") {
		t.Fatalf("zero Corroborations should be omitted from frontmatter:\n%s", b)
	}
}
