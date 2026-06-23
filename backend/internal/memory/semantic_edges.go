package memory

import "sort"

// Edge is an undirected relationship between two records, identified by id, carrying the
// cosine similarity (Score, in [0,1] after the threshold) that produced it — the frontend
// weights both the layout force and the edge's visual strength by it, so stronger memory
// associations pull harder and read brighter.
type Edge struct {
	A     string
	B     string
	Score float64
}

// SemanticEdges builds the k-nearest-neighbour similarity graph over a set of embedding
// vectors: for each record it keeps up to perNodeCap neighbours whose cosine similarity is at
// least threshold, then returns the union as undirected, de-duplicated edges. This is the
// embedding-powered *relationship* set for the brain graph — the client force simulation
// self-balances it into clusters, so semantically similar memories pull together without the
// backend ever computing a position. It preserves the full high-dimensional similarity as graph
// topology, unlike a 2D projection which collapses it to two axes.
//
// ids and vectors are parallel (ids[i] ↔ vectors[i]). O(n²·d) — fine at the brain's scale (low
// thousands), and called request-time through the warmed embed cache. perNodeCap ≤ 0 disables
// the per-node cap (every pair ≥ threshold); a vector that has no neighbour ≥ threshold yields
// no edge (an honestly isolated fact). Deterministic: candidate order and tie-breaks are fixed.
func SemanticEdges(ids []string, vectors [][]float32, threshold float64, perNodeCap int) []Edge {
	n := len(ids)
	if n < 2 || len(vectors) != n {
		return nil
	}
	type cand struct {
		j     int
		score float64
	}
	neighbours := make([][]cand, n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			s := CosineSimilarity(vectors[i], vectors[j])
			if s < threshold {
				continue
			}
			neighbours[i] = append(neighbours[i], cand{j, s})
			neighbours[j] = append(neighbours[j], cand{i, s})
		}
	}

	seen := make(map[[2]int]struct{})
	var edges []Edge
	addPair := func(a, b int, score float64) {
		if a == b {
			return
		}
		key := [2]int{a, b}
		if a > b {
			key = [2]int{b, a}
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		edges = append(edges, Edge{A: ids[key[0]], B: ids[key[1]], Score: score})
	}
	for i := 0; i < n; i++ {
		cs := neighbours[i]
		// Strongest neighbours first; ties broken by index so the cap is deterministic.
		sort.SliceStable(cs, func(a, b int) bool {
			if cs[a].score != cs[b].score {
				return cs[a].score > cs[b].score
			}
			return cs[a].j < cs[b].j
		})
		if perNodeCap > 0 && len(cs) > perNodeCap {
			cs = cs[:perNodeCap]
		}
		for _, c := range cs {
			addPair(i, c.j, c.score)
		}
	}
	return edges
}
