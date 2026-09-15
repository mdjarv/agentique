package brain

import (
	"context"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// fakeEmbedder maps text to a coarse topic vector so the test can make lexically-disjoint
// facts semantically identical.
type fakeEmbedder struct{ topic func(text string) []float32 }

func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = f.topic(t)
	}
	return out, nil
}

// With an embedder configured, AssignAreas should cluster lexically-disjoint facts that
// are semantically the same into one cross-scope area — something lexical clustering
// alone cannot do.
func TestAssignAreasSemanticClustersAcrossScopes(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	// Activate semantic similarity with a topic embedder: "race"/"concurrent" → one
	// vector, everything else → an orthogonal one.
	s.sem.Store(&semanticBackend{cosThresh: 0.9, embedder: fakeEmbedder{topic: func(text string) []float32 {
		for _, kw := range []string{"race", "concurrent", "goroutine"} {
			if strings.Contains(text, kw) {
				return []float32{1, 0}
			}
		}
		return []float32{0, 1}
	}}})

	// Two scopes, lexically-disjoint facts about the same concept (concurrency safety).
	mustAdd(t, s, ctx, "project:one", "race detector required before merge")
	mustAdd(t, s, ctx, "project:two", "concurrent access must be goroutine safe")
	// A different topic in a third scope — must not join the concurrency area.
	mustAdd(t, s, ctx, "project:three", "frontend uses tailwind for styling")

	if _, err := s.AssignAreas(ctx, AreasOpts{}); err != nil {
		t.Fatal(err)
	}

	recs, _ := s.List(ctx)
	areaByText := map[string]string{}
	for _, r := range recs {
		areaByText[r.Text] = r.Area
	}
	a := areaByText["race detector required before merge"]
	b := areaByText["concurrent access must be goroutine safe"]
	c := areaByText["frontend uses tailwind for styling"]
	if a == "" || a != b {
		t.Errorf("semantically-equal cross-scope facts should share an area: a=%q b=%q", a, b)
	}
	if c == a && c != "" {
		t.Errorf("a different topic must not join the concurrency area: c=%q", c)
	}
}

func mustAdd(t *testing.T, s *Service, ctx context.Context, scope memory.Scope, text string) {
	t.Helper()
	if _, err := s.Add(ctx, scope, text, memory.CategoryFact, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}
}

// mintingExtractor rewrites one fact under its id and abstracts a new one, the two kinds of
// record a consolidation pass relinks without having embedded them up front.
type mintingExtractor struct{ rewriteID, rewriteTo, abstract string }

func (mintingExtractor) Extract(context.Context, []string) ([]memory.Candidate, error) { return nil, nil }

func (m mintingExtractor) Reorganize(_ context.Context, facts []memory.Fact) ([]memory.Fact, error) {
	out := make([]memory.Fact, 0, len(facts)+1)
	for _, f := range facts {
		if f.ID == m.rewriteID {
			f.Text = m.rewriteTo
		}
		out = append(out, f)
	}
	return append(out, memory.Fact{Text: m.abstract, Category: memory.CategoryFact}), nil
}

// Facts a pass mints or rewrites are relinked by their own embeddings, not left to Jaccard
// because their ids (or their texts) postdate the vectors computed before the pass.
func TestConsolidateLinksMintedAndRewrittenFactsSemantically(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	s.sem.Store(&semanticBackend{cosThresh: 0.9, embedder: fakeEmbedder{topic: func(text string) []float32 {
		for _, kw := range []string{"race", "concurrent", "goroutine", "mutex"} {
			if strings.Contains(text, kw) {
				return []float32{1, 0}
			}
		}
		return []float32{0, 1}
	}}})
	scope := ScopeForProject("mint")
	anchor, err := s.Add(ctx, scope, "race detector required before merge", memory.CategoryFact, memory.SourceConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Add(ctx, scope, "frontend styling uses tailwind classes", memory.CategoryFact, memory.SourceConsolidated)
	if err != nil {
		t.Fatal(err)
	}

	// The rewrite moves `other` onto the anchor's topic; the abstraction is new and on it too.
	// Neither shares a word with the anchor, so only embeddings can link them.
	ex := mintingExtractor{rewriteID: other.ID, rewriteTo: "every shared map needs a mutex", abstract: "goroutine safety is mandatory"}
	if _, err := s.Consolidate(ctx, scope, ex, memory.DecayPolicy{}, false, ConsolidateOpts{}); err != nil {
		t.Fatal(err)
	}

	recs, err := s.List(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	linked := map[string]bool{}
	for _, r := range recs {
		for _, id := range r.Related {
			if id == anchor.ID {
				linked[r.Text] = true
			}
		}
	}
	for _, text := range []string{"every shared map needs a mutex", "goroutine safety is mandatory"} {
		if !linked[text] {
			t.Errorf("%q is not linked to the anchor it is semantically identical to (records: %+v)", text, recs)
		}
	}
}
