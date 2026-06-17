package memory

import (
	"context"
	"sort"
)

// DefaultRelatedThreshold is the token-Jaccard similarity at or above which two
// distinct facts are treated as related (a `[[link]]` edge). It sits below
// DefaultDuplicateThreshold: duplicates are merged by consolidation, related facts
// are linked.
const DefaultRelatedThreshold = 0.3

// maxRelatedDegree bounds edges per fact so dense scopes don't hairball.
const maxRelatedDegree = 6

// RelinkScope recomputes the similarity link graph for a scope: each durable
// fact's Related is rebuilt to its strongest token-Jaccard neighbors above
// DefaultRelatedThreshold, capped at maxRelatedDegree, bidirectional. Deterministic
// and idempotent (a fact whose edge set is unchanged is not rewritten, so it
// doesn't churn files or timestamps). Captures are ignored. Returns the edge count.
//
// It currently REBUILDS Related (overwrites) — correct while nothing else writes
// that field. When curated/human links are introduced, this must preserve them
// (tag auto vs. curated edges) rather than overwrite.
func RelinkScope(ctx context.Context, store Store, scope Scope) (int, error) {
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
	if len(facts) < 2 {
		return 0, nil
	}

	toks := make([]map[string]struct{}, len(facts))
	for i, f := range facts {
		toks[i] = tokenSet(f.Text)
	}

	type edge struct {
		j   int
		sim float64
	}
	neighbors := make([][]edge, len(facts))
	for i := 0; i < len(facts); i++ {
		for j := i + 1; j < len(facts); j++ {
			if sim := jaccardSets(toks[i], toks[j]); sim >= DefaultRelatedThreshold {
				neighbors[i] = append(neighbors[i], edge{j, sim})
				neighbors[j] = append(neighbors[j], edge{i, sim})
			}
		}
	}

	edges := 0
	for i, f := range facts {
		ns := neighbors[i]
		sort.Slice(ns, func(a, b int) bool {
			if ns[a].sim != ns[b].sim {
				return ns[a].sim > ns[b].sim
			}
			return facts[ns[a].j].ID < facts[ns[b].j].ID // stable tie-break
		})
		if len(ns) > maxRelatedDegree {
			ns = ns[:maxRelatedDegree]
		}
		related := make([]string, 0, len(ns))
		for _, e := range ns {
			related = append(related, facts[e.j].ID)
		}
		if sameStrings(f.Related, related) {
			continue
		}
		f.Related = related
		if err := store.Put(ctx, f); err != nil {
			return edges, err
		}
		edges += len(related)
	}
	return edges, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
