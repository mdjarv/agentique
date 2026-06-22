package memory

import (
	"testing"
	"time"
)

// D6: a well-established but long-unused fact is due for review; a fresh one and a
// weakly-held one are not.
func TestDueForReview(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-120 * 24 * time.Hour) // ~4 half-lives → retrieval well below the ceiling

	established := func(id string, last time.Time) Record {
		r := mk(id, scopeA, "an established fact "+id, CategoryFact, SourceConsolidated)
		r.Uses = 15
		r.DerivedFrom = []string{"a", "b", "c"}
		r.UpdatedAt = old
		r.LastUsedAt = last
		return r
	}

	cold := established("cold", old) // strong + unused → due
	warm := established("warm", now) // strong but recently used → not due
	weak := mk("weak", scopeA, "thinly held", CategoryFact, SourceConsolidated)
	weak.UpdatedAt = old
	weak.LastUsedAt = old // cold but low storage → not worth reviewing
	pinned := established("pin", old)
	pinned.Pinned = true // always injected → never due

	due := DueForReview([]Record{cold, warm, weak, pinned}, now, 0)
	if len(due) != 1 || due[0].ID != "cold" {
		ids := make([]string, len(due))
		for i, r := range due {
			ids[i] = r.ID
		}
		t.Fatalf("only the strong, cold, non-pinned fact is due, got %v", ids)
	}
}

// D5: a similar-but-not-duplicate pair is interference; near-identical (duplicate)
// and unrelated pairs are not.
func TestDetectInterference(t *testing.T) {
	recs := []Record{
		// a~b share {deploy, service, using, script/...}; Jaccard ≈ 0.43, in [0.3,0.6).
		mk("a", scopeA, "deploy the service using the deploy script now", CategoryFact, SourceConsolidated),
		mk("b", scopeA, "deploy the service using a different command", CategoryFact, SourceConsolidated),
		mk("c", scopeA, "the user lives in stockholm and likes coffee", CategoryFact, SourceConsolidated),
	}
	pairs := DetectInterference(recs, DefaultRelatedThreshold, DefaultDuplicateThreshold, 0)
	if len(pairs) != 1 {
		t.Fatalf("expected one interference pair (a~b), got %d: %+v", len(pairs), pairs)
	}
	if pairs[0].A != "a" || pairs[0].B != "b" {
		t.Fatalf("pair should be the two deploy facts in canonical id order, got %+v", pairs[0])
	}
	if pairs[0].Similarity < DefaultRelatedThreshold || pairs[0].Similarity >= DefaultDuplicateThreshold {
		t.Fatalf("similarity should be in the interference band, got %.2f", pairs[0].Similarity)
	}
}

// Exact duplicates are NOT interference (consolidation merges those).
func TestDetectInterferenceExcludesDuplicates(t *testing.T) {
	recs := []Record{
		mk("a", scopeA, "uses just as the task runner", CategoryFact, SourceConsolidated),
		mk("b", scopeA, "uses just as the task runner", CategoryFact, SourceConsolidated),
	}
	if pairs := DetectInterference(recs, DefaultRelatedThreshold, DefaultDuplicateThreshold, 0); len(pairs) != 0 {
		t.Fatalf("identical facts are duplicates, not interference, got %+v", pairs)
	}
}

// #2: with SimOptions the related-lower bound blends cosine, so a semantic near-match
// (lexically disjoint, high cosine) is surfaced as interference — the pair an agent
// confuses that lexical-only detection misses. Mirrors TestDetectCommunitiesUsesEmbeddings.
func TestDetectInterferenceUsesEmbeddings(t *testing.T) {
	recs := []Record{
		mk("a", scopeA, "always run with the race detector", CategoryFact, SourceConsolidated),
		mk("b", scopeA, "verify concurrent safety under load", CategoryFact, SourceConsolidated), // disjoint words
	}
	vecs := map[string][]float32{"a": {1, 0}, "b": {1, 0}} // same direction → cosine 1, jaccard 0

	// Lexical-only: disjoint words → below the related-lower bound → no interference.
	if pairs := DetectInterference(recs, DefaultRelatedThreshold, DefaultDuplicateThreshold, 0); len(pairs) != 0 {
		t.Fatalf("lexical-only: disjoint facts should not be interference, got %+v", pairs)
	}

	// Semantic: cosine clears the link threshold → the pair surfaces (and is not excluded
	// as a duplicate, since duplicate-exclusion stays lexical).
	pairs := DetectInterference(recs, DefaultRelatedThreshold, DefaultDuplicateThreshold, 0,
		WithEmbeddingLookup(func(id string) []float32 { return vecs[id] }),
		WithCosineThreshold(0.5))
	if len(pairs) != 1 || pairs[0].A != "a" || pairs[0].B != "b" {
		t.Fatalf("semantic: expected the a~b near-match as interference, got %+v", pairs)
	}
}

// A high-cosine pair that is also a LEXICAL duplicate stays excluded — duplicate-exclusion
// is lexical, so consolidation (not interference) still owns exact/near-exact dups even in
// semantic mode.
func TestDetectInterferenceSemanticStillExcludesLexicalDuplicates(t *testing.T) {
	recs := []Record{
		mk("a", scopeA, "uses just as the task runner", CategoryFact, SourceConsolidated),
		mk("b", scopeA, "uses just as the task runner", CategoryFact, SourceConsolidated),
	}
	vecs := map[string][]float32{"a": {1, 0}, "b": {1, 0}}
	pairs := DetectInterference(recs, DefaultRelatedThreshold, DefaultDuplicateThreshold, 0,
		WithEmbeddingLookup(func(id string) []float32 { return vecs[id] }),
		WithCosineThreshold(0.5))
	if len(pairs) != 0 {
		t.Fatalf("lexical duplicates must stay excluded even with high cosine, got %+v", pairs)
	}
}
