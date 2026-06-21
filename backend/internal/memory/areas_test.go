package memory

import (
	"context"
	"strings"
	"testing"
)

// fact is a small Record constructor for the area tests (memStore lives in recall_test.go).
func fact(id string, scope Scope, text string) Record {
	return Record{ID: id, Scope: scope, Text: text, Category: CategoryFact, Source: SourceConsolidated}
}

func TestAssignAreasGroupsCrossScope(t *testing.T) {
	ctx := context.Background()
	// Two scopes share a "race detector tests" topic → a cross-scope area. A lone fact
	// in a third scope shares nothing → no area.
	store := newMemStore(
		fact("a", "project:one", "run go test race detector before commit"),
		fact("b", "project:two", "go test race detector required before commit"),
		fact("c", "project:three", "frontend uses tailwind for styling components"),
	)

	n, err := AssignAreas(ctx, store, DefaultAreaThreshold, 2)
	if err != nil {
		t.Fatalf("AssignAreas: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 records to gain an area, got %d", n)
	}

	a, _ := store.Get(ctx, "a")
	b, _ := store.Get(ctx, "b")
	c, _ := store.Get(ctx, "c")
	if a.Area == "" || a.Area != b.Area {
		t.Errorf("a and b should share a non-empty area: a=%q b=%q", a.Area, b.Area)
	}
	if c.Area != "" {
		t.Errorf("single-scope fact c should have no area, got %q", c.Area)
	}
	// Label is built from the most frequent shared significant tokens.
	if !strings.Contains(a.Area, "detector") {
		t.Errorf("area label %q should be drawn from the shared topic tokens", a.Area)
	}
}

func TestAssignAreasIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		fact("a", "project:one", "deploy with nginx proxy and lets encrypt tls"),
		fact("b", "project:two", "nginx proxy manager handles tls lets encrypt"),
	)
	if _, err := AssignAreas(ctx, store, DefaultAreaThreshold, 2); err != nil {
		t.Fatal(err)
	}
	n, err := AssignAreas(ctx, store, DefaultAreaThreshold, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second pass should change nothing, changed %d", n)
	}
}

func TestAssignAreasClearsStaleArea(t *testing.T) {
	ctx := context.Background()
	// A fact that no longer belongs to any cross-scope area must have its stale Area cleared.
	store := newMemStore(
		Record{ID: "x", Scope: "project:one", Text: "totally unique solitary fact",
			Category: CategoryFact, Source: SourceConsolidated, Area: "old label"},
	)
	if _, err := AssignAreas(ctx, store, DefaultAreaThreshold, 2); err != nil {
		t.Fatal(err)
	}
	x, _ := store.Get(ctx, "x")
	if x.Area != "" {
		t.Errorf("stale area should be cleared, got %q", x.Area)
	}
}

func TestExpandAssociativePullsCrossScopeAreaSiblings(t *testing.T) {
	// A ranked hit in project:one carries area "X". The fan-out should pull the same-area
	// facts from sibling scopes (the cross-project win) but not a different area, a
	// capture, or an already-included fact.
	seed := Record{ID: "seed", Scope: "project:one", Text: "seed", Area: "X", Source: SourceConsolidated}
	sibTwo := Record{ID: "sibTwo", Scope: "project:two", Text: "sibling two", Area: "X", Source: SourceConsolidated}
	sibThree := Record{ID: "sibThree", Scope: "project:three", Text: "sibling three", Area: "X", Source: SourceConsolidated}
	otherArea := Record{ID: "other", Scope: "project:two", Text: "different topic", Area: "Y", Source: SourceConsolidated}
	captureSameArea := Record{ID: "cap", Scope: "project:two", Text: "capture", Area: "X", Source: SourceCapture}

	all := []Record{seed, sibTwo, sibThree, otherArea, captureSameArea}
	got := expandAssociative([]Record{seed}, nil, all, 5)

	ids := map[string]bool{}
	for _, r := range got {
		ids[r.ID] = true
	}
	if !ids["sibTwo"] || !ids["sibThree"] {
		t.Errorf("expected cross-scope area siblings pulled in, got %v", ids)
	}
	if ids["other"] {
		t.Errorf("a different area must not be pulled in")
	}
	if ids["cap"] {
		t.Errorf("a capture must never be recalled")
	}
}

func TestAssignAreasIgnoresCaptures(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		Record{ID: "cap1", Scope: "project:one", Text: "raw capture about race detector tests",
			Category: CategoryFact, Source: SourceCapture},
		Record{ID: "cap2", Scope: "project:two", Text: "raw capture about race detector tests",
			Category: CategoryFact, Source: SourceCapture},
	)
	n, err := AssignAreas(ctx, store, DefaultAreaThreshold, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("captures must never be assigned an area, changed %d", n)
	}
}
