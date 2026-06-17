package memory

import (
	"context"
	"errors"
	"testing"
)

const scopeA Scope = "project:a"
const scopeB Scope = "project:b"

func projFact(id string, scope Scope, text string) Record {
	return mk(id, scope, text, CategoryFact, SourceConsolidated)
}

// Plan runs the model once; preview (dry-run) shows the cross-scope changelog
// without writing; apply creates the global fact and deletes the subsumed copies.
func TestPlanApplyGlobalPromotion(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		projFact("a1", scopeA, "uses just as the task runner"),
		projFact("b1", scopeB, "task runner is just"),
		projFact("a2", scopeA, "reviewbot stores feedback in feedback.yaml"), // project-specific, stays
	)
	var calls int
	pr := fakeExtractor{promote: func(c []ScopedFact) []Promotion {
		calls++
		return []Promotion{{Text: "Uses `just` as the task runner across projects", Category: CategoryProject, Subsumes: []string{"a1", "b1"}}}
	}}

	plan, err := PlanGlobalPromotion(ctx, store, pr, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(plan.Promotions) != 1 {
		t.Fatalf("plan should call model once and capture 1 promotion: calls=%d plan=%+v", calls, plan)
	}

	// Preview: no writes, no further model call.
	prev, err := ApplyGlobalPromotion(ctx, store, plan, ConsolidateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("dry-run apply must not call the model, calls=%d", calls)
	}
	if len(prev.Promoted) != 1 || len(prev.Deleted) != 2 {
		t.Fatalf("preview should create 1 global, remove 2 copies: %+v", prev)
	}
	if all, _ := store.List(ctx); len(all) != 3 {
		t.Fatalf("dry-run must not write, store has %d", len(all))
	}

	// Apply for real.
	rep, err := ApplyGlobalPromotion(ctx, store, plan, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("real apply must not call the model, calls=%d", calls)
	}
	globals, _ := store.List(ctx, ScopeGlobal)
	if len(globals) != 1 || globals[0].DerivedFrom == nil {
		t.Fatalf("expected one global fact with provenance, got %+v", globals)
	}
	all, _ := store.List(ctx)
	if len(all) != 2 { // a2 (untouched) + the new global
		t.Fatalf("expected a2 + global to remain, got %d: %+v", len(all), all)
	}
	for _, r := range all {
		if r.ID == "a1" || r.ID == "b1" {
			t.Fatalf("subsumed copy %s should have been deleted", r.ID)
		}
	}
	_ = rep
}

func TestApplyGlobalRefusesStale(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		projFact("a1", scopeA, "uses just"),
		projFact("b1", scopeB, "uses just too"),
	)
	pr := fakeExtractor{promote: func(_ []ScopedFact) []Promotion {
		return []Promotion{{Text: "uses just", Category: CategoryProject, Subsumes: []string{"a1", "b1"}}}
	}}
	plan, err := PlanGlobalPromotion(ctx, store, pr, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A project changes after planning.
	if err := store.Put(ctx, projFact("a2", scopeA, "new fact in A")); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyGlobalPromotion(ctx, store, plan, ConsolidateOptions{}); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("expected ErrStalePlan, got %v", err)
	}
}

// Hallucinated or protected ids in a promotion's subsumes are never deleted.
func TestApplyGlobalDropsUnknownAndProtectedSubsumes(t *testing.T) {
	ctx := context.Background()
	locked := mk("a1", scopeA, "locked fact", CategoryFact, SourceConsolidated)
	locked.Locked = true
	store := newMemStore(
		locked,
		projFact("a2", scopeA, "real fact"),
	)
	plan := GlobalPlan{
		Promotions: []Promotion{{
			Text: "a global fact", Category: CategoryFact, Subsumes: []string{"a2", "a1", "ghost-id"},
		}},
		// a1 is locked (protected) so it's not in the fingerprinted set; only a2 is.
		Fingerprints: map[string]string{string(scopeA): fingerprint([]Record{projFact("a2", scopeA, "real fact")})},
	}
	rep, err := ApplyGlobalPromotion(ctx, store, plan, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Deleted) != 1 || rep.Deleted[0].ID != "a2" {
		t.Fatalf("only the real, unprotected id should be deleted: %+v", rep.Deleted)
	}
	if _, err := store.Get(ctx, "a1"); err != nil {
		t.Fatalf("locked fact must survive: %v", err)
	}
}

func TestApplyGlobalOverDeletionGuard(t *testing.T) {
	ctx := context.Background()
	recs := make([]Record, 0, 10)
	subsume := make([]string, 0, 6)
	for i := 0; i < 10; i++ {
		id := string(rune('a'+i)) + "1"
		recs = append(recs, projFact(id, scopeA, "fact "+id))
		if i < 6 { // subsume 6 of 10 → >half
			subsume = append(subsume, id)
		}
	}
	store := newMemStore(recs...)
	plan := GlobalPlan{
		Promotions:   []Promotion{{Text: "too much", Category: CategoryFact, Subsumes: subsume}},
		Fingerprints: map[string]string{string(scopeA): fingerprint(recs)},
	}
	rep, err := ApplyGlobalPromotion(ctx, store, plan, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.ReorgRefused {
		t.Fatal("deleting >half a scope should be refused")
	}
	if all, _ := store.List(ctx); len(all) != 10 {
		t.Fatalf("refusal must write nothing, store has %d", len(all))
	}
}
