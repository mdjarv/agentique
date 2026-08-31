# The brain — persistent agent memory

A knowledge store agents read from and write to across sessions. Facts learned in
one session (conventions, preferences, gotchas, decisions) come back in later
ones, surviving the per-worktree isolation that otherwise resets an agent's
context every run.

Three phases, borrowed from how human memory is described:

- **recall** — the agent gets what is already known, pushed in rather than pulled.
- **encode** — durable facts are saved; raw turn material is staged as episodic
  *captures*.
- **consolidate** — a periodic pass promotes captures into facts, merges
  duplicates, abstracts repeated episodes into rules, and ages out what has gone
  unused.

## Layering

`backend/internal/memory` is the liftable core: policy-free machinery depending
only on the standard library, `google/uuid` and `yaml.v3`, all already in
agentkit's `go.mod`. `backend/internal/brain` is agentique's policy on top of it:
scope-is-project, config, the REST and MCP surfaces.

The dependency direction is the invariant. `internal/memory` imports nothing from
agentique, which is what makes the lift a directory move and an import rename.
`docs/agentkit-extraction.md` is the mechanical playbook for doing it once a
second consumer (formica, hittat) needs memory.

Model choice is a required caller parameter in the core, never a library default.
That is a lift constraint, not a style preference.

## Storage

**Markdown is the source of truth.** One file per memory under
`<data-dir>/brain/<scope>/<id>.md`, YAML frontmatter plus the fact as the body.
Greppable, hand-editable, git-friendly. A hand-edit is picked up on the next read.
Editing through the UI marks the record `source: human`, which exempts it from
consolidation rewrite and decay.

**Everything else is a rebuildable index.** The graph, the areas and the Chroma
vector collection all derive from those files. Durable writes never fail because
an index is down, and recall degrades to keyword ranking on any vector error or
empty result. Scope is written into vector metadata and used as a query-time
`where` filter, so semantic search is isolated per scope at the source rather
than post-filtered.

Chroma 1.x does not embed server-side, so semantic recall needs an `Embedder`.
`embedhttp` calls any OpenAI-compatible embeddings endpoint.

### Labels

Every record carries a controlled vocabulary that the churn and the aging pass
branch on:

- `Evidence` — `user_stated`, `code_verified`, `corroborated`, `inferred`,
  `observed_once`
- `Volatility` — `evergreen`, `slow`, `ephemeral`, which sets the decay rate
- `Lifecycle` — `active`, `superseded`, `archived`
- typed `Relations` — supersedes, contradicts, duplicates, generalizes,
  corroborates. Replaces the untyped `Related`, which is retained.
- free-form `Keywords`, plus `LastCurated` and `CuratorNote`

Defaults flow from source (human to `user_stated`, capture to `observed_once`,
otherwise `inferred`; a non-human fact with `Helped >= 2` and no contradiction
becomes `corroborated`) and from category (identity to `evergreen`, task to
`ephemeral`, otherwise `slow`). `NormalizeLabels` fills empties on load and never
overwrites an explicit value, so it is idempotent and safe against human curation.

### Aging, and why it archives rather than deletes

Confidence is a living scalar: the stored `ConfidenceScore` eroded by time since
last use on a volatility-keyed half-life (slow 90d, ephemeral 14d, evergreen
never), clamped up to an evidence floor (trusted 0.50, which sits above the
archive line so trusted facts never fade out; inferred 0.30; observed-once 0.15).
This effective confidence is computed at recall by `memory.EffectiveConfidence`
and **never written on a nudge**.

Forgetting means archiving. Once a fact has faded below the archive floor and
gone untouched longer than `archive-after`, the churn sets
`Lifecycle=archived`: a cold tier excluded from recall, promotion, areas, the
graph and the review queue, but kept on disk and restorable. A hand-edit revives
it and restarts its clock. Human, pinned, locked and evergreen facts never erode
and never archive. **Nothing in the aging path deletes a record.**

Two safeguards on the way in: the read-time fade is gated on archiving being
enabled, and `archive-after` defaults to off, so nothing drops until an operator
opts in after curating. The label backfill stamps `last_used=now` where it is
zero, so the disuse clock starts at the migration boundary rather than at an
ancient `updated`.

## Recall

`memory.Recall` returns pinned facts, always, plus the top-K query-relevant
non-pinned facts. Episodic captures are never recalled.

Ranking is keyword-only (idf-weighted token overlap, category boosts, recency
tiebreaker) unless the Store implements `Searcher`, which Chroma does. Then it
blends vector and keyword scores, degrading cleanly to keyword-only when the
vector path is unavailable or returns nothing.

**Associative recall** folds in a bounded set of each top match's `Related`
neighbours after the flat top-K (at most 3 per seed, at most K total) at lower
priority. It reads the persisted link graph, so nothing is recomputed on the hot
path, and it is only active on scopes that have been consolidated.

### Recall is fluid and per-turn

The system preamble is fixed at connect, before the task prompt exists, so
query-dependent recall cannot live there. Two pushes instead:

- **Pinned facts to the system preamble** at create and resume. Always-on facts,
  injected before any prompt exists.
- **Task-relevant recall on every turn.** `Session.injectRecall` runs
  `memory.Recall` against the actual prompt each turn, so recall follows the
  conversation as it drifts. A session-level seen-set is passed as `exclude`, so
  each turn injects only what is newly relevant rather than re-dumping. A
  low-content gate (`memory.TokenCount < 2`) skips trivial turns like "ok".

Do not reintroduce first-turn-only recall.

Hits are prepended as a `<brain><fact id="…">…</fact></brain>` envelope, which
gives the model an unambiguous memory-versus-user boundary and gives the frontend
something to parse into a "Recalled from memory" card. The framing (background
context, verify first) and the `MemoryUsed`/`MemoryFlag` hooks are explained once
in the system preamble, so the per-turn block stays compact.

A 3s timeout bounds each lookup. It degrades to no injection when the brain is
off, recall is slow or fails, or nothing new matches. A read-through corpus cache
keeps the per-turn `List` cheap.

Each newly-surfaced fact gets `BumpUses` and `LastUsedAt` stamped, so per-turn
recall doubles as the read signal feeding two-factor strength, strength-weighted
decay and spaced review. Those were starved when recall was pull-only and fired
once.

## Consolidation

`memory.Consolidate` is conservative by construction:

- **promote** captures into durable facts through the LLM `Extractor`, deduped
  against existing facts. Identity facts are auto-pinned. An empty extraction
  never consumes captures.
- **reorganize** the non-protected durable set: merge duplicates, rewrite vague
  entries, abstract repeated episodes into rules. Invented IDs are dropped. An
  over-deletion guard refuses a reorganization that shrinks a set of 8 or more
  facts below a survivor ratio (0.5 by default; an aggressive pass lowers it to
  0.2). The ratio is captured into the `Plan`, so preview and apply enforce the
  same guard.
- **decay** stale, low-use facts, opt-in through `DecayPolicy`. Weighting by
  confidence, strength or salience makes the brain forget what it is least sure
  about first.
- **never touches** pinned, locked or human-authored facts.
- **relink**, on a real apply: `RelinkScope` rebuilds the scope's `Related` edges
  from similarity neighbours. Previews skip it, since it is derived metadata.
- **cluster**, after relink: `AssignCommunities` recomputes each fact's
  scope-local topic cluster. Deterministic and idempotent; previews skip it.

A fingerprint of the reorganizable set is persisted per scope, so an unchanged
set skips the expensive LLM reorganization.

Chunking is cluster-aware: facts are tagged with a topic community and whole
communities are packed into one reorganize call, so related facts merge across a
large scope rather than only within an arbitrary 100-fact slice.

The extract prompt deliberately prefers **fewer, broader** facts (cap 3), skips
code-discoverable trivia unless it is a surprising gotcha, and records only facts
about the session's own project. Scopes stay high-signal instead of accumulating
implementation details.

**Consolidation is preview then apply.** The preview runs the model once and
returns a held plan; apply replays that plan deterministically with no model call
and returns `409` on a stale plan. Background jobs run off the request context, so
a request hiccup cannot SIGTERM the model subprocess. Job state is in-memory, one
active job at a time: a backend restart drops an in-flight preview, which is
harmless because a preview is a dry run, and the frontend re-hydrates on WS
reconnect and clears a stale spinner.

## Snapshots and rollback

Every churn is made reversible by a filesystem copy taken before it runs.
Snapshots live in `brain/.snapshots/<ts>/` with UTC timestamp ids, so lexical
order is chronological.

That directory is invisible to recall and consolidation because `filestore.List`
is non-recursive and reads only the direct `*.md` of each top-level scope dir.
`.snapshots` holds no direct `*.md`, so it yields zero records and never enters
`ListScopes` or `Recall`.

Scheduled consolidation snapshots the whole brain at the top of each pass. A
snapshot failure is logged at WARN and does **not** block the pass, because the
archive-not-delete churn keeps the pass reversible regardless. Retention keeps the
newest `snapshot-retain` (7 by default).

Live restore holds the service lock so the file rewrite cannot race a single-fact
write, takes a pre-restore safety snapshot, restores the tree, then invalidates
the read-through cache. Without that invalidation the cache keeps serving the
pre-restore corpus until the next write. It then broadcasts `brain.updated` so
every tab refetches. In semantic mode the vector index is not reindexed here; it
reconciles lazily per fact on the next write, or on a `reindex` or restart. The
memory list is correct immediately while recall ranking may be briefly stale.

## Agent surface

Auto-approved MCP tools, scoped to the calling session's project plus global:

| Tool | Effect |
|---|---|
| `MemoryAdd(text, category)` | Save a durable fact. |
| `MemorySearch(query)` | Recall pinned plus relevant facts. Output carries each fact's id. |
| `MemoryUsed(id)` | Confirm a recalled fact helped. Strengthens it toward a 0.95 corroboration ceiling. |
| `MemoryFlag(id, reason)` | Flag a recalled fact as wrong or outdated. Weakens it into the review queue, never deletes. |

Ids come from `MemorySearch` output or from a recalled-memory block.

## Wire types are hand-synced

There is no typegen for brain types. Every `memoryDTO`, `snapshotDTO` and
`statusCounts` change in `internal/brain/http.go` needs a mirror edit in
`frontend/src/lib/brain-api.ts`. This is the one place in the repo where a Go
wire change does not propagate by running a generator.

## Scope model

agentique is single-user, so scopes are project-based: `global` for cross-project
facts, `project:<id>` for codebase-specific ones. A session reads its project
scope plus global. The scope string is opaque to the core, so another consumer
can map its own concepts (board, persona, whatever).

## Configuration

The brain is on by default with keyword recall over markdown at
`<data-dir>/brain`. Semantic recall is opt-in and needs all three of
`chroma-url`, `embed-url` and `embed-model` set, with Chroma answering a
heartbeat. Otherwise it logs a warning and uses keyword recall.

Every key is settable in `config.toml` under `[brain]`, with an
`AGENTIQUE_BRAIN_*` environment override that wins. README's configuration
section has the full table.

The two thresholds are embedding-model specific:

- **Vector veto** drops a candidate the embedder scores as actively unrelated,
  regardless of keyword match. Semantics can exclude, not just re-rank.
- **Vouch bar** is the complementary lever on the way in.

Defaults (0.45 and 0.15) are all-MiniLM numbers. For another model run
`agentique brain calibrate`, which prints the corpus's own cosine distribution
and the percentile-derived values, or set `autocal` to derive them at boot. An
explicitly-set threshold still wins per knob.

The Chroma collection is created with cosine distance. Changing the embedding
model or space needs a fresh collection name, because a stale collection of the
same name created with a different space skews scores.

Index maintenance is lazy: each `Put` indexes one fact. A bulk hand-edit of the
markdown or an embedding-model change leaves vectors stale until a later pass
touches each fact. `agentique brain reindex` rebuilds the whole collection in one
shot from the markdown. The slow self-heal is scheduled consolidation, which runs
once shortly after server start and then on `consolidate-interval`, so a
frequently-restarted server can no longer defer that refresh forever.

## Automation

The loop runs on its own, not just from the CLI and UI.

**The subsystem is opt-in.** `[brain] enabled` is the master switch and defaults
to false, so everything in this document is inert until it is set. Off means the
brain is never constructed: no `/api/brain` routes, no memory MCP tools, no
recall, no session-end learning, no scheduled consolidation, and `features.brain`
in `/api/health` is false so the SPA drops the Brain destination rather than
offering a link that lands on the catch-all. Nothing on disk is touched, so
turning it back on resumes with the store intact. A config carrying other
`[brain]` keys while the switch is off logs a line at boot naming the switch —
settings that silently do nothing are worse than settings that are absent.

**Auto-recall** is on by default *once the subsystem is on*; `recall = "off"`
disables it. Covered above. Note it is a quoted string rather than a bool
because it defaults on, and a Go bool cannot separate "unset" from "false";
`enabled` has no such problem precisely because it defaults off.

**Auto-encode is opt-in and stages captures only.** With `learn-model` set, a
finished session's transcript is distilled into raw captures (`source: capture`,
never injected) in the project scope, asynchronously, skipping trivial sessions.

The only path from capture to injectable fact is the churn. So a deployment with
a learn model set **must also enable scheduled consolidation with a consolidate
model**. Promotion is LLM-only, so an interval set with `consolidate-model` empty
runs deterministic dedup and decay that never drains captures, and they pile up
forever. Set both.

Re-observing a known fact reinforces the durable fact instead of stacking a
redundant capture. The dedup set stays durable-only, so capture-versus-capture
still never dedups.

**Scheduled consolidation is opt-in** through `consolidate-interval`, with
`consolidate-model` for LLM reorganization (otherwise deterministic dedup and
decay). Auto-apply is safe because of the consolidation guards.

Every memory change broadcasts a `brain.updated` WebSocket event that flares the
nav button and refreshes open tabs.

## CLI

`agentique brain …` covers inspection (`list`, `show`, `search`, `stats`),
snapshots (`snapshot`, `restore`), churn (`consolidate`, `assign-areas`,
`calibrate`, `reindex`), migration (`backfill`, `backfill-labels`,
`backfill-subsumed`) and portability (`export`, `import`). README's CLI reference
has the table; `--help` has the flags.

One gotcha: a non-release (`go run`) build resolves a *relative* `agentique.db`,
so point it at the real data dir with
`AGENTIQUE_DB=~/.local/share/agentique/agentique.db`.

`brain restore` refuses to run when a server pidfile is live, because rewriting
files under a running cache is unsafe. `-f` overrides.

## Runbook: local embedder and Chroma

```bash
# 1. Ollama. CPU is fine for embeddings. The tarball needs no root.
curl -fSL https://github.com/ollama/ollama/releases/latest/download/ollama-linux-amd64.tar.zst \
  | tar --use-compress-program=unzstd -xf - -C /tmp/ollama
OLLAMA_HOST=127.0.0.1:11434 OLLAMA_MODELS=/tmp/ollama/models \
  LD_LIBRARY_PATH=/tmp/ollama/lib /tmp/ollama/bin/ollama serve &
/tmp/ollama/bin/ollama pull all-minilm     # 45 MB, 384-dim

# 2. Chroma v2. The client uses /api/v2.
docker run -d --name chroma -p 127.0.0.1:8000:8000 chromadb/chroma:latest

# 3. Point the brain at them, in config.toml or the environment.
export AGENTIQUE_BRAIN_CHROMA_URL=http://127.0.0.1:8000
export AGENTIQUE_BRAIN_EMBED_URL=http://127.0.0.1:11434/v1/embeddings
export AGENTIQUE_BRAIN_EMBED_MODEL=all-minilm

# On boot, look for:
#   brain: semantic recall enabled ... cosineThreshold=0.45 vectorVeto=0.15
# and with autocal:
#   brain: semantic thresholds auto-calibrated ... cosineThreshold=0.42 ...

# 4. Verify. These integration tests are env-gated; they re-measure and assert.
CHROMA_TEST_URL=http://127.0.0.1:8000 \
EMBED_TEST_URL=http://127.0.0.1:11434/v1/embeddings \
EMBED_TEST_MODEL=all-minilm \
  go test ./internal/memory/chroma/ -run TestSemanticRecallVetoesGithubMisRecall -v
go test ./internal/brain/ -run TestBrainSemanticWiring -v
```

Run both containers with `--restart unless-stopped` if you want this to survive a
reboot.

## Why it works this way

Condensed from nine RFC and ADR documents written between April and June 2026.
The originals are in git history: `git log --diff-filter=D -- docs/brain-*.md`.

### Graph layer

The brain had facts but no structure. `Related` was dead, so recall could only
find what a query lexically matched. Making the corpus a graph and using it for
retrieval, clustering and consolidation was the fix.

What is worth keeping from the decisions:

- **Similarity edges are persisted** into `Related` and rebuilt on each apply. The
  graph view still recomputes Jaccard for its dashed edges.
- **Label propagation** for communities, made deterministic by id-sorted node
  order and smallest-id tie-breaks. Reproducible plans without Louvain's extra
  code.
- **Topic clustering uses a lower Jaccard threshold** (0.15) than the 0.3
  `Related`-edge threshold. Measured on the live reviewbot scope: at 0.3, 386 of
  404 facts are singletons and chunking degenerates to fixed-size. 0.15 yields
  coherent clusters and co-locates 229 facts the old chunker scattered, while
  staying above the roughly 0.10 point where everything collapses into one blob.
- **Confidence backfills lazily from `Source` on load** and is never blank,
  persisted on the record's next write. No migration pass.
- **The graph index is computed per request**, with the expensive semantic kNN
  memoized by a corpus fingerprint.
- **Embeddings became weighted kNN edges, not positions.** Cosine weights force
  strength, distance and visual weight together. The PCA-projection layout was
  retired deliberately: the graph is a model of a mind, not a scatter plot.

neo4j is a documented non-goal at this scale, parked as an optional export.

### Learning dynamics

The graph gave the brain structure; this gave it feedback loops, so a fact's
standing changes with what happens to it rather than being frozen at encode time.

Two-factor strength separates storage strength (derived from confidence,
cumulative uses and derivation depth) from retrieval strength (decays with
disuse). The one new persisted field is `LastUsedAt`, needed for disuse aging.

**Recall is a write.** That is the keystone. All three signals ship: shown
(`BumpUses` stamps `LastUsedAt`), contradicted (`MemoryFlag`), and
confirmed-useful (`MemoryUsed`). Interference detection and spaced review follow
from it.

Episodic staging and replay is the piece still missing; it is the natural consumer
of salience.

### The outcome signal

The brain strengthened a fact when it was *shown*, not when it *helped*, so trust
never reflected usefulness.

A confirmed-useful outcome now raises confidence toward a **0.95 corroboration
ceiling**: gap-closing, and deliberately below human ground truth. **Human
confirmation outranks corroboration, always.** A contradiction knocks it down.
Trust is calibrated by outcome and gates behaviour at `ActOnConfidence`, which is
what promotes high-confidence preferences into the operating contract the agent
follows without re-asking.

A session-end LLM judge recovers the facts recall injected and emits the same
signal itself, so the loop does not depend on an agent remembering to call the
tools. An automatic `helped` weighs **half** an explicit one; the negative half
keeps a high evidence bar.

### Salience gating

Consolidation already strengthened by outcome. Salience lets outcome decide what
consolidation keeps and forgets, gating reorg retention and decay weighting.

Neutral salience is **0.5, not 0**: an unproven fact must not be treated as a
disproven one. Deliberately not done: telling the model about salience. It is a
property of what happened, not a hint to be gamed.

### Semantic recall

Lexical recall is blunt. Keyword overlap surfaces facts that merely share
vocabulary, which is how a query about one thing recalls an unrelated fact that
happens to mention "github". The cure is vector recall blended with keyword rather
than replacing it.

The **vector veto** drops a candidate the embedder scores as actively unrelated,
regardless of keyword match, so semantics can exclude and not just re-rank. The
**vouch bar** is the complementary lever on the way in. Both are
embedding-model-specific, which is why `calibrate` and `autocal` exist; the
Configuration section above covers tuning them.

Everything degrades cleanly to keyword and Jaccard with no embedder configured.
That is a hard requirement, not a nicety: recall never fails because optional
infrastructure is down.

Per-turn recall latency is why the corpus read is cached and why each lookup is
bounded by a 3s timeout.

### Cross-scope areas

Measured first, on a copy of the live brain: 1417 of 1510 durable facts were
isolated. The link graph was about 94% disconnected, so associative recall had
almost nothing to traverse. Cross-scope structure was the real win, not more
within-scope linking.

Topic **areas** are the cross-project sibling of scope-local communities,
recomputed on consolidation apply, feeding sibling-scope associative recall. Area
labels are TF-IDF derived, which replaced noisy frequency-based labels. Semantic
links blend Jaccard with embedding cosine through one pluggable similarity
primitive wired into relink, community detection and areas.

### Band 1, "Migrate"

An audit found the brain captured but did not evolve. About 98% of facts carried
`source=consolidated`, about 97% were frozen at confidence 0.80, scheduled
consolidation passed an empty `DecayPolicy{}` so decay never fired, re-observing
a known fact was a no-op, interference detection never reconciled, and learning
only triggered on session *deletion*.

Band 1 turned that into an ingest, churn and inject pipeline with an injection
gate. The reversibility rules that came out of it are load-bearing: markdown is
the source of truth, everything else is a rebuildable index; archive, never
delete; snapshot before every churn.

Band 2, the Curator, is still design-only.

### Brain UI

Band 1 was backend-only by design, so none of it was visible or manageable. The
Brain tab makes it so.

Every memory row is self-describing: a capture, archived or superseded badge,
compact evidence and volatility chips, and a corroboration count. The defaults
(evidence `inferred`, volatility `slow`, lifecycle `active`) render nothing, so
ordinary rows stay quiet.

The list shows only live injectable facts by default. Two toolbar toggles reveal
captures and archived rows, each with a count. Filtering is component-local rather
than in the store, because the stable-selector rule keeps derived lists out of
selectors.

An archived row has a Restore action. A Snapshots panel lists, takes and restores
snapshots, guarded by a confirm and blocked while a consolidation runs. The graph
draws typed relations as distinct directed edges coloured by kind, can colour by
evidence or volatility, excludes archived nodes and dims superseded ones. A Health
popover shows the corpus distribution: capture backlog, archived and superseded
counts, the evidence, volatility and confidence spread, and the review queue.

A later UX round made the graph the default view, turned regions off by default,
and put full text on hover.

## Open questions

- **Episodic staging and replay.** The largest remaining piece. Activates the
  dormant capture path and attacks scope bloat at the root. The churn's
  replay-and-abstract of captures is what Band 2 delivers.
- **Recall fan-out budget.** How many hops and neighbours associative recall may
  expand, and what decay to apply to associative hits, without blowing the token
  budget.
- **Reconsolidation gating.** How much recall may change a fact without human
  review, and how an auto-update is marked in provenance.
- **Scheduler placement.** Spaced review as part of scheduled consolidation, or
  its own lighter tick.
- **Outcome-judge tuning.** The session-end judge's precision and recall over
  organic sessions, and the 0.25 automatic weight, want a live soak.
- **Persisted cross-scope edges.** Deferred; tracked in `docs/tech-debt.md`.
</content>
