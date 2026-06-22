# RFC: A graph layer for the brain

Status: Draft · 2026-06-17 · Sibling to [brain-memory.md](brain-memory.md)

> **Progress:** graph-view v1, **P1 (link graph + associative recall)**, **P3
> (community detection → cluster-aware consolidation + aggressive consolidation)**, **P2
> (confidence tiers + centrality + confirm UX)** and **P5 (ConsolidateGlobal via the
> graph)** are implemented — see
> `memory/{link,community,confidence,centrality,global_graph}.go`, `recall.go`'s
> `expandAssociative`, the cluster-aware chunker in `brain/extractor.go`, the
> `brain/graph.go` insight endpoint, and the relink/cluster/confidence hooks in
> `consolidate.go`. **All RFC proposals are now shipped.**
>
> **Built on top since (2026-06-21, see [brain-cross-scope-areas.md](brain-cross-scope-areas.md)):**
> cross-scope topic *areas* (`Record.Area`, `memory/areas.go`) — a cross-project sibling
> of `Community`, surfaced as `colorBy: "area"` hulls and feeding sibling-scope associative
> recall; **fluid per-turn delta recall** (was first-turn-only); a **pluggable similarity**
> primitive (`memory/similarity.go`) blending Jaccard with embedding cosine, wired into
> RelinkScope/DetectCommunities/areas and activated for areas. Decisions resolved:
> - **#1** (edge persistence): *persist* similarity edges in `Related` (rebuilt each
>   apply); the graph view still recomputes Jaccard for dashed edges.
> - **#2** (community algorithm): **label propagation**, made deterministic by
>   sorting nodes by id and breaking label ties by smallest id — reproducible plans
>   without Louvain's extra code.
> - **#4** (confidence backfill): *derive lazily from `Source` on load*, never blank
>   — human→EXTRACTED/ground-truth, everything else→INFERRED at the default score;
>   the derived value is persisted on the record's next write (see P2 below).
> - **community threshold**: topic clustering uses a *separate, lower* Jaccard
>   threshold (`DefaultCommunityThreshold` = 0.15) than the 0.3 Related-edge
>   threshold. Verified on the live reviewbot scope: at 0.3, 386/404 facts are
>   singletons (chunking degenerates to fixed-size); 0.15 yields coherent topic
>   clusters and co-locates 229 facts that the old chunker scattered, while staying
>   above the ≈0.10 point where everything collapses into one blob.

## Motivation

The brain already stores a graph and uses it like a flat list.

`memory.Record` carries `Related []string` (a `[[link]]` graph) and `DerivedFrom []string`
(consolidation provenance). Both are persisted by `filestore` and surfaced over `brain/http.go`'s
`memoryDTO` — but **nothing populates `Related`, and neither recall nor consolidation reads either
field.** Recall is flat top-K (keyword + optional vector); consolidation dedups pairwise and decays.
The structural signal we already collect is inert.

`~/git/graphify` (a knowledge-graph tool: folder → NetworkX graph → Leiden clustering → centrality
analysis → report) is a working blueprint for what to *do* with that structure. It maps **code**;
we store **distilled facts** — so we lift its graph-algorithm layer, not its extractor. This RFC
proposes activating the latent graph and rendering it.

## Principles (unchanged from brain-memory.md)

- **Markdown files are the source of truth.** Everything else is a rebuildable index.
- **Pure Go, `uuid` + `yaml` only**, so the core stays liftable into `agentkit/memory`. No CGo, no
  JVM, no Python. Centrality and community detection are a few hundred lines of Go.
- **Cognition is offline.** The hot path (recall) stays cheap; LLM work happens in the
  scheduled consolidation pass.

### Architecture framing

```
markdown files (source of truth)
   ├── Chroma vector index      (rebuildable, optional — memory/chroma)
   └── graph adjacency index    (rebuildable — in-Go by default)   ← this RFC
```

A graph database (neo4j/falkordb) would be a *third rebuildable index* behind the same seam, never
canonical. See "Non-goals."

## Proposals

### P1 — Activate the link graph (foundational)

**Populate `Related` during consolidation.** When dedup / the extractor's reorganize step finds two
durable facts on the same topic (Jaccard or embedding-cosine over the configured threshold, reusing
`memory/dedup.go`), record a `Related` edge instead of only merging-or-leaving. Edge creation is
part of the deterministic `Plan` → `ApplyPlan` flow, so it is previewable and reversible like every
other consolidation change. Locked/human records still get linked *to* but their own field is only
appended, never rewritten.

**Use `Related` in recall (associative recall).** After the flat top-K match, expand one hop along
`Related` and fold neighbors in at a decayed score. This mirrors human associative recall and the
project's own "cognitive loop" framing. Bounded fan-out (e.g. ≤ N neighbors per seed, total cap) so
the hot path stays cheap.

### P2 — Confidence tiers on facts ✅ implemented

Adopt graphify's three-tier rubric (`EXTRACTED` / `INFERRED` / `AMBIGUOUS`, with a 0.55–0.95 score
for inferred). We have `Source` but no confidence. Mapping: hand-edited / human = ground truth;
LLM-distilled capture = `INFERRED`; cross-project generalization = `INFERRED` + score. Drives two
things: a sharper decay signal (decay low-confidence, low-`Uses` facts faster) and the
"confirm what I'm unsure about" UX.

**Shipped (2026-06-17):**
- `memory/confidence.go` — `ConfidenceTier` (`extracted`/`inferred`/`ambiguous`) +
  a 0..1 `ConfidenceScore` on `Record`. The **score is canonical**; the tier is
  always derived from `(Source, score)` via `TierForScore`, so the pair can't drift.
  `ConfidenceForSource` is the RFC mapping: human→`EXTRACTED`@1.0,
  everything-else→`INFERRED`@0.8; `ApplyGlobalPromotion` overrides a generalized fact
  to `CrossProjectInferredScore` (0.65) — a riskier inference lands near the bottom of
  the band where the confirm UX surfaces it. `New()` stamps confidence from Source;
  the reorganize-rewrite path reconciles it; `filestore` persists `confidence` /
  `confidence_score` and **backfills missing values from Source on load**
  (open-decision #4) so pre-P2 facts need no migration.
- **Sharper decay** — `DecayPolicy.ConfidenceWeighted` scales each fact's effective
  `MaxAge` by its score, so the brain forgets what it is least sure about first.
  Off by default, like decay itself (EXTRACTED facts are human-authored → already
  exempt from decay via `isProtected`). The mechanism lives in the core; no live path
  enables decay today.
- **Centrality** (the cheap win below) — `memory/centrality.go`.
- **Confirm UX** — `brain/graph.go` derives a `needsConfirmation` queue (non-protected
  facts at/under `NeedsConfirmationScore`); `POST /memories/{id}/confirm`
  (`Service.Confirm`) accepts a fact as ground truth (human/`EXTRACTED`, thereafter
  protected). The Brain tab shows a confidence dot on each card with a Confirm action,
  and the graph view's Insights panel lists the confirm queue inline. The reject side
  is a plain Delete.

`AMBIGUOUS` is the bucket for an inferred fact whose score has fallen below
`AmbiguousScoreThreshold` (0.55). At creation nothing is AMBIGUOUS (the lowest
assigned score is 0.65); the tier becomes reachable through a hand-edit or, once
enabled, confidence-weighted decay erosion — a documented future extension.

### P3 — Community detection in Go ✅ implemented

Build a similarity graph (edges from `Related` + Jaccard/embedding similarity) and run label
propagation or Louvain (pure Go, no `graspologic`/NetworkX). Outputs a `community` tag per record.
Two payoffs:

- **Consolidate within a community** instead of whole-scope → tighter merges, less cross-topic noise.
- **Cluster coloring** in the graph view (P4).

Centrality is cheap and immediately useful: degree (→ "god nodes" / load-bearing facts) and
betweenness (→ bridge facts) straight off the adjacency list.

**Shipped (2026-06-17):**
- `memory/community.go` — `DetectCommunities` (deterministic label propagation over
  `Related` ∪ token-Jaccard ≥ `DefaultCommunityThreshold`) and `AssignCommunities`
  (persists a scope-local `Record.Community`, idempotent, run after `RelinkScope` on
  apply; previews skip it).
- **Cluster-aware chunking** — `PlanConsolidation` tags each reorg fact with its
  community; `brain/extractor.go`'s `chunkByCommunity` packs whole communities into
  `≤ maxReorgBatch` chunks so related facts merge across a large scope, not just
  within an arbitrary 100-fact slice.
- **Aggressive consolidation** — a less-conservative reorganize prompt that collapses families
  of granular facts into broad rules, exposed as a conservative/aggressive toggle in
  the Brain tab (and `brain consolidate --aggressive`). Safe because it's
  preview-gated. The over-deletion guard is now a configurable `MinSurvivorRatio`
  (default 0.5; aggressive lowers it to 0.2) captured into the `Plan` so preview and
  apply enforce the identical guard.
- **Force-rerun** — `ConsolidateOptions.Force` skips the unchanged-fingerprint
  short-circuit; surfaced as a "Force re-run" button on an already-tidied scope and a
  `brain consolidate --rerun` flag (needed to re-consolidate after prompt/algorithm changes).
- **Cluster coloring** — the graph view's "Color by scope/cluster" toggle paints
  nodes by `community`.
- **Centrality** (shipped with P2) — `memory/centrality.go`'s `ComputeCentrality`:
  degree and Brandes betweenness (normalized to [0,1]) over the structural graph
  (`Related` ∪ resolved `DerivedFrom`, the solid-edge signal — not the cosmetic dashed
  Jaccard). Deterministic, request-time, never persisted. `GET /api/brain/graph`
  serves it with a derived report — god nodes (top degree, load-bearing), bridges
  (top betweenness, the riskiest to lose), isolated gaps, and the confirm queue —
  which the graph view's Insights panel renders. Node size now blends degree, so god
  nodes read bigger.

### P4 — Graph view ("what the brain knows about you")

An Obsidian-style force-directed view on the `/brain` tab. The API already returns enough to build
it client-side (`memoryDTO`: `related`, `derivedFrom`, `uses`, `pinned`, `scope`, `category`).

Edge sources, in increasing cost — **v1 ships before P1 lands**:

1. `derivedFrom` — exists today (consolidation writes it), free.
2. Computed similarity — Jaccard token overlap (works keyword-only, our default) or embedding cosine
   (when Chroma is on), thresholded to edges.
3. Curated `related` — once P1 populates it.

Encodings: node size = `Uses`; color = scope (then community after P3); star = `Pinned`; solid edge
= `derivedFrom` provenance, dashed = computed similarity. Isolated nodes (degree ≤ 1) render as
floating dots — graphify's "knowledge gap" signal, made visual.

Derived report panel (graphify `analyze.py` analogs): god nodes (top `Uses`/degree); surprising
connections (a community spanning ≥ 2 scopes → a `global`-promotion candidate); gaps (isolated /
low-cohesion / `AMBIGUOUS` facts) → "confirm X?" prompts.

Library: add `react-force-graph-2d` (canvas, scales past our node counts). We already ship `mermaid`
but it is static/declarative — wrong tool for an organic, interactive graph.

### P5 — ConsolidateGlobal via the graph ✅ implemented

`HandlePreviewGlobal`/`HandleApplyGlobal` already exist. The graph sharpens them: a cross-scope
community that touches ≥ 2–3 distinct `project:*` scopes is exactly the "transferable pattern"
guardrail. graphify's `global_graph.py` dedups shared nodes by label and keeps a content-hash
manifest for incremental rebuild — the same mechanic as "the same tooling/preference fact across N
projects → one `global` node." Guardrail stays: generalize preferences/workflow/tooling, never
codebase-specific facts.

**Shipped (2026-06-17):**
- `memory/global_graph.go` — the cross-scope graph layer, pure Go:
  - `CrossScopeGroups` runs `DetectCommunities` over the candidate facts (token-Jaccard
    at `DefaultCommunityThreshold`; `Related` edges are scope-local so they're ignored)
    and keeps only communities spanning ≥ `DefaultMinPromotionScopes` (2, tunable via
    `ConsolidateOptions.MinPromotionScopes`) distinct project scopes. Codebase-specific
    facts live in one scope, so they fall out here with **no content heuristic** — the
    scope span *is* the guardrail. `crossScopeCandidates` flattens the groups keeping each
    community contiguous so a topic's copies share a model batch and the Promoter can
    collapse them into one `global` node ("dedup shared nodes by label" — `labelFor` is
    the dedup key; identical text → Jaccard 1.0 → same community, adjacent in the batch).
  - `ScopeManifest` + `manifestsEqual` — the per-scope content-hash **manifest** (the same
    order-independent fingerprint used as the apply-time staleness guard). Incremental
    rebuild: `PlanGlobalPromotion` skips the (expensive) model run when no project changed
    since the last pass; `ConsolidateOptions.Force` overrides, mirroring the single-scope
    fingerprint short-circuit. A `GlobalPlan.Skipped` flag carries the no-op through apply.
- `PlanGlobalPromotion` now narrows candidates through the guardrail before batching and
  short-circuits on an unchanged manifest; `ApplyGlobalPromotion` treats a skipped plan as a
  deterministic no-op (`Report.Skipped`). Confidence/over-deletion/staleness guards unchanged.
- `brain.Service` persists the manifest (`.global-manifest.json`, separate from the per-scope
  consolidation `.fingerprints.json`): `PlanGlobal` loads it as `PrevManifest` and records it when a
  pass finds nothing to promote; `ApplyGlobal` advances it to the post-apply state. The
  manifest only advances once promotions are actually applied (or a pass is genuinely clean),
  so an unapplied preview is never wrongly skipped.
- **Subsumed-source snapshot.** Apply also snapshots each merged-away project fact's
  `{scope, text}` onto the promoted record (`Record.Subsumed`), since the originals are
  deleted — so the review surface can show the merge *inputs → output* rather than just a
  count. Facts promoted before this existed degrade to "originals not retained". See the
  review surface in [brain-memory.md](brain-memory.md).

## Non-goals

- **No graph database dependency now.** Scale is dozens–low-thousands of records; an in-Go in-memory
  graph does traversal/centrality/clustering instantly. neo4j is a JVM server that would shatter the
  pure-Go invariant. Even graphify defaults to in-memory NetworkX and treats neo4j/falkordb as
  optional adapters. If ad-hoc Cypher exploration is ever wanted, add an export sub-package mirroring
  `memory/chroma` — rebuildable, throwaway, never the source of truth.
- **Not a code-graph tool.** graphify maps code structure (a separate codebase-RAG concern). At most,
  the brain could one day ingest a `GRAPH_REPORT.md` as project-reference memory.
- **No tree-sitter / LLM-emitted edges.** Our edges come from existing dedup similarity + provenance,
  not from a parse of source files.

## Open decisions

1. **Edge persistence vs. recompute.** Persist similarity edges into `Related`, or recompute on read
   and reserve `Related` for curated/provenance links only? (Affects determinism of the graph view
   and what consolidation may overwrite.)
2. ~~**Community algorithm.**~~ **Resolved: label propagation**, made deterministic
   by id-sorted node order + smallest-id label tie-break (no Louvain). The topic
   threshold is a *separate, lower* tunable (`DefaultCommunityThreshold` = 0.15) than
   the 0.3 Related-edge threshold — see the progress note for the reviewbot data
   behind that number.
3. **Recall fan-out budget.** How many hops / neighbors, and the decay applied to associative hits,
   without blowing the recall token budget.
4. ~~**Confidence backfill.**~~ **Resolved: derive lazily from `Source` on load**,
   never blank — `filestore.toRecord` calls `memory.NormalizeConfidence`, so a fact
   with no `confidence` frontmatter (everything written before P2) gets the
   source-implied tier/score and is persisted on its next write. Chosen over
   leave-blank because `Source` already encodes the exact provenance the mapping keys
   on (so the derivation is lossless) and a blank/unknown state would force every
   consumer — decay, the confirm queue, the graph — to special-case it. No migration
   pass needed.
5. **Where the graph index lives.** Computed per-request in `brain`, or a cached adjacency in
   `memory` invalidated on write? **Started request-time** (P2's `GET /api/brain/graph`
   computes centrality + report on each call); cache if it bites.

## Sequencing

1. ~~**Graph-view v1** — force-graph over `derivedFrom` + computed similarity. No backend change.~~ ✅ done.
2. ~~**P1** — populate `Related` in consolidation; associative recall.~~ ✅ done (`memory/link.go`).
3. ~~**P3** — community detection → cluster coloring + within-community (cluster-aware) consolidation + aggressive consolidation.~~ ✅ done (`memory/community.go`, `brain/extractor.go`).
4. ~~**P2** — confidence tiers + centrality + the "confirm" UX.~~ ✅ done (`memory/{confidence,centrality}.go`, `brain/graph.go`).
5. ~~**P5** — ConsolidateGlobal via the graph: cross-scope community guardrail + content-hash manifest.~~ ✅ done (`memory/global_graph.go`). neo4j remains parked as a documented optional export — the only unimplemented item, and a non-goal at current scale.

## What's next

This RFC gave the brain its *structure*. The follow-on — [brain-learning-dynamics.md](brain-learning-dynamics.md)
— adds the *dynamics* that structure still lacks: feedback loops from human-memory research
(retrieval strengthens and updates memory, salience gates consolidation, similar-but-distinct facts
get disambiguated). It reuses everything here — the graph, communities, centrality, and confidence
are the substrate those loops run on.

## References

- graphify: `analyze.py` (god nodes / surprising connections / suggested questions / `graph_diff`),
  `cluster.py` (Leiden + `cohesion_score`), `global_graph.py` (cross-repo merge, dedup-by-label,
  manifest), `docs/how-it-works.md` ("graph structure *is* the similarity signal").
- brain: `internal/memory/{record,recall,consolidate,dedup}.go`, `internal/brain/http.go`.
