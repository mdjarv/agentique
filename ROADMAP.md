# Agentique - Project Roadmap

## Vision

A lightweight GUI for managing concurrent Claude Code agents across multiple projects.
Inspired by [pingdotgg/t3code](https://github.com/pingdotgg/t3code) but purpose-built for Claude Code,
with a Go backend leveraging [mdjarv/claudecli-go](https://github.com/mdjarv/claudecli-go).

## Why not just use t3code?

- Codex-first design; Claude support is secondary and incomplete
- Buggy backend with stalling/crashing process management issues
- Heavy Node.js backend with Effect-TS adds unnecessary complexity
- We already have a battle-tested Go wrapper for Claude CLI

See [README.md](README.md) for architecture, tech stack, and development setup.

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

- [x] Git integration: merge worktree branches, create PRs
- [x] Todo/task checklist visualization
- [x] Tool input streaming (partial messages)
- [x] Rate limit handling + UI banner
- [x] Tool category classification (command, file_write, file_read, mcp, etc.)
- [x] State machine enforcement (reject invalid transitions)
- [ ] Hung session watchdog — detect sessions with no events for N seconds, auto-fail after timeout
- [ ] File checkpointing / rewind — claudecli-go supports `WithFileCheckpointing()` + `RewindFiles()`
- [ ] MCP server management — expose reconnect/toggle/status via WS
- [ ] Terminal emulator (xterm.js)
- [ ] Desktop app via Tauri
- [ ] Session templates / saved prompts
- [ ] Split pane session layout
- [ ] Use `/btw` (side query) for auto-naming and PR description generation — requires claudecli-go support for the `/btw` protocol

---

### M5: Channel Coordination & Agent Hierarchy [DONE]

**Goal:** Turn channels from flat membership lists into first-class coordination surfaces — agents introduce themselves with structured capabilities, leads can spawn workers autonomously, and the resulting tree is observable and actionable in the UI.

**Channel introductions:**
- [x] `messageType: "introduction"` emitted once per (session, channel) pair on join, deduped via a message-count query
- [x] Intro content includes avatar, role, worktree path, capabilities; structured metadata in `messages.metadata` for programmatic consumption
- [x] Intros excluded from the legacy per-session `agent_message` event writer so they don't pollute single-session timelines
- [x] `PersonaConfig.capabilities` added (backend + frontend); capability tag input in `ProfileForm`; generate prompt parses `CAPABILITIES:` field

**Agent-initiated spawning:**
- [x] `SpawnAuthCallback` on `Session` decides UI approval / auto-approve / reject before the approval flow runs
- [x] Channel leads auto-approve `@spawn`; workers get a structured reject explaining to ask the lead; non-channel sessions keep the existing UI approval flow
- [x] `SpawnWorkersRequest.channelId` lets a lead add workers to an existing channel instead of creating a fresh one
- [x] `messageType: "spawn"` audit message emitted to the target channel on every successful spawn (both UI-approved and auto-approved)

**Dynamic hierarchy:**
- [x] `sessions.parent_session_id` column with `ON DELETE CASCADE` FK (migration 033)
- [x] `CreateSwarm` / `extendSwarm` populate the parent pointer with the lead's ID
- [x] `DeleteSession` walks descendants depth-first, running the full cleanup path (stop, worktree, branch, files, broadcast) for each child before the parent row disappears; FK cascade is the fallback
- [x] `SessionInfo.parentSessionId` on the wire type; `buildSessionHierarchy` + `countDescendants` helpers

**Teams tab UI:**
- [x] "Session hierarchy" collapsible tree section on `/teams`, sorted alphabetically, hides solo sessions
- [x] Clickable node labels navigate to `/project/$projectSlug/session/$sessionShortId`
- [x] State dot per node (running / idle / merging / failed / done) driven by the live Zustand sessions map
- [x] Trash button with cascade-count confirmation ("N descendant(s) will be deleted")
- [x] Scissors button on channel-lead nodes that invokes `dissolveChannel` — stops workers, cleans worktrees, deletes channel, keeps the lead alive

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

---

---

### Persistent Teams & Cross-Agent Chat (experimental)

**Status:** Phase 0 complete. Gated behind `[experimental] teams = true` in config.toml.

**Full brainstorm:** [`plans/persistent-teams-brainstorm.md`](plans/persistent-teams-brainstorm.md)

**Concept:** A persistent team of expert agents across projects that communicate like developers on Discord. Each agent has a persistent identity (profile), can discover peers via lightweight Haiku-powered personas, and messages teammates across project boundaries. Coexists with existing task-force swarms — swarms are ephemeral and hierarchical, teams are persistent and peer-to-peer.

**Core new concepts:**
- **Agent Profile** — persistent identity (name, role, capabilities, project) that outlives sessions. Sessions are transient bindings.
- **Persona** — stateless Haiku micro-agent representing an offline profile. Handles discovery ("do you handle API routing?"), triages incoming messages (spawn/queue/reject/redirect), provides summaries. Uses existing `BlockingRunner` pattern from `msggen.go`.
- **Team Channel** — persistent group chat. Message history survives session restarts.

**Architectural approach:**
- Additive only — new packages (`team/`, `persona/`), new DB tables, new WS handlers. Existing session/channel/swarm code untouched.
- Shared improvements welcome: if we find a better message routing pattern, both swarms and teams benefit. But existing features must keep working. If fundamental changes are needed, restoring existing functionality is top priority.
- One nullable column on `sessions` table (`agent_profile_id`). All existing queries unaffected.
- Feature flag gates at boundaries (handler registration, preamble building, routing entry point). Interior code doesn't know about the flag.

**Phased rollout:**

| Phase | Focus | Key Deliverables |
|---|---|---|
| **0: Visibility** | Agent profiles + team panel | **Done.** `agent_profiles` / `teams` tables, profile editor UI, team panel in sidebar, session→profile binding, preamble team context injection |
| **1: Personas** | Discovery + triage | **Done.** `persona/` package, `AskTeammate` tool, persona query via Haiku, interaction logging, profile generation (`agent-profile.generate`) |
| **2: Channels** | Multi-agent coordination | **Done** (M5 above). Unified `messages` + `message_deliveries` tables, channel CRUD, `@spawn` worker delegation with lead auto-approval, structured introductions with capabilities, `parent_session_id` hierarchy tree, clickable hierarchy with delete-cascade and dissolve actions |
| **3: Cross-project DMs** | Cross-project messaging | `channels.project_id` currently NOT NULL — make nullable or add junction table. Metadata-driven 1:1 routing or lightweight auto-created 1:1 channels. Message queue for offline agents already in place via `message_deliveries` |
| **4: Topology presets** | Named preamble modes | `PersonaConfig.communicationMode` field exists but unused. Inject different routing instructions per mode: `spoke` (default), `mesh` (workers talk directly), `spoke+request` (workers ask lead before peer messaging). No routing enforcement — preamble text only |
| **5: Autonomy tuning** | Gated auto-spawn policy | Persona confidence threshold as autonomy dial, concurrent session cap already enforced via `max_sessions`, spawn idempotency key, partial-failure reporting in spawn audit messages |

**Key design decisions:**
- User-gated for MVP — messages queue for offline agents, user decides when to spawn
- Personas as the autonomy gateway — confidence threshold replaces binary policy flags
- Visibility first — observe discovery patterns (persona interactions) before building full messaging

---

## Inspiration

Half-baked ideas worth revisiting once the underlying systems mature.

### Cross-Session Delegation for Project Housekeeping

When merging a worktree branch, the project root must be clean (no uncommitted changes). If a "local" session (no worktree, working directly on master) owns those changes, the merge UI could offer a button to message that session: "commit your changes so I can merge." The local agent knows what it was doing and can write a proper commit message.

**Depends on:** inter-session messaging (channels), a way to identify which session "owns" uncommitted project-root changes, handling the case where dirty state isn't from any agent (manual edits, external tools).

**Simpler alternative:** A "commit all & merge" compound action that auto-commits the project root with a generated message, then proceeds with the merge. No agent coordination needed.

---

## claudecli-go Notes

- `Events()` is session-lifetime, not per-turn. Detect turn boundaries via `ResultEvent`.
- Claude CLI init takes ~30-40s on first connect; frontend needs long timeout for `session.create`.
- Source: [github.com/mdjarv/claudecli-go](https://github.com/mdjarv/claudecli-go)
