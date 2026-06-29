package memory

import (
	"math"
	"time"
)

// Computed disuse-confidence aging (brain-evolution Band 1 M5, RFC-LD D1). Confidence is a
// living scalar: the stored ConfidenceScore eroded by time-since-last-use on a volatility-keyed
// half-life, clamped up to an evidence floor. It is computed at recall and NEVER written on a
// nudge — state is persisted exactly once, at the archive transition (Lifecycle=archived: a cold
// tier excluded from recall, kept on disk, restorable — never auto-deleted). Human/pinned/locked/
// evergreen never erode and never archive. Reads M6's Volatility/Evidence/Lifecycle labels
// directly (no Category/Source fallbacks — M6 is a hard predecessor).
const (
	// halfLifeSlowDays is the disuse half-life for an ordinary fact (volatility=slow/unset).
	halfLifeSlowDays = 90.0
	// halfLifeEphemeralDays is the (shorter) half-life for a fast-fading fact (ephemeral).
	halfLifeEphemeralDays = 14.0

	// DefaultArchiveConfidenceFloor is the effective-confidence line below which a faded fact is
	// archived when DecayPolicy.ArchiveFloor is unset.
	DefaultArchiveConfidenceFloor = 0.35

	// Evidence floors: effective confidence never erodes below these. Trusted evidence floors
	// ABOVE the archive line (so those facts never archive); ordinary inference floors below it
	// (so it can fade out); observed-once floors lowest.
	floorTrusted  = 0.50 // user_stated / code_verified / corroborated
	floorInferred = 0.30 // ordinary inference / unset
	floorObserved = 0.15 // observed_once
)

// EffectiveConfidence returns the stored ConfidenceScore eroded by time-since-last-use via the
// volatility half-life, clamped up to the evidence floor. Protected/evergreen facts return the
// stored score unchanged. It is a PURE READ — it never writes. now is injected so callers share
// one clock and the result is testable.
func EffectiveConfidence(r Record, now time.Time) float64 {
	base := NormalizeConfidence(r).ConfidenceScore
	if base <= 0 || base > 1 {
		base = DefaultInferredScore
	}
	if isProtected(r) || isEvergreen(r) {
		return base
	}
	days := now.Sub(lastSeen(r)).Hours() / 24
	if days < 0 {
		days = 0
	}
	eff := base * math.Pow(0.5, days/volatilityHalfLifeDays(r))
	if fl := evidenceFloor(r); eff < fl {
		eff = fl
	}
	return clamp01(eff)
}

// shouldArchive is the archive-transition test, expressed as a DecayPolicy method so the churn's
// decay block can call it like the old shouldDecay. MaxAge is reused as a HARD minimum disuse age
// (and the on/off switch when 0). Idempotent (an already-archived fact returns false);
// protected/evergreen never archive.
func (d DecayPolicy) shouldArchive(r Record, now time.Time) bool {
	if d.MaxAge <= 0 {
		return false
	}
	if isProtected(r) || isEvergreen(r) || isArchived(r) {
		return false
	}
	if now.Sub(lastSeen(r)) < d.MaxAge {
		return false
	}
	floor := d.ArchiveFloor
	if floor <= 0 {
		floor = DefaultArchiveConfidenceFloor
	}
	return EffectiveConfidence(r, now) <= floor
}

// isEvergreen reports whether a fact never erodes (volatility=evergreen, M6 label).
func isEvergreen(r Record) bool { return r.Volatility == VolatilityEvergreen }

// volatilityHalfLifeDays maps a fact's volatility label to its disuse half-life in days
// (evergreen is infinite — it never erodes).
func volatilityHalfLifeDays(r Record) float64 {
	switch r.Volatility {
	case VolatilityEphemeral:
		return halfLifeEphemeralDays
	case VolatilityEvergreen:
		return math.Inf(1)
	default:
		return halfLifeSlowDays // slow / unset
	}
}

// evidenceFloor maps a fact's evidence label to the lowest effective confidence it can erode to.
func evidenceFloor(r Record) float64 {
	switch r.Evidence {
	case EvidenceUserStated, EvidenceCodeVerified, EvidenceCorroborated:
		return floorTrusted
	case EvidenceObservedOnce:
		return floorObserved
	default:
		return floorInferred // inferred / unset
	}
}
