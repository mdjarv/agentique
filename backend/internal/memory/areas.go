package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// DefaultAreaThreshold is the token-Jaccard similarity at or above which facts join the
// same cross-scope topic area. It matches DefaultCommunityThreshold — areas are the
// cross-scope projection of the same topic-clustering signal communities use.
const DefaultAreaThreshold = DefaultCommunityThreshold

// areaLabelTokens is how many of an area's most frequent significant tokens form its
// human-readable label/id.
const areaLabelTokens = 3

// AreaInfo describes one computed cross-scope area: its label/id, how many facts it
// holds and which scopes it spans. Used for read-only previews.
type AreaInfo struct {
	Label  string
	Size   int
	Scopes []Scope
}

// durableFacts returns the non-capture records (areas, like communities, never include
// raw episodic captures).
func durableFacts(all []Record) []Record {
	out := make([]Record, 0, len(all))
	for _, r := range all {
		if r.Source != SourceCapture {
			out = append(out, r)
		}
	}
	return out
}

// computeAreas partitions durable facts into cross-scope topic areas and returns both the
// per-record area assignment (id "" for facts in no area) and the per-area metadata. Pure
// and deterministic — the shared core of AssignAreas (persist) and PreviewAreas (inspect).
func computeAreas(durable []Record, threshold float64, minScopes int, opts ...SimOption) (map[string]string, []AreaInfo) {
	if threshold <= 0 {
		threshold = DefaultAreaThreshold
	}
	cands := make([]ScopedFact, 0, len(durable))
	for _, r := range durable {
		cands = append(cands, ScopedFact{Scope: r.Scope, ID: r.ID, Text: r.Text, Category: r.Category})
	}
	groups := CrossScopeGroups(cands, threshold, minScopes, opts...)

	areaByID := make(map[string]string)
	usedLabels := make(map[string]int)
	var infos []AreaInfo
	for _, g := range groups {
		label := areaLabel(g.Members)
		if label == "" {
			continue // a group with no significant tokens can't be named — skip
		}
		// Disambiguate two distinct groups that distill to the same label so they stay
		// separate areas. Deterministic: CrossScopeGroups returns a stable group order.
		id := label
		if n := usedLabels[label]; n > 0 {
			id = fmt.Sprintf("%s (%d)", label, n+1)
		}
		usedLabels[label]++
		for _, m := range g.Members {
			areaByID[m.ID] = id
		}
		infos = append(infos, AreaInfo{Label: id, Size: len(g.Members), Scopes: g.Scopes})
	}
	return areaByID, infos
}

// AssignAreas recomputes the cross-scope topic "area" of every durable fact and persists
// it onto Record.Area. An area is a topic community (CrossScopeGroups) spanning at least
// minScopes distinct scopes — the unit of transferable knowledge made first-class. Facts
// that are single-scope or isolated get an empty Area.
//
// It is the cross-scope sibling of AssignCommunities: deterministic, idempotent (a record
// whose Area is unchanged is not rewritten, so it doesn't churn files or timestamps), and
// it ignores captures. The Area value is a readable label derived from the group's most
// frequent shared tokens, so it is comparable across the whole brain and needs no sidecar
// to display. Returns the number of records whose Area changed.
func AssignAreas(ctx context.Context, store Store, threshold float64, minScopes int, opts ...SimOption) (int, error) {
	all, err := store.List(ctx)
	if err != nil {
		return 0, err
	}
	durable := durableFacts(all)
	areaByID, _ := computeAreas(durable, threshold, minScopes, opts...)

	changed := 0
	for _, r := range durable {
		want := areaByID[r.ID] // "" for facts in no cross-scope area
		if r.Area == want {
			continue
		}
		r.Area = want
		if err := store.Put(ctx, r); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// PreviewAreas computes the cross-scope areas without persisting anything — for a
// read-only inspection (e.g. a CLI dry run before writing). Sorted largest-first.
func PreviewAreas(ctx context.Context, store Store, threshold float64, minScopes int, opts ...SimOption) ([]AreaInfo, error) {
	all, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	_, infos := computeAreas(durableFacts(all), threshold, minScopes, opts...)
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Size != infos[j].Size {
			return infos[i].Size > infos[j].Size
		}
		return infos[i].Label < infos[j].Label
	})
	return infos, nil
}

// areaLabel names an area from the most frequent significant tokens across its members
// (ties broken alphabetically for determinism). Returns "" when the members share no
// significant tokens.
func areaLabel(members []ScopedFact) string {
	freq := make(map[string]int)
	for _, m := range members {
		for t := range tokenSet(m.Text) {
			freq[t]++
		}
	}
	if len(freq) == 0 {
		return ""
	}
	toks := make([]string, 0, len(freq))
	for t := range freq {
		toks = append(toks, t)
	}
	sort.Slice(toks, func(i, j int) bool {
		if freq[toks[i]] != freq[toks[j]] {
			return freq[toks[i]] > freq[toks[j]]
		}
		return toks[i] < toks[j]
	})
	if len(toks) > areaLabelTokens {
		toks = toks[:areaLabelTokens]
	}
	return strings.Join(toks, " ")
}
