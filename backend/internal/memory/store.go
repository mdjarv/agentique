package memory

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Store.Get when no record matches the given ID.
var ErrNotFound = errors.New("memory: record not found")

// Store is the persistence contract for memories. Implementations must treat Text
// as the source of truth and may keep embeddings out of the canonical
// representation entirely. Put inserts or replaces by ID; Delete on a missing ID
// is not an error.
type Store interface {
	Put(ctx context.Context, r Record) error
	Get(ctx context.Context, id string) (Record, error)
	Delete(ctx context.Context, id string) error
	// List returns all records in the given scopes; with no scopes, all records.
	List(ctx context.Context, scopes ...Scope) ([]Record, error)
}

// Query parameters for Recall.
type Query struct {
	Text   string
	Scopes []Scope // scopes to search; empty = all scopes
	K      int     // max non-pinned results; <= 0 uses DefaultRecallK
	// VectorVetoScore is the hybrid-mode floor below which a vector-scored candidate is
	// dropped regardless of keyword overlap: when the embedder scores a candidate as
	// semantically unrelated to the query, that verdict vetoes an incidental keyword
	// match (brain-semantic-recall.md, priority #1). <= 0 uses DefaultVectorVetoScore.
	// It is MODEL-SPECIFIC (cosine distributions differ per embedder) — calibrate it
	// alongside the cosine link threshold. Only consulted when a Searcher is present.
	VectorVetoScore float64
	// VectorVouchScore is the vector score at/above which the embedder is trusted to
	// OVERRIDE the lexical lone-token guard (singleTokenMinShare) — i.e. the cosine
	// "related" line (the brain passes its cosThresh here). A merely-meaningful cosine is
	// not enough: a compressed-distribution model (all-MiniLM scores ~0.35 even for
	// unrelated pairs) would otherwise vouch for everything. <= 0 uses
	// DefaultSemanticThreshold. Only consulted when a Searcher is present.
	VectorVouchScore float64
}

// Result splits recall output the way it is injected: pinned facts are always
// included, Recalled holds the top query-relevant non-pinned facts.
type Result struct {
	Pinned   []Record
	Recalled []Record
}

// BumpUses increments the Uses counter of the given records and stamps LastUsedAt,
// persisting them. Call after the records have actually been injected into a prompt:
// this is the "retrieval practice" event (RFC-LD D1/D2) — it raises both storage
// (cumulative Uses) and retrieval (recency) strength. Missing IDs are skipped.
// UpdatedAt is intentionally left untouched so a uses bump does not look like a
// content edit; LastUsedAt is the separate recall timestamp.
func BumpUses(ctx context.Context, store Store, ids ...string) error {
	now := time.Now().UTC()
	for _, id := range ids {
		r, err := store.Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return err
		}
		r.Uses++
		r.LastUsedAt = now
		if err := store.Put(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
