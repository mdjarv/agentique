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
- **consolidate** — a periodic pass distills captures into durable
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
  consolidate.go Extractor interface + Consolidate() pass
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

**Archived cold tier + disuse aging (M5).** Confidence is a *living scalar*: the stored
`ConfidenceScore` eroded by time-since-last-use on a volatility-keyed half-life (slow 90d,
ephemeral 14d; evergreen never), clamped up to an evidence floor (trusted 0.50 — above the
archive line, so trusted facts never fade out; inferred 0.30; observed-once 0.15). This
*effective confidence* is computed at recall (`memory.EffectiveConfidence`), **never written on
a nudge**. Forgetting = **archive**, not delete: when a fact has faded below the archive floor
and gone untouched longer than `archive-after`, the churn moves it to `Lifecycle=archived` — a
cold tier excluded from recall and every other live consumer (promotion, areas/community/link/
graph, review queue, operating contract), **kept on disk and restorable** (a hand-edit revives
it and restarts its clock). Human/pinned/locked/evergreen never erode or archive. Two deploy
safeguards: the read-time fade is gated on archiving being enabled (`archive-after` defaults to
off, so nothing hard-drops until an operator opts in after curating), and the M6 backfill stamps
`last_used=now`-where-zero so the disuse clock starts at the migration boundary, not an ancient
`updated`. Nothing in the aging path ever deletes a record.

**Label control plane (M6).** Every record carries a controlled vocabulary the churn and
aging branch on (`internal/memory/labels.go`): `Evidence` (`user_stated`/`code_verified`/
`corroborated`/`inferred`/`observed_once`), `Volatility` (`evergreen`/`slow`/`ephemeral` →
decay rate), `Lifecycle` (`active`/`superseded`/`archived`), typed `Relations`
(supersedes/contradicts/duplicates/generalizes/corroborates — replaces the untyped
`Related`, which is retained), free-form `Keywords`, plus `LastCurated`/`CuratorNote`.
`New()` stamps the source/category defaults; `NormalizeLabels` fills empties on load (never
overwriting an explicit value — idempotent and human-curation-safe). Defaults flow from
source (human→`user_stated`, capture→`observed_once`, else `inferred`; non-human with
`Helped≥2 && !contradicted`→`corroborated`) and category (identity→`evergreen`,
task→`ephemeral`, else `slow`). `agentique brain backfill-labels` persists these onto the
existing files and into Chroma metadata, snapshot-first and idempotently.

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

## Consolidation

`memory.Consolidate(store, extractor, scope, opts)` is conservative by
construction:

- **promote** episodic captures into durable facts via the `Extractor` (LLM),
  deduped against existing facts; identity facts are auto-pinned. An empty
  extraction never consumes captures.
- **reorganize** the non-protected durable set: merge duplicates, rewrite vague
  entries, abstract repeated episodes into general rules. Invented IDs are
  dropped; an over-deletion safety net refuses a reorganization that shrinks a set
  of ≥8 facts below a survivor ratio (default 0.5; an aggressive consolidation lowers it to
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
- `MemorySearch(query)` — recall pinned + relevant facts (output includes each fact's id).
- `MemoryFlag(id, reason)` — flag a recalled fact as wrong/outdated (RFC-LD D2
  reconsolidation): weakens it into the review queue, never deletes. The id comes from
  `MemorySearch` output.
- `MemoryUsed(id)` — confirm a recalled fact was useful/correct (RFC-LD D2 positive half,
  `docs/brain-outcome-signal.md`): strengthens it and raises confidence toward a 0.95
  corroboration ceiling, so well-proven preferences graduate into the operating contract.
  The positive twin of `MemoryFlag`; id likewise from `MemorySearch` (or a recalled-memory block).

## Brain tab REST API

- `/api/brain/memories` (GET/POST), `/{id}` (GET/PUT/DELETE), `/{id}/pin`,
  `/{id}/lock`, `/{id}/confirm` (accept a low-confidence fact as ground truth),
  `/{id}/flag {reason}` (RFC-LD D2: weaken a contradicted fact into the review queue,
  mirrors the agent `MemoryFlag` tool), `/{id}/refine {text,instruction,model}`
  (RFC-LD: the model rewrites a fact per an instruction and returns a *draft* — no
  write; the review UI's "Refine with AI"), `/{id}/restore` (un-archive a cold fact
  back into the live set — flips `lifecycle` to active and restarts the disuse clock;
  a normal cachestore-consistent write, idempotent on a non-archived fact; the Brain
  tab's per-row Restore action on archived rows), `/search`, `/graph` (centrality +
  insight report incl. `dueForReview`/`interference`), `/status` (the `semantic` flag plus
  the brain-health `counts`: `total`, `byLifecycle`, `bySource`, `byEvidence`,
  `byVolatility`, `byConfidenceTier`, `reviewQueue`, `corroboratedTotal` — a single-pass
  aggregation over the corpus, the Band-3 E2 report).
- Every `memoryDTO` carries the Band-1 controlled-vocabulary labels (`lifecycle`,
  `evidence`, `volatility`, `corroborations`, typed `relations`, `keywords`,
  `lastCurated`, `curatorNote`). These wire types are **hand-synced** with
  `frontend/src/lib/brain-api.ts` (`Memory`) — there is no typegen for brain types.
- **Consolidation is preview → apply.** `POST /consolidate/preview
  {scope,model,mode,force}` (mode `conservative|aggressive`; force re-runs an
  unchanged scope) and `POST /consolidate/global/preview {model}` start a background
  job (they
  return `202` with the initial job; progress + the final `{report, plan}` arrive
  over the WebSocket bus and via `GET /consolidate/job`). `POST /consolidate/apply
  {plan}` and `/consolidate/global/apply {plan}` replay the held plan
  deterministically — no model call — returning `409` on a stale plan.
- `POST /consolidate {scope}` remains for a synchronous deterministic-only pass.
- `POST /consolidate/all {model}` — the "Consolidate all" button: a background job that
  consolidates **every scope and auto-applies each** (an on-demand consolidation,
  kind `"all"`), relying on the guards; progress is per-scope. One job at a time, so
  it can't overlap a single-scope/global consolidation.
- **Snapshots.** `GET /snapshots` (list newest-first), `POST /snapshots` (take one),
  `POST /snapshots/{id}/restore` (roll the whole brain back; 204 + `brain.updated`
  broadcast). Restore invalidates the live read-through cache so the UI reflects the
  restored tree immediately (see *Snapshots & rollback*).

Background consolidation runs off the request context so a request hiccup can't
SIGTERM the model subprocess; see `brain/job.go`. Job state is **in-memory** (one
active job): a backend restart drops an in-flight preview — harmless, since preview
is a dry-run — and the frontend re-hydrates on WS reconnect (`useBrainSubscriptions`
→ `GET /consolidate/job`), clearing a stale "Analyzing…" spinner. A preview's model
batches run with bounded concurrency (`memory.RunBounded`, `maxParallelBatches` /
`maxParallelReorg` = 4) — independent calls whose results just merge.

## Brain tab (UI surface for the Band-1 pipeline)

The Brain tab makes the Band-1 backend visible and manageable (the spec is
`docs/brain-ui-spec.md`):

- **Tier visibility (F1).** Every memory row is self-describing: a *capture* / *archived* /
  *superseded* badge, compact *evidence* + *volatility* chips, and a corroboration `×N`
  count. The defaults (evidence `inferred`, volatility `slow`, lifecycle `active`) render
  nothing, so ordinary rows stay quiet. The vocabulary→label map lives in `lib/brain-labels`
  and the shared `MemoryLabels` component renders it on both the list and the review surface.
- **Default-hide filters (F2).** The list shows only *live injectable* facts
  (`lifecycle=active`, non-capture) by default; two toolbar toggles reveal captures and
  archived, each with a count. Filtering is component-local in `BrainPage` (the
  stable-selector rule keeps derived lists out of the store) and threads through both the
  list and the graph's `graphMemories`.
- **Restore (F3).** An archived row has a **Restore** action (`store.restore` →
  `POST /memories/{id}/restore`).
- **Snapshots (F4).** A **Snapshots** toolbar button opens a panel to list / take / restore
  brain snapshots; restore is guarded by a confirm and blocked while a consolidation runs.
- **Graph (F5 / E4).** Typed relations render as distinct directed edges (coloured by kind);
  colour-by gains *evidence* and *volatility*; archived nodes are excluded (via the F2 filter)
  and superseded nodes can be dimmed; the new labels show on hover.
- **Health (F6 / E2).** A **Health** popover shows the `/status` `counts` distribution
  (capture backlog, archived/superseded, evidence/volatility/confidence spread, review
  queue), refreshed on `brain.updated`.

Brain wire types are **hand-synced** (no typegen): each `memoryDTO`/`snapshotDTO`/
`statusCounts` change in `internal/brain/http.go` has a mirror edit in `lib/brain-api.ts`.

## CLI commands (`agentique brain …`)

Admin/migration operations, defined in `backend/cmd/agentique/brain.go`;
read-only inspection in `backend/cmd/agentique/brain_inspect.go`. A
non-release (`go run`) build resolves a *relative* `agentique.db`, so point it at
the real data dir: `AGENTIQUE_DB=~/.local/share/agentique/agentique.db`.

Read-only inspection (never mutates the corpus; reuses the same `brain.Service`,
so search uses the live hybrid/keyword recall path; all take `--json`):

- `list [--scope --category --limit --sort uses|new --json]` — list memories
  (id, scope, category, trust, uses, truncated text). Most-used first by default.
- `show <id> [--json]` — one memory's full text + all frontmatter (source,
  confidence, derived-from, subsumed sources, related, community/area, review note).
  `<id>` may be a unique id prefix.
- `search <query> [--scope --limit --json]` — run the query through the production
  recall path: a vector+keyword hybrid when semantic recall is configured, else
  keyword-only. Returns the ranked facts the brain would surface.
- `stats [--json]` — corpus summary: total facts, per-scope counts, trust-tier
  breakdown (extracted/inferred/ambiguous), graph connectivity (connected vs
  isolated) and the semantic-edge count.

Admin / migration:

- `backfill [--project --limit --min-events --model --dry-run -f]` — retroactively
  distill memories from past session transcripts.
- `consolidate (--project|--scope) [--model --aggressive --rerun --dry-run -f]` — run
  scheduled consolidation over one scope (no `--model` = deterministic). `--aggressive`
  collapses granular facts into broad rules (relaxes the over-deletion guard);
  `--rerun` reorganizes even an unchanged scope (ignores the saved fingerprint).
- `export <file>` — write all memories to a portable JSON bundle (project scopes
  tagged with name/slug).
- `import <file> [--map src=local -y]` — merge a bundle. Global merges directly;
  project scopes match local projects by **slug**, with an unmatched source project
  resolved interactively (pick a local project / skip / send to global) unless
  `-y/--skip-unmatched` or `--map source-slug=local-slug` pre-resolves it.
  Duplicates are skipped, so import is idempotent.
- `backfill-labels [--brain-dir --dry-run -f --no-reindex]` — one-time: seed the
  Evidence/Volatility/Lifecycle defaults onto the existing markdown files, stamp `last_used`
  where it is zero (start the disuse clock at the migration boundary), and reindex Chroma so
  its metadata carries `volatility`/`lifecycle`. Snapshot-first and idempotent (a second run
  rewrites nothing). Run with the server idle and **restart afterward**.
- `snapshot [--retain N]` — take a restorable filesystem snapshot of the brain dir
  (prints the new snapshot id, file count and the retained list). Pure FS, no model.
- `restore <id> [--retain N -f]` — restore the brain to a snapshot. A fresh
  pre-restore safety snapshot is written first, so restore is itself reversible. An
  unknown id prints the available ids. Offline-only: it refuses when a server pidfile
  is live (rewriting files under a running cache is unsafe) unless `-f/--force`.

## Snapshots & rollback (reversibility)

The markdown brain dir is the source of truth, so every churn is made reversible by a
one-shot filesystem copy taken *before* it runs (`brain.Snapshot`, `internal/brain/snapshot.go`).
Snapshots live in a sibling `brain/.snapshots/<ts>/` directory (UTC timestamp ids,
lexically == chronological). This directory is **invisible to recall/consolidation**:
`filestore.List` is non-recursive and reads only the direct `*.md` of each top-level
scope dir, so `.snapshots` (which holds no direct `*.md`) yields zero records and never
enters `ListScopes`/`Recall`. Scheduled consolidation snapshots the whole brain at the
top of each pass (a snapshot failure is WARN-logged and does **not** block the pass — the
archive-not-delete churn keeps the pass reversible regardless). Retention keeps the
newest `snapshot-retain` (default 7); older snapshots are pruned. This is the single
snapshot mechanism — the label backfill and the CLI reuse `brain.Snapshot`.

**Live restore (Brain tab).** `Service.RestoreSnapshot(id)` makes restore safe against a
*running* server: it holds `s.mu` (so the file rewrite can't race a single-fact write),
takes a pre-restore safety snapshot, restores the tree, then calls `cachestore.Invalidate()`
so the read-through cache rebuilds from the restored files — without it the cache would keep
serving the pre-restore corpus until the next write (the old "offline-only" caveat). It then
broadcasts `brain.updated` so every tab refetches. In semantic mode the chroma vector index
is *not* reindexed here (it reconciles lazily on the next write per fact / on a Reindex or
restart warm), so the memory list is correct immediately while recall ranking may be briefly
stale — a deliberate follow-up. The Brain tab's **Snapshots** panel lists/takes/restores
snapshots; restore is guarded by a confirm and blocked while a consolidation job runs.

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

Other `[brain]` tunables (config-file key → env override; env wins):

| `[brain]` key | Env override | Default | Meaning |
|---|---|---|---|
| `snapshot-retain` | `AGENTIQUE_BRAIN_SNAPSHOT_RETAIN` | 7 | pre-churn brain snapshots to keep under `brain/.snapshots/` |
| `archive-after` | `AGENTIQUE_BRAIN_ARCHIVE_AFTER` | `""` (off) | disuse-aging archival: a fact untouched longer than this **and** faded below the floor is archived (e.g. `"720h"`). Empty = no fade/archive |
| `archive-confidence-floor` | `AGENTIQUE_BRAIN_ARCHIVE_FLOOR` | 0.35 | effective-confidence line below which a faded fact is archived / faded from recall |
| `retry-max` | `AGENTIQUE_BRAIN_RETRY_MAX` | 5 | session-end learn/outcome job retries before dead-lettering |

## Automation (the live recall → encode → consolidate loop)

The cognitive loop runs automatically, not just via the CLI/UI:

- **Auto-recall (on by default).** Two complementary pushes, both gated by
  `AGENTIQUE_BRAIN_RECALL` (disable with `=off`):
  - **Pinned facts → system preamble** at create/resume (`Service.PinnedPreamble`
    → `Manager.MemoryPreambleFn`). Always-on facts, injected before any prompt
    exists.
  - **Task-relevant recall → every turn (fluid, delta)** (`Service.RecallBlock` →
    `Manager.MemoryRecallFn`, installed per session via `wireRecall`). The system
    preamble is fixed at connect, *before* the task prompt is known, so
    query-dependent recall can't go there. Instead `Session.injectRecall` runs
    `memory.Recall` against the actual prompt on **every** turn — recall follows the
    conversation as it drifts, not just the first message. A session-level seen-set is
    passed as `exclude` so each turn injects only the facts that are *newly* relevant
    (delta — no re-dumping), and a low-content gate (`memory.TokenCount < 2`) skips
    trivial turns ("ok", "go for it"). The top-K non-pinned hits are prepended as a
    `<brain><fact id="…">…</fact></brain>` envelope — an unambiguous memory-vs-user
    boundary for the model, parsed by the frontend into a "Recalled from memory" card.
    The framing (background context, verify first) and the `MemoryUsed`/`MemoryFlag`
    hooks are explained once in the system preamble (`RecallPreamble`), so the per-turn
    block stays compact. A 3s timeout bounds
    each lookup; degrades to no injection when the brain is off, recall is slow/fails,
    or nothing new matches. A read-through corpus cache (`memory/cachestore`) keeps the
    per-turn `List` cheap. Each newly-surfaced fact gets `BumpUses`/`LastUsedAt`
    stamped, so per-turn recall is also the read signal that feeds two-factor strength
    (D1), strength-weighted decay, and spaced review — previously starved because
    recall was pull-only and fired once.
- **Auto-encode → capture tier (opt-in).** When a session ends, its transcript is
  distilled into **raw captures** (`source: capture`, never injected) staged in the
  project scope (async) — set `AGENTIQUE_BRAIN_LEARN_MODEL=haiku|sonnet|opus`. Skips
  trivial sessions. Ingest is now tier-1: it stages captures, it does **not** write
  injectable facts. The only path `capture → consolidated` is the churn (scheduled
  consolidation), which promotes captures with `derived_from` provenance — so a
  deployment with a learn model set **must also enable scheduled consolidation _with a
  consolidate model_**: promotion is LLM-only (`Extractor`-gated), so an interval set but
  `AGENTIQUE_BRAIN_CONSOLIDATE_MODEL` left empty runs deterministic dedup/decay that never
  drains captures — they pile up indefinitely. Set BOTH the interval and the model.
  `LearnFromTranscript` returns the count of
  captures *staged* (M2). M4 reconciles re-observation by reinforcing a duplicated
  *durable* fact instead of stacking a redundant capture (the dedup set stays
  durable-only, so capture-vs-capture still never dedups).
- **Scheduled consolidation (opt-in).** `AGENTIQUE_BRAIN_CONSOLIDATE_INTERVAL` (e.g. `6h`),
  or `[brain] consolidate-interval` in `config.toml` (env wins), starts a background pass
  that consolidates every scope on each tick; add `AGENTIQUE_BRAIN_CONSOLIDATE_MODEL` /
  `consolidate-model` for LLM reorganization (else deterministic dedup/decay). Auto-apply
  is safe by the consolidation guards. (Inspired by sleep-based memory consolidation.)

Memory changes (HTTP, agent `MemoryAdd`, auto-encode, scheduled consolidation) broadcast a
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
  Narrows candidates through the cross-scope graph guardrail and short-circuits on an
  unchanged manifest (RFC P5).
- `global_graph.go` — the cross-scope promotion graph (RFC P5): `CrossScopeGroups`
  (communities spanning ≥ `DefaultMinPromotionScopes` distinct project scopes — the
  transferable-pattern guardrail), `crossScopeCandidates`/`labelFor` (dedup-by-label
  batch colocation), and `ScopeManifest`/`manifestsEqual` (the per-scope content-hash
  manifest for incremental rebuild). Mirrors graphify's `global_graph.py`.
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
- `strength.go` — `StorageStrength` (durable) + `RetrievalStrength` (decays with disuse),
  Bjork's two-factor model (RFC-LD D1); `DueForReview` (spaced review, D6). Drives recall
  ranking + `DecayPolicy.StrengthWeighted`. Reads `Record.LastUsedAt` (stamped by `BumpUses`).
- `reconsolidate.go` — `MarkContradicted` (RFC-LD D2): weaken a recalled fact found wrong into
  the review band + record `Record.ReviewNote`; never deletes; protected facts keep their score.
- `interference.go` — `DetectInterference` (RFC-LD D5): similar-but-not-duplicate pairs
  (token-Jaccard in [related, duplicate)) for the "same, or distinct?" queue.
- `dedup.go` `tokenize.go` `filestore/` `chroma/` `embedhttp/`.

agentique glue — `backend/internal/brain/`:
- `brain.go` — `Service`: composes the core, project↔scope mapping, fingerprints,
  `PinnedPreamble` (pinned → preamble), `RecallBlock` (task-relevant → first turn,
  stamps `BumpUses`), `ListScopes`, `LearnFromTranscript`, `ImportRecords`.
- `extractor.go` — `ClaudeExtractor` (model is a required param; JSON-schema
  constrained; chunked; `Extract`/`Reorganize`/`Promote`/`Refine`). `Refine` rewrites
  one fact per a user instruction; `unwrapRefineText` defends against the model
  echoing the `{"text":…}` schema into its own output.
- `graph.go` — `GET /api/brain/graph`: centrality-annotated nodes + a derived
  insight report (god nodes, bridges, confirm queue, isolated gaps, `dueForReview`,
  `interference`). RFC P2/D5/D6.
- `job.go` — async consolidation jobs + WS push types (`brain.consolidation`,
  `EventBrainUpdated`).
- `automation.go` — the scheduled consolidation loop.
- `mcp.go` `http.go` — agent tools and the REST handlers.
- `transcript.go` — transcript reconstruction for extraction.

Session/server wiring (the additive integration points):
- recall → `session/manager.go` `MemoryPreambleFn` (set in `server.go`).
- auto-encode → `session/service.go` `onSessionEnd` (fired in `DeleteSession`).
- scheduler + config → `server.go` (the `AGENTIQUE_BRAIN_*` env switches).
- CLI → `cmd/agentique/brain.go`.

Frontend — `frontend/src/`: `lib/brain-api.ts`, `stores/brain-store.ts`,
`hooks/useBrainSubscriptions.ts`, `components/brain/BrainPage.tsx`,
`components/brain/BrainGraph.tsx` (force-graph view, RFC graph-view v1; gains a
`compact`/`focusId` mode), `components/brain/MemoryReview.tsx` (the dedicated review
surface — merge proposal *inputs → output*, confidence/state, Confirm/Edit/Delete +
AI refine; launched from the Brain toolbar's "Review (N)" button),
`lib/scope-color.ts` (per-scope colours shared by the graph and the review surface),
nav flare in `components/layout/AppSidebar.tsx` (`brain-flare` in `index.css`).

**To add a feature:** keep policy (model choice, scope mapping, env) in the glue or
caller; keep `internal/memory` portable (no agentique imports — verified by the
playbook in `docs/agentkit-extraction.md`). Mutations should broadcast
`brain.EventBrainUpdated`; long model work runs off the request context as a job.

## Known limitations / remaining work

Several items below are feedback-loop gaps — recall doesn't strengthen or update memory,
salience doesn't gate consolidation, the episodic stage is skipped. They are consolidated into
a forward design: [brain-learning-dynamics.md](brain-learning-dynamics.md) (RFC: learning
dynamics — what the brain borrows next from human-memory research).

- **The `Extractor` is the caller's, with a caller-chosen model.** `ClaudeExtractor`
  takes the model as a required parameter (no library default). It drives
  `brain backfill`, the preview/apply consolidation (per-scope consolidation + cross-scope
  global), auto-encode, and scheduled consolidation. (Anthropic has no embeddings
  API, so semantic recall still needs an external embeddings endpoint.)
- **Recall is pushed per-turn, and the outcome loop is closed (shipped).**
  Auto-recall pushes *pinned* facts + the *operating contract* (preamble) and
  *task-relevant* top-K recall **every turn** (`Service.RecallBlock` →
  `Manager.MemoryRecallFn` → `Session.injectRecall`, delta-deduped against a session
  seen-set), so the agent gets relevance-ranked context without calling `MemorySearch`.
  Injected facts get `BumpUses`/`LastUsedAt` ("shown"). The loop's *outcome* half now
  exists too (RFC-LD **D2**, [brain-outcome-signal.md](brain-outcome-signal.md)):
  `MemoryUsed`/`MarkHelped` strengthen a *confirmed-useful* fact (raising confidence
  toward a 0.95 corroboration ceiling) and `MemoryFlag`/`MarkContradicted` weaken a
  contradicted one — so strength now changes on outcome, not just injection. **Still
  open:** the signal is agent-volunteered (no *automatic* session-end judge yet), so on
  a fresh corpus `helped` is 0 until agents adopt the tool — the durable fix is the
  automatic emitter (RFC-LD decision #2).
- **Link graph is recompute-on-consolidate, not curated.** `RelinkScope` rebuilds
  similarity edges each apply; there's no curated/human `[[link]]` UI yet, and the
  graph view still draws client-side Jaccard for dashed edges on top. RFC P3
  (community detection → cluster-aware consolidation + aggressive consolidation), P2
  (confidence tiers + degree/betweenness centrality + the confirm UX) and P5
  (cross-scope-community guardrail + content-hash manifest for global promotion)
  are **done** — all RFC proposals are now shipped (neo4j export remains a parked
  non-goal).
- **Episodic `capture` staging is the ingest tier (Band 1 M2/M3 — shipped).** Auto-encode now
  stages RAW captures (`SourceCapture`, never injected) on session completion and delete; the
  scheduled consolidation is the only `capture → consolidated` promotion path. The remaining D4
  work is the churn's *replay-and-abstract* of those captures (batch abstraction per Complementary
  Learning Systems), which the Band 2 Curator delivers.
- **Chroma collection space.** The collection is created with cosine distance;
  changing the embedding model/space requires a fresh collection name (a stale
  collection of the same name created with a different space would skew scores).
