package memory

import (
	"context"
	"testing"
	"time"
)

type fakeExtractor struct {
	extract    func(episodes []string) []Candidate
	reorganize func(facts []Fact) []Fact
}

func (f fakeExtractor) Extract(_ context.Context, e []string) ([]Candidate, error) {
	if f.extract == nil {
		return nil, nil
	}
	return f.extract(e), nil
}

func (f fakeExtractor) Reorganize(_ context.Context, facts []Fact) ([]Fact, error) {
	if f.reorganize == nil {
		return facts, nil
	}
	return f.reorganize(facts), nil
}

func mk(id string, scope Scope, text string, cat Category, src Source) Record {
	r := New(scope, text, cat, src)
	r.ID = id
	return r
}

func capture(id, text string) Record { return mk(id, ScopeGlobal, text, CategoryFact, SourceCapture) }

func TestConsolidatePromotesCaptures(t *testing.T) {
	store := newMemStore(capture("c1", "user ran the build with just"), capture("c2", "user fixed a flaky test"))
	ex := fakeExtractor{extract: func(_ []string) []Candidate {
		return []Candidate{{Text: "Project builds via `just`.", Category: CategoryProject}}
	}}
	rep, err := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Promoted) != 1 {
		t.Fatalf("promoted=%d want 1", len(rep.Promoted))
	}
	p := rep.Promoted[0]
	if p.Source != SourceConsolidated || len(p.DerivedFrom) != 2 {
		t.Fatalf("promoted provenance wrong: %+v", p)
	}
	if len(rep.CapturesConsumed) != 2 {
		t.Fatalf("consumed=%d want 2", len(rep.CapturesConsumed))
	}
	// captures gone, durable fact present
	all, _ := store.List(context.Background())
	if len(all) != 1 || all[0].Source == SourceCapture {
		t.Fatalf("store should hold exactly the promoted fact, got %+v", all)
	}
}

func TestConsolidateDedupsPromotion(t *testing.T) {
	store := newMemStore(
		mk("d1", ScopeGlobal, "Project builds via just.", CategoryProject, SourceAgent),
		capture("c1", "user ran just again"),
	)
	ex := fakeExtractor{extract: func(_ []string) []Candidate {
		return []Candidate{{Text: "project builds via just", Category: CategoryProject}} // dup of d1
	}}
	rep, err := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Promoted) != 0 {
		t.Fatalf("duplicate candidate should not be promoted, got %+v", rep.Promoted)
	}
}

func TestConsolidateIdentityAutoPin(t *testing.T) {
	store := newMemStore(capture("c1", "my name is mathias"))
	ex := fakeExtractor{extract: func(_ []string) []Candidate {
		return []Candidate{{Text: "User's name is Mathias.", Category: CategoryIdentity}}
	}}
	rep, _ := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if len(rep.Promoted) != 1 || !rep.Promoted[0].Pinned {
		t.Fatalf("identity fact should be auto-pinned: %+v", rep.Promoted)
	}
}

func TestConsolidateReorganizeRewriteAndDelete(t *testing.T) {
	store := newMemStore(
		mk("a", ScopeGlobal, "vague thing", CategoryFact, SourceAgent),
		mk("b", ScopeGlobal, "fact b", CategoryFact, SourceAgent),
		mk("c", ScopeGlobal, "fact c", CategoryFact, SourceAgent),
	)
	ex := fakeExtractor{reorganize: func(facts []Fact) []Fact {
		// rewrite a, keep b, drop c
		out := []Fact{}
		for _, f := range facts {
			switch f.ID {
			case "a":
				out = append(out, Fact{ID: "a", Text: "clarified thing", Category: CategoryFact})
			case "b":
				out = append(out, f)
			}
		}
		return out
	}}
	rep, err := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rewritten) != 1 || rep.Rewritten[0].After.Text != "clarified thing" {
		t.Fatalf("expected one rewrite, got %+v", rep.Rewritten)
	}
	if len(rep.Deleted) != 1 || rep.Deleted[0].ID != "c" {
		t.Fatalf("expected c deleted, got %+v", rep.Deleted)
	}
	if _, err := store.Get(context.Background(), "c"); err != ErrNotFound {
		t.Fatal("c should be gone from store")
	}
}

func TestConsolidateAbstractsNewFact(t *testing.T) {
	store := newMemStore(
		mk("a", ScopeGlobal, "used just on monday", CategoryFact, SourceAgent),
		mk("b", ScopeGlobal, "used just on tuesday", CategoryFact, SourceAgent),
	)
	ex := fakeExtractor{reorganize: func(facts []Fact) []Fact {
		out := append([]Fact(nil), facts...)
		out = append(out, Fact{Text: "User consistently uses just.", Category: CategoryPreference}) // empty ID -> abstraction
		return out
	}}
	rep, _ := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if len(rep.Abstracted) != 1 || rep.Abstracted[0].Source != SourceConsolidated {
		t.Fatalf("expected one abstraction, got %+v", rep.Abstracted)
	}
}

func TestConsolidateDropsInventedIDs(t *testing.T) {
	store := newMemStore(mk("a", ScopeGlobal, "fact a", CategoryFact, SourceAgent))
	ex := fakeExtractor{reorganize: func(_ []Fact) []Fact {
		return []Fact{
			{ID: "a", Text: "fact a", Category: CategoryFact},
			{ID: "fabricated-id", Text: "hallucinated", Category: CategoryFact},
		}
	}}
	rep, _ := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if len(rep.Abstracted) != 0 {
		t.Fatalf("invented id must not become a record, got %+v", rep.Abstracted)
	}
	all, _ := store.List(context.Background())
	if len(all) != 1 {
		t.Fatalf("store should be unchanged size, got %d", len(all))
	}
}

func TestConsolidateOverDeletionSafetyNet(t *testing.T) {
	var recs []Record
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} { // 8 facts
		recs = append(recs, mk(id, ScopeGlobal, "fact "+id, CategoryFact, SourceAgent))
	}
	store := newMemStore(recs...)
	ex := fakeExtractor{reorganize: func(_ []Fact) []Fact {
		// keep only 3 of 8 -> <50% -> must be refused
		return []Fact{
			{ID: "a", Text: "fact a"}, {ID: "b", Text: "fact b"}, {ID: "c", Text: "fact c"},
		}
	}}
	rep, _ := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if !rep.ReorgRefused {
		t.Fatal("expected reorg refusal")
	}
	if len(rep.Deleted) != 0 {
		t.Fatalf("no deletions should be applied on refusal, got %+v", rep.Deleted)
	}
	all, _ := store.List(context.Background())
	if len(all) != 8 {
		t.Fatalf("all 8 facts should remain, got %d", len(all))
	}
}

func TestConsolidateFingerprintSkip(t *testing.T) {
	store := newMemStore(
		mk("a", ScopeGlobal, "fact a", CategoryFact, SourceAgent),
		mk("b", ScopeGlobal, "fact b", CategoryFact, SourceAgent),
	)
	// First pass: no-op reorganize, capture fingerprint.
	rep1, _ := Consolidate(context.Background(), store, fakeExtractor{}, ScopeGlobal, ConsolidateOptions{})
	// Second pass with prev fingerprint and a destructive reorganize that should be skipped.
	destructive := fakeExtractor{reorganize: func(_ []Fact) []Fact { return nil }}
	rep2, _ := Consolidate(context.Background(), store, destructive, ScopeGlobal, ConsolidateOptions{PrevFingerprint: rep1.Fingerprint})
	if !rep2.Skipped {
		t.Fatal("expected reorganization to be skipped on unchanged fingerprint")
	}
	all, _ := store.List(context.Background())
	if len(all) != 2 {
		t.Fatalf("skip must not mutate store, got %d", len(all))
	}
}

func TestConsolidateProtectsPinnedLockedHuman(t *testing.T) {
	pinned := mk("p", ScopeGlobal, "pinned fact", CategoryIdentity, SourceAgent)
	pinned.Pinned = true
	locked := mk("l", ScopeGlobal, "locked fact", CategoryFact, SourceAgent)
	locked.Locked = true
	human := mk("h", ScopeGlobal, "human fact", CategoryFact, SourceHuman)
	store := newMemStore(pinned, locked, human)
	// Reorganize returns nothing -> would delete everything it's allowed to touch.
	ex := fakeExtractor{reorganize: func(_ []Fact) []Fact { return nil }}
	rep, _ := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if len(rep.Deleted) != 0 {
		t.Fatalf("protected facts must not be deleted, got %+v", rep.Deleted)
	}
	all, _ := store.List(context.Background())
	if len(all) != 3 {
		t.Fatalf("all protected facts should remain, got %d", len(all))
	}
}

func TestConsolidateDecay(t *testing.T) {
	old := mk("old", ScopeGlobal, "stale unused fact", CategoryFact, SourceAgent)
	old.UpdatedAt = time.Now().UTC().Add(-100 * 24 * time.Hour)
	usedOld := mk("usedold", ScopeGlobal, "stale but useful", CategoryFact, SourceAgent)
	usedOld.UpdatedAt = time.Now().UTC().Add(-100 * 24 * time.Hour)
	usedOld.Uses = 5
	fresh := mk("fresh", ScopeGlobal, "recent fact", CategoryFact, SourceAgent)
	store := newMemStore(old, usedOld, fresh)

	rep, err := Consolidate(context.Background(), store, nil, ScopeGlobal, ConsolidateOptions{
		Decay: DecayPolicy{MaxAge: 30 * 24 * time.Hour, MinUses: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decayed) != 1 || rep.Decayed[0].ID != "old" {
		t.Fatalf("expected only 'old' decayed, got %+v", rep.Decayed)
	}
	if _, err := store.Get(context.Background(), "old"); err != ErrNotFound {
		t.Fatal("old should be pruned")
	}
	if _, err := store.Get(context.Background(), "usedold"); err != nil {
		t.Fatal("frequently-used stale fact should survive")
	}
}

func TestConsolidateNilExtractorOnlyDecays(t *testing.T) {
	store := newMemStore(capture("c1", "some episode"))
	rep, err := Consolidate(context.Background(), store, nil, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Promoted) != 0 {
		t.Fatal("nil extractor must not promote")
	}
	// capture remains (not distilled, not decayed without policy)
	all, _ := store.List(context.Background())
	if len(all) != 1 {
		t.Fatalf("capture should remain untouched, got %d", len(all))
	}
}
