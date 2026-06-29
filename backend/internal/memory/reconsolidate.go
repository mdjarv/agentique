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
	// AutoCorroborationGapClose is the gentler gap-close for an AUTOMATICALLY-inferred
	// positive outcome (the session-end transcript judge, brain-outcome-signal.md "Automatic
	// outcome emitter"). It is deliberately HALF the explicit weight: an agent that calls
	// MemoryUsed, or a human Confirm, is firsthand testimony ("I was there, it helped"); a
	// judge reading a finished transcript is a weaker, secondhand inference. A machine
	// inference therefore moves trust less per outcome (0.8 → 0.8375 → 0.866 → …), so it
	// takes more corroborations to graduate a preference into the operating contract.
	AutoCorroborationGapClose = 0.25
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
	return MarkHelpedWith(r, now, corroborationGapClose)
}

// MarkHelpedWith is MarkHelped parameterized by the gap-close fraction, so a weaker,
// automatically-inferred outcome can move confidence less than an explicit one. It
// always increments Helped and stamps LastUsedAt (the fact WAS surfaced and judged
// useful — retrieval recency) regardless of weight; only the confidence step scales
// with gapClose. The ceiling, protected-fact exemption, and "never rewrite text"
// guarantees of MarkHelped are unchanged. Callers: MarkHelped (explicit, 0.5) and the
// auto emitter (AutoCorroborationGapClose, 0.25). gapClose is clamped to [0,1].
func MarkHelpedWith(r Record, now time.Time, gapClose float64) Record {
	if gapClose < 0 {
		gapClose = 0
	} else if gapClose > 1 {
		gapClose = 1
	}
	r.Helped++
	r.LastUsedAt = now
	if !isProtected(r) && r.ConfidenceScore < CorroborationCeiling {
		r.ConfidenceScore += gapClose * (CorroborationCeiling - r.ConfidenceScore)
	}
	return NormalizeConfidence(r)
}

// Reinforce records that the same fact was independently OBSERVED AGAIN — a re-observation
// at ingest that duplicates a DURABLE memory (the third reconsolidation verb, beside
// MarkHelped/MarkContradicted). It increments Corroborations, stamps LastUsedAt (driving
// retrieval recency + decay-by-disuse), and for a non-protected fact raises ConfidenceScore
// toward CorroborationCeiling by AutoCorroborationGapClose of the remaining gap — a dup match
// is a machine inference, so it earns the gentle automatic weight, not firsthand 0.5. Protected
// facts (pinned/locked/human ground truth) keep their score but still accrue the count. Like
// MarkHelped (and unlike MarkContradicted), it leaves UpdatedAt untouched: re-observation is
// retrieval, not a content edit. now stamps LastUsedAt.
func Reinforce(r Record, now time.Time) Record {
	r.Corroborations++
	r.LastUsedAt = now
	if !isProtected(r) && r.ConfidenceScore < CorroborationCeiling {
		r.ConfidenceScore += AutoCorroborationGapClose * (CorroborationCeiling - r.ConfidenceScore)
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
