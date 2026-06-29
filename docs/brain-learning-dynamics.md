# RFC: Learning dynamics for the brain

Status: Draft · 2026-06-17 · Sibling to [brain-memory.md](brain-memory.md) and
[brain-graph-layer.md](brain-graph-layer.md)

> **Framing.** [brain-graph-layer.md](brain-graph-layer.md) gave the brain a *structure* —
> a link graph, schemas (communities), hubs (centrality), associative recall, and confidence
> as metamemory. This RFC is about the *dynamics* that structure still lacks: the feedback
> loops that make biological memory adaptive. The through-line of every proposal below is the
> same — **retrieval and outcome should change memory.** Today the brain is write-once and
> manually maintained; the structure is brain-like, the learning is not yet.
>
> **Progress (2026-06-17):** **D1 (two-factor strength)**, **D2 (reconsolidating recall +
> MemoryFlag agent tool)**, **D5 (interference detection)** and **D6 (spaced review)** are
> implemented — see `memory/{strength,reconsolidate,interference}.go`, `Record.LastUsedAt` /
> `Record.ReviewNote`, `BumpUses`, `DecayPolicy.StrengthWeighted`, the `MemoryFlag` MCP tool,
> and the `dueForReview` / `interference` lists in `brain/graph.go`. **D3 (salience-gated
> consolidation)** and **D4 (episodic staging + replay)** remain — both wait on the outcome
> signal (open decision #2); D2's `MemoryFlag` is the first concrete instance of that signal.
>
> **Progress (2026-06-21):** the **positive outcome signal** shipped — `MemoryUsed`
> (positive twin of `MemoryFlag`), `memory.MarkHelped`, `Record.Helped`, and confidence
> **calibration** (a confirmed-useful recall raises `ConfidenceScore` toward a 0.95
> corroboration ceiling). This **resolves the positive half of decision #2** and **decision
> #5 (calibration)**, and feeds part 2: high-confidence preferences now drive behavior via
> `Service.OperatingContract`. See [brain-outcome-signal.md](brain-outcome-signal.md). D3/D4
> can now build on `Helped` / outcome-derived salience.
>
> **Progress (2026-06-22):** decision #2's remaining *automatic* branch shipped — a session-end
> **transcript judge** (`internal/brain/outcome.go`) emits the outcome signal without the agent,
> so the loop self-feeds on the live corpus. See the ADR addendum in
> [brain-outcome-signal.md](brain-outcome-signal.md).
>
> **Progress (2026-06-23):** **D3 (salience-gated consolidation)** shipped — an outcome-derived
> `memory.Salience` now gates what consolidation keeps vs. forgets: a strongly-corroborated fact
> (`Helped ≥ 2`) is retained from the reorganizer (always on, gives D3 live impact even with decay
> off), and `DecayPolicy.SalienceWeighted` makes a contradicted fact a decay candidate while
> corroboration resists decay. See [brain-salience-gating.md](brain-salience-gating.md). **D4
> (episodic staging + replay)** is now the only remaining proposal.

## Motivation

The brain already embodies the *architecture* of several real theories of human memory:

- **Complementary Learning Systems** (McClelland, McNaughton & O'Reilly 1995) — a fast episodic
  store + a slow abstracting store, which is how brains resolve stability-vs-plasticity. Our
  `capture` → `consolidate` → durable-fact pipeline is exactly this shape.
- **Systems consolidation** — the consolidation pass abstracts rules and decays specifics, mirroring
  hippocampus→neocortex transfer.
- **Spreading activation** (Collins & Loftus 1975) — associative recall (graph-layer P1) folds in
  one-hop `Related` neighbours, almost literally.
- **Schemas / semantic hubs** — communities (P3) and centrality god-nodes (P2).
- **Metamemory** — confidence tiers (P2).

What's missing is the *physiology*. Once a fact is written, its trust and strength change only via
an explicit human confirm/edit (`Service.Confirm`) or the opt-in age/use `DecayPolicy`. **Recall is
read-only** — it bumps a flat `Record.Uses` counter (`Service.MarkUsed`, called once from
`mcp.go` on `MemorySearch`) and nothing else; on the live corpus that counter is `0` on every
record. The richest signals an agent generates — *did this memory help, did the session contradict
it, was the outcome notable?* — are discarded. Human-memory research names the loops we're throwing
away, and most map onto fields we already have.

## Principles

Carried from [brain-memory.md](brain-memory.md):

- **Markdown files are the source of truth.** Crucially, this means — unlike a brain — we never
  have to lose the verbatim trace. We abstract for recall *and* keep provenance for audit. The
  dynamics below must never overwrite the source signal they derive from.
- **Pure Go, liftable core; cognition is offline.** The hot path (recall) stays cheap. Strengthening
  is a cheap post-recall write; replay/abstraction happens in scheduled consolidation.

New to this RFC:

- **Feedback over fiat.** Prefer mechanisms where *use and outcome* update a fact over static values
  stamped at encode time.
- **Don't cargo-cult biology.** Borrow mechanisms that raise signal and trust. The agent is not
  capacity-limited and need not inherit the brain's lossy reconstruction or its false-memory failure
  mode (see Non-goals).

## Proposals

### D1 — Two-factor strength (storage vs. retrieval) ✅ implemented · disuse-confidence aging shipped (Band 1 M5)

- **Status (Band 1 M5).** Disuse now also erodes **confidence** (not just retrieval strength):
  `memory.EffectiveConfidence(r, now)` = stored `ConfidenceScore` decayed by time-since-last-use on
  a volatility half-life (slow 90d / ephemeral 14d / evergreen ∞), clamped to an evidence floor.
  Computed at recall, **never persisted on a nudge**; persisted exactly once at the **archive**
  transition (`Lifecycle=archived` — cold tier, out of recall, restorable, never deleted). Human/
  pinned/locked/evergreen are exempt. Opt-in via `archive-after`, with a recall-cliff gate +
  backfill clock-reset for deploy safety.
- **Principle.** Bjork & Bjork's *New Theory of Disuse*: a memory has two strengths — **storage**
  (how deeply learned; ~permanent) and **retrieval** (how accessible right now; decays with disuse).
  Retrieval practice raises both; only retrieval strength fades.
- **Today.** A single flat `Record.Uses` int conflates "important" with "recently used", and recall
  ranking (`recall.go`) leans on a recency tiebreaker + `Uses`.
- **Proposal.** Derive two signals. *Storage* ≈ corroboration (how many independent
  sessions/encodes produced this fact), `DerivedFrom` depth, and graph centrality — durable.
  *Retrieval* ≈ a recency-decaying accessibility score, bumped on use. Recall ranks primarily on
  retrieval; decay (D6) targets facts low in **both**; a successful use raises both.
- **Lives in.** Core (`recall.go` ranking, a `confidence.go`/`record.go` field or a derived
  helper). No new external signal required — both are computable from data we already persist.
- **Safety.** Pure ranking/decay inputs; previewable like every consolidation change.

### D2 — Reconsolidation: make recall a *write* (keystone) ✅ implemented

- **Principle.** The two most robust results in learning science: the **testing effect** (Roediger &
  Karpicke — retrieval strengthens more than re-reading) and **reconsolidation** (Nader et al. — a
  memory becomes labile when retrieved; that is the window to update it).
- **Today.** Recall only bumps `Uses`. A fact an agent acted on — and a session that *contradicted*
  it — leave no trace.
- **Proposal.** On recall, write back: a fact recalled-and-not-corrected → strengthen (D1 retrieval
  ↑, reset its decay clock); a fact recalled-and-contradicted → weaken / flag for confirm. The cheap
  half (strengthen on injection) needs no new signal beyond "was recalled". The valuable half needs a
  *contradiction/outcome* signal (see Open decisions).
- **Lives in.** Core exposes the update; glue supplies the signal (session-end judge, an explicit
  agent tool, or transcript analysis in auto-encode).
- **Safety.** Auto-updates must be gated and provenance-tracked (Non-goals: false memories). Never
  rewrite a fact's *text* on recall — only its strength/flags; text changes stay in preview→apply.

> This is the smallest mechanism with the largest behavioural change, and it **subsumes** the
> open "dynamic confidence" and "capture corrections" gaps noted in brain-memory.md.

### D3 — Salience-gated encoding & consolidation ✅ implemented

- **Principle.** Brains preferentially consolidate **rewarded, surprising, or aversive** experiences
  (dopamine-gated consolidation; amygdala emotional tagging) — not everything uniformly.
- **Today.** Encode is roughly uniform (the extraction bias prefers "fewer, broader" + "surprising
  gotcha", a crude salience proxy, but it's per-chunk and unweighted afterward).
- **Proposal.** Tag facts with an outcome-derived salience (a gotcha that *caused a costly bug* >> a
  routine convention) and let it drive encode priority and decay resistance.
- **Shipped.** `memory.Salience(r)` (`salience.go`) derives a [0,1] outcome score from `Helped` /
  `ReviewNote`, orthogonal to `StorageStrength`. It gates consolidation two ways: a
  strongly-corroborated fact (`Helped ≥ 2`) is held back from the reorganizer (`reorgRetained`,
  always on — the live-impact lever), and `DecayPolicy.SalienceWeighted` folds salience into
  `effectiveMaxAge` (contradicted → decay candidate, with a `MinUses` bypass; corroborated →
  resists). Deliberately *not* exposed to the model — gating is deterministic in core. See
  [brain-salience-gating.md](brain-salience-gating.md).
- **Lives in.** Core (`salience.go` + `consolidate.go` — decay/retention weight); the signal it
  consumes comes from the outcome loop (`brain-outcome-signal.md`).

### D4 — Episodic staging + replay (activate the unused `capture` path) — ✅ ingest staged (Band 1 M2/M3)

- **Status (Band 1).** Ingest now stages RAW captures (M2: `LearnFromTranscript` → `Capture`,
  `SourceCapture`, never injected) and fires on clean **completion** as well as delete (M3:
  `StateDone`, idempotent via a per-session event high-water mark). The ingest passes are
  **durable** (M7: a `brain_jobs` queue, drained on startup, retried then dead-lettered) so a
  crash mid-extraction never silently loses them — at-least-once (`docs/tech-debt.md`). Replay/
  abstraction of those captures by the churn is the remaining D4 work (Band 2 Curator).
- **Principle.** CLS again: store episodic traces first, then *replay* and abstract them during
  sleep, prioritising the salient ones — rather than transcribing straight to semantic memory.
- **Today.** `SourceCapture` exists but nothing writes it; auto-encode (`LearnFromTranscript`)
  shortcuts directly to `SourceConsolidated` durable facts. This is why scopes bloat with granular,
  code-discoverable trivia (the live corpus: 896 `category:fact` entries, scopes up to 427 facts).
- **Proposal.** Stage raw episodic captures, and have scheduled consolidation replay-and-abstract them
  (salience-weighted, D3) into durable facts — so abstraction sees a *batch* of related episodes,
  not one transcript at a time.
- **Lives in.** Core (`consolidate.go` promote phase already has the seam) + glue (auto-encode writes
  captures instead of facts).
- **Safety.** Captures are never recalled; promotion is preview-gated like today.

### D5 — Interference detection via the graph ✅ implemented

- **Principle.** What humans actually confuse is *similar-but-distinct* memories (proactive /
  retroactive interference).
- **Today.** Dedup (`dedup.go`) merges near-duplicates ≥ 0.6 Jaccard; the graph links neighbours
  ≥ 0.3. The band *between* "linked" and "duplicate" is exactly the interference zone, and nothing
  surfaces it.
- **Proposal.** Flag high-similarity-but-not-duplicate pairs as interference candidates → a confirm-UX
  prompt ("same fact, or genuinely distinct?"). Cheap; reuses the existing similarity graph.
- **Lives in.** Core (a query over `RelinkScope` output) + glue (`brain/graph.go` insight report).

### D6 — Spaced-review scheduling ✅ implemented

- **Principle.** Spaced retrieval beats massed (Ebbinghaus → SM-2 → FSRS). An important fact not
  retrieved in a while is *due for review*, not dead.
- **Today.** Decay silently prunes by age + use; there is no "resurface/re-verify before forgetting"
  step.
- **Proposal.** A scheduler over (storage, retrieval, last-seen) that proactively resurfaces or
  re-verifies high-storage / low-recent-retrieval facts instead of decaying them away.
- **Lives in.** Scheduled consolidation (`automation.go`) over D1's two-factor signal.
- **Depends on.** D1.

## Non-goals (where the brain metaphor misleads)

- **No capacity-driven forgetting.** Brains forget partly because storage is finite; the agent's
  isn't. Forgetting here is a signal-to-noise / retrieval-cost optimisation, not a necessity — never
  decay aggressively *just* to be brain-like.
- **No lossy reconstruction.** Human memory is reconstructive because it must be; we keep markdown
  provenance. Abstract for recall, keep the verbatim trace for audit — have both.
- **No ungated mutation-on-recall.** Reconsolidation creates false memories in humans. D2's
  write-back must stay gated, bounded, and provenance-tracked, or it amplifies errors instead of
  correcting them.
- **Not a brain simulation.** The goal is a useful assistant memory. Borrow mechanisms that raise
  signal and trust; skip the biological accidents.

## Open decisions

1. ~~**Storage strength: derived vs. persisted field?**~~ **Resolved: derived** (`StorageStrength`
   in `strength.go`, computed from confidence + cumulative `Uses` + `DerivedFrom` depth). The one
   new persisted field is `LastUsedAt` (recall timestamp), needed for retrieval-strength disuse.
2. **The outcome / contradiction signal — who emits it?** **Resolved (2026-06-22).** Both explicit
   agent emitters exist — `MemoryFlag` (negative) and `MemoryUsed` (positive, 2026-06-21) — and the
   remaining *automatic* branch now ships: a **session-end LLM judge** over the transcript
   (`brain-outcome-signal.md` ADR addendum; `internal/brain/outcome.go`) recovers the facts recall
   injected and emits the same signal itself (`MarkAutoHelped`, gentler 0.25 weight, / `Flag`),
   conservatively, without depending on the agent. The cheap explicit precursors shipped first
   (mirroring how D2 shipped `MemoryFlag` before any auto-detector); the automatic emitter is the
   bigger lever for making the signal non-inert on the live corpus. Open only on the margins: the
   judge's precision/recall over organic sessions and the auto weight want a live soak to tune.
3. **Reconsolidation gating.** How much may recall change a fact without human review, and how is an
   auto-update marked in provenance?
4. **Scheduler placement.** D6 as part of scheduled consolidation, or a separate lighter tick.
5. ~~**Confidence calibration.**~~ **Resolved (2026-06-21):** a confirmed-useful outcome
   (`MarkHelped`) raises `ConfidenceScore` toward a 0.95 corroboration ceiling (gap-closing,
   below human ground truth); a contradiction (`MarkContradicted`) knocks it down. Trust is now
   calibrated by outcome, not frozen at encode time, and gates behaviour at `ActOnConfidence`
   (the operating contract). Tracking *aggregate* high-confidence-correct vs. high-confidence-wrong
   rates for band re-tuning remains a future refinement. See
   [brain-outcome-signal.md](brain-outcome-signal.md).

## Sequencing

1. ~~**D1 — two-factor strength.**~~ ✅ done (`memory/strength.go`, `Record.LastUsedAt`,
   `DecayPolicy.StrengthWeighted`). Foundational: D6 builds on it.
2. ~~**D2 — reconsolidating recall (keystone).**~~ ✅ done. All three signals shipped:
   inject/"shown" (`BumpUses` stamps `LastUsedAt`), the contradiction half (`MemoryFlag` +
   `memory.MarkContradicted`), and — 2026-06-21 — the true positive "confirmed-useful" half
   (`MemoryUsed` + `memory.MarkHelped` + `Record.Helped`, with confidence calibration). See
   [brain-outcome-signal.md](brain-outcome-signal.md).
3. ~~**D5 — interference detection.**~~ ✅ done (`memory/interference.go`; surfaced in the graph report).
4. ~~**D6 — spaced-review scheduling.**~~ ✅ done (`memory.DueForReview`; `dueForReview` in the report).
   (Done out of original order — it only needed D1, and rode along with D5 in the report.)
5. ~~**D3 — salience-gated consolidation.**~~ ✅ done (`memory/salience.go`; `reorgRetained`,
   `DecayPolicy.SalienceWeighted`, `Salience`). `Record.Helped` and the `ReviewNote` contradiction
   flag drive what consolidation keeps (reorg-retention) and forgets (salience-weighted decay). See
   [brain-salience-gating.md](brain-salience-gating.md).
6. **D4 — episodic staging + replay.** Remaining. Largest; activates the dormant `capture` path and
   attacks scope bloat at the root — and is the natural consumer of D3's salience (replay prioritises
   the salient episodes).

## References

- Bjork & Bjork — *A New Theory of Disuse* (storage vs. retrieval strength).
- Roediger & Karpicke — the testing effect; Ebbinghaus — the forgetting curve; the spacing effect.
- Nader, Schafe & LeDoux — memory reconsolidation.
- McClelland, McNaughton & O'Reilly (1995) — Complementary Learning Systems; Tse et al. (2007) — schemas.
- Collins & Loftus (1975) — spreading activation; Tulving — episodic / semantic / procedural memory.
- FSRS / SuperMemo SM-2 — spaced-repetition scheduling.
- brain: `internal/memory/{record,recall,consolidate,confidence,strength,salience,reconsolidate,link,community,centrality}.go`,
  `internal/brain/{brain,automation,graph,mcp,outcome}.go`; [brain-memory.md](brain-memory.md);
  [brain-graph-layer.md](brain-graph-layer.md); [brain-outcome-signal.md](brain-outcome-signal.md);
  [brain-salience-gating.md](brain-salience-gating.md).
