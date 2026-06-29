// Package cachestore is an in-memory read-through cache in front of any memory.Store.
// It exists because per-turn auto-recall (the fluid recall path) calls List on every
// turn, and an uncached filestore re-reads and re-parses every markdown file each time.
// The cache holds the whole corpus (a small, bounded set) and rebuilds lazily; any write
// (Put/Delete) invalidates it, so a single server process — which funnels all its writes
// through this decorator — always reads a consistent snapshot. An out-of-band writer
// (e.g. a `brain` CLI run against the same dir) won't invalidate a running server's
// cache; that's acceptable for rare maintenance ops and clears on the next write.
package cachestore

import (
	"context"
	"sync"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// Store wraps an inner memory.Store with a lazily-built, write-invalidated cache of the
// full record set. Reads (List/Get) serve from the cache; writes pass through and clear it.
type Store struct {
	inner memory.Store

	mu   sync.RWMutex
	all  []memory.Record          // full corpus snapshot; nil = cold
	byID map[string]memory.Record // id index over the same snapshot
}

var _ memory.Store = (*Store)(nil)

// New wraps inner with a read-through cache.
func New(inner memory.Store) *Store { return &Store{inner: inner} }

// snapshot returns the cached corpus + id index, rebuilding from the inner store on a
// cold cache. Two concurrent cold rebuilds are harmless (identical data, last wins).
func (s *Store) snapshot(ctx context.Context) ([]memory.Record, map[string]memory.Record, error) {
	s.mu.RLock()
	all, byID := s.all, s.byID
	s.mu.RUnlock()
	if all != nil {
		return all, byID, nil
	}

	fresh, err := s.inner.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	idx := make(map[string]memory.Record, len(fresh))
	for _, r := range fresh {
		idx[r.ID] = r
	}
	s.mu.Lock()
	s.all, s.byID = fresh, idx
	s.mu.Unlock()
	return fresh, idx, nil
}

func (s *Store) invalidate() {
	s.mu.Lock()
	s.all, s.byID = nil, nil
	s.mu.Unlock()
}

// Invalidate drops the cached corpus so the next read rebuilds it from the inner store.
// Put/Delete already invalidate on their own, so callers only need this after an EXTERNAL
// rewrite of the underlying files that bypasses this decorator — notably a snapshot restore
// (brain.Service.RestoreSnapshot), which rewrites the markdown tree underneath the cache.
// Without it the cache would keep serving the pre-restore corpus until the next write.
func (s *Store) Invalidate() { s.invalidate() }

// List returns records in the given scopes (all when none given), in the inner store's
// order. Callers must treat the returned records as read-only — they share slice backing
// with the cache (the codebase's write pattern is replace-field-then-Put, which is safe).
func (s *Store) List(ctx context.Context, scopes ...memory.Scope) ([]memory.Record, error) {
	all, _, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return append([]memory.Record(nil), all...), nil
	}
	want := make(map[memory.Scope]struct{}, len(scopes))
	for _, sc := range scopes {
		want[sc] = struct{}{}
	}
	var out []memory.Record
	for _, r := range all {
		if _, ok := want[r.Scope]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// Get returns the record with the given ID, or memory.ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (memory.Record, error) {
	_, byID, err := s.snapshot(ctx)
	if err != nil {
		return memory.Record{}, err
	}
	if r, ok := byID[id]; ok {
		return r, nil
	}
	return memory.Record{}, memory.ErrNotFound
}

// Put writes through to the inner store and invalidates the cache.
func (s *Store) Put(ctx context.Context, r memory.Record) error {
	if err := s.inner.Put(ctx, r); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Delete removes from the inner store and invalidates the cache.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.inner.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidate()
	return nil
}
