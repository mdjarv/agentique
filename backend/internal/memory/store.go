package memory

import (
	"context"
	"errors"
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
}

// Result splits recall output the way it is injected: pinned facts are always
// included, Recalled holds the top query-relevant non-pinned facts.
type Result struct {
	Pinned   []Record
	Recalled []Record
}

// BumpUses increments the Uses counter of the given records and persists them.
// Call after the records have actually been injected into a prompt. Missing IDs
// are skipped. UpdatedAt is intentionally left untouched so a uses bump does not
// look like a content edit.
func BumpUses(ctx context.Context, store Store, ids ...string) error {
	for _, id := range ids {
		r, err := store.Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return err
		}
		r.Uses++
		if err := store.Put(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
