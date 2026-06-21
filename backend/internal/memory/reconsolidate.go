package memory

import (
	"strings"
	"time"
)

// Reconsolidation (RFC-LD D2): retrieval makes a memory labile — the moment to
// update it. When an agent recalls a fact and finds it contradicted, we don't
// silently delete it; we weaken it and queue it for human review. The complementary
// "strengthen on successful recall" half is BumpUses (storage + retrieval up).

// ContradictedScore is the confidence a fact is knocked down to when flagged as
// contradicted. It sits below AmbiguousScoreThreshold so the fact becomes AMBIGUOUS
// and lands in the confirm/review queue — surfaced for the human to correct or drop,
// never auto-deleted.
const ContradictedScore = 0.4

const (
	// CorroborationCeiling is the highest ConfidenceScore a fact can reach by outcome
	// corroboration alone (positive MemoryUsed acknowledgements). It sits BELOW
	// ScoreGroundTruth (1.0) on purpose: ground truth is asserted by a human (Confirm),
	// not earned by agent corroboration (see brain-outcome-signal.md, RFC-LD #5).
	CorroborationCeiling = 0.95
	// corroborationGapClose is the fraction of the remaining gap to CorroborationCeiling
	// that a single positive outcome closes (0.8 → 0.875 → 0.9125 → …). Asymptotic, so
	// no single MemoryUsed call can jump a fact more than halfway to the ceiling — a
	// guardrail against an agent self-certifying a wrong fact (RFC Non-goals: false memories).
	corroborationGapClose = 0.5
)

// MarkHelped applies the POSITIVE half of reconsolidation (RFC-LD D2): an agent that
// recalled this fact explicitly confirmed it was used/correct (the MemoryUsed tool).
// Unlike a bare injection (BumpUses, "shown"), a confirmed-useful outcome is corroboration:
// it increments Helped, stamps LastUsedAt (it was just used — retrieval recency), and for a
// non-protected fact raises ConfidenceScore toward CorroborationCeiling, closing half the gap
// each time. Protected facts (pinned / locked / human ground truth) keep their score — we never
// let an agent re-rate what a human asserted — but still accrue the Helped count.
//
// Like BumpUses (and unlike MarkContradicted, which surfaces a fact for review), it leaves
// UpdatedAt untouched: a positive outcome is retrieval practice, not a content edit. now stamps
// LastUsedAt.
func MarkHelped(r Record, now time.Time) Record {
	r.Helped++
	r.LastUsedAt = now
	if !isProtected(r) && r.ConfidenceScore < CorroborationCeiling {
		r.ConfidenceScore += corroborationGapClose * (CorroborationCeiling - r.ConfidenceScore)
	}
	return NormalizeConfidence(r)
}

// maxReviewNote bounds the stored reason so an over-eager agent can't write an essay
// into a fact's frontmatter.
const maxReviewNote = 280

// MarkContradicted applies reconsolidation to a fact an agent found wrong on recall:
// it records the reason (ReviewNote) and, for a non-protected fact, weakens its
// confidence into the review band so the confirm UX surfaces it. Protected facts
// (pinned / locked / human ground truth) keep their score — we never silently degrade
// what a human asserted — but still carry the note so the contradiction is visible.
// The fact is never deleted; the human decides. now stamps UpdatedAt.
func MarkContradicted(r Record, reason string, now time.Time) Record {
	reason = strings.TrimSpace(reason)
	if len(reason) > maxReviewNote {
		reason = strings.TrimSpace(reason[:maxReviewNote])
	}
	r.ReviewNote = reason
	r.UpdatedAt = now
	if !isProtected(r) && r.ConfidenceScore > ContradictedScore {
		r.ConfidenceScore = ContradictedScore
	}
	return NormalizeConfidence(r)
}
