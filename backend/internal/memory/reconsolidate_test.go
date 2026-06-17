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
