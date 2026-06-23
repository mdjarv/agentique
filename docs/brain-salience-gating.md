# ADR: Salience-gated consolidation — outcome decides what the brain keeps (RFC-LD D3)

Status: Accepted · 2026-06-23 · Implements [brain-learning-dynamics.md](brain-learning-dynamics.md)
proposal **D3** (salience-gated encoding & consolidation). Sibling to
[brain-outcome-signal.md](brain-outcome-signal.md) and [brain-memory.md](brain-memory.md).

> **One-line thesis.** Consolidation already *strengthens* a fact by outcome (a confirmed-useful
> recall raises confidence; a contradiction knocks it down). It did not yet let outcome decide
> **what consolidation keeps vs. forgets**: the reorganizer and decay treated a fact the loop has
> *proven useful* exactly like an unproven one, and a fact a session *contradicted* exactly like a
> healthy one. This ADR makes outcome-derived **salience** a first-class input to consolidation —
> a corroborated-useful fact resists churn and decay, a contradicted one becomes a decay candidate.

## Context

The outcome loop is closed end-to-end ([brain-outcome-signal.md](brain-outcome-signal.md)): recall
injects → an agent (`MemoryUsed`/`MemoryFlag`) or the session-end judge (`internal/brain/outcome.go`)
emits an outcome → `memory.MarkHelped` / `MarkContradicted` move `Record.Helped`, `LastUsedAt`,
`ConfidenceScore`, `ReviewNote`. RFC-LD D3 was *"unblocked on the signal side"* by exactly this work
(RFC sequencing #5): the outcome-derived fields it needs now exist and accrue.

What still treated every fact equally was **consolidation** — the pass that decides what to keep,
abstract, and decay (`internal/memory/consolidate.go`):

1. **The reorganizer is outcome-blind.** `Reorganize` sees only `{ID, Text, Category, Community}`.
   Whether a fact was *corroborated five times* or *contradicted last week* is invisible to the
   keep/merge/drop decision. A proven fact can be churned away as readily as a stale guess.

2. **Decay is only *indirectly* outcome-aware.** `DecayPolicy.StrengthWeighted` (D1) folds `Helped`
   into `StorageStrength`, so corroboration buys *some* decay resistance. But there is no signal
   that a **contradicted** fact should decay *sooner* — a flagged-wrong fact rides the same age
   curve as a healthy one, and the `MinUses` keep-alive even *protects* a wrong fact that happened
   to be injected a lot. "Shown often" is not "correct".

D1/D2 calibrated a fact's *trust and accessibility* by outcome. D3 is the missing third axis:
calibrate **consolidation's keep/forget decision** by outcome.

## Decision

### 1. A first-class, outcome-derived salience signal

A new pure function `memory.Salience(r Record) float64 ∈ [0,1]`, in `internal/memory/salience.go`,
derived **only** from the outcome fields (not provenance or recency — those are `StorageStrength`'s
job). It is deliberately a *separate axis* from storage strength:

- **Neutral baseline `0.5`** — a fact with no outcome history (`Helped == 0`, unflagged): merely
  *shown*, never *judged*. This is the pivot — see §3.
- **Corroboration raises it**, saturating: each `Helped` closes half the gap from neutral to `1.0`
  (`0.5 → 0.75 → 0.875 → 0.9375 → …`), mirroring the confidence gap-close in `reconsolidate.go` so
  the two outcome signals move in step.
- **Contradiction collapses it** to a `0.1` floor, regardless of `Helped`: a currently-flagged fact
  (`ReviewNote != ""`, the same contradiction signal `graph.go`/`brain.go` already key on) is a prime
  decay candidate. A later `Confirm`/edit clears the note and the floor with it.

Why a distinct function and not just a richer `StorageStrength`: storage strength answers *"how
well-established"* (confidence + cumulative use + provenance depth) and is monotonic — it only grows.
Salience answers *"did acting on this pay off or backfire"* and is **signed** — it can drop. Folding
a sharp contradiction penalty into the only-grows storage signal would corrupt it. Keeping them
orthogonal also keeps each call site honest about which axis it wants.

### 2. Salience gates two consolidation decisions

**(A) Salience-weighted decay — `DecayPolicy.SalienceWeighted` (opt-in, composes with the existing
two flags).** `effectiveMaxAge` multiplies in a salience factor `2·Salience(r)`:

| fact | salience | factor | effect |
|---|---|---|---|
| contradicted | 0.1 | 0.2× | decays ~5× sooner — a decay candidate |
| neutral (no outcome) | 0.5 | 1.0× | **unchanged** — the signal is inert until outcome moves it |
| corroborated ×1 | 0.75 | 1.5× | resists decay |

Plus: a contradicted fact **bypasses the `MinUses` keep-alive**. Today decay spares any fact with
`Uses ≥ MinUses`; under salience gating a *contradicted* fact decays no matter how often it was
shown — injection count cannot redeem a fact later found wrong. (The whole decay decision is now one
predicate, `DecayPolicy.shouldDecay`, so this exception lives in one readable place.)

**(B) Salience retention from reorganization — always on.** A **strongly-corroborated** fact
(`Helped ≥ 2`, not contradicted) joins the consolidation-protected set: it is held back from the
reorganizer entirely (excluded from the reorg input, exactly like a pinned/locked/human fact via the
new `reorgRetained` predicate). The model never sees it, so it can neither silently drop nor rewrite
it — and, being out of the reorg set, it is also exempt from decay. Outcome-proven facts earn the
same protection human-authorship grants.

This is the "keep" half of D3, and it is what gives D3 **live impact even with decay off** (decay is
opt-in and currently disabled everywhere — `DecayPolicy{}`; the *reorganizer* is what runs on the
live corpus). It is **dormant today and self-activating**: `Helped` is ≈0 across the live corpus, so
nothing is retained yet — but as the outcome loop accrues corroboration, genuinely proven facts stop
getting consolidated away. The `Helped ≥ 2` bar means a fact must be corroborated at least twice
(explicit *or* auto — `Helped` increments on both) before it freezes; one outcome is not enough.

### 3. Why neutral salience is `0.5`, not `0`

The pivot is load-bearing. `2·Salience` makes the decay factor `1.0` exactly at the neutral baseline,
so **`SalienceWeighted` is a no-op for any fact the outcome loop hasn't touched** — it only moves
facts that were actually judged. A `0` baseline would silently halve every unproven fact's lifespan,
turning an outcome *refinement* into an across-the-board *decay acceleration* — precisely the
"don't decay aggressively just to be brain-like" Non-goal. Salience earns its effect from evidence;
absence of evidence changes nothing.

### 4. What we deliberately did *not* do: tell the model

We do **not** expose salience to the `Reorganize` prompt (the way `Community` is *withheld*, salience
is *withheld* too). Salience gates consolidation **deterministically, in core** — retention is a hard
predicate, decay is a computed weight — never "ask the model to please honor this number." This is the
RFC's *feedback over fiat* and *no ungated mutation* stance: the outcome signal moves what the brain
keeps through mechanism, not through trusting an LLM to weigh it. It also keeps plans reproducible —
salience is derived from persisted fields, so a plan and its later apply compute the identical
retention set (a mismatch — e.g. an outcome landing mid-plan — trips the existing staleness guard and
forces a re-plan, which is correct).

## Consequences

- **The keep/forget decision is now outcome-calibrated**, completing the trio with D1 (trust/accessibility)
  and D2 (reconsolidation): retrieve → outcome → *what survives consolidation* changes, not just a fact's
  score. This is the north-star "retrieval and outcome should change memory", now reaching the
  abstracting store itself.
- **Live impact without enabling decay.** Reorg-retention (B) takes effect on the live reorganizer as
  corroboration accrues; the decay weighting (A) is ready for whenever decay is switched on.
- **Additive & reversible.** No new persisted field, no DB/wire schema change — `Salience` is derived
  from `Helped`/`ReviewNote`, both already persisted. `SalienceWeighted` defaults off and composes
  multiplicatively with `ConfidenceWeighted`/`StrengthWeighted`. Reorg-retention only ever *keeps* a
  fact (the safe direction): worst case is a proven fact that escapes a beneficial merge and coexists
  with its abstraction until a later pass/dedup tidies it — we never lose proven signal.
- **Guardrails inherited.** The contradiction floor keys on the same `ReviewNote` flag a human can
  clear via `Confirm`; protected facts (pinned/locked/human) are unaffected; markdown provenance and
  the review queue remain the backstop.
- **Remaining gap / future.** The retention bar (`Helped ≥ 2`) and decay multiplier slope (`2·`) are
  first calibrations — they want the same live multi-turn soak the outcome-judge weights do
  (tech-debt P2). D4 (episodic staging + replay) is the natural next step: salience is exactly the
  signal a replay pass would prioritise by.

## Key constants & symbols (all in `internal/memory`)

| symbol | value / where | role |
|---|---|---|
| `Salience(r)` | `salience.go` | outcome-derived salience ∈ [0,1] |
| `neutralSalience` | `0.5` | baseline for a fact with no outcome history (the decay-factor pivot) |
| `contradictedSalience` | `0.1` | floor a flagged fact collapses to (decay candidate) |
| `salienceHelpedGapClose` | `0.5` | gap-to-1.0 a single `Helped` closes (mirrors confidence gap-close) |
| `salienceRetentionHelped` | `2` | `Helped` at/above which a fact is retained from reorganization |
| `DecayPolicy.SalienceWeighted` | `consolidate.go` | opt-in: fold salience into `effectiveMaxAge` + contradicted `MinUses` bypass |
| `reorgRetained(r)` | `consolidate.go` | `isProtected(r) ‖ isStronglyCorroborated(r)` — held back from the reorganizer |
| `DecayPolicy.shouldDecay(r, now)` | `consolidate.go` | the single decay-decision predicate (houses the contradicted bypass) |
