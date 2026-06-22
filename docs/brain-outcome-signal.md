# ADR: The outcome signal — making *use* (not just injection) change memory

Status: Accepted · 2026-06-21 · Implements [brain-learning-dynamics.md](brain-learning-dynamics.md)
open decisions #2 (positive emitter) and #5 (confidence calibration). Sibling to
[brain-memory.md](brain-memory.md).

> **One-line thesis.** The brain already *strengthens a fact when it is shown* (recall bumps
> `Uses`). It did not yet *strengthen a fact when it actually helped*, nor let earned trust
> change what an agent does. This ADR closes that loop: a confirmed-useful recall is a
> first-class positive outcome that raises trust, and high-confidence preferences graduate
> from soft context into an **operating contract** the agent acts on by default.

## Context

[brain-learning-dynamics.md](brain-learning-dynamics.md) shipped D1 (two-factor strength),
D2 (reconsolidating recall + the `MemoryFlag` *contradiction* signal), D5 (interference) and
D6 (spaced review). Two things were still missing, and they are the keystone the rest waits on:

1. **A positive outcome signal.** D2's `MemoryFlag` is the *negative* half of reconsolidation
   ("this recalled fact was wrong"). There was no *positive* half ("this recalled fact was
   right and I acted on it"). Injection bumps `Uses`, but injection is "shown", not "helped" —
   on the live corpus `Uses` is `0` everywhere and `RetrievalStrength`/strength-weighted decay
   are starved of signal (tech-debt P2). RFC open decision #2.

2. **Earned trust doesn't change behavior.** High-confidence preferences are injected as
   *background context* ("verify before relying on specifics"), the same framing as a freshly
   inferred guess. Nothing lets a well-established preference become a *standing instruction*
   the agent follows without re-asking. RFC open decision #5 (calibration) is the missing input.

## Decision

### 1. What counts as a "good outcome" (the simplest defensible definition)

> A recalled fact had a **good outcome** when an agent that saw it **explicitly acknowledges it
> was used / correct / helpful** for the task, via the new `MemoryUsed` tool. A **bad outcome**
> is an explicit `MemoryFlag` contradiction (D2, already shipped). Mere injection is the
> **weakest** signal and remains `Uses`/`BumpUses`; it is "shown", not "helped".

We deliberately choose the **explicit agent acknowledgement** over an automatic LLM judge or a
"survived the session" heuristic:

- **Cheapest viable** (the RFC's bar): deterministic, no model call on the hot path, symmetric
  with `MemoryFlag`, reuses the same scope-checked adapter machinery.
- **Honest**: the agent affirming "I used this and it was correct" is a real, attributable
  signal — not an inference about intent, and not the near-tautology of "nothing contradicted it".

The known cost — like `MemoryFlag`, it depends on the agent *choosing* to call the tool — is
mitigated by **actively prompting for it** in the recall framing (`MemorySearch` output, and the
`RecallPreamble` system block that explains the per-turn `<brain>` envelope, both tell the agent
to call `MemoryUsed`/`MemoryFlag`). The `<brain>` card additionally exposes per-fact
**Helpful / Outdated** buttons, so a human can drive the same `Confirm`/`Flag` signal from the
transcript. An automatic
turn-end judge (RFC decision #2's other branch) remains a future option; this ADR does not
foreclose it, it ships the cheap precursor first, exactly as D2 shipped `MemoryFlag` before any
automatic contradiction detector.

### 2. A positive outcome raises confidence, with a ceiling below human ground-truth

Each `MemoryUsed` acknowledgement (`memory.MarkHelped`):

- increments a new **`Record.Helped`** counter (distinct from `Uses` — corroborated-useful, not
  merely shown),
- stamps `LastUsedAt` (it *was* just used — feeds retrieval-strength recency),
- raises `ConfidenceScore` toward a **corroboration ceiling of 0.95**, closing half the
  remaining gap each time (`0.8 → 0.875 → 0.9125 → …`). It **never reaches `1.0`**: ground truth
  is *asserted by a human* (`Confirm`), not *earned by agent corroboration*.

This closes RFC decision #5: trust is now **calibrated by outcome**, not frozen at encode time.
A contradiction (`MarkContradicted`) still knocks the score down into the review band (0.4), so
the loop is bidirectional. Protected facts (pinned / locked / human) keep their score — we never
let an agent re-rate what a human asserted.

`Helped` also feeds `StorageStrength` (weighted higher than a bare injection), so a
corroborated fact ranks higher and resists decay.

**Guardrail against self-certification (RFC Non-goals: false memories).** The ceiling < 1.0, the
gap-closing step (no single call can jump a fact more than halfway to the ceiling), the
protected-fact exemption, and the fact that *text is never rewritten on outcome* together bound
how far an agent can inflate its own facts. The worst case is an agent talking a *wrong* inferred
fact up toward 0.95 — but a later `MemoryFlag` reverses it, and the human review queue and
markdown provenance remain the backstop.

### 3. High-confidence preferences become an operating contract

`Service.OperatingContract` selects **preference** facts with `ConfidenceScore ≥ 0.85`
(`ActOnConfidence`) and not flagged for review, and injects them into the system preamble under
directive framing — *"standing instructions you should follow without re-asking"* — distinct from
the softer "background context, verify before relying" framing of pinned/recall blocks.

The 0.85 gate sits **above** the default inferred score (0.8), which gives the keystone property:

> A *freshly inferred* preference does **not** yet drive behavior. It earns that authority by
> being **human-confirmed** (`Confirm` → 1.0) or **corroborated by outcome** (`MemoryUsed` →
> past 0.85). Low-confidence preferences stay advisory and remain in the existing confirm queue.

So a cross-project preference (0.65) needs two confirmations to act-on; an ordinary inferred one
(0.8) needs one; a human-authored one (1.0) acts immediately. Trust is *earned*, then *acted on*.

## Consequences

- **Closed loop, end to end:** retrieve → (used? contradicted?) → confidence/strength change →
  recall ranking, decay resistance, and **behavior** (the operating contract) change. This is the
  north-star "retrieval and outcome should change memory", now including outcome.
- **Unblocks D3 (salience-gated consolidation) and #5 (calibration):** both wanted exactly this
  positive signal; `Helped`/`ConfidenceScore` deltas are now available to drive encode priority.
- **Additive & reversible:** `Helped` is a new omitempty markdown field (pre-existing facts read
  as `Helped: 0`); no DB or wire schema change. The contract is a new preamble block; sessions
  outside a project, or with no qualifying prefs, see no change.
- **Remaining gap:** the signal is still agent-volunteered, so it is only as good as agent
  discipline; verifying real-world call rate (and deciding whether to add an automatic judge)
  needs a live multi-turn session — tracked in tech-debt.

## Key constants (all in `internal/memory`)

| constant | value | meaning |
|---|---|---|
| `CorroborationCeiling` | 0.95 | max confidence reachable by outcome corroboration (< ground truth) |
| `corroborationGapClose` | 0.5 | fraction of the gap-to-ceiling closed per **explicit** positive outcome |
| `AutoCorroborationGapClose` | 0.25 | fraction closed per **automatically-inferred** positive outcome (the emitter) |
| `helpedUseWeight` | 2 | how many injections a single `Helped` is worth in `StorageStrength` |
| `ActOnConfidence` | 0.85 | preference confidence at/above which a fact joins the operating contract |

---

# ADR addendum: the automatic outcome emitter — closing the loop without the agent

Status: Accepted · 2026-06-22 · Implements [brain-learning-dynamics.md](brain-learning-dynamics.md)
open decision #2's remaining *automatic* branch. Extends the ADR above.

> **One-line thesis.** The explicit emitters (`MemoryUsed`/`MemoryFlag`) shipped first but are
> *agent-volunteered* — agents rarely call them, so `helped` is `0` across the live corpus and the
> whole outcome loop is starved (tech-debt P2). This addendum ships the **automatic** emitter: a
> session-end pass that reads the finished transcript and emits the same outcome signal itself, so
> the loop self-feeds whether or not the agent cooperates.

## Context

The base ADR deliberately shipped the cheap explicit precursor and left the automatic judge as
future work — "the durable fix is the *automatic* emitter" (tech-debt P2; RFC-LD decision #2's open
branch). With recall already tracking the injected fact-ids and `LearnFromTranscript` already
proving the session-end auto-encode pattern, the precursor work is done and the automatic twin is
the single highest-leverage step left toward the north star (a self-improving brain).

## Decision

### 1. Trigger: the session-end transcript pass (twin of auto-encode)

The emitter runs from the **existing `onSessionEnd` hook** (fired on session delete with the
captured transcript), composed alongside auto-encode under one closure. Rationale over the
scheduled-consolidation alternative: the transcript is the evidence, and it is gone after delete —
running anywhere else would mean separately capturing/persisting per-session injected-id sets.
Opt-in behind a model, off by default, best-effort, exactly like `LearnFromTranscript`.

### 2. Attribution: an LLM judge over the transcript + the injected facts (conservative)

This is RFC decision #2's genuinely-open design call — *how do you decide a recalled fact was
"used" from a transcript?* The v1 answer:

> Recover the facts recall **injected** this session by parsing the persisted `<brain>` envelopes
> from the prompt events (the exact blocks `RecallBlock` wrote and the agent saw), fetch the live
> records, and ask an LLM judge to rule **helped / contradicted / neutral** on each, from the
> transcript. **Neutral is the default**; the judge is instructed that *a fact merely being shown
> is not "helped"* and to prefer false-neutrals over false-positives.

Why transcript-parsing for the input set (not the in-memory `recalledIDs` seen-set): the hook fires
on delete, possibly a restart after the session ran, so only the **persisted** transcript is
reliable — and it is also what auto-encode already consumes. The judge is handed the facts
explicitly (id + text) and a transcript with the `<brain>` envelopes stripped (they would only
duplicate the fact list and waste budget). Guards: a verdict for an id that was not surfaced is
ignored (anti-hallucination); a since-deleted/edited-away fact is skipped; a defensive scope filter
means the emitter can never re-rate another project's memory.

We pick a **defensible v1 and ship it** rather than chase perfect attribution: the conservative
judge biases to false-negatives (it under-credits rather than over-credits), and every downstream
effect is bounded (ceiling < 1.0, gap-closing step, protected-fact exemption, human review queue).

### 3. An automatic `helped` weighs **half** an explicit one

A transcript judge is a weaker, secondhand signal than an agent calling `MemoryUsed` ("I was there,
it helped") or a human `Confirm`. So `MarkAutoHelped` uses `AutoCorroborationGapClose = 0.25`
(vs `0.5` for the explicit `MarkHelped`): same `Helped` increment and recency stamp, but it moves
confidence half as fast (`0.8 → 0.8375 → 0.866 → …`). A machine inference therefore needs **more**
corroborations to graduate a preference past `ActOnConfidence` into the operating contract — earned
trust stays earned, and a wrong inferred fact talked up by the judge climbs slower and is still
reversed by any later contradiction. (`memory.MarkHelpedWith` is the shared, gap-close-parameterized
core; `MarkHelped` is `MarkHelpedWith(…, 0.5)`.)

### 4. The negative half: auto-flag, high evidence bar

A clear contradiction routes through the **same `Flag`/`MarkContradicted`** path as the explicit
tool: it weakens the fact into the review band (0.4) and queues it for the human — never deletes,
always reversible. The judge is held to a high evidence bar (explicit user correction / reality
diverging) and the stored `ReviewNote` is prefixed **`auto:`** so the review queue shows the
contradiction came from the session-end judge, not an agent. Keeping the standard review-band demotion
(rather than a softer one) is deliberate: the demotion is what *surfaces* the fact for a human to
confirm/correct/drop, which is the whole point of a flag.

## Consequences

- **The loop self-feeds.** `helped`/`contradicted` now accrue without agent discipline, so
  strength-weighted decay and operating-contract graduation finally get real signal on the live
  corpus (the explicit tools remain, and stack — an agent's `MemoryUsed` still applies the stronger
  0.5 weight).
- **Additive & reversible.** No DB/wire schema change; reuses `Helped`/`ConfidenceScore`/`ReviewNote`.
  Off unless `AGENTIQUE_BRAIN_OUTCOME_MODEL` / `[brain] outcome-model` is set; a session outside a
  project, with no recall, or with an all-neutral verdict set sees no change.
- **Verified end-to-end (2026-06-22).** A live Haiku judge over a hand-built transcript correctly
  strengthened a clearly-used fact (`helped=1`, `0.8→0.8375`), flagged a clearly-contradicted one
  (`0.4`, note `auto: User corrected: database is SQLite, not Postgres`), and left a merely-shown
  fact untouched. Wiring activates against an isolated copy of the live 1444-fact brain
  (`brain: auto-outcome emitter enabled`).
- **Remaining gap.** Real-world precision/recall of the judge over *organic* sessions (not authored
  ones) still needs a live multi-turn soak; the weights (`0.25`, the high bar) are first calibrations.
  Tracked in tech-debt P2.

## Key additions (code)

| symbol | where | role |
|---|---|---|
| `memory.MarkHelpedWith` / `AutoCorroborationGapClose` | `internal/memory/reconsolidate.go` | gap-close-parameterized positive outcome; the 0.25 auto weight |
| `brain.OutcomeJudge` / `ClaudeOutcomeJudge` | `internal/brain/outcome.go` | the judge contract + its Claude implementation (schema-constrained) |
| `brain.Service.ApplyOutcomesFromTranscript` | `internal/brain/outcome.go` | orchestration: recover injected ids → fetch (scoped) → judge → apply |
| `brain.Service.MarkAutoHelped` | `internal/brain/brain.go` | the gentler positive entry point |
| `AGENTIQUE_BRAIN_OUTCOME_MODEL` / `[brain] outcome-model` | `server.go` / `config.go` | opt-in model (env wins over file), composed onto `onSessionEnd` |
