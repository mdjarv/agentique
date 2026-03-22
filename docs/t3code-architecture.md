# t3code Architecture Reference

Reference notes from exploring [pingdotgg/t3code](https://github.com/pingdotgg/t3code).
Local clone at `/c/Projects/t3code/`.

## Overview

T3 Code is a GUI for concurrent AI coding agents (Codex-first, Claude secondary).
Monorepo with 4 apps (server, web, desktop, marketing) and 2 packages (contracts, shared).
Built on Effect-TS with event sourcing, SQLite persistence, and WebSocket transport.

## Stack

| Layer | Tech |
|---|---|
| Package manager | Bun |
| Monorepo | Turborepo |
| Frontend | React 19, Vite 8, TanStack Router, Zustand, Tailwind CSS |
| Backend | Node.js, Effect-TS, `ws` library |
| Desktop | Electron 40 |
| IPC | JSON-RPC 2.0 over stdio (to Codex/Claude CLI) |
| Persistence | SQLite via @effect/sql-sqlite-bun |
| Testing | Vitest, Playwright |
| Linting | oxlint, oxfmt |

## Server Architecture

Entry: `apps/server/src/main.ts`
HTTP + WebSocket: `apps/server/src/wsServer.ts`

- Node.js `http.createServer()` with `ws` WebSocket upgrade
- Default port 3773
- Optional auth token via query param
- Routes defined in `routeRequest()` function (~30 methods)
- All requests: `{ id, body: { _tag, ...params } }` -> `{ id, result?, error? }`

### Key Server Routes

| Route | Purpose |
|---|---|
| `orchestration.getSnapshot` | Full read model |
| `orchestration.dispatchCommand` | Submit command |
| `orchestration.replayEvents` | Event replay |
| `projects.searchEntries` | File search |
| `git.*` | Git operations |
| `terminal.*` | Terminal I/O |
| `server.getConfig` | Config + keybindings |

### Push Channels (Server -> Client)

```
server.welcome          - Connection init (cwd, project, bootstrap)
server.configUpdated    - Config changes
terminal.event          - Terminal output
orchestration.domainEvent - Domain events (the main one)
```

## Provider/Agent Management

### Adapter Pattern

Each provider implements `ProviderAdapterShape`:

Key methods:
- `startSession()` / `stopSession()`
- `sendTurn()` / `interruptTurn()`
- `respondToRequest()` (tool approvals)
- `streamEvents` (event stream)

Implementations:
- `CodexAdapter` - spawns `codex app-server` via node-pty, JSON-RPC over stdio
- `ClaudeAdapter` - uses `@anthropic-ai/claude-agent-sdk`

Registry: `ProviderAdapterRegistry` maps provider kind -> adapter instance.

### Session Lifecycle

1. Client sends `thread.turn.start` command
2. Orchestration engine validates, persists event
3. Provider reactor dispatches to adapter
4. Adapter streams events back
5. Events published via push bus to all WebSocket clients

Session state stored in `provider_session_runtime` table:
- threadId, provider, status, model, cwd, activeTurnId, etc.

## Orchestration (Event Sourcing)

### Core Pattern

```
Command -> Decider (pure) -> Event -> Projector (pure) -> Read Model
```

- `decider.ts` - pure functions: command + state -> events
- `projector.ts` - pure functions: event + state -> new state
- `OrchestrationEngine` - wires it together with persistence

### Command Types

- `project.create`, `project.meta.update`
- `thread.create`, `thread.turn.start`, `thread.revert`
- `thread.session.set`, `thread.runtime-mode.set`

### Database Schema

**Event store:**
```sql
orchestration_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT UNIQUE,
  aggregate_kind TEXT,     -- "project" | "thread"
  stream_id TEXT,          -- projectId or threadId
  stream_version INTEGER,
  event_type TEXT,
  occurred_at TEXT,
  command_id TEXT,
  payload_json TEXT,
  metadata_json TEXT
)
```

**Projection tables:**
- `projection_projects` - id, title, workspace_root, default_model, scripts
- `projection_threads` - id, project_id, title, model, branch
- `projection_thread_messages` - id, thread_id, turn_id, role, text
- `projection_thread_activities` - id, thread_id, kind, summary
- `projection_thread_sessions` - thread_id, status, provider
- `projection_turns` - thread_id, turn_id, state, checkpoint
- `projection_pending_approvals` - request_id, thread_id, status

## Frontend Architecture

Entry: `apps/web/src/main.tsx`

### State Management

| Store | Purpose |
|---|---|
| Zustand (`store.ts`) | App state (projects, threads) |
| TanStack React Query | Server data sync/cache |
| `composerDraftStore.ts` | Message draft persistence |
| `terminalStateStore.ts` | Terminal UI state |

### WebSocket Client

`apps/web/src/wsTransport.ts` - `WsTransport` class:
- Request/response RPC: `request<T>(method, params): Promise<T>`
- Push subscriptions: `subscribe(channel, listener): unsubscribe`
- Auto-reconnect with exponential backoff (500ms -> 8s)
- 60s request timeout
- Queues outbound during disconnection

### Chat Rendering

Key files:
- `components/ChatView.tsx` - main chat component
- `components/chat/MessagesTimeline.tsx` - message list
- `components/chat/ChatMarkdown.tsx` - markdown rendering
- `session-logic.ts` - derive timeline entries from read model

Flow: snapshot -> threads -> messages -> timeline entries -> render

### Key Frontend Patterns

- Rich text editor via Lexical (composer)
- Terminal emulator via xterm.js
- Diff viewer
- Drag-and-drop sidebar (dnd-kit)

## What We Should Adopt

- WebSocket with request/response + push channels
- Session-per-agent model
- Sidebar with projects/sessions
- Tab-based session switching
- Auto-reconnect with backoff
- Projection-based read model (but simpler than event sourcing)

## What We Should Avoid

- Effect-TS (massive complexity for what amounts to DI)
- Full event sourcing (simple CRUD is fine for MVP)
- Codex-first design assumptions
- node-pty dependency (we use claudecli-go over stdio)
- 4-app monorepo structure
