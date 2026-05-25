# Tech Debt — Provider Abstraction Migration

Captured 2026-05-25 at the end of the agentkit/runtime migration session
(commit 21e6157, branch `session-b082fd25`). Severity tiers describe what
will break or surprise someone first, not effort to fix.

## P0 — Will bite a user

### Claude partial-message streaming is OFF

- **Symptom:** assistant text appears all at once at the end of a turn instead
  of streaming. `AssistantTextDeltaEvent` / `StreamEvent` /
  `ContextSnapshot` are never emitted for claude sessions.
- **Cause:** the agentkit `runtime/cli/claude` adapter doesn't forward
  `claudecli.WithIncludePartialMessages()`. There is no `Extra` map hook on
  the claude side to plumb it through either.
- **Fix path:** upstream — extend `claude.NewConnector` to accept variadic
  `claudecli.Option` defaults (codex already does:
  `codex.NewConnector(defaults ...codexcli.Option)`), then in
  `backend/internal/server/server.go` pass
  `claudeadapter.NewConnector(claudecli.WithIncludePartialMessages(), claudecli.WithReplayUserMessages())`.
  Until then this is the most visible regression.

### `SendMessage` delivery confirmation is OFF

- **Symptom:** the "message delivered" transient wire event
  (`WireMessageDeliveryEvent`) never fires for messages injected via
  `SendMessage`. The UI doesn't show the receipt tick.
- **Cause:** same as above — `WithReplayUserMessages()` isn't plumbed
  through the claude adapter. The pipeline's `processUserEcho` correctly
  treats a `MessageID`-only UserEcho as a replay confirmation; agentkit
  just never produces one.
- **Fix:** same upstream change.

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

### Codex resume is a fresh-start, not a real resume

- **Symptom:** Stopping and resuming a codex session loses the
  conversation. `Service.resumeSession` detects `dbSess.Provider == "codex"`
  and always takes the `Reconnect` path.
- **Cause:** `codexcli-go` doesn't expose `--resume`, and the codex adapter
  returns `runtime.ErrResumeUnsupported`.
- **Fix path:** upstream codex SDK + adapter; until then, document this in
  the UI when the user picks the codex provider so it isn't a surprise.

### Codex feature flags are off but not surfaced in UI

- The runtime advertises capabilities (`PlanMode`, `Resume`, `Fork`,
  `MidTurnSendMessage`, `Thinking`, `Subagents`, `RateLimitEvents`,
  `CompactionEvents`, `InteractivePermissions`, `AskUserQuestion`, etc.)
  per session via `Capabilities()`. The frontend renders the same controls
  regardless of provider — buttons that don't work, plan-mode toggles that
  no-op, mid-turn-send composer that fails silently.
- **Fix path:** thread `Provider` (already added to `SessionInfo`) and a
  `Capabilities` snapshot through to the frontend store, then gate the UI
  affordances.

### Codex attachments path is half-baked

- `runtime/cli/codex/connector.go`'s `userInput` rejects
  image/document attachments with `ErrNotSupported`. agentique's
  `toRuntimeAttachments` happily produces them. Frontend lets the user
  attach files for any provider.
- **Fix path:** either teach the codex adapter to write attachments to
  temp files and pass paths (the codex SDK accepts paths), or have
  agentique inspect `Capabilities()` and disable the attach button for
  codex sessions.

### Frontend has no provider picker

- `SessionCreatePayload.Provider` is wired all the way through the backend
  and persisted, but no UI element sets it. New sessions always go to
  claude. Codex is reachable only via direct RPC.
- **Fix path:** add a provider dropdown to the New Session form;
  default to claude.

## P2 — Smells / drift

### `claudecli` still imported in five session-package files for narrow reasons

The migration intentionally keeps a few `claudecli` imports under
`backend/internal/session/`:

- `session.go` — `claudeSession()` type-assert via
  `interface{ Underlying() *claudecli.Session }` so MCP reconnect
  (`ReconnectMCPServer` / `ReconnectMCPServerWait`) can reach the
  claudecli session. Codex returns an error.
- `wire.go` — `errorDetail` + `wireErrorEvent`'s `errors.Is` chain for
  claudecli error sentinels. `runtime.ErrorKind` covers most cases but
  not `ErrNotFound` / `ErrRequestTooLarge`; the
  `claudecli.RateLimitError` type assertion picks up `RetryAfter`.
- `channel.go` — `claudecli.FormatAgentMessage` is a free helper the
  migration left in place; agentkit-side neutralization is a follow-up.
- `cli.go` — `BlockingRunner` is the autotitle path; deliberately
  not behind the runtime per the migration proposal.
- `msggen/msggen.go` — same: one-shot Haiku invocation for message-name
  generation, claude-only.

Each one is a small abstraction leak. None block correctness today, but
they constrain future providers.

### `WireAgentResultEvent` / `WireResultEvent.Usage`

`WireResultEvent.Usage` used to be the typed `claudecli.Usage`; it's now
populated from `runtime.TokenUsage` but the JSON field is typed `any` in
`wire.go` and the frontend reads through a permissive shape. The
serialized layout did not survive the migration unchanged — confirm the
frontend hasn't drifted before relying on `usage.*` fields end-to-end.

### `capturingConnector.hintNext` is racy under concurrent Create

The current routing scheme sets a per-Connect "next provider" hint right
before calling `m.rt.Create` / `Resume` and resets it on the connector
side. If two `Manager.Create` calls land at the same instant, the wrong
adapter could be picked. In practice agentique's `Manager` mutex
sequences these, but the contract is fragile. A cleaner design: pass the
provider through `runtime.ConnectParams.Extra["provider"]` and let
agentique's outer connector dispatch on that — no shared mutable state.

### `Service.resumeSession` switch-case is brittle

The provider-aware fresh-start logic is a hardcoded
`dbSess.Provider == "codex"` check. The moment a third provider lands
without `--resume`, this falls over silently. Better: ask the runtime
via `Capabilities().Resume` after a probe Connect, or carry the
fresh-start flag explicitly in the wire from agentkit.

### Frontend types: `agent_result` events are dead code

`WireAgentResultEvent` still appears in `frontend/src/lib/generated-types.ts`
because the Go type still exists. Any frontend code that listens for
`agent_result` will silently never fire for new sessions. Either delete
the wire type when the upstream is fixed, or leave a comment in
`wire.go` referencing this debt.

## P3 — Dependency hygiene

### Pinned versions

- `github.com/allbin/agentkit v0.0.0-20260525124511-5bd5f42cfa49` — a
  pseudo-version pointing at the migration commit. Upstream isn't tagged.
  If we depend on a fix landing in agentkit, we'll need to either tag a
  release or keep bumping the pseudo-version.
- `github.com/allbin/codexcli-go v0.0.0-20260525123631-5a0be1d76936` —
  same story, slightly earlier commit. The README explicitly warns the
  SDK is "early"; expect breaking changes.
- `github.com/allbin/claudecli-go v0.0.0-20260525103406-8e84ddd02dcc` —
  newer than the version agentkit's `go.mod` pins
  (`df6ce28bcd4e`, from April). MVS resolves to ours, but the agentkit
  claude adapter is tested against the older version.

### Skipped tests as silent debt

`TestPipeline_AgentResultPersisted` is `t.Skip`-ed with a comment
pointing at the AgentResult gap. Skipped tests rot quickly; this should
be revisited the moment agentkit grows the event. Add a checklist in
the agentkit follow-up issue rather than relying on someone running
`go test -v` and reading skip messages.

## P4 — Out of scope but noticed

- `WireResultEvent.Timestamp` is stamped by `emitWireEvent` in the
  pipeline if zero. Fine, but the pipeline shouldn't be authoring
  timestamps for events it didn't originate; the adapter should.
- `backend/internal/store/channels.sql.go` regenerated unrelated rows
  during this session — the order of columns in the SELECT matches
  CreateSession's column list (which now includes `provider`). Future
  schema additions on `sessions` will keep churning this file. Not
  fixable; just be aware.
- `frontend/src/lib/generated-{types,schemas}.ts` need a `just typegen`
  any time Go-side wire shapes change. There is no CI guard that this
  matches the Go source.
