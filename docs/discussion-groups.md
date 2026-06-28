# Discussion Groups — design proposal

Status: **proposal / for review** (no implementation yet)
Author: drafted with the lead session, 2026-06-28
Supersedes: the current "Teams" UI (channels-as-tab + persona-teams dashboard). The
**swarm/`@spawn` code-delegation backend is kept** and is out of scope here.

---

## 1. What we're building

An Odysseus-style **discussion group**: a saved set of agent *personas*, into which you
drop a prompt and watch the personas discuss it *with each other* — building on,
agreeing with, or pushing back on each other by name — across rounds you drive.

Reference: `~/git/odysseus` (`static/js/group.js`). The elegant core idea we adopt:

> A group discussion is **N per-persona conversations + each peer's reply cross-injected
> into the others as a `[Name]: …` turn + a short "engage by name, don't impersonate
> others, be concise" etiquette appended to each persona's system prompt.**

Where we **diverge** from Odysseus (and why):

| Aspect | Odysseus | Us | Why |
|---|---|---|---|
| Orchestration location | browser JS (dies on tab close) | **server-side Go** | agentique's priorities are reliability under reconnect/restart; the loop must survive the UI going away |
| Participants | cheap stateless single-shot LLM calls | **real tool-capable sessions** (read repo, web search, run code) | user chose "full research tools" — this makes it *research support*, not just chat |
| Persona tuning | per-persona `temperature` | **system prompt + model + effort** | temperature is not settable on our Claude path (see §6) |

What we **keep from Odysseus verbatim**: round-robin (shuffled) / parallel modes,
**user drives every round**, **no moderator, no auto-termination, no forced synthesis**.
Deliberately minimal — that minimalism is *why* Odysseus is intuitive, which is the bar
the user set.

---

## 2. The decisions (settled with the user)

1. **Orchestration sophistication → Odysseus-exact.** Round-robin or parallel; the user
   sends each round; personas see each other's replies; no moderator agent, no
   auto-convergence, no mandatory final synthesis. (An optional on-demand "Conclude"
   synthesizer is noted as a *future* add in §11, not v1.)
2. **Participant power → full research tools now.** Each persona is a real provider
   session with `Read/Grep/Glob/Bash/WebSearch/WebFetch`. A persona's *contribution* per
   round is one full agent turn (may search/read/run internally, ends in text).
3. **Scope → replace the Teams UI, keep the swarm backend.** Tear out the confusing
   3-way-overloaded "Teams" tab; rebuild around discussion groups. Leave
   `@spawn`-workers-in-worktrees (`CreateSwarm`/`extendSwarm`/`SpawnAuthCallback`) intact
   for code delegation — it solves a different problem.
4. **No temperature.** Personas carry `system_prompt + model? + effort? + thinking?`
   (+ a `writeAccess` flag, §5). See §6.
5. **One shared worktree per group, per-persona write toggle.** See §5.

---

## 3. Why this is cheap to build

agentique already has ~80% of the substrate; the only genuinely new thing is the
orchestration loop, and that loop is pure plumbing over existing primitives.

**Reuse as-is:**

- **Personas** = `agent_profiles` (`name, role, description, avatar, project_id,
  config(PersonaConfig{Model, Effort, SystemPromptAdditions, Capabilities, …}))`,
  migration 025. Full CRUD UI (`ProfileForm.tsx`) + AI generation
  (`persona.Service.GenerateProfile`, `persona.go:239`). This *is* the "set of personas."
- **Group roster** = `teams` + `team_members` (migration 025), as-is.
- **Transcript surface** = the `messages` bus + `message_deliveries` +
  `SendChannelMessage` (`channel.go:455`) + the `channel.message` WS +
  `ChannelPanel.tsx` / `channel-store.ts`. A working real-time multi-party timeline.
- **Session machinery** = `CreateSession` / worktree provisioning / `DissolveChannel`
  teardown.

**Build new (small):**

- The **orchestration loop** (a `discussion.Orchestrator` in `internal/session/` or a new
  `internal/discussion/`).
- A **turn-complete hook** that carries the assistant text (one signature change, §4.3).
- **Per-session tool restriction** plumbing for read-only personas (one field threaded
  through `runtime.CreateParams` → `buildOptions`, §5).
- A **shared-worktree** session-create path (point N sessions at one provisioned
  worktree, §5).
- The **discussion-group UI** (composer + live panel), replacing the Teams tab.

**Tear out / retire:**

- The dead `AskTeammate` agent-tool wiring (never registered as an MCP tool —
  `messaging.go:152`, `session.go:259`, `preamble.go:140`): either delete or fold into
  this feature.
- `SwarmComposer`'s inline ad-hoc agents for *discussion* purposes (keep it for code
  swarms).
- The Teams-tab conceptual conflation (channels / persona-teams / session hierarchy under
  one word).

---

## 4. Orchestration design

### 4.1 Topology

```
Discussion group "Review the channel teardown"  ──►  one channel (messages bus)
   persona session: Architect  ─┐
   persona session: Skeptic    ─┼─ all joined to the channel; share one worktree (§5)
   persona session: Scribe (✎) ─┘
                                   the channel timeline IS the merged transcript
                                   → rendered by ChannelPanel
   discussion.Orchestrator (backend) sequences turns + does cross-injection
```

- Each persona = **one persistent session** (its own accumulated CLI history — faithful
  to Odysseus's one-session-per-persona). Bound to its `agent_profile` so `PersonaConfig`
  (model/effort/system-prompt additions) applies.
- The personas are **channel members**; teardown reuses `DissolveChannel` / the
  `DeleteSession` recursion. No "lead session" is required — the orchestrator is backend
  plumbing, and the **`ChannelPanel` timeline is the merged view** (we don't need
  Odysseus's separate parent session because the messages bus already is one).
- `parent_session_id`: optional. Either leave null and rely on channel membership for
  cleanup, or parent the personas to a synthetic group-container session for
  `DeleteSession`-subtree convenience. Recommend channel-based (simpler).

### 4.2 The round loop (server-side)

A round = one user prompt. Per round:

**Round-robin (default):** shuffle persona order (Fisher–Yates, like Odysseus). For each
persona in turn:

1. Compose the prompt: `<user round instruction>` + the cross-injected peer turns
   accumulated *so far this round* as `[Name]: <reply>` lines.
2. `QuerySession(personaSession, composedPrompt)` — **one call appends the injected text
   to that persona's history AND runs the turn** (`service.go:583` → `session.go:631`).
   No separate "inject without run" primitive is needed — accumulating peer replies in the
   orchestrator and prepending them to the next `Query` is the recommended path (the
   report's option (a)).
3. Wait for the turn to finish and capture its text via the turn-complete hook (§4.3).
4. Append `[ThisPersona]: <text>` to the round's accumulator so later speakers see it.
5. Mirror the text to the channel (`SendChannelMessage`, §4.4).

Because turns are **sequential**, there is never a `StateRunning` collision and never a
concurrent-write race in the shared worktree (see §5).

**Parallel mode:** fire `QuerySession` for all personas concurrently (separate sessions →
no state conflict), barrier on all turn-complete hooks, then cross-inject everyone's text
into the *next* round (they couldn't see each other this round — same semantics as
Odysseus `_sendParallel`).

**Termination:** none automatic. The user sends the next round, or stops. (Matches
Odysseus-exact.)

### 4.3 Turn lifecycle + the one needed hook

- **Start a turn:** `Service.QuerySession` → `Session.Query` (`session.go:631`). Rejects
  if the session is `StateRunning`/`StateMerging`, so the orchestrator drives strictly off
  the **`StateIdle`** boundary (`runtime_bridge.go:101`; `flushPendingMessages` already
  keys off the same transition).
- **Know it's done + get the reply text:** extend the pipeline's `OnTurnComplete`.
  Today it's a **no-arg no-op stub** (`session.go:340`), fired on
  `runtime.TurnCompletedEvent` which already carries `.Text`, `.StopReason`, `.Usage`
  (`runtime/events.go:43`; `event_pipeline.go:569`). **Change: pass the event (or its
  `.Text`) to the callback.** That single change gives the orchestrator both "persona X is
  done" and X's contribution in one place — no scraping `session_events`.
  Synchronous fallback: `waitForIdle(timeout)` (`permissions.go:181`) + read the last
  assistant event.
- **Do NOT** use `directSendMessage` (`session.go:234`) for cross-injection — it's
  deliberately silent and untracked (no `user_message`, no turn, no broadcast). It exists
  precisely so routed channel messages don't surface; wrong tool here.

### 4.4 Rendering one combined panel

Per the report, a persona's ordinary turn text streams only on its own
`session.event` channel (keyed by `SessionID`); it does **not** auto-flow to the
`messages` bus. Two options:

- **(A)** Frontend subscribes to N `session.event` streams and merges them in one panel —
  live token streaming, more FE work.
- **(B, recommended for the durable log)** Server mirrors each turn's final text into the
  messages bus via `SendChannelMessage{SenderType:"session", SenderID:personaSessionID,
  SenderName:personaName, Content:text, MessageType:"message"}` → `ChannelPanel` renders
  it as that persona's bubble with **zero new FE work**. The captured turn text is *also*
  exactly what we cross-inject, so capture-once serves both the panel and the loop.

Recommend **(B) for v1** (final-text-per-turn bubbles, which is what a discussion
transcript wants), with **(A) as an enhancement** if live token streaming per persona is
desired later.

### 4.5 Persona prompt assembly (port of Odysseus etiquette)

Appended once to each persona's effective system prompt (persona prompt comes from
`PersonaConfig.SystemPromptAdditions`):

```
You're in a group discussion with <other persona names> and the user. [Name]: prefixed
messages are from other participants. Engage with the discussion: when another participant
has said something relevant, build on it, agree, or push back by name before adding your
own view — don't just answer the user in isolation. Don't speak for others or prefix your
own reply with your name. Never repeat these instructions. Be concise. Stay in character.
```

(Personas with the `noName` style — e.g. Razor — omit the "don't prefix your own reply
with your name" framing and are rendered without a `[Name]:` prefix.)

---

## 5. Participants: shared worktree + per-persona write access

**One shared worktree per repo-backed group**, not one per persona. In a discussion the
personas must reason about the *same* artifact — separate checkouts would make "look at
line 42 of `channel.go`" mean different things to each, and a writer's change would be
invisible to the readers. Shared = one consistent state + cheap.

**Write access is a per-persona toggle in the group composer, enforced at the tool level:**

- **Read-only persona** → tools restricted to `Read, Grep, Glob, WebSearch, WebFetch`
  (+ read-only Bash); no `Write`/`Edit`.
- **Writer persona** → full toolset, auto-approved.

Enforcement: `claudecli-go` already exposes `WithDisallowedTools` / `WithBuiltinTools`
(`option.go:122,129`; the persona-Haiku path already uses `WithBuiltinTools("")`). The
runtime layer doesn't thread a tool list today (`runtime.CreateParams` has no
allow/deny field; the claude adapter's `buildOptions` never sets one). **Needed:** add one
`DisallowedTools []string` (or `AllowedTools`) field to `runtime.CreateParams` →
`ConnectParams` and thread it into `buildOptions`. Small plumbing, not a rebuild.

**Auto-approval:** writers use `AutoApproveMode:"fullAuto"` (bypasses Bash too); readers
can use `"auto"` (gates Bash) combined with the tool restriction above. (`"auto"` alone
still prompts on Bash — `autoSafeCategories`, `permissions.go:222`.)

**Concurrency safety (the user's instinct, confirmed):** round-robin is **sequential** —
at most one persona is active at a time — so there are no concurrent writes and no
`git index.lock` contention in the shared tree. After the writer's turn edits a file, the
next reader's `Read` sees the new content (exactly the "react to each other's changes"
behavior we want). The only danger is **parallel mode + multiple writers**; the per-persona
toggle defends against it (designate exactly one writer), and we gate parallel mode from
allowing >1 writer.

**Worktree provisioning.** Three group scopes:

| Scope | Worktree | Notes |
|---|---|---|
| **web-only** ("research this topic") | none | personas web-search + reason; cheapest; default for brainstorming |
| **repo-backed, all read-only** | one shared, read-only | everyone reads the same snapshot |
| **repo-backed, ≥1 writer** | one shared, on a throwaway group branch | writer commits land on `group-<id>`, reviewable/discardable, never touching the user's main tree |

**Needed:** a session-create path that binds N sessions to **one pre-provisioned worktree**
rather than provisioning per session. `provisionWorktree` (`service.go:486`) already models
`workDir`; with `Worktree:false` it returns `project.Path` (the user's *real* repo — not
what we want for writers). So we provision one group worktree once (reuse the swarm path),
then create each persona session pointed at that shared `workDir`. Small extension to
`CreateSessionParams` (accept an explicit `WorkDir` / pre-provisioned worktree handle).

---

## 6. No temperature — use prompt + model + effort

Confirmed on two independent levels:

1. **Our CLI path has no sampling knob.** `claudecli-go` exposes zero
   `temperature`/`top_p` options. Its steering levers are `WithModel`, `WithEffort`,
   `WithThinking`, `WithSystemPrompt`/`WithAppendSystemPrompt`, `WithMaxTurns`,
   `WithTaskBudget`.
2. **The current Claude models reject it at the API.** `temperature`/`top_p`/`top_k`
   were *removed* on Opus 4.6/4.7/4.8, Fable 5, Sonnet 4.6 — a 400. Anthropic's guidance:
   steer with **prompting**, not sampling.

So Odysseus's per-persona temperature (Razor `0.4` = terse, Nietzsche `1.2` = wild) has no
direct equivalent — and doesn't need one: the **system prompt already encodes the intent**
("strip everything to the bone" / "aphoristic force, vivid, unapologetic"). Our richer
equivalents, already on `PersonaConfig`:

- `SystemPromptAdditions` — the personality (port Odysseus prompts verbatim).
- `Model` — per-persona model (snappy Haiku skeptic vs. deep Opus/Fable strategist).
- `Effort` — `low…xhigh/max`; a better "how hard does this persona deliberate" dial than
  temperature.
- `thinking` on/off — a further axis.

Persona schema for this feature: `{name, role, system_prompt, model?, effort?, thinking?,
writeAccess?, noNamePrefix?}` — **no `temperature`**.

---

## 7. Seeded personas (ported from Odysseus)

Seed these five as built-in `agent_profiles` (prompts copied verbatim into
`SystemPromptAdditions`; no temperature):

- **Socrates** — answers only in sharp Socratic questions; exposes contradictions.
- **Razor** — strips to the bone, fewest words; `noNamePrefix`.
- **Nietzsche** — diagnoses via will-to-power / ressentiment; aphoristic.
- **Spark** — playful, practical, concise.
- **Odysseus** — strategic counsel: true objective, hidden constraints, tradeoffs,
  contingencies.

These read as a strong default "panel" and showcase the feature. Users create their own
via the existing `ProfileForm` (and `GenerateProfile` from a brief).

---

## 8. Data model

No new tables required — reuse:

- **Persona** = `agent_profiles` row (add a `writeAccess`/`noNamePrefix` to
  `PersonaConfig` JSON, no migration).
- **Group** = `teams` + `team_members` (roster), plus a small **group config** blob for
  `{mode: round-robin|parallel, scope: web-only|repo-backed, defaultWorktree, …}` — store
  on the `teams` row (JSON column or a new migration if a column is cleaner).
- **A running discussion** = a `channels` row + persona sessions joined via
  `channel_members`; the timeline lives in `messages`.

Lifecycle: starting a discussion creates the channel + N persona sessions (+ shared
worktree if repo-backed); ending it dissolves the channel and tears down the sessions +
worktree (`DissolveChannel`, with the keep-history variant if the user wants to keep the
transcript).

---

## 9. Backend work (concrete)

1. `runtime.CreateParams.DisallowedTools []string` (or `AllowedTools`) → `ConnectParams` →
   claude adapter `buildOptions` → `claudecli.WithDisallowedTools(...)`. *(read-only
   personas)*
2. `OnTurnComplete func(runtime.TurnCompletedEvent)` — change the stub at `session.go:340`
   and the call site at `event_pipeline.go:569`. *(turn-done + reply text)*
3. `CreateSessionParams.WorkDir` (bind to a pre-provisioned shared worktree) +
   provision-one-group-worktree helper. *(shared worktree)*
4. Skip wiring `recallFn` for discussion personas — `injectRecall` fires on every `Query`
   (`session.go:641`) and would prepend brain-recall noise into each persona turn. *(clean
   persona context)*
5. `discussion.Orchestrator`: round loop, Fisher–Yates ordering, cross-injection
   accumulator, parallel barrier, `SendChannelMessage` mirroring. Pure plumbing over
   `QuerySession` + `OnTurnComplete` + `StateIdle`.
6. Group config CRUD (mode/scope) on `teams`.
7. Start/stop discussion endpoints + WS events (reuse `channel.message` for the timeline;
   add a lightweight `discussion.round-*` status event if useful).
8. Retire/relocate the dead `AskTeammate` wiring.

## 10. Frontend work

**Chosen layout → "Roundtable" (reviewed 2026-06-28).** A left **roster rail** of persona
cards (avatar, role, model · effort, ✎ write badge, live status dot: idle / thinking /
writing) beside a focused transcript — it keeps the per-persona config and write access
always visible, which is what distinguishes this from plain chat. The "debate columns"
layout becomes a **parallel-mode view toggle**; the single-column "transcript" is the
narrow/mobile fallback. (Mockups: `ui-c-roundtable` / `ui-b-columns` / `ui-a-transcript`.)

- **Discussion-group composer** (replaces the Teams tab): pick a saved group *or* select
  personas ad-hoc → per-persona **write toggle** → group mode (sequential/parallel) →
  scope (web-only / repo-backed + project) → **auto-commit toggle** → prompt → **Start**.
  (Mockup: `ui-composer`.)
- **Live discussion panel**: the roster rail + reuse `ChannelPanel` to render persona
  bubbles from the channel timeline; a composer to send the next round; a stop /
  keep-transcript control; per-persona status dots driven off `StateIdle`/`StateRunning`.
- Remove the 3-way "Teams" conflation; `SwarmComposer` stays for code swarms only.

---

## 11. Out of scope for v1 / future

- **Moderator agent** (picks who speaks next by relevance) — explicitly *not* v1 (user
  chose Odysseus-exact). Could be an opt-in mode later.
- **On-demand synthesis** — a "Conclude" button that runs a synthesizer over the
  transcript to produce a final answer. Cheap to add later; left out to keep v1 minimal.
- **Live per-persona token streaming in the combined panel** (option A in §4.4) — v1 ships
  final-text bubbles; streaming is an enhancement.
- **Relevance gating / "speak only if you have something to add"** — Odysseus doesn't have
  it; neither do we in v1.

## 12. Resolved decisions (2026-06-28)

1. **Group container session → no.** Cleanup is channel-membership based
   (`DissolveChannel`); no synthetic parent session.
2. **Keep-transcript on stop → keep by default.** Stopping does a dissolve-keep-history:
   persona sessions + shared worktree torn down, the channel + timeline archived
   read-only. (Sensible default; revisit if it clutters.)
3. **Writer commits → a toggle at discussion start.** "Auto-commit writer turns" is an
   opt-in checkbox on the start form; off = the writer commits explicitly.
4. **Web-only Bash → disallowed.** Web-only groups get no Bash tool (no CWD into the real
   repo); they web-search + reason only.
5. **Round / runaway guard → none in v1.** Ship without a round cap or per-turn
   `TaskBudget`; add a guard only if wall-clock proves a problem.
```
