package cachestore

import (
	"context"
	"sync"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// countingStore is an in-memory Store that counts List calls so the test can prove the
// cache serves reads without re-hitting the inner store until a write invalidates it.
type countingStore struct {
	recs  map[string]memory.Record
	lists int
}

func newCounting(recs ...memory.Record) *countingStore {
	m := &countingStore{recs: map[string]memory.Record{}}
	for _, r := range recs {
		m.recs[r.ID] = r
	}
	return m
}

func (c *countingStore) Put(_ context.Context, r memory.Record) error {
	c.recs[r.ID] = r
	return nil
}
func (c *countingStore) Get(_ context.Context, id string) (memory.Record, error) {
	if r, ok := c.recs[id]; ok {
		return r, nil
	}
	return memory.Record{}, memory.ErrNotFound
}
func (c *countingStore) Delete(_ context.Context, id string) error {
	delete(c.recs, id)
	return nil
}
func (c *countingStore) List(_ context.Context, scopes ...memory.Scope) ([]memory.Record, error) {
	c.lists++
	var out []memory.Record
	for _, r := range c.recs {
		if len(scopes) == 0 {
			out = append(out, r)
			continue
		}
		for _, s := range scopes {
			if r.Scope == s {
				out = append(out, r)
				break
			}
		}
	}
	return out, nil
}

func rec(id string, scope memory.Scope) memory.Record {
	return memory.Record{ID: id, Scope: scope, Text: id, Source: memory.SourceAgent}
}

func TestCacheServesReadsWithoutReListing(t *testing.T) {
	ctx := context.Background()
	inner := newCounting(rec("a", "project:one"), rec("b", "project:two"))
	c := New(inner)

	// First List populates the cache (one inner List).
	if got, _ := c.List(ctx); len(got) != 2 {
		t.Fatalf("List = %d, want 2", len(got))
	}
	// Subsequent reads (List + Get, any scope) serve from cache — no more inner Lists.
	for i := 0; i < 5; i++ {
		_, _ = c.List(ctx, memory.Scope("project:one"))
		_, _ = c.Get(ctx, "b")
	}
	if inner.lists != 1 {
		t.Fatalf("inner List should be called once, got %d", inner.lists)
	}

	// Scope filtering is correct.
	one, _ := c.List(ctx, memory.Scope("project:one"))
	if len(one) != 1 || one[0].ID != "a" {
		t.Fatalf("scoped List = %+v, want [a]", one)
	}
}

func TestCacheInvalidatesOnWrite(t *testing.T) {
	ctx := context.Background()
	inner := newCounting(rec("a", "project:one"))
	c := New(inner)

	if _, err := c.List(ctx); err != nil { // warm
		t.Fatal(err)
	}
	if inner.lists != 1 {
		t.Fatalf("warm: lists = %d", inner.lists)
	}

	// Put invalidates → next read re-lists and reflects the write.
	if err := c.Put(ctx, rec("c", "project:one")); err != nil {
		t.Fatal(err)
	}
	got, _ := c.List(ctx)
	if len(got) != 2 {
		t.Fatalf("after Put, List = %d, want 2", len(got))
	}
	if inner.lists != 2 {
		t.Fatalf("Put should invalidate (re-list), lists = %d", inner.lists)
	}
	if r, err := c.Get(ctx, "c"); err != nil || r.ID != "c" {
		t.Fatalf("Get(c) after Put = %+v, %v", r, err)
	}

	// Delete invalidates too.
	if err := c.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "a"); err != memory.ErrNotFound {
		t.Fatalf("Get(a) after Delete = %v, want ErrNotFound", err)
	}
}

// gatedStore captures the corpus at the moment List is entered, signals that it has entered,
// then blocks on a gate before returning the captured snapshot. This lets a test deterministically
// force the TOCTOU interleaving: a reader that captures the OLD corpus, is held mid-read while an
// Invalidate (an external rewrite) happens, then resumes — without sleeps or flakiness.
type gatedStore struct {
	mu      sync.Mutex
	recs    []memory.Record
	gate    chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (g *gatedStore) setRecs(recs []memory.Record) {
	g.mu.Lock()
	g.recs = recs
	g.mu.Unlock()
}
func (g *gatedStore) Put(context.Context, memory.Record) error { return nil }
func (g *gatedStore) Delete(context.Context, string) error     { return nil }
func (g *gatedStore) Get(context.Context, string) (memory.Record, error) {
	return memory.Record{}, memory.ErrNotFound
}
func (g *gatedStore) List(_ context.Context, _ ...memory.Scope) ([]memory.Record, error) {
	g.mu.Lock()
	captured := append([]memory.Record(nil), g.recs...) // snapshot the corpus at entry
	g.mu.Unlock()
	g.once.Do(func() { close(g.entered) })
	<-g.gate // held mid-read
	return captured, nil
}

// TestStore_InvalidateDuringRebuild_NotClobbered is the regression test for the snapshot-restore
// TOCTOU: an Invalidate that lands while a reader is mid-rebuild must NOT be clobbered by that
// reader installing its now-stale snapshot. Without the generation guard the cache would serve the
// pre-invalidate corpus forever; with it, the next read rebuilds from the current corpus.
func TestStore_InvalidateDuringRebuild_NotClobbered(t *testing.T) {
	inner := &gatedStore{
		recs:    []memory.Record{{ID: "a", Text: "old"}},
		gate:    make(chan struct{}),
		entered: make(chan struct{}),
	}
	c := New(inner)

	done := make(chan []memory.Record, 1)
	go func() {
		recs, _ := c.List(context.Background()) // reader A: blocks in inner.List having captured "old"
		done <- recs
	}()

	<-inner.entered // reader A is mid-read of the OLD corpus
	// A concurrent restore: the underlying corpus changes and the cache is invalidated.
	inner.setRecs([]memory.Record{{ID: "a", Text: "new"}})
	c.Invalidate()
	close(inner.gate) // let reader A resume and try to install its stale "old" snapshot
	<-done            // reader A has finished its (skipped) install

	// The cache must reflect the post-restore corpus, not the clobbered-back "old".
	got, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "new" {
		t.Fatalf("cache poisoned by a stale mid-rebuild install: got %+v, want a=\"new\"", got)
	}
}
