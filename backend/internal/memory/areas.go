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
func AssignAreas(ctx context.Context, store Store, threshold float64, minScopes int) (int, error) {
	if threshold <= 0 {
		threshold = DefaultAreaThreshold
	}
	all, err := store.List(ctx)
	if err != nil {
		return 0, err
	}
	durable := make([]Record, 0, len(all))
	cands := make([]ScopedFact, 0, len(all))
	for _, r := range all {
		if r.Source == SourceCapture {
			continue
		}
		durable = append(durable, r)
		cands = append(cands, ScopedFact{Scope: r.Scope, ID: r.ID, Text: r.Text, Category: r.Category})
	}

	groups := CrossScopeGroups(cands, threshold, minScopes)

	areaByID := make(map[string]string)
	usedLabels := make(map[string]int)
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
	}

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
