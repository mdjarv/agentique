package chroma_test

import (
	"context"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/chroma"
	"github.com/mdjarv/agentique/backend/internal/memory/embedhttp"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
	"github.com/mdjarv/agentique/backend/internal/memory/vectortest"
)

// IndexStale indexes what the collection is missing or holds under an old text, skips what it
// already holds and every capture, and deletes nothing — a vector the base store does not name
// may belong to another brain sharing the collection.
func TestIndexStaleCatchesUpWithoutRebuildingOrDeleting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fc, fe := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	base := filestore.New(t.TempDir())
	emb := embedhttp.New(fe.URL, "fake")
	st, err := chroma.NewStore(ctx, base, chroma.NewClient(fc.URL), emb, "stale")
	if err != nil {
		t.Fatal(err)
	}

	// Indexed on the way in, through the decorator.
	kept := memory.New("proj", "indexed and unchanged", memory.CategoryFact, memory.SourceAgent)
	edited := memory.New("proj", "indexed under its first text", memory.CategoryFact, memory.SourceAgent)
	for _, r := range []memory.Record{kept, edited} {
		if err := st.Put(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	// A vector nobody here wrote: another brain's, in a shared collection.
	foreign := memory.New("proj", "a fact from another brain", memory.CategoryFact, memory.SourceAgent)
	other, err := chroma.NewStore(ctx, filestore.New(t.TempDir()), chroma.NewClient(fc.URL), emb, "stale")
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Put(ctx, foreign); err != nil {
		t.Fatal(err)
	}

	// Written behind the index's back, the way a detached brain writes.
	missed := memory.New("proj", "written while the index was away", memory.CategoryFact, memory.SourceAgent)
	edited.Text = "indexed under its second text"
	capture := memory.New("proj", "a raw capture", memory.CategoryFact, memory.SourceCapture)
	for _, r := range []memory.Record{missed, edited, capture} {
		if err := base.Put(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	before := fc.Upserted()
	n, err := st.IndexStale(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || fc.Upserted()-before != 2 {
		t.Fatalf("indexed %d (upserted %d), want the missed and the edited fact only", n, fc.Upserted()-before)
	}
	docs := fc.Docs("stale")
	for _, r := range []memory.Record{kept, edited, missed} {
		if docs[r.ID] != r.Text {
			t.Errorf("%s indexed as %q, want %q", r.ID, docs[r.ID], r.Text)
		}
	}
	if _, ok := docs[capture.ID]; ok {
		t.Error("a capture was indexed")
	}
	if _, ok := docs[foreign.ID]; !ok {
		t.Error("a vector the base store does not name was deleted")
	}

	// Caught up: a second pass has nothing to do.
	if n, err := st.IndexStale(ctx); err != nil || n != 0 {
		t.Fatalf("second pass indexed %d (%v), want 0", n, err)
	}
}
