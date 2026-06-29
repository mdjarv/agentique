package memory

import (
	"context"
	"testing"
	"time"
)

// agingStale is an absolute far-past instant so a fact built with it is unambiguously
// archived/faded regardless of when the test runs (real-clock paths: shouldArchive in
// ApplyPlan, the fade gate in rank).
var agingStale = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// fadedFact is a durable inferred fact untouched since agingStale (so its effective
// confidence has eroded to the inferred floor 0.30, below the default archive floor 0.35).
func fadedFact(id, text string, vol Volatility) Record {
	r := New(ScopeGlobal, text, CategoryFact, SourceConsolidated)
	r.ID = id
	r.Volatility = vol
	r.LastUsedAt = agingStale
	r.UpdatedAt = agingStale
	return r
}

// --- EffectiveConfidence (deterministic via fixedNow) ---

func effAt(base Record, disuseDays float64) float64 {
	base.LastUsedAt = fixedNow.Add(-time.Duration(disuseDays*24) * time.Hour)
	return EffectiveConfidence(base, fixedNow)
}

func TestEffectiveConfidence_DisuseErosion(t *testing.T) {
	r := New(ScopeGlobal, "x", CategoryFact, SourceConsolidated) // slow, inferred, base 0.8
	at0 := effAt(r, 0)
	at90 := effAt(r, 90)
	at365 := effAt(r, 365)
	if at0 < 0.79 || at0 > 0.81 {
		t.Fatalf("eff@0d=%v, want ~0.8", at0)
	}
	if at90 < 0.39 || at90 > 0.41 {
		t.Fatalf("eff@90d=%v, want ~0.4 (one slow half-life)", at90)
	}
	if !(at90 < at0) {
		t.Fatalf("erosion not monotone: %v !< %v", at90, at0)
	}
	if at365 != floorInferred {
		t.Fatalf("eff@365d=%v, want clamped to floorInferred %v", at365, floorInferred)
	}
}

func TestEffectiveConfidence_EphemeralFadesFasterThanSlow(t *testing.T) {
	slow := New(ScopeGlobal, "x", CategoryFact, SourceConsolidated)
	slow.Volatility = VolatilitySlow
	eph := New(ScopeGlobal, "x", CategoryFact, SourceConsolidated)
	eph.Volatility = VolatilityEphemeral
	if effAt(eph, 10) >= effAt(slow, 10) {
		t.Fatalf("ephemeral should fade faster than slow at equal disuse")
	}
}

func TestEffectiveConfidence_EvergreenNeverErodes(t *testing.T) {
	r := New(ScopeGlobal, "x", CategoryFact, SourceConsolidated)
	r.Volatility = VolatilityEvergreen
	if got := effAt(r, 1000); got != r.ConfidenceScore {
		t.Fatalf("evergreen eroded: %v != base %v", got, r.ConfidenceScore)
	}
}

func TestEffectiveConfidence_HumanProtectedNeverErodes(t *testing.T) {
	r := New(ScopeGlobal, "x", CategoryFact, SourceHuman) // protected, base 1.0
	if got := effAt(r, 1000); got != r.ConfidenceScore {
		t.Fatalf("protected eroded: %v != base %v", got, r.ConfidenceScore)
	}
}

func TestEffectiveConfidence_HelpedRevives(t *testing.T) {
	r := New(ScopeGlobal, "x", CategoryFact, SourceConsolidated)
	r.LastUsedAt = fixedNow.Add(-365 * 24 * time.Hour)
	before := EffectiveConfidence(r, fixedNow)
	revived := MarkHelped(r, fixedNow)
	after := EffectiveConfidence(revived, fixedNow)
	if !(after > before) {
		t.Fatalf("MarkHelped should revive effective confidence: %v !> %v", after, before)
	}
	if after <= DefaultArchiveConfidenceFloor {
		t.Fatalf("revived eff=%v should clear the archive floor %v", after, DefaultArchiveConfidenceFloor)
	}
}

// --- shouldArchive ---

func TestShouldArchive_FadedInferred(t *testing.T) {
	d := DecayPolicy{MaxAge: 24 * time.Hour}
	if !d.shouldArchive(fadedFact("f", "x", VolatilitySlow), time.Now().UTC()) {
		t.Fatal("a faded inferred fact past the hard minimum should archive")
	}
}

func TestShouldArchive_RecentNotArchived(t *testing.T) {
	d := DecayPolicy{MaxAge: 30 * 24 * time.Hour}
	r := New(ScopeGlobal, "x", CategoryFact, SourceConsolidated)
	r.LastUsedAt = time.Now().UTC() // touched just now
	if d.shouldArchive(r, time.Now().UTC()) {
		t.Fatal("a recently-used fact must not archive (disuse < MaxAge)")
	}
}

func TestShouldArchive_EvergreenAndProtectedNever(t *testing.T) {
	now := time.Now().UTC()
	d := DecayPolicy{MaxAge: 24 * time.Hour}
	cases := map[string]Record{
		"evergreen": fadedFact("e", "x", VolatilityEvergreen),
		"human":     func() Record { r := fadedFact("h", "x", VolatilitySlow); r.Source = SourceHuman; return r }(),
		"pinned":    func() Record { r := fadedFact("p", "x", VolatilitySlow); r.Pinned = true; return r }(),
		"locked":    func() Record { r := fadedFact("l", "x", VolatilitySlow); r.Locked = true; return r }(),
	}
	for name, r := range cases {
		if d.shouldArchive(r, now) {
			t.Errorf("%s fact must never archive", name)
		}
	}
}

func TestShouldArchive_AlreadyArchivedIdempotent(t *testing.T) {
	r := fadedFact("a", "x", VolatilitySlow)
	r.Lifecycle = LifecycleArchived
	if (DecayPolicy{MaxAge: 24 * time.Hour}).shouldArchive(r, time.Now().UTC()) {
		t.Fatal("an already-archived fact must not re-archive (idempotent)")
	}
}

func TestShouldArchive_DisabledWhenMaxAgeZero(t *testing.T) {
	if (DecayPolicy{MaxAge: 0}).shouldArchive(fadedFact("f", "x", VolatilitySlow), time.Now().UTC()) {
		t.Fatal("MaxAge==0 disables archiving")
	}
}

// --- Recall: archived exclusion + read-time fade ---

func TestRecall_ExcludesArchived(t *testing.T) {
	arch := rec("arch", "quantum widget calibration manual", CategoryFact)
	arch.Lifecycle = LifecycleArchived
	archPinned := rec("archpin", "archived but pinned identity", CategoryIdentity)
	archPinned.Pinned = true
	archPinned.Lifecycle = LifecycleArchived
	store := newMemStore(arch, archPinned, rec("live", "quantum widget calibration manual", CategoryFact))
	res, err := Recall(context.Background(), store, Query{Text: "quantum widget calibration", K: 5})
	if err != nil {
		t.Fatal(err)
	}
	if contains(res.Recalled, "arch") || contains(res.Pinned, "archpin") {
		t.Fatalf("archived facts must be excluded from recall: recalled=%v pinned=%v", res.Recalled, res.Pinned)
	}
}

func TestRecall_FadedFactDropsWhenArchiveEnabled(t *testing.T) {
	faded := fadedFact("faded", "obscure plasma conduit alignment procedure", VolatilitySlow)
	store := newMemStore(faded)
	res, err := Recall(context.Background(), store, Query{Text: "obscure plasma conduit alignment", K: 3, ArchiveFloor: DefaultArchiveConfidenceFloor})
	if err != nil {
		t.Fatal(err)
	}
	if contains(res.Recalled, "faded") {
		t.Fatalf("a faded fact should drop from recall when archiving is enabled")
	}
	// The fade is COMPUTED, never persisted — the fact is still active on disk.
	got, _ := store.Get(context.Background(), "faded")
	if got.Lifecycle != LifecycleActive {
		t.Fatalf("read-time fade must not persist a lifecycle change: got %q", got.Lifecycle)
	}
}

func TestRecall_NoFadeWhenArchiveDisabled(t *testing.T) {
	faded := fadedFact("faded", "obscure plasma conduit alignment procedure", VolatilitySlow)
	store := newMemStore(faded)
	res, err := Recall(context.Background(), store, Query{Text: "obscure plasma conduit alignment", K: 3}) // ArchiveFloor 0
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res.Recalled, "faded") {
		t.Fatalf("with archiving disabled (floor 0) a faded fact must still recall (cliff defense)")
	}
}

func TestRecall_FreshFactOutranksColdFact(t *testing.T) {
	fresh := rec("fresh", "shared distinctive turbo encabulator keyword", CategoryFact)
	fresh.LastUsedAt = time.Now().UTC()
	cold := rec("cold", "shared distinctive turbo encabulator keyword", CategoryFact)
	cold.LastUsedAt = agingStale
	store := newMemStore(fresh, cold)
	res, err := Recall(context.Background(), store, Query{Text: "shared distinctive turbo encabulator keyword", K: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Recalled) == 0 || res.Recalled[0].ID != "fresh" {
		t.Fatalf("fresh fact should outrank the equally-relevant cold one, got %v", res.Recalled)
	}
}

// --- ApplyPlan: archive not delete ---

func TestApplyPlan_ArchivesNotDeletes(t *testing.T) {
	store := newMemStore(fadedFact("fade", "a long-untouched fact about ledger reconciliation", VolatilitySlow))
	rep, err := Consolidate(context.Background(), store, nil, ScopeGlobal,
		ConsolidateOptions{Decay: DecayPolicy{MaxAge: 24 * time.Hour, ArchiveFloor: DefaultArchiveConfidenceFloor}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decayed) != 1 || rep.Decayed[0].ID != "fade" {
		t.Fatalf("expected 'fade' in Decayed (archived), got %+v", rep.Decayed)
	}
	got, err := store.Get(context.Background(), "fade")
	if err != nil {
		t.Fatalf("archived fact must NOT be deleted: %v", err)
	}
	if got.Lifecycle != LifecycleArchived {
		t.Fatalf("Lifecycle=%s, want archived", got.Lifecycle)
	}
}

func TestApplyPlan_DryRunArchiveWritesNothing(t *testing.T) {
	store := newMemStore(fadedFact("fade", "a long-untouched dry-run fact", VolatilitySlow))
	rep, err := Consolidate(context.Background(), store, nil, ScopeGlobal,
		ConsolidateOptions{DryRun: true, Decay: DecayPolicy{MaxAge: 24 * time.Hour, ArchiveFloor: DefaultArchiveConfidenceFloor}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decayed) != 1 {
		t.Fatalf("dry-run should still report what WOULD archive, got %+v", rep.Decayed)
	}
	got, _ := store.Get(context.Background(), "fade")
	if got.Lifecycle == LifecycleArchived {
		t.Fatal("dry-run must not write the archive transition")
	}
}

func TestApplyPlan_ProtectedAndEvergreenSurviveArchive(t *testing.T) {
	human := fadedFact("h", "human ground truth", VolatilitySlow)
	human.Source = SourceHuman
	ever := fadedFact("e", "evergreen identity", VolatilityEvergreen)
	store := newMemStore(human, ever)
	rep, err := Consolidate(context.Background(), store, nil, ScopeGlobal,
		ConsolidateOptions{Decay: DecayPolicy{MaxAge: 24 * time.Hour, ArchiveFloor: DefaultArchiveConfidenceFloor}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decayed) != 0 {
		t.Fatalf("protected/evergreen must not archive, got %+v", rep.Decayed)
	}
}

func TestApplyPlan_ArchivedExcludedFromDurable(t *testing.T) {
	arch := mk("arch", ScopeGlobal, "the sky is azure today", CategoryFact, SourceConsolidated)
	arch.Lifecycle = LifecycleArchived
	store := newMemStore(arch, capture("cap", "the sky is azure today"))
	ex := fakeExtractor{extract: func(_ []string) []Candidate {
		return []Candidate{{Text: "the sky is azure today", Category: CategoryFact}}
	}}
	rep, err := Consolidate(context.Background(), store, ex, ScopeGlobal, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The archived fact is NOT a dedup target, so the capture promotes a fresh consolidated fact.
	if len(rep.Promoted) != 1 {
		t.Fatalf("capture duplicating an ARCHIVED fact should promote fresh, got %+v", rep.Promoted)
	}
}

// --- Per-consumer exclusion ---

func TestDueForReview_SkipsArchived(t *testing.T) {
	arch := New(ScopeGlobal, "well-established but archived", CategoryFact, SourceConsolidated)
	arch.ID = "arch"
	arch.Uses = 50
	arch.LastUsedAt = agingStale
	arch.Lifecycle = LifecycleArchived
	got := DueForReview([]Record{arch}, time.Now().UTC(), 0)
	for _, r := range got {
		if r.ID == "arch" {
			t.Fatal("DueForReview must skip archived facts")
		}
	}
}

func TestAreas_SkipArchived(t *testing.T) {
	arch := New(ScopeGlobal, "archived fact", CategoryFact, SourceConsolidated)
	arch.ID = "arch"
	arch.Lifecycle = LifecycleArchived
	live := New(ScopeGlobal, "live fact", CategoryFact, SourceConsolidated)
	live.ID = "live"
	got := durableFacts([]Record{arch, live})
	for _, r := range got {
		if r.ID == "arch" {
			t.Fatal("durableFacts (areas) must exclude archived")
		}
	}
}

func TestCommunity_SkipArchived(t *testing.T) {
	ctx := context.Background()
	arch := mk("arch", ScopeGlobal, "archived community fact alpha beta", CategoryFact, SourceConsolidated)
	arch.Lifecycle = LifecycleArchived
	live := mk("live", ScopeGlobal, "live community fact alpha beta", CategoryFact, SourceConsolidated)
	store := newMemStore(arch, live)
	if _, err := AssignCommunities(ctx, store, ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ctx, "arch")
	if got.Community != 0 {
		t.Fatalf("archived fact should not be assigned a community, got %d", got.Community)
	}
}

func TestLink_SkipArchived(t *testing.T) {
	ctx := context.Background()
	arch := mk("arch", ScopeGlobal, "archived linkable fact about widgets", CategoryFact, SourceConsolidated)
	arch.Lifecycle = LifecycleArchived
	live1 := mk("live1", ScopeGlobal, "live linkable fact about widgets", CategoryFact, SourceConsolidated)
	live2 := mk("live2", ScopeGlobal, "another live fact about widgets here", CategoryFact, SourceConsolidated)
	store := newMemStore(arch, live1, live2)
	if _, err := RelinkScope(ctx, store, ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ctx, "arch")
	if len(got.Related) != 0 {
		t.Fatalf("archived fact should not be linked, got %v", got.Related)
	}
}

func TestPromote_SkipsArchived(t *testing.T) {
	arch := mk("arch", Scope("project:a"), "a recurring convention worth promoting", CategoryFact, SourceConsolidated)
	arch.Lifecycle = LifecycleArchived
	if isPromotable(arch) {
		t.Fatal("an archived project fact must not be promotable")
	}
}

func TestGlobalGraph_SkipArchived(t *testing.T) {
	ctx := context.Background()
	arch := mk("arch", Scope("project:a"), "archived scoped fact", CategoryFact, SourceConsolidated)
	arch.Lifecycle = LifecycleArchived
	live := mk("live", Scope("project:a"), "live scoped fact", CategoryFact, SourceConsolidated)
	store := newMemStore(arch, live)
	m, err := ScopeManifest(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	// The manifest fingerprints only promotable (non-archived) facts; archiving 'arch' must
	// not leave it contributing to the scope's manifest.
	store2 := newMemStore(live)
	m2, err := ScopeManifest(ctx, store2)
	if err != nil {
		t.Fatal(err)
	}
	if m[string(Scope("project:a"))] != m2[string(Scope("project:a"))] {
		t.Fatal("ScopeManifest must exclude archived facts (manifest changed by an archived fact)")
	}
}
