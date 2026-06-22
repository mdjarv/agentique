package memory

import (
	"context"
	"testing"
)

// memStore is an in-memory Store for tests.
type memStore struct{ recs map[string]Record }

func newMemStore(rs ...Record) *memStore {
	m := &memStore{recs: make(map[string]Record)}
	for _, r := range rs {
		m.recs[r.ID] = r
	}
	return m
}

func (m *memStore) Put(_ context.Context, r Record) error { m.recs[r.ID] = r; return nil }
func (m *memStore) Get(_ context.Context, id string) (Record, error) {
	r, ok := m.recs[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}
func (m *memStore) Delete(_ context.Context, id string) error { delete(m.recs, id); return nil }
func (m *memStore) List(_ context.Context, scopes ...Scope) ([]Record, error) {
	var out []Record
	for _, r := range m.recs {
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

// searchStore adds a canned semantic Searcher to memStore.
type searchStore struct {
	*memStore
	hits []Hit
}

func (s *searchStore) Search(_ context.Context, _ string, _ []Scope, _ int) ([]Hit, error) {
	return s.hits, nil
}

func rec(id, text string, cat Category) Record {
	r := New(ScopeGlobal, text, cat, SourceAgent)
	r.ID = id
	return r
}

func contains(rs []Record, id string) bool {
	for _, r := range rs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestRecallPinnedAlwaysIncluded(t *testing.T) {
	pinned := rec("p1", "totally unrelated pinned identity fact", CategoryIdentity)
	pinned.Pinned = true
	store := newMemStore(pinned, rec("a", "something else entirely", CategoryFact))

	res, err := Recall(context.Background(), store, Query{Text: "quantum widgets", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res.Pinned, "p1") {
		t.Fatalf("pinned record not returned: %+v", res.Pinned)
	}
}

func TestRecallRanksByRelevance(t *testing.T) {
	store := newMemStore(
		rec("a", "use just targets never raw npx tsc fails silently", CategoryPreference),
		rec("b", "auth flow lives in internal auth package", CategoryProject),
		rec("c", "user prefers dark mode in editor", CategoryPreference),
	)
	res, err := Recall(context.Background(), store, Query{Text: "run the build using npx or just please", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res.Recalled, "a") {
		t.Fatalf("expected relevant record 'a' recalled, got %+v", res.Recalled)
	}
	if contains(res.Recalled, "b") || contains(res.Recalled, "c") {
		t.Fatalf("irrelevant records should be excluded, got %+v", res.Recalled)
	}
}

func TestRecallExcludesCaptures(t *testing.T) {
	capture := rec("cap", "run the build using npx or just", CategoryFact)
	capture.Source = SourceCapture
	store := newMemStore(capture)
	res, err := Recall(context.Background(), store, Query{Text: "run the build using npx or just", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if contains(res.Recalled, "cap") {
		t.Fatalf("episodic capture must not be recalled, got %+v", res.Recalled)
	}
}

func TestRecallEmptyQueryReturnsOnlyPinned(t *testing.T) {
	pinned := rec("p", "pinned fact", CategoryIdentity)
	pinned.Pinned = true
	store := newMemStore(pinned, rec("a", "non pinned fact", CategoryFact))
	res, err := Recall(context.Background(), store, Query{Text: "   ", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Recalled) != 0 {
		t.Fatalf("empty query should recall nothing, got %+v", res.Recalled)
	}
	if !contains(res.Pinned, "p") {
		t.Fatal("pinned should still be returned for empty query")
	}
}

func TestRecallHybridVectorLiftsWeakKeyword(t *testing.T) {
	// 'd' has no keyword overlap with the query but a strong vector hit.
	d := rec("d", "kubernetes deployment uses helm charts", CategoryFact)
	base := newMemStore(d)

	// Without a Searcher: keyword-only, no overlap -> excluded.
	resKW, err := Recall(context.Background(), base, Query{Text: "container orchestration", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if contains(resKW.Recalled, "d") {
		t.Fatal("keyword-only recall should not surface 'd'")
	}

	// With a Searcher returning a strong hit: vector lifts it in.
	vec := &searchStore{memStore: base, hits: []Hit{{ID: "d", Score: 0.8}}}
	resVec, err := Recall(context.Background(), vec, Query{Text: "container orchestration", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(resVec.Recalled, "d") {
		t.Fatalf("vector hit should surface 'd', got %+v", resVec.Recalled)
	}
}

func TestRecallEmptyVectorResultFallsBackToKeyword(t *testing.T) {
	// 'a' shares two query tokens (alpha+zulu), so it clears the lone-token precision
	// guard and is the only real keyword hit. Under keyword-only ranking 'a' clears the
	// cutoff; if an empty (but successful) vector search were treated as "vector
	// available", the keyword weight would collapse and 'a' would be wrongly dropped.
	base := newMemStore(
		rec("a", "alpha zulu", CategoryFact),
		rec("b", "bravo", CategoryFact),
		rec("c", "charlie", CategoryFact),
		rec("d", "delta", CategoryFact),
		rec("e", "echo", CategoryFact),
	)
	q := Query{Text: "alpha zulu yankee xray whiskey", K: 3}

	// Sanity: keyword-only store surfaces 'a'.
	kw, err := Recall(context.Background(), base, q)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(kw.Recalled, "a") {
		t.Fatalf("keyword-only baseline should surface 'a', got %+v", kw.Recalled)
	}

	// A Searcher that returns no hits must behave identically (graceful fallback).
	ss := &searchStore{memStore: base, hits: nil}
	res, err := Recall(context.Background(), ss, q)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res.Recalled, "a") {
		t.Fatalf("empty vector result must fall back to keyword recall and surface 'a', got %+v", res.Recalled)
	}
}

// A lone incidental glue token (here "github", shared only via a URL in an off-topic
// fact) must NOT surface that fact when the query's actually-discriminating terms
// (secrets/vars) match nothing — the real-world mis-recall from the meta-spec session.
// "github" appears in two facts (df=2) while secrets/vars appear nowhere (df=0), so the
// lone github match carries only ~0.21 of the query's idf mass: under the floor alone it
// would still surface (final ≈ 0.95·0.21 ≈ 0.2 > minFinalScore), but the lone-token guard
// (< singleTokenMinShare) drops it. The guard is what's under test, not the floor.
func TestRecallDropsLoneGlueTokenMatch(t *testing.T) {
	goprivate := rec("go", "Private allbin Go modules require GOPRIVATE=github.com/allbin/* plus git SSH config", CategoryFact)
	store := newMemStore(
		goprivate,
		rec("ci", "the release workflow pushes artifacts to github actions", CategoryFact), // 2nd github mention → df(github)=2
		rec("x", "the linter embeds schemas via root embed.go and validates structure", CategoryProject),
		rec("y", "nginx proxy manager handles tls termination", CategoryFact),
	)
	res, err := Recall(context.Background(), store, Query{Text: "secrets and vars on github", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if contains(res.Recalled, "go") {
		t.Fatalf("a lone glue-token (github) match on an off-topic fact must be dropped, got %+v", res.Recalled)
	}

	// Non-vacuous proof it's the lone-token guard (not the score floor) doing the work:
	// a query that matches the SAME fact on multiple tokens (github + go + modules) must
	// surface it — the guard only targets the single-token case.
	res2, err := Recall(context.Background(), store, Query{Text: "github go modules setup", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res2.Recalled, "go") {
		t.Fatalf("a multi-token match on the same fact must surface (guard targets lone tokens only), got %+v", res2.Recalled)
	}
}

// A genuinely dominant single-keyword match (the one matched token carries most of the
// query's intent) must still surface — the guard targets weak lone tokens, not all of them.
func TestRecallKeepsDominantSingleToken(t *testing.T) {
	fact := rec("lint", "the linter embeds schemas via root embed.go and validates structure", CategoryFact)
	store := newMemStore(
		fact,
		rec("o1", "user uses nginx proxy manager for tls", CategoryFact),
		rec("o2", "deploys run via just release", CategoryFact),
	)
	// "linter" is the rare, defining term; "run" is common — the lone match dominates.
	res, err := Recall(context.Background(), store, Query{Text: "how do I run the linter", K: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res.Recalled, "lint") {
		t.Fatalf("a dominant single-keyword match should still surface, got %+v", res.Recalled)
	}
}

func TestBumpUses(t *testing.T) {
	r := rec("a", "fact", CategoryFact)
	store := newMemStore(r)
	if err := BumpUses(context.Background(), store, "a", "missing"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(context.Background(), "a")
	if got.Uses != 1 {
		t.Fatalf("uses=%d want 1", got.Uses)
	}
}
