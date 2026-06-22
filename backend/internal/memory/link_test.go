package memory

import (
	"context"
	"testing"
)

func hasID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// #2: ApplyPlan must thread its SimOptions into the post-apply graph rebuild, so per-scope
// Related links become embedding-aware in semantic mode (not just AssignAreas). Two
// lexically-disjoint but semantically-identical facts are unlinked lexically and linked
// only when ApplyPlan carries an embedding lookup — proving cosine reaches RelinkScope.
func TestApplyPlanThreadsSemanticSimOptionsToRelink(t *testing.T) {
	ctx := context.Background()
	mkStore := func() *memStore {
		return newMemStore(
			mk("a", scopeA, "always run with the race detector", CategoryFact, SourceConsolidated),
			mk("b", scopeA, "verify concurrent safety under load", CategoryFact, SourceConsolidated),
		)
	}
	vecs := map[string][]float32{"a": {1, 0}, "b": {1, 0}} // cosine 1, jaccard 0

	// Lexical apply (no SimOptions): disjoint words → no link.
	lex := mkStore()
	lexPlan, err := PlanConsolidation(ctx, lex, nil, scopeA, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPlan(ctx, lex, scopeA, lexPlan, ConsolidateOptions{}); err != nil {
		t.Fatal(err)
	}
	if a, _ := lex.Get(ctx, "a"); hasID(a.Related, "b") {
		t.Fatalf("lexical ApplyPlan should not link disjoint facts, got Related=%v", a.Related)
	}

	// Semantic apply: SimOptions carry the cosine signal → RelinkScope links a<->b.
	sem := mkStore()
	semPlan, err := PlanConsolidation(ctx, sem, nil, scopeA, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPlan(ctx, sem, scopeA, semPlan, ConsolidateOptions{
		SimOptions: []SimOption{
			WithEmbeddingLookup(func(id string) []float32 { return vecs[id] }),
			WithCosineThreshold(0.5),
		},
	}); err != nil {
		t.Fatal(err)
	}
	a, _ := sem.Get(ctx, "a")
	b, _ := sem.Get(ctx, "b")
	if !hasID(a.Related, "b") || !hasID(b.Related, "a") {
		t.Fatalf("semantic ApplyPlan should link a<->b via cosine: a=%v b=%v", a.Related, b.Related)
	}
}

func TestRelinkScope(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		mk("a", scopeA, "user prefers dark mode in the editor", CategoryPreference, SourceConsolidated),
		mk("b", scopeA, "dark mode is the editor preference", CategoryPreference, SourceConsolidated),
		mk("c", scopeA, "project builds with just", CategoryProject, SourceConsolidated),
	)
	n, err := RelinkScope(ctx, store, scopeA)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected an edge between the two similar facts")
	}

	a, _ := store.Get(ctx, "a")
	b, _ := store.Get(ctx, "b")
	c, _ := store.Get(ctx, "c")
	if !hasID(a.Related, "b") || !hasID(b.Related, "a") {
		t.Fatalf("a<->b should be linked: a=%v b=%v", a.Related, b.Related)
	}
	if hasID(c.Related, "a") || hasID(c.Related, "b") {
		t.Fatalf("unrelated fact c should not be linked: %v", c.Related)
	}

	// Idempotent: nothing changed since last relink → no rewrites.
	if n2, _ := RelinkScope(ctx, store, scopeA); n2 != 0 {
		t.Fatalf("re-relink should be a no-op, wrote %d edges", n2)
	}
}

func TestExpandAssociativeRecall(t *testing.T) {
	ctx := context.Background()
	seed := mk("s", ScopeGlobal, "deployment uses docker compose", CategoryProject, SourceConsolidated)
	seed.Related = []string{"n"} // s links to n
	neighbor := mk("n", ScopeGlobal, "compose file lives at deploy/docker-compose.yml", CategoryProject, SourceConsolidated)
	other := mk("o", ScopeGlobal, "user prefers tabs over spaces", CategoryPreference, SourceConsolidated)
	store := newMemStore(seed, neighbor, other)

	res, err := Recall(ctx, store, Query{Text: "deployment", Scopes: []Scope{ScopeGlobal}, K: 3})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range res.Recalled {
		got[r.ID] = true
	}
	if !got["s"] {
		t.Fatalf("seed should be recalled for 'deployment', got %v", got)
	}
	if !got["n"] {
		t.Fatalf("associative neighbor should be folded in via Related, got %v", got)
	}
	if got["o"] {
		t.Fatalf("unrelated, unmatched fact must not be recalled, got %v", got)
	}
}
