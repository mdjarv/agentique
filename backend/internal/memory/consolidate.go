package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Candidate is a proposed durable fact produced by an Extractor from raw episodes.
type Candidate struct {
	Text     string
	Category Category
}

// Fact is the minimal id+text+category view of a Record handed to an Extractor's
// Reorganize step. Protected metadata (pinned/locked/uses/source/timestamps) is
// never exposed to the model, so reorganization cannot tamper with it.
type Fact struct {
	ID       string
	Text     string
	Category Category
}

// Extractor is the LLM-backed cognition behind consolidation. The host
// application implements it (it owns the model call); this package supplies the
// surrounding policy, dedup and safety nets. Either method may legitimately
// return nothing.
type Extractor interface {
	// Extract distills raw episodic transcripts into candidate durable facts.
	Extract(ctx context.Context, episodes []string) ([]Candidate, error)
	// Reorganize returns a cleaned version of the given facts: duplicates merged,
	// vague entries rewritten, related facts abstracted into general rules. It
	// must keep the IDs of facts it retains and leave ID empty for newly
	// abstracted facts; the orchestrator drops any unknown (invented) IDs.
	Reorganize(ctx context.Context, facts []Fact) ([]Fact, error)
}

// DecayPolicy prunes stale, low-value durable facts. Disabled when MaxAge==0.
// Pinned, Locked and human-authored facts are never decayed.
type DecayPolicy struct {
	MaxAge  time.Duration
	MinUses int
}

// ConsolidateOptions configures a consolidation pass.
type ConsolidateOptions struct {
	// PrevFingerprint lets the pass skip the (expensive) Reorganize step when the
	// reorganizable set is byte-for-byte unchanged since the last pass.
	PrevFingerprint string
	Decay           DecayPolicy
	// DuplicateThreshold for promoting captures; <=0 uses DefaultDuplicateThreshold.
	DuplicateThreshold float64
}

// Change records a rewritten fact.
type Change struct {
	Before Record
	After  Record
}

// Report is the changelog of a consolidation pass — the "what the brain did"
// record surfaced in the UI and used for auditing/reverting.
type Report struct {
	Scope            Scope
	Promoted         []Record // facts distilled from captures
	CapturesConsumed []string // capture IDs removed after distillation
	Rewritten        []Change // facts whose text/category changed
	Abstracted       []Record // new general facts from reorganization
	Deleted          []Record // facts removed by reorganization
	Decayed          []Record // facts pruned by decay
	Skipped          bool     // reorganization skipped (fingerprint unchanged)
	ReorgRefused     bool     // reorganization rejected by the over-deletion safety net
	Fingerprint      string   // new fingerprint of the reorganizable set
}

// minFactsForDeletionGuard is the floor below which the >50% over-deletion safety
// net does not apply (small sets can legitimately collapse a lot).
const minFactsForDeletionGuard = 8

// isProtected reports whether a record is exempt from reorganization and decay:
// pinned (core context), locked (hand-protected), or human-authored (ground truth).
func isProtected(r Record) bool {
	return r.Pinned || r.Locked || r.Source == SourceHuman
}

// Consolidate runs the "sleep" pass over one scope: distill episodic captures into
// durable facts, reorganize (merge/rewrite/abstract) the durable set, and decay
// stale low-value facts. It is conservative by construction — it never mutates
// pinned, locked or human-authored facts, never deletes more than half of a
// non-trivial set, and skips the LLM reorganization when nothing has changed. A
// nil Extractor restricts the pass to deterministic decay.
func Consolidate(ctx context.Context, store Store, ex Extractor, scope Scope, opts ConsolidateOptions) (Report, error) {
	dupThreshold := opts.DuplicateThreshold
	if dupThreshold <= 0 {
		dupThreshold = DefaultDuplicateThreshold
	}
	now := time.Now().UTC()
	rep := Report{Scope: scope}

	all, err := store.List(ctx, scope)
	if err != nil {
		return rep, err
	}

	var captures, durable []Record
	for _, r := range all {
		if r.Source == SourceCapture {
			captures = append(captures, r)
		} else {
			durable = append(durable, r)
		}
	}

	// 1) Promote episodic captures into durable facts.
	if ex != nil && len(captures) > 0 {
		promoted, consumed, perr := promoteCaptures(ctx, store, ex, scope, captures, durable, dupThreshold)
		if perr != nil {
			return rep, perr
		}
		durable = append(durable, promoted...)
		rep.Promoted = promoted
		rep.CapturesConsumed = consumed
	}

	// Partition: protected facts pass through untouched.
	var reorgInput []Record
	for _, r := range durable {
		if !isProtected(r) {
			reorgInput = append(reorgInput, r)
		}
	}

	// 2) Reorganize the non-protected durable set.
	rep.Fingerprint = fingerprint(reorgInput)
	if ex != nil && len(reorgInput) > 0 {
		if rep.Fingerprint == opts.PrevFingerprint {
			rep.Skipped = true
		} else {
			out, rerr := ex.Reorganize(ctx, toFacts(reorgInput))
			if rerr != nil {
				return rep, fmt.Errorf("memory: reorganize: %w", rerr)
			}
			applied, refused, aerr := applyReorg(ctx, store, now, reorgInput, out, &rep)
			if aerr != nil {
				return rep, aerr
			}
			rep.ReorgRefused = refused
			if !refused {
				reorgInput = applied
			}
		}
	}

	// 3) Decay stale, low-value facts (deterministic; runs regardless of skip).
	if opts.Decay.MaxAge > 0 {
		kept := reorgInput[:0]
		for _, r := range reorgInput {
			if now.Sub(r.UpdatedAt) > opts.Decay.MaxAge && r.Uses < opts.Decay.MinUses {
				if err := store.Delete(ctx, r.ID); err != nil {
					return rep, err
				}
				rep.Decayed = append(rep.Decayed, r)
				continue
			}
			kept = append(kept, r)
		}
		reorgInput = kept
	}

	rep.Fingerprint = fingerprint(reorgInput)
	return rep, nil
}

func promoteCaptures(ctx context.Context, store Store, ex Extractor, scope Scope, captures, durable []Record, dupThreshold float64) (promoted []Record, consumed []string, err error) {
	episodes := make([]string, len(captures))
	captureIDs := make([]string, len(captures))
	for i, c := range captures {
		episodes[i] = c.Text
		captureIDs[i] = c.ID
	}
	cands, eerr := ex.Extract(ctx, episodes)
	if eerr != nil {
		return nil, nil, fmt.Errorf("memory: extract captures: %w", eerr)
	}
	if len(cands) == 0 {
		// A weak/empty extraction must not destroy raw episodic material — keep
		// the captures for a future pass rather than consuming them for nothing.
		return nil, nil, nil
	}
	existing := append([]Record(nil), durable...)
	for _, cand := range cands {
		text := strings.TrimSpace(cand.Text)
		if text == "" {
			continue
		}
		if _, dup := FindDuplicate(text, existing, dupThreshold); dup {
			continue
		}
		r := New(scope, text, cand.Category, SourceConsolidated)
		r.DerivedFrom = append([]string(nil), captureIDs...)
		if r.Category == CategoryIdentity {
			r.Pinned = true // identity facts are core context (Odysseus behavior)
		}
		if err := store.Put(ctx, r); err != nil {
			return nil, nil, err
		}
		existing = append(existing, r)
		promoted = append(promoted, r)
	}
	// Captures have been distilled; remove them so they aren't reprocessed.
	for _, id := range captureIDs {
		if err := store.Delete(ctx, id); err != nil {
			return nil, nil, err
		}
	}
	return promoted, captureIDs, nil
}

// applyReorg validates and applies an Extractor's reorganization. It drops
// invented IDs, enforces the over-deletion safety net, then applies rewrites,
// deletions and new abstractions. On refusal it leaves the input untouched.
func applyReorg(ctx context.Context, store Store, now time.Time, input []Record, out []Fact, rep *Report) (applied []Record, refused bool, err error) {
	inputByID := make(map[string]Record, len(input))
	for _, r := range input {
		inputByID[r.ID] = r
	}
	outByID := make(map[string]Fact, len(out))
	var news []Fact
	for _, f := range out {
		if f.ID == "" {
			news = append(news, f)
			continue
		}
		if _, ok := inputByID[f.ID]; !ok {
			continue // invented ID — drop, never trust the model to mint IDs
		}
		outByID[f.ID] = f
	}

	// Over-deletion safety net: refuse if a non-trivial set would lose >half of
	// its facts. Survivors include both retained originals and newly abstracted
	// facts, so a legitimate merge (drop N originals, add M abstractions) is not
	// mistaken for mass deletion.
	survivors := len(outByID) + len(news)
	if len(input) >= minFactsForDeletionGuard && survivors*2 < len(input) {
		return input, true, nil
	}

	applied = make([]Record, 0, len(outByID)+len(news))
	for _, r := range input {
		f, kept := outByID[r.ID]
		if !kept {
			if err := store.Delete(ctx, r.ID); err != nil {
				return nil, false, err
			}
			rep.Deleted = append(rep.Deleted, r)
			continue
		}
		text := strings.TrimSpace(f.Text)
		if text != "" && (text != r.Text || (f.Category != "" && f.Category != r.Category)) {
			before := r
			r.Text = text
			if f.Category != "" {
				r.Category = f.Category
			}
			r.Source = SourceConsolidated
			r.UpdatedAt = now
			if err := store.Put(ctx, r); err != nil {
				return nil, false, err
			}
			rep.Rewritten = append(rep.Rewritten, Change{Before: before, After: r})
		}
		applied = append(applied, r)
	}
	for _, f := range news {
		text := strings.TrimSpace(f.Text)
		if text == "" {
			continue
		}
		nr := New(rep.Scope, text, f.Category, SourceConsolidated)
		if err := store.Put(ctx, nr); err != nil {
			return nil, false, err
		}
		rep.Abstracted = append(rep.Abstracted, nr)
		applied = append(applied, nr)
	}
	return applied, false, nil
}

func toFacts(rs []Record) []Fact {
	out := make([]Fact, len(rs))
	for i, r := range rs {
		out[i] = Fact{ID: r.ID, Text: r.Text, Category: r.Category}
	}
	return out
}

// fingerprint is an order-independent hash of the id/text/category of a record
// set — used to detect whether the reorganizable set changed between passes.
func fingerprint(rs []Record) string {
	items := make([][3]string, len(rs))
	for i, r := range rs {
		items[i] = [3]string{r.ID, r.Text, string(r.Category)}
	}
	sort.Slice(items, func(i, j int) bool { return items[i][0] < items[j][0] })
	h := sha256.New()
	for _, it := range items {
		for _, s := range it {
			h.Write([]byte(s))
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
