package memory

import (
	"math"
	"sort"
	"time"
)

// Two-factor memory strength (RFC-LD D1), after Bjork & Bjork's New Theory of
// Disuse: a fact has a durable STORAGE strength (how well established — it only
// grows) and a RETRIEVAL strength (how accessible right now — it fades with disuse).
// Retrieval practice — a successful recall (BumpUses) — raises both. Both are derived
// from already-persisted fields, so nothing here is stored; they are rebuildable
// signals that sharpen recall ranking (recall.go) and decay (consolidate.go).

const (
	// retrievalHalfLifeDays is the disuse half-life of retrieval strength: a fact
	// unused for this many days reads at ~half its storage strength. Tunable.
	retrievalHalfLifeDays = 30.0

	// Storage blend weights (sum to 1): confidence (trust) dominates; saturating
	// cumulative use and provenance depth refine.
	storageConfWeight = 0.5
	storageUseWeight  = 0.35
	storageProvWeight = 0.15
	// storageProvCap caps how much provenance depth (DerivedFrom) can contribute.
	storageProvCap = 4
	// helpedUseWeight is how many bare injections (Uses) a single confirmed-useful
	// outcome (Helped) is worth in the saturating use term: corroborated-useful builds
	// storage faster than merely-shown (brain-outcome-signal.md).
	helpedUseWeight = 2
)

// StorageStrength is the durable "how well established" score in [0,1]: a blend of
// confidence (trust), saturating cumulative Uses (retrieval practice builds storage),
// and provenance depth (how many facts it was distilled from). Pinned facts are core
// context and read as maximally established. It never decays.
func StorageStrength(r Record) float64 {
	if r.Pinned {
		return 1
	}
	conf := NormalizeConfidence(r).ConfidenceScore
	if conf <= 0 || conf > 1 {
		conf = DefaultInferredScore
	}
	// Confirmed-useful outcomes (Helped) count for more than bare injections (Uses):
	// corroboration builds storage faster than merely being shown.
	use := 1 - 1/float64(1+r.Uses+helpedUseWeight*r.Helped) // 0 when never used, asymptotically →1
	prov := float64(min(len(r.DerivedFrom), storageProvCap)) / float64(storageProvCap)
	return clamp01(storageConfWeight*conf + storageUseWeight*use + storageProvWeight*prov)
}

// RetrievalStrength is current accessibility in [0,1]: storage strength decayed by
// time since the fact was last used (or, if never recalled, last touched). A fact
// recalled recently reads as strong even with modest storage; a well-established fact
// left untouched fades in accessibility — the disuse curve. now is passed so callers
// share one clock and the result is testable.
func RetrievalStrength(r Record, now time.Time) float64 {
	days := now.Sub(lastSeen(r)).Hours() / 24
	if days < 0 {
		days = 0
	}
	return clamp01(StorageStrength(r) * math.Pow(0.5, days/retrievalHalfLifeDays))
}

// lastSeen is when the fact was last touched in any way that signals it's still
// live: its recall timestamp if it has one, else its last content edit (so pre-D1
// facts, which have no LastUsedAt, behave as before).
func lastSeen(r Record) time.Time {
	if !r.LastUsedAt.IsZero() {
		return r.LastUsedAt
	}
	return r.UpdatedAt
}

const (
	// reviewStorageFloor: only well-established facts are worth resurfacing for review.
	reviewStorageFloor = 0.6
	// reviewRetrievalCeil: ...once they've gone cold (low current accessibility).
	reviewRetrievalCeil = 0.3
)

// DueForReview returns the well-established facts that have gone cold — high storage
// strength but low retrieval strength — so a spaced-review pass can resurface them
// before disuse decays them away (RFC-LD D6, the spacing effect: review what you know
// but haven't touched, rather than silently forgetting it). Pinned facts (always
// injected, never cold) and captures are excluded. Sorted most-due first (largest
// storage−retrieval gap, then id), capped at limit (<=0 → uncapped). Deterministic.
func DueForReview(records []Record, now time.Time, limit int) []Record {
	type scored struct {
		r   Record
		gap float64
	}
	var out []scored
	for _, r := range records {
		if r.Pinned || r.Source == SourceCapture || isArchived(r) { // archived = cold tier, never surfaced for review (M5)
			continue
		}
		st := StorageStrength(r)
		if st < reviewStorageFloor {
			continue
		}
		rt := RetrievalStrength(r, now)
		if rt > reviewRetrievalCeil {
			continue
		}
		out = append(out, scored{r, st - rt})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].gap != out[j].gap {
			return out[i].gap > out[j].gap
		}
		return out[i].r.ID < out[j].r.ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	res := make([]Record, len(out))
	for i := range out {
		res[i] = out[i].r
	}
	return res
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
