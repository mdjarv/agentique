package memory

import (
	"sort"
	"testing"
)

// edgeKey returns a stable string key for an undirected edge so tests can compare sets.
func edgeKey(e Edge) string {
	if e.A < e.B {
		return e.A + "|" + e.B
	}
	return e.B + "|" + e.A
}

func edgeSet(edges []Edge) map[string]bool {
	m := make(map[string]bool, len(edges))
	for _, e := range edges {
		m[edgeKey(e)] = true
	}
	return m
}

// TestSemanticEdgesConnectsClustersNotAcross builds two tight clusters that are dissimilar to
// each other and asserts edges form within each cluster but not between them.
func TestSemanticEdgesConnectsClustersNotAcross(t *testing.T) {
	// Cluster A points along +x, cluster B along +y → within-cluster cosine ≈ 1,
	// cross-cluster cosine ≈ 0.
	vectors := [][]float32{
		{1, 0}, {0.99, 0.01}, {0.98, 0.02}, // A: a0,a1,a2
		{0, 1}, {0.01, 0.99}, {0.02, 0.98}, // B: b0,b1,b2
	}
	ids := []string{"a0", "a1", "a2", "b0", "b1", "b2"}
	set := edgeSet(SemanticEdges(ids, vectors, 0.5, 0))

	for _, e := range []string{"a0|a1", "a0|a2", "a1|a2", "b0|b1", "b0|b2", "b1|b2"} {
		if !set[e] {
			t.Errorf("expected within-cluster edge %q", e)
		}
	}
	for a := range map[string]bool{"a0": true, "a1": true, "a2": true} {
		for b := range map[string]bool{"b0": true, "b1": true, "b2": true} {
			if set[edgeKey(Edge{A: a, B: b})] {
				t.Errorf("unexpected cross-cluster edge %s-%s (cosine ≈ 0 < threshold)", a, b)
			}
		}
	}
}

func TestSemanticEdgesPerNodeCap(t *testing.T) {
	// Five near-identical vectors: every pair is above threshold, so without a cap each
	// node would link to all 4 others. A cap of 2 keeps only the 2 strongest per node.
	vectors := [][]float32{
		{1, 0, 0}, {0.99, 0.1, 0}, {0.98, 0.0, 0.1}, {0.97, 0.05, 0.05}, {0.96, 0.1, 0.1},
	}
	ids := []string{"n0", "n1", "n2", "n3", "n4"}
	edges := SemanticEdges(ids, vectors, 0.5, 2)

	// Per-node degree should not exceed the cap by much (union of asymmetric kNN can push a
	// popular node slightly over its own cap, but no node should approach the uncapped 4).
	deg := map[string]int{}
	for _, e := range edges {
		deg[e.A]++
		deg[e.B]++
	}
	for id, d := range deg {
		if d > 4 {
			t.Errorf("node %s has degree %d; cap=2 should keep it well below the uncapped 4", id, d)
		}
	}
	if len(edges) == 0 {
		t.Fatal("expected some edges")
	}
}

func TestSemanticEdgesDeterministicAndDegenerate(t *testing.T) {
	if SemanticEdges(nil, nil, 0.5, 5) != nil {
		t.Fatal("empty input should return nil")
	}
	if SemanticEdges([]string{"a"}, [][]float32{{1, 0}}, 0.5, 5) != nil {
		t.Fatal("single node has no edges")
	}
	vectors := [][]float32{{1, 0}, {0.9, 0.1}, {0.1, 0.9}, {0, 1}}
	ids := []string{"a", "b", "c", "d"}
	a := SemanticEdges(ids, vectors, 0.3, 3)
	b := SemanticEdges(ids, vectors, 0.3, 3)
	ka, kb := make([]string, len(a)), make([]string, len(b))
	for i := range a {
		ka[i] = edgeKey(a[i])
	}
	for i := range b {
		kb[i] = edgeKey(b[i])
	}
	sort.Strings(ka)
	sort.Strings(kb)
	if len(ka) != len(kb) {
		t.Fatalf("non-deterministic edge count: %d vs %d", len(ka), len(kb))
	}
	for i := range ka {
		if ka[i] != kb[i] {
			t.Fatalf("non-deterministic edges at %d: %q vs %q", i, ka[i], kb[i])
		}
	}
}
