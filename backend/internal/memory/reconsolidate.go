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
