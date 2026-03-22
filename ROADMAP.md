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
- **Syntax highlighting:** react-syntax-highlighter (frontend, Prism + oneDark)

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

## Project Structure

```
agentique/
  backend/
    cmd/
      agentique/             # main entrypoint
        main.go
    internal/
      server/                # HTTP server, routes, SPA handler, CORS
      ws/                    # WebSocket handler, connection, message types, dispatch
      session/               # Session manager (singleton), session wrapper, worktree module
      project/               # project CRUD handlers
      store/                 # SQLite persistence layer (sqlc generated)
    db/
      migrations/            # goose SQL migration files (001_projects, 002_sessions)
      queries/               # sqlc query files (projects.sql, sessions.sql)
      embed.go               # embeds migrations via embed.FS
      sqlc.yaml
    go.mod
    go.sum
  frontend/
    src/
      components/
        ui/                  # shadcn/ui components
        chat/                # ChatPanel, TurnBlock, SessionTabs, NewSessionDialog,
                             # MessageComposer, MessageList, Markdown, ThinkingBlock,
                             # ToolUseBlock, ToolResultBlock
        layout/              # AppSidebar, ProjectList, NewProjectDialog
      hooks/                 # useWebSocket, useChatSession, useProjects
      lib/                   # ws-client, api, types, utils
      stores/                # app-store (projects), chat-store (multi-session)
      routes/                # TanStack Router file-based routes
    index.html
    package.json
    biome.json
    vite.config.ts
    tsconfig.json
    components.json          # shadcn/ui config
  Makefile
  ROADMAP.md
```

## Development Experience

### Hot Reload / Fast Iteration

During development, frontend and backend run as separate processes:

```
# Terminal 1: Frontend (Vite dev server with HMR)
cd frontend && npm run dev        # localhost:5173, hot module replacement

# Terminal 2: Backend (Go with auto-rebuild)
cd backend && air                 # or: go run ./cmd/agentique -addr :8080
```

- **Frontend:** Vite HMR gives instant feedback on React/CSS changes. API requests
  are proxied to the Go backend via `vite.config.ts`.
- **Backend:** Use [air](https://github.com/air-verse/air) for auto-rebuild on .go
  file changes, or just `go run` manually.
- **Proxy config:** Vite proxies `/api/*` to `http://localhost:8080`. WebSocket
  connects directly to `:8080` in dev mode (bypasses Vite proxy for reliability).

In production, the Go binary embeds the built frontend -- but during dev we never
need to rebuild the frontend to test changes.

### Tooling

| Tool | Purpose |
|---|---|
| air | Go hot reload (rebuilds on file change) |
| Vite | Frontend dev server with HMR |
| Biome | Lint + format TypeScript/React |
| sqlc | Generate Go code from SQL queries |
| goose | Database migrations |

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
- Chat history is in-memory only (lost on page reload, M3 adds persistence)
- Worktree creation not tested end-to-end in browser (backend tests pass)

---

### M3: Polish + Persistence (post-MVP)

- [ ] Persist chat history to SQLite (messages table)
- [ ] Session resume via claudecli-go `WithResume()`
- [ ] Cost/token usage display per session and aggregate
- [ ] Tool permission handling from UI (approve/deny tool calls)
- [ ] Reconnection handling (WebSocket drops)
- [ ] Error toasts and better error UX
- [ ] Keyboard shortcuts (new session, switch tabs, focus composer)

### M4: Advanced Features (future)

- [ ] Git integration (branch per session, checkpointing, diffs)
- [ ] File browser / diff viewer
- [ ] Terminal emulator (xterm.js)
- [ ] Desktop app via Tauri
- [ ] Session templates / saved prompts
- [ ] Drag-and-drop session layout (split panes)

## claudecli-go Integration Notes

Key APIs in use:
- `claudecli.New()` -> `Client` for creating sessions
- `Client.Connect()` -> `*Session` for interactive multi-turn agents
- `Session.Query()` to send user messages
- `Session.Events()` channel for streaming events (session-lifetime, not per-turn)
- `Session.SetCallbacks()` for WS reconnection (added in M2)
- `Session.Close()` for graceful shutdown
- `WithModel(ModelOpus)` for Opus as default model
- `WithPermissionMode(PermissionBypass)` for no-approval mode
- `WithWorkDir()` to set the project or worktree directory

Lessons learned:
- `Events()` returns a session-lifetime channel, not per-turn. Detect turn boundaries via `ResultEvent`.
- Don't call `Wait()` after draining `Events()` -- they share the same channel.
- claudecli-go required a Windows fix for `Setpgid`/`syscall.Kill` (build tags in executor).
- Claude CLI init can take 30-40s on first connect; frontend needs long timeout for `session.create`.
- claudecli-go needed a Windows subprocess init fix (stdin/stdout pipe handling).

## Lessons from t3code

Things adopted:
- WebSocket for real-time event streaming (not polling)
- Session-per-agent model with independent lifecycle
- Sidebar navigation for projects and sessions
- Tab-based session switching
- Git worktree support for session isolation

Things avoided:
- Effect-TS complexity in the backend (our Go backend is simpler by nature)
- Event sourcing for MVP (overkill, simple CRUD is fine)
- Codex-first assumptions baked into the protocol
- Monorepo with 4+ apps (keep it simple: backend + frontend)
- Over-engineering the WebSocket protocol (keep messages thin, forward events as-is)
