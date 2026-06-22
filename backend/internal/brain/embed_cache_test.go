package brain

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/chroma"
)

// fakeVectorStore is an in-memory stand-in for the Chroma-backed vector store: it serves the
// vectors a previous process "wrote" so a restart can warm from them. It counts LoadVectors
// calls (to prove warm runs once) and can return a transient error (to prove retry).
type fakeVectorStore struct {
	mu    sync.Mutex
	recs  []chroma.VectorRecord
	loads int
	err   error
}

func (f *fakeVectorStore) LoadVectors(_ context.Context) ([]chroma.VectorRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]chroma.VectorRecord, len(f.recs))
	copy(out, f.recs)
	return out, nil
}

func (f *fakeVectorStore) clearErr() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = nil
}

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

// TestWarmEmbedCacheZeroReembedAfterRestart is the cold-start proof: a fresh Service (a process
// restart) over the SAME vector store warms its cache from the vectors Chroma already holds and
// re-embeds nothing for an unchanged corpus. This is the gap the in-process-only cache left open.
func TestWarmEmbedCacheZeroReembedAfterRestart(t *testing.T) {
	ctx := context.Background()
	corpus := []memory.Record{
		{ID: "a", Text: "race detector catches data races"},
		{ID: "b", Text: "modernc.org/sqlite is pure Go"},
		{ID: "c", Text: "race detector catches data races"}, // duplicate text of a
	}
	// The vector store already holds the two DISTINCT vectors, written before the restart.
	chromaVecs := &fakeVectorStore{recs: []chroma.VectorRecord{
		{ID: "a", Document: "race detector catches data races", Embedding: []float32{1, 0, 0, 0}},
		{ID: "b", Document: "modernc.org/sqlite is pure Go", Embedding: []float32{0, 1, 0, 0}},
	}}

	// Restarted process: cold in-process cache + a warm source over the same store.
	emb := &countingEmbedder{dim: 4}
	s := &Service{embedder: emb, embedCache: make(map[string][]float32), warmSrc: chromaVecs}

	out, err := s.embedRecords(ctx, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("want 3 vectors resolved, got %d", len(out))
	}
	if emb.texts != 0 {
		t.Fatalf("a restart over an unchanged corpus must re-embed nothing, embedded %d texts", emb.texts)
	}
	if chromaVecs.loads != 1 {
		t.Fatalf("warm should run exactly one bulk load, got %d", chromaVecs.loads)
	}
	if got := out["a"]; len(got) != 4 || got[0] != 1 {
		t.Fatalf("warmed vector not served for id a: %v", got)
	}

	// Second pass: warmed flag + cache both hold → still zero embeds, no second load.
	if _, err := s.embedRecords(ctx, corpus); err != nil {
		t.Fatal(err)
	}
	if emb.texts != 0 {
		t.Fatalf("second pass re-embedded; texts=%d", emb.texts)
	}
	if chromaVecs.loads != 1 {
		t.Fatalf("warm must run once per process, loads=%d", chromaVecs.loads)
	}
}

// TestWarmEmbedCacheEmbedsOnlyNewFacts proves warming is additive: facts already in the vector
// store resolve from the warm; a fact added since the last index is the only one embedded.
func TestWarmEmbedCacheEmbedsOnlyNewFacts(t *testing.T) {
	ctx := context.Background()
	chromaVecs := &fakeVectorStore{recs: []chroma.VectorRecord{
		{ID: "a", Document: "warmed fact", Embedding: []float32{1, 0}},
	}}
	emb := &countingEmbedder{dim: 2}
	s := &Service{embedder: emb, embedCache: make(map[string][]float32), warmSrc: chromaVecs}

	recs := []memory.Record{
		{ID: "a", Text: "warmed fact"},    // resolved from the warm
		{ID: "b", Text: "brand new fact"}, // not indexed yet → must be embedded
	}
	out, err := s.embedRecords(ctx, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 vectors, got %d", len(out))
	}
	if emb.texts != 1 {
		t.Fatalf("only the new fact should be embedded, embedded %d texts", emb.texts)
	}
	if got := out["a"]; len(got) != 2 || got[0] != 1 {
		t.Fatalf("warmed vector not served for id a: %v", got)
	}
}

// TestWarmEmbedCacheRetriesAfterFailure proves a transient vector-store error doesn't
// permanently disable warming: the pass falls back to embedding, warmed stays false, and the
// next pass re-attempts the warm and succeeds.
func TestWarmEmbedCacheRetriesAfterFailure(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVectorStore{
		err:  errors.New("chroma unreachable"),
		recs: []chroma.VectorRecord{{ID: "a", Document: "warmable", Embedding: []float32{1}}},
	}
	emb := &countingEmbedder{dim: 1}
	s := &Service{embedder: emb, embedCache: make(map[string][]float32), warmSrc: fake}
	recs := []memory.Record{{ID: "a", Text: "warmable"}}

	// First pass: warm fails → fall back to embedding the fact.
	if _, err := s.embedRecords(ctx, recs); err != nil {
		t.Fatal(err)
	}
	if emb.texts != 1 {
		t.Fatalf("warm failure should fall back to embedding, texts=%d", emb.texts)
	}
	if s.warmed {
		t.Fatal("warm must not be marked done after a failure")
	}

	// Drop the in-process cache so the next pass can only avoid an embed by re-warming.
	s.embedCache = make(map[string][]float32)
	fake.clearErr()

	if _, err := s.embedRecords(ctx, recs); err != nil {
		t.Fatal(err)
	}
	if !s.warmed {
		t.Fatal("warm should succeed and latch on retry")
	}
	if emb.texts != 1 {
		t.Fatalf("retry warm should serve from the vector store, not re-embed; texts=%d", emb.texts)
	}
	if fake.loads != 2 {
		t.Fatalf("want two load attempts (fail + retry), got %d", fake.loads)
	}
}

// TestPruneEmbedCacheDropsStaleTexts proves the cache is bounded by the live corpus: entries for
// texts no longer present (edited/deleted facts) are dropped, live entries are kept.
func TestPruneEmbedCacheDropsStaleTexts(t *testing.T) {
	emb := &countingEmbedder{dim: 1}
	s := &Service{embedder: emb, embedCache: make(map[string][]float32)}
	for _, txt := range []string{"alpha fact", "beta fact", "stale edited fact"} {
		s.embedCache[embedKey(txt)] = []float32{1}
	}

	// The live corpus no longer contains "stale edited fact".
	live := []memory.Record{
		{ID: "1", Text: "alpha fact"},
		{ID: "2", Text: "beta fact"},
	}
	s.pruneEmbedCache(live)

	if _, ok := s.embedCache[embedKey("stale edited fact")]; ok {
		t.Fatal("stale entry was not pruned")
	}
	if _, ok := s.embedCache[embedKey("alpha fact")]; !ok {
		t.Fatal("live entry alpha was wrongly pruned")
	}
	if len(s.embedCache) != 2 {
		t.Fatalf("want 2 live entries after prune, got %d", len(s.embedCache))
	}
}

// TestPruneEmbedCacheNoopWithoutEmbedder guards the keyword-mode path: with no embedder the
// cache is never populated, so pruning must do nothing (and never panics on a nil live set).
func TestPruneEmbedCacheNoopWithoutEmbedder(t *testing.T) {
	s := &Service{embedCache: map[string][]float32{embedKey("x"): {1}}}
	s.pruneEmbedCache(nil)
	if len(s.embedCache) != 1 {
		t.Fatalf("prune must be a no-op in keyword mode, cache size=%d", len(s.embedCache))
	}
}
