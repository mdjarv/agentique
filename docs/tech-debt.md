# Tech Debt

Maintained as a living document. Severity tiers describe what will break
or surprise someone first, not effort to fix.

Last full audit: 2026-06-18 (RFC-LD D1/D2/D5/D6 + the review surface).

## P0 — Will bite a user

(No open P0 items.)

## P1 — Surprising or limiting

### Brain: promoted-fact merge inputs are forward-only
The review surface's headline feature — showing a cross-scope promotion as *inputs →
output* — depends on `Record.Subsumed`, snapshotted at apply time. It is **not
backfilled**: every fact promoted before the snapshot landed (incl. the current live
~25-fact confirm queue) has empty `Subsumed`, so it degrades to "originals not
retained" and the reviewer judges the generated summary without seeing its sources —
exactly the case the feature was built for. A one-time backfill from the export bundle
(`brain-export-2026-06-17.json`, which still holds the deleted originals) was offered
but not run. Fix: a `brain` CLI migration that fills `Subsumed` from `DerivedFrom` ids
against a bundle. → `internal/memory/{record,promote}.go`, `cmd/agentique/brain.go`.

### Brain: AI refine is a synchronous, uncancellable model call
`HandleRefine` runs the model on `context.Background()` (so a client disconnect can't
SIGTERM the subprocess) but **blocks the HTTP request** until it returns, with no
server-side timeout and no cancel. A slow or hung model leaves the request — and the
review dialog's spinner — hanging indefinitely. The big passes avoid this by being
background jobs; refine traded that for inline UX. Add a bounded timeout (and ideally
surface cancel), or move it to the job channel. → `internal/brain/http.go`
(`HandleRefine`), `internal/brain/extractor.go` (`Refine`).

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
  from `LastUsedAt`, which is only stamped by `MemorySearch` (`BumpUses`) — and `uses`
  is `0` across the entire live corpus (recall is pull-based and agents rarely call the
  tool). So retrieval ≈ storage and `DecayPolicy.StrengthWeighted` is a no-op until real
  query-relevant recall traffic exists. The mechanism is correct; it's starved of signal.
- **Interference + due-for-review (D5/D6):** computed and served in `GET /graph`'s
  report (`interference`, `dueForReview`) but **rendered nowhere** — no frontend
  consumer. Backend-only features drift toward "we built it but no one sees it."
→ `internal/memory/{strength,interference}.go`, `internal/brain/graph.go`,
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
`maxNeedsConfirmation=25`), recall fan-out (`assocPerSeed=3`, total ≤K) — no
flags/config to tune per deployment or scope size.

### Brain: single consolidation job slot
Only one consolidation runs at a time (`beginJob` 409s a second); "Tidy all" is
sequential and two scopes can't tidy concurrently. Parallel-across-scopes was
deferred — needs a multi-job map + frontend tracking multiple previews.
→ `internal/brain/job.go`, `frontend/src/stores/brain-store.ts`.

### Brain: `brain.Handler` is a grab-bag
One type owns memory CRUD + search + status + consolidation preview/apply + global
+ tidy-all + the job runner. Growing; a split (CRUD vs. consolidation/jobs) would
help. → `internal/brain/{http,job}.go`.

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
recall, extractor parsing). Untested: the async job runners
(`runScopeJob`/`runGlobalJob`/`runTidyAllJob`), the `server.go` automation wiring
(auto-recall preamble, auto-encode on delete, scheduled sleep), and the CLI
`export`/`import` interactive resolution — they need a live runner / DB / stdin.
→ `internal/brain/job.go`, `internal/server/server.go`, `cmd/agentique/brain.go`.

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
