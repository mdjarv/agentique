# Tech Debt

Maintained as a living document. Severity tiers describe what will break
or surprise someone first, not effort to fix.

Last full audit: 2026-05-27.

## P0 — Will bite a user

(No open P0 items.)

## P1 — Surprising or limiting

### Remaining delta events have no frontend renderers

- **Status (2026-05-27):** `tool_output_delta` and `tool_progress` are
  now wired through the streaming store and rendered on in-flight tool
  blocks (header shows last output line + elapsed time; expanded detail
  shows full streaming output).
- **Remaining:** `ReasoningDeltaEvent` and `TurnDiffEvent` are still
  classified as `"skip"` in `segments.ts` — no React components render
  them. Reasoning deltas could accumulate and display alongside thinking
  blocks. Turn diffs could power a turn-level diff view.

### Codex error classification is generic

- **Symptom:** all codex-originated errors get `errorType: "api_error"`
  in the frontend. No rate-limit retry-after, no auth-specific messaging.
- **Cause:** `wireErrorEvent()` in `wire.go` falls back to claudecli
  error sentinels when `runtime.ErrorKind` is unset. codexcli-go has no
  comparable sentinel errors, so every codex error hits the `default`
  branch. Now that codexcli-go emits `RateLimitsUpdatedEvent`, the runtime
  `ErrorKind` enum should cover rate limits, but other error types
  (auth, billing) still fall through.
- **Fix path:** either add error sentinels to codexcli-go, or ensure
  the agentkit codex adapter always sets `ErrorKind` on `ErrorEvent`.

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

### `capturingConnector.hintNext` is racy under concurrent Create

The current routing scheme sets a per-Connect "next provider" hint right
before calling `m.rt.Create` / `Resume` and resets it on the connector
side. If two `Manager.Create` calls land at the same instant, the wrong
adapter could be picked. In practice agentique's `Manager` mutex
sequences these, but the contract is fragile.

### ~~Frontend types: `agent_result` events are dead code~~

- **Resolved (2026-05-27):** `runtime.AgentResultEvent` added to
  agentkit; claude adapter emits it from `mapUserEvent`. Agentique's
  `ToWireEvent` maps it to `WireAgentResultEvent`. The pipeline now
  persists and broadcasts `agent_result` events.

### `WireResultEvent.Usage` typed as `any`

`WireResultEvent.Usage` is typed `any` in `wire.go` — populated from
`runtime.TokenUsage` but the frontend reads through a permissive shape.
Should be a concrete struct.

### `context.Background()` in async session operations

49 call-sites across `backend/internal/session/` use
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

- `github.com/allbin/agentkit v0.0.0-20260527065524-6104454df451`
- `github.com/allbin/claudecli-go v0.0.0-20260526133153-078bd7705f3b`
- `github.com/allbin/codexcli-go v0.0.0-20260526133513-9ffb447bd3d5`

None are tagged. If we depend on a fix landing upstream, we'll need to
either tag releases or keep bumping pseudo-versions. codexcli-go README
explicitly warns the SDK is "early"; expect breaking changes.

### codexcli-go schema is hand-rolled despite JSON Schema availability

Codex CLI publishes a full JSON Schema Draft 7 via
`codex app-server generate-json-schema`. codexcli-go has the raw schemas
in `schema/v2_raw/` (~18k lines) and a `cmd/genschema` tool, but Go
types in `schema/types.go` are still hand-written. claudecli-go has no
upstream schema at all (the Claude CLI wire format is undocumented).

### Skipped tests as silent debt

Three tests are `t.Skip`-ed:

- `handler_test.go:253` — skips Claude CLI integration test in `-short`
  mode (expected).
- `setup_test.go:539,576` — "no checks registered" — setup
  self-tests that skip because no health checks are wired yet.

All remaining skips are structural placeholders, not masked gaps.

### ~~No CI guard for typegen freshness~~

- **Resolved (2026-05-27):** `ci.yml` includes a `typegen-freshness`
  job that regenerates types and checks for drift via `git diff
  --exit-code`.

### Release workflow builds but does not test

`release.yml` compiles the binary but runs zero tests before publishing
it. A tagged release with a broken test suite will still produce a
GitHub release with downloadable artifacts. This is downstream of the
missing CI pipeline — once a `ci.yml` exists, the release workflow
should either depend on it or replicate its checks.

### No `.env.example` or environment variable inventory

Environment variables are documented across the README, `justfile`,
`vite.config.ts`, and `main.go` CLI flags — there's no single canonical
list. Backend: `AGENTIQUE_DB`, `AGENTIQUE_TLS_HOST`, `AGENTIQUE_HOME`,
`XDG_DATA_HOME`. Frontend dev: `VITE_TLS`, `VITE_MSW`,
`VITE_BACKEND_PORT`, `VITE_PORT`, `VITE_PUBLIC_HOST`,
`VITE_MSW_STRICT`. A `.env.example` would reduce onboarding friction.

### `mcphttp.register` panics on programmer error

`backend/internal/mcphttp/setup.go:170` panics if an MCP tool
registration fails (duplicate name, bad schema). This is intentional —
it catches programmer errors at startup before any sessions are created.
But it's an unrecovered panic in production code. If tool registration
ever becomes dynamic (user-supplied MCP configs), this needs to become
an error return.

## Resolved (kept for audit trail)

### ~~Codex resume is a fresh-start, not a real resume~~

- **Resolved (2026-05-27):** codexcli-go exposes `Conn.ResumeThread`,
  agentkit codex adapter wires it via `ConnectParams.ProviderSessionID`,
  `caps.Resume` is `true`, and the `service.go` codex workaround was
  removed. Codex sessions resume with conversation history.

### ~~Codex feature flags not surfaced in UI~~

- **Resolved (2026-05-25):** `WireCapabilities` on `SessionInfo`, chat
  UI gates features on capability flags, provider picker in New Session.

### ~~Frontend has no provider picker~~

- **Resolved (2026-05-25):** `MessageComposer` provider dropdown.

### ~~Service.resumeSession codex workaround~~

- **Resolved (2026-05-27):** removed the `dbSess.Provider == "codex"`
  check that force-set `freshStart = true`. The `freshStart` flag now
  only depends on whether a provider session ID exists, which is
  provider-agnostic.

### ~~Claude partial-message streaming is OFF~~

- **Resolved (2026-05-27):** upstream `claude.NewConnector` now accepts
  variadic `claudecli.Option` defaults. `server.go` passes
  `WithIncludePartialMessages()` and `WithReplayUserMessages()`.
  Assistant text streams in real time; `SendMessage` delivery
  confirmation works.

### ~~`SendMessage` delivery confirmation is OFF~~

- **Resolved (2026-05-27):** same upstream change as partial-message
  streaming — `WithReplayUserMessages()` is now plumbed through.

### ~~Delta events have no frontend renderers~~

- **Resolved (2026-05-27):** `tool_output_delta` and `tool_progress`
  are wired through `streaming-store` and rendered on `InFlightToolContent`
  (header) and `ToolUseBlock` (expanded detail). `reasoning_delta` and
  `turn_diff` remain unrendered (tracked as P1).

### ~~`AgentResult` metadata is dropped~~

- **Resolved (2026-05-27):** added `runtime.AgentResultEvent` to
  agentkit. The claude adapter's `mapUserEvent` emits it alongside
  `UserEcho` when `UserEvent.AgentResult` is non-nil. Agentique's
  `ToWireEvent` maps it to `WireAgentResultEvent`, which is persisted
  and broadcast. `TestPipeline_AgentResultPersisted` un-skipped.

### ~~No CI pipeline beyond release~~

- **Resolved (2026-05-27):** `.github/workflows/ci.yml` runs on PRs
  and pushes to master. Three parallel jobs: backend (`go vet` + `go
  test`), frontend (`biome check` + `tsc` + `vitest`), and
  typegen-freshness (`git diff --exit-code` after regeneration).
