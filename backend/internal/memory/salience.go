package memory

import (
	"math"
	"strings"
)

// Outcome-derived salience (RFC-LD D3, brain-salience-gating.md). Where StorageStrength
// (strength.go) answers "how well established" — a monotonic blend of confidence, use and
// provenance — Salience answers the orthogonal, *signed* question "did acting on this pay off
// or backfire?". It rises with corroboration (Helped) and collapses on contradiction
// (ReviewNote), and it is what gates consolidation's keep/forget decision: a corroborated fact
// resists decay and is held back from reorganization, a contradicted one becomes a decay
// candidate. Like the strength signals it is derived from already-persisted fields, never stored.

const (
	// neutralSalience is the salience of a fact with no outcome history — merely *shown*,
	// never judged useful or wrong (Helped == 0, unflagged). It is the pivot:
	// salienceDecayFactor(neutral) == 1.0, so SalienceWeighted decay is a no-op for any fact
	// the outcome loop has not touched. Absence of evidence must not change a fact's life
	// (RFC Non-goals: don't decay aggressively just to be brain-like).
	neutralSalience = 0.5
	// contradictedSalience is the floor a currently-flagged fact (ReviewNote set) collapses
	// to, regardless of how often it was corroborated or shown — a prime decay candidate. Far
	// below neutral so SalienceWeighted decay shortens its life sharply. A Confirm/edit clears
	// the note and lifts the floor with it.
	contradictedSalience = 0.1
	// salienceHelpedGapClose is the fraction of the remaining gap from neutral to 1.0 that a
	// single confirmed-useful outcome (Helped) closes, saturating (0.5 → 0.75 → 0.875 → …). It
	// mirrors corroborationGapClose on confidence (reconsolidate.go) so the two outcome signals
	// move in step.
	salienceHelpedGapClose = 0.5
	// salienceRetentionHelped is the Helped count at or above which an unflagged fact is
	// "strongly corroborated" and retained from reorganization (reorgRetained). Two outcomes —
	// explicit or automatic, since Helped increments on both — so a single acknowledgement is
	// not enough to freeze a fact from consolidation.
	salienceRetentionHelped = 2
)

// Salience returns the outcome-derived salience of a record in [0,1]: a neutral baseline
// (no outcome history), raised by corroboration (Helped, saturating) and collapsed to a low
// floor by contradiction (a set ReviewNote). It is the consolidation keep/forget weight — see
// salienceDecayFactor (decay) and isStronglyCorroborated (reorg retention).
func Salience(r Record) float64 {
	if isContradicted(r) {
		return contradictedSalience
	}
	// Each confirmed-useful outcome closes salienceHelpedGapClose of the remaining gap from
	// neutralSalience up to 1.0 — asymptotic, so corroboration accrues with diminishing returns.
	remaining := (1 - neutralSalience) * math.Pow(1-salienceHelpedGapClose, float64(r.Helped))
	return clamp01(1 - remaining)
}

// isContradicted reports whether a fact is currently flagged as contradicted — the same
// ReviewNote signal MarkContradicted sets and Confirm/edit clears, used elsewhere (graph.go,
// brain.go) to mean "an agent or the session-end judge found this wrong".
func isContradicted(r Record) bool {
	return strings.TrimSpace(r.ReviewNote) != ""
}

// isStronglyCorroborated reports whether a fact has earned protection from consolidation
// churn through outcome: corroborated at least salienceRetentionHelped times and not currently
// contradicted. Such a fact is held back from the reorganizer (reorgRetained), the way a
// human-authored fact is — outcome-proven facts resist being merged or rewritten away.
func isStronglyCorroborated(r Record) bool {
	return r.Helped >= salienceRetentionHelped && !isContradicted(r)
}

// reorgRetained reports whether a record is held back from the model's reorganization
// entirely: the existing protected set (pinned/locked/human — isProtected) plus, RFC-LD D3,
// strongly outcome-corroborated facts. The reorganizer never sees these, so it can neither
// drop nor rewrite them; being out of the reorg set, they are also exempt from decay. Used at
// both the plan and apply reorg-input filters so a plan and its later apply agree.
func reorgRetained(r Record) bool {
	return isProtected(r) || isStronglyCorroborated(r)
}

// salienceDecayFactor scales a fact's effective decay age by outcome (SalienceWeighted): a
// contradicted fact (salience 0.1 → 0.2×) decays far sooner, a neutral fact (0.5 → 1.0×) is
// unchanged, a corroborated one (>0.5 → >1×) resists. The 2× slope places the no-op exactly at
// the neutral baseline so only outcome-touched facts move.
func salienceDecayFactor(r Record) float64 {
	return 2 * Salience(r)
}
