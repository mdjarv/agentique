package cachestore

import (
	"context"
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
