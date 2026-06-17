package memory

import (
	"context"
	"reflect"
	"testing"
)

// Two tight topic clusters with no token overlap between them must land in two
// distinct communities; an unrelated fact forms its own singleton.
func TestDetectCommunitiesSplitsTopics(t *testing.T) {
	recs := []Record{
		mk("a", scopeA, "user prefers dark mode in the editor", CategoryPreference, SourceConsolidated),
		mk("b", scopeA, "dark mode is the editor preference", CategoryPreference, SourceConsolidated),
		mk("c", scopeA, "editor uses dark mode by default", CategoryPreference, SourceConsolidated),
		mk("d", scopeA, "deployment runs through docker compose", CategoryProject, SourceConsolidated),
		mk("e", scopeA, "docker compose handles deployment", CategoryProject, SourceConsolidated),
		mk("f", scopeA, "quarterly invoices reconcile against ledger totals", CategoryFact, SourceConsolidated),
	}
	comm := DetectCommunities(recs, DefaultRelatedThreshold)

	if comm["a"] != comm["b"] || comm["b"] != comm["c"] {
		t.Fatalf("editor/dark-mode facts should share a community: %v", comm)
	}
	if comm["d"] != comm["e"] {
		t.Fatalf("deployment facts should share a community: %v", comm)
	}
	if comm["a"] == comm["d"] {
		t.Fatalf("unrelated topics must not share a community: %v", comm)
	}
	if comm["f"] == comm["a"] || comm["f"] == comm["d"] {
		t.Fatalf("isolated fact f should be its own singleton community: %v", comm)
	}
}

// Related edges link clusters even when the texts share no tokens — the curated
// graph is part of the similarity signal.
func TestDetectCommunitiesUsesRelatedEdges(t *testing.T) {
	a := mk("a", scopeA, "alpha bravo charlie", CategoryFact, SourceConsolidated)
	b := mk("b", scopeA, "delta echo foxtrot", CategoryFact, SourceConsolidated)
	a.Related = []string{"b"} // no token overlap, but explicitly linked
	b.Related = []string{"a"}
	comm := DetectCommunities([]Record{a, b}, DefaultRelatedThreshold)
	if comm["a"] != comm["b"] {
		t.Fatalf("Related edge should merge a and b into one community: %v", comm)
	}
}

// The partition must be identical regardless of input ordering and stable across
// repeated runs — plans built from it have to be reproducible.
func TestDetectCommunitiesDeterministic(t *testing.T) {
	recs := []Record{
		mk("a", scopeA, "user prefers dark mode editor", CategoryPreference, SourceConsolidated),
		mk("b", scopeA, "dark mode editor preference set", CategoryPreference, SourceConsolidated),
		mk("c", scopeA, "deployment docker compose stack", CategoryProject, SourceConsolidated),
		mk("d", scopeA, "docker compose deployment stack", CategoryProject, SourceConsolidated),
	}
	want := DetectCommunities(recs, DefaultRelatedThreshold)

	shuffled := []Record{recs[3], recs[1], recs[0], recs[2]}
	got := DetectCommunities(shuffled, DefaultRelatedThreshold)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("community assignment must be order-independent:\n want=%v\n got =%v", want, got)
	}
	// And idempotent across runs.
	if again := DetectCommunities(recs, DefaultRelatedThreshold); !reflect.DeepEqual(want, again) {
		t.Fatalf("community assignment must be stable across runs:\n first=%v\n again=%v", want, again)
	}
}

func TestDetectCommunitiesEmpty(t *testing.T) {
	if got := DetectCommunities(nil, DefaultRelatedThreshold); len(got) != 0 {
		t.Fatalf("empty input should yield no communities, got %v", got)
	}
}

func TestAssignCommunitiesPersistsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		mk("a", scopeA, "user prefers dark mode in the editor", CategoryPreference, SourceConsolidated),
		mk("b", scopeA, "dark mode is the editor preference", CategoryPreference, SourceConsolidated),
		mk("c", scopeA, "deployment runs through docker compose", CategoryProject, SourceConsolidated),
		mk("d", scopeA, "docker compose handles deployment", CategoryProject, SourceConsolidated),
	)
	changed, err := AssignCommunities(ctx, store, scopeA)
	if err != nil {
		t.Fatal(err)
	}
	if changed == 0 {
		t.Fatal("first assignment should write communities")
	}

	a, _ := store.Get(ctx, "a")
	b, _ := store.Get(ctx, "b")
	c, _ := store.Get(ctx, "c")
	if a.Community != b.Community {
		t.Fatalf("editor facts should share a community: a=%d b=%d", a.Community, b.Community)
	}
	if a.Community == c.Community {
		t.Fatalf("editor and deployment facts must differ: a=%d c=%d", a.Community, c.Community)
	}

	// Idempotent: a second run over an unchanged set rewrites nothing.
	if again, _ := AssignCommunities(ctx, store, scopeA); again != 0 {
		t.Fatalf("re-assign over an unchanged set should be a no-op, wrote %d", again)
	}
}
