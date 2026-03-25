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

### M3: Polish + Persistence [DONE]

- [x] Session resume via claudecli-go `WithResume()`
- [x] Tool permission handling from UI (approve/deny tool calls)
- [x] WebSocket reconnection with exponential backoff
- [x] Keyboard shortcuts (Ctrl+N new session, Ctrl+1-9 switch)
- [x] Event persistence to SQLite (`session_events` table)
- [x] Git worktree diff viewer
- [x] Reload chat history from DB on session resume
- [x] Systematic error toasts (sonner) for user-initiated actions

---

### M4: Advanced Features (future)

- [ ] Git integration: merge worktree branches, create PRs (backend infra exists in `session/git.go`)
- [ ] Terminal emulator (xterm.js)
- [ ] Desktop app via Tauri
- [ ] Session templates / saved prompts
- [ ] Split pane session layout

---

## Investigations

### Prompt Handoff: Sessions Spawning Sessions

**Status:** Open question — needs design exploration.

**Concept:** A "planning" session can produce prompt suggestions that spawn new sessions with one click. Enables a workflow where you start broad ("review the roadmap"), then fan out into parallel execution sessions for individual items.

**Example flow:**
1. User starts a session: "review the roadmap and suggest what to work on next"
2. Agent responds with analysis and prioritized items
3. User: "help me create prompts for items 3 and 4"
4. Agent responds with structured prompt suggestions, each rendered with a "Start Session" button
5. Clicking the button creates a new session (with worktree) pre-filled with that prompt

**Open questions:**

- **Detection:** How does the frontend know a response contains a "spawn-able prompt"? Options:
  - **Structured tool output:** Claude already emits tool_use events. Could we define a convention (e.g. a fenced block with a special marker like ` ```prompt `) that the frontend parses into a button? Fragile but zero backend work.
  - **Custom tool:** Register a fake MCP tool (e.g. `suggest_session`) in the system prompt so Claude emits it as a tool_use event. Frontend intercepts and renders a button instead of a tool block. More reliable detection but couples to Claude's tool-use behavior.
  - **Post-processing:** Backend scans assistant text for a known pattern and injects metadata into the event stream. Most robust detection but adds complexity.

- **Prompt content:** Should the spawned session get just the prompt text, or also inherit context (project, worktree settings, CLAUDE.md references)? Probably: same project, auto-worktree, prompt only.

- **Parent-child relationship:** Should we track which session spawned which? Useful for:
  - Showing a "spawned from" breadcrumb
  - Letting the parent session see child status ("items 3 and 4 are in progress")
  - Potential future: parent waits for children, aggregates results

- **UI affordance:** Where do spawn buttons live?
  - Inline in the chat message (most natural)
  - A dedicated "suggested sessions" panel
  - Both — inline buttons + a collected list in the sidebar

- **Batch spawning:** "Create sessions for all 5 items" — should this be a single action that creates multiple sessions at once?

**Decided approach — tiered:**

1. **Agentique preamble (DONE):** `WithAppendSystemPrompt` injects a runtime preamble into every session (`session/preamble.go`). Claude knows it's inside Agentique, knows parallel sessions and worktrees exist, and knows how to suggest session prompts via ` ```prompt title="..." ``` ` fenced blocks. This preamble is the foundation for all future runtime awareness features.

2. **Frontend parsing (next):** Parse ` ```prompt ` fenced blocks from assistant text, render each as a card with the prompt text + a "Start Session" button. Button calls `session.create` + `session.query` with the prompt. No parent-child tracking.

3. **Future:** Custom tool approach (`suggest_session` tool_use events) if markdown parsing proves fragile. Parent-child session tracking. Batch spawning. `/fan-out` skill for explicit invocation.

---

### Sibling Session Awareness

**Status:** Future investigation — depends on preamble infrastructure.

**Concept:** Sessions know about other active sessions in the same project. Enables coordination: avoiding duplicate work, aligning on shared interfaces, reporting sibling status.

**How it'd work:** Build the preamble dynamically at connect time by querying active sessions for the project. Inject a summary like: *"Other active sessions: [B: 'refactor auth' (running), C: 'avatar upload' (idle)]"*.

**Key challenge: session descriptors go stale.** A session's initial prompt doesn't reflect where it ends up after several turns of discussion. Needs a mechanism for sessions to self-describe their current focus — either:
- Claude periodically emits a structured "status" (like a tool_use convention)
- Backend infers a summary from recent assistant text
- Session name gets updated as the conversation evolves

**Other open questions:**
- **Staleness during conversation:** Preamble is set at connect time. Sibling state changes mid-conversation won't be visible unless we can update the system prompt per-query (need to check claudecli-go support).
- **Token cost:** Grows linearly with active sessions. May need to cap or summarize aggressively.
- **Over-coordination:** Claude might spend tokens reasoning about siblings when it should just focus. Probably opt-in or only injected when >1 sibling exists.

## claudecli-go Notes

- `Events()` is session-lifetime, not per-turn. Detect turn boundaries via `ResultEvent`.
- Claude CLI init takes ~30-40s on first connect; frontend needs long timeout for `session.create`.
- See [docs/claudecli-go-api.md](docs/claudecli-go-api.md) for full API reference.
