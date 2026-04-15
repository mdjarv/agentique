# Cross-Agent Chat: Persistent Teams Brainstorm

## The Idea

"Discord for agents" — a persistent team of expert agents across projects that communicate autonomously. Each agent has a persistent identity (role, project, capabilities), can discover peers via lightweight personas, and messages them like developers on Slack/Discord. Coexists with current task-force swarms.

---

## Current Model vs Proposed Model

| | Task-Force (Swarm) | Persistent Team |
|---|---|---|
| **Lifecycle** | Ephemeral — created for a task, dissolved when done | Long-lived — exists as long as the team exists |
| **Hierarchy** | Lead → Workers (top-down) | Peer-to-peer (no hierarchy, optional roles) |
| **Member binding** | Members = sessions | Members = agent profiles (sessions are transient) |
| **Communication** | SendMessage within channel | SendMessage within team + DMs |
| **Scope** | Single project | Cross-project |
| **Discovery** | Lead knows workers by name (injected) | Persona-based discovery (Haiku answers "can you help with X?") |
| **Triage** | Lead decides what workers do | Persona triages incoming requests before spawning full session |

**Shared mechanics** (can be unified):
- Message routing (SendMessage between members)
- Channel/timeline UI
- Member awareness / context injection
- Message persistence and history

---

## Core Concepts

### Agent Profile

A persistent identity that outlives any single session.

```
agent_profiles:
  id          UUID
  name        TEXT        -- "Backend Expert", "Frontend Lead"
  role        TEXT        -- short role tag for context injection
  description TEXT        -- capabilities, expertise, project context
  project_id  UUID        -- primary project (where it creates worktrees)
  config      JSON        -- model, permission mode, behavior presets, custom instructions
  created_at  TIMESTAMP
```

- Not tied to a session — sessions come and go, the profile persists
- The profile is injected into every session's preamble as identity context
- Think of it as a "team member slot" that sessions fill temporarily

**Session binding:** An agent profile has 0 or 1 active sessions at any time.
- `agent_profiles.active_session_id` — nullable FK to sessions
- When a session stops/dies, the binding clears
- When a new session is needed, one is created with the profile's config + preamble

### Persona (Haiku Lightweight Agent)

Each agent profile has a **persona** — a stateless, Haiku-powered micro-agent that represents the profile when no full session is running.

**What it does:**
- **Discovery:** Other agents (or the user) can ask "Do you handle API routing?" → Haiku responds in <1s with a yes/no + context
- **Triage:** When a message arrives for an offline agent, the persona evaluates: "Is this worth spawning a full session?" → Spawn / Queue / Reject
- **Summary:** Can describe the agent's recent work, current status, capabilities
- **Delegation routing:** "I don't handle that, but Frontend Expert does" — redirects to the right teammate

**How it works:**
- Stateless — spun up per-query, no persistent process
- Context = agent profile description + recent message history + team roster
- Runs on Haiku (fast, cheap, good enough for classification/routing)
- Returns structured responses: `{action: "spawn" | "queue" | "reject" | "redirect", reason: "...", redirect_to?: "..."}`

**Why this is valuable:**
- Discovery is cheap — no Opus session needed to ask "who handles X?"
- Solves cascade risk without config — the persona IS the policy
- Natural gateway for future autonomy: persona decides auto-spawn, not a static rule
- Agents can learn about teammates without waking them up
- Provides visibility into decision-making: "why did/didn't this agent wake up?"

### Persistent Team Channel

Extends the existing channel concept.

```
teams:
  id, name, description, created_at

team_members:
  team_id (FK), agent_profile_id (FK), joined_at

team_channels:
  id, team_id (FK), name, topic, created_at

team_messages:
  id, channel_id (FK), sender_profile_id (FK), content,
  created_at, delivered_to (JSON array of profile IDs that received it)
```

Existing swarm `channels` + `channel_members` tables stay unchanged.

---

## Design Decisions (Resolved)

### Autonomy: User-gated for MVP
- Messages queue for offline agents. User sees notification, decides when to spawn.
- Persona handles discovery queries without spawning (cheap triage).
- Future: persona-gated auto-spawn (persona decides, user can override).

### Setup: UI Wizard
- Create team, add agent profiles, configure channels through the Agentique GUI.

### Lifecycle: Hybrid (on-demand with lazy resume)
- Active sessions stay alive while working.
- When idle too long, session stops but profile persists.
- Next interaction triggers lazy resume (existing infrastructure) or fresh spawn.
- Persona is always available (stateless, no process needed).

---

## MVP Phasing: Visibility First

The key insight: **observe patterns before committing to infrastructure.** Build visibility into how agents discover and communicate before building the full messaging system.

### Phase 0: Agent Profiles + Team Panel (visibility foundation) — DONE

**Goal:** Establish persistent agent identities and make them visible. No messaging yet.

**Build:**
- `agent_profiles` table + CRUD ✓
- `teams` + `team_members` tables + CRUD ✓
- UI: Team panel in sidebar (toggled via Users icon in header) ✓
- UI: Agent profile editor (name, role, description, project, avatar) ✓
- UI: Team editor (name, description, member management) ✓
- Session binding: `sessions.agent_profile_id` nullable FK, passed through CreateParams ✓
- Preamble includes team roster when session bound to profile ✓
- Experimental flag: `[experimental] teams = true` in config.toml ✓
- Backend: `backend/internal/team/` service package, 10 WS handlers ✓

**Not yet built (deferred to Phase 1+):**
- Agent status indicators (online/offline/working) in team panel — needs active session lookup
- Model/permission/behavior preset config in profile editor UI — backend supports it, UI deferred
- Session creation bound to agent profile from UI — backend supports it, needs UI integration

**What we learn:**
- Do agents naturally reference teammates by name?
- Does the roster injection change how agents think about their work?
- Is the profile description sufficient for agents to understand their role?

### Phase 1: Personas (discovery + triage)

**Goal:** Lightweight, observable agent-to-agent discovery.

**Build:**
- Persona endpoint: `POST /api/agent-profiles/:id/ask` — sends question to Haiku with profile context, returns answer
- Tool for active sessions: `AskTeammate(name, question)` — routes to persona, returns answer without spawning
- UI: Persona interactions visible in team panel (who asked what, what the persona answered)
- UI: "Ask" button on each agent profile card — user can query personas directly

**What we learn:**
- How do agents use discovery? What questions do they ask?
- Are persona responses accurate enough for routing decisions?
- What information do agents need that the profile description doesn't cover?

### Phase 2: Cross-Project DMs (messaging)

**Goal:** Agents can message each other across projects. User-gated delivery.

**Build:**
- `team_messages` table for persistence
- SendMessage routing extension: resolve team member names across projects
- Message queue for offline agents
- UI: DM view between two agents (timeline of messages)
- UI: Notification badge on offline agents with pending messages
- UI: "Deliver messages" button — user clicks to spawn/resume agent and deliver queued messages
- Persona triage: when message arrives for offline agent, persona evaluates and recommends spawn/queue/reject (displayed to user)

**What we learn:**
- How often do agents actually need cross-project communication?
- Is persona triage accurate? Does it reduce unnecessary spawns?
- What's the typical message→response→continue latency? Is it acceptable?

### Phase 3: Group Channels (team chat)

**Goal:** Shared channels for team-wide discussion.

**Build:**
- `team_channels` table
- Broadcast to channel: message goes to all members
- Channel history injection on session resume (last N messages as context)
- UI: Channel list within team, timeline view per channel
- Persona-mediated channel participation: persona can decide whether to wake agent for a channel message

### Phase 4: Autonomy (persona-gated auto-spawn)

**Goal:** Personas autonomously decide whether to spawn full sessions.

**Build:**
- Persona auto-spawn: if persona says "spawn," session is auto-created and message delivered
- Concurrent session cap (configurable per team)
- Merge gating: agents can commit but merges still require user approval
- UI: Autonomy settings per team (auto-spawn on/off, session cap, merge policy)
- UI: Audit log of persona decisions ("Backend Expert auto-spawned because Frontend Expert reported a 500 error")

---

## Message Routing (unified, target state)

```
SendMessage(to="PeerName", message="...")
  │
  ├── Is sender in a swarm channel with a member named PeerName?
  │   └── Yes → route via existing swarm logic (no change)
  │
  ├── Is PeerName a team member?
  │   ├── Has active session? → inject message via CLI
  │   ├── No active session? → query persona for triage
  │   │   ├── Persona says "spawn" → auto-spawn (if autonomy enabled) or queue + notify
  │   │   ├── Persona says "queue" → queue + notify user
  │   │   └── Persona says "redirect" → re-route to suggested teammate
  │   └── Message persisted to team_messages regardless
  │
  └── Is PeerName "@channel-name"?
      └── broadcast to all team members in that channel (same triage per member)
```

---

## Scenarios Walkthrough

### Scenario A: Frontend needs a new API route

1. User sends Frontend Expert a task: "Build the widgets page"
2. Frontend Expert starts working, realizes it needs `GET /api/widgets`
3. Frontend Expert calls `AskTeammate("Backend Expert", "Do you handle REST API endpoints?")` → persona confirms
4. Frontend Expert sends DM: "Need GET /api/widgets returning {id, name, status}"
5. Backend Expert is offline → persona evaluates: "This is an API implementation request, spawning recommended" → user sees notification
6. User clicks "Deliver" → Backend Expert spawns, reads message, starts working
7. Frontend Expert continues with unrelated parts (layout, state management)
8. Backend Expert creates endpoint, commits, merges, replies: "Done, merged to master"
9. Frontend Expert receives reply, pulls latest, wires up the API call

### Scenario B: Task-force within a persistent team

1. Backend Expert gets a complex task and spawns a task-force (swarm) of 3 workers
2. Workers run in isolated worktrees — standard swarm behavior, unchanged
3. Workers complete subtasks, report back to Backend Expert via swarm channel
4. Backend Expert synthesizes results, merges, posts to team #general: "Refactored auth middleware, 3 endpoints updated"
5. Task-force dissolves. Backend Expert continues as a persistent team member.

### Scenario C: Persona-mediated discovery

1. DevOps agent encounters a failing integration test
2. Calls `AskTeammate("Backend Expert", "The /api/health endpoint returns 503 in CI. Is this a known issue?")` 
3. Persona (Haiku) responds: "I maintain the health endpoint. A 503 typically means the DB migration hasn't run. Check if the CI step runs `goose up` before tests."
4. DevOps agent fixes the CI config without needing to spawn Backend Expert at all
5. Team panel shows: "DevOps asked Backend Expert's persona about health endpoint (resolved without spawn)"

---

---

## Deep Dive: Personas

### Implementation: Reuse BlockingRunner

`backend/internal/msggen/msggen.go` already has the exact pattern needed:
- `RunWithRetry()` wraps `claudecli.RunBlocking()` with retry on rate limits
- Uses `claudecli.WithModel(claudecli.ModelHaiku)`, `WithMaxTurns(1)`, disabled tools
- Stateless, one-shot, fast (~1-2s for Haiku)

Persona queries are structurally identical to commit message generation — a prompt goes in, a structured response comes out. New file: `backend/internal/persona/persona.go`, same `Runner` interface.

### Context Design: What the Persona "Knows"

Per-query context, assembled from DB + git:

```
STATIC (always included, ~500 tokens):
- Agent profile: name, role, full description
- Team roster: names + roles + projects of all teammates
- Project metadata: name, description

DYNAMIC (fetched per-query, ~500-1000 tokens):
- Session status: idle / running / stopped / offline
- If running: what the agent is currently working on (last user prompt)
- Last 5 messages sent/received by this agent
- Last 5 commits by this agent's sessions (from git log)

QUERY (from the caller, ~100 tokens):
- Who's asking (name, role, project)
- The question
- Optional context ("I'm building the widgets page and need...")
```

Total: ~1-2K tokens input. Haiku handles this in <1s.

**What's deliberately excluded:**
- Full codebase knowledge (persona doesn't read files)
- Full conversation history (too large, not needed for triage)
- Tool access (persona can't run code, read files, etc.)

The persona knows *about* the agent. It doesn't *become* the agent.

### Response Format

Structured output parsed from Haiku's response:

```
ACTION: answer | spawn | queue | reject | redirect
CONFIDENCE: 0.0-1.0
REDIRECT_TO: (teammate name, only if action=redirect)
REASON: (one-line explanation of why this action)

RESPONSE: (natural language answer to the caller)
```

**Actions explained:**
- `answer` — Persona handled it directly. No session needed. (Discovery queries, capability checks, status checks)
- `spawn` — This needs the full agent. Recommend spawning a session. (Work requests, bug reports, complex questions)
- `queue` — Not urgent. Queue for when the agent is next active. (FYI messages, non-blocking updates)
- `reject` — Not this agent's domain. Don't spawn. (Irrelevant requests)
- `redirect` — Wrong agent, but I know who's right. (Routing assistance)

### Query Types and Expected Behavior

| Query Type | Example | Expected Action |
|---|---|---|
| Discovery | "Do you handle API routing?" | answer |
| Capability | "Can you write database migrations?" | answer |
| Status | "What are you working on?" | answer |
| Work request | "I need GET /api/widgets" | spawn |
| Bug report | "Your API returns 500 on empty param" | spawn |
| FYI/informational | "I merged the new types" | queue |
| Off-topic | "How's the weather?" | reject |
| Wrong domain | "Can you fix the CSS layout?" (to Backend) | redirect → Frontend Expert |

### Persona System Prompt

```
You are the persona of {name}, a {role} on the {team_name} team.

## Your Identity
{description}

## Your Project
{project_name}: {project_description}

## Your Teammates
{for each teammate: "- {name} ({role}) — {project_name}, currently {status}"}

## Your Recent Activity
{last 5 commits}
{last 5 messages}

## Current Status
{session_status}: {current_task_summary or "idle"}

## Instructions

Someone is asking you a question. Evaluate it and respond.

- If you can answer directly (capability questions, status checks) → ACTION: answer
- If this requires your full attention (work requests, bugs) → ACTION: spawn
- If this is just informational / FYI → ACTION: queue  
- If this isn't your domain → ACTION: reject
- If another teammate is better suited → ACTION: redirect

Respond in EXACTLY this format:
ACTION: <action>
CONFIDENCE: <0.0-1.0>
REDIRECT_TO: <teammate name or empty>
REASON: <one line>

RESPONSE: <your natural language answer>
```

### Exposure: Two Interfaces

**1. Tool for active sessions: `AskTeammate`**

Injected into preamble when session is bound to an agent profile in a team:

```
You can query your teammates without waking them up:
  AskTeammate(name: "Backend Expert", question: "Do you handle API routing?")
This asks their persona (a lightweight proxy) and returns immediately.
Use this for discovery and quick questions before sending full work requests.
```

Implementation: EventPipeline intercepts `AskTeammate` tool use → calls `persona.Query()` → returns result as tool output. No session spawned.

**2. REST endpoint for the UI:**

```
POST /api/teams/:teamId/agents/:profileId/ask
Body: { "question": "What are you working on?" }
Response: { "action": "answer", "confidence": 0.9, "response": "I'm currently idle..." }
```

User can click an "Ask" button on any agent card in the team panel. Response displays in a popover or inline.

### Observability: The Visibility Layer

Every persona interaction is logged and displayed:

```
persona_interactions:
  id, profile_id, asker_type ("agent" | "user"), asker_id,
  question, action, confidence, response, redirect_to,
  created_at, response_time_ms
```

**Team panel shows:**
- Recent persona interactions per agent (who asked what, what happened)
- Triage decisions: "Frontend Expert asked → spawn recommended" with reason
- Redirect chains: "DevOps asked Backend → redirected to Frontend"
- Stats: how often each agent is queried, spawn rate, redirect rate

This is the **visibility** the user asked for — you can watch discovery patterns emerge before building full messaging.

### Failure Modes and Mitigations

| Failure | Impact | Mitigation |
|---|---|---|
| **False spawn** — persona recommends spawning for trivial query | Wasted session | User-gated in MVP; persona just recommends |
| **Missed urgency** — persona queues something urgent | Delayed response | UI shows queue with reasoning; user can override |
| **Bad redirect** — sends to wrong teammate | Wasted query | Target persona can re-redirect or reject; chain visible in UI |
| **Hallucinated capability** — claims agent can do something it can't | Wasted spawn | Profile description is source of truth; keep it accurate |
| **Stale context** — doesn't know about recent changes | Inaccurate answer | Include recent git log; accept ~5min staleness |
| **Persona loop** — agent A asks B, B redirects to A | Infinite redirect | Max redirect depth (2); return "unresolved" after limit |

### Cost / Performance

- Haiku via CLI `RunBlocking`: ~1-2s including CLI spawn overhead
- Input: ~1-2K tokens. Output: ~100-200 tokens.
- No tool use, no file access, single turn.
- Rate limit: shares the account's rate limit with other sessions. Under normal usage (a few persona queries per minute), negligible impact.
- Could batch-precompute persona contexts and cache for ~5min to reduce DB queries.

### Future: Persona as Autonomy Gateway

When autonomy is enabled (Phase 4):

```
Message arrives for offline agent
  → Persona evaluates
  → If action=spawn AND confidence > 0.8 → auto-spawn session, deliver message
  → If action=spawn AND confidence < 0.8 → queue, notify user with persona's reasoning
  → If action=queue/reject/redirect → handle accordingly, no spawn
```

The persona's confidence threshold becomes the tunable knob for autonomy — not a binary policy flag, but a continuous dial. User can set "auto-spawn threshold: 0.9" (conservative) or "0.5" (aggressive).

---

---

## Architectural Safety: Zero Breakage Strategy

### Principle: Additive only, existing paths untouched

Every new feature is behind a "is this session part of a team?" check. If no teams exist or the feature flag is off, all code paths are identical to today.

### New packages (no changes to existing packages)

```
backend/internal/team/        — team CRUD, membership, agent profile management
backend/internal/persona/     — persona query engine (uses msggen.Runner interface)
```

These packages are self-contained. They import from `session` (to read session state) but `session` does NOT import from them. Dependency flows one way.

### Existing packages: minimal, guarded changes

**`session/manager.go`** — Session creation:
- Add optional `AgentProfileID` to `CreateParams` (nullable, default empty)
- If set, look up profile and append team context to preamble
- If unset, existing behavior unchanged

**`session/event_pipeline.go`** — Tool interception:
- Add `AskTeammate` handler alongside existing `SendMessage` handler
- Only triggers if `toolName == "AskTeammate"` — completely separate from `SendMessage` path
- If no team features enabled, this handler is never registered

**`session/channel.go`** — SendMessage routing (Phase 2 only):
- Extend `RouteAgentMessage()`: after existing swarm lookup fails, try team member lookup
- This is a **fallback addition**, not a modification. Existing swarm routing runs first, unchanged.
- If team lookup also fails, same error as today

**`ws/handlers.go`** — New handlers (additive):
- `handleTeamList`, `handleTeamCreate`, `handleTeamMemberAdd`, etc.
- `handlePersonaQuery`
- All new message types, no changes to existing ones

### Database: New tables only

```
-- Phase 0: new tables, no changes to existing tables
agent_profiles (id, name, role, description, project_id, config, created_at)
teams (id, name, description, created_at)
team_members (team_id, agent_profile_id, joined_at)

-- Phase 1: persona logging
persona_interactions (id, profile_id, asker_type, asker_id, question, action, confidence, response, created_at)

-- Phase 2: team messaging
team_messages (id, channel_id, sender_profile_id, content, created_at, delivered_to)
team_channels (id, team_id, name, topic, created_at)
```

One addition to existing table:
```
sessions: + agent_profile_id UUID NULLABLE  -- links session to a profile (optional)
```

This is a nullable column. Existing sessions have NULL. All existing queries unaffected.

### Frontend: New components, existing components unchanged

```
New:
  src/components/team/TeamPanel.tsx       — sidebar panel for team overview
  src/components/team/AgentProfileCard.tsx — agent card with status + ask button
  src/components/team/ProfileEditor.tsx   — create/edit agent profiles
  src/components/team/PersonaPopover.tsx  — displays persona Q&A responses
  src/stores/team-store.ts               — Zustand store for team state

Unchanged:
  src/components/chat/*                   — existing session chat, channel panel
  src/stores/chat-store.ts               — existing session events
  src/stores/channel-store.ts            — existing swarm channels
```

The team panel is a new sidebar section. It doesn't replace or modify the existing session list, chat view, or channel panel.

---

## Experimental Flag

### Gating mechanism

Backend startup flag: `--experimental-teams` or env var `AGENTIQUE_EXPERIMENTAL_TEAMS=true`

```go
// backend/internal/config/config.go
type Config struct {
    // ...existing fields...
    ExperimentalTeams bool `env:"AGENTIQUE_EXPERIMENTAL_TEAMS"`
}
```

### What the flag controls

**When OFF (default):**
- DB migrations still run (tables exist but empty)
- Team-related WS handlers return `{"error": "teams feature not enabled"}`
- Session preamble does NOT inject team context
- `AskTeammate` tool is NOT injected into session tools
- SendMessage does NOT attempt team member routing (swarm-only, as today)
- Frontend: team panel hidden (backend returns empty team list)

**When ON:**
- Full team functionality available
- Team panel visible in UI
- Sessions can be bound to agent profiles
- Persona queries work
- SendMessage falls through to team routing after swarm lookup

### Implementation: single guard per entry point

```go
// In WS handler registration
if cfg.ExperimentalTeams {
    mux.Handle("team.list", handleTeamList(svc))
    mux.Handle("team.create", handleTeamCreate(svc))
    mux.Handle("persona.query", handlePersonaQuery(svc))
    // ...
}

// In preamble builder
if profileID != "" && cfg.ExperimentalTeams {
    preamble += buildTeamContext(profile, teammates)
}

// In SendMessage routing
if cfg.ExperimentalTeams {
    if routed := tryTeamRoute(senderID, targetName, content); routed {
        return nil
    }
}
```

No feature-flag checks deep in business logic. The flag gates at the boundary (handler registration, preamble building, routing entry point). Interior code doesn't know about the flag.

---

## Open Questions

1. **Can an agent be in multiple teams?** Probably yes, but adds routing complexity.
2. **Multiple agents per project?** Two backend experts in the same project — worktree conflicts?
3. **Persona context depth:** How much history/context does the persona need to be useful? Just the profile description, or also recent git log, open PRs, etc.?
4. **Rate limiting:** What prevents agents from spamming each other? Persona triage helps, but need a hard cap too.
5. **Session continuity:** When an agent is resumed for a team message, does it pick up where it left off (resume) or start fresh? Resume preserves context but may confuse if the previous task was different.
6. **Swarm ↔ Team interaction:** Can a swarm worker message a team member? Or only the swarm lead can communicate outside the swarm?
