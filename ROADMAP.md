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
- **Markdown:** react-markdown + @tailwindcss/typography
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
|   Zustand        |                                   |  claudecli-go    |
|   shadcn/ui      |                                   |  sessions        |
+------------------+                                   +------------------+
                                                              |
                                                         stdin/stdout
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
{ "id": "req-1", "type": "session.create", "payload": { "projectId": "..." } }
{ "id": "req-2", "type": "session.query", "payload": { "sessionId": "...", "prompt": "..." } }

// Server -> Client (response, correlated by id)
{ "id": "req-1", "type": "response", "payload": { "sessionId": "..." } }

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
      ws/                    # WebSocket handler, connection, message types
      session/               # Claude session manager (wraps claudecli-go)
      project/               # project CRUD handlers
      store/                 # SQLite persistence layer (sqlc generated)
    db/
      migrations/            # goose SQL migration files
      queries/               # sqlc query files
      embed.go               # embeds migrations via embed.FS
      sqlc.yaml
    go.mod
    go.sum
  frontend/
    src/
      components/
        ui/                  # shadcn/ui components
        chat/                # ChatPanel, TurnBlock, MessageComposer, etc.
        layout/              # AppSidebar, ProjectList, NewProjectDialog
      hooks/                 # useWebSocket, useChatSession, useProjects
      lib/                   # ws-client, api, types, utils
      stores/                # app-store (projects), chat-store (session/turns)
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
- [x] Message rendering: user messages, assistant text (Markdown), thinking blocks (collapsible), tool use/result blocks
- [x] Working message composer: send prompt on Enter, disable while agent is running
- [x] Session state indicator badge (disconnected/idle/running)
- [x] Auto-scroll to latest message
- [x] Cost and duration display per turn

**Known issues:**
- First message takes ~30-40s due to Claude CLI subprocess initialization
- WebSocket connects directly to backend port in dev mode (Vite WS proxy unreliable)

---

### M2: Multi-Session + Multi-Project

**Goal:** Full MVP -- multiple parallel agent sessions across multiple projects.

Backend:
- [ ] Session manager supports multiple concurrent sessions (goroutine per session)
- [ ] Sessions scoped to projects (each session has a `projectId` and `workDir`)
- [ ] Session CRUD API + WebSocket commands (create, stop, list)
- [ ] Session metadata persisted to SQLite (id, project, model, state, created_at)
- [ ] Per-session configuration (model, permission mode, system prompt)

Frontend:
- [ ] Session tabs within a project: create new session tab, switch between sessions
- [ ] Each tab shows its own chat history and state
- [ ] Session creation dialog (pick model, optional system prompt)
- [ ] Stop/kill session button
- [ ] Project switching loads that project's sessions
- [ ] Zustand store restructured: projects -> sessions -> messages

**Deliverable:** Run multiple Claude agents in parallel on different tasks,
switch between them, manage multiple projects. Core MVP complete.

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
- `Session.Close()` for graceful shutdown
- `WithModel(ModelOpus)` for Opus as default model
- `WithPermissionMode(PermissionBypass)` for no-approval mode
- `WithWorkDir()` to set the project directory

Lessons learned:
- `Events()` returns a session-lifetime channel, not per-turn. Detect turn boundaries via `ResultEvent`.
- Don't call `Wait()` after draining `Events()` -- they share the same channel.
- claudecli-go required a Windows fix for `Setpgid`/`syscall.Kill` (build tags in executor).
- Claude CLI init can take 30-40s on first connect; frontend needs long timeout for `session.create`.

## Lessons from t3code

Things adopted:
- WebSocket for real-time event streaming (not polling)
- Session-per-agent model with independent lifecycle
- Sidebar navigation for projects and sessions
- Tab-based session switching

Things avoided:
- Effect-TS complexity in the backend (our Go backend is simpler by nature)
- Event sourcing for MVP (overkill, simple CRUD is fine)
- Codex-first assumptions baked into the protocol
- Monorepo with 4+ apps (keep it simple: backend + frontend)
- Over-engineering the WebSocket protocol (keep messages thin, forward events as-is)
