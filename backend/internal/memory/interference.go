package memory

import "sort"

// Interference detection (RFC-LD D5). What an agent actually confuses is not exact
// duplicates (consolidation merges those) nor unrelated facts, but the band between:
// facts similar enough to be conflated yet distinct enough to both survive — the
// proactive/retroactive interference zone. Surfacing these pairs lets the confirm UX
// ask "same fact, or genuinely distinct?".

// InterferencePair is two facts whose similarity sits in the interference band. A is
// the lexicographically smaller id, so a pair has one canonical form.
type InterferencePair struct {
	A          string  `json:"a"`
	B          string  `json:"b"`
	Similarity float64 `json:"similarity"`
}

// DetectInterference finds fact pairs in the "related but not a lexical duplicate" band:
// pairs at/above upper (token-Jaccard) are duplicates (consolidation's job); pairs related
// by neither signal aren't a confusion risk. With SimOptions (semantic mode) the
// related-lower bound blends embedding cosine, so a semantic near-match (low Jaccard, high
// cosine) — exactly what an agent confuses — is surfaced too; duplicate-exclusion stays
// lexical (see Similarity.interference). Without SimOptions it is pure token-Jaccard, so
// existing behaviour is unchanged. Captures are excluded. Each fact is reported in at most
// maxPerFact pairs (its strongest), so one hub fact can't flood the list. Deterministic:
// sorted by similarity desc, then ids. O(n^2) — fine at the dozens-to-low-thousands scale
// the brain targets (see RFC non-goals).
func DetectInterference(records []Record, lower, upper float64, limit int, opts ...SimOption) []InterferencePair {
	facts := make([]Record, 0, len(records))
	for _, r := range records {
		if r.Source != SourceCapture {
			facts = append(facts, r)
		}
	}
	if len(facts) < 2 {
		return nil
	}
	sim := newSimilarity(facts, opts...)

	var pairs []InterferencePair
	for i := 0; i < len(facts); i++ {
		for j := i + 1; j < len(facts); j++ {
			s, inBand := sim.interference(i, j, lower, upper)
			if !inBand {
				continue
			}
			a, b := facts[i].ID, facts[j].ID
			if b < a {
				a, b = b, a
			}
			pairs = append(pairs, InterferencePair{A: a, B: b, Similarity: s})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Similarity != pairs[j].Similarity {
			return pairs[i].Similarity > pairs[j].Similarity
		}
		if pairs[i].A != pairs[j].A {
			return pairs[i].A < pairs[j].A
		}
		return pairs[i].B < pairs[j].B
	})

	// Degree cap: keep each fact in at most maxPerFact of its strongest pairs so a
	// generic hub doesn't dominate the list. Pairs are already strongest-first.
	const maxPerFact = 3
	deg := map[string]int{}
	kept := pairs[:0]
	for _, p := range pairs {
		if deg[p.A] >= maxPerFact || deg[p.B] >= maxPerFact {
			continue
		}
		deg[p.A]++
		deg[p.B]++
		kept = append(kept, p)
	}
	if limit > 0 && len(kept) > limit {
		kept = kept[:limit]
	}
	return kept
}
