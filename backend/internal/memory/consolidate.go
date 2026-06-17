package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Candidate is a proposed durable fact produced by an Extractor from raw episodes.
type Candidate struct {
	Text     string   `json:"text"`
	Category Category `json:"category"`
}

// Fact is the minimal id+text+category view of a Record handed to an Extractor's
// Reorganize step. Protected metadata (pinned/locked/uses/source/timestamps) is
// never exposed to the model, so reorganization cannot tamper with it.
type Fact struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Category Category `json:"category"`

	// Community is a topic-cluster id (from DetectCommunities) used purely as a
	// batching hint: a cluster-aware Extractor groups facts of the same community
	// into one Reorganize call so related facts can merge even across a large
	// scope. It is NEVER shown to the model and is omitted from the wire plan when
	// zero — it carries no semantic meaning the apply step depends on.
	Community int `json:"community,omitempty"`
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
	// ConfidenceWeighted sharpens decay by scaling each fact's effective MaxAge by
	// its confidence score (RFC P2): a low-confidence fact must go stale for a shorter
	// time before it decays, so the brain forgets what it is least sure about first. A
	// score-1.0 fact gets the full MaxAge; an AMBIGUOUS fact (score < 0.55) barely
	// over half of it. Ground-truth/EXTRACTED facts are human-authored and therefore
	// already exempt from decay entirely (isProtected). Off by default, like decay.
	ConfidenceWeighted bool
	// StrengthWeighted sharpens decay by storage strength (RFC-LD D1): a well-
	// established fact (high confidence / many uses / deep provenance) keeps the full
	// MaxAge, a weakly-held one decays sooner. It also measures staleness from
	// LastUsedAt (disuse) rather than UpdatedAt (last edit), so a fact recalled
	// recently is protected even if old. Off by default, like decay. Composes with
	// ConfidenceWeighted (both factors multiply).
	StrengthWeighted bool
}

// effectiveMaxAge returns the staleness threshold for a record under this policy:
// the configured MaxAge, scaled down by the record's confidence score
// (ConfidenceWeighted) and/or storage strength (StrengthWeighted) so the brain
// forgets what it holds least firmly first. The factors compose multiplicatively.
func (d DecayPolicy) effectiveMaxAge(r Record) time.Duration {
	factor := 1.0
	if d.ConfidenceWeighted {
		score := NormalizeConfidence(r).ConfidenceScore
		if score <= 0 || score > 1 {
			score = DefaultInferredScore
		}
		factor *= score
	}
	if d.StrengthWeighted {
		factor *= StorageStrength(r)
	}
	if factor == 1.0 {
		return d.MaxAge
	}
	return time.Duration(float64(d.MaxAge) * factor)
}

// staleAge returns how long a record has gone without being touched, for decay. With
// StrengthWeighted this is disuse (time since LastUsedAt, falling back to UpdatedAt);
// otherwise it is time since the last content edit, preserving pre-D1 behavior.
func (d DecayPolicy) staleAge(r Record, now time.Time) time.Duration {
	if d.StrengthWeighted {
		return now.Sub(lastSeen(r))
	}
	return now.Sub(r.UpdatedAt)
}

// ConsolidateOptions configures a consolidation pass.
type ConsolidateOptions struct {
	// PrevFingerprint lets the pass skip the (expensive) Reorganize step when the
	// reorganizable set is byte-for-byte unchanged since the last pass.
	PrevFingerprint string
	// Force runs Reorganize even when the fingerprint is unchanged. Needed to
	// re-tidy a scope after the prompt or chunking algorithm changes (the set is
	// the same but the model would now produce a different, better result).
	Force bool
	Decay DecayPolicy
	// DuplicateThreshold for promoting captures; <=0 uses DefaultDuplicateThreshold.
	DuplicateThreshold float64
	// MinSurvivorRatio is the fraction of a non-trivial reorganizable set that must
	// survive (retained + abstracted), below which the over-deletion safety net
	// refuses the reorganization. <=0 (or >1) uses defaultMinSurvivorRatio (0.5). A
	// lower value (e.g. an aggressive Tidy) lets a bloated scope collapse further in
	// one pass while still catching a model that nukes nearly everything. The value
	// is captured into the Plan so preview and apply enforce an identical guard.
	MinSurvivorRatio float64
	// MinPromotionScopes is the cross-scope guardrail for global promotion (RFC P5):
	// only facts in a topic community spanning at least this many distinct project
	// scopes are offered to the Promoter. <=0 uses DefaultMinPromotionScopes (2).
	// Global-only — single-scope passes ignore it.
	MinPromotionScopes int
	// PrevManifest is the per-scope content-hash manifest from the previous global
	// pass (ScopeManifest). When it matches the current promotable state and Force is
	// unset, PlanGlobalPromotion skips the model run — nothing has changed across any
	// project, so there is nothing new to promote. Global-only.
	PrevManifest map[string]string
	// DryRun runs the full pass — including the LLM calls — and populates the
	// Report with every change it WOULD make, but writes nothing to the Store. Used
	// for safe previews on live, session-shared memory.
	DryRun bool
	// Progress, when set, is called after each batch of a chunked LLM pass with the
	// number of batches completed and the total. Lets a host report live progress.
	Progress func(done, total int)
	// OnError, when set, is called for a recoverable per-batch LLM error; the pass
	// logs it via the host and continues with the remaining batches instead of
	// aborting. A context cancellation always aborts regardless.
	OnError func(error)
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

// minFactsForDeletionGuard is the floor below which the over-deletion safety net
// does not apply (small sets can legitimately collapse a lot).
const minFactsForDeletionGuard = 8

// defaultMinSurvivorRatio is the share of a non-trivial reorganizable set that
// must survive a reorganization; below it the over-deletion safety net refuses.
// 0.5 reproduces the original "never lose more than half" guard.
const defaultMinSurvivorRatio = 0.5

// normalizeMinSurvivorRatio clamps a configured survivor ratio into (0,1],
// defaulting an unset/invalid value to the conservative 0.5.
func normalizeMinSurvivorRatio(r float64) float64 {
	if r <= 0 || r > 1 {
		return defaultMinSurvivorRatio
	}
	return r
}

// isProtected reports whether a record is exempt from reorganization and decay:
// pinned (core context), locked (hand-protected), or human-authored (ground truth).
func isProtected(r Record) bool {
	return r.Pinned || r.Locked || r.Source == SourceHuman
}

// Plan is the serializable output of the LLM phase of a consolidation pass: the
// model's proposed reorganization plus any captures it distilled, together with a
// fingerprint of the set it was computed against. It is sufficient to apply the
// pass deterministically (no further model calls), so a UI can preview a plan and
// then apply EXACTLY what was shown — the expensive, non-deterministic model step
// runs once, at plan time.
type Plan struct {
	Scope             Scope       `json:"scope"`
	InputFingerprint  string      `json:"inputFingerprint"`  // fingerprint of the reorganizable set at plan time
	Reorganized       []Fact      `json:"reorganized"`       // ex.Reorganize output
	ReorganizeRan     bool        `json:"reorganizeRan"`     // a reorganization was performed (vs skipped / no extractor)
	ReorganizeSkipped bool        `json:"reorganizeSkipped"` // skipped because the set was unchanged since the last pass
	Promoted          []Candidate `json:"promoted"`          // ex.Extract output distilled from captures
	CaptureIDs        []string    `json:"captureIds"`        // captures considered; consumed on apply when promotion yields facts
	MinSurvivorRatio  float64     `json:"minSurvivorRatio,omitempty"` // over-deletion guard floor in effect for this plan (apply mirrors preview)
}

// ErrStalePlan is returned by ApplyPlan when the scope's reorganizable set changed
// since the plan was computed, so applying it could clobber newer edits. The caller
// should re-plan (re-preview) rather than apply blindly.
var ErrStalePlan = errors.New("memory: consolidation plan is stale; re-plan")

// PlanConsolidation runs the LLM phase ONLY: it distills captures and computes the
// reorganization, returning a Plan and writing nothing. A nil Extractor yields an
// empty plan (deterministic decay still happens in ApplyPlan). Promoted captures
// are reorganized on the NEXT pass, not this one, so the plan stays a pure function
// of the current durable set.
func PlanConsolidation(ctx context.Context, store Store, ex Extractor, scope Scope, opts ConsolidateOptions) (Plan, error) {
	p := Plan{Scope: scope, MinSurvivorRatio: normalizeMinSurvivorRatio(opts.MinSurvivorRatio)}
	all, err := store.List(ctx, scope)
	if err != nil {
		return p, err
	}
	var captures, durable []Record
	for _, r := range all {
		if r.Source == SourceCapture {
			captures = append(captures, r)
		} else {
			durable = append(durable, r)
		}
	}

	// LLM: distill captures into candidate facts (no write).
	if ex != nil && len(captures) > 0 {
		episodes := make([]string, len(captures))
		p.CaptureIDs = make([]string, len(captures))
		for i, c := range captures {
			episodes[i] = c.Text
			p.CaptureIDs[i] = c.ID
		}
		cands, eerr := ex.Extract(ctx, episodes)
		if eerr != nil {
			return p, fmt.Errorf("memory: extract captures: %w", eerr)
		}
		p.Promoted = cands
	}

	// LLM: reorganize the non-protected durable set (no write).
	var reorgInput []Record
	for _, r := range durable {
		if !isProtected(r) {
			reorgInput = append(reorgInput, r)
		}
	}
	p.InputFingerprint = fingerprint(reorgInput)
	if ex != nil && len(reorgInput) > 0 {
		if !opts.Force && p.InputFingerprint == opts.PrevFingerprint {
			p.ReorganizeSkipped = true
		} else {
			out, rerr := ex.Reorganize(ctx, factsForReorg(reorgInput))
			if rerr != nil {
				return p, fmt.Errorf("memory: reorganize: %w", rerr)
			}
			p.Reorganized = out
			p.ReorganizeRan = true
		}
	}
	return p, nil
}

// ApplyPlan applies a previously computed Plan DETERMINISTICALLY — no model calls.
// It refuses with ErrStalePlan if the reorganizable set changed since the plan was
// made. With opts.DryRun set it builds the full changelog but writes nothing, which
// is how a preview is rendered from the same plan that apply will replay. It never
// mutates pinned, locked or human-authored facts and enforces the over-deletion
// safety net.
func ApplyPlan(ctx context.Context, store Store, scope Scope, p Plan, opts ConsolidateOptions) (Report, error) {
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
	var durable []Record
	for _, r := range all {
		if r.Source != SourceCapture {
			durable = append(durable, r)
		}
	}
	var reorgInput []Record
	for _, r := range durable {
		if !isProtected(r) {
			reorgInput = append(reorgInput, r)
		}
	}
	// Staleness guard: the plan was computed against a specific set; if it changed
	// (manual edits, another pass) applying could clobber newer state.
	if fingerprint(reorgInput) != p.InputFingerprint {
		return rep, ErrStalePlan
	}

	// 1) Promote the captures the plan distilled (deterministic write).
	if len(p.Promoted) > 0 {
		promoted, consumed, perr := writePromoted(ctx, store, scope, p.Promoted, p.CaptureIDs, durable, dupThreshold, opts.DryRun)
		if perr != nil {
			return rep, perr
		}
		rep.Promoted = promoted
		rep.CapturesConsumed = consumed
	}

	// 2) Apply the reorganization the plan proposed.
	if p.ReorganizeSkipped {
		rep.Skipped = true
	} else if p.ReorganizeRan {
		applied, refused, aerr := applyReorg(ctx, store, now, reorgInput, p.Reorganized, normalizeMinSurvivorRatio(p.MinSurvivorRatio), &rep, opts.DryRun)
		if aerr != nil {
			return rep, aerr
		}
		rep.ReorgRefused = refused
		if !refused {
			reorgInput = applied
		}
	}

	// 3) Decay stale, low-value facts (deterministic; runs regardless of skip). With
	// a confidence-weighted policy the staleness threshold shrinks for low-confidence
	// facts, so the brain forgets what it is least sure about first.
	if opts.Decay.MaxAge > 0 {
		kept := reorgInput[:0]
		for _, r := range reorgInput {
			if opts.Decay.staleAge(r, now) > opts.Decay.effectiveMaxAge(r) && r.Uses < opts.Decay.MinUses {
				if !opts.DryRun {
					if err := store.Delete(ctx, r.ID); err != nil {
						return rep, err
					}
				}
				rep.Decayed = append(rep.Decayed, r)
				continue
			}
			kept = append(kept, r)
		}
		reorgInput = kept
	}

	rep.Fingerprint = fingerprint(reorgInput)

	// Rebuild the scope's link graph from the settled set, then recompute topic
	// communities over those fresh edges (associative-recall + graph-view signal +
	// cluster-aware future passes). Both are derived metadata, so previews skip them.
	if !opts.DryRun {
		if _, err := RelinkScope(ctx, store, scope); err != nil {
			return rep, err
		}
		if _, err := AssignCommunities(ctx, store, scope); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// Consolidate runs the full "sleep" pass in one shot: plan (LLM) then apply. It is
// conservative by construction — it never mutates pinned, locked or human-authored
// facts, never deletes more than half of a non-trivial set, and skips the LLM
// reorganization when nothing has changed. A nil Extractor restricts the pass to
// deterministic decay. For a preview→apply UI, call PlanConsolidation and ApplyPlan
// separately so the model runs only once.
func Consolidate(ctx context.Context, store Store, ex Extractor, scope Scope, opts ConsolidateOptions) (Report, error) {
	p, err := PlanConsolidation(ctx, store, ex, scope, opts)
	if err != nil {
		return Report{Scope: scope}, err
	}
	return ApplyPlan(ctx, store, scope, p, opts)
}

// writePromoted turns already-extracted candidates into durable facts: it dedups
// them against the current durable set, writes the survivors (unless dryRun) and
// consumes the captures they came from. The LLM Extract that produced cands runs
// earlier, in PlanConsolidation, so this step is deterministic.
func writePromoted(ctx context.Context, store Store, scope Scope, cands []Candidate, captureIDs []string, durable []Record, dupThreshold float64, dryRun bool) (promoted []Record, consumed []string, err error) {
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
		if !dryRun {
			if err := store.Put(ctx, r); err != nil {
				return nil, nil, err
			}
		}
		existing = append(existing, r)
		promoted = append(promoted, r)
	}
	if dryRun {
		// Report what would be consumed without removing the raw captures.
		return promoted, captureIDs, nil
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
func applyReorg(ctx context.Context, store Store, now time.Time, input []Record, out []Fact, minSurvivorRatio float64, rep *Report, dryRun bool) (applied []Record, refused bool, err error) {
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

	// Over-deletion safety net: refuse if a non-trivial set would shrink below the
	// configured survivor ratio (default half). Survivors include both retained
	// originals and newly abstracted facts, so a legitimate merge (drop N originals,
	// add M abstractions) is not mistaken for mass deletion. An aggressive Tidy
	// lowers the ratio so a bloated scope can collapse further in one pass.
	survivors := len(outByID) + len(news)
	if len(input) >= minFactsForDeletionGuard && float64(survivors) < minSurvivorRatio*float64(len(input)) {
		return input, true, nil
	}

	applied = make([]Record, 0, len(outByID)+len(news))
	for _, r := range input {
		f, kept := outByID[r.ID]
		if !kept {
			if !dryRun {
				if err := store.Delete(ctx, r.ID); err != nil {
					return nil, false, err
				}
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
			// A reorganized fact stays an inference; reconcile its tier with the
			// (unchanged) score so the (tier, score) pair can't drift after the rewrite.
			r = NormalizeConfidence(r)
			r.UpdatedAt = now
			if !dryRun {
				if err := store.Put(ctx, r); err != nil {
					return nil, false, err
				}
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
		if !dryRun {
			if err := store.Put(ctx, nr); err != nil {
				return nil, false, err
			}
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

// factsForReorg is toFacts plus a community label per fact (from the Related graph
// + token-Jaccard), so a cluster-aware Extractor batches related facts into the
// same Reorganize call and they can merge across a large scope, not just within an
// arbitrary 100-fact slice. The labels never reach the model and don't affect the
// fingerprint (which hashes only id/text/category), so plans stay reproducible.
func factsForReorg(rs []Record) []Fact {
	comm := DetectCommunities(rs, DefaultCommunityThreshold)
	out := make([]Fact, len(rs))
	for i, r := range rs {
		out[i] = Fact{ID: r.ID, Text: r.Text, Category: r.Category, Community: comm[r.ID]}
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
