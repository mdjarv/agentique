# The brain — the assistant's long-term memory

**As of M2 this store has one reader: the assistant** (`docs/assistant.md`). It
was built for a coding agent and disabled for adding noise — a coding agent has
the repo, CLAUDE.md and git history in front of it, and facts injected beside
those competed with them, arrived without provenance, and were judged by an
outcome signal a session never gives cleanly. The facts it holds are about the
operator's world, which is what the assistant needs on every turn.

So memory reaches no coding session. Nothing is injected into a session's
preamble or its turns, no session tool writes a fact, and nothing learns from a
finished transcript. What remains is the store, its churn, its page and its CLI —
and one reader that pulls from it because it knows what it is trying to do.

A knowledge store of durable facts about the operator's world: conventions,
preferences, gotchas, decisions. Three phases, borrowed from how human memory is
described:

- **recall** — the assistant pulls what is already known, by asking.
- **encode** — durable facts are saved; raw material is staged as episodic
  *captures*.
- **consolidate** — a periodic pass promotes captures into facts, merges
  duplicates, abstracts repeated episodes into rules, and ages out what has gone
  unused.

## Layering

`backend/internal/memory` is the liftable core: policy-free machinery depending
only on the standard library, `google/uuid` and `yaml.v3`, all already in
agentkit's `go.mod`. `backend/internal/brain` is agentique's policy on top of it:
scope-is-project, config, and the REST surface the memory page reads.

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
opts in after curating. A third follows from where the fade is applied — only on
the model-facing pull (`Service.RecallForPull`), never on the browsing recall the
memory page and `brain search` read, so what is fading stays curatable. The label backfill stamps `last_used=now` where it is
zero, so the disuse clock starts at the migration boundary rather than at an
ancient `updated`.

## Recall

`memory.Recall` returns pinned facts, always, plus the top-K query-relevant
non-pinned facts. Episodic captures are never recalled.

Ranking is keyword-only (idf-weighted token overlap, category boosts, recency
tiebreaker) unless the Store implements `Searcher`, which Chroma does. Then it
blends vector and keyword scores, degrading cleanly to keyword-only when the
vector path is unavailable or returns nothing. Whether the Chroma store is in play
at all is decided per call, by whichever vector backend is attached at that moment
(see "The vector backend attaches at runtime" below).

**Associative recall** folds in a bounded set of each top match's `Related`
neighbours after the flat top-K (at most 3 per seed, at most K total) at lower
priority. It reads the persisted link graph, so nothing is recomputed on the hot
path, and it is only active on scopes that have been consolidated.

### Knowledge is pulled; only news is pushed

The noise was push: a retriever guessing relevance from keyword or cosine overlap
and injecting its guess into every turn, judged by a model that had not asked.
So nothing pushes facts anywhere. The assistant's head carries the **pinned set**
and an **index** (one line per area or scope with a count) in its preamble, on the
precedent of a memory directory whose index is loaded and whose bodies are read on
demand; the bodies arrive only through the `recall` verb, which the head calls
because it knows what it is trying to do.

`Service.RecallForPull` is the implementation behind that verb, reached through
`assistant.Memory` (`internal/server/assistant_memory.go`). It runs
`memory.Recall` over every scope `ListScopes` answers, with `k` clamped
server-side, and hands back records rather than prose — the head sees facts with
their ids, categories, sources and confidence tiers. Pinned facts are dropped from
the answer, because they are already in the preamble with their ids.

**Two recalls, one query, and the difference is the read-time fade.**
`Service.Recall` is the same query for a *person* — the memory page's search box
and `agentique brain search` — and applies no `ArchiveFloor`. `RecallForPull` is
the same query for a *model* and applies the configured one, so a fact that has
gone cold stops being asserted to a head a while before the churn archives it.
The split is not decoration: a faded fact has not been archived yet, so it is
still a live row in the list beside that search box, and a row you can see and
cannot find by searching for it is one surface telling the operator two things.
Both build their query in one place (`Service.recall`), so the veto and vouch
thresholds cannot drift between them.

`Service.RecallBlock` is the OLD path and nothing calls it: it composed the
`<brain><fact id="…">…</fact></brain>` envelope a session's turn was injected
with, and `exclude` was the per-session seen-set that made that delta. Its doc
comment says so, at length, because wiring it back is the one change this section
forbids. The frontend still parses the envelope, for the transcripts recorded
before M2 that carry one.

**Do not reintroduce injection into sessions** — not first-turn, not per-turn, not
pinned facts in a preamble. Memory reaches a coding session only through the
prompt the assistant drafts, in text the operator can read and edit before it
goes: visible, never injected.

A read-through corpus cache keeps `List` cheap. Each fact a pull returns gets
`BumpUses` and `LastUsedAt` stamped, so recall doubles as the read signal feeding
two-factor strength, strength-weighted decay and spaced review. Corroboration
improved for free in the move: a fact the head pulled and then used is a real
signal, where an injected fact that was maybe read was not.

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

**There is none.** A coding session sees no memory tool, and its tool list does
not carry one: `mcphttp` registers no memory group, and the `MemoryAdd` /
`MemorySearch` / `MemoryUsed` / `MemoryFlag` tools are gone rather than gated.
Sessions never write facts. A session that learns something worth keeping says so
through `AssistantReport`, which lands in the assistant's journal as untrusted
text; a notable entry becomes a capture for consolidation to judge.

The one reader reaches the store through the assistant's verb table
(`docs/assistant.md`): `recall`, `remember`, `confirm_memory`, `flag_memory`,
each with the provenance the write carries. Every write is journaled, so the
conversation holds a record of it.

`brain.Service` is what those verbs are built on — `RecallForPull`, `Add`,
`Capture`/`CaptureFrom`, `Confirm`, `Flag`, `SetPinned`, `List`, `ListScopes` —
plus `Recall` and the rest of the REST surface the memory page reads, and the
`agentique brain …` CLI.

## Wire types are hand-synced

There is no typegen for brain types. Every `memoryDTO`, `snapshotDTO` and
`statusCounts` change in `internal/brain/http.go` needs a mirror edit in
`frontend/src/lib/brain-api.ts`. The same holds for `SemanticStatus.Wire`
(`internal/brain/semantic.go`), which `/api/brain/status` and the `brain.semantic`
push both carry and `lib/brain-semantic.ts` reads. Its fields beyond `semantic` are
optional on the client, so an older server that sends only the boolean reads as
unknown rather than as "off". This is the one place in the repo where a Go
wire change does not propagate by running a generator.

## Scope model

agentique is single-user, so scopes are project-based: `global` for cross-project
facts, `project:<id>` for codebase-specific ones. A session reads its project
scope plus global. The scope string is opaque to the core, so another consumer
can map its own concepts (board, persona, whatever).

## Configuration

The brain uses keyword recall over markdown at `<data-dir>/brain`. Semantic
recall is opt-in and needs all three of `chroma-url`, `embed-url` and
`embed-model` set. With them set, recall is semantic while Chroma and the
embedder both answer, and keyword while either does not; the server keeps trying
for the life of the process.

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

Index maintenance is lazy: each `Put` indexes one fact. Every attach catches the
collection up first (`chroma.Store.IndexStale`: facts whose id is missing, or
whose indexed text differs, in batches of 64), so a fresh Chroma and the writes
made while the backend was away are indexed without a command. A write whose
vector upsert fails while attached marks the backend, and the next healthy probe
runs the same catch-up. None of that notices a bulk hand-edit made behind the
server's back or an embedding-model change, which leave vectors stale until a
later pass touches each fact; `agentique brain reindex` rebuilds the whole
collection in one shot from the markdown. The slow self-heal is scheduled consolidation, which runs
once shortly after server start and then on `consolidate-interval`, so a
frequently-restarted server can no longer defer that refresh forever.

## Automation

The loop runs on its own, not just from the CLI and UI.

**The subsystem is opt-in.** `[brain] enabled` is the master switch and defaults
to false, so everything in this document is inert until it is set. Off means the
brain is never constructed: no `/api/brain` routes, no scheduled consolidation,
and `features.brain` in `/api/health` is false so the SPA drops the memory
destination rather than offering a link that lands on the catch-all. Nothing on
disk is touched, so turning it back on resumes with the store intact. A config
carrying other `[brain]` keys while the switch is off logs a line at boot naming
the switch — settings that silently do nothing are worse than settings that are
absent.

**On with the assistant off, memory is stored and browsable and nothing recalls
it**, and serve says exactly that at boot. Nothing refuses to boot over it: the
store, the routes and the page all work, and `[experimental] assistant` is the
fix.

**Four keys are retired and ignored**: `recall`, `learn-model`, `outcome-model`
and `retry-max`, plus their `AGENTIQUE_BRAIN_*` overrides. They described
session-side recall and session-end learning, which are gone rather than switched
off. A config carrying one still decodes and still boots **whatever type it was
written as** — each is `config.RetiredKey`, an alias for `any`, because `recall`
used to be a string whose off switch was `"false"` and a typed field made the
bool spelling a decode error that refused to start the server. Each key is named
in its own warning at startup.

**Captures come from the assistant, never from a transcript.** Its `remember`
verb writes a fact outright; a notable journal entry becomes a capture when it is
written. There is no background pass over a finished session, so nothing stages
memory from work the operator never asked to be remembered.

The only path from a capture to a recallable fact is the churn, and promotion is
LLM-only — so a deployment that stages captures **must also enable scheduled
consolidation with a consolidate model**. An interval set with
`consolidate-model` empty runs deterministic dedup and decay that never drains
captures, and they pile up forever. Staging is not opt-in any more — every
notable journal entry is captured — so the pairing is enforced by a boot warning
rather than by the operator having set a `learn-model` first: brain and assistant
both on with either consolidate key empty says at startup that captures are being
staged and nothing can promote them.

Re-observing a known fact reinforces the durable fact instead of stacking a
redundant capture. The dedup set stays durable-only, so capture-versus-capture
still never dedups.

**Scheduled consolidation is opt-in** through `consolidate-interval`, with
`consolidate-model` for LLM reorganization (otherwise deterministic dedup and
decay). Auto-apply is safe because of the consolidation guards.

**Consolidation does not run while a configured vector backend is detached.**
Recall degrades to keyword because it sits on a model's latency path.
Consolidation does not, and it persists what it computes: `RelinkScope`,
`AssignCommunities` and the areas pass blend embedding cosine when there is an
embedder, so a pass during an outage would rewrite `Related`, communities and
areas from Jaccard alone and the next attached pass would rewrite them back.
`Consolidate`, `ApplyPlan`, `ApplyGlobal` and `AssignAreas` return
`ErrSemanticUnavailable` instead. Dry runs and `PreviewAreas` write nothing and
are not gated. A scheduled pass that finds the backend detached skips, logs why,
and runs on the next attach rather than waiting out the interval. Each pass takes
one backend snapshot and its vectors up front, so the scope in flight when the
backend goes finishes on the vectors it holds, and the next scope refuses. The
HTTP routes answer 409 with the reason, consolidate-all fails its job, and the
CLI refuses unless `--allow-lexical`.

Clustering looks a record's vector up by its text, not its id. A pass relinks
what it just wrote, so a rewritten fact carries its id under a new text and an
abstracted one has an id nothing had when the vectors were computed. Both are
embedded then (`Service.vectorFor`), a handful per pass, instead of falling to
Jaccard.

Every memory change broadcasts a `brain.updated` WebSocket event that flares the
rail's ⋯ trigger and Memory's row in it, and refreshes open tabs.

## CLI

`agentique brain …` covers inspection (`list`, `show`, `search`, `stats`),
snapshots (`snapshot`, `restore`), churn (`consolidate`, `assign-areas`,
`calibrate`, `reindex`), migration (`backfill`, `backfill-labels`,
`backfill-subsumed`) and portability (`export`, `import`). README's CLI reference
has the table; `--help` has the flags.

A one-shot command attaches the configured vector backend once, synchronously
(`Service.Connect`), with no catch-up and no retry. Unreachable, it says so and
carries on in keyword mode where that is good enough: `search` degrades,
`reindex` and `calibrate` refuse, and `consolidate` and `assign-areas` refuse
unless `--allow-lexical`, for the reason in Automation above.

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

**Where disk is tight, serve the embedder with text-embeddings-inference instead
of Ollama.** `ollama/ollama` is 3.7 GB compressed; TEI's CPU image is about 240
MB and serves the same all-MiniLM-L6-v2 on the same OpenAI route, so the default
thresholds hold (`TestSemanticRecallVetoesGithubMisRecall` passes against it).
It ignores the model field, so name the model it actually serves:

```bash
docker run -d --name agentique-embed --restart unless-stopped \
  -p 127.0.0.1:8081:80 -v agentique-embed:/data \
  ghcr.io/huggingface/text-embeddings-inference:cpu-latest \
  --model-id sentence-transformers/all-MiniLM-L6-v2 --auto-truncate
export AGENTIQUE_BRAIN_EMBED_URL=http://127.0.0.1:8081/v1/embeddings
export AGENTIQUE_BRAIN_EMBED_MODEL=sentence-transformers/all-MiniLM-L6-v2
```

**The vector backend attaches at runtime, not at boot.** `brain.New` never dials.
`Service.RunSemantic`, started from serve's production block, owns the backend
for the life of the process, and swaps it in and out as one atomically replaced
bundle (the Chroma store, the embedder and the thresholds derived for them) that
every operation reads once.

- **Attach.** A probe asks Chroma for its heartbeat and the embedder for one
  vector. The boot probe used to ask Chroma alone, which called a backend with a
  dead embedder semantic. Then the collection is caught up and published. The
  first attempt is immediate. After a failure it retries with backoff from 5s to
  two minutes.
- **Boot window.** Recall is keyword until the first attach completes. Against a
  populated collection of 1435 facts the attach took 244 to 308 ms (four boots),
  within 200 ms of `/api/health` first answering. Against an empty one it indexes
  everything first, 9 to 24 s for the same corpus on the CPU embedder, and recall
  is keyword for that long.
- **Detach.** While attached, it probes every 30s and detaches after two failed
  probes in a row, so a blip does not flap it and an outage is noticed within
  about a minute. Recall is then keyword, and nothing pays an embed timeout per
  call.
- **Auto-calibration** runs on the first attach of the process only. It embeds
  the whole corpus, and a flapping backend must not repeat that per flap.

The state is visible without reading a log. `/api/brain/status` carries
`semanticState` (`off`, `connecting`, `on`, `unreachable`) with a closed
`semanticReason` and `semanticDownSince`. The Memory page's badge reads amber
Keyword naming the failed half while the backend is unreachable, and follows an
attach or a detach live through a `brain.semantic` push. After ten minutes down,
the steward opens `semantic-recall-down` (`docs/peers.md`), which puts the mark
in the footer.

So after (re)creating the containers there is nothing to run: the next attempt
attaches and indexes the empty collection. `agentique brain reindex` is still the
tool for a model change or a hand-edit.

**Never point a scratch server at the live Chroma.** Attaching catches the
collection up from that server's own brain directory, and `IndexStale` upserts by
id. A sandbox running on a copy of the brain overwrites the live index's
documents and vectors for every id the two share, with the copy's text, and the
live server corrects them only at its own next attach. Give a verify server its
own Chroma (a throwaway `chromadb/chroma` container on another port) or no
`chroma-url` at all. `IndexStale` never deletes, so a sandbox cannot empty the
live index, but that is the only guarantee. Namespacing the collection per data
directory is in `docs/tech-debt.md`.

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
(`BumpUses` stamps `LastUsedAt`), contradicted (`Flag`), and confirmed-useful
(`Confirm`/`MarkHelped`). Interference detection and spaced review follow from
it. The tool names those two wore in the session era (`MemoryFlag`,
`MemoryUsed`) are gone; the verbs the assistant calls are `flag_memory` and
`confirm_memory`.

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

The signal is **conversational** as of M2. "Yes" and "no, we changed that" are
in-band on the assistant's thread, which is the human confirmation this design
ranks above corroboration and could never get from a session: `confirm_memory`
and `flag_memory` are the two verbs. The session-end LLM judge that used to
recover the facts recall had injected and rule on them is gone with the injection
it audited. `MarkAutoHelped` — an automatic `helped` weighing **half** an explicit
one — survives in the service with no producer; it is what a future non-human
corroborator would use.

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
infrastructure is down. A backend that is configured and unreachable is the
narrower case, and the two halves split there: recall still degrades, and a
consolidation pass refuses, because it would persist the degraded graph (see
Automation).

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

Band 1 turned that into an ingest, churn and recall pipeline with a gate on what
is recallable. Its injection half is what M2 removed; the reversibility rules that
came out of it are load-bearing and unchanged: markdown is the source of truth,
everything else is a rebuildable index; archive, never delete; snapshot before
every churn.

Band 2, the Curator, is still design-only.

### Brain UI

Band 1 was backend-only by design, so none of it was visible or manageable. The
page makes it so. It lives at `/assistant/memory` and is titled "Memory", under
the assistant that reads it; `/brain` stays as a redirect, and `features.brain`
still decides whether it is offered. It has **two ways in**, by the operator's
choice (2026-09-15), and is the one destination that does: the rail's ⋯ menu
(`AppSidebar`), because the brain reports on its own and the flare on that
menu's trigger is the report, and a button on the assistant thread's band
(`AssistantThreadHeader`), because the thread is where a recalled fact is read.
The rail row is also the one that exists with the assistant off. The heading
keeps the old name because a dozen code comments cite `brain.md#brain-ui` as
the spec anchor for F0-F6.

The thread shows which facts a reply was built on: a recall step carries the
facts it returned, as chips with the Helpful / Outdated pair (`docs/assistant.md`,
"A turn's working").

Every memory row is self-describing: a capture, archived or superseded badge,
compact evidence and volatility chips, and a corroboration count. The defaults
(evidence `inferred`, volatility `slow`, lifecycle `active`) render nothing, so
ordinary rows stay quiet.

The list shows only live recallable facts by default. Two toolbar toggles reveal
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
- **Corroboration without a human.** `MarkAutoHelped` and its 0.25 automatic
  weight have no producer now that the session-end judge is gone. Whether
  anything other than the operator should ever move trust is open.
- **Persisted cross-scope edges.** Deferred; tracked in `docs/tech-debt.md`.
</content>
