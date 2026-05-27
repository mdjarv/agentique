# Tech Debt

Maintained as a living document. Severity tiers describe what will break
or surprise someone first, not effort to fix.

Last full audit: 2026-05-27 (provider library upgrade session).

## P0 — Will bite a user

### `AgentResult` metadata is dropped

- **Symptom:** Agent (subagent) completions no longer carry
  `agent_id` / `agent_type` / `total_duration_ms` / `total_tokens` /
  `total_tool_use_count`. Tool results inside the same UserEcho still
  flow.
- **Cause:** the neutral `runtime.UserEcho` shape only has
  `ToolResults []ToolResult`; there's no field for AgentResult. The
  agentkit claude adapter drops the field when mapping `UserEvent`.
- **Wire impact:** `WireAgentResultEvent` definition is preserved but
  unreachable from the new pipeline. Frontend code that listens for
  `agent_result` events sees them stop arriving for new sessions.
- **Fix path:** either add a new neutral `runtime.AgentResultEvent`
  (preferred) or a structured field on `UserEcho`. Then re-emit from
  agentique's `processUserEcho`. Skipped tests (`TestPipeline_AgentResultPersisted`)
  mark the gap.

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

### Frontend types: `agent_result` events are dead code

`WireAgentResultEvent` still appears in `frontend/src/lib/generated-types.ts`
because the Go type still exists. Any frontend code that listens for
`agent_result` will silently never fire for new sessions.

### `WireResultEvent.Usage` typed as `any`

`WireResultEvent.Usage` is typed `any` in `wire.go` — populated from
`runtime.TokenUsage` but the frontend reads through a permissive shape.
Should be a concrete struct.

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

`TestPipeline_AgentResultPersisted` is `t.Skip`-ed with a comment
pointing at the AgentResult gap. Skipped tests rot quickly; this should
be revisited the moment agentkit grows the event.

### No CI guard for typegen freshness

`frontend/src/lib/generated-{types,schemas}.ts` need a `just typegen`
any time Go-side wire shapes change. There is no CI check that the
generated output matches the Go source.

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
