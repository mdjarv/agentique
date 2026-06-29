package memory

import (
	"context"
	"testing"
	"time"
)

func TestStorageStrengthRanksEstablishedHigher(t *testing.T) {
	now := time.Now().UTC()
	weak := mk("w", scopeA, "a thinly held fact", CategoryFact, SourceConsolidated)
	weak.UpdatedAt = now

	strong := mk("s", scopeA, "a well established fact", CategoryFact, SourceConsolidated)
	strong.Uses = 12
	strong.DerivedFrom = []string{"x", "y", "z"}
	strong.UpdatedAt = now

	if StorageStrength(strong) <= StorageStrength(weak) {
		t.Fatalf("more uses + provenance should mean more storage: strong=%.3f weak=%.3f",
			StorageStrength(strong), StorageStrength(weak))
	}
	pinned := mk("p", scopeA, "pinned core fact", CategoryFact, SourceConsolidated)
	pinned.Pinned = true
	if StorageStrength(pinned) != 1 {
		t.Fatalf("pinned facts are maximally established, got %.3f", StorageStrength(pinned))
	}
}

func TestRetrievalStrengthDecaysWithDisuse(t *testing.T) {
	now := time.Now().UTC()
	base := mk("a", scopeA, "some fact used at different times", CategoryFact, SourceConsolidated)
	base.Uses = 5
	base.DerivedFrom = []string{"x", "y"}

	fresh := base
	fresh.LastUsedAt = now
	cold := base
	cold.LastUsedAt = now.Add(-90 * 24 * time.Hour) // 3 half-lives

	if RetrievalStrength(fresh, now) <= RetrievalStrength(cold, now) {
		t.Fatalf("a recently-used fact must be more retrievable: fresh=%.3f cold=%.3f",
			RetrievalStrength(fresh, now), RetrievalStrength(cold, now))
	}
	// Storage is unaffected by disuse — only retrieval fades.
	if StorageStrength(fresh) != StorageStrength(cold) {
		t.Fatal("disuse must not change storage strength")
	}
	// ~3 half-lives → ~1/8 of storage.
	got := RetrievalStrength(cold, now)
	want := StorageStrength(cold) * 0.125
	if got < want*0.8 || got > want*1.2 {
		t.Fatalf("retrieval after 3 half-lives ≈ storage/8: got %.4f want ≈ %.4f", got, want)
	}
}

func TestRetrievalFallsBackToUpdatedWhenNeverUsed(t *testing.T) {
	now := time.Now().UTC()
	r := mk("a", scopeA, "never recalled", CategoryFact, SourceConsolidated)
	r.UpdatedAt = now // LastUsedAt zero → falls back to UpdatedAt, so reads as fresh
	if RetrievalStrength(r, now) < StorageStrength(r)-1e-9 {
		t.Fatalf("a never-used but just-edited fact should be ~fully retrievable, got %.3f", RetrievalStrength(r, now))
	}
}

// BumpUses is the retrieval-practice event: it raises Uses and stamps LastUsedAt.
func TestBumpUsesStampsLastUsed(t *testing.T) {
	ctx := context.Background()
	r := mk("a", scopeA, "fact", CategoryFact, SourceConsolidated)
	store := newMemStore(r)
	if err := BumpUses(ctx, store, "a"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ctx, "a")
	if got.Uses != 1 || got.LastUsedAt.IsZero() {
		t.Fatalf("BumpUses should increment Uses and set LastUsedAt, got uses=%d lastUsed=%v", got.Uses, got.LastUsedAt)
	}
}

// StrengthWeighted decay forgets a weakly-held, long-unused fact while sparing a
// strong, recently-used one of the same age-since-edit.
func TestStrengthWeightedDecay(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-100 * 24 * time.Hour)

	weak := mk("weak", scopeA, "weakly held and untouched", CategoryFact, SourceConsolidated)
	weak.UpdatedAt = old
	weak.LastUsedAt = old

	strong := mk("strong", scopeA, "strongly held and recently recalled", CategoryFact, SourceConsolidated)
	strong.Uses = 20
	strong.DerivedFrom = []string{"a", "b", "c", "d"}
	strong.UpdatedAt = old
	strong.LastUsedAt = now // recalled today → protected by disuse measure

	// shouldDecay directly (M5's live path is shouldArchive, which does not read
	// StrengthWeighted; the strength weighting is retained and still covered here).
	policy := DecayPolicy{MaxAge: 60 * 24 * time.Hour, MinUses: 1000, StrengthWeighted: true}
	if !policy.shouldDecay(weak, now) {
		t.Fatal("a weakly-held, long-unused fact should decay under StrengthWeighted")
	}
	if policy.shouldDecay(strong, now) {
		t.Fatal("a strong, recently-recalled fact must survive (disuse-measured age is ~0)")
	}
}

// A confirmed-useful outcome (Helped) builds more storage strength than the same number
// of bare injections (Uses): corroboration counts for more than merely being shown.
func TestStorageStrengthHelpedBeatsBareInjection(t *testing.T) {
	shown := mk("shown", scopeA, "a fact only ever shown", CategoryFact, SourceConsolidated)
	shown.Uses = 1

	helped := mk("helped", scopeA, "a fact confirmed useful", CategoryFact, SourceConsolidated)
	helped.Helped = 1

	if StorageStrength(helped) <= StorageStrength(shown) {
		t.Fatalf("a confirmed-useful fact should be more established than a merely-shown one: helped=%.4f shown=%.4f",
			StorageStrength(helped), StorageStrength(shown))
	}
}
