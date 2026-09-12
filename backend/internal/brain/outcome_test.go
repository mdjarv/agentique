package brain

import (
	"context"
	"math"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// approxEq compares two confidence scores without tripping on float noise.
func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The outcome signal itself survives the removal of the session-end transcript judge
// (docs/assistant.md, "The outcome signal becomes conversational"): an automatic
// confirmation still weighs half an explicit one.
func TestMarkAutoHelpedIsGentlerThanExplicit(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	p1 := ScopeForProject("p1")

	auto, err := s.Add(ctx, p1, "auto fact", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := s.Add(ctx, p1, "explicit fact", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.MarkAutoHelped(ctx, auto.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkHelped(ctx, explicit.ID); err != nil {
		t.Fatal(err)
	}

	gotAuto, _ := s.Get(ctx, auto.ID)
	gotExplicit, _ := s.Get(ctx, explicit.ID)

	if !approxEq(gotAuto.ConfidenceScore, 0.8375) {
		t.Fatalf("auto-helped score = %.4f, want 0.8375", gotAuto.ConfidenceScore)
	}
	if !approxEq(gotExplicit.ConfidenceScore, 0.875) {
		t.Fatalf("explicit-helped score = %.4f, want 0.875", gotExplicit.ConfidenceScore)
	}
	if !(gotAuto.ConfidenceScore < gotExplicit.ConfidenceScore) {
		t.Fatalf("auto (%.4f) must move trust less than explicit (%.4f)", gotAuto.ConfidenceScore, gotExplicit.ConfidenceScore)
	}
	// Both still record a Helped outcome and the recency stamp.
	if gotAuto.Helped != 1 || gotExplicit.Helped != 1 {
		t.Fatalf("both should increment Helped, got auto=%d explicit=%d", gotAuto.Helped, gotExplicit.Helped)
	}
	if gotAuto.LastUsedAt.IsZero() {
		t.Fatalf("auto-helped should stamp LastUsedAt")
	}
}
