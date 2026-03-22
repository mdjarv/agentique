# Agentique WebSocket Protocol

Design for the WebSocket protocol between frontend and Go backend.
Deliberately simpler than t3code's approach -- no event sourcing, no Effect schemas.

## Transport

- Single WebSocket connection per browser tab
- Endpoint: `ws://localhost:8080/ws`
- JSON messages, newline-delimited is not needed (WebSocket frames)
- Auto-reconnect with exponential backoff on the client side

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

### Project Management

```jsonc
// List all projects
{ "id": "1", "type": "project.list" }
// -> { "id": "1", "type": "response", "payload": [{ "id": "...", "name": "...", "path": "..." }] }

// Create project
{ "id": "2", "type": "project.create", "payload": { "name": "my-app", "path": "/home/user/my-app" } }
// -> { "id": "2", "type": "response", "payload": { "id": "...", "name": "my-app", "path": "..." } }

// Delete project
{ "id": "3", "type": "project.delete", "payload": { "projectId": "..." } }
// -> { "id": "3", "type": "response", "payload": {} }

// Update project settings
{ "id": "4", "type": "project.update", "payload": { "projectId": "...", "name": "...", "defaultModel": "sonnet" } }
```

### Session Management

```jsonc
// Create new session in a project
{
  "id": "5",
  "type": "session.create",
  "payload": {
    "projectId": "...",
    "model": "sonnet",            // optional, defaults to project default
    "systemPrompt": "...",        // optional
    "permissionMode": "default"   // optional
  }
}
// -> { "id": "5", "type": "response", "payload": { "sessionId": "..." } }

// List sessions for a project
{ "id": "6", "type": "session.list", "payload": { "projectId": "..." } }

// Send a message to a session
{
  "id": "7",
  "type": "session.query",
  "payload": {
    "sessionId": "...",
    "prompt": "Refactor the auth middleware"
  }
}
// -> { "id": "7", "type": "response", "payload": {} }
// Events stream via push after this

// Stop a session
{ "id": "8", "type": "session.stop", "payload": { "sessionId": "..." } }

// Interrupt current generation
{ "id": "9", "type": "session.interrupt", "payload": { "sessionId": "..." } }
```

## Push Events

### Session Events (forwarded from claudecli-go)

```jsonc
// Text chunk from assistant
{
  "type": "session.event",
  "payload": {
    "sessionId": "...",
    "event": {
      "type": "text",
      "content": "Here's the refactored code..."
    }
  }
}

// Thinking
{
  "type": "session.event",
  "payload": {
    "sessionId": "...",
    "event": {
      "type": "thinking",
      "content": "Let me analyze the middleware..."
    }
  }
}

// Tool use
{
  "type": "session.event",
  "payload": {
    "sessionId": "...",
    "event": {
      "type": "tool_use",
      "id": "tool-call-1",
      "name": "Read",
      "input": { "file_path": "/src/middleware/auth.go" }
    }
  }
}

// Tool result
{
  "type": "session.event",
  "payload": {
    "sessionId": "...",
    "event": {
      "type": "tool_result",
      "id": "tool-call-1",
      "content": "package middleware\n\nfunc Auth()..."
    }
  }
}

// Turn complete
{
  "type": "session.event",
  "payload": {
    "sessionId": "...",
    "event": {
      "type": "result",
      "cost": 0.0234,
      "duration": 12500,
      "usage": { "inputTokens": 1200, "outputTokens": 800 },
      "stopReason": "end_turn"
    }
  }
}

// Error
{
  "type": "session.event",
  "payload": {
    "sessionId": "...",
    "event": {
      "type": "error",
      "message": "Rate limited",
      "fatal": false
    }
  }
}
```

### Session State Changes

```jsonc
{
  "type": "session.state",
  "payload": {
    "sessionId": "...",
    "state": "running"    // "starting" | "idle" | "running" | "done" | "failed"
  }
}
```

## Frontend Implementation Notes

### WebSocket Client

Simple class wrapping native WebSocket:
- `send(type, payload): Promise<response>` -- request/response with timeout
- `subscribe(eventType, callback): unsubscribe` -- push event listeners
- Auto-reconnect with backoff: 500ms, 1s, 2s, 4s, 8s
- Queue outbound messages during reconnection
- 30s request timeout

### Zustand Store Integration

```typescript
// On push event:
ws.subscribe("session.event", (msg) => {
  const { sessionId, event } = msg.payload;
  useStore.getState().appendEvent(sessionId, event);
});

ws.subscribe("session.state", (msg) => {
  const { sessionId, state } = msg.payload;
  useStore.getState().setSessionState(sessionId, state);
});
```

## Comparison with t3code

| Aspect | t3code | Agentique |
|---|---|---|
| Protocol | JSON-RPC style with `_tag` | Simple `type` field |
| Push | Channel-based with sequence numbers | Type-based, no sequencing (for now) |
| Events | Transformed through orchestration layer | Forwarded as-is from claudecli-go |
| Auth | Token via query param | None (MVP) |
| Reconnect | Exponential backoff, event replay | Exponential backoff, refetch state |

## Future Considerations

- Message sequencing / ordering guarantees (if needed)
- Binary protocol (protobuf) for performance (unlikely needed)
- Authentication (when adding multi-user or remote access)
- Event replay on reconnect (fetch missed events from server)
