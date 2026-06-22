package brain

import (
	"context"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// countingEmbedder records how many texts it was asked to embed, so a test can prove the
// cache skips work. Returns a trivial fixed-dim vector per text.
type countingEmbedder struct {
	calls int // number of Embed invocations
	texts int // cumulative number of texts embedded
	dim   int
}

func (e *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	e.texts += len(texts)
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func TestEmbedRecordsCachesByTextHash(t *testing.T) {
	ctx := context.Background()
	emb := &countingEmbedder{dim: 4}
	s := &Service{embedder: emb, embedCache: make(map[string][]float32)}

	recs := []memory.Record{
		{ID: "a", Text: "race detector"},
		{ID: "b", Text: "concurrent safety"},
		{ID: "c", Text: "race detector"}, // identical text to a → shares one embed
	}

	// First pass: 3 records but only 2 DISTINCT texts → 2 embeds; all ids resolved.
	out, err := s.embedRecords(ctx, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("want 3 vectors, got %d", len(out))
	}
	if emb.texts != 2 {
		t.Fatalf("first pass should embed 2 distinct texts, got %d", emb.texts)
	}

	// Second pass over the SAME records: everything is cached → no new embeds.
	if _, err := s.embedRecords(ctx, recs); err != nil {
		t.Fatal(err)
	}
	if emb.texts != 2 {
		t.Fatalf("cached second pass must not re-embed, total texts=%d want 2", emb.texts)
	}

	// Edit one record's text: only the changed text is re-embedded (new hash → miss).
	recs[1].Text = "concurrent safety verified under load"
	if _, err := s.embedRecords(ctx, recs); err != nil {
		t.Fatal(err)
	}
	if emb.texts != 3 {
		t.Fatalf("an edited text should add exactly one embed, total texts=%d want 3", emb.texts)
	}
}
