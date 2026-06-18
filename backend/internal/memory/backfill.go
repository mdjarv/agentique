package memory

// SubsumedBackfill is the outcome of reconstructing one promoted fact's Subsumed
// provenance from its DerivedFrom ids during the one-time backfill.
type SubsumedBackfill struct {
	// Record is the eligible record with its Subsumed field filled from the matched
	// ids (in DerivedFrom order). It is only mutated when MatchedIDs is non-empty;
	// a record with no matches is returned unchanged so the caller can report it.
	Record Record
	// MatchedIDs are the DerivedFrom ids that resolved against the source index.
	MatchedIDs []string
	// UnmatchedIDs are the DerivedFrom ids absent from the source index — dangling
	// references not recoverable from this snapshot.
	UnmatchedIDs []string
}

// BackfillSubsumed reconstructs the empty Record.Subsumed of promoted facts from
// their DerivedFrom ids, resolving each id against index (id → the source fact it was
// merged from, captured before that original was deleted). It is the deterministic
// core of the one-time migration that repairs facts promoted before Subsumed was
// snapshotted at apply time (the review surface's inputs → output framing degrades to
// "originals not retained" without it).
//
// Only records that carry DerivedFrom ids and have an empty Subsumed are eligible, so
// the pass is idempotent: a record already carrying its merge inputs, or one with no
// provenance to rebuild, is skipped and never returned. For each eligible record the
// matched sources are appended in DerivedFrom order and ids missing from the index are
// reported as UnmatchedIDs; the record is only mutated when at least one id matched.
// The input records are never mutated in place — Subsumed is appended onto a per-record
// copy.
func BackfillSubsumed(records []Record, index map[string]SubsumedSource) []SubsumedBackfill {
	var out []SubsumedBackfill
	for _, r := range records {
		if len(r.DerivedFrom) == 0 || len(r.Subsumed) > 0 {
			continue
		}
		res := SubsumedBackfill{Record: r}
		// r is a value copy, but its Subsumed slice header is shared with the input;
		// it is empty here (guarded above), so reset to nil to guarantee the append
		// below allocates fresh and never aliases the caller's backing array.
		res.Record.Subsumed = nil
		for _, id := range r.DerivedFrom {
			src, ok := index[id]
			if !ok {
				res.UnmatchedIDs = append(res.UnmatchedIDs, id)
				continue
			}
			res.MatchedIDs = append(res.MatchedIDs, id)
			res.Record.Subsumed = append(res.Record.Subsumed, src)
		}
		out = append(out, res)
	}
	return out
}
