package chroma

import (
	"context"
	"errors"
	"fmt"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// Store decorates a base memory.Store with a Chroma-backed semantic index. The
// base store is the source of truth; Chroma is a derived, rebuildable index.
// Reads (Get/List) delegate to the base. Writes go to the base first (and only
// fail if the base fails); the index is then updated best-effort, with errors
// routed to the error handler rather than failing the durable write. Store
// implements memory.Searcher, so memory.Recall automatically uses hybrid ranking
// — and falls back to keyword-only if a Search call errors.
//
// Only durable facts are indexed; episodic captures are not (they are never
// recalled). Scope is written to vector metadata and used as a query-time filter,
// so semantic search is isolated per scope at the source — not post-filtered.
type Store struct {
	base     memory.Store
	client   *Client
	embedder memory.Embedder
	coll     string
	onErr    func(error)
}

var (
	_ memory.Store    = (*Store)(nil)
	_ memory.Searcher = (*Store)(nil)
)

// StoreOption configures a Store.
type StoreOption func(*Store)

// WithErrorHandler sets a callback for best-effort index errors (stale index,
// embedding failures). Without it, such errors are dropped — the durable write in
// the base store still succeeds and the index can be rebuilt with Reindex.
func WithErrorHandler(f func(error)) StoreOption {
	return func(s *Store) {
		if f != nil {
			s.onErr = f
		}
	}
}

// NewStore creates (or opens) the named collection and returns a decorating Store.
func NewStore(ctx context.Context, base memory.Store, client *Client, embedder memory.Embedder, collectionName string, opts ...StoreOption) (*Store, error) {
	if base == nil || client == nil || embedder == nil {
		return nil, errors.New("chroma: base store, client and embedder are required")
	}
	coll, err := client.GetOrCreateCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	s := &Store{base: base, client: client, embedder: embedder, coll: coll, onErr: func(error) {}}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// Get delegates to the base store.
func (s *Store) Get(ctx context.Context, id string) (memory.Record, error) {
	return s.base.Get(ctx, id)
}

// List delegates to the base store.
func (s *Store) List(ctx context.Context, scopes ...memory.Scope) ([]memory.Record, error) {
	return s.base.List(ctx, scopes...)
}

// Put writes to the base store (authoritative) then updates the index best-effort.
func (s *Store) Put(ctx context.Context, r memory.Record) error {
	if err := s.base.Put(ctx, r); err != nil {
		return err
	}
	s.index(ctx, r)
	return nil
}

// Delete removes from the base store then de-indexes best-effort.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.base.Delete(ctx, id); err != nil {
		return err
	}
	if err := s.client.Delete(ctx, s.coll, []string{id}); err != nil {
		s.onErr(fmt.Errorf("chroma: deindex %s: %w", id, err))
	}
	return nil
}

func (s *Store) index(ctx context.Context, r memory.Record) {
	// Captures are never recalled, so keep them out of the vector index. If a
	// record became (or already was) a capture, ensure no stale vector lingers.
	if r.Source == memory.SourceCapture {
		if err := s.client.Delete(ctx, s.coll, []string{r.ID}); err != nil {
			s.onErr(fmt.Errorf("chroma: drop capture vector %s: %w", r.ID, err))
		}
		return
	}
	emb, err := s.embedder.Embed(ctx, []string{r.Text})
	if err != nil || len(emb) == 0 {
		s.onErr(fmt.Errorf("chroma: embed %s: %w", r.ID, err))
		return
	}
	md := map[string]any{
		"scope":    string(r.Scope),
		"category": string(r.Category),
		"source":   string(r.Source),
	}
	if err := s.client.Upsert(ctx, s.coll, []string{r.ID}, emb, []string{r.Text}, []map[string]any{md}); err != nil {
		s.onErr(fmt.Errorf("chroma: index %s: %w", r.ID, err))
	}
}

// Search satisfies memory.Searcher: embed the query, ask Chroma for the nearest
// durable facts in the given scopes, and convert cosine distance to a [0,1] score.
func (s *Store) Search(ctx context.Context, text string, scopes []memory.Scope, k int) ([]memory.Hit, error) {
	emb, err := s.embedder.Embed(ctx, []string{text})
	if err != nil || len(emb) == 0 {
		return nil, fmt.Errorf("chroma: embed query: %w", err)
	}
	hits, err := s.client.Query(ctx, s.coll, emb[0], k, scopeWhere(scopes))
	if err != nil {
		return nil, err
	}
	out := make([]memory.Hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, memory.Hit{ID: h.ID, Score: distanceToScore(h.Distance)})
	}
	return out, nil
}

// Reindex rebuilds the entire collection from the base store. Use after bulk
// hand-edits, an embedder change, or to recover from index drift.
func (s *Store) Reindex(ctx context.Context) error {
	recs, err := s.base.List(ctx)
	if err != nil {
		return err
	}
	var ids, texts []string
	var metas []map[string]any
	for _, r := range recs {
		if r.Source == memory.SourceCapture {
			continue
		}
		ids = append(ids, r.ID)
		texts = append(texts, r.Text)
		metas = append(metas, map[string]any{
			"scope":    string(r.Scope),
			"category": string(r.Category),
			"source":   string(r.Source),
		})
	}
	if len(ids) == 0 {
		return nil
	}
	emb, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return err
	}
	return s.client.Upsert(ctx, s.coll, ids, emb, texts, metas)
}

func scopeWhere(scopes []memory.Scope) map[string]any {
	if len(scopes) == 0 {
		return nil
	}
	vals := make([]string, len(scopes))
	for i, sc := range scopes {
		vals[i] = string(sc)
	}
	return map[string]any{"scope": map[string]any{"$in": vals}}
}

// distanceToScore maps a cosine distance (Chroma, range [0,2]) to a similarity in
// [0,1]. Negative similarities (>90° apart) clamp to 0.
func distanceToScore(d float64) float64 {
	s := 1.0 - d
	if s < 0 {
		return 0
	}
	if s > 1 {
		return 1
	}
	return s
}
