package memory

import (
	"math"
	"testing"
)

// rel builds a record with the given id and Related neighbours, in a fixed scope.
func rel(id string, related ...string) Record {
	r := New(ScopeGlobal, "fact "+id, CategoryFact, SourceConsolidated)
	r.ID = id
	r.Related = related
	return r
}

func TestComputeCentralityDegree(t *testing.T) {
	// Star: hub connected to a, b, c. Related is bidirectional, but ComputeCentrality
	// also treats it undirected, so listing it on the hub alone is enough.
	recs := []Record{
		rel("hub", "a", "b", "c"),
		rel("a"),
		rel("b"),
		rel("c"),
	}
	c := ComputeCentrality(recs)
	if c["hub"].Degree != 3 {
		t.Errorf("hub degree = %d, want 3", c["hub"].Degree)
	}
	for _, leaf := range []string{"a", "b", "c"} {
		if c[leaf].Degree != 1 {
			t.Errorf("%s degree = %d, want 1 (edge is undirected)", leaf, c[leaf].Degree)
		}
	}
	// In a star the hub lies on every leaf-to-leaf shortest path → max betweenness;
	// leaves lie on none → zero.
	if c["hub"].Betweenness <= 0 {
		t.Errorf("hub betweenness = %v, want > 0", c["hub"].Betweenness)
	}
	for _, leaf := range []string{"a", "b", "c"} {
		if c[leaf].Betweenness != 0 {
			t.Errorf("%s betweenness = %v, want 0", leaf, c[leaf].Betweenness)
		}
	}
}

func TestComputeCentralityBridge(t *testing.T) {
	// Two triangles joined by a single bridge edge m1—m2. The bridge nodes carry the
	// highest betweenness; surfacing them is the "bridge fact" signal.
	recs := []Record{
		rel("a1", "b1", "m1"),
		rel("b1", "a1", "m1"),
		rel("m1", "a1", "b1", "m2"),
		rel("m2", "a2", "b2", "m1"),
		rel("a2", "b2", "m2"),
		rel("b2", "a2", "m2"),
	}
	c := ComputeCentrality(recs)
	// m1 and m2 bridge the two clusters → strictly higher betweenness than a corner.
	if !(c["m1"].Betweenness > c["a1"].Betweenness) {
		t.Errorf("bridge m1 (%v) should exceed corner a1 (%v)", c["m1"].Betweenness, c["a1"].Betweenness)
	}
	if !(c["m2"].Betweenness > c["a2"].Betweenness) {
		t.Errorf("bridge m2 (%v) should exceed corner a2 (%v)", c["m2"].Betweenness, c["a2"].Betweenness)
	}
	// Normalized into [0,1].
	for id, cc := range c {
		if cc.Betweenness < 0 || cc.Betweenness > 1 {
			t.Errorf("%s betweenness %v out of [0,1]", id, cc.Betweenness)
		}
	}
}

func TestComputeCentralityPathBetweenness(t *testing.T) {
	// Path a—b—c: b is on the single a–c shortest path. Undirected normalization is
	// (n-1)(n-2)/2 = 1, so b's normalized betweenness is exactly 1, ends are 0.
	recs := []Record{rel("a", "b"), rel("b", "a", "c"), rel("c", "b")}
	c := ComputeCentrality(recs)
	if math.Abs(c["b"].Betweenness-1) > 1e-9 {
		t.Errorf("middle betweenness = %v, want 1", c["b"].Betweenness)
	}
	if c["a"].Betweenness != 0 || c["c"].Betweenness != 0 {
		t.Errorf("endpoint betweenness = (%v,%v), want 0", c["a"].Betweenness, c["c"].Betweenness)
	}
}

func TestComputeCentralityDropsDanglingEdges(t *testing.T) {
	// A Related link to a non-existent id (e.g. a deleted capture) is dropped, not
	// counted as degree.
	recs := []Record{rel("a", "ghost", "b"), rel("b", "a")}
	c := ComputeCentrality(recs)
	if c["a"].Degree != 1 {
		t.Errorf("degree = %d, want 1 (dangling 'ghost' edge dropped)", c["a"].Degree)
	}
}

func TestComputeCentralityEmptyAndSingleton(t *testing.T) {
	if got := ComputeCentrality(nil); len(got) != 0 {
		t.Errorf("nil centrality = %v, want empty", got)
	}
	one := ComputeCentrality([]Record{rel("solo")})
	if one["solo"].Degree != 0 || one["solo"].Betweenness != 0 {
		t.Errorf("singleton = %+v, want zero", one["solo"])
	}
}

// TestComputeCentralityWithEdges folds extra (semantic) edges into the graph: two facts
// with no structural link become connected, so they're no longer isolated and gain degree.
func TestComputeCentralityWithEdges(t *testing.T) {
	recs := []Record{rel("a"), rel("b"), rel("c")} // no structural edges → all isolated

	base := ComputeCentrality(recs)
	for _, id := range []string{"a", "b", "c"} {
		if base[id].Degree != 0 {
			t.Fatalf("%s should be isolated structurally, got degree %d", id, base[id].Degree)
		}
	}

	// A semantic edge a–b (plus one to a node outside the set, which must be ignored).
	withEdges := ComputeCentralityWithEdges(recs, []Edge{{A: "a", B: "b", Score: 0.9}, {A: "a", B: "ghost"}})
	if withEdges["a"].Degree != 1 || withEdges["b"].Degree != 1 {
		t.Errorf("a,b should be connected by the extra edge: a=%d b=%d", withEdges["a"].Degree, withEdges["b"].Degree)
	}
	if withEdges["c"].Degree != 0 {
		t.Errorf("c has no edge of any kind; want isolated, got degree %d", withEdges["c"].Degree)
	}
}
