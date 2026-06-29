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
	// gen is bumped on every invalidation. snapshot() reads the inner store WITHOUT the lock
	// held (so concurrent reads don't serialize on a slow filestore read), then re-checks gen
	// before installing: if an Invalidate happened during that unlocked read, the freshly-read
	// snapshot may predate it and must NOT be installed, or it would silently re-poison the
	// cache with stale data (the TOCTOU an external rewrite — a snapshot restore — exposes).
	gen uint64
}

var _ memory.Store = (*Store)(nil)

// New wraps inner with a read-through cache.
func New(inner memory.Store) *Store { return &Store{inner: inner} }

// snapshot returns the cached corpus + id index, rebuilding from the inner store on a
// cold cache. Two concurrent cold rebuilds are harmless (identical data, last wins).
func (s *Store) snapshot(ctx context.Context) ([]memory.Record, map[string]memory.Record, error) {
	s.mu.RLock()
	all, byID, gen := s.all, s.byID, s.gen
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
	defer s.mu.Unlock()
	if s.all != nil {
		// A concurrent rebuild already installed a snapshot; prefer it (at least as fresh
		// as ours) for cache consistency.
		return s.all, s.byID, nil
	}
	if s.gen != gen {
		// The cache was invalidated while we read the inner store, so `fresh` may predate
		// that invalidation. Do NOT install it (that would re-poison the cache); leave the
		// cache cold so the next read rebuilds from the now-current store. Return our
		// best-effort read to THIS caller only (a benign one-time slightly-stale read).
		return fresh, idx, nil
	}
	s.all, s.byID = fresh, idx
	return fresh, idx, nil
}

func (s *Store) invalidate() {
	s.mu.Lock()
	s.all, s.byID = nil, nil
	s.gen++ // signal any in-flight rebuild that its snapshot is now stale (see snapshot()).
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
