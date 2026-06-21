package memory

import (
	"context"
	"sort"
)

// DefaultCommunityThreshold is the token-Jaccard similarity at or above which two
// facts are joined for *topic clustering* (DetectCommunities). It is deliberately
// LOWER than DefaultRelatedThreshold (0.3): a Related edge means "nearly the same
// fact", whereas a community is a broader topic group. On real bloated scopes 0.3
// leaves almost everything a singleton (so cluster-aware chunking degenerates to
// fixed-size); 0.15 surfaces coherent topic clusters (e.g. all of a project's
// "config/discovery" facts) that fit in one reorganize call, while staying above
// the point where unrelated facts collapse into one giant blob. Tunable.
const DefaultCommunityThreshold = 0.15

// AssignCommunities recomputes the topic-cluster id of every durable fact in a
// scope (DetectCommunities over the current Related graph + token-Jaccard) and
// persists it onto each record. Like RelinkScope it is deterministic and
// idempotent — a record whose community is unchanged is not rewritten, so it
// doesn't churn files or timestamps — and it ignores captures. Returns the number
// of records whose community changed. Run it AFTER RelinkScope so it sees the
// freshly rebuilt edges.
func AssignCommunities(ctx context.Context, store Store, scope Scope, opts ...SimOption) (int, error) {
	all, err := store.List(ctx, scope)
	if err != nil {
		return 0, err
	}
	facts := make([]Record, 0, len(all))
	for _, r := range all {
		if r.Source != SourceCapture {
			facts = append(facts, r)
		}
	}
	if len(facts) == 0 {
		return 0, nil
	}
	comm := DetectCommunities(facts, DefaultCommunityThreshold, opts...)
	changed := 0
	for _, r := range facts {
		c := comm[r.ID]
		if r.Community == c {
			continue
		}
		r.Community = c
		if err := store.Put(ctx, r); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// DetectCommunities partitions a record set into communities (topic clusters) via
// deterministic label propagation over a similarity graph. Edges come from two
// sources, unioned:
//
//   - each record's Related set (the persisted [[link]] graph from RelinkScope), and
//   - token-Jaccard >= threshold between any two records' text.
//
// Including the Jaccard pass means the algorithm still clusters a scope that has
// never been relinked (e.g. at plan time, before ApplyPlan rebuilds Related), while
// the Related edges let curated/graph structure sharpen the partition once it exists.
//
// The result maps record ID -> community id. Community ids are small ints assigned
// in ascending-record-id order, so the partition is fully reproducible: the same
// input always yields the same labels (RFC open-decision #2 — label propagation with
// a deterministic seed and tie-break). Isolated records (no edges) each form their
// own singleton community. Capture records should be filtered out by the caller.
func DetectCommunities(records []Record, threshold float64, opts ...SimOption) map[string]int {
	result := make(map[string]int, len(records))
	n := len(records)
	if n == 0 {
		return result
	}

	// Deterministic node order: sort a copy by ID. All downstream indices, the
	// label seed and the iteration order derive from this, so the output never
	// depends on the caller's record ordering or Go map iteration order.
	nodes := make([]Record, n)
	copy(nodes, records)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	idx := make(map[string]int, n)
	for i, r := range nodes {
		idx[r.ID] = i
	}

	sim := newSimilarity(nodes, opts...)

	// Build an undirected adjacency list, deduping Related and similarity edges so a
	// pair counts once. Self-edges and edges to records outside this set are dropped.
	adj := make([][]int, n)
	seen := make(map[[2]int]struct{})
	key := func(a, b int) [2]int {
		if a < b {
			return [2]int{a, b}
		}
		return [2]int{b, a}
	}
	connect := func(a, b int) {
		if a == b {
			return
		}
		k := key(a, b)
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
	}
	for i, r := range nodes {
		for _, rel := range r.Related {
			if j, ok := idx[rel]; ok {
				connect(i, j)
			}
		}
	}
	// Similarity edges. The lexical (Jaccard) graph is sparse, so connect every pair over
	// the threshold. With embeddings the cosine graph is dense and hub facts chain the
	// whole corpus into one cluster, so cap each node to its strongest maxSemanticDegree
	// neighbours first (measure-first finding) — keeping label propagation, which
	// resists chaining, as the partitioner.
	if sim.semantic() {
		type nb struct {
			j int
			s float64
		}
		cand := make([][]nb, n)
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if sim.Linked(i, j, threshold) {
					sc := sim.Score(i, j)
					cand[i] = append(cand[i], nb{j, sc})
					cand[j] = append(cand[j], nb{i, sc})
				}
			}
		}
		for i := 0; i < n; i++ {
			es := cand[i]
			sort.Slice(es, func(a, b int) bool {
				if es[a].s != es[b].s {
					return es[a].s > es[b].s
				}
				return es[a].j < es[b].j
			})
			if len(es) > maxSemanticDegree {
				es = es[:maxSemanticDegree]
			}
			for _, x := range es {
				connect(i, x.j)
			}
		}
	} else {
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if sim.Linked(i, j, threshold) {
					connect(i, j)
				}
			}
		}
	}

	// Asynchronous label propagation. Each node starts in its own community
	// (label = its id-sorted index). Sweeping in id-sorted order, a node adopts the
	// label most common among its neighbours, ties broken by the smallest label —
	// an order-independent choice, so the sweep is deterministic. Updates are read
	// back within the same sweep (async), which converges faster than synchronous.
	label := make([]int, n)
	for i := range label {
		label[i] = i
	}
	const maxIters = 50
	for iter := 0; iter < maxIters; iter++ {
		changed := false
		for i := 0; i < n; i++ {
			if len(adj[i]) == 0 {
				continue
			}
			counts := make(map[int]int, len(adj[i]))
			for _, nb := range adj[i] {
				counts[label[nb]]++
			}
			best, bestCount := -1, -1
			for lab, c := range counts {
				if c > bestCount || (c == bestCount && lab < best) {
					best, bestCount = lab, c
				}
			}
			if best != label[i] {
				label[i] = best
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// Compact the raw labels into ascending community ids by first appearance in
	// id-sorted order, so callers get 0,1,2,… rather than sparse seed indices.
	remap := make(map[int]int)
	next := 0
	for i := 0; i < n; i++ {
		c, ok := remap[label[i]]
		if !ok {
			c = next
			remap[label[i]] = c
			next++
		}
		result[nodes[i].ID] = c
	}
	return result
}
