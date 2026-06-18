package memory

import (
	"reflect"
	"testing"
)

func TestBackfillSubsumed(t *testing.T) {
	index := map[string]SubsumedSource{
		"a": {Scope: "project:one", Text: "fact A"},
		"b": {Scope: "project:two", Text: "fact B"},
		"c": {Scope: "project:three", Text: "fact C"},
	}

	promoted := Record{
		ID:          "g1",
		Scope:       ScopeGlobal,
		Text:        "merged rule",
		DerivedFrom: []string{"a", "b", "c"},
	}
	partial := Record{
		ID:          "g2",
		Scope:       ScopeGlobal,
		Text:        "partly recoverable",
		DerivedFrom: []string{"a", "missing", "c"},
	}
	dangling := Record{
		ID:          "g3",
		Scope:       ScopeGlobal,
		Text:        "no sources survive",
		DerivedFrom: []string{"gone1", "gone2"},
	}
	alreadyFilled := Record{
		ID:          "g4",
		Scope:       ScopeGlobal,
		Text:        "already has provenance",
		DerivedFrom: []string{"a"},
		Subsumed:    []SubsumedSource{{Scope: "project:one", Text: "fact A"}},
	}
	noProvenance := Record{
		ID:    "p1",
		Scope: "project:one",
		Text:  "plain fact, never promoted",
	}

	got := BackfillSubsumed(
		[]Record{promoted, partial, dangling, alreadyFilled, noProvenance},
		index,
	)

	// alreadyFilled (Subsumed already set) and noProvenance (no DerivedFrom) are
	// skipped — the pass is idempotent and only touches unfilled promotions.
	if len(got) != 3 {
		t.Fatalf("expected 3 eligible results, got %d: %+v", len(got), got)
	}

	// g1: full match, Subsumed in DerivedFrom order.
	if got[0].Record.ID != "g1" {
		t.Fatalf("expected first result g1, got %s", got[0].Record.ID)
	}
	wantG1 := []SubsumedSource{
		{Scope: "project:one", Text: "fact A"},
		{Scope: "project:two", Text: "fact B"},
		{Scope: "project:three", Text: "fact C"},
	}
	if !reflect.DeepEqual(got[0].Record.Subsumed, wantG1) {
		t.Errorf("g1 Subsumed = %+v, want %+v", got[0].Record.Subsumed, wantG1)
	}
	if !reflect.DeepEqual(got[0].MatchedIDs, []string{"a", "b", "c"}) {
		t.Errorf("g1 MatchedIDs = %v", got[0].MatchedIDs)
	}
	if len(got[0].UnmatchedIDs) != 0 {
		t.Errorf("g1 UnmatchedIDs = %v, want none", got[0].UnmatchedIDs)
	}

	// g2: partial — matched ids skip the missing one, order preserved.
	if got[1].Record.ID != "g2" {
		t.Fatalf("expected second result g2, got %s", got[1].Record.ID)
	}
	wantG2 := []SubsumedSource{
		{Scope: "project:one", Text: "fact A"},
		{Scope: "project:three", Text: "fact C"},
	}
	if !reflect.DeepEqual(got[1].Record.Subsumed, wantG2) {
		t.Errorf("g2 Subsumed = %+v, want %+v", got[1].Record.Subsumed, wantG2)
	}
	if !reflect.DeepEqual(got[1].MatchedIDs, []string{"a", "c"}) {
		t.Errorf("g2 MatchedIDs = %v", got[1].MatchedIDs)
	}
	if !reflect.DeepEqual(got[1].UnmatchedIDs, []string{"missing"}) {
		t.Errorf("g2 UnmatchedIDs = %v", got[1].UnmatchedIDs)
	}

	// g3: nothing matched — returned for reporting but Subsumed stays empty so the
	// caller knows not to write it.
	if got[2].Record.ID != "g3" {
		t.Fatalf("expected third result g3, got %s", got[2].Record.ID)
	}
	if len(got[2].Record.Subsumed) != 0 {
		t.Errorf("g3 Subsumed = %+v, want empty", got[2].Record.Subsumed)
	}
	if len(got[2].MatchedIDs) != 0 {
		t.Errorf("g3 MatchedIDs = %v, want none", got[2].MatchedIDs)
	}
	if !reflect.DeepEqual(got[2].UnmatchedIDs, []string{"gone1", "gone2"}) {
		t.Errorf("g3 UnmatchedIDs = %v", got[2].UnmatchedIDs)
	}
}

func TestBackfillSubsumedIdempotent(t *testing.T) {
	index := map[string]SubsumedSource{"a": {Scope: "project:one", Text: "fact A"}}
	rec := Record{ID: "g1", Scope: ScopeGlobal, DerivedFrom: []string{"a"}}

	first := BackfillSubsumed([]Record{rec}, index)
	if len(first) != 1 || len(first[0].Record.Subsumed) != 1 {
		t.Fatalf("first pass did not fill Subsumed: %+v", first)
	}

	// Feeding the filled record back must produce no work — the second run is a no-op.
	second := BackfillSubsumed([]Record{first[0].Record}, index)
	if len(second) != 0 {
		t.Fatalf("second pass should be a no-op, got %+v", second)
	}
}

func TestBackfillSubsumedDoesNotMutateInput(t *testing.T) {
	index := map[string]SubsumedSource{"a": {Scope: "project:one", Text: "fact A"}}
	rec := Record{ID: "g1", Scope: ScopeGlobal, DerivedFrom: []string{"a"}}

	_ = BackfillSubsumed([]Record{rec}, index)
	if rec.Subsumed != nil {
		t.Errorf("input record was mutated: Subsumed = %+v", rec.Subsumed)
	}
}
