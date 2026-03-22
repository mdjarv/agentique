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

- **HTTP/WS server:** net/http + gorilla/websocket (or nhooyr.io/websocket)
- **Claude integration:** github.com/allbin/claudecli-go
- **Database:** SQLite via modernc.org/sqlite (pure Go, no CGO)
- **Query generation:** sqlc
- **Migrations:** goose or golang-migrate

### Frontend (TypeScript + React)

- **Framework:** React 19
- **Build tool:** Vite
- **Routing:** TanStack Router
- **State management:** Zustand
- **Styling:** Tailwind CSS 4 + shadcn/ui
- **Linting/Formatting:** Biome
- **Terminal rendering:** xterm.js (future, not MVP)

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

Simple JSON messages with a `type` field. All messages follow:

```jsonc
// Client -> Server
{ "type": "session.query", "sessionId": "...", "prompt": "..." }
{ "type": "session.create", "projectId": "...", "model": "sonnet", ... }
{ "type": "session.stop", "sessionId": "..." }

// Server -> Client
{ "type": "session.event", "sessionId": "...", "event": { /* claudecli-go event */ } }
{ "type": "session.state", "sessionId": "...", "state": "running" }
{ "type": "error", "message": "..." }
```

Events from claudecli-go are forwarded as-is to the frontend, keeping the protocol
thin and avoiding translation layers. The frontend understands the event types:
text, thinking, tool_use, tool_result, result, error, etc.

## Project Structure

```
agentique/
  backend/
    cmd/
      agentique/             # main entrypoint
        main.go
    internal/
      server/                # HTTP + WebSocket server, routes
      session/               # Claude session manager (wraps claudecli-go)
      project/               # project/workspace management
      store/                 # SQLite persistence layer
    db/
      migrations/            # SQL migration files
      queries/               # sqlc query files
      sqlc.yaml
    go.mod
    go.sum
  frontend/
    src/
      components/
        ui/                  # shadcn/ui components
        chat/                # chat-specific components
        layout/              # sidebar, header, panels
      hooks/                 # custom React hooks
      lib/                   # WebSocket client, types, utilities
      stores/                # Zustand stores
      routes/                # TanStack Router routes
    index.html
    package.json
    biome.json
    vite.config.ts
    tsconfig.json
    tailwind.config.ts
    components.json          # shadcn/ui config
  ROADMAP.md
```

## Development Experience

### Hot Reload / Fast Iteration

During development, frontend and backend run as separate processes:

```
# Terminal 1: Frontend (Vite dev server with HMR)
cd frontend && npm run dev        # localhost:5173, hot module replacement

# Terminal 2: Backend (Go with auto-rebuild)
air                               # or: gow run ./cmd/agentique
```

- **Frontend:** Vite's HMR gives instant feedback on React/CSS changes. The dev
  server proxies API/WebSocket requests to the Go backend (configured in `vite.config.ts`).
- **Backend:** Use [air](https://github.com/air-verse/air) for auto-rebuild on .go file
  changes. Restarts the server in ~1-2 seconds. Alternative: `gow` (Go watcher).
- **Proxy config:** Vite proxies `/api/*` and `/ws` to `localhost:8080` (Go backend)
  so the frontend can use relative paths and avoid CORS issues.

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

### M0: Skeleton + Static UI

**Goal:** Deployable app with API server and a non-functional chat layout.
No Claude integration yet -- just the shell of the application.

Backend:
- [ ] Go module setup with basic HTTP server
- [ ] Health check endpoint (`GET /api/health`)
- [ ] Embed and serve built frontend assets
- [ ] SQLite database initialization with initial schema (projects table)
- [ ] CRUD API for projects (`GET/POST/DELETE /api/projects`)

Frontend:
- [ ] Vite + React + TypeScript scaffolding
- [ ] Biome config
- [ ] Tailwind CSS + shadcn/ui setup
- [ ] App layout: sidebar + main content area
- [ ] Sidebar: project list (fetched from API), "new project" button
- [ ] Main area: static chat layout (hardcoded messages for visual design)
- [ ] Session tabs bar (non-functional, static)
- [ ] Message composer input (non-functional)

**Deliverable:** Run `go run ./cmd/agentique`, open browser, see the UI with
a sidebar listing projects and a chat area with placeholder messages.

---

### M1: Single Chat Session (WebSocket)

**Goal:** One working chat session connected to a real Claude Code agent.
Validates the full stack: frontend -> WebSocket -> Go -> claudecli-go -> Claude CLI.

Backend:
- [ ] WebSocket endpoint (`/ws`)
- [ ] Session manager: create a single claudecli-go `Session`
- [ ] Forward `session.query` messages to the session via `Session.Query()`
- [ ] Stream claudecli-go events back over WebSocket
- [ ] Session state tracking (idle, running, done, failed)
- [ ] Graceful session shutdown on WebSocket disconnect

Frontend:
- [ ] WebSocket client hook (`useWebSocket`)
- [ ] Zustand store for session state and messages
- [ ] Message rendering: user messages, assistant text, thinking blocks, tool use/result
- [ ] Working message composer: send prompt, disable while agent is running
- [ ] Session state indicator (badge showing idle/running/etc.)
- [ ] Auto-scroll to latest message
- [ ] Markdown rendering for assistant responses

**Deliverable:** Type a prompt, see Claude respond in real-time with streaming text,
tool calls rendered inline. One session, one project.

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
- [ ] Dark/light theme toggle
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

Key APIs we will use:
- `Client.Connect()` -> `Session` for interactive multi-turn agents
- `Session.Query()` to send user messages
- `Session.Events()` channel for streaming events to frontend via WebSocket
- `Session.Wait()` for turn completion
- `Session.Close()` for graceful shutdown
- `WithModel()`, `WithPermissionMode()`, `WithSystemPrompt()` for per-session config
- `WithWorkDir()` to set the project directory
- `WithCanUseTool()` for permission callbacks (M3)

Potential enhancements needed in claudecli-go:
- TBD based on integration experience

## Lessons from t3code

Things to adopt:
- WebSocket for real-time event streaming (not polling)
- Session-per-agent model with independent lifecycle
- Sidebar navigation for projects and sessions
- Tab-based session switching

Things to avoid:
- Effect-TS complexity in the backend (our Go backend is simpler by nature)
- Event sourcing for MVP (overkill, simple CRUD is fine)
- Codex-first assumptions baked into the protocol
- Monorepo with 4+ apps (keep it simple: backend + frontend)
- Over-engineering the WebSocket protocol (keep messages thin, forward events as-is)
