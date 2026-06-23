package memory

import (
	"context"
	"testing"
	"time"
)

// Salience orders facts by OUTCOME: contradicted < neutral < corroborated, monotonic in
// Helped, with a hard floor for a contradicted fact regardless of how often it was corroborated.
func TestSalienceOutcomeOrdering(t *testing.T) {
	neutral := mk("n", scopeA, "merely shown, never judged", CategoryFact, SourceConsolidated)

	helped1 := neutral
	helped1.Helped = 1
	helped3 := neutral
	helped3.Helped = 3

	contradicted := neutral
	contradicted.Helped = 5 // corroborated a lot...
	contradicted.ReviewNote = "user said this is wrong"

	if got := Salience(neutral); got != neutralSalience {
		t.Fatalf("neutral salience = %.3f, want %.3f (the decay-factor pivot)", got, neutralSalience)
	}
	if !(Salience(contradicted) < Salience(neutral)) {
		t.Fatalf("a contradicted fact must be less salient than a neutral one: %.3f vs %.3f",
			Salience(contradicted), Salience(neutral))
	}
	if got := Salience(contradicted); got != contradictedSalience {
		t.Fatalf("contradiction floors salience regardless of Helped: got %.3f want %.3f", got, contradictedSalience)
	}
	if !(Salience(neutral) < Salience(helped1) && Salience(helped1) < Salience(helped3)) {
		t.Fatalf("salience must rise monotonically with Helped: %.3f < %.3f < %.3f?",
			Salience(neutral), Salience(helped1), Salience(helped3))
	}
	// Gap-close shape: each Helped closes half the gap from neutral to 1.0.
	if d := Salience(helped1) - 0.75; d < -1e-9 || d > 1e-9 {
		t.Fatalf("Helped=1 salience should be 0.75 (half the gap), got %.4f", Salience(helped1))
	}
}

// The salience decay factor is a no-op at the neutral baseline, so SalienceWeighted does not
// touch a fact the outcome loop hasn't judged — it refines, it does not accelerate broadly.
func TestSalienceWeightedNeutralIsNoOp(t *testing.T) {
	now := time.Now().UTC()
	r := mk("n", scopeA, "no outcome history", CategoryFact, SourceConsolidated)
	r.UpdatedAt = now.Add(-100 * 24 * time.Hour)

	plain := DecayPolicy{MaxAge: 60 * 24 * time.Hour, MinUses: 1}
	salient := DecayPolicy{MaxAge: 60 * 24 * time.Hour, MinUses: 1, SalienceWeighted: true}

	if plain.effectiveMaxAge(r) != salient.effectiveMaxAge(r) {
		t.Fatalf("neutral fact must have the same effective max age with/without SalienceWeighted: %v vs %v",
			plain.effectiveMaxAge(r), salient.effectiveMaxAge(r))
	}
	if !salient.shouldDecay(r, now) {
		t.Fatal("a stale neutral fact still decays under SalienceWeighted (factor is exactly 1.0)")
	}
}

// SalienceWeighted decay: of three equally-aged facts, the contradicted one decays, the
// corroborated one survives (its threshold is extended past the age), and the neutral one
// sits on the original boundary.
func TestSalienceWeightedDecayContradictedVsCorroborated(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	age := now.Add(-50 * 24 * time.Hour) // 50d old; MaxAge is 60d

	contradicted := mk("bad", scopeA, "an outcome found this wrong", CategoryFact, SourceConsolidated)
	contradicted.UpdatedAt = age
	contradicted.ReviewNote = "contradicted in session"

	corroborated := mk("good", scopeA, "an outcome confirmed this useful", CategoryFact, SourceConsolidated)
	corroborated.UpdatedAt = age
	corroborated.Helped = 1 // factor 1.5 → effective max age 90d > 50d

	store := newMemStore(contradicted, corroborated)
	policy := DecayPolicy{MaxAge: 60 * 24 * time.Hour, MinUses: 1, SalienceWeighted: true}
	rep, err := Consolidate(ctx, store, nil, scopeA, ConsolidateOptions{Decay: policy})
	if err != nil {
		t.Fatal(err)
	}
	decayed := map[string]bool{}
	for _, r := range rep.Decayed {
		decayed[r.ID] = true
	}
	if !decayed["bad"] {
		t.Fatal("a contradicted fact (salience 0.1 → 0.2× max age) should decay at 50d")
	}
	if decayed["good"] {
		t.Fatal("a corroborated fact (salience 0.75 → 1.5× max age) should resist decay at 50d")
	}
	// Without SalienceWeighted, neither decays at 50d (< 60d MaxAge) — proving salience drove it.
	store2 := newMemStore(contradicted, corroborated)
	rep2, err := Consolidate(ctx, store2, nil, scopeA, ConsolidateOptions{Decay: DecayPolicy{MaxAge: 60 * 24 * time.Hour, MinUses: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep2.Decayed) != 0 {
		t.Fatalf("flat decay should prune nothing at 50d < 60d, got %+v", rep2.Decayed)
	}
}

// A contradicted fact decays even when it was injected often (Uses >= MinUses): under salience
// gating, being shown a lot does not redeem a fact later found wrong. The flat MinUses guard
// would have spared it.
func TestSalienceWeightedContradictedBypassesMinUses(t *testing.T) {
	now := time.Now().UTC()
	r := mk("shownbutwrong", scopeA, "injected a lot, then contradicted", CategoryFact, SourceConsolidated)
	r.UpdatedAt = now.Add(-100 * 24 * time.Hour)
	r.Uses = 50 // well above MinUses
	r.ReviewNote = "contradicted"

	salient := DecayPolicy{MaxAge: 60 * 24 * time.Hour, MinUses: 5, SalienceWeighted: true}
	if !salient.shouldDecay(r, now) {
		t.Fatal("a contradicted fact should decay despite Uses >= MinUses under SalienceWeighted")
	}
	// The same policy without salience gating keeps it alive (use-protected).
	flat := DecayPolicy{MaxAge: 60 * 24 * time.Hour, MinUses: 5}
	if flat.shouldDecay(r, now) {
		t.Fatal("without SalienceWeighted, a frequently-injected fact stays use-protected")
	}
}

// A strongly-corroborated fact (Helped >= 2) is retained from the reorganizer: the model never
// sees it, so it cannot drop or rewrite it — even when the model proposes dropping everything else.
func TestStronglyCorroboratedRetainedFromReorg(t *testing.T) {
	ctx := context.Background()
	proven := mk("proven", scopeA, "an outcome-proven convention", CategoryFact, SourceConsolidated)
	proven.Helped = 2
	a := mk("a", scopeA, "ordinary fact a", CategoryFact, SourceConsolidated)
	b := mk("b", scopeA, "ordinary fact b", CategoryFact, SourceConsolidated)
	store := newMemStore(proven, a, b)

	var seen []string
	ex := fakeExtractor{reorganize: func(facts []Fact) []Fact {
		for _, f := range facts {
			seen = append(seen, f.ID)
		}
		return []Fact{{ID: "a", Text: "ordinary fact a", Category: CategoryFact}} // keep a, drop the rest
	}}
	rep, err := Consolidate(ctx, store, ex, scopeA, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range seen {
		if id == "proven" {
			t.Fatal("the reorganizer must not see a strongly-corroborated fact")
		}
	}
	for _, d := range rep.Deleted {
		if d.ID == "proven" {
			t.Fatal("a strongly-corroborated fact must never be deleted by reorganization")
		}
	}
	if _, err := store.Get(ctx, "proven"); err != nil {
		t.Fatal("the proven fact should survive untouched")
	}
	if _, err := store.Get(ctx, "b"); err != ErrNotFound {
		t.Fatal("an ordinary dropped fact (b) should be gone — retention is not blanket")
	}
}

// Retention is earned only while the fact is healthy: a Helped>=2 fact that has since been
// contradicted is NOT retained — it re-enters the reorganizable set (and decay candidacy).
func TestContradictedCorroboratedNotRetained(t *testing.T) {
	good := mk("g", scopeA, "corroborated and healthy", CategoryFact, SourceConsolidated)
	good.Helped = 3
	if !reorgRetained(good) {
		t.Fatal("a healthy Helped>=2 fact should be retained from reorg")
	}
	good.ReviewNote = "later contradicted"
	if reorgRetained(good) {
		t.Fatal("once contradicted, a corroborated fact loses reorg retention")
	}
	// Below the bar: a single corroboration is not enough to freeze a fact.
	once := mk("o", scopeA, "corroborated once", CategoryFact, SourceConsolidated)
	once.Helped = 1
	if reorgRetained(once) {
		t.Fatal("Helped=1 is below salienceRetentionHelped; the fact stays reorganizable")
	}
}
