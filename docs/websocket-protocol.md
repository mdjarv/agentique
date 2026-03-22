# Agentique WebSocket Protocol

WebSocket protocol between the React frontend and Go backend.
Simpler than t3code's approach -- no event sourcing, no Effect schemas.

**Status:** Fully implemented through M2. Multi-session support with
session.create, session.list, session.stop, session.subscribe, session.query.

## Transport

- Single WebSocket connection per browser tab (singleton WsClient)
- Endpoint: `ws://localhost:8080/ws`
- In dev mode, frontend connects directly to `:8080` (Vite WS proxy is unreliable)
- JSON messages within WebSocket frames
- Auto-reconnect with exponential backoff (500ms -> 1s -> 2s -> 4s -> 8s cap)
- CORS middleware skips WebSocket upgrade requests to avoid handshake corruption
- `request()` waits for connection before sending (handles race on page load)

## Message Format

All messages have a `type` field. Request/response pairs share an `id`.

### Client -> Server (Requests)

```jsonc
{
  "id": "req-1",              // unique per request, used to correlate response
  "type": "method.name",
  "payload": { ... }          // method-specific data
}
```

### Server -> Client (Responses)

```jsonc
{
  "id": "req-1",              // matches request id
  "type": "response",
  "payload": { ... }          // or "error": { "message": "..." }
}
```

### Server -> Client (Push Events)

```jsonc
{
  "type": "event.category",
  "payload": { ... }
}
```

Push events have no `id` -- they are fire-and-forget from server to client.

## Methods

### Session Management

```jsonc
// Create new session in a project (with optional worktree)
{
  "id": "1",
  "type": "session.create",
  "payload": {
    "projectId": "...",
    "name": "Session 1",          // optional, auto-generated if empty
    "worktree": true,             // optional, creates git worktree
    "branch": "feature-x"        // optional, auto-generated if empty
  }
}
// -> { "id": "1", "type": "response", "payload": {
//      "sessionId": "...", "name": "Session 1", "state": "idle",
//      "worktreePath": "...", "worktreeBranch": "feature-x", "createdAt": "..."
//    }}

// List sessions for a project
{ "id": "2", "type": "session.list", "payload": { "projectId": "..." } }
// -> { "id": "2", "type": "response", "payload": { "sessions": [...] } }

// Send a message to a session
{
  "id": "3",
  "type": "session.query",
  "payload": {
    "sessionId": "...",
    "prompt": "Refactor the auth middleware"
  }
}
// -> { "id": "3", "type": "response", "payload": {} }
// Events stream via push after this

// Stop a session (closes Claude CLI, removes worktree if present)
{ "id": "4", "type": "session.stop", "payload": { "sessionId": "..." } }
// -> { "id": "4", "type": "response", "payload": {} }

// Subscribe to an existing session's events (after WS reconnect)
{ "id": "5", "type": "session.subscribe", "payload": { "sessionId": "..." } }
// -> { "id": "5", "type": "response", "payload": {} }
```

Note: Project CRUD uses REST (`/api/projects`), not WebSocket.

## Push Events

### Session Events (forwarded from claudecli-go)

All events wrapped in `session.event` with `sessionId` for routing:

```jsonc
// Text chunk
{ "type": "session.event", "payload": { "sessionId": "...",
  "event": { "type": "text", "content": "Here's the code..." } } }

// Thinking
{ "type": "session.event", "payload": { "sessionId": "...",
  "event": { "type": "thinking", "content": "Let me analyze..." } } }

// Tool use
{ "type": "session.event", "payload": { "sessionId": "...",
  "event": { "type": "tool_use", "id": "call-1", "name": "Read",
             "input": { "file_path": "/src/main.go" } } } }

// Tool result
{ "type": "session.event", "payload": { "sessionId": "...",
  "event": { "type": "tool_result", "toolUseId": "call-1",
             "content": "package main..." } } }

// Turn complete
{ "type": "session.event", "payload": { "sessionId": "...",
  "event": { "type": "result", "costUsd": 0.0234, "duration": 12500,
             "usage": {...}, "stopReason": "end_turn" } } }

// Error
{ "type": "session.event", "payload": { "sessionId": "...",
  "event": { "type": "error", "message": "Rate limited", "fatal": false } } }
```

### Session State Changes

```jsonc
{
  "type": "session.state",
  "payload": {
    "sessionId": "...",
    "state": "running"    // "idle" | "running" | "done" | "failed" | "stopped"
  }
}
```

## Frontend Implementation Notes

### WebSocket Client (ws-client.ts)

- `request<T>(type, payload, timeoutMs)` -- sends request, resolves on correlated response
- `subscribe(type, handler)` -- push event listener, returns unsubscribe function
- `waitForConnection()` -- ensures WS is open before sending
- Auto-reconnect with exponential backoff
- Default timeout: 30s for queries, 120s for session.create (Claude CLI init is slow)

### Zustand Store (chat-store.ts)

Multi-session store shape:
```typescript
sessions: Record<string, SessionData>  // indexed by session ID
activeSessionId: string | null         // currently displayed session

// SessionData = { meta: SessionMetadata, turns: Turn[], currentAssistantText: string }
// All actions take sessionId for routing events to correct session
```

Push event routing:
```typescript
ws.subscribe("session.event", (payload) => {
  useChatStore.getState().appendEvent(payload.sessionId, event);
});
```

### Session Lifecycle

1. User navigates to project -> `session.list` fetches existing sessions
2. Live sessions re-subscribed via `session.subscribe`
3. First message auto-creates a session if none usable (stopped/done skipped)
4. Session creation takes ~30-40s (Claude CLI init)
5. Tab switching is instant (just changes activeSessionId)
6. Stopping a session auto-selects the next available tab

## Comparison with t3code

| Aspect | t3code | Agentique |
|---|---|---|
| Protocol | JSON-RPC style with `_tag` | Simple `type` field |
| Push | Channel-based with sequence numbers | Type-based, no sequencing |
| Events | Transformed through orchestration layer | Forwarded as-is from claudecli-go |
| Sessions | Event-sourced with projections | Simple CRUD with SQLite |
| Worktrees | Per-thread, shared between threads | Per-session, cleaned up on stop |
| Auth | Token via query param | None (MVP) |
| Reconnect | Exponential backoff, event replay | Exponential backoff, session.subscribe |
