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
