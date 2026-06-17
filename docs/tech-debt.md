# Tech Debt

Maintained as a living document. Severity tiers describe what will break
or surprise someone first, not effort to fix.

Last full audit: 2026-06-17.

## P0 — Will bite a user

(No open P0 items.)

## P1 — Surprising or limiting

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

## Resolved

Condensed log — `git log -- docs/tech-debt.md` and the referenced commits
hold the full detail.

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
