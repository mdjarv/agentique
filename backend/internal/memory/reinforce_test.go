package memory

import (
	"testing"
	"time"
)

func TestReinforceRaisesConfidenceTowardCeiling(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	r := New(ScopeGlobal, "x", CategoryFact, SourceConsolidated) // inferred, 0.8
	prev := r.ConfidenceScore
	for i := 1; i <= 3; i++ {
		r = Reinforce(r, now)
		if r.ConfidenceScore <= prev {
			t.Fatalf("call %d: score did not increase: %v <= %v", i, r.ConfidenceScore, prev)
		}
		if r.ConfidenceScore > CorroborationCeiling+1e-9 {
			t.Fatalf("call %d: score exceeded ceiling: %v", i, r.ConfidenceScore)
		}
		if r.Corroborations != i {
			t.Fatalf("call %d: Corroborations=%d", i, r.Corroborations)
		}
		if !r.LastUsedAt.Equal(now) {
			t.Fatalf("call %d: LastUsedAt not stamped to now", i)
		}
		prev = r.ConfidenceScore
	}
}

func TestReinforceSparesProtectedScore(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	r := New(ScopeGlobal, "x", CategoryFact, SourceHuman) // extracted, 1.0, protected
	before := r.ConfidenceScore
	r = Reinforce(r, now)
	if r.ConfidenceScore != before {
		t.Fatalf("protected fact score must not change: %v -> %v", before, r.ConfidenceScore)
	}
	if r.Corroborations != 1 {
		t.Fatalf("Corroborations=%d, want 1 (count still accrues)", r.Corroborations)
	}
	if !r.LastUsedAt.Equal(now) {
		t.Fatalf("LastUsedAt not stamped on a protected fact")
	}
}

func TestReinforceRevivesArchived(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	r := New(ScopeGlobal, "a re-observed cold fact", CategoryFact, SourceConsolidated)
	r.Lifecycle = LifecycleArchived
	r.LastUsedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	got := Reinforce(r, now)
	if got.Lifecycle != LifecycleActive {
		t.Fatalf("re-observing an archived fact must revive it: Lifecycle=%s", got.Lifecycle)
	}
	if !got.LastUsedAt.Equal(now) {
		t.Fatalf("revive must restart the disuse clock: LastUsedAt=%v", got.LastUsedAt)
	}
	if got.Corroborations != 1 {
		t.Fatalf("Corroborations=%d, want 1", got.Corroborations)
	}
}

func TestReinforceDoesNotTouchUpdatedAt(t *testing.T) {
	r := New(ScopeGlobal, "x", CategoryFact, SourceConsolidated)
	updated := r.UpdatedAt
	r = Reinforce(r, updated.Add(48*time.Hour))
	if !r.UpdatedAt.Equal(updated) {
		t.Fatalf("Reinforce must leave UpdatedAt untouched: %v -> %v", updated, r.UpdatedAt)
	}
}
