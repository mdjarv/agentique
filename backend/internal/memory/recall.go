package memory

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultRecallK is the number of non-pinned records returned when Query.K<=0.
	DefaultRecallK = 3

	minKeywordNorm = 0.08 // below this a keyword match is treated as noise
	minFinalScore  = 0.12 // final score cutoff for inclusion
	recencyWeight  = 0.05 // recency is a tiebreaker only

	// Hybrid blend weights when semantic scores are available (Odysseus-derived):
	// the vector signal dominates, keyword overlap refines.
	vectorWeight  = 0.55
	keywordWeight = 0.40
	// Keyword-only weight (no vector signal); recency takes the rest.
	keywordOnlyWeight = 0.95
	// A candidate with neither a meaningful vector nor keyword signal is dropped.
	minVectorScore = 0.20
)

// Hit is a semantic search result: a record ID and a similarity in [0,1].
type Hit struct {
	ID    string
	Score float64
}

// Searcher is an optional capability a Store may implement to provide semantic
// (vector) ranking. When a Store satisfies Searcher, Recall blends its scores
// with keyword overlap; otherwise Recall degrades to keyword-only ranking.
type Searcher interface {
	Search(ctx context.Context, text string, scopes []Scope, k int) ([]Hit, error)
}

// Recall retrieves the memories to inject for a query: all pinned records, plus
// the top-K query-relevant non-pinned records. Episodic captures are never
// recalled. If the Store implements Searcher, ranking is a vector+keyword hybrid
// that degrades gracefully to keyword-only on any vector error.
func Recall(ctx context.Context, store Store, q Query) (Result, error) {
	all, err := store.List(ctx, q.Scopes...)
	if err != nil {
		return Result{}, err
	}
	k := q.K
	if k <= 0 {
		k = DefaultRecallK
	}

	var res Result
	candidates := make([]Record, 0, len(all))
	for _, r := range all {
		if r.Pinned {
			res.Pinned = append(res.Pinned, r)
			continue
		}
		if r.Source == SourceCapture {
			continue // raw episodic material is not injected
		}
		candidates = append(candidates, r)
	}
	if len(candidates) == 0 || strings.TrimSpace(q.Text) == "" {
		return res, nil
	}

	// Optional semantic scores. Any failure — or an empty result — degrades to
	// keyword-only: recall must never break, nor silently down-weight keyword
	// matches, because the vector index is down OR simply returned nothing (e.g.
	// a freshly enabled, not-yet-reindexed collection).
	var vec map[string]float64
	if s, ok := store.(Searcher); ok {
		if hits, serr := s.Search(ctx, q.Text, q.Scopes, k*3); serr == nil && len(hits) > 0 {
			vec = make(map[string]float64, len(hits))
			for _, h := range hits {
				vec[h.ID] = h.Score
			}
		}
	}

	res.Recalled = rank(q.Text, candidates, vec, k)
	res.Recalled = expandAssociative(res.Recalled, res.Pinned, all, k)
	return res, nil
}

// expandAssociative folds in a bounded set of `Related` neighbors of the top
// query matches — mirroring human associative recall — appended after the ranked
// hits at lower priority. Bounded fan-out (≤ assocPerSeed per seed, ≤ k total
// extra) keeps the hot path cheap; it reads persisted Related, no recompute.
func expandAssociative(recalled, pinned, all []Record, k int) []Record {
	if len(recalled) == 0 {
		return recalled
	}
	const assocPerSeed = 3
	byID := make(map[string]Record, len(all))
	for _, r := range all {
		byID[r.ID] = r
	}
	included := make(map[string]struct{}, len(recalled)+len(pinned))
	for _, r := range pinned {
		included[r.ID] = struct{}{}
	}
	for _, r := range recalled {
		included[r.ID] = struct{}{}
	}
	seeds := recalled // snapshot: iterate the ranked hits, not the growing result
	for _, seed := range seeds {
		added := 0
		for _, nid := range seed.Related {
			if len(recalled)-len(seeds) >= k || added >= assocPerSeed {
				break
			}
			nr, ok := byID[nid]
			if !ok || nr.Source == SourceCapture {
				continue
			}
			if _, dup := included[nid]; dup {
				continue
			}
			included[nid] = struct{}{}
			recalled = append(recalled, nr)
			added++
		}
	}
	return recalled
}

func rank(query string, candidates []Record, vec map[string]float64, k int) []Record {
	kwNorm := keywordScores(query, candidates)
	qcats := queryCategories(query)
	now := time.Now().UTC()
	haveVec := vec != nil

	type scored struct {
		r Record
		s float64
	}
	out := make([]scored, 0, len(candidates))
	for i, c := range candidates {
		kw := kwNorm[i]
		if b, ok := categoryBoost(qcats, c.Category); ok {
			kw = math.Min(kw*b, 1.0)
		}
		rec := recency(now, c.UpdatedAt)

		var final float64
		if haveVec {
			vs := vec[c.ID]
			if vs < minVectorScore && kw < minKeywordNorm {
				continue
			}
			final = vectorWeight*vs + keywordWeight*kw + recencyWeight*rec
		} else {
			if kw < minKeywordNorm {
				continue
			}
			final = keywordOnlyWeight*kw + recencyWeight*rec
		}
		if final < minFinalScore {
			continue
		}
		out = append(out, scored{c, final})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].s > out[j].s })
	if len(out) > k {
		out = out[:k]
	}
	res := make([]Record, len(out))
	for i := range out {
		res[i] = out[i].r
	}
	return res
}

// keywordScores returns an idf-weighted overlap score in [0,1] per candidate,
// aligned by index. A query token contributes its idf (computed over the
// candidate set) when present in the record; the sum is normalized by the total
// query-token weight so longer queries don't inflate scores.
func keywordScores(query string, candidates []Record) []float64 {
	scores := make([]float64, len(candidates))
	qtokens := uniqueTokens(query)
	if len(qtokens) == 0 {
		return scores
	}
	df := make(map[string]int)
	docSets := make([]map[string]struct{}, len(candidates))
	for i, c := range candidates {
		ts := tokenSet(c.Text)
		docSets[i] = ts
		for t := range ts {
			df[t]++
		}
	}
	n := float64(len(candidates))
	weights := make(map[string]float64, len(qtokens))
	var total float64
	for _, t := range qtokens {
		w := math.Log(1 + n/(1+float64(df[t])))
		weights[t] = w
		total += w
	}
	if total == 0 {
		return scores
	}
	for i := range candidates {
		var sum float64
		for t, w := range weights {
			if _, ok := docSets[i][t]; ok {
				sum += w
			}
		}
		scores[i] = sum / total
	}
	return scores
}

func uniqueTokens(s string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, t := range tokenize(s) {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func recency(now, t time.Time) float64 {
	days := now.Sub(t).Hours() / 24
	if days < 0 {
		days = 0
	}
	return 1.0 / (1.0 + days*0.05)
}

// categoryBoosts lift facts that are usually high-value as ambient context when
// the query signals that category. Values are Odysseus-derived.
var categoryBoosts = map[Category]float64{
	CategoryIdentity:   1.4,
	CategoryContact:    1.3,
	CategoryPreference: 1.2,
}

// categoryTriggers map query keywords to the category they favor. Kept
// conservative to avoid boosting on common words.
var categoryTriggers = map[Category][]string{
	CategoryIdentity:   {"name", "who", "identity"},
	CategoryContact:    {"email", "phone", "contact", "address"},
	CategoryPreference: {"prefer", "preference", "favorite", "favourite", "usually"},
}

func queryCategories(query string) map[Category]struct{} {
	ts := tokenSet(query)
	out := make(map[Category]struct{})
	for cat, trigs := range categoryTriggers {
		for _, t := range trigs {
			if _, ok := ts[t]; ok {
				out[cat] = struct{}{}
				break
			}
		}
	}
	return out
}

func categoryBoost(qcats map[Category]struct{}, c Category) (float64, bool) {
	if _, ok := qcats[c]; !ok {
		return 0, false
	}
	b, ok := categoryBoosts[c]
	return b, ok
}
