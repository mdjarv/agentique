package memory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

// ScopedFact is a fact tagged with its scope, handed to a Promoter so it can see
// which facts recur across projects. Protected metadata is never exposed.
type ScopedFact struct {
	Scope    Scope    `json:"scope"`
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Category Category `json:"category"`
}

// Promotion is a proposal to lift a cross-cutting fact into the global scope,
// subsuming the per-project copies it generalizes (named by ID).
type Promotion struct {
	Text     string   `json:"text"`
	Category Category `json:"category"`
	Subsumes []string `json:"subsumes"`
}

// Promoter is the optional LLM capability behind cross-scope consolidation: given
// project facts (each tagged with its scope so recurrence is visible) it returns
// the global facts to create and, for each, the project-fact IDs it replaces.
// Only facts useful in EVERY project belong in global — recurring conventions or
// inherently user-level facts (identity, contact, durable preferences).
type Promoter interface {
	Promote(ctx context.Context, candidates []ScopedFact) ([]Promotion, error)
}

// GlobalPlan is the serializable proposal of a cross-scope promotion pass: the
// global facts to create (each naming the project copies it subsumes) plus a
// per-scope fingerprint so a stale plan is refused at apply time. Replaying it
// needs no further model call.
type GlobalPlan struct {
	Promotions   []Promotion       `json:"promotions"`
	Fingerprints map[string]string `json:"fingerprints"`
}

// maxPromoteBatch bounds how many candidates go to the Promoter per call. A var so
// tests can shrink it.
var maxPromoteBatch = 120

// maxParallelBatches caps concurrent model calls during a chunked LLM pass. Each
// batch is a separate subprocess, so this bounds real resource use (and provider
// rate pressure), not just goroutines. A var for tuning/tests.
var maxParallelBatches = 4

// RunBounded runs fn(i) for i in [0,n) with at most `limit` concurrent goroutines,
// stopping early when ctx is cancelled. fn must be safe for concurrent use and do
// its own locking for shared state. Returns once all started goroutines finish.
// Stdlib-only so it stays in the liftable core.
func RunBounded(ctx context.Context, n, limit int, fn func(i int)) {
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

// PlanGlobalPromotion runs the LLM phase of cross-scope consolidation: it gathers
// non-protected project facts, asks the Promoter which belong in global, and
// returns a replayable plan. It writes nothing. Candidates are sorted to colocate
// similar facts so recurrences tend to land in the same batch; a recurrence split
// across batches is caught on a subsequent pass.
func PlanGlobalPromotion(ctx context.Context, store Store, pr Promoter, opts ConsolidateOptions) (GlobalPlan, error) {
	plan := GlobalPlan{Fingerprints: map[string]string{}}
	all, err := store.List(ctx)
	if err != nil {
		return plan, err
	}

	var candidates []ScopedFact
	byScope := map[Scope][]Record{}
	for _, r := range all {
		if r.Scope == ScopeGlobal || r.Source == SourceCapture || isProtected(r) {
			continue // global stays put; captures and protected facts are off-limits
		}
		candidates = append(candidates, ScopedFact{Scope: r.Scope, ID: r.ID, Text: r.Text, Category: r.Category})
		byScope[r.Scope] = append(byScope[r.Scope], r)
	}
	if len(candidates) == 0 {
		return plan, nil
	}

	// Colocate similar facts so cross-project recurrences tend to share a batch.
	sort.SliceStable(candidates, func(i, j int) bool {
		ki, kj := promoteSortKey(candidates[i].Text), promoteSortKey(candidates[j].Text)
		if ki != kj {
			return ki < kj
		}
		return candidates[i].ID < candidates[j].ID
	})

	// Split into batches and run them with bounded concurrency — each batch is an
	// independent model call whose promotions just merge, so order doesn't matter.
	type span struct{ lo, hi int }
	var spans []span
	for i := 0; i < len(candidates); i += maxPromoteBatch {
		end := i + maxPromoteBatch
		if end > len(candidates) {
			end = len(candidates)
		}
		spans = append(spans, span{i, end})
	}
	total := len(spans)

	var mu sync.Mutex
	var done int
	var cancelErr error
	RunBounded(ctx, total, maxParallelBatches, func(idx int) {
		got, perr := pr.Promote(ctx, candidates[spans[idx].lo:spans[idx].hi])
		mu.Lock()
		defer mu.Unlock()
		if perr != nil {
			// A cancelled context aborts the whole pass; any other batch error is
			// recoverable — skip it so one bad call can't sink the run.
			if ctx.Err() != nil || errors.Is(perr, context.Canceled) {
				if cancelErr == nil {
					cancelErr = perr
				}
			} else if opts.OnError != nil {
				opts.OnError(perr)
			}
		} else {
			plan.Promotions = append(plan.Promotions, got...)
		}
		done++
		if opts.Progress != nil {
			opts.Progress(done, total)
		}
	})
	if cancelErr != nil {
		return plan, cancelErr
	}

	for scope, rs := range byScope {
		plan.Fingerprints[string(scope)] = fingerprint(rs)
	}
	return plan, nil
}

// ApplyGlobalPromotion applies a GlobalPlan deterministically — no model calls. It
// refuses with ErrStalePlan if any affected scope changed since the plan was made,
// drops hallucinated/protected subsumed IDs, and enforces a per-scope over-deletion
// safety net. With opts.DryRun it builds the full changelog (Promoted = global
// facts created, Deleted = project copies removed) but writes nothing.
func ApplyGlobalPromotion(ctx context.Context, store Store, plan GlobalPlan, opts ConsolidateOptions) (Report, error) {
	dupThreshold := opts.DuplicateThreshold
	if dupThreshold <= 0 {
		dupThreshold = DefaultDuplicateThreshold
	}
	rep := Report{Scope: ScopeGlobal}

	all, err := store.List(ctx)
	if err != nil {
		return rep, err
	}
	byID := make(map[string]Record, len(all))
	var globalFacts []Record
	byScope := map[Scope][]Record{}
	for _, r := range all {
		byID[r.ID] = r
		if r.Scope == ScopeGlobal {
			if r.Source != SourceCapture {
				globalFacts = append(globalFacts, r)
			}
			continue
		}
		if r.Source == SourceCapture || isProtected(r) {
			continue
		}
		byScope[r.Scope] = append(byScope[r.Scope], r)
	}

	// Staleness: each affected scope's non-protected set must match the plan.
	for scope, fp := range plan.Fingerprints {
		if fingerprint(byScope[Scope(scope)]) != fp {
			return rep, ErrStalePlan
		}
	}

	// Resolve subsumed IDs: must exist, be project-scoped and non-protected. Drop
	// unknown (hallucinated) or protected IDs — never delete what wasn't offered.
	deletions := map[string]Record{}
	for _, p := range plan.Promotions {
		for _, id := range p.Subsumes {
			r, ok := byID[id]
			if !ok || r.Scope == ScopeGlobal || r.Source == SourceCapture || isProtected(r) {
				continue
			}
			deletions[id] = r
		}
	}

	// Over-deletion safety net: refuse the whole pass if any non-trivial scope would
	// lose more than half of its facts.
	perScope := map[Scope]int{}
	for _, r := range deletions {
		perScope[r.Scope]++
	}
	for scope, n := range perScope {
		total := len(byScope[scope])
		if total >= minFactsForDeletionGuard && n*2 > total {
			rep.ReorgRefused = true
			return rep, nil
		}
	}

	existingGlobal := append([]Record(nil), globalFacts...)
	for _, p := range plan.Promotions {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		// A promotion that duplicates an existing global fact creates nothing, but
		// its subsumed copies are still removed below (folded under the existing one).
		if _, dup := FindDuplicate(text, existingGlobal, dupThreshold); dup {
			continue
		}
		nr := New(ScopeGlobal, text, p.Category, SourceConsolidated)
		// A promoted fact is a cross-project generalization — a riskier inference than
		// a directly distilled one (RFC P2), so it starts lower in the inferred band
		// where the confirm UX will surface it for the user to confirm or drop.
		nr.ConfidenceScore = CrossProjectInferredScore
		nr = NormalizeConfidence(nr)
		for _, id := range p.Subsumes {
			if _, ok := deletions[id]; ok {
				nr.DerivedFrom = append(nr.DerivedFrom, id)
			}
		}
		if nr.Category == CategoryIdentity {
			nr.Pinned = true
		}
		if !opts.DryRun {
			if err := store.Put(ctx, nr); err != nil {
				return rep, err
			}
		}
		existingGlobal = append(existingGlobal, nr)
		rep.Promoted = append(rep.Promoted, nr)
	}

	for _, r := range deletions {
		if !opts.DryRun {
			if err := store.Delete(ctx, r.ID); err != nil {
				return rep, err
			}
		}
		rep.Deleted = append(rep.Deleted, r)
	}
	if !opts.DryRun {
		if _, err := RelinkScope(ctx, store, ScopeGlobal); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// promoteSortKey returns a coarse key (first few sorted significant tokens) used to
// colocate similar facts so cross-project recurrences tend to share a batch.
func promoteSortKey(text string) string {
	toks := uniqueTokens(text)
	sort.Strings(toks)
	if len(toks) > 3 {
		toks = toks[:3]
	}
	return strings.Join(toks, " ")
}
