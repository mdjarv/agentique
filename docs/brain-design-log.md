# Brain — design log

Why the brain works the way it does. `docs/brain-memory.md` is the subsystem
doc (architecture, storage, recall, API, runbook); this file is the **record of
decisions** behind it, condensed 2026-08-23 from nine separate RFC/ADR/spec
documents that had accumulated over three months.

Every proposal below is implemented unless its entry says otherwise. The
originals — including full task-level implementation specs for the shipped
work — are in git history: `git log --diff-filter=D -- docs/brain-*.md`.

**Still open across the whole brain**, gathered here so it is one list rather
than six:

- **D4 — episodic staging + replay** (the largest remaining piece; activates
  the dormant `capture` path and attacks scope bloat at the root).
- **Recall fan-out budget** — how many hops/neighbours associative recall may
  expand, and the decay on associative hits, without blowing the token budget.
- **Reconsolidation gating** — how much recall may change a fact without human
  review, and how an auto-update is marked in provenance.
- **Scheduler placement** — spaced review as part of scheduled consolidation,
  or its own lighter tick.
- **Outcome-judge tuning** — the session-end judge's precision/recall over
  organic sessions, and the 0.25 auto weight, want a live soak.
- **Persisted cross-scope edges ("B4")** — deferred; see `docs/tech-debt.md`.

---

## Graph layer

RFC · 2026-06-17 · **all proposals shipped**

The brain had facts but no structure: `Related` was dead, so recall could only
find what a query lexically matched. The graph layer makes the corpus a graph
and uses it for retrieval, clustering and consolidation.

- **P1 — activate the link graph.** Consolidation populates `Related`; recall
  expands associatively (`memory/link.go`, `recall.go`'s `expandAssociative`).
- **P2 — confidence tiers + centrality + confirm UX**
  (`memory/{confidence,centrality}.go`, `brain/graph.go`).
- **P3 — community detection** → cluster-aware and aggressive consolidation
  (`memory/community.go`, the chunker in `brain/extractor.go`).
- **P4 — graph view**, "what the brain knows about you".
- **P5 — `ConsolidateGlobal` via the graph**: cross-scope community guardrail
  + content-hash manifest (`memory/global_graph.go`).
- **P6 — self-balancing semantic graph** (2026-06-23): embeddings become
  weighted kNN **edges**, not positions — cosine weights force strength,
  distance and visual weight together. The PCA-projection cut was retired
  deliberately; the graph is a model of the mind, not a scatter plot.

**Decisions worth keeping:**

- *Edge persistence* — similarity edges are **persisted** into `Related` and
  rebuilt on each apply; the graph view still recomputes Jaccard for its
  dashed edges.
- *Community algorithm* — **label propagation**, made deterministic by
  id-sorted node order and smallest-id tie-breaks. Reproducible plans without
  Louvain's extra code.
- *Community threshold* — topic clustering uses a **separate, lower** Jaccard
  threshold (`DefaultCommunityThreshold` = 0.15) than the 0.3 `Related`-edge
  threshold. Measured on the live reviewbot scope: at 0.3, 386/404 facts are
  singletons and chunking degenerates to fixed-size; 0.15 yields coherent
  clusters and co-locates 229 facts the old chunker scattered, while staying
  above the ≈0.10 point where everything collapses into one blob.
- *Confidence backfill* — **derived lazily from `Source` on load**, never
  blank (`filestore.toRecord` → `memory.NormalizeConfidence`), persisted on
  the record's next write. No migration pass.
- *Graph index* — computed per request; the expensive part (semantic kNN) is
  memoized by a corpus fingerprint (`Service.SemanticEdges`).

Non-goal at current scale: neo4j, parked as a documented optional export.

## Learning dynamics

RFC · 2026-06-17 · **D1, D2, D3, D5, D6 shipped; D4 remains**

The graph gave the brain structure; this RFC gave it *dynamics* — feedback
loops borrowed from human-memory research, so a fact's standing changes with
what happens to it rather than being frozen at encode time.

- **D1 — two-factor strength** (storage vs. retrieval). `StorageStrength` is
  derived in `strength.go` from confidence + cumulative `Uses` + `DerivedFrom`
  depth; the one new persisted field is `LastUsedAt`, needed for disuse aging.
- **D2 — reconsolidation: recall is a write** (the keystone). All three
  signals ship: inject/"shown" (`BumpUses` stamps `LastUsedAt`), the
  contradiction half (`MemoryFlag` → `MarkContradicted`), and the
  confirmed-useful half (`MemoryUsed` → `MarkHelped`, `Record.Helped`).
- **D3 — salience-gated consolidation** (see below).
- **D5 — interference detection** via the graph (`memory/interference.go`).
- **D6 — spaced review** (`memory.DueForReview`), shipped early since it only
  needed D1.
- **D4 — episodic staging + replay.** *Not built.* The natural consumer of
  D3's salience: replay prioritises the salient episodes.

## The outcome signal

ADR · 2026-06-21, extended 2026-06-22 · **accepted, shipped**

The brain strengthened a fact when it was *shown*. It did not strengthen a
fact when it actually *helped* — so trust never reflected usefulness.

- A confirmed-useful outcome raises `ConfidenceScore` toward a **0.95
  corroboration ceiling** — gap-closing, and deliberately **below human
  ground truth**. Human confirmation outranks corroboration, always.
- A contradiction knocks it down. Trust is calibrated by outcome, not frozen
  at encode time, and gates behaviour at `ActOnConfidence` — which is what
  promotes high-confidence preferences into the **operating contract** the
  agent follows without re-asking.
- **Automatic emitter** (2026-06-22): a session-end LLM judge over the
  transcript (`internal/brain/outcome.go`) recovers the facts recall injected
  and emits the same signal itself, so the loop does not depend on an agent
  remembering to call `MemoryUsed`/`MemoryFlag`. An automatic `helped` weighs
  **half** an explicit one (0.25); the negative half keeps a high evidence
  bar.
- **`Reinforce`** — the third reconsolidation verb (Band 1 M4): re-observing a
  known fact is no longer a no-op.

Constants live in `internal/memory`.

## Salience gating

ADR · 2026-06-23 · **accepted, shipped**

Consolidation already strengthened by outcome; this lets outcome decide what
consolidation **keeps and forgets**. A first-class salience signal
(`memory/salience.go`) derived from `Record.Helped` and the `ReviewNote`
contradiction flag gates two decisions: reorg retention (`reorgRetained`) and
`DecayPolicy.SalienceWeighted`.

Neutral salience is **0.5, not 0** — an unproven fact must not be treated as a
disproven one. And deliberately *not* done: telling the model about salience.
It is a property of what happened, not a hint to be gamed.

## Semantic recall

2026-06-22 · **shipped and live in production** (Chroma + Ollama `all-minilm`)

Lexical recall is blunt: keyword overlap surfaces facts that merely share
vocabulary. The cure is vector recall, blended with keyword rather than
replacing it.

- **Vector veto** — drop a candidate the embedder scores as actively
  unrelated, regardless of keyword match. Semantics may now *exclude*, not
  just re-rank.
- **Vouch bar** — the complementary lever on the way in.
- Both thresholds are **embedding-model-specific**. `agentique brain
  calibrate` prints the corpus's own cosine distribution and the
  percentile-derived values; `AGENTIQUE_BRAIN_AUTOCAL=1` derives them at boot.
  Hand defaults (0.45 / 0.15, all-MiniLM) remain the fallback; an explicit
  setting still wins per knob.
- Everything degrades cleanly to keyword/Jaccard with no embedder configured.
  That is a hard requirement, not a nicety: recall never fails because
  optional infrastructure is down.

Setup and verification: see the runbook in `docs/brain-memory.md`.

## Cross-scope areas and semantic links

RFC · 2026-06-21 · **B shipped; C shipped (core); B4 deferred**

Measured first (live-brain copy, 1510 durable facts): **1417 were isolated** —
the link graph was ~94% disconnected, so associative recall had almost nothing
to traverse. Cross-scope structure was the real win, not more within-scope
linking.

- **B — topic areas.** `Record.Area` + `AssignAreas` (`memory/areas.go`),
  recomputed on consolidation apply; `brain assign-areas` CLI + `PreviewAreas`;
  `colorBy: "area"` hulls in the graph view. A cross-project sibling of
  `Community`, feeding sibling-scope associative recall.
- **C — semantic links.** A pluggable similarity primitive
  (`memory/similarity.go`) blending Jaccard with embedding cosine, wired into
  `RelinkScope`, `DetectCommunities` and areas.
- Area labels are **TF-IDF** derived (2026-06-23), replacing noisy
  frequency-based labels.
- **B4** — persisting cross-scope edges — deferred; tracked in tech-debt.

Also from this round: **fluid per-turn delta recall**, replacing first-turn-only
injection. Don't reintroduce first-turn-only recall.

## Band 1 — "Migrate" (evolution pipeline)

Implementation spec · **shipped 2026-06-29**

An audit found the brain *captured* but did not *evolve*: ~98% of facts carried
`source=consolidated`, ~97% were frozen at confidence 0.80, scheduled
consolidation passed an empty `DecayPolicy{}` so decay never fired, re-observing
a known fact was a no-op, interference detection never reconciled, and learning
only triggered on session *deletion*.

Band 1 migrated that into an **ingest → churn → inject pipeline with an
injection gate**: capture-tier ingest, learn-on-completion (not on delete),
`Reinforce`, confidence as a living scalar that erodes on disuse (computed at
recall, persisted only at the archive transition), a controlled-vocabulary
label control plane, snapshot/rollback before every churn, and a durable job
queue.

The reversibility rules that came out of it are load-bearing: **markdown is the
source of truth**, everything else (graph, areas, vectors) is a rebuildable
index; **archive, never delete**; snapshot before every churn.

Band 2 ("Curator") remains design-only.

## Brain UI

Implementation spec · **shipped 2026-06-29**

Band 1 was backend-only by design, so none of it was visible or manageable.
The UI surface added: labels on the hand-synced wire, tier badges and filters,
restore, a snapshots panel with cachestore invalidation, the typed-relation
graph with colour-by, and the health report.

Later UX round: default to the graph view, regions off by default, full-text
on hover.
