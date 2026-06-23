package brain

import (
	"context"
	"errors"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// errEmbedder fails every Embed call. Swapped in after a corpus is first computed, it proves a
// SemanticEdges path that DOESN'T touch it served from cache, and one that DOES recomputed.
type errEmbedder struct{}

func (errEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embedder must not be called on a cache hit")
}

func sameEdges(a, b []memory.Edge) bool {
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

// TestSemanticEdgesCachedByFingerprint proves the per-corpus memoization: a repeated graph load
// over an unchanged corpus skips the embed + kNN entirely (served from cache), while any change
// to a fact's text re-fingerprints and recomputes.
func TestSemanticEdgesCachedByFingerprint(t *testing.T) {
	ctx := context.Background()
	emb := &countingEmbedder{dim: 3}
	s := &Service{
		semantic:     true,
		embedder:     emb,
		embedCache:   make(map[string][]float32),
		semEdgeCache: make(map[string][]memory.Edge),
		graph:        GraphConfig{EdgeThreshold: 0.5, EdgeCap: 6},
	}
	recs := []memory.Record{
		{ID: "a", Text: "race detector"},
		{ID: "b", Text: "concurrent safety"},
		{ID: "c", Text: "goroutine leak"},
	}

	first, err := s.SemanticEdges(ctx, recs)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if emb.calls == 0 {
		t.Fatal("first pass should embed the corpus")
	}
	if len(first) == 0 {
		t.Fatal("expected edges over identical-direction vectors")
	}

	// Break the embedder. A fingerprint hit must short-circuit before embedRecords, so the same
	// records still resolve from cache without error and return the identical edge set.
	s.embedder = errEmbedder{}
	second, err := s.SemanticEdges(ctx, recs)
	if err != nil {
		t.Fatalf("a cache hit must not re-embed: %v", err)
	}
	if !sameEdges(first, second) {
		t.Fatalf("cached result differs: %v vs %v", first, second)
	}

	// Editing a fact's text changes the fingerprint → miss → recompute, which here hits the
	// broken embedder and surfaces its error (proving the cache was bypassed for new content).
	recs[1].Text = "concurrent safety verified under load"
	if _, err := s.SemanticEdges(ctx, recs); err == nil {
		t.Fatal("a changed corpus must recompute, not serve a stale cache entry")
	}
}

// TestSemanticEdgesFingerprintTracksKnobs proves the threshold/cap are part of the fingerprint:
// the same corpus under different knobs is a cache miss, not a stale hit.
func TestSemanticEdgesFingerprintTracksKnobs(t *testing.T) {
	recs := []memory.Record{
		{ID: "a", Text: "alpha"},
		{ID: "b", Text: "beta"},
	}
	base := semanticEdgeFingerprint(recs, 0.45, 6)
	if semanticEdgeFingerprint(recs, 0.46, 6) == base {
		t.Error("threshold change should change the fingerprint")
	}
	if semanticEdgeFingerprint(recs, 0.45, 5) == base {
		t.Error("per-node cap change should change the fingerprint")
	}
	// Order-independent: the same corpus in a different List order fingerprints identically.
	reordered := []memory.Record{recs[1], recs[0]}
	if semanticEdgeFingerprint(reordered, 0.45, 6) != base {
		t.Error("fingerprint must be independent of record order")
	}
	// A text edit changes it.
	edited := []memory.Record{{ID: "a", Text: "alpha prime"}, recs[1]}
	if semanticEdgeFingerprint(edited, 0.45, 6) == base {
		t.Error("a text edit should change the fingerprint")
	}
}
