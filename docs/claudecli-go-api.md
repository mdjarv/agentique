# claudecli-go API Reference

Reference notes for [allbin/claudecli-go](https://github.com/allbin/claudecli-go).
Go SDK wrapping the Claude Code CLI as a subprocess. Zero external dependencies.
Requires Go 1.23+ and Claude CLI >= 2.0.0 on PATH.

## Execution Modes

### 1. Interactive Session (primary for Agentique)

```go
client := claudecli.NewClient()
session, err := client.Connect(ctx,
    claudecli.WithWorkDir("/path/to/project"),
    claudecli.WithModel(claudecli.ModelSonnet),
    claudecli.WithPermissionMode(claudecli.PermissionDefault),
    claudecli.WithSystemPrompt("You are a helpful assistant"),
)

// Send a query
err = session.Query(ctx, "Explain the main.go file")

// Stream events
for event := range session.Events() {
    switch e := event.(type) {
    case *claudecli.TextEvent:
        // Assistant text content
    case *claudecli.ThinkingEvent:
        // Extended thinking
    case *claudecli.ToolUseEvent:
        // Tool invocation (name, input)
    case *claudecli.ToolResultEvent:
        // Tool result
    case *claudecli.ResultEvent:
        // Turn complete (cost, usage, stop reason)
    case *claudecli.ErrorEvent:
        // Error (may be fatal or non-fatal)
    }
}

// Wait for turn completion
result, err := session.Wait()

// Send another query (multi-turn)
err = session.Query(ctx, "Now refactor that function")

// Graceful shutdown
session.Close()
```

### 2. Streaming (one-shot)

```go
stream, err := claudecli.Run(ctx, "Explain this code",
    claudecli.WithWorkDir("/path"),
)
for event := range stream.Events() { ... }
result, err := stream.Wait()
```

### 3. Blocking (one-shot, simpler)

```go
result, err := client.RunBlocking(ctx, "Explain this code")
// result.Text, result.Cost, result.Usage, result.SessionID
```

### 4. Convenience Wrappers

```go
// Get full text response
text, result, err := client.RunText(ctx, "Explain this code")

// Get typed JSON response
var analysis CodeAnalysis
err := claudecli.RunJSON(ctx, "Analyze this code", &analysis,
    claudecli.WithJSONSchema(schema),
)
```

## Session Control Protocol

Mid-session commands (bidirectional JSON over stdin/stdout):

```go
session.SetModel(claudecli.ModelOpus)
session.SetPermissionMode(claudecli.PermissionBypass)
session.Interrupt()  // interrupt current generation
// MCP server add/remove also available
```

## Event Types

| Event | Fields | Description |
|---|---|---|
| `StartEvent` | model, args, workdir | Before CLI starts |
| `InitEvent` | sessionID, model, tools | Session initialized |
| `TextEvent` | content | Assistant text chunk |
| `ThinkingEvent` | content, signature | Extended thinking |
| `ToolUseEvent` | id, name, input (JSON) | Tool invocation |
| `ToolResultEvent` | id, content | Tool result |
| `ResultEvent` | cost, duration, usage, stopReason | Turn complete |
| `RateLimitEvent` | status, utilization | Rate limit info |
| `StderrEvent` | content | CLI stderr output |
| `ErrorEvent` | message, fatal, details | Error (implements error) |
| `ControlRequestEvent` | (internal) | Session control |
| `StreamEvent` | (partial) | Partial message updates |

## Configuration Options

### Model & Thinking

```go
WithModel(ModelSonnet)             // ModelHaiku, ModelSonnet, ModelOpus
WithFallbackModel(ModelHaiku)
WithMaxThinkingTokens(4096)
WithEffort(EffortHigh)             // EffortLow, EffortMedium, EffortHigh
```

### Prompts

```go
WithSystemPrompt("custom system prompt")
WithAppendSystemPrompt("appended to default")
```

### Tools & Permissions

```go
WithPermissionMode(PermissionDefault)  // Default, Plan, AcceptEdits, Bypass, DontAsk, Auto
WithTools("tool1", "tool2")            // Allow specific tools
WithDisallowedTools("tool1")           // Block specific tools
WithBuiltinTools("tool1")

// Permission callback (for interactive approval)
WithCanUseTool(func(req ToolPermissionRequest) PermissionResponse {
    // req.ToolName, req.Input, req.Suggestion
    return PermissionResponse{Allow: true}
})
```

### Sessions

```go
WithSessionID("existing-session-id")   // Resume specific session
WithResume()                           // Resume last session
WithContinue()                         // Continue last conversation
```

### Output

```go
WithJSONSchema(schemaString)           // Structured output with validation
```

### Execution

```go
WithWorkDir("/path/to/project")
WithEnv(map[string]string{"KEY": "val"})
WithTimeout(5 * time.Minute)
```

### MCP

```go
WithMCPConfig(config)
WithStrictMCPConfig(config)
```

## Session State Machine

```
StateStarting -> StateIdle -> StateRunning -> StateIdle (next query)
                                           -> StateDone
                                           -> StateFailed
```

- `StateStarting` - CLI process spawning
- `StateIdle` - Ready for next query
- `StateRunning` - Processing a query
- `StateDone` - Session ended normally
- `StateFailed` - Session ended with error

## Error Handling

```go
var cliErr *claudecli.Error
if errors.As(err, &cliErr) {
    cliErr.ExitCode
    cliErr.Stderr
    cliErr.Details.Type      // "rate_limit", "auth", "overloaded"
    cliErr.Details.RetryAfter
    cliErr.IsRateLimit()
    cliErr.IsAuth()
    cliErr.IsOverloaded()
}

var unmarshalErr *claudecli.UnmarshalError
if errors.As(err, &unmarshalErr) {
    unmarshalErr.Raw         // Original text before parse failure
}
```

## Testing

```go
// Fixture executor replays recorded JSONL (no real CLI needed)
executor := claudecli.NewFixtureExecutorFromFile("testdata/session.jsonl")
client := claudecli.NewClient(claudecli.WithExecutor(executor))

// BidiFixtureExecutor for interactive session testing
executor := claudecli.NewBidiFixtureExecutor(...)
```

## Key Design Decisions for Agentique

1. **Use `Session` (Connect) for all agent interactions** - multi-turn, streaming, lifecycle
2. **Forward events as-is to frontend** - converted to wire types via `ToWireEvent()`, event types map cleanly to UI rendering
3. **One event loop goroutine per session** - started at session creation, runs for session lifetime, detects turn boundaries via `ResultEvent`
4. **Session manager is a server-level singleton** - sessions survive WS reconnects, metadata persisted to SQLite
5. **SetCallbacks for WS reconnection** - new WS connection can adopt existing live sessions via `session.subscribe`
6. **Session.Close() for cleanup** - handles stdin EOF, SIGTERM, goroutine cleanup
7. **WithCanUseTool for permission UI** - callback bridges to WebSocket for user approval (M3)
8. **State machine maps to UI** - Idle/Running/Done/Failed/Stopped -> session badges (using Go constants)

## Lessons Learned (from M1 + M2 integration)

**API usage:**
- The client constructor is `claudecli.New()`, not `claudecli.NewClient()`.
- `Session.Query()` takes only `(prompt string)`, no `context.Context` parameter.
- `ResultEvent` fields: `CostUSD` (float64), `Duration` (time.Duration), `Usage` (struct), `StopReason` (string).
- `ToolResultEvent` uses `ToolUseID` (not `ID`) to correlate with `ToolUseEvent.ID`.
- `ErrorEvent` uses `Error()` method for the message string, and has `Fatal` bool.

**Event channel semantics:**
- `Events()` returns a **session-lifetime channel**, not a per-turn channel. Don't `range` over it expecting it to close after each turn -- instead watch for `ResultEvent` as the turn boundary.
- Do NOT call `Wait()` after draining `Events()` -- they share the same underlying channel. `Wait()` will block indefinitely waiting for events already consumed.
- claudecli-go's internal state transitions to `StateIdle` automatically when it sees a `ResultEvent` via `trackState()`. No explicit state management needed on our side.
- The event loop goroutine reads callbacks under mutex lock to support hot-swapping via `SetCallbacks`.

**Windows support:**
- claudecli-go required a fix: `Setpgid` and `syscall.Kill` don't exist on Windows. Fixed with build-tag-guarded `executor_unix.go` / `executor_windows.go`.
- Claude CLI subprocess init needed a Windows-specific pipe handling fix.
- First `Connect()` can take 30-40s on Windows due to Claude CLI subprocess init time.

**Multi-session (M2):**
- Multiple sessions per project share one `session.Manager` singleton.
- Each session gets its own event loop goroutine and claudecli-go `Session`.
- `WithWorkDir()` can point to either the project directory or a git worktree path.
- Session state changes are persisted to SQLite via the `onState` callback wrapper.
