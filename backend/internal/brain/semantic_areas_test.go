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
	s.embedder = fakeEmbedder{topic: func(text string) []float32 {
		for _, kw := range []string{"race", "concurrent", "goroutine"} {
			if strings.Contains(text, kw) {
				return []float32{1, 0}
			}
		}
		return []float32{0, 1}
	}}
	s.cosThresh = 0.9

	// Two scopes, lexically-disjoint facts about the same concept (concurrency safety).
	mustAdd(t, s, ctx, "project:one", "race detector required before merge")
	mustAdd(t, s, ctx, "project:two", "concurrent access must be goroutine safe")
	// A different topic in a third scope — must not join the concurrency area.
	mustAdd(t, s, ctx, "project:three", "frontend uses tailwind for styling")

	if _, err := s.AssignAreas(ctx); err != nil {
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
