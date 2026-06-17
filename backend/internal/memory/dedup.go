package memory

import "strings"

// DefaultDuplicateThreshold is the token-Jaccard similarity at or above which two
// texts are considered duplicates (Odysseus-derived).
const DefaultDuplicateThreshold = 0.6

// jaccard returns the token-set Jaccard similarity of two texts in [0,1].
func jaccard(a, b string) float64 {
	return jaccardSets(tokenSet(a), tokenSet(b))
}

// jaccardSets is jaccard over pre-tokenized sets, so callers doing many pairwise
// comparisons (e.g. RelinkScope) tokenize each text once.
func jaccardSets(sa, sb map[string]struct{}) float64 {
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	inter := 0
	for t := range sa {
		if _, ok := sb[t]; ok {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	return float64(inter) / float64(union)
}

// IsTextDuplicate reports whether a and b are the same fact: exact (case- and
// space-insensitive) match, or token-Jaccard >= threshold.
func IsTextDuplicate(a, b string, threshold float64) bool {
	if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
		return true
	}
	return jaccard(a, b) >= threshold
}

// FindDuplicate returns the first record in existing whose text duplicates the
// given text at the given threshold.
func FindDuplicate(text string, existing []Record, threshold float64) (Record, bool) {
	for _, r := range existing {
		if IsTextDuplicate(text, r.Text, threshold) {
			return r, true
		}
	}
	return Record{}, false
}
