package memory

import (
	"strings"
	"testing"
	"time"
)

// A contradicted non-protected fact is weakened into the AMBIGUOUS review band and
// keeps the reason; it is never deleted.
func TestMarkContradictedWeakensNonProtected(t *testing.T) {
	now := time.Now().UTC()
	r := mk("a", scopeA, "the build uses make", CategoryFact, SourceConsolidated) // 0.8 inferred
	got := MarkContradicted(r, "actually it uses just now", now)

	if got.ConfidenceScore > AmbiguousScoreThreshold {
		t.Fatalf("a contradicted fact should drop into the review band, score=%.2f", got.ConfidenceScore)
	}
	if got.Confidence != ConfidenceAmbiguous {
		t.Fatalf("expected AMBIGUOUS tier, got %q", got.Confidence)
	}
	if !strings.Contains(got.ReviewNote, "just") {
		t.Fatalf("reason should be recorded, got %q", got.ReviewNote)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Fatal("UpdatedAt should advance on reconsolidation")
	}
}

// Protected facts (human ground truth, locked, pinned) keep their score but still
// carry the note — we surface the contradiction without silently degrading truth.
func TestMarkContradictedSparesProtectedScore(t *testing.T) {
	now := time.Now().UTC()
	human := mk("h", scopeA, "the user prefers tabs", CategoryPreference, SourceHuman) // ground truth 1.0
	got := MarkContradicted(human, "session used spaces", now)
	if got.ConfidenceScore < ScoreGroundTruth-1e-9 {
		t.Fatalf("human ground truth must keep its score, got %.2f", got.ConfidenceScore)
	}
	if got.ReviewNote == "" {
		t.Fatal("a protected fact should still carry the review note")
	}
}

func TestMarkContradictedTruncatesReason(t *testing.T) {
	long := strings.Repeat("x", maxReviewNote+50)
	got := MarkContradicted(mk("a", scopeA, "f", CategoryFact, SourceConsolidated), long, time.Now().UTC())
	if len(got.ReviewNote) > maxReviewNote {
		t.Fatalf("reason should be bounded to %d, got %d", maxReviewNote, len(got.ReviewNote))
	}
}

// A positive outcome (MemoryUsed) increments Helped, refreshes recency, and raises a
// non-protected fact's confidence by closing half the gap to the ceiling each time —
// asymptotically, never reaching ground truth.
func TestMarkHelpedRaisesConfidenceTowardCeiling(t *testing.T) {
	now := time.Now().UTC()
	r := mk("a", scopeA, "an inferred convention", CategoryFact, SourceConsolidated) // 0.8 inferred
	prev := r.ConfidenceScore

	for i := 0; i < 6; i++ {
		r = MarkHelped(r, now)
		if r.ConfidenceScore <= prev {
			t.Fatalf("outcome %d should raise confidence: %.4f !> %.4f", i, r.ConfidenceScore, prev)
		}
		if r.ConfidenceScore > CorroborationCeiling+1e-9 {
			t.Fatalf("confidence must never exceed the corroboration ceiling, got %.4f", r.ConfidenceScore)
		}
		if r.ConfidenceScore >= ScoreGroundTruth {
			t.Fatalf("corroboration must never reach ground truth, got %.4f", r.ConfidenceScore)
		}
		prev = r.ConfidenceScore
	}
	if r.Helped != 6 {
		t.Fatalf("Helped should count each outcome, got %d", r.Helped)
	}
	if !r.LastUsedAt.Equal(now) {
		t.Fatal("a positive outcome is a use — LastUsedAt should be stamped")
	}
	// One outcome must be enough to graduate a default-inferred fact past the act-on gate
	// (the keystone property for the operating contract).
	one := MarkHelped(mk("b", scopeA, "x", CategoryPreference, SourceConsolidated), now)
	if one.ConfidenceScore < ActOnConfidence {
		t.Fatalf("one positive outcome should lift a 0.8 inferred pref past ActOnConfidence (%.2f), got %.4f",
			ActOnConfidence, one.ConfidenceScore)
	}
}

// MarkHelped does not look like a content edit (UpdatedAt untouched) — it is retrieval
// practice, kin to BumpUses, not a human edit.
func TestMarkHelpedDoesNotTouchUpdatedAt(t *testing.T) {
	edited := time.Now().UTC().Add(-time.Hour)
	r := mk("a", scopeA, "f", CategoryFact, SourceConsolidated)
	r.UpdatedAt = edited
	got := MarkHelped(r, time.Now().UTC())
	if !got.UpdatedAt.Equal(edited) {
		t.Fatalf("UpdatedAt should be left untouched by an outcome, got %v want %v", got.UpdatedAt, edited)
	}
}

// Protected facts (human ground truth) keep their score — an agent can't re-rate what a
// human asserted — but still accrue the Helped count.
func TestMarkHelpedSparesProtectedScore(t *testing.T) {
	now := time.Now().UTC()
	human := mk("h", scopeA, "the user prefers tabs", CategoryPreference, SourceHuman) // 1.0
	got := MarkHelped(human, now)
	if got.ConfidenceScore < ScoreGroundTruth-1e-9 || got.ConfidenceScore > ScoreGroundTruth+1e-9 {
		t.Fatalf("human ground truth must keep its score, got %.4f", got.ConfidenceScore)
	}
	if got.Helped != 1 {
		t.Fatalf("a protected fact should still count the outcome, got Helped=%d", got.Helped)
	}
}

// The loop is bidirectional: a fact talked up by outcomes is still knocked down by a
// later contradiction (no runaway self-certification).
func TestMarkHelpedThenContradictedReverses(t *testing.T) {
	now := time.Now().UTC()
	r := mk("a", scopeA, "an inferred convention", CategoryFact, SourceConsolidated)
	r = MarkHelped(MarkHelped(r, now), now) // corroborated up
	if r.ConfidenceScore < ActOnConfidence {
		t.Fatalf("precondition: should be corroborated above the gate, got %.4f", r.ConfidenceScore)
	}
	r = MarkContradicted(r, "turned out wrong", now)
	if r.ConfidenceScore > AmbiguousScoreThreshold {
		t.Fatalf("a contradiction must still knock a corroborated fact into review, got %.4f", r.ConfidenceScore)
	}
}
