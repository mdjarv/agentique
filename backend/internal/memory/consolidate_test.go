package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeExtractor struct {
	extract    func(episodes []string) []Candidate
	reorganize func(facts []Fact) []Fact
	promote    func(candidates []ScopedFact) []Promotion
}

func (f fakeExtractor) Promote(_ context.Context, c []ScopedFact) ([]Promotion, error) {
	if f.promote == nil {
		return nil, nil
	}
	return f.promote(c), nil
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

// The whole point of the Plan/Apply split: the model runs once at plan time;
// neither preview (dry-run apply) nor real apply calls it again.
func TestPlanApplyRunsModelOnce(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		mk("a", ScopeGlobal, "vague a", CategoryFact, SourceConsolidated),
		mk("b", ScopeGlobal, "fact b", CategoryFact, SourceConsolidated),
	)
	var reorgCalls int
	ex := fakeExtractor{reorganize: func(facts []Fact) []Fact {
		reorgCalls++
		return []Fact{{ID: "a", Text: "clarified a", Category: CategoryFact}} // keep a (rewritten), drop b
	}}

	p, err := PlanConsolidation(ctx, store, ex, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if reorgCalls != 1 || !p.ReorganizeRan || len(p.Reorganized) != 1 {
		t.Fatalf("plan should call model once and capture output: calls=%d plan=%+v", reorgCalls, p)
	}

	// Preview: dry-run apply must not call the model and must not write.
	prev, err := ApplyPlan(ctx, store, ScopeGlobal, p, ConsolidateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if reorgCalls != 1 {
		t.Fatalf("dry-run apply must not call the model, calls=%d", reorgCalls)
	}
	if len(prev.Rewritten) != 1 || len(prev.Deleted) != 1 {
		t.Fatalf("preview changelog wrong: rewritten=%d deleted=%d", len(prev.Rewritten), len(prev.Deleted))
	}
	if all, _ := store.List(ctx); len(all) != 2 {
		t.Fatalf("dry-run must not write, store has %d", len(all))
	}

	// Apply for real: still no second model call; the previewed change lands.
	rep, err := ApplyPlan(ctx, store, ScopeGlobal, p, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if reorgCalls != 1 {
		t.Fatalf("real apply must not call the model, calls=%d", reorgCalls)
	}
	if len(rep.Rewritten) != 1 || len(rep.Deleted) != 1 {
		t.Fatalf("apply changelog wrong: rewritten=%d deleted=%d", len(rep.Rewritten), len(rep.Deleted))
	}
	all, _ := store.List(ctx)
	if len(all) != 1 || all[0].ID != "a" || all[0].Text != "clarified a" {
		t.Fatalf("apply result wrong: %+v", all)
	}
}

// A plan applied after the underlying set changed must be refused, not clobber the
// newer state.
func TestApplyPlanRefusesStale(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		mk("a", ScopeGlobal, "fact a", CategoryFact, SourceConsolidated),
		mk("b", ScopeGlobal, "fact b", CategoryFact, SourceConsolidated),
	)
	ex := fakeExtractor{reorganize: func(facts []Fact) []Fact {
		return []Fact{{ID: "a", Text: "merged", Category: CategoryFact}}
	}}
	p, err := PlanConsolidation(ctx, store, ex, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A new reorganizable fact appears after planning.
	if err := store.Put(ctx, mk("c", ScopeGlobal, "new fact c", CategoryFact, SourceConsolidated)); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPlan(ctx, store, ScopeGlobal, p, ConsolidateOptions{}); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("expected ErrStalePlan, got %v", err)
	}
}

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

// A lower MinSurvivorRatio (aggressive consolidation) permits a deeper cut that the default
// 0.5 guard would refuse; the chosen ratio rides along in the Plan so a preview and
// its later apply enforce the identical guard.
func TestConsolidateAggressiveSurvivorRatio(t *testing.T) {
	var recs []Record
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} { // 8 facts
		recs = append(recs, mk(id, ScopeGlobal, "fact "+id, CategoryFact, SourceAgent))
	}
	keep3 := fakeExtractor{reorganize: func(_ []Fact) []Fact {
		return []Fact{{ID: "a", Text: "fact a"}, {ID: "b", Text: "fact b"}, {ID: "c", Text: "fact c"}}
	}}

	// Default guard (0.5) refuses keeping only 3/8.
	store := newMemStore(recs...)
	rep, _ := Consolidate(context.Background(), store, keep3, ScopeGlobal, ConsolidateOptions{})
	if !rep.ReorgRefused {
		t.Fatal("default guard should refuse a 3/8 survivor reorg")
	}

	// Aggressive guard (0.3) allows it: 3 >= 0.3*8 = 2.4.
	store2 := newMemStore(recs...)
	rep2, err := Consolidate(context.Background(), store2, keep3, ScopeGlobal, ConsolidateOptions{MinSurvivorRatio: 0.3})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.ReorgRefused {
		t.Fatal("aggressive guard (0.3) should permit a 3/8 survivor reorg")
	}
	if len(rep2.Deleted) != 5 {
		t.Fatalf("expected 5 deletions, got %d", len(rep2.Deleted))
	}
	all, _ := store2.List(context.Background())
	if len(all) != 3 {
		t.Fatalf("expected 3 surviving facts, got %d", len(all))
	}
}

// The guard ratio captured at plan time governs apply, even if the caller passes a
// different ConsolidateOptions to ApplyPlan — preview and apply must agree.
func TestApplyPlanUsesPlanSurvivorRatio(t *testing.T) {
	var recs []Record
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		recs = append(recs, mk(id, ScopeGlobal, "fact "+id, CategoryFact, SourceAgent))
	}
	store := newMemStore(recs...)
	keep3 := fakeExtractor{reorganize: func(_ []Fact) []Fact {
		return []Fact{{ID: "a", Text: "fact a"}, {ID: "b", Text: "fact b"}, {ID: "c", Text: "fact c"}}
	}}
	// Plan with the aggressive ratio; apply with bare options.
	plan, err := PlanConsolidation(context.Background(), store, keep3, ScopeGlobal, ConsolidateOptions{MinSurvivorRatio: 0.3})
	if err != nil {
		t.Fatal(err)
	}
	if plan.MinSurvivorRatio != 0.3 {
		t.Fatalf("plan should carry the 0.3 ratio, got %v", plan.MinSurvivorRatio)
	}
	rep, err := ApplyPlan(context.Background(), store, ScopeGlobal, plan, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ReorgRefused {
		t.Fatal("apply must honour the plan's aggressive ratio, not the bare options' default")
	}
}

func TestConsolidateKeepsCapturesOnEmptyExtraction(t *testing.T) {
	store := newMemStore(capture("c1", "some episode"), capture("c2", "another episode"))
	// Extractor has a weak turn and returns nothing (valid, no error).
	ex := fakeExtractor{extract: func(_ []string) []Candidate { return nil }}
	rep, err := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.CapturesConsumed) != 0 {
		t.Fatalf("empty extraction must not consume captures, got %+v", rep.CapturesConsumed)
	}
	all, _ := store.List(context.Background())
	if len(all) != 2 {
		t.Fatalf("captures must survive an empty extraction, got %d", len(all))
	}
}

func TestConsolidateAllowsAbstractionHeavyReorg(t *testing.T) {
	var recs []Record
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} { // 8 facts
		recs = append(recs, mk(id, ScopeGlobal, "fact "+id, CategoryFact, SourceAgent))
	}
	store := newMemStore(recs...)
	// Keep 1 original, add 4 abstractions => 5 survivors of 8 => NOT refused.
	ex := fakeExtractor{reorganize: func(_ []Fact) []Fact {
		return []Fact{
			{ID: "a", Text: "fact a"},
			{Text: "general rule 1", Category: CategoryPreference},
			{Text: "general rule 2", Category: CategoryPreference},
			{Text: "general rule 3", Category: CategoryPreference},
			{Text: "general rule 4", Category: CategoryPreference},
		}
	}}
	rep, err := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ReorgRefused {
		t.Fatal("abstraction-heavy reorg with 5/8 survivors should not be refused")
	}
	if len(rep.Abstracted) != 4 {
		t.Fatalf("expected 4 abstractions, got %d", len(rep.Abstracted))
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

// Force overrides the fingerprint short-circuit: an unchanged scope is reorganized
// anyway, so a re-consolidate after a prompt/algorithm change isn't blocked.
func TestConsolidateForceRerunsUnchanged(t *testing.T) {
	store := newMemStore(
		mk("a", ScopeGlobal, "user prefers dark mode in the editor", CategoryPreference, SourceAgent),
		mk("b", ScopeGlobal, "dark mode is the editor preference", CategoryPreference, SourceAgent),
	)
	rep1, _ := Consolidate(context.Background(), store, fakeExtractor{}, ScopeGlobal, ConsolidateOptions{})

	// Same fingerprint, but Force => reorganize runs. A merge collapses a→b's text.
	merge := fakeExtractor{reorganize: func(in []Fact) []Fact {
		return []Fact{{ID: in[0].ID, Text: "editor uses dark mode", Category: CategoryPreference}}
	}}
	plan, err := PlanConsolidation(context.Background(), store, merge, ScopeGlobal, ConsolidateOptions{
		PrevFingerprint: rep1.Fingerprint, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ReorganizeSkipped || !plan.ReorganizeRan {
		t.Fatalf("force should reorganize an unchanged scope, got skipped=%v ran=%v", plan.ReorganizeSkipped, plan.ReorganizeRan)
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
