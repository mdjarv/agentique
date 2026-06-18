# RFC: Cross-scope topic areas (B) + semantic links (C)

Status: **draft for review** — no code yet. Follows the shipped phase-A graph viz
(regions, headless-report surfacing, subsumed-on-hover).

## Problem

Two structural limits make the brain graph mostly disconnected dots and keep recall
inside a single project:

1. **Everything is scope-local.** `RelinkScope` and `AssignCommunities`
   (`internal/memory/{link,community}.go`) only ever link/cluster *within* a scope.
   `expandAssociative` (`recall.go`) fans out over `Related`, which therefore never
   crosses projects. The only cross-scope signal is `CrossScopeGroups`
   (`global_graph.go`) — and it's computed transiently for promotion, then thrown away.
2. **Everything is lexical.** Every similarity edge — `RelinkScope`,
   `DetectCommunities`, `CrossScopeGroups`, `DetectInterference`, the graph's "similar"
   edges — is token-Jaccard. Cross-project facts that mean the same thing in different
   words ("race detector" vs "concurrent safety") never link.

Verified empirically on a copy of the live brain (2026-06-18): **1417 of 1510 facts
(~94%) are structurally isolated.** Phase A made this legible; B and C are what fix it.

## What already exists (build on, don't rebuild)

- `CrossScopeGroups(candidates, threshold, minScopes)` (`global_graph.go`) — partitions
  facts into topic communities and keeps those spanning ≥N scopes. **This is the B
  clustering primitive**; today only `crossScopeCandidates` (promotion) consumes it.
- `Record.Community int` — persisted, scope-local cluster id (powers cluster colouring).
  Areas are its cross-scope sibling.
- `expandAssociative(recalled, pinned, all, k)` (`recall.go`) — bounded `Related`
  fan-out at recall. The hook where cross-area recall plugs in.
- `memory.Embedder.Embed(ctx, texts) ([][]float32, error)` + `memory.Searcher` +
  the Chroma store (`internal/memory/chroma`). **The C vector source.** `Record.Embedding`
  exists but is never persisted by the filestore today.
- Apply hooks: `ApplyPlan` calls `RelinkScope`+`AssignCommunities` per scope
  (`consolidate.go:356`); `ApplyGlobalPromotion` calls `RelinkScope(global)`
  (`promote.go:309`). B/C extend exactly these (infrequent "sleep"-pass) points — never
  the hot recall path.

---

## Phase B — cross-scope topic areas

**Goal:** a persisted, named "area" that spans projects — the literal "areas of
projects" — feeding the viz, recall, and promotion.

**Data model.** Add `Record.Area string` (a stable area id) + a sidecar
`brain/.areas.json` mapping `areaID → {label, scopes[]}`. Parallels `Community` (a
rebuildable derived index, never source of truth). *Not* new `Related` edges — see
Risk.

**Compute** (new `AssignAreas(ctx, store)` run once at the global/sleep pass): run
`CrossScopeGroups` over the whole promotable+global set, assign each member an area id,
deterministic-label it from the group's top TF-IDF tokens (LLM naming deferred to a
later D). Idempotent, like `AssignCommunities`. Isolated/single-scope facts get no area.

**Recall win** (`expandAssociative`): after the scope-local `Related` fan-out, add a
second bounded pass that pulls top same-`Area` facts from *sibling scopes* (≤ assocPerSeed,
≤ k total). This is the substantive brain change: recalling in project X can surface the
proven convention from project Y. Stays on the persisted index — no recompute, hot path
cheap.

**Promotion win:** persisted areas spanning ≥N scopes *are* the promotion candidates —
surface them in `/graph`'s report as "could promote" so the existing machinery becomes
visible/explainable instead of firing silently.

**Viz win:** add `colorBy: "area"` + an area-keyed region hull (cross-project areas), and
draw cross-scope same-area edges. Reuses the phase-A hull renderer.

**Risk:** `RelinkScope` rebuilds (overwrites) `Related` each apply (tech-debt P1) — so B
must **not** store areas as `Related` edges or they'd be clobbered, and the moment curated
`[[links]]` land that field needs auto-vs-curated tagging. Keeping areas a separate field
sidesteps this for B (open decision D2).

---

## Phase C — semantic links

**Goal:** make the similarity signal mean *meaning*, not shared tokens — uplifts every
consumer (RelinkScope, communities, areas, interference) at once.

**Approach:** introduce `Similarity` as a pluggable function. Default = current
`jaccardSets`. When an `Embedder` is available, compute embedding cosine and combine
`max(jaccard, cosine)` (open decision D4) so lexical-only and semantic-only pairs both
link. The four call sites switch from calling `jaccardSets` directly to the injected
similarity.

**Vector access:** batch-`Embed` all texts at the (infrequent) sleep pass and compute
pairwise cosine (O(n²) over ~1500 × ~768-dim ≈ a few M dot products — trivial offline).
Cache vectors on `Record.Embedding` (persist it, or a sidecar) keyed by text hash so
unchanged facts aren't re-embedded.

**Gating:** only active in semantic mode (Chroma+embeddings configured). Pure keyword
deployments keep today's behaviour.

**Calibration:** cosine thresholds differ from Jaccard's 0.3/0.15 and must be tuned on
the real corpus before defaults are set (open decision D5 — pairs well with a measure-first
prototype).

---

## Sequencing

B first (no new infra; uses existing lexical `CrossScopeGroups`), then C (which then
*re-clusters* B's areas on the better signal). C uplifts both areas and RelinkScope, so
B's value compounds once C lands. Each is independently shippable and independently
verifiable on the live-brain copy (phase-A verify recipe).

## Open decisions

- **D1 — Area storage:** new `Record.Area` field + `.areas.json` sidecar (recommended,
  parallels `Community`) vs a standalone area-index only.
- **D2 — RelinkScope/curated edges:** keep B areas as a *field only* (sidesteps the P1
  overwrite debt, recommended) vs fix auto-vs-curated `Related` tagging now and store
  cross-scope edges.
- **D3 — Embedding access:** re-embed at the sleep pass + cache on `Record.Embedding`
  (recommended, provider-agnostic) vs add a Chroma bulk-vector fetch.
- **D4 — Combine vs replace:** `max(jaccard, cosine)` blend (recommended) vs cosine
  replaces Jaccard outright.
- **D5 — Thresholds:** calibrate cosine thresholds on the real corpus before committing
  (recommended — a measure-first prototype).

## Confidence / risk

High confidence the primitives exist and the hooks are correct (read the code). Main
unknowns are empirical: cluster/threshold quality (D5) and whether cross-area recall
helps vs adds noise — both want a measure-first check on the real corpus before the
defaults are locked. Blast radius is contained to the infrequent sleep/global pass and
the graph view; the hot recall path only gains one bounded fan-out.
