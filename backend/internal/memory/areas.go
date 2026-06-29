package memory

import (
	"context"
	"fmt"
	"math"
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
		if r.Source != SourceCapture && !isArchived(r) { // archived = cold tier, excluded from areas (M5)
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

	// idf over the whole durable corpus, so areaLabel can down-weight tokens that are
	// common everywhere ("go", "user") in favour of the ones that distinguish an area.
	idf := corpusIDF(durable)

	areaByID := make(map[string]string)
	usedLabels := make(map[string]int)
	var infos []AreaInfo
	for _, g := range groups {
		label := areaLabel(g.Members, idf)
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

// areaLabel names an area by TF-IDF over its members' tokens: a token's weight is its
// in-area document frequency times its inverse document frequency across the whole durable
// corpus (idf), so a token that appears in *every* area ("go", "user", "agentkit") is
// down-weighted in favour of the ones that actually distinguish this area. This replaces
// raw frequency, which over-weighted generic glue and produced labels like "go agentkit
// codex". Ties broken by idf (the rarer, more distinctive token wins) then alphabetically,
// so the result is deterministic. Returns "" when the members share no significant tokens.
func areaLabel(members []ScopedFact, idf map[string]float64) string {
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
	// idf is ≥ 1 for any corpus token (see corpusIDF); a token absent from the corpus
	// map can't occur here (idf is built from the same durable set the members come from),
	// but default to 1 so an unexpected miss falls back to raw frequency rather than zero.
	weight := func(t string) float64 {
		w := idf[t]
		if w == 0 {
			w = 1
		}
		return w
	}
	score := func(t string) float64 { return float64(freq[t]) * weight(t) }
	sort.Slice(toks, func(i, j int) bool {
		if si, sj := score(toks[i]), score(toks[j]); si != sj {
			return si > sj
		}
		if wi, wj := weight(toks[i]), weight(toks[j]); wi != wj {
			return wi > wj
		}
		return toks[i] < toks[j]
	})
	if len(toks) > areaLabelTokens {
		toks = toks[:areaLabelTokens]
	}
	return strings.Join(toks, " ")
}

// corpusIDF returns the inverse document frequency of every significant token across the
// durable corpus: idf(t) = 1 + ln((1+N)/(1+df)), where N is the corpus size and df is the
// number of facts whose token set contains t. The +1 smoothing keeps it finite for a token
// in every fact (idf → 1, never 0, so a ubiquitous token still ranks by raw frequency
// instead of being dropped) and safe for a singleton corpus. idf grows as a token gets
// rarer — exactly the down-weight of corpus-common glue that areaLabel needs.
func corpusIDF(durable []Record) map[string]float64 {
	df := make(map[string]int)
	for _, r := range durable {
		for t := range tokenSet(r.Text) {
			df[t]++
		}
	}
	n := float64(len(durable))
	idf := make(map[string]float64, len(df))
	for t, d := range df {
		idf[t] = 1 + math.Log((1+n)/(1+float64(d)))
	}
	return idf
}
