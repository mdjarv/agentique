package memory

// Confidence tiers (RFC P2, graphify's rubric). A fact carries both a coarse tier
// (the human-facing bucket, for badges and filtering) and a finer numeric score in
// ConfidenceScore. The score is canonical: the tier is always derived from
// (Source, score) via TierForScore, so the two can never drift. Confidence drives
// two things: a sharper decay signal (low-confidence facts decay sooner — see
// DecayPolicy.ConfidenceWeighted) and the "confirm what I'm unsure about" UX (the
// brain surfaces its least-trusted facts for the user to confirm or drop).
type ConfidenceTier string

const (
	// ConfidenceExtracted is a fact stated directly by a trusted source — anything
	// hand-authored or hand-edited (SourceHuman). Treated as ground truth: it is
	// already exempt from consolidation rewrite and decay (isProtected), and is never
	// surfaced for confirmation.
	ConfidenceExtracted ConfidenceTier = "extracted"
	// ConfidenceInferred is an LLM-distilled fact: a capture extraction, a reorganize
	// abstraction, an agent's MemoryAdd, or a cross-project generalization. Trusted
	// enough to inject, but not ground truth — a candidate for confirmation when its
	// score is low.
	ConfidenceInferred ConfidenceTier = "inferred"
	// ConfidenceAmbiguous is a fact the brain is unsure about: an inferred fact whose
	// score has fallen below AmbiguousScoreThreshold. The prime target for the
	// "confirm X?" UX and for the fastest decay.
	ConfidenceAmbiguous ConfidenceTier = "ambiguous"
)

const (
	// ScoreGroundTruth is the score of a human-authored/edited fact. It sits above
	// the 0.55–0.95 inferred band deliberately: ground truth is not "a very confident
	// inference", it is asserted.
	ScoreGroundTruth = 1.0
	// DefaultInferredScore is the score of an ordinary LLM-distilled fact (capture
	// promotion, reorganize abstraction, agent MemoryAdd).
	DefaultInferredScore = 0.8
	// CrossProjectInferredScore is the score of a fact generalized to global from
	// per-project facts. A generalization is a riskier inference than a directly
	// distilled fact, so it starts lower — landing it near the bottom of the inferred
	// band where the confirm UX will surface it.
	CrossProjectInferredScore = 0.65
	// AmbiguousScoreThreshold is the score below which an inferred fact is classed
	// AMBIGUOUS. It is the bottom of graphify's 0.55–0.95 inferred band.
	AmbiguousScoreThreshold = 0.55
	// NeedsConfirmationScore is the upper bound (inclusive) of the "confirm what I'm
	// unsure about" band: non-protected facts at or below this score are the ones the
	// brain offers up for confirmation. Set at the cross-project score so freshly
	// generalized global facts are surfaced, while ordinary inferred facts (0.8) are
	// not nagged about.
	NeedsConfirmationScore = CrossProjectInferredScore
	// ActOnConfidence is the confidence at or above which a preference graduates from
	// soft "background context" into an acted-on operating contract (brain-outcome-signal.md).
	// It sits ABOVE DefaultInferredScore (0.8) deliberately: a freshly inferred preference
	// must EARN the authority to drive behavior — by human Confirm (→1.0) or by outcome
	// corroboration (MemoryUsed raising it past this gate) — before it becomes a standing
	// instruction. Below it, a preference stays advisory and in the confirm queue.
	ActOnConfidence = 0.85
)

// ConfidenceForSource returns the tier and score a freshly created record should
// carry given how it came to exist (the RFC P2 mapping): human-authored facts are
// ground truth (EXTRACTED), everything else is an LLM-distilled inference. Callers
// that know they are generalizing across projects override the score afterwards
// (see ApplyGlobalPromotion → CrossProjectInferredScore).
func ConfidenceForSource(source Source) (ConfidenceTier, float64) {
	if source == SourceHuman {
		return ConfidenceExtracted, ScoreGroundTruth
	}
	return ConfidenceInferred, DefaultInferredScore
}

// TierForScore derives the tier from a record's source and numeric score so the two
// stay consistent regardless of how the score was set (creation, cross-project
// override, hand-edit, future erosion). Human facts are always EXTRACTED; otherwise
// a score below AmbiguousScoreThreshold is AMBIGUOUS and anything higher is INFERRED.
func TierForScore(source Source, score float64) ConfidenceTier {
	if source == SourceHuman {
		return ConfidenceExtracted
	}
	if score < AmbiguousScoreThreshold {
		return ConfidenceAmbiguous
	}
	return ConfidenceInferred
}

// withDerivedConfidence fills in a record's confidence when it is missing (a fact
// that predates P2 — RFC open-decision #4) and always recomputes the tier from the
// score so the two fields can't drift. The backfill is sourced from Source, which
// already encodes the exact provenance the P2 mapping keys on, so it is lossless and
// needs no migration pass: the derived value is persisted on the record's next
// write. A zero score means "unset"; a real score is never zero (the lowest assigned
// value is well above 0).
func (r Record) withDerivedConfidence() Record {
	if r.ConfidenceScore <= 0 {
		_, r.ConfidenceScore = ConfidenceForSource(r.Source)
	}
	r.Confidence = TierForScore(r.Source, r.ConfidenceScore)
	return r
}

// NormalizeConfidence backfills and reconciles a record's confidence (see
// withDerivedConfidence). Stores call it on load so every record handed to the rest
// of the system carries a coherent (tier, score) pair regardless of when it was
// written.
func NormalizeConfidence(r Record) Record { return r.withDerivedConfidence() }
