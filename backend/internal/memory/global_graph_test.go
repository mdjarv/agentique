package memory

import (
	"context"
	"testing"
)

// scoped builds a ScopedFact for the cross-scope graph tests.
func scoped(id string, scope Scope, text string) ScopedFact {
	return ScopedFact{ID: id, Scope: scope, Text: text, Category: CategoryProject}
}

// A topic shared across two projects forms a cross-scope group; a codebase-specific
// fact that lives in a single scope is excluded — the transferable-pattern guardrail.
func TestCrossScopeGroupsKeepsSharedDropsLocal(t *testing.T) {
	cands := []ScopedFact{
		scoped("a1", scopeA, "uses just as the task runner"),
		scoped("b1", scopeB, "task runner is just across the repo"),
		scoped("a2", scopeA, "reviewbot stores feedback in feedback.yaml"), // single-scope, codebase-specific
		scoped("c1", scopeC, "the widget controller lives in widget_controller.go"),
	}
	groups := CrossScopeGroups(cands, DefaultCommunityThreshold, DefaultMinPromotionScopes)
	if len(groups) != 1 {
		t.Fatalf("expected exactly one cross-scope group, got %d: %+v", len(groups), groups)
	}
	g := groups[0]
	if len(g.Scopes) != 2 || g.Scopes[0] != scopeA || g.Scopes[1] != scopeB {
		t.Fatalf("group should span project:a and project:b sorted, got %v", g.Scopes)
	}
	got := map[string]bool{}
	for _, m := range g.Members {
		got[m.ID] = true
	}
	if !got["a1"] || !got["b1"] || len(g.Members) != 2 {
		t.Fatalf("group should hold exactly the shared facts a1,b1, got %+v", g.Members)
	}
}

// The same convention seen in three projects with identical wording is one group
// spanning all three scopes (dedup-by-label: identical text → Jaccard 1.0).
func TestCrossScopeGroupsThreeScopes(t *testing.T) {
	cands := []ScopedFact{
		scoped("a1", scopeA, "prefers small focused pull requests"),
		scoped("b1", scopeB, "prefers small focused pull requests"),
		scoped("c1", scopeC, "prefers small focused pull requests"),
	}
	groups := CrossScopeGroups(cands, DefaultCommunityThreshold, 3)
	if len(groups) != 1 || len(groups[0].Scopes) != 3 {
		t.Fatalf("expected one group across 3 scopes, got %+v", groups)
	}
	// minScopes=4 can't be met by 3 scopes → no transferable pattern.
	if g := CrossScopeGroups(cands, DefaultCommunityThreshold, 4); len(g) != 0 {
		t.Fatalf("minScopes beyond the available scopes should yield no groups, got %+v", g)
	}
}

// The Promoter only ever sees facts that recur across projects; codebase-specific
// facts are filtered out before any model call (RFC P5 guardrail).
func TestPlanGlobalOnlyOffersTransferableFacts(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		projFact("a1", scopeA, "uses just as the task runner"),
		projFact("b1", scopeB, "the task runner is just"),
		projFact("a2", scopeA, "reviewbot stores feedback in feedback.yaml"), // codebase-specific
		projFact("c1", scopeC, "auth middleware lives in internal/auth"),     // codebase-specific
	)
	var seen []string
	pr := fakeExtractor{promote: func(c []ScopedFact) []Promotion {
		for _, f := range c {
			seen = append(seen, f.ID)
		}
		return nil
	}}
	if _, err := PlanGlobalPromotion(ctx, store, pr, ConsolidateOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !containsStr(seen, "a1") || !containsStr(seen, "b1") {
		t.Fatalf("only the cross-scope facts a1,b1 should reach the Promoter, got %v", seen)
	}
}

// When nothing changed since the last pass (matching manifest) the model run is
// skipped, and applying the skipped plan is a deterministic no-op.
func TestPlanGlobalIncrementalSkip(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		projFact("a1", scopeA, "uses just as the task runner"),
		projFact("b1", scopeB, "the task runner is just"),
	)
	manifest, err := ScopeManifest(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	pr := fakeExtractor{promote: func(c []ScopedFact) []Promotion {
		calls++
		return []Promotion{{Text: "uses just everywhere", Category: CategoryProject, Subsumes: []string{"a1", "b1"}}}
	}}

	plan, err := PlanGlobalPromotion(ctx, store, pr, ConsolidateOptions{PrevManifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Skipped || calls != 0 || len(plan.Promotions) != 0 {
		t.Fatalf("unchanged manifest should skip the model: skipped=%v calls=%d promos=%d", plan.Skipped, calls, len(plan.Promotions))
	}

	// Applying a skipped plan writes nothing and reports Skipped.
	rep, err := ApplyGlobalPromotion(ctx, store, plan, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Skipped || len(rep.Promoted) != 0 || len(rep.Deleted) != 0 {
		t.Fatalf("applying a skipped plan must be a no-op, got %+v", rep)
	}
	if all, _ := store.List(ctx); len(all) != 2 {
		t.Fatalf("skipped apply must not touch the store, has %d", len(all))
	}

	// Force overrides the skip and runs the model.
	if _, err := PlanGlobalPromotion(ctx, store, pr, ConsolidateOptions{PrevManifest: manifest, Force: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("Force should bypass the manifest skip and call the model once, calls=%d", calls)
	}
}

// A changed project (added/edited fact) advances the manifest, so the next pass is
// not skipped.
func TestPlanGlobalManifestDetectsChange(t *testing.T) {
	ctx := context.Background()
	store := newMemStore(
		projFact("a1", scopeA, "uses just as the task runner"),
		projFact("b1", scopeB, "the task runner is just"),
	)
	manifest, err := ScopeManifest(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	// A project changes after the manifest was taken.
	if err := store.Put(ctx, projFact("b2", scopeB, "also pins node version via .nvmrc")); err != nil {
		t.Fatal(err)
	}
	var calls int
	pr := fakeExtractor{promote: func(c []ScopedFact) []Promotion { calls++; return nil }}
	plan, err := PlanGlobalPromotion(ctx, store, pr, ConsolidateOptions{PrevManifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Skipped {
		t.Fatal("a changed project must not be skipped")
	}
	if manifestsEqual(manifest, plan.Fingerprints) {
		t.Fatal("the new manifest should differ from the stale one")
	}
}

func TestManifestsEqual(t *testing.T) {
	a := map[string]string{"project:a": "x", "project:b": "y"}
	if !manifestsEqual(a, map[string]string{"project:a": "x", "project:b": "y"}) {
		t.Fatal("identical manifests should be equal")
	}
	if manifestsEqual(a, map[string]string{"project:a": "x"}) {
		t.Fatal("different scope sets are not equal")
	}
	if manifestsEqual(a, map[string]string{"project:a": "x", "project:b": "z"}) {
		t.Fatal("different hashes are not equal")
	}
	if manifestsEqual(nil, nil) || manifestsEqual(map[string]string{}, map[string]string{}) {
		t.Fatal("an empty manifest is never equal (first pass always runs)")
	}
}

func TestLabelForDedup(t *testing.T) {
	// Same words in a different order → same label (dedup-by-label key).
	if labelFor("uses just as the task runner") != labelFor("the task runner uses just") {
		t.Fatal("word order must not change the label")
	}
	if labelFor("uses just") == labelFor("uses make") {
		t.Fatal("different significant tokens must produce different labels")
	}
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
