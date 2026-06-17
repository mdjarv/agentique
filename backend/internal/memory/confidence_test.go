package memory

import (
	"testing"
	"time"
)

func TestConfidenceForSource(t *testing.T) {
	cases := []struct {
		src       Source
		wantTier  ConfidenceTier
		wantScore float64
	}{
		{SourceHuman, ConfidenceExtracted, ScoreGroundTruth},
		{SourceAgent, ConfidenceInferred, DefaultInferredScore},
		{SourceConsolidated, ConfidenceInferred, DefaultInferredScore},
		{SourceCapture, ConfidenceInferred, DefaultInferredScore},
	}
	for _, c := range cases {
		tier, score := ConfidenceForSource(c.src)
		if tier != c.wantTier || score != c.wantScore {
			t.Errorf("ConfidenceForSource(%s) = (%s,%v), want (%s,%v)", c.src, tier, score, c.wantTier, c.wantScore)
		}
	}
}

func TestTierForScore(t *testing.T) {
	// Human is always EXTRACTED regardless of score.
	if got := TierForScore(SourceHuman, 0.1); got != ConfidenceExtracted {
		t.Errorf("human@0.1 tier = %s, want extracted", got)
	}
	// Inferred sources cross into AMBIGUOUS below the threshold.
	if got := TierForScore(SourceConsolidated, AmbiguousScoreThreshold-0.01); got != ConfidenceAmbiguous {
		t.Errorf("below threshold tier = %s, want ambiguous", got)
	}
	if got := TierForScore(SourceConsolidated, AmbiguousScoreThreshold); got != ConfidenceInferred {
		t.Errorf("at threshold tier = %s, want inferred", got)
	}
	if got := TierForScore(SourceConsolidated, DefaultInferredScore); got != ConfidenceInferred {
		t.Errorf("default score tier = %s, want inferred", got)
	}
}

func TestNewStampsConfidence(t *testing.T) {
	h := New(ScopeGlobal, "a fact", CategoryFact, SourceHuman)
	if h.Confidence != ConfidenceExtracted || h.ConfidenceScore != ScoreGroundTruth {
		t.Errorf("human New = (%s,%v), want extracted/ground-truth", h.Confidence, h.ConfidenceScore)
	}
	c := New(ScopeGlobal, "a fact", CategoryFact, SourceConsolidated)
	if c.Confidence != ConfidenceInferred || c.ConfidenceScore != DefaultInferredScore {
		t.Errorf("consolidated New = (%s,%v), want inferred/default", c.Confidence, c.ConfidenceScore)
	}
}

func TestNormalizeConfidenceBackfill(t *testing.T) {
	// A pre-P2 record: no confidence set. Backfill must derive from Source.
	pre := Record{ID: "x", Source: SourceConsolidated}
	got := NormalizeConfidence(pre)
	if got.Confidence != ConfidenceInferred || got.ConfidenceScore != DefaultInferredScore {
		t.Errorf("backfill consolidated = (%s,%v), want inferred/default", got.Confidence, got.ConfidenceScore)
	}
	preHuman := Record{ID: "y", Source: SourceHuman}
	gotH := NormalizeConfidence(preHuman)
	if gotH.Confidence != ConfidenceExtracted || gotH.ConfidenceScore != ScoreGroundTruth {
		t.Errorf("backfill human = (%s,%v), want extracted/ground-truth", gotH.Confidence, gotH.ConfidenceScore)
	}
	// A stored score with a drifted tier is reconciled to the score, not left as-is.
	drift := Record{ID: "z", Source: SourceConsolidated, Confidence: ConfidenceInferred, ConfidenceScore: 0.4}
	gotD := NormalizeConfidence(drift)
	if gotD.Confidence != ConfidenceAmbiguous {
		t.Errorf("drifted tier = %s, want ambiguous (reconciled from 0.4)", gotD.Confidence)
	}
}

func TestDecayConfidenceWeighted(t *testing.T) {
	now := time.Now().UTC()
	// Two facts equally stale (1.5× a 100-unit MaxAge would be needed at full score).
	age := 70 * time.Minute
	maxAge := 100 * time.Minute
	low := Record{Source: SourceConsolidated, ConfidenceScore: 0.5, UpdatedAt: now.Add(-age)}   // eff = 50m → stale
	high := Record{Source: SourceConsolidated, ConfidenceScore: 0.95, UpdatedAt: now.Add(-age)} // eff = 95m → fresh

	weighted := DecayPolicy{MaxAge: maxAge, MinUses: 1, ConfidenceWeighted: true}
	if !(now.Sub(low.UpdatedAt) > weighted.effectiveMaxAge(low)) {
		t.Error("low-confidence fact should be past its (scaled-down) max age")
	}
	if now.Sub(high.UpdatedAt) > weighted.effectiveMaxAge(high) {
		t.Error("high-confidence fact should still be within its max age")
	}

	// Without weighting, both share the same (full) threshold and neither is stale.
	flat := DecayPolicy{MaxAge: maxAge, MinUses: 1}
	if now.Sub(low.UpdatedAt) > flat.effectiveMaxAge(low) {
		t.Error("unweighted policy should not scale max age")
	}
}
