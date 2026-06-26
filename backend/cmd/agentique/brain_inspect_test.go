package main

import (
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func TestComputeBrainStats(t *testing.T) {
	recs := []memory.Record{
		{ID: "1", Scope: "global", Category: memory.CategoryPreference, Source: memory.SourceHuman, ConfidenceScore: 1.0, Pinned: true, Uses: 3},
		{ID: "2", Scope: "global", Category: memory.CategoryFact, Source: memory.SourceConsolidated, ConfidenceScore: 0.8},
		{ID: "3", Scope: "project:a", Category: memory.CategoryFact, Source: memory.SourceConsolidated, ConfidenceScore: 0.4}, // ambiguous (<0.55)
		{ID: "4", Scope: "project:a", Category: memory.CategoryProject, Source: memory.SourceAgent, ConfidenceScore: 0.8, Locked: true, ReviewNote: "contradicted"},
		{ID: "6", Scope: "project:a", Category: memory.CategoryFact, Source: memory.SourceConsolidated, ConfidenceScore: 0.8},
		{ID: "5", Scope: "project:a", Category: memory.CategoryFact, Source: memory.SourceCapture}, // capture: excluded from total
	}

	s := computeBrainStats(recs)

	if s.Total != 5 {
		t.Errorf("Total = %d, want 5 (captures excluded)", s.Total)
	}
	if s.Captures != 1 {
		t.Errorf("Captures = %d, want 1", s.Captures)
	}
	if s.Pinned != 1 {
		t.Errorf("Pinned = %d, want 1", s.Pinned)
	}
	if s.Locked != 1 {
		t.Errorf("Locked = %d, want 1", s.Locked)
	}
	if s.FlaggedReview != 1 {
		t.Errorf("FlaggedReview = %d, want 1", s.FlaggedReview)
	}
	// Trust tiers (derived from source+score): human->extracted, 0.8->inferred, 0.4->ambiguous.
	if got := s.ByTrust[string(memory.ConfidenceExtracted)]; got != 1 {
		t.Errorf("extracted = %d, want 1", got)
	}
	if got := s.ByTrust[string(memory.ConfidenceInferred)]; got != 3 {
		t.Errorf("inferred = %d, want 3", got)
	}
	if got := s.ByTrust[string(memory.ConfidenceAmbiguous)]; got != 1 {
		t.Errorf("ambiguous = %d, want 1", got)
	}
	// Per-scope: largest first. project:a has 3 durable, global has 2.
	if len(s.ByScope) != 2 {
		t.Fatalf("ByScope len = %d, want 2", len(s.ByScope))
	}
	if s.ByScope[0].Scope != "project:a" || s.ByScope[0].Count != 3 {
		t.Errorf("ByScope[0] = %+v, want project:a/3", s.ByScope[0])
	}
	if s.ByScope[1].Scope != "global" || s.ByScope[1].Count != 2 {
		t.Errorf("ByScope[1] = %+v, want global/2", s.ByScope[1])
	}
	if s.BySource[string(memory.SourceCapture)] != 0 {
		t.Errorf("captures must not count toward BySource, got %d", s.BySource[string(memory.SourceCapture)])
	}
}

func TestSortRecords(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id string, uses int, ageDays int) memory.Record {
		return memory.Record{ID: id, Uses: uses, CreatedAt: t0.AddDate(0, 0, -ageDays)}
	}

	t.Run("uses desc, newest tiebreak", func(t *testing.T) {
		recs := []memory.Record{mk("a", 1, 10), mk("b", 5, 5), mk("c", 5, 1)}
		sortRecords(recs, "uses")
		got := []string{recs[0].ID, recs[1].ID, recs[2].ID}
		// b and c tie on uses=5; c is newer (ageDays 1 < 5) so c first.
		want := []string{"c", "b", "a"}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("uses sort = %v, want %v", got, want)
				break
			}
		}
	})

	t.Run("new = newest first", func(t *testing.T) {
		recs := []memory.Record{mk("a", 9, 10), mk("b", 0, 1), mk("c", 0, 5)}
		sortRecords(recs, "new")
		got := []string{recs[0].ID, recs[1].ID, recs[2].ID}
		want := []string{"b", "c", "a"} // by recency, ignoring uses
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("new sort = %v, want %v", got, want)
				break
			}
		}
	})
}

func TestTruncateOneLine(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"a\nb\tc  d", 20, "a b c d"},        // newlines/tabs collapse to single spaces
		{"abcdefghij", 5, "abcd…"},           // clip to max runes incl. ellipsis
		{"exactly ten!", 12, "exactly ten!"}, // boundary: len == max, no clip
	}
	for _, c := range cases {
		if got := truncateOneLine(c.in, c.max); got != c.want {
			t.Errorf("truncateOneLine(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestScopeLabel(t *testing.T) {
	names := map[string]string{"project:abc": "My Project"}
	if got := scopeLabel(memory.ScopeGlobal, names); got != "global" {
		t.Errorf("global scope = %q, want global", got)
	}
	if got := scopeLabel("project:abc", names); got != "My Project" {
		t.Errorf("known project scope = %q, want My Project", got)
	}
	if got := scopeLabel("project:unknown", names); got != "project:unknown" {
		t.Errorf("unknown scope = %q, want raw scope", got)
	}
}

func TestTrustLabel(t *testing.T) {
	// Score is canonical; the tier is derived from (source, score).
	if got := trustLabel(memory.Record{Source: memory.SourceHuman, ConfidenceScore: 1.0}); got != "extracted 1.00" {
		t.Errorf("human trust = %q, want extracted 1.00", got)
	}
	if got := trustLabel(memory.Record{Source: memory.SourceConsolidated, ConfidenceScore: 0.8}); got != "inferred 0.80" {
		t.Errorf("inferred trust = %q, want inferred 0.80", got)
	}
	if got := trustLabel(memory.Record{Source: memory.SourceConsolidated, ConfidenceScore: 0.3}); got != "ambiguous 0.30" {
		t.Errorf("ambiguous trust = %q, want ambiguous 0.30", got)
	}
}

func TestFilterRecords(t *testing.T) {
	recs := []memory.Record{
		{ID: "1", Category: memory.CategoryFact},
		{ID: "2", Category: memory.CategoryPreference},
		{ID: "3", Category: memory.CategoryFact},
	}
	got := filterRecords(recs, func(r memory.Record) bool { return r.Category == memory.CategoryFact })
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "3" {
		t.Errorf("filterRecords = %+v, want ids 1,3", got)
	}
}
