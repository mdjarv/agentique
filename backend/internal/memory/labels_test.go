package memory

import "testing"

func TestEvidenceForSource(t *testing.T) {
	cases := map[Source]Evidence{
		SourceHuman:        EvidenceUserStated,
		SourceCapture:      EvidenceObservedOnce,
		SourceAgent:        EvidenceInferred,
		SourceConsolidated: EvidenceInferred,
	}
	for src, want := range cases {
		if got := EvidenceForSource(src); got != want {
			t.Errorf("EvidenceForSource(%s)=%s, want %s", src, got, want)
		}
	}
}

func TestVolatilityForCategory(t *testing.T) {
	cases := map[Category]Volatility{
		CategoryIdentity: VolatilityEvergreen,
		CategoryTask:     VolatilityEphemeral,
		CategoryFact:     VolatilitySlow,
		CategoryGoal:     VolatilitySlow, // only identity/task are special; everything else is slow
	}
	for cat, want := range cases {
		if got := VolatilityForCategory(cat); got != want {
			t.Errorf("VolatilityForCategory(%s)=%s, want %s", cat, got, want)
		}
	}
}

func TestNormalizeLabelsFillsEmpty(t *testing.T) {
	// consolidated, Helped>=2, no ReviewNote → corroborated.
	r := Record{Source: SourceConsolidated, Category: CategoryFact, Helped: 2}
	got := NormalizeLabels(r)
	if got.Evidence != EvidenceCorroborated {
		t.Errorf("Evidence=%s, want corroborated", got.Evidence)
	}
	if got.Volatility != VolatilitySlow {
		t.Errorf("Volatility=%s, want slow", got.Volatility)
	}
	if got.Lifecycle != LifecycleActive {
		t.Errorf("Lifecycle=%s, want active", got.Lifecycle)
	}
	// capture → observed_once.
	if c := NormalizeLabels(Record{Source: SourceCapture, Category: CategoryFact}); c.Evidence != EvidenceObservedOnce {
		t.Errorf("capture Evidence=%s, want observed_once", c.Evidence)
	}
}

func TestNormalizeLabelsPreservesExplicit(t *testing.T) {
	r := Record{
		Source: SourceAgent, Category: CategoryFact,
		Evidence: EvidenceCodeVerified, Volatility: VolatilityEphemeral, Lifecycle: LifecycleArchived,
	}
	got := NormalizeLabels(r)
	if got.Evidence != EvidenceCodeVerified || got.Volatility != VolatilityEphemeral || got.Lifecycle != LifecycleArchived {
		t.Fatalf("explicit labels must not be overwritten: %+v", got)
	}
}

func TestNormalizeLabelsIdempotent(t *testing.T) {
	r := Record{Source: SourceConsolidated, Category: CategoryGoal, Helped: 2}
	once := NormalizeLabels(r)
	twice := NormalizeLabels(once)
	if once.Evidence != twice.Evidence || once.Volatility != twice.Volatility || once.Lifecycle != twice.Lifecycle {
		t.Fatalf("NormalizeLabels not idempotent: %+v vs %+v", once, twice)
	}
}

func TestNewSetsLabels(t *testing.T) {
	r := New(ScopeGlobal, "x", CategoryIdentity, SourceAgent)
	if r.Volatility != VolatilityEvergreen {
		t.Errorf("Volatility=%s, want evergreen", r.Volatility)
	}
	if r.Lifecycle != LifecycleActive {
		t.Errorf("Lifecycle=%s, want active", r.Lifecycle)
	}
	if r.Evidence != EvidenceInferred { // Helped==0, so source-only base (agent → inferred)
		t.Errorf("Evidence=%s, want inferred", r.Evidence)
	}
}

func TestIsArchived(t *testing.T) {
	if !isArchived(Record{Lifecycle: LifecycleArchived}) {
		t.Error("archived record not detected")
	}
	if isArchived(Record{Lifecycle: LifecycleActive}) {
		t.Error("active record flagged archived")
	}
}

func TestIsArchivedExported(t *testing.T) {
	if !IsArchived(Record{Lifecycle: LifecycleArchived}) {
		t.Error("exported IsArchived must match isArchived")
	}
	if IsArchived(Record{Lifecycle: LifecycleActive}) {
		t.Error("exported IsArchived false positive")
	}
}
