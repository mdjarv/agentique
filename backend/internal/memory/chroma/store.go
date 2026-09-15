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

// metadataFor builds the Chroma metadata for a record, shared by index() and Reindex() so
// the two never drift. It normalizes labels first (M6) so an un-labeled input never writes
// an empty-string volatility/lifecycle. All values are strings (Chroma v2 accepts string
// metadata), enabling future filtered recall by volatility/lifecycle.
func metadataFor(r memory.Record) map[string]any {
	r = memory.NormalizeLabels(r)
	return map[string]any{
		"scope":      string(r.Scope),
		"category":   string(r.Category),
		"source":     string(r.Source),
		"volatility": string(r.Volatility),
		"lifecycle":  string(r.Lifecycle),
	}
}

func (s *Store) index(ctx context.Context, r memory.Record) {
	// Captures are never recalled, so keep them out of the vector index. If a
	// record became (or already was) a capture, ensure no stale vector lingers.
	if r.Source.Staged() {
		if err := s.client.Delete(ctx, s.coll, []string{r.ID}); err != nil {
			s.onErr(fmt.Errorf("chroma: drop capture vector %s: %w", r.ID, err))
		}
		return
	}
	emb, err := s.embedder.Embed(ctx, []string{r.Text})
	if err != nil {
		s.onErr(fmt.Errorf("chroma: embed %s: %w", r.ID, err))
		return
	}
	if len(emb) == 0 {
		s.onErr(fmt.Errorf("chroma: embed %s: embedder returned no vector", r.ID))
		return
	}
	md := metadataFor(r)
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

// LoadVectors returns every indexed (document, embedding) pair so a caller can warm a
// text-keyed embedding cache after a restart instead of re-embedding the corpus. It reads only
// the derived vectors Chroma already holds — the base store stays the source of truth. The
// vectors carry whatever embedder produced them; the caller assumes a fixed model (as the
// text-hash cache does) and should Reindex after a model change.
func (s *Store) LoadVectors(ctx context.Context) ([]VectorRecord, error) {
	return s.client.GetEmbeddings(ctx, s.coll, nil)
}

// reindexBatch bounds how many facts one embed request, and the upsert behind
// it, carries during a Reindex.
//
// Embedding endpoints cap a request's inputs — OpenAI at 2048,
// text-embeddings-inference at its in-flight limit (512 by default, answered
// with a 429) — so sending the corpus as one request fails on exactly the
// brains big enough to need a reindex. The same number brain.embedRecords uses.
const reindexBatch = 64

// Reindex rebuilds the entire collection from the base store. Use after bulk
// hand-edits, an embedder change, or to recover from index drift.
func (s *Store) Reindex(ctx context.Context) error {
	recs, err := s.base.List(ctx)
	if err != nil {
		return err
	}
	var durable []memory.Record
	for _, r := range recs {
		if !r.Source.Staged() {
			durable = append(durable, r)
		}
	}
	return s.upsertBatched(ctx, "reindex", durable)
}

// IndexStale brings the collection up to date with the base store without rebuilding it:
// only the durable facts the collection does not hold, or holds under a different text, are
// embedded and upserted, in the same bounded batches as Reindex. It returns how many facts it
// indexed. A fresh collection is therefore indexed whole, and one that missed writes while it
// was unreachable is caught up by exactly those writes.
//
// It never deletes. A vector whose fact is gone costs a search slot and nothing else, because
// recall only scores candidates it listed from the base store; deleting what the base store
// does not name would let a copy of the brain pointed at a shared collection empty the index
// the original is recalling from.
func (s *Store) IndexStale(ctx context.Context) (int, error) {
	recs, err := s.base.List(ctx)
	if err != nil {
		return 0, err
	}
	indexed, err := s.client.GetDocuments(ctx, s.coll)
	if err != nil {
		return 0, fmt.Errorf("chroma: read indexed documents: %w", err)
	}
	var stale []memory.Record
	for _, r := range recs {
		if r.Source.Staged() {
			continue
		}
		if doc, ok := indexed[r.ID]; ok && doc == r.Text {
			continue
		}
		stale = append(stale, r)
	}
	if err := s.upsertBatched(ctx, "index stale", stale); err != nil {
		return 0, err
	}
	return len(stale), nil
}

// upsertBatched embeds and upserts recs reindexBatch at a time. op names the caller in errors.
func (s *Store) upsertBatched(ctx context.Context, op string, recs []memory.Record) error {
	ids := make([]string, len(recs))
	texts := make([]string, len(recs))
	metas := make([]map[string]any, len(recs))
	for i, r := range recs {
		ids[i], texts[i], metas[i] = r.ID, r.Text, metadataFor(r)
	}
	for start := 0; start < len(ids); start += reindexBatch {
		end := min(start+reindexBatch, len(ids))
		emb, err := s.embedder.Embed(ctx, texts[start:end])
		if err != nil {
			return fmt.Errorf("chroma: %s embed facts %d-%d of %d: %w", op, start, end, len(ids), err)
		}
		if len(emb) != end-start {
			return fmt.Errorf("chroma: %s embed facts %d-%d of %d: embedder returned %d vectors", op, start, end, len(ids), len(emb))
		}
		if err := s.client.Upsert(ctx, s.coll, ids[start:end], emb, texts[start:end], metas[start:end]); err != nil {
			return fmt.Errorf("chroma: %s upsert facts %d-%d of %d: %w", op, start, end, len(ids), err)
		}
	}
	return nil
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
