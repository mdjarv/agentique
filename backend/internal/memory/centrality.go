package memory

import "sort"

// Centrality is a record's position in the structural link graph (RFC P2 / graphify
// analyze.py). Degree counts its direct neighbours — a high-degree node is a "god
// node" / load-bearing fact many others hang off. Betweenness is the share of
// shortest paths that pass through it (Brandes, normalized to [0,1]) — a high-
// betweenness node is a "bridge" fact connecting otherwise separate topic clusters,
// the riskiest thing to lose.
type Centrality struct {
	Degree      int     `json:"degree"`
	Betweenness float64 `json:"betweenness"`
}

// ComputeCentrality returns degree and (normalized) betweenness centrality for each
// record, computed over the STRUCTURAL graph: undirected edges from each record's
// Related set unioned with its DerivedFrom provenance, keeping only edges whose
// endpoints are both in the set (dangling links to deleted captures are dropped).
// This is the same solid-edge signal the graph view draws and that P3 clusters over
// — deliberately NOT the client-side dashed Jaccard similarity, which is cosmetic.
//
// Pure stdlib, deterministic (id-sorted node order + sorted adjacency, so float
// accumulation order is fixed). It is computed on demand (RFC open-decision #5:
// request-time, cache if it bites), never persisted — like the link graph itself it
// is a rebuildable index, not source of truth.
func ComputeCentrality(records []Record) map[string]Centrality {
	out := make(map[string]Centrality, len(records))
	n := len(records)
	if n == 0 {
		return out
	}

	// Deterministic node order: id-sorted, so indices, BFS order and the float
	// accumulation in Brandes are reproducible regardless of caller ordering.
	nodes := make([]Record, n)
	copy(nodes, records)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	idx := make(map[string]int, n)
	for i, r := range nodes {
		idx[r.ID] = i
	}

	// Undirected adjacency, deduped (a Related edge and a reverse DerivedFrom edge
	// between the same pair count once), no self-loops, no dangling endpoints.
	adjSet := make([]map[int]struct{}, n)
	for i := range adjSet {
		adjSet[i] = map[int]struct{}{}
	}
	connect := func(a, b int) {
		if a == b {
			return
		}
		adjSet[a][b] = struct{}{}
		adjSet[b][a] = struct{}{}
	}
	for i, r := range nodes {
		for _, nid := range r.Related {
			if j, ok := idx[nid]; ok {
				connect(i, j)
			}
		}
		for _, nid := range r.DerivedFrom {
			if j, ok := idx[nid]; ok {
				connect(i, j)
			}
		}
	}

	// Materialize sorted adjacency lists for deterministic traversal.
	adj := make([][]int, n)
	for i := range adjSet {
		lst := make([]int, 0, len(adjSet[i]))
		for j := range adjSet[i] {
			lst = append(lst, j)
		}
		sort.Ints(lst)
		adj[i] = lst
	}

	bc := brandesBetweenness(adj)
	// Normalize undirected betweenness into [0,1]: the maximum possible value is the
	// number of unordered (s,t) pairs that could route through one node, (n-1)(n-2)/2.
	var norm float64
	if n > 2 {
		norm = float64((n-1)*(n-2)) / 2
	}
	for i, r := range nodes {
		c := Centrality{Degree: len(adj[i])}
		if norm > 0 {
			c.Betweenness = bc[i] / norm
		}
		out[r.ID] = c
	}
	return out
}

// brandesBetweenness computes unnormalized betweenness for an unweighted undirected
// graph (Brandes 2001, BFS variant). Each unordered pair is counted once (the raw
// sums are halved at the end, the standard undirected correction). O(V·E).
func brandesBetweenness(adj [][]int) []float64 {
	n := len(adj)
	bc := make([]float64, n)
	for s := 0; s < n; s++ {
		stack := make([]int, 0, n)
		pred := make([][]int, n)
		sigma := make([]float64, n) // # shortest paths s→v
		dist := make([]int, n)
		for i := range dist {
			dist[i] = -1
		}
		sigma[s] = 1
		dist[s] = 0

		queue := []int{s}
		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)
			for _, w := range adj[v] {
				if dist[w] < 0 { // first visit
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 { // shortest path to w via v
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], v)
				}
			}
		}

		// Back-propagate dependencies in reverse BFS order.
		delta := make([]float64, n)
		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, v := range pred[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
			}
			if w != s {
				bc[w] += delta[w]
			}
		}
	}
	for i := range bc {
		bc[i] /= 2 // undirected: each pair counted from both endpoints
	}
	return bc
}
