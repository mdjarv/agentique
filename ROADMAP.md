# Agentique - Project Roadmap

## Vision

A lightweight GUI for managing concurrent Claude Code agents across multiple projects.
Inspired by [pingdotgg/t3code](https://github.com/pingdotgg/t3code) but purpose-built for Claude Code,
with a Go backend leveraging [allbin/claudecli-go](https://github.com/allbin/claudecli-go).

## Why not just use t3code?

- Codex-first design; Claude support is secondary and incomplete
- Buggy backend with stalling/crashing process management issues
- Heavy Node.js backend with Effect-TS adds unnecessary complexity
- We already have a battle-tested Go wrapper for Claude CLI

## Tech Stack

### Backend (Go)

- **HTTP/WS server:** net/http + gorilla/websocket
- **Claude integration:** github.com/allbin/claudecli-go
- **Database:** SQLite via modernc.org/sqlite (pure Go, no CGO)
- **Query generation:** sqlc
- **Migrations:** goose

### Frontend (TypeScript + React)

- **Framework:** React 19
- **Build tool:** Vite
- **Routing:** TanStack Router
- **State management:** Zustand
- **Styling:** Tailwind CSS 4 + shadcn/ui (Catppuccin Mocha theme)
- **Markdown:** react-markdown + @tailwindcss/typography + react-syntax-highlighter
- **Linting/Formatting:** Biome

### Deployment

- Single binary: Go backend embeds built frontend assets via `embed.FS`
- Separate dev servers during development (Vite dev server + Go backend)
- Desktop wrapper (Tauri) deferred to post-MVP

## Architecture

```
+------------------+         WebSocket / HTTP          +------------------+
|                  | <-------------------------------> |                  |
|   React SPA      |                                   |   Go Backend     |
|   (Vite)         |                                   |                  |
|   Zustand        |                                   |  session.Manager |
|   shadcn/ui      |                                   |  (singleton)     |
+------------------+                                   +------------------+
                                                              |
                                                     claudecli-go Sessions
                                                         (one per tab)
                                                              |
                                                       +------------------+
                                                       |  Claude CLI      |
                                                       |  processes       |
                                                       +------------------+
```

### WebSocket Protocol

JSON messages with `id` for request/response correlation. Push events have no `id`.

```jsonc
// Client -> Server (request)
{ "id": "req-1", "type": "session.create", "payload": { "projectId": "...", "name": "...", "worktree": false } }
{ "id": "req-2", "type": "session.list", "payload": { "projectId": "..." } }
{ "id": "req-3", "type": "session.query", "payload": { "sessionId": "...", "prompt": "..." } }
{ "id": "req-4", "type": "session.stop", "payload": { "sessionId": "..." } }
{ "id": "req-5", "type": "session.subscribe", "payload": { "sessionId": "..." } }

// Server -> Client (response, correlated by id)
{ "id": "req-1", "type": "response", "payload": { "sessionId": "...", "name": "...", "state": "idle" } }

// Server -> Client (push, no id)
{ "type": "session.event", "payload": { "sessionId": "...", "event": { "type": "text", "content": "..." } } }
{ "type": "session.state", "payload": { "sessionId": "...", "state": "running" } }
```

Event types forwarded from claudecli-go: text, thinking, tool_use, tool_result, result, error.

## Development

See `just --list` for all commands. Key ones:

```
just dev              # Run both servers in parallel
just dev-frontend     # Vite HMR on :9200
just dev-backend      # Go server on :9201
just check            # Biome lint + tsc typecheck
just test-backend     # Go tests
```

Frontend connects WebSocket directly to `:9201` (bypasses Vite proxy for reliability).
In production, the Go binary embeds the built frontend via `embed.FS`.

## Milestones

### M0: Skeleton + Static UI [DONE]

**Goal:** Deployable app with API server and a non-functional chat layout.

Backend:
- [x] Go module setup with basic HTTP server
- [x] Health check endpoint (`GET /api/health`)
- [x] Embed and serve built frontend assets
- [x] SQLite database initialization with initial schema (projects table)
- [x] CRUD API for projects (`GET/POST/DELETE /api/projects`)

Frontend:
- [x] Vite + React + TypeScript scaffolding
- [x] Biome config
- [x] Tailwind CSS + shadcn/ui setup (Catppuccin Mocha theme)
- [x] App layout: sidebar + main content area
- [x] Sidebar: project list (fetched from API), "new project" dialog with auto-name
- [x] Session tabs bar and message composer layout

---

### M1: Single Chat Session (WebSocket) [DONE]

**Goal:** One working chat session connected to a real Claude Code agent.

Backend:
- [x] WebSocket endpoint (`/ws`) with gorilla/websocket
- [x] Session manager wrapping claudecli-go `Session`
- [x] Forward `session.query` messages to the session via `Session.Query()`
- [x] Stream claudecli-go events back over WebSocket (text, thinking, tool_use, tool_result, result, error)
- [x] Session state tracking (idle, running, done, failed)
- [x] Session-lifetime event loop detecting turn boundaries via ResultEvent
- [x] Graceful session shutdown on WebSocket disconnect
- [x] CORS middleware skips WebSocket upgrade requests

Frontend:
- [x] WebSocket client class with request/response correlation and push subscriptions
- [x] Auto-reconnect with exponential backoff
- [x] Zustand chat store with turn-based event accumulation
- [x] Message rendering: user messages, assistant text (Markdown with syntax highlighting), thinking blocks (collapsible), tool use/result blocks (compact format, collapsed results)
- [x] Working message composer: send prompt on Enter, disable while agent is running
- [x] Session state indicator badge (disconnected/idle/running)
- [x] Auto-scroll to latest message
- [x] Cost and duration display per turn
- [x] Streaming activity indicator (spinner while waiting for content)

---

### M2: Multi-Session + Worktrees [DONE]

**Goal:** Multiple parallel agent sessions per project with optional git worktree isolation.

Backend:
- [x] Session manager refactored to server-level singleton (survives WS reconnects)
- [x] Sessions table in SQLite (id, project_id, name, work_dir, worktree_path, state)
- [x] Session CRUD via WebSocket: session.create, session.list, session.stop, session.subscribe
- [x] Git worktree create/remove module (`~/.agentique/worktrees/`)
- [x] Session state persisted to DB, overridden by live state for active sessions
- [x] SetCallbacks for WS reconnect to adopt existing live sessions
- [x] Graceful shutdown via srv.Shutdown() closes all live sessions
- [x] Session state constants (StateIdle, StateRunning, etc.)
- [x] Project path validated as directory

Frontend:
- [x] Chat store rewritten: `sessions: Record<id, SessionData>` with `activeSessionId`
- [x] Interactive session tabs: click to switch, X to stop, + to create
- [x] New Session dialog with name input and optional worktree toggle
- [x] Auto-create session on first message (preserves M1 UX)
- [x] Auto-select next tab when stopping active session
- [x] Skip stopped/done sessions when auto-creating
- [x] WsClient.request() waits for connection before sending
- [x] Event routing by sessionId to correct session in store
- [x] Session list fetched on project navigation, live sessions re-subscribed

**Bug sweep (post-M2):**
- [x] Query failure resets session state to idle (not stuck in running)
- [x] NewSessionDialog shows error on creation failure
- [x] Fixed nested button HTML in ProjectList (div role="button")
- [x] Aria-labels on all icon-only buttons
- [x] Zero console errors in normal operation

**Known limitations:**
- First session creation takes ~30-40s (Claude CLI subprocess init)

---

### M3: Polish + Persistence

**Done:**
- [x] Session resume via claudecli-go `WithResume()`
- [x] Tool permission handling from UI (approve/deny tool calls)
- [x] WebSocket reconnection with exponential backoff
- [x] Keyboard shortcuts (Ctrl+N new session, Ctrl+1-9 switch)
- [x] Event persistence to SQLite (`session_events` table)
- [x] Git worktree diff viewer

**Remaining:**
- [ ] Reload chat history from DB on session resume (events are persisted, frontend doesn't reload them)
- [ ] Systematic error UX (sonner toasts used in spots, no global coverage)

---

### M4: Advanced Features (future)

- [ ] Git integration: merge worktree branches, create PRs (backend infra exists in `session/git.go`)
- [ ] Terminal emulator (xterm.js)
- [ ] Desktop app via Tauri
- [ ] Session templates / saved prompts
- [ ] Split pane session layout

## claudecli-go Notes

- `Events()` is session-lifetime, not per-turn. Detect turn boundaries via `ResultEvent`.
- Claude CLI init takes ~30-40s on first connect; frontend needs long timeout for `session.create`.
- See [docs/claudecli-go-api.md](docs/claudecli-go-api.md) for full API reference.
