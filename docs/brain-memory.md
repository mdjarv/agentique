# The Brain — persistent agent memory

A user-/project-scoped knowledge store that agents read from and write to across
sessions. Facts learned in one session (conventions, preferences, gotchas,
architectural decisions) are recalled in future ones, surviving the per-worktree
isolation that otherwise resets an agent's context every run.

The design follows a three-phase cognitive loop:

- **recall** — at the start of a task an agent searches the brain (or pinned
  facts are surfaced) and gets back what's already known.
- **encode** — the agent saves durable facts; raw turn material can be staged as
  episodic *captures*.
- **consolidate ("sleep")** — a periodic pass distills captures into durable
  facts, merges duplicates, abstracts repeated episodes into general rules, and
  decays stale unused facts.

## Layering — liftable core vs. agentique glue

The feature is built so the reusable parts can later move into the shared
`agentkit` library with a directory move + import rename. **The core depends only
on the standard library, `github.com/google/uuid`, and `gopkg.in/yaml.v3`** — both
already in agentkit's `go.mod`.

```
backend/internal/memory/            # LIFTABLE CORE (no agentique imports)
  record.go      Record, Scope, Category, Source
  store.go       Store interface, Query/Result, BumpUses
  embed.go       Embedder interface, CosineSimilarity
  recall.go      hybrid keyword+vector ranking (Searcher optional)
  dedup.go       exact + token-Jaccard duplicate detection
  consolidate.go Extractor interface + Consolidate() "sleep" pass
  tokenize.go    tokenizer/stopwords (internal)
  filestore/     markdown+frontmatter Store — source of truth, hand-editable
  chroma/        ChromaDB decorator Store (semantic index) + minimal HTTP client
  embedhttp/     OpenAI-compatible /v1/embeddings Embedder

backend/internal/brain/             # AGENTIQUE GLUE (stays here)
  brain.go       Service: composes the core, maps projects->scopes, fingerprints
  mcp.go         MCPAdapter: agent-facing MemoryAdd/MemorySearch
  http.go        Brain tab REST API
```

`internal/memory` is policy-free machinery; `internal/brain` supplies the
agentique policy (scope = project, env-driven config, REST/MCP surfaces). When a
second consumer (formica/hittat) needs memory, lift `internal/memory/**` into
`agentkit/memory` and keep `internal/brain` as the agentique-specific glue.
**See `docs/agentkit-extraction.md` for the mechanical lift playbook** (what
moves, the dependency invariant + verify command, import rewrites, and the public
API surface).

## Storage

- **Source of truth: markdown files** (`filestore`), one per memory, under
  `<data-dir>/brain/<scope>/<id>.md`. YAML frontmatter + the fact as the body.
  Files are greppable, hand-editable, and git-friendly. A hand-edit is picked up
  on the next read; editing via the UI marks a record `source: human`, which
  exempts it from consolidation rewrite/decay.
- **Semantic index: ChromaDB** (`chroma`), optional and *derived*. The base
  filestore is authoritative; Chroma is a rebuildable index (`Reindex()`).
  Durable writes never fail because the index is down, and recall degrades to
  keyword ranking on any vector error or empty result. Scope is written to vector
  metadata and used as a query-time `where` filter, so semantic search is
  isolated per scope at the source (not post-filtered).

Chroma 1.x does not embed server-side, so an `Embedder` is required for semantic
recall; `embedhttp` calls any OpenAI-compatible embeddings endpoint.

## Recall ranking

`memory.Recall` returns pinned facts (always) plus the top-K query-relevant
non-pinned facts. Episodic captures are never recalled. Ranking is keyword-only
(idf-weighted token overlap + category boosts + recency tiebreaker) unless the
Store implements `Searcher` (Chroma does), in which case it blends vector and
keyword scores — degrading cleanly to keyword-only when the vector path is
unavailable or returns nothing.

**Associative recall** (RFC P1): after the flat top-K, `expandAssociative` folds in
a bounded set of each top match's `Related` neighbours (≤3 per seed, ≤K total) at
lower priority — reading the persisted link graph, no recompute on the hot path.
The graph is built by `RelinkScope` (see below), so it's only active on scopes
that have been consolidated.

## Consolidation ("sleep")

`memory.Consolidate(store, extractor, scope, opts)` is conservative by
construction:

- **promote** episodic captures into durable facts via the `Extractor` (LLM),
  deduped against existing facts; identity facts are auto-pinned. An empty
  extraction never consumes captures.
- **reorganize** the non-protected durable set: merge duplicates, rewrite vague
  entries, abstract repeated episodes into general rules. Invented IDs are
  dropped; an over-deletion safety net refuses a reorganization that shrinks a set
  of ≥8 facts below a survivor ratio (default 0.5; an aggressive Tidy lowers it to
  0.2). The ratio is captured into the `Plan` so preview and apply enforce the same
  guard. Chunking is **cluster-aware** (RFC P3): facts are tagged with a topic
  community (`DetectCommunities`) and whole communities are packed into one
  reorganize call, so related facts merge across a large scope — not just within an
  arbitrary 100-fact slice. A **conservative** prompt merges only true duplicates;
  an **aggressive** prompt collapses families of granular facts into broad rules
  (preview-gated).
- **decay** stale, low-use facts (opt-in via `DecayPolicy`). With
  `ConfidenceWeighted` set, each fact's effective max-age is scaled by its confidence
  score (RFC P2), so the brain forgets what it is least sure about first.
- **never touches** pinned, locked, or human-authored facts.
- a **fingerprint** of the reorganizable set is persisted per scope; an unchanged
  set skips the (expensive) LLM reorganization.
- **relink** (RFC P1): on a real apply, `RelinkScope` rebuilds the scope's `Related`
  edge graph from token-Jaccard neighbours (≥0.3, below the 0.6 dup threshold;
  degree-capped, bidirectional, deterministic/idempotent). Previews skip it
  (derived metadata). Powers associative recall and the graph view's curated edges.
- **cluster** (RFC P3): after relink, `AssignCommunities` recomputes each fact's
  scope-local `Community` (topic cluster) over the fresh edges + token-Jaccard ≥
  `DefaultCommunityThreshold` (0.15 — lower than the Related threshold; topic groups
  are broader than near-duplicate links). Deterministic/idempotent, previews skip it.
  Drives cluster-aware chunking and graph-view cluster coloring.

The pass returns a `Report` (the changelog) describing exactly what changed.

**Extraction bias.** The extract prompt deliberately prefers FEWER, BROADER facts
(cap 3), skips code-discoverable trivia unless it's a surprising gotcha, and records
only facts about the session's own project — to keep scopes high-signal rather than
accumulating implementation details.

## Agent surface (MCP tools)

Auto-approved, scoped to the calling session's project (+ global):

- `MemoryAdd(text, category)` — save a durable fact.
- `MemorySearch(query)` — recall pinned + relevant facts.

## Brain tab REST API

- `/api/brain/memories` (GET/POST), `/{id}` (GET/PUT/DELETE), `/{id}/pin`,
  `/{id}/lock`, `/{id}/confirm` (accept a low-confidence fact as ground truth),
  `/search`, `/graph` (centrality + insight report), `/status`.
- **Consolidation is preview → apply.** `POST /consolidate/preview
  {scope,model,mode,force}` (mode `conservative|aggressive`; force re-runs an
  unchanged scope) and `POST /consolidate/global/preview {model}` start a background
  job (they
  return `202` with the initial job; progress + the final `{report, plan}` arrive
  over the WebSocket bus and via `GET /consolidate/job`). `POST /consolidate/apply
  {plan}` and `/consolidate/global/apply {plan}` replay the held plan
  deterministically — no model call — returning `409` on a stale plan.
- `POST /consolidate {scope}` remains for a synchronous deterministic-only pass.
- `POST /consolidate/all {model}` — the "Tidy all" button: a background job that
  consolidates **every scope and auto-applies each** (an on-demand sleep pass,
  kind `"all"`), relying on the guards; progress is per-scope. One job at a time, so
  it can't overlap a single-scope/global tidy.

Background consolidation runs off the request context so a request hiccup can't
SIGTERM the model subprocess; see `brain/job.go`. Job state is **in-memory** (one
active job): a backend restart drops an in-flight preview — harmless, since preview
is a dry-run — and the frontend re-hydrates on WS reconnect (`useBrainSubscriptions`
→ `GET /consolidate/job`), clearing a stale "Analyzing…" spinner. A preview's model
batches run with bounded concurrency (`memory.RunBounded`, `maxParallelBatches` /
`maxParallelReorg` = 4) — independent calls whose results just merge.

## CLI commands (`agentique brain …`)

Admin/migration operations, defined in `backend/cmd/agentique/brain.go`. A
non-release (`go run`) build resolves a *relative* `agentique.db`, so point it at
the real data dir: `AGENTIQUE_DB=~/.local/share/agentique/agentique.db`.

- `backfill [--project --limit --min-events --model --dry-run -f]` — retroactively
  distill memories from past session transcripts.
- `consolidate (--project|--scope) [--model --aggressive --rerun --dry-run -f]` — run
  the sleep pass over one scope (no `--model` = deterministic). `--aggressive`
  collapses granular facts into broad rules (relaxes the over-deletion guard);
  `--rerun` reorganizes even an unchanged scope (ignores the saved fingerprint).
- `export <file>` — write all memories to a portable JSON bundle (project scopes
  tagged with name/slug).
- `import <file> [--map src=local -y]` — merge a bundle. Global merges directly;
  project scopes match local projects by **slug**, with an unmatched source project
  resolved interactively (pick a local project / skip / send to global) unless
  `-y/--skip-unmatched` or `--map source-slug=local-slug` pre-resolves it.
  Duplicates are skipped, so import is idempotent.

## Configuration

The brain is enabled by default with keyword recall over markdown files at
`<data-dir>/brain`. Semantic recall is opt-in via environment variables:

| Env var | Meaning |
|---|---|
| `AGENTIQUE_BRAIN_CHROMA_URL` | ChromaDB base URL (e.g. `http://localhost:8000`) |
| `AGENTIQUE_BRAIN_EMBED_URL` | OpenAI-compatible embeddings endpoint |
| `AGENTIQUE_BRAIN_EMBED_MODEL` | embedding model name |
| `AGENTIQUE_BRAIN_EMBED_KEY` | optional bearer token for the embeddings endpoint |

All three of `CHROMA_URL`, `EMBED_URL`, `EMBED_MODEL` must be set (and Chroma
reachable) for semantic recall; otherwise the brain logs a warning and uses
keyword recall.

## Automation (the live recall → encode → consolidate loop)

The cognitive loop runs automatically, not just via the CLI/UI:

- **Auto-recall (on by default).** The project's pinned facts are injected into
  every session's system preamble at create/resume (`Service.PinnedPreamble` →
  `Manager.MemoryPreambleFn`), so the brain shapes behaviour without the agent
  having to call `MemorySearch` (query-relevant recall stays pull-based via that
  tool). Disable with `AGENTIQUE_BRAIN_RECALL=off`.
- **Auto-encode (opt-in).** When a session is deleted, its transcript is distilled
  into durable facts and added to the project scope (async, deduped) — set
  `AGENTIQUE_BRAIN_LEARN_MODEL=haiku|sonnet|opus`. Skips trivial sessions.
- **Scheduled "sleep" (opt-in).** `AGENTIQUE_BRAIN_SLEEP_INTERVAL` (e.g. `6h`)
  starts a background pass that consolidates every scope on each tick; add
  `AGENTIQUE_BRAIN_SLEEP_MODEL` for LLM reorganization (else deterministic
  dedup/decay). Auto-apply is safe by the consolidation guards.

Memory changes (HTTP, agent `MemoryAdd`, auto-encode, sleep) broadcast a
`brain.updated` WebSocket event that flares the nav button and refreshes open tabs.

## Scope model

agentique is single-user, so scopes are project-based: `global` for cross-project
facts, `project:<id>` for codebase-specific facts. A session reads its project
scope plus global. The scope string is opaque to the core, so other consumers can
map their own concepts (board, persona, …).

## Where the pieces live (contributor map)

Liftable core — `backend/internal/memory/` (stdlib + uuid + yaml only):
- `record.go` `store.go` `embed.go` — `Record`/`Scope`/`Category`/`Source`, the
  `Store` and `Embedder` contracts, `BumpUses`.
- `recall.go` — hybrid keyword+vector ranking; `DefaultRecallK`, the score cutoffs.
- `consolidate.go` — `PlanConsolidation` (LLM, runs once) + `ApplyPlan`
  (deterministic, `ErrStalePlan`) + `Consolidate` (one-shot); the `Extractor`
  contract; over-deletion guard; `Progress`/`OnError` hooks.
- `promote.go` — cross-scope `Promoter` + `PlanGlobalPromotion`/`ApplyGlobalPromotion`.
- `link.go` — `RelinkScope` (the `Related` similarity graph); `recall.go`'s
  `expandAssociative` consumes it. See `docs/brain-graph-layer.md` (RFC).
- `community.go` — `DetectCommunities` (deterministic label propagation) +
  `AssignCommunities` (persists `Record.Community`); feeds cluster-aware chunking
  (`brain/extractor.go`) and graph cluster coloring. RFC P3.
- `confidence.go` — `ConfidenceTier`/`ConfidenceScore` on `Record` (RFC P2): the
  source→confidence mapping (`ConfidenceForSource`), score-canonical tier derivation
  (`TierForScore`), and lazy backfill (`NormalizeConfidence`). Drives
  `DecayPolicy.ConfidenceWeighted` and the confirm UX.
- `centrality.go` — `ComputeCentrality` (degree + Brandes betweenness over the
  `Related`/`DerivedFrom` structural graph); request-time, never persisted. RFC P2.
- `dedup.go` `tokenize.go` `filestore/` `chroma/` `embedhttp/`.

agentique glue — `backend/internal/brain/`:
- `brain.go` — `Service`: composes the core, project↔scope mapping, fingerprints,
  `PinnedPreamble`, `ListScopes`, `LearnFromTranscript`, `ImportRecords`.
- `extractor.go` — `ClaudeExtractor` (model is a required param; JSON-schema
  constrained; chunked; `Extract`/`Reorganize`/`Promote`).
- `graph.go` — `GET /api/brain/graph`: centrality-annotated nodes + a derived
  insight report (god nodes, bridges, confirm queue, isolated gaps). RFC P2.
- `job.go` — async consolidation jobs + WS push types (`brain.consolidation`,
  `EventBrainUpdated`).
- `automation.go` — the scheduled "sleep" loop.
- `mcp.go` `http.go` — agent tools and the REST handlers.
- `transcript.go` — transcript reconstruction for extraction.

Session/server wiring (the additive integration points):
- recall → `session/manager.go` `MemoryPreambleFn` (set in `server.go`).
- auto-encode → `session/service.go` `onSessionEnd` (fired in `DeleteSession`).
- scheduler + config → `server.go` (the `AGENTIQUE_BRAIN_*` env switches).
- CLI → `cmd/agentique/brain.go`.

Frontend — `frontend/src/`: `lib/brain-api.ts`, `stores/brain-store.ts`,
`hooks/useBrainSubscriptions.ts`, `components/brain/BrainPage.tsx`,
`components/brain/BrainGraph.tsx` (force-graph view, RFC graph-view v1),
nav flare in `components/layout/AppSidebar.tsx` (`brain-flare` in `index.css`).

**To add a feature:** keep policy (model choice, scope mapping, env) in the glue or
caller; keep `internal/memory` portable (no agentique imports — verified by the
playbook in `docs/agentkit-extraction.md`). Mutations should broadcast
`brain.EventBrainUpdated`; long model work runs off the request context as a job.

## Known limitations / remaining work

- **The `Extractor` is the caller's, with a caller-chosen model.** `ClaudeExtractor`
  takes the model as a required parameter (no library default). It drives
  `brain backfill`, the preview/apply consolidation (per-scope Tidy + cross-scope
  global), auto-encode, and the scheduled sleep pass. (Anthropic has no embeddings
  API, so semantic recall still needs an external embeddings endpoint.)
- **Query-relevant recall is still pull-based.** Auto-recall injects *pinned*
  facts into the preamble; relevance-ranked top-K recall (now graph-augmented via
  associative recall) for a specific task is still the agent's `MemorySearch` call
  (no per-turn push of query-relevant facts).
- **Link graph is recompute-on-consolidate, not curated.** `RelinkScope` rebuilds
  similarity edges each apply; there's no curated/human `[[link]]` UI yet, and the
  graph view still draws client-side Jaccard for dashed edges on top. RFC P3
  (community detection → cluster-aware consolidation + aggressive Tidy) and P2
  (confidence tiers + degree/betweenness centrality + the confirm UX) are **done**;
  the cross-scope graph play (P5) is the only remaining RFC proposal.
- **Episodic `capture` staging is unused.** Auto-encode distills a finished
  session's transcript directly into durable facts rather than staging raw
  captures for a later sleep pass; the `SourceCapture` path exists but nothing
  writes to it.
- **Chroma collection space.** The collection is created with cosine distance;
  changing the embedding model/space requires a fresh collection name (a stale
  collection of the same name created with a different space would skew scores).
