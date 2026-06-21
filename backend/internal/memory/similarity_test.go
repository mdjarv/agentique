package memory

import (
	"math"
	"testing"
)

func TestCosine(t *testing.T) {
	cases := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0}, []float32{1, 0}, 1},
		{[]float32{1, 0}, []float32{0, 1}, 0},
		{[]float32{1, 1}, []float32{1, 1}, 1},
		{[]float32{2, 0}, []float32{3, 0}, 1}, // non-unit, same direction
		{[]float32{1, 0}, nil, 0},             // missing vector
		{[]float32{1, 0}, []float32{1}, 0},    // mismatched length
	}
	for _, c := range cases {
		if got := cosine(c.a, c.b); math.Abs(got-c.want) > 1e-6 {
			t.Errorf("cosine(%v,%v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSimilarityJaccardOnly(t *testing.T) {
	recs := []Record{
		{ID: "a", Text: "go test race detector"},
		{ID: "b", Text: "go test race detector"}, // identical → jaccard 1
		{ID: "c", Text: "frontend tailwind styling"},
	}
	s := newSimilarity(recs)
	if s.semantic() {
		t.Fatal("no embeddings → should not be semantic")
	}
	if got := s.Score(0, 1); math.Abs(got-1) > 1e-6 {
		t.Errorf("identical text Score = %v, want 1", got)
	}
	if got := s.Score(0, 2); got > 0.01 {
		t.Errorf("disjoint text Score = %v, want ~0", got)
	}
}

func TestSimilarityMaxCombinesLexicalAndCosine(t *testing.T) {
	// a and b are lexically disjoint but semantically identical (same vector); c shares
	// no words and an orthogonal vector.
	recs := []Record{
		{ID: "a", Text: "race detector"},
		{ID: "b", Text: "concurrent safety"},
		{ID: "c", Text: "unrelated topic"},
	}
	vecs := map[string][]float32{
		"a": {1, 0},
		"b": {1, 0}, // same direction as a → cosine 1, jaccard 0
		"c": {0, 1},
	}
	s := newSimilarity(recs, WithEmbeddingLookup(func(id string) []float32 { return vecs[id] }))
	if !s.semantic() {
		t.Fatal("embeddings supplied → should be semantic")
	}
	// Lexically disjoint but semantically identical → max() lifts to ~1.
	if got := s.Score(0, 1); math.Abs(got-1) > 1e-6 {
		t.Errorf("semantic-only pair Score = %v, want ~1 (cosine wins over jaccard 0)", got)
	}
	// Orthogonal vector + no shared words → ~0.
	if got := s.Score(0, 2); got > 0.01 {
		t.Errorf("unrelated pair Score = %v, want ~0", got)
	}
}

func TestSimilarityLinkedTwoThresholds(t *testing.T) {
	recs := []Record{
		{ID: "a", Text: "race detector"},
		{ID: "b", Text: "concurrent safety"},  // lexically disjoint from a
		{ID: "c", Text: "race detector tool"}, // lexically close to a, orthogonal vector
	}
	vecs := map[string][]float32{"a": {1, 0}, "b": {1, 0}, "c": {0, 1}}
	s := newSimilarity(recs,
		WithEmbeddingLookup(func(id string) []float32 { return vecs[id] }),
		WithCosineThreshold(0.9))

	if !s.Linked(0, 1, 0.15) {
		t.Error("a-b should link via cosine (jaccard 0, cosine 1)")
	}
	if !s.Linked(0, 2, 0.15) {
		t.Error("a-c should link via jaccard (shared words) despite orthogonal vectors")
	}
	if s.Linked(1, 2, 0.15) {
		t.Error("b-c should not link (no shared words, orthogonal vectors)")
	}

	// Without embeddings the cosine path is inert: a-b (lexically disjoint) must not link.
	if newSimilarity(recs).Linked(0, 1, 0.15) {
		t.Error("lexical-only: a-b must not link")
	}
}

func TestDetectCommunitiesUsesEmbeddings(t *testing.T) {
	// Two lexically-disjoint but semantically-identical facts should share a community
	// only when embeddings are supplied — proving cosine reaches the clustering.
	recs := []Record{
		{ID: "a", Text: "race detector required"},
		{ID: "b", Text: "concurrent safety verified"},
	}
	vecs := map[string][]float32{"a": {1, 0}, "b": {1, 0}}

	lexical := DetectCommunities(recs, DefaultCommunityThreshold)
	if lexical["a"] == lexical["b"] {
		t.Fatal("without embeddings, lexically-disjoint facts should be separate communities")
	}

	semantic := DetectCommunities(recs, DefaultCommunityThreshold,
		WithEmbeddingLookup(func(id string) []float32 { return vecs[id] }),
		WithCosineThreshold(0.5))
	if semantic["a"] != semantic["b"] {
		t.Fatal("with embeddings, semantically-identical facts should share a community")
	}
}

func TestSimilarityPerRecordEmbeddingFallback(t *testing.T) {
	// Only "a" has a vector; the a–b pair has no cosine signal and falls back to Jaccard.
	recs := []Record{
		{ID: "a", Text: "shared word here"},
		{ID: "b", Text: "shared word here"},
	}
	s := newSimilarity(recs, WithEmbeddingLookup(func(id string) []float32 {
		if id == "a" {
			return []float32{1, 0}
		}
		return nil
	}))
	// Jaccard is 1 (identical text); cosine is 0 (b has no vector); max → 1.
	if got := s.Score(0, 1); math.Abs(got-1) > 1e-6 {
		t.Errorf("fallback Score = %v, want 1 (jaccard, since b lacks a vector)", got)
	}
}
