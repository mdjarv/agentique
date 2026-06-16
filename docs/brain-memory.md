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

## Consolidation ("sleep")

`memory.Consolidate(store, extractor, scope, opts)` is conservative by
construction:

- **promote** episodic captures into durable facts via the `Extractor` (LLM),
  deduped against existing facts; identity facts are auto-pinned. An empty
  extraction never consumes captures.
- **reorganize** the non-protected durable set: merge duplicates, rewrite vague
  entries, abstract repeated episodes into general rules. Invented IDs are
  dropped; an over-deletion safety net refuses a reorganization that would shrink
  a set of ≥8 facts to under half (counting both retained and abstracted facts).
- **decay** stale, low-use facts (opt-in via `DecayPolicy`).
- **never touches** pinned, locked, or human-authored facts.
- a **fingerprint** of the reorganizable set is persisted per scope; an unchanged
  set skips the (expensive) LLM reorganization.

The pass returns a `Report` (the changelog) describing exactly what changed.

## Agent surface (MCP tools)

Auto-approved, scoped to the calling session's project (+ global):

- `MemoryAdd(text, category)` — save a durable fact.
- `MemorySearch(query)` — recall pinned + relevant facts.

## Brain tab REST API

`/api/brain/memories` (GET/POST), `/{id}` (GET/PUT/DELETE), `/{id}/pin`,
`/{id}/lock`, `/search`, `/consolidate`, `/status`.

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

## Scope model

agentique is single-user, so scopes are project-based: `global` for cross-project
facts, `project:<id>` for codebase-specific facts. A session reads its project
scope plus global. The scope string is opaque to the core, so other consumers can
map their own concepts (board, persona, …).

## Known limitations / remaining work

- **No LLM `Extractor` is wired yet.** `MemoryAdd` (agent-curated) is the primary
  write path and needs no LLM. Consolidation currently performs deterministic
  decay/dedup; promotion and reorganization activate once an `Extractor` is
  configured. (Anthropic has no embeddings API, so semantic recall needs an
  external embeddings endpoint.)
- **Auto-capture on turn-end is not wired.** Deliberately deferred until an
  `Extractor` exists, so raw captures don't accumulate undistilled.
- **Preamble push-injection is deferred.** Recall today is pull-based via
  `MemorySearch`. Always-on injection of pinned facts into the session preamble
  is a planned enhancement.
- **Chroma collection space.** The collection is created with cosine distance;
  changing the embedding model/space requires a fresh collection name (a stale
  collection of the same name created with a different space would skew scores).
