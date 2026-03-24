# Improvements from t3code Comparison

Findings from comparing agentique against [t3code](https://github.com/pingdotgg/t3code), which wraps `@anthropic-ai/claude-agent-sdk` (official TypeScript SDK). Both apps solve the same problem — GUI for concurrent Claude Code sessions — with different stacks.

## High Impact

### 1. Tool Input Streaming (Partial Messages)

**Problem:** Agentique shows tool use blocks only after Claude finishes composing the input. Users stare at a spinner while Claude writes a 50-line edit or a long bash command.

**What t3code does:** Enables `--include-partial-messages`. Receives `content_block_delta` events with `input_json_delta` — tool inputs stream in character-by-character. The UI shows the command/edit being typed in real-time.

**What we have:** claudecli-go already supports `StreamEvent` and the `WithIncludePartialMessages()` option. We just never enable it.

**Fix:**
- Enable `WithIncludePartialMessages()` when connecting sessions
- Forward `StreamEvent` through the WS protocol as a new event type
- Frontend renders partial tool inputs (streaming JSON into the tool_use block)
- Handle `content_block_start` (tool started) and `content_block_stop` (tool input complete) transitions

**Complexity:** Medium. Backend change is trivial (one option flag + forwarding). Frontend needs a streaming JSON accumulator for tool input blocks.

### 2. Hung Session Watchdog

**Problem:** If the Claude CLI process hangs (not crashes — hangs), agentique has no detection. The session stays in "Running" forever. Users must manually stop and re-create.

**What t3code does:** Has 60s command timeouts at the process runner level.

**Fix:**
- Add a watchdog timer in the event loop. If no event arrives within N seconds (configurable, maybe 120s) during Running state, emit a warning event to the frontend.
- After a longer timeout (300s?), auto-transition to Failed with a "session appears hung" error.
- Reset the timer on every event received.

**Complexity:** Low. Timer in the event loop goroutine.

### 3. Rate Limit Handling

**Problem:** claudecli-go emits `RateLimitEvent` with status (active/approaching/exceeded) and utilization (0.0-1.0). Agentique's event loop silently ignores it. If Claude rate-limits, the user sees nothing — the session just seems slow or stuck.

**Fix:**
- Add `rate_limit` to the wire event types
- Forward to frontend
- Frontend shows a non-intrusive banner ("Rate limited — utilization 85%", "Rate limited — waiting for capacity")
- Consider auto-pausing query submission when utilization > 0.9

**Complexity:** Low. Wire type + frontend banner.

### 4. Event Type Classification

**Problem:** Agentique passes raw tool names to the frontend (`Bash`, `Edit`, `Read`, `Write`, `Glob`, `Grep`, `mcp__playwright__browser_click`, etc.). Frontend treats all tools identically — same rendering, same approval UI.

**What t3code does:** Classifies every tool into canonical types: `command_execution`, `file_change`, `file_read`, `mcp_tool_call`, `web_search`, `context_compaction`, etc. Frontend renders each type with specialized components.

**Fix:**
- Add a `category` field to tool_use wire events, classified server-side:
  - `Bash` → `command`
  - `Edit`, `Write`, `NotebookEdit` → `file_write`
  - `Read`, `Glob`, `Grep` → `file_read`
  - `mcp__*` → `mcp`
  - `WebSearch`, `WebFetch` → `web`
  - `Agent` → `agent`
  - `TodoWrite`, `TodoRead` → `task`
  - Everything else → `other`
- Frontend uses category for rendering decisions (icons, colors, auto-expand behavior, approval UX)

**Complexity:** Low. String matching on tool name, one new field.

### 5. Invalid State Transitions

**Problem:** `state.go` logs invalid transitions but allows them. A race between `ResultEvent` (→ Idle) and `Stop()` (→ Stopped) can leave the session in an inconsistent state. The state machine is advisory, not enforced.

**Fix:**
- `SetState()` should return `(oldState, error)` and reject invalid transitions
- Callers handle the error (usually: log and skip the action that triggered it)
- Use atomic CAS or hold the mutex across check-and-set

**Complexity:** Low. Tighten the existing state machine.

## Medium Impact

### 6. File Checkpointing / Rewind

**Problem:** claudecli-go supports `WithFileCheckpointing()` and `session.RewindFiles(messageID)`. Not exposed in agentique. Users can't undo Claude's file changes to a specific point in the conversation.

**What t3code does:** Tracks message UUIDs and can rewind file state to any assistant message boundary.

**Fix:**
- Enable `WithFileCheckpointing()` on Connect
- Add `session.rewind` WS method that calls `cliSess.RewindFiles()`
- Frontend: "Rewind to here" button on turn boundaries in chat history

**Complexity:** Medium. Backend plumbing is straightforward. UI design for the rewind UX needs thought.

### 7. Context Compaction Detection

**Problem:** When Claude's context window fills, the CLI emits a compaction event. Agentique doesn't detect or surface this. Users don't know their earlier conversation context may have been summarized/dropped.

**Fix:**
- Detect compaction events (either as a special tool_use or as a dedicated event type from StreamEvent)
- Insert a visual separator in the chat: "Context compressed — earlier messages summarized"
- Consider showing a tooltip with pre/post token counts if available

**Complexity:** Low. Detection + UI marker.

### 8. MCP Server Management

**Problem:** claudecli-go has `ReconnectMCPServer(name)`, `ToggleMCPServer(name, enabled)`, `GetMCPStatus()`. None exposed through agentique's WS protocol. If an MCP server disconnects mid-session, there's no way to reconnect without restarting the session.

**Fix:**
- Add WS methods: `session.mcp-status`, `session.mcp-reconnect`, `session.mcp-toggle`
- Frontend: MCP status panel (maybe in session settings or a debug drawer)

**Complexity:** Low-medium. Backend is direct passthrough. UI needs design.

### 9. Raw Event Logging (NDJSON)

**Problem:** Agentique persists events to SQLite but converts to wire format first, losing the raw claudecli-go event shape. When debugging issues, the DB events don't show what the CLI actually emitted.

**What t3code does:** Writes raw SDK messages to rotating NDJSON files (10MB max, 10 files, batched writes every 200ms). Invaluable for post-mortem debugging.

**Fix:**
- Optional NDJSON file logging per session (enabled via flag/env var, not always-on)
- Write raw events before conversion to wire format
- Rotate files to prevent unbounded growth
- Store alongside session data or in a debug directory

**Complexity:** Low. File writer goroutine with rotation.

### 10. Structured Error Forwarding

**Problem:** When Claude errors, agentique forwards a generic error message. claudecli-go's `Error` type has `IsRateLimit()`, `IsAuth()`, `IsOverloaded()` and parsed `ErrorDetails{Type, Message, RetryAfter}`. None of this reaches the frontend.

**Fix:**
- Enrich the `error` wire event with `error_type` (rate_limit, auth, overloaded, unknown) and optional `retry_after_seconds`
- Frontend shows actionable messages: "Rate limited — retry in 30s" vs "Authentication failed — check API key" vs generic "Error"

**Complexity:** Low. Depends on claudecli-go enriching `ErrorEvent` with details (see claudecli-go improvements doc).

## Lower Impact / Future

### 11. Plan Mode Proposal Extraction

t3code parses `<proposed_plan>` XML blocks from assistant text and emits `turn.proposed.completed` events. Could enable a dedicated plan review UI in agentique — render proposed plans as structured cards with accept/reject actions instead of raw markdown.

### 12. Task Management Passthrough

claudecli-go has `StopTask(taskID)`. Could enable cancelling individual sub-agent tasks without interrupting the whole session.

### 13. Fast Mode Toggle

claudecli-go doesn't expose `fastMode` yet (see claudecli-go improvements doc). Once it does, add a toggle in the session UI — useful for quick tasks where latency matters more than depth.

### 14. Auto-Retry on Transient Failures

Neither t3code nor agentique auto-retry on rate limits or transient errors. With rate limit events and retry_after info available, agentique could auto-retry the last query after the backoff period. Needs careful UX — show a countdown, let users cancel the retry.

### 15. Session Liveness Indicator

t3code's architecture maintains per-session event queues with health tracking. Agentique could add a lightweight heartbeat: backend pings each live Claude process periodically, frontend shows a stale indicator if no heartbeat received.

## DB Event Persistence Fragility

**Problem:** `session.go` logs DB errors on event insertion but still broadcasts the event to the frontend. If the DB is temporarily unavailable:
- Frontend shows events that won't survive a page reload
- Resume breaks because `claude_session_id` (stored via DB on InitEvent) may be missing
- Turn history has gaps

**Fix:**
- If DB insert fails, mark the session as "degraded" in the state machine
- Surface this to the frontend ("events may not persist — check disk space")
- Consider buffering failed inserts for retry

## Lazy Resume Fragility

**Problem:** `resumeSession()` depends on `claude_session_id` stored in DB from the InitEvent. If that DB write failed (above), or if the Claude CLI session expired server-side, resume silently fails.

**Fix:**
- Detect "session not found" errors from Claude CLI during resume
- Fall back to creating a new session with conversation context replayed from DB events
- Or surface the error clearly: "Previous session expired — start a new one?"
