package memory

// Controlled-vocabulary label control plane (brain-evolution Band 1 M6). These are the
// labels the churn and aging branch on — Evidence (trust source), Volatility (decay rate),
// Lifecycle (active/superseded/archived) and typed Relations — as opposed to free-form
// Keywords (recall hints only, no logic branches on them). M6 seeds and round-trips them;
// no churn logic keys off them yet (that is Band 2). M5 reads Volatility/Evidence/Lifecycle
// for computed disuse-confidence aging.

// Evidence is how firmly a fact is grounded — its trust source. The churn re-checks it.
type Evidence string

const (
	// EvidenceUserStated: asserted by a human (Source human). The strongest evidence.
	EvidenceUserStated Evidence = "user_stated"
	// EvidenceCodeVerified: checked against live code (set by the churn's grounding pass).
	EvidenceCodeVerified Evidence = "code_verified"
	// EvidenceCorroborated: independently confirmed (Helped>=2 and not contradicted).
	EvidenceCorroborated Evidence = "corroborated"
	// EvidenceInferred: an ordinary LLM-distilled inference (the default for non-human facts).
	EvidenceInferred Evidence = "inferred"
	// EvidenceObservedOnce: seen a single time and not yet promoted (a raw capture).
	EvidenceObservedOnce Evidence = "observed_once"
)

// Volatility is how quickly a fact goes stale — it keys the disuse half-life (M5).
type Volatility string

const (
	// VolatilityEvergreen never erodes (identity/standing preferences).
	VolatilityEvergreen Volatility = "evergreen"
	// VolatilitySlow erodes on a long half-life (the default for most facts).
	VolatilitySlow Volatility = "slow"
	// VolatilityEphemeral erodes fast (goals/tasks tied to a moment).
	VolatilityEphemeral Volatility = "ephemeral"
)

// Lifecycle is a fact's coarse state. Archived is the cold tier excluded from recall but
// kept on disk and restorable (M5); never auto-deleted.
type Lifecycle string

const (
	LifecycleActive     Lifecycle = "active"
	LifecycleSuperseded Lifecycle = "superseded"
	LifecycleArchived   Lifecycle = "archived"
)

// RelationType is the typed edge kind for the link graph (replaces the untyped Related list;
// Related is retained for back-compat). The churn populates Relations (Band 2).
type RelationType string

const (
	RelationSupersedes   RelationType = "supersedes"
	RelationContradicts  RelationType = "contradicts"
	RelationDuplicates   RelationType = "duplicates"
	RelationGeneralizes  RelationType = "generalizes"
	RelationCorroborates RelationType = "corroborates"
)

// TypedRelation is one typed edge to another record. Replaces the untyped Related ids.
type TypedRelation struct {
	Type   RelationType `json:"type"`
	Target string       `json:"target"`
}

// EvidenceForSource is the default evidence implied by how a fact came to exist: a human
// statement is user_stated, a raw capture is observed_once, everything else is inferred.
func EvidenceForSource(s Source) Evidence {
	switch s {
	case SourceHuman:
		return EvidenceUserStated
	case SourceCapture:
		return EvidenceObservedOnce
	default:
		return EvidenceInferred
	}
}

// VolatilityForCategory is the default volatility implied by a fact's category: identity is
// evergreen, a task is ephemeral, everything else erodes slowly.
func VolatilityForCategory(c Category) Volatility {
	switch c {
	case CategoryIdentity:
		return VolatilityEvergreen
	case CategoryTask:
		return VolatilityEphemeral
	default:
		return VolatilitySlow
	}
}

// withDefaultLabels FILLS EMPTY label fields only — it never overwrites an explicit value
// (idempotent and human-curation-safe), exactly mirroring withDerivedConfidence. An unknown
// non-empty vocabulary value is left intact (forward-compat). The corroborated upgrade is a
// one-shot classification: it reads Helped only while Evidence is still empty (the churn, not
// this normalizer, reclassifies later).
func (r Record) withDefaultLabels() Record {
	if r.Evidence == "" {
		r.Evidence = EvidenceForSource(r.Source)
		if r.Source != SourceHuman && isStronglyCorroborated(r) {
			r.Evidence = EvidenceCorroborated
		}
	}
	if r.Volatility == "" {
		r.Volatility = VolatilityForCategory(r.Category)
	}
	if r.Lifecycle == "" {
		r.Lifecycle = LifecycleActive
	}
	return r
}

// NormalizeLabels fills a record's missing labels with their coherent defaults. Stores call
// it on load (beside NormalizeConfidence) so every record carries labels regardless of when
// it was written. Idempotent.
func NormalizeLabels(r Record) Record { return r.withDefaultLabels() }

// isArchived reports whether a fact is in the cold (archived) tier — excluded from recall and
// every other live consumer, kept on disk, restorable.
func isArchived(r Record) bool { return r.Lifecycle == LifecycleArchived }

// IsArchived is the exported form of isArchived for the brain layer (M5), which cannot reach
// the unexported predicate.
func IsArchived(r Record) bool { return isArchived(r) }
