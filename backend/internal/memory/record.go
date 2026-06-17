package memory

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Scope is an opaque namespace that isolates memories (e.g. a project, board,
// persona, or "global"). The memory package never interprets it; callers define
// the semantics and pass the active scope(s) at recall time.
type Scope string

// ScopeGlobal is the conventional scope for facts that apply everywhere.
const ScopeGlobal Scope = "global"

// Category classifies a memory. It influences recall ranking (see categoryBoosts)
// but is otherwise opaque to the store.
type Category string

const (
	CategoryFact       Category = "fact"
	CategoryIdentity   Category = "identity"
	CategoryPreference Category = "preference"
	CategoryContact    Category = "contact"
	CategoryProject    Category = "project"
	CategoryGoal       Category = "goal"
	CategoryTask       Category = "task"
)

// Source records how a memory came to exist. It governs trust and lifecycle:
// human/agent/consolidated records are injectable; capture records are raw
// episodic material that only consolidation promotes.
type Source string

const (
	// SourceHuman is hand-authored or hand-edited; treated as ground truth.
	SourceHuman Source = "human"
	// SourceAgent was explicitly remembered by an agent (e.g. a memory_add tool).
	SourceAgent Source = "agent"
	// SourceConsolidated was produced by the consolidation ("sleep") pass.
	SourceConsolidated Source = "consolidated"
	// SourceCapture is raw episodic material staged at turn end; not injected.
	SourceCapture Source = "capture"
)

// Record is a single memory. Text is the source of truth; Embedding is a derived,
// optional cache that may be recomputed from Text at any time.
type Record struct {
	ID       string
	Scope    Scope
	Text     string
	Category Category
	Source   Source

	// Pinned records are always injected and survive decay.
	Pinned bool
	// Locked records are exempt from consolidation rewrite/merge/decay — set this
	// on hand-edited records so the brain does not overwrite a human's correction.
	Locked bool
	// Uses counts how many times this record was injected into a prompt.
	Uses int

	CreatedAt time.Time
	UpdatedAt time.Time

	// DerivedFrom links a consolidated record back to the capture record IDs it
	// was abstracted from, for provenance and reversibility.
	DerivedFrom []string
	// Related links to other record IDs (the [[link]] graph).
	Related []string
	// Community is a derived topic-cluster id within the record's scope (from
	// AssignCommunities / DetectCommunities). Like Embedding and Related it is a
	// rebuildable index, never the source of truth — it powers cluster coloring in
	// the graph view and cluster-aware consolidation. Scope-local: cluster ids are
	// only comparable among records of the same scope.
	Community int

	// Confidence is the coarse trust tier (EXTRACTED / INFERRED / AMBIGUOUS) and
	// ConfidenceScore the finer 0..1 signal behind it (RFC P2). The score is
	// canonical; the tier is always derived from (Source, score) — see confidence.go.
	// Confidence sharpens decay and powers the "confirm what I'm unsure about" UX.
	Confidence      ConfidenceTier
	ConfidenceScore float64

	// Embedding is an optional derived vector cache; never the source of truth.
	Embedding []float32
}

// New constructs a Record, stamping a fresh ID and timestamps and the confidence
// tier/score implied by its Source (the RFC P2 mapping). Text is trimmed.
func New(scope Scope, text string, category Category, source Source) Record {
	now := time.Now().UTC()
	tier, score := ConfidenceForSource(source)
	return Record{
		ID:              uuid.NewString(),
		Scope:           scope,
		Text:            strings.TrimSpace(text),
		Category:        category,
		Source:          source,
		Confidence:      tier,
		ConfidenceScore: score,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}
