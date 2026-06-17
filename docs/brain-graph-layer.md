# RFC: A graph layer for the brain

Status: Draft · 2026-06-17 · Sibling to [brain-memory.md](brain-memory.md)

> **Progress:** graph-view v1 and **P1 (link graph + associative recall) are
> implemented** — see `memory/link.go`, `recall.go`'s `expandAssociative`, and the
> relink hooks in `consolidate.go`/`promote.go`. P3 (community detection →
> cluster-aware consolidation) is next; P2/P5 not started. Open decision #1 was
> resolved as: *persist* consolidation-discovered similarity edges in `Related`
> (rebuilt each apply); the graph view still recomputes Jaccard for dashed edges.

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
  "sleep" consolidation pass.

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

### P2 — Confidence tiers on facts

Adopt graphify's three-tier rubric (`EXTRACTED` / `INFERRED` / `AMBIGUOUS`, with a 0.55–0.95 score
for inferred). We have `Source` but no confidence. Mapping: hand-edited / human = ground truth;
LLM-distilled capture = `INFERRED`; cross-project generalization = `INFERRED` + score. Drives two
things: a sharper decay signal (decay low-confidence, low-`Uses` facts faster) and the
"confirm what I'm unsure about" UX in P4.

### P3 — Community detection in Go

Build a similarity graph (edges from `Related` + Jaccard/embedding similarity) and run label
propagation or Louvain (pure Go, no `graspologic`/NetworkX). Outputs a `community` tag per record.
Two payoffs:

- **Consolidate within a community** instead of whole-scope → tighter merges, less cross-topic noise.
- **Cluster coloring** in the graph view (P4).

Centrality is cheap and immediately useful: degree (→ "god nodes" / load-bearing facts) and
betweenness (→ bridge facts) straight off the adjacency list.

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

### P5 — ConsolidateGlobal via the graph

`HandlePreviewGlobal`/`HandleApplyGlobal` already exist. The graph sharpens them: a cross-scope
community that touches ≥ 2–3 distinct `project:*` scopes is exactly the "transferable pattern"
guardrail. graphify's `global_graph.py` dedups shared nodes by label and keeps a content-hash
manifest for incremental rebuild — the same mechanic as "the same tooling/preference fact across N
projects → one `global` node." Guardrail stays: generalize preferences/workflow/tooling, never
codebase-specific facts.

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
2. **Community algorithm.** Label propagation (simplest, non-deterministic ordering) vs. Louvain
   (stable-ish, more code). Need a seed/tie-break for reproducible plans.
3. **Recall fan-out budget.** How many hops / neighbors, and the decay applied to associative hits,
   without blowing the recall token budget.
4. **Confidence backfill.** How to assign confidence to facts that predate P2 (default `INFERRED`?
   leave blank and treat as unknown?).
5. **Where the graph index lives.** Computed per-request in `brain`, or a cached adjacency in
   `memory` invalidated on write? Start request-time; cache if it bites.

## Sequencing

1. ~~**Graph-view v1** — force-graph over `derivedFrom` + computed similarity. No backend change.~~ ✅ done.
2. ~~**P1** — populate `Related` in consolidation; associative recall.~~ ✅ done (`memory/link.go`).
3. **P3** — community detection → cluster coloring + within-community consolidation. ← next.
4. **P2** — confidence tiers + the "confirm" UX.
5. neo4j: parked as a documented optional export.

## References

- graphify: `analyze.py` (god nodes / surprising connections / suggested questions / `graph_diff`),
  `cluster.py` (Leiden + `cohesion_score`), `global_graph.py` (cross-repo merge, dedup-by-label,
  manifest), `docs/how-it-works.md` ("graph structure *is* the similarity signal").
- brain: `internal/memory/{record,recall,consolidate,dedup}.go`, `internal/brain/http.go`.
