# Tech Debt

Maintained as a living document. Severity tiers describe what will break
or surprise someone first, not effort to fix.

Last full audit: 2026-06-23 (brain **self-balancing semantic graph** — embeddings as weighted kNN
edges, force layout, embedding-weighted forces/visuals + legibility pass, graph is the default brain
view; **semantic recall enabled in production** —
ChromaDB + Ollama all-minilm as durable docker containers; **all brain config now config.toml-
settable**). Prior: 2026-06-22 (brain **automatic outcome emitter** — session-end transcript judge
that auto-feeds MarkAutoHelped/Flag, gentler 0.25 auto weight, session-end model knobs in
config.toml; semantic recall — vector veto + vouch bar, model-specific auto-calibration, warmed +
pruned embed cache; scheduled-consolidation config-file support; consolidation vocabulary
unification — retired "Tidy"/"sleep"). Prior: 2026-06-21 (outcome signal v1: MemoryUsed +
confidence calibration + operating contract; cross-scope areas, pluggable semantic similarity,
fluid per-turn recall + corpus cache, recall precision).

## P0 — Will bite a user

(No open P0 items.)

## P1 — Surprising or limiting

### Brain: promoted-fact merge inputs are forward-only (backfill shipped, not yet run)
The review surface's headline feature — showing a cross-scope promotion as *inputs →
output* — depends on `Record.Subsumed`, snapshotted at apply time. It is **not
backfilled**: every fact promoted before the snapshot landed (incl. the current live
~25-fact confirm queue) has empty `Subsumed`, so it degrades to "originals not
retained" and the reviewer judges the generated summary without seeing its sources —
exactly the case the feature was built for.

The one-time migration is now shipped — `brain backfill-subsumed` (deterministic core
in `internal/memory/backfill.go`, CLI in `cmd/agentique/brain.go`) resolves each fact's
`DerivedFrom` ids against a `--source` snapshot of the deleted originals and fills
`Subsumed`; it is idempotent and global-only. **It has not been run against the live
brain** (awaiting that — see below).

**Source gotcha (verified 2026-06-18):** the obvious candidate
`brain-export-2026-06-17.json` is **useless as a source** — `brain export` stripped
record ids before this change, so it has no id to match `DerivedFrom` against (export
now writes `id`, but that doesn't help a bundle taken earlier, and the originals are
already deleted so it can't be re-exported). The originals survive id-keyed only in the
`brain.pre-tidyall-2026-06-17` markdown backup: it resolves **61/141** `DerivedFrom`
ids, making **25 facts fully recoverable** (matching the ~25-fact queue) plus some
partial. Run against that dir, not the JSON bundle. The command refuses an id-less
bundle with guidance. → `internal/memory/{record,promote,backfill}.go`,
`cmd/agentique/brain.go`.

### Brain: AI refine is a synchronous model call ~~uncancellable~~ (timeout shipped)
`HandleRefine` runs the model on a detached context (so a client disconnect can't
SIGTERM the subprocess) and **blocks the HTTP request** until it returns. It is now
**bounded by a 2-minute timeout** (`refineTimeout`): a wedged or long-rate-limited
call is cancelled and the handler returns `504 Gateway Timeout` instead of hanging the
request and the review dialog's spinner indefinitely (`RunWithRetry` honors ctx, so
the deadline unblocks an in-flight call or a retry backoff). Still synchronous and
inline (not on the job channel) and there's no user-facing cancel button — acceptable
for an interactive rewrite, but a remaining nicety. → `internal/brain/http.go`
(`HandleRefine`, `refine_timeout_test.go`), `internal/brain/extractor.go` (`Refine`).

### Brain: consolidation apply is not transactional
`ApplyPlan` / `ApplyGlobalPromotion` / `writePromoted` write facts one
`store.Put`/`Delete` at a time with no transaction. A crash or backend restart
mid-apply leaves a partially-consolidated scope. Self-healing (the plan's
fingerprint goes stale → next apply returns `ErrStalePlan` → re-preview) and not
corrupting, but a surprising in-between state. The async preview *job* is in-memory
only, so a restart mid-preview drops it (mitigated: the frontend re-hydrates on WS
reconnect and clears the stale spinner). → `internal/memory/{consolidate,promote}.go`,
`internal/brain/job.go`.

### Brain: `RelinkScope` overwrites `Related` (will clobber curated links)
Relink rebuilds the entire `Related` edge set each apply — correct while nothing
else writes the field, but the moment a curated/human `[[link]]` UI lands it will
silently erase those edges. Must tag auto vs. curated edges first (noted in-code).
`Record.Community` (P3) is a *separate* field, so it isn't affected by this — but it
shares the same "rebuilt each apply, will fight a curated source" shape.
→ `internal/memory/link.go`.

### Remaining delta events have no frontend renderers

- **Status (2026-05-27):** `tool_output_delta` and `tool_progress` are
  now wired through the streaming store and rendered on in-flight tool
  blocks (header shows last output line + elapsed time; expanded detail
  shows full streaming output). `reasoning_delta` events now accumulate
  in the streaming store and render as a live `ThinkingBlock`
  (auto-expanded with spinner) in the last agent section during
  streaming.
- **Remaining:** `TurnDiffEvent` is still classified as `"skip"` in
  `segments.ts` — it could power a turn-level diff view.

### Codex error classification is generic

- **Symptom:** all codex-originated errors get `errorType: "api_error"`
  in the frontend. No rate-limit retry-after, no auth-specific messaging.
- **Cause:** `wireErrorEvent()` in `wire.go` now switches on
  `runtime.ErrorKind` first (rate_limit, auth, billing, overloaded,
  permission, invalid_request, max_turns) and only falls back to claudecli
  sentinels when `Kind` is unset. The consumer side is ready — but the
  agentkit codex adapter emits `ErrorEvent` with **no `Kind` set**
  (`connector.go`), so codex errors still fall through to generic
  `api_error`. Codex rate limits arrive via a separate
  `RateLimitsUpdatedEvent`, not via `ErrorEvent`.
- **Fix path:** the only remaining work is on the adapter — have the
  agentkit codex adapter set `ErrorKind` on `ErrorEvent` (or add error
  sentinels to codexcli-go). No agentique-side change needed.

### Codex attachments path is half-baked

- **Frontend gate added (2026-05-25):** the paperclip button is hidden
  on codex sessions via `WireCapabilities.Attachments`. Paste / drag-drop
  paths still produce attachments; the backend `toRuntimeAttachments`
  call will still fail loudly on submit. Cheap follow-up: have
  `useAttachments` ignore drops when `attachmentsSupported === false`.
- **Real fix:** teach the codex adapter to write attachments to
  temp files and pass paths (the codex SDK accepts paths) so the gate
  becomes unnecessary.

## P2 — Smells / drift

### Brain: new signals are inert / headless on the live corpus
Several shipped features can't yet show value because their inputs don't exist in
practice:
- **Two-factor strength + strength-weighted decay (D1):** `RetrievalStrength` decays
  from `LastUsedAt`, which is only stamped by `MemorySearch`/per-turn recall injection
  (`BumpUses`) and the new `MemoryUsed` outcome tool — and `uses`/`helped` are `0` across
  the entire live corpus (recall injection is recent; agents rarely call the explicit
  tools). So retrieval ≈ storage and `DecayPolicy.StrengthWeighted` is a near-no-op until
  real recall/outcome traffic accrues. The mechanism is correct; it's starved of signal.
- **Outcome signal v1 (D2 positive half, 2026-06-21) — automatic emitter SHIPPED 2026-06-22.**
  `MemoryUsed` / `MarkHelped` / `Record.Helped` + confidence calibration shipped, but the explicit
  signal is **agent-volunteered** (`helped` was `0` everywhere on the live corpus). The durable fix —
  the **automatic emitter** (session-end LLM judge over the transcript) — is now built
  (`brain-outcome-signal.md` ADR addendum; `internal/brain/outcome.go`): on session delete it recovers
  the facts recall injected (from the persisted `<brain>` envelopes), judges helped/contradicted/neutral
  conservatively, and applies `MarkAutoHelped` (gentler `0.25` gap-close — a machine inference weighs
  half an explicit acknowledgement) / `Flag`. Opt-in via `AGENTIQUE_BRAIN_OUTCOME_MODEL` / `[brain]
  outcome-model`, off by default. Verified end-to-end with a live Haiku judge and against an isolated
  copy of the live brain. **Remaining (now narrower):** the emitter is dormant on the live server until
  the model is configured, and its precision/recall over *organic* (not authored) sessions — plus the
  `0.25` weight — want a live multi-turn soak to calibrate. The **operating contract** remains the
  already-non-inert piece (8 human-confirmed global prefs act today via `Confirm`→1.0).
- **Interference + due-for-review (D5/D6):** computed and served in `GET /graph`'s
  report (`interference`, `dueForReview`) but **rendered nowhere** — no frontend
  consumer. Backend-only features drift toward "we built it but no one sees it." The new
  `Helped` count is likewise served in the `memoryDTO` but has no brain-UI badge yet.
→ `internal/memory/{strength,interference,reconsolidate}.go`, `internal/brain/{graph,brain}.go`,
`frontend/src/components/brain/`.

### Brain: refine/edit leave stale provenance
Editing or AI-refining a promoted fact changes its `text` but leaves `Subsumed` /
`DerivedFrom` untouched, so the displayed "merged from N facts" provenance can describe
a statement the user has since rewritten. Harmless (provenance is informational) but
mildly misleading. Decide whether an edited fact keeps or sheds its merge provenance.
→ `internal/brain/brain.go` (`Update`), `MemoryReview` refine flow.

### Brain: scope leakage in existing memories
~9% of reviewbot's facts are about *other* projects it reviews (alltix/mobilix/
agentkit…), scoped to reviewbot. The tightened extract prompt prevents *new*
leakage but there's no cleanup of the existing ~40; global-consolidation can promote
genuinely cross-cutting ones but won't catch codebase-specific leaks. → data debt.

### Brain: three similarity passes + per-apply relink/cluster write cost
On each real apply the backend runs `RelinkScope` (O(n²) Jaccard + up to N markdown
writes) **and then** `AssignCommunities` (another O(n²) detect + up to N more writes),
while the graph view *also* recomputes Jaccard client-side for dashed edges. Three
passes over the same similarity signal; the two backend passes each list+write the
scope. Fine at current scale (dozens–low-thousands) but a clear merge target — both
could share one tokenize+adjacency build and one write per changed record.
→ `internal/memory/{link,community}.go`, `frontend/src/components/brain/BrainGraph.tsx`.

### Brain: large reorganize chunks intermittently crash the CLI
A structured-output reorganize of a ~45-fact chunk sometimes crashes the `claude`
subprocess (`claudecli: exit 1`, **no** result events — not `error_max_turns`,
which claudecli classifies as non-fatal). Mitigated, not fixed. Current mitigations:
per-chunk retry (`reorgMaxAttempts=4`, raised from 3) + resilient no-op (a chunk that
still fails keeps its facts unchanged instead of aborting the scope) + a smaller
aggressive batch (`aggressiveMaxReorgBatch=35` vs the conservative 50), since
aggressive runs on exactly the bloated, long-fact scopes that crash most.
**Instrumentation added (2026-06-17):** the retry and give-up logs now capture
`claudecli.Error.{ExitCode,Stderr,LastEvents}` via `cliErrorFields` — `LastEvents`
holds the last raw stdout JSONL lines before the exit, the one handle on this
otherwise-silent death (`WithStderrCallback` produced nothing). Next step: read those
`lastEvents` from a live crash to root-cause whether it's an output-budget wall (→
shrink the batch by token estimate, not fact count) or a CLI bug (→ upstream).
→ `internal/brain/extractor.go`.

### Brain: tunables are hardcoded constants
`maxReorgBatch=50`, `aggressiveMaxReorgBatch=35`, `reorgMaxAttempts=4`,
`maxPromoteBatch=120`, `maxParallelBatches`/`maxParallelReorg=4`, `maxRelatedDegree=6`,
`DefaultRelatedThreshold=0.3`, `DefaultCommunityThreshold=0.15`,
`AggressiveMinSurvivorRatio=0.2` / `defaultMinSurvivorRatio=0.5`, the P2 confidence
scores (`DefaultInferredScore=0.8`, `CrossProjectInferredScore=0.65`,
`AmbiguousScoreThreshold=0.55`) and report caps (`maxGodNodes`/`maxBridges=8`,
`maxNeedsConfirmation=25`), recall fan-out (`assocPerSeed=3`, total ≤K), the recall
precision guard (`singleTokenMinShare=0.40`), and the outcome-signal constants
(`CorroborationCeiling=0.95`, `corroborationGapClose=0.5`, `helpedUseWeight=2`,
`ActOnConfidence=0.85`) — no flags/config to tune per deployment or scope size. The
recall + outcome constants in particular are calibration choices made on a small data
sample (one real mis-recall, the live pref distribution) and want revisiting once there
is real `MemoryUsed`/recall traffic to measure against.

### Brain: durable job queue is at-least-once (outcome double-count on replay)
The session-end learn/outcome passes now run through a durable retry queue (`brain/jobqueue.go`,
`brain_jobs` table, M7) so a crash mid-extraction no longer loses the work — drained on startup,
retried, then dead-lettered. Delivery is **at-least-once**: the row is deleted only after a
successful handler, so a crash *after* the LLM pass mutated memory but *before* `DeleteBrainJob`
replays the job on restart. `LearnFromTranscript` is self-healing under replay (`Add` dedups + M4
reinforces), but `ApplyOutcomesFromTranscript` is **not** idempotent — a replay can re-increment
`Helped`. This is an accepted bound for Band 1; the follow-up is a per-`(scope, fact-id)` applied
marker (exactly-once outcomes). Also: `DeleteSession` holds `endEvents` in memory and enqueues
after the row delete, so a `kill -9` in that gap loses one transcript (pre-existing, smaller than
the bug M7 fixes; `Enqueue` is a single fast insert to minimise it).
→ `internal/brain/jobqueue.go`, `internal/server/server.go`.

### Brain: single consolidation job slot
Only one consolidation runs at a time (`beginJob` 409s a second); "Consolidate all" is
sequential and two scopes can't consolidate concurrently. Parallel-across-scopes was
deferred — needs a multi-job map + frontend tracking multiple previews.
→ `internal/brain/job.go`, `frontend/src/stores/brain-store.ts`.

### Brain: config-file coverage ~~is partial~~ → COMPLETE 2026-06-22; one env hard-rename, one naming seam
**Resolved (2026-06-22).** Every brain knob is now settable in `config.toml` under `[brain]`, with
the matching `AGENTIQUE_BRAIN_*` env var still winning when set: `chroma-url`, `embed-url`,
`embed-model`, `embed-key`, `semantic-threshold`, `vector-veto`, `autocal`, and `recall` joined the
already-covered `consolidate-interval`/`consolidate-model`/`learn-model`/`outcome-model`. Strings
layer via `firstNonEmpty`, floats via `envFloatOr`, bools via `envBoolOr`; the default-ON `recall`
toggle uses `resolveRecall` (env → file → on). Matches the user's config-over-env preference
(`feedback_config_over_env`) for a persistent service. → `internal/config/config.go`,
`cmd/agentique/serve.go`, `internal/server/server.go`.

Two smaller residues remain from the 2026-06-22 vocabulary unification:
(a) the env rename `AGENTIQUE_BRAIN_SLEEP_*` → `AGENTIQUE_BRAIN_CONSOLIDATE_*` is a **hard
rename with no alias** — an operator with the old var set silently loses scheduled
consolidation (acceptable: opt-in, barely deployed); (b) the cross-scope op is named three ways
across layers — UI "Lift to global", API path `/api/brain/consolidate/global`, backend
"promotion" (`promote.go`/`PlanGlobal`). Per-scope consolidation was unified; this seam wasn't.
→ `internal/config/config.go`, `cmd/agentique/serve.go`, `internal/server/server.go`,
`frontend/src/components/brain/BrainPage.tsx`.

### Brain: `brain.Handler` is a grab-bag
One type owns memory CRUD + search + status + consolidation preview/apply + global
+ consolidate-all + the job runner. Growing; a split (CRUD vs. consolidation/jobs) would
help. → `internal/brain/{http,job}.go`.

### Brain: semantic similarity is activated only for areas (C) ~~partial~~ → CLOSED 2026-06-22
**Resolved** (`docs/brain-semantic-recall.md` #2). `ConsolidateOptions.SimOptions` now
threads the embedding-blended `memory.Similarity` through `memory.ApplyPlan` into the
post-apply `RelinkScope` + `AssignCommunities`, so per-scope **links and communities are
semantic** in semantic mode (`brain.ApplyPlan`/`Consolidate` compute the SimOptions —
`scopeSimOptions`, embed the scope — *before* taking `s.mu`, so the network embed never
blocks under the lock; skipped on dry run). `DetectInterference` takes `SimOptions` and
blends cosine into its related-lower bound (duplicate-exclusion stays lexical — consolidation
owns dups), threaded from the graph endpoint (request-time, not the per-turn hot path).
`ApplyGlobal` now refreshes areas via the semantic `s.AssignAreas` instead of the bare
lexical `memory.AssignAreas`. Remaining nuance: the graph-endpoint and `ApplyGlobal` embeds
run inline (acceptable — neither is the hot path), and the per-pass re-embed has no cache
(separate item below). → `internal/memory/{consolidate,similarity,interference}.go`,
`internal/brain/{brain,graph}.go`. Tests: `TestApplyPlanThreadsSemanticSimOptionsToRelink`,
`TestDetectInterferenceUsesEmbeddings`.

### Brain: embeddings re-embed the whole corpus every pass ~~(no cache)~~ → cache + cold-start warm + pruning SHIPPED 2026-06-22 → CLOSED
`Service.embedRecords` memoizes vectors in an in-process **text-hash cache**
(`embedCache`, sha256 of text; embedding is pure in (text, per-Service-fixed model), so
text-hash is a sufficient id-independent key) and embeds only DISTINCT miss texts. After the
first pass an unchanged corpus costs zero embed calls — which matters now that #2 widened the
call sites (per-`ApplyPlan`/`Consolidate` scope embed + per-graph-load embed, on top of
`AssignAreas`). The per-turn *recall* path was never affected (single query-embed + search).
**Cold start closed (the Chroma bulk-vector read):** on the first clustering embed after a
restart, `warmEmbedCache` seeds the cache from the vectors Chroma already holds —
`chroma.Client.GetEmbeddings` (a `/get` with `include:["embeddings","documents"]`) →
`chroma.Store.LoadVectors`, keyed by `embedKey(document)` so an unchanged fact resolves without
re-embedding. It runs at most once per process (`warmed`/`warmMu`), serializes concurrent first
passes, and retries on a transient failure (best-effort; a cold cache just falls back to
embedding). **Pruning closed:** `pruneEmbedCache` runs at the whole-brain checkpoint
(`AssignAreas`, after every scheduled-consolidation/consolidate-all/global pass) and drops entries whose text-hash is no
longer in the live corpus, so edited/deleted facts' stale vectors don't accumulate — the cache
is bounded by the live fact set, not by every text ever seen. → `internal/brain/brain.go`,
`internal/memory/chroma/{client,store}.go`. Tests: `TestEmbedRecordsCachesByTextHash`,
`TestWarmEmbedCacheZeroReembedAfterRestart`, `TestWarmEmbedCacheEmbedsOnlyNewFacts`,
`TestWarmEmbedCacheRetriesAfterFailure`, `TestPruneEmbedCacheDropsStaleTexts`, and the live
`TestWarmEmbedCacheLiveZeroReembedAfterRestart` (env-gated).

### Brain: semantic graph is a per-request O(n²) kNN ~~with no caching / fixed knobs~~ → CLOSED 2026-06-23
The graph layout (P6, `docs/brain-graph-layer.md`) was reworked from a PCA *projection* (retired —
positions collapsed 384-dim similarity to 2 axes) to **semantic edges + a self-balancing force layout**:
`memory.SemanticEdges` computes the embedding kNN graph (cosine ≥ threshold, per-node cap) with cosine
scores that weight the layout forces. All three open items are now closed: (a) **caching** —
`Service.SemanticEdges` memoizes by a corpus fingerprint (record ids + text-hashes plus the resolved
threshold/cap), so a repeated load over an unchanged corpus skips the re-embed + O(n²·d) kNN entirely;
the map is bounded (cleared past `semEdgeCacheMax`) and a fingerprint change can never serve a stale
result. (b)+(c) **configurable** — the per-node cap, the cosine edge threshold (0 ⇒ the recall
`cosThresh`), and the force-layout curves (link strength/distance base+span, gravity) are now
`[brain.graph]` config with `AGENTIQUE_BRAIN_GRAPH_*` env overrides; the force curves are threaded to
the frontend on the graph payload (`graphDTO.tuning`, falling back to `LAYOUT_DEFAULTS`). →
`internal/memory/semantic_edges.go`, `internal/brain/{brain,graph}.go`, `internal/config/config.go`,
`backend/cmd/agentique/serve.go`, `frontend/src/components/brain/BrainGraph.tsx`. Tests:
`TestSemanticEdgesCachedByFingerprint`, `TestSemanticEdgesFingerprintTracksKnobs`.

### Brain: semantic infra is operator-run docker, not managed by agentique
Semantic recall is now **live in production** (ChromaDB + Ollama all-minilm), but the two services are
**hand-run docker containers** (`chroma`, `ollama`), not provisioned or health-managed by agentique.
Both now carry `--restart unless-stopped` (a gap found 2026-06-23: the pre-existing `chroma` container
had `--restart no`, so after it exited agentique silently fell back to keyword recall until manually
restarted — recall.go degrades cleanly, so no breakage, just lost semantics). Remaining: no
agentique-side health surfacing (an operator can't see "semantic is configured but Chroma is down"
except in logs), the Ollama model lives in a docker volume (durable) but the stack is a manual
`docker run`, and there's no compose/systemd unit checked in. → ops/runbook gap; see
`docs/brain-semantic-recall.md` runbook. Candidate: a `GET /api/brain/status` field for embedder/Chroma
reachability + a docker-compose in the repo.

### Brain: cross-scope area labels ~~are frequency-based (noisy)~~ → TF-IDF SHIPPED 2026-06-23
`areaLabel` now scores tokens by in-area document frequency × inverse document frequency across the
durable corpus (`corpusIDF`, smoothed `1 + ln((1+N)/(1+df))` so a corpus-ubiquitous token bottoms out
at idf 1 rather than being zeroed/dropped), so generic glue ("go", "user") is down-weighted in favour
of the tokens that actually distinguish an area — replacing raw-frequency labels like "go agentkit
codex". Deterministic (ties broken by idf, then alphabetically), so no LLM naming pass was needed. →
`internal/memory/areas.go`. Test: `TestAssignAreasLabelDownweightsCorpusCommonTokens`.

### Brain: cosine threshold is model-specific and hand-tuned ~~(no auto-calibration)~~ → auto-calibration SHIPPED 2026-06-22
The 3 coupled knobs are still model-specific, but no longer have to be hand-tuned: an opt-in
**auto-calibration** pass derives them from the corpus's OWN pairwise cosine distribution
(`docs/brain-semantic-recall.md` #5). `memory.Calibrate` (`internal/memory/calibrate.go`, pure)
samples the pairwise cosines and reads the cosine **related line off a high percentile (p99) and
the veto floor off a low one (p25)**; `brain.New` opts in via `AGENTIQUE_BRAIN_AUTOCAL=1`, embeds
the live corpus (through the text-hash cache) and overrides only the knobs the operator didn't pin
(explicit `AGENTIQUE_BRAIN_SEMANTIC_THRESHOLD`/`_VECTOR_VETO` still win). `agentique brain calibrate`
prints the distribution + derived thresholds without booting.

Measured live on the real 1509-fact brain (all-MiniLM, 227k pairs): p99 = 0.4187 → cosineThreshold,
p25 = 0.0455 → veto. The auto-derived related line (0.42) stays above the off-topic survivor (~0.36)
so the github mis-recall stays excluded (`TestBrainAutoCalibrateExcludesGithub`). Notably the hand
veto 0.15 sits at ~p63 of the real corpus — tuned on the 5-fact example, over-aggressive on a broad
brain; auto-cal corrects it downward, safe because the vouch bar (not the veto) carries the github
exclusion. The hand defaults remain the fallback (thin corpus / no embedder / embed failure).
**Residual:** percentile picks (p99/p25) and `MaxPairs` are themselves constants (sensible, grounded
in the live measurement, but not yet per-deployment configurable); calibration is a boot-time
snapshot, not refreshed as the corpus grows. → `internal/memory/{calibrate,similarity,recall}.go`,
`internal/brain/brain.go`, `cmd/agentique/brain.go`. Tests: `internal/memory/calibrate_test.go`,
`TestBrainAutoCalibrateExcludesGithub`.

### Brain: persisted cross-scope edges deferred (the "B4" decision)
The planned `RelinkScope` curated-edge tagging + persisted cross-scope `Related` edges was
**deferred**: areas-as-a-field (`Record.Area`) delivered the cross-scope value (grouping,
viz, sibling-scope recall) without them. Cost: cross-scope centrality (god-nodes/bridges
*spanning* projects) isn't computed, and the future curated-`[[link]]` UI still needs the
auto-vs-curated tagging the P1 "`RelinkScope` overwrites `Related`" item flags. → data/UX
gap, not a bug. → `internal/memory/link.go`.

### Brain: read-through cache staleness + shared-slice contract
`memory/cachestore` invalidates only on its own `Put`/`Delete`. A `brain` CLI run against
the same dir while the server is up won't invalidate the server's in-memory cache (rare;
clears on the next server write) — no TTL backstop. Also `List`/`Get` return records that
share slice backing with the cache: safe under the current replace-field-then-`Put` write
pattern, but a future in-place mutation of `Related`/`Embedding` would corrupt the cache
(documented in-code, not enforced). → `internal/memory/cachestore/cachestore.go`.

### Brain: lexical recall precision ~~is a blunt mitigation~~ → semantic cure SHIPPED 2026-06-22
The keyword-only lone-token guard (`singleTokenMinShare`) remains as the `semantic=false`
safeguard, but the **semantic cure** is now built and verified end-to-end against a live
all-MiniLM + Chroma (`docs/brain-semantic-recall.md` #1+#3):
- **Vector veto** (`Query.VectorVetoScore`/`DefaultVectorVetoScore`): a candidate the
  embedder scores as actively unrelated is dropped regardless of keyword — kills the
  MULTI-token off-topic survivor the lexical guard can't (`kwMatches>1`).
- **Vouch bar** (`Query.VectorVouchScore` = cosThresh): the lexical lone-token guard is now
  overridden *only* when the vector genuinely vouches (vs ≥ the cosine related line), not at
  the old `minVectorScore` 0.20 — which was far too low for a compressed-distribution model
  and was the actual reason the github fact leaked the hybrid path (it scored ~0.36, cleared
  0.20, skipped the guard). This is the lever that excludes the real mis-recall.
Live proof: recall of "secrets and vars on github" over {GOPRIVATE, github-actions, Sentry,
…} returns **only the Sentry fact** under shipped defaults (veto 0.15 / vouch 0.45).
Residual: thresholds are model-specific + hand-calibrated (item above); the per-pass embed
has no cache (item above). → `internal/memory/recall.go`,
tests `TestRecallVectorVetoes…`, `TestRecallVouchBarDropsMidScoreLoneToken`,
`internal/memory/chroma/semantic_recall_integration_test.go`.

### Brain: per-turn recall injection is cumulatively unbounded
Fluid recall bounds each turn (≤K, delta-deduped against a per-session seen-set), but the
seen-set only suppresses repeats — across a long, topic-drifting session the *total*
injected (and the `BumpUses` churn) grows unbounded. Low risk (K small, low-content gate),
but a per-session injection budget would cap it. → `internal/session/session.go`
(`injectRecall`), `internal/brain/brain.go` (`RecallBlock`).

### `claudecli` still imported in session-package files for narrow reasons

The migration intentionally keeps a few `claudecli` imports under
`backend/internal/session/`:

- `session.go` — `claudeSession()` type-assert for MCP reconnect.
- `wire.go` — `errorDetail` + `wireErrorEvent`'s `errors.Is` chain for
  claudecli error sentinels and `RateLimitError.RetryAfter` extraction.
  Also `ErrContextWindowExceeded` (added 2026-05-27).
- `channel.go` — `claudecli.FormatAgentMessage` free helper.
- `cli.go` — `BlockingRunner` for autotitle; deliberately not behind
  the runtime.
- `msggen/msggen.go` — one-shot Haiku invocation, claude-only.

Each one is a small abstraction leak. None block correctness today, but
they constrain future providers.

### `WireResultEvent.Usage` typed as `any`

`WireResultEvent.Usage` is typed `any` in `wire.go` — populated from
`runtime.TokenUsage` but the frontend reads through a permissive shape.
Should be a concrete struct.

### `context.Background()` in async session operations

~50 call-sites across `backend/internal/session/` use
`context.Background()` instead of deriving from a parent context. Most
are fire-and-forget DB writes where cancellation semantics don't matter.
But several are in `channel.go` goroutines (e.g. `injectChannelContext`,
`executeSpawn`, `DissolveChannel`) that run user-visible work — if the
parent session is force-closed, these goroutines will keep running until
they finish or hit a network timeout. Low blast radius today (they're
all short-lived), but will need attention if channel operations grow
long-running (e.g. multi-worktree operations).

### Non-deferred mutex pattern in `state.go`

`setState` and `UnlockGitOp` in `session/state.go` manually call
`s.mu.Unlock()` at multiple return points instead of using `defer`.
This is intentional — the code releases the lock before broadcasting
to avoid holding it during channel sends. But the pattern is fragile:
any future code added between `Lock()` and `Unlock()` that panics will
deadlock the session. A safer approach would be to split each method
into a locked inner function (returning the new state) and an unlocked
outer function (doing the broadcast).

### Raw SQL in backup module

`backend/internal/backup/backup_metadata.go` contains a raw SQL query
(`SELECT COUNT(*) FROM projects, sessions, session_events`) outside of
the sqlc-managed `db/queries/` directory. It's read-only metadata for
the backup header, so correctness risk is low. If the schema changes
(table renames), this query will break silently at runtime instead of at
`just sqlc` generation time.

## P3 — Dependency hygiene

### Brain: the orchestration layer is untested
The deterministic cores are well covered (Plan/Apply, promote, relink, associative
recall, extractor parsing, and — new — `MarkHelped`/`MarkHelpedWith`/`OperatingContract`/the recall
lone-token guard, the `MemoryUsed` adapter scope check, and — 2026-06-22 — the automatic outcome
emitter: `ApplyOutcomesFromTranscript` with a fake judge, `<brain>`-envelope id extraction, the
scope guard, anti-hallucination, the gentler auto weight, and judge parsing, plus a live env-gated
`TestOutcomeEmitterLive`). Untested: the async job
runners (`runScopeJob`/`runGlobalJob`/`runConsolidateAllJob`), the `server.go` automation wiring
(auto-recall preamble, auto-encode + auto-outcome on delete, scheduled consolidation), and the CLI
`export`/`import` interactive resolution — they need a live runner / DB / stdin. **New
gaps from the outcome-signal work:** `MemoryUsed` over the real `/mcp` HTTP transport
(token minted per-session → needs a model-backed session) and the operating-contract
preamble wiring (`MemoryContractFn` → `Manager.memoryContract` → the three
create/resume/reconnect assembly sites) have no end-to-end test — same "needs a live
runner" shape. → `internal/brain/job.go`, `internal/session/manager.go`,
`internal/server/server.go`, `cmd/agentique/brain.go`.

### Brain: `react-force-graph-2d` added, loosely typed
The graph view pulled in `react-force-graph-2d` (canvas force-graph). It wasn't
installed in this worktree post-merge (`just check` failed until `npm install`), and
`BrainGraph.tsx`'s render callbacks lean on the lib's loose types.
→ `frontend/package.json`, `frontend/src/components/brain/BrainGraph.tsx`.

### All provider dependencies are pseudo-versioned

`github.com/allbin/{agentkit, claudecli-go, codexcli-go}` are all pinned to
untagged `v0.0.0-<timestamp>-<hash>` pseudo-versions (see `go.mod` for the
current commits). If we depend on a fix landing upstream, we'll need to
either tag releases or keep bumping pseudo-versions. codexcli-go README
explicitly warns the SDK is "early"; expect breaking changes.

### codexcli-go schema is hand-rolled despite JSON Schema availability

Codex CLI publishes a full JSON Schema Draft 7 via
`codex app-server generate-json-schema`. codexcli-go has the raw schemas
in `schema/v2_raw/` (~18k lines) and a `cmd/genschema` tool, but Go
types in `schema/types.go` are still hand-written. claudecli-go has no
upstream schema at all (the Claude CLI wire format is undocumented).

### Skipped tests as silent debt

A handful of tests are `t.Skip`-ed across `cmd/agentique/setup_test.go`,
`internal/{ws,filebrowser,session}/*_test.go`, and
`internal/memory/chroma/store_integration_test.go`. They split into two
benign buckets: integration tests gated on `-short` mode or a live
external service (Claude CLI, ChromaDB), and setup self-tests that skip
when no health checks are registered. All are structural placeholders or
environment gates, not masked gaps.

### Release workflow builds but does not test

`release.yml` compiles the binary but runs zero tests before publishing
it. A tagged release with a broken test suite will still produce a
GitHub release with downloadable artifacts. This is downstream of the
missing CI pipeline — once a `ci.yml` exists, the release workflow
should either depend on it or replicate its checks.

### No `.env.example` file

The README now carries a backend env-var table (`AGENTIQUE_HOME`,
`AGENTIQUE_DB`, `XDG_*`, `LOG_LEVEL`/`JSON_LOG`, the `AGENTIQUE_BRAIN_*`
set), but there's still no checked-in `.env.example`, and the frontend dev
vars (`VITE_TLS`, `VITE_MSW`, `VITE_BACKEND_PORT`, `VITE_PORT`,
`VITE_PUBLIC_HOST`, `VITE_MSW_STRICT`) remain documented only in
`justfile` / `vite.config.ts`. A single `.env.example` would still reduce
onboarding friction.

### `mcphttp.register` panics on programmer error

`backend/internal/mcphttp/setup.go:170` panics if an MCP tool
registration fails (duplicate name, bad schema). This is intentional —
it catches programmer errors at startup before any sessions are created.
But it's an unrecovered panic in production code. If tool registration
ever becomes dynamic (user-supplied MCP configs), this needs to become
an error return.

### Brain: refine + review-surface coverage gaps
`unwrapRefineText` is unit-tested for the JSON shapes seen in the wild, but
`HandleRefine` (model wiring, scope/model validation, the detached-context path) has
no end-to-end test — it extends the existing "orchestration layer is untested" gap.
`MemoryReview` has component tests for full-text display, the inputs→output framing,
and refine-via-chip, but the error path, edit→save, delete, and skip aren't covered.
→ `internal/brain/{http,extractor}.go`, `frontend/src/components/brain/__tests__/`.

### Brain: areas / semantic / fluid-recall on a live server ~~not verified~~ → semantic NOW LIVE 2026-06-23
**Semantic recall is enabled in production** (2026-06-23): ChromaDB + Ollama all-minilm wired via
`[brain] chroma-url`/`embed-url`/`embed-model`, boot logs `semantic=true cosineThreshold=0.45
vectorVeto=0.15`, and the graph endpoint projects the live ~1450-fact corpus's embeddings. So the
"live is keyword-only/`semantic=false`" caveat is closed, and semantic clustering with a real
embedder is exercised on the real brain. **Still open**: a multi-turn live *soak* measuring fluid
recall on real topic drift and semantic recall *quality* on the live corpus (the thresholds were
calibrated offline; autocal is available but not enabled — the hand defaults are running);
`brain assign-areas` applied to the live brain (only run on a copy; `backfill-subsumed` was only
`--dry-run` against live). → verification gap narrowed, not a known bug.

**Outcome signal v1 (2026-06-21) — partially closed.** Verified on an isolated copy of the
live brain (server boot with `AGENTIQUE_HOME`/`AGENTIQUE_DB` redirected to temp copies):
`OperatingContract` produces a correct, non-empty contract for 16/16 scopes; `MarkHelped`
calibration follows the gap-closing curve (0.875→0.9125→0.9312) under the 0.95 ceiling; the
`helped` field serializes over `GET /api/brain/memories`; the server logs the operating-
contract preamble wiring as active. **Still not exercised:** the `MemoryUsed` tool over the
real `/mcp` HTTP transport by a model-backed agent (the token is minted per-session at
session creation, which needs a live `claude` run), and whether agents actually *call*
`MemoryUsed`/`MemoryFlag` mid-task often enough to move the corpus. Needs a live multi-turn
session to close. → verification gap, not a known bug.

### Brain: scopeColor is a 10-entry hash (collisions possible)
`~/lib/scope-color.ts` hashes a scope into a 10-colour palette, so two projects can
share a colour in the graph and the review surface. Cosmetic, but the colour is sold as
"which project" info-scent. Fine at current project counts; revisit if it misleads.

## Resolved

Condensed log — `git log -- docs/tech-debt.md` and the referenced commits
hold the full detail.

- **2026-06-18** — Brain review surface: force-graph re-layout jump on every
  `brain.updated` fixed (position carry-forward + reheat-on-topology-change +
  fit-once); applied preview no longer re-hydrates (apply clears the held job);
  AI-refine raw-JSON leak fixed (`unwrapRefineText` peels schema-echo).
- **2026-06-17** — `capturingConnector.hintNext` routing race closed by a
  dedicated `routeMu` serializing the hint→Connect→pop handshake.
- **2026-05-27** — Codex resume is a real resume (`Conn.ResumeThread`,
  `caps.Resume = true`); `Service.resumeSession` codex workaround removed.
- **2026-05-27** — Claude partial-message streaming + `SendMessage`
  delivery confirmation ON (`server.go` plumbs `WithIncludePartialMessages`
  / `WithReplayUserMessages`).
- **2026-05-27** — `tool_output_delta` / `tool_progress` rendered via the
  streaming store (`reasoning_delta` / `turn_diff` still open, see P1).
- **2026-05-27** — `AgentResult` metadata flows end-to-end
  (`runtime.AgentResultEvent` → `WireAgentResultEvent`, persisted).
- **2026-05-27** — CI pipeline (`ci.yml`): backend, frontend, and
  typegen-freshness jobs on PRs + pushes to master.
- **2026-05-25** — Codex capability flags surfaced in UI
  (`WireCapabilities`), provider picker in New Session composer.
