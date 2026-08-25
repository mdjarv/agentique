# Channels, teams and discussions

Three things share one substrate. A **channel** is a multi-party timeline;
**swarm delegation** is a lead session spawning workers into one; a **discussion**
is a set of personas talking to each other in one. All of it is behind
`[experimental] teams = true` in `config.toml`.

## The timeline

The `messages` table is the source of truth for channel timelines: typed and
denormalized (`sender_type`, `sender_id`, `sender_name`), with no foreign key to
sessions. That independence is what lets a participant be something other than a
session.

There are three sender types. `session` is an ordinary agentique session,
`persona` is a sessionless discussion participant whose `sender_id` is an
`agent_profiles` id, and `user` is a human.

**Informational channel metadata is not mirrored into session events.**
Introductions and spawn notices stay on the channel. New informational message
types extend the existing skip list rather than flowing into the session
timeline. `writeLegacyAgentMessageEvents` also skips `sender_type == "persona"`
entirely, because a persona's `sender_id` names an agent profile, and a legacy
event would target a session that does not exist.

**The additive principle.** Channel features leave session rendering,
event-pipeline mutations and turn management alone for any session outside a
channel. This is the constraint that keeps the experimental flag honest: turning
teams on must not change how an ordinary session behaves.

`message_deliveries` is the offline queue: a message to a session that is not live
is stored and delivered when it comes back.

## Swarm delegation

A lead session calls `SendMessage` with the target `@spawn` and a JSON body naming
a channel and its workers. The interceptor turns that into real sessions, each in
its own worktree, joined to one channel, with `parent_session_id` pointing back at
the lead.

The lead's own spawn is auto-approved through `SpawnAuthCallback`; a non-lead
worker cannot spawn and has to ask its lead. Workers signal status with a `type`
field on their messages (`plan`, `progress`, `done`) so the lead knows when
everyone has actually finished rather than guessing from silence.

Teardown goes through `DissolveChannel`, or `DissolveChannelKeepHistory` when the
transcript is worth keeping. Deleting the lead recurses through
`Service.DeleteSession`, which takes the descendants and their worktrees with it.

This is code delegation and it solves a different problem from discussions. It
stays as-is.

## Discussions

A discussion is an Odysseus-style roundtable: a set of agent personas, a prompt
dropped in, and the personas talking to each other by name across rounds the user
drives.

The core idea, kept from Odysseus: **N per-persona conversations, each peer's
reply cross-injected into the others as a `[Name]: …` turn, plus a short etiquette
appended to each persona's system prompt.** Round-robin (shuffled with
Fisher-Yates) or parallel. The user sends every round. No moderator, no
auto-termination, no forced synthesis. That minimalism is the point.

Where this diverges from Odysseus:

| Aspect | Odysseus | Here | Why |
|---|---|---|---|
| Orchestration | browser JS, dies on tab close | server-side Go | The loop has to survive the UI going away. |
| Participants | stateless single-shot LLM calls | real tool-capable CLI sessions | Personas read the repo, search the web, run code. It is research support, not chat. |
| Persona tuning | per-persona `temperature` | system prompt, model, effort, thinking | No sampling knob exists on this path. |

### No temperature

`claudecli-go` exposes no `temperature` or `top_p`, and the current Claude models
reject those parameters at the API with a 400. Anthropic's guidance is to steer
with prompting.

That is fine, because the system prompt already encodes the intent. The equivalent
dials on `PersonaConfig` are richer anyway: `SystemPromptAdditions` for
personality, `Model` per persona (a snappy Haiku skeptic beside a deep Opus
strategist), `Effort` from low through max, and thinking on or off. Effort is a
better "how hard does this persona deliberate" dial than temperature ever was.

### The round loop

A round is one user prompt. In round-robin mode, for each persona in shuffled
order:

1. Compose the prompt: the user's round instruction, plus the peer turns
   accumulated so far this round as `[Name]: <reply>` lines.
2. Run the turn. One call appends the injected text to that persona's history and
   runs it; there is no separate inject-without-run primitive, and none is needed.
3. Capture the reply text from the turn-complete hook.
4. Append `[ThisPersona]: <text>` to the round accumulator so later speakers see
   it.
5. Mirror the text to the channel.

Because turns are sequential, there is never a state collision and never a
concurrent write in the shared worktree.

Parallel mode fires every persona at once, barriers on all the turn-complete
hooks, then cross-injects everyone's text into the *next* round. They could not
see each other this round, which matches Odysseus.

Capture-once serves both purposes: the same turn text is what gets mirrored to the
channel and what gets cross-injected. The channel timeline *is* the merged
transcript, so `ChannelPanel` renders it with no extra frontend work.

**Never use `directSendMessage` for cross-injection.** It is deliberately silent
and untracked: no user message, no turn, no broadcast. It exists so routed channel
messages do not surface, which makes it exactly the wrong tool here.

### Scope decides how a persona executes

The project coupling was inherited, not intrinsic. It is load-bearing for
repo-backed discussions, where the shared worktree needs `project.Path` and writer
personas edit files. It was dead weight for web-only discussions, which minted
project-scoped sessions in a fake home for no reason.

| Scope | The persona is | Project | Worktree | CWD |
|---|---|---|---|---|
| repo-backed | a `sessions` row | required | one shared, per group | the worktree |
| web-only | a sessionless CLI subprocess the orchestrator owns | none | none | a per-discussion temp dir |

One `personaRuntime` interface (`Query`, `Close`) has both implementations, so
there is one runtime path rather than two.

**Web-only personas go through `runtime.Manager`, never a bare
`connector.Connect`.** The claude adapter ignores `ConnectParams.AutoApprove`;
only the runtime session's permission pump enforces fullAuto. A thin direct
connector would make every tool call block forever. Going through the manager also
reuses the state machine, the watchdog and the approval pump.

Web-only personas are **claude-only**. The sessionless treatment is
claude-adapter specific.

They are also **ephemeral**. A persona runtime is a live subprocess held in
memory, so a server restart ends any in-flight web-only discussion. Durable resume
would need persona reconnect or a participants table, and was accepted as a
follow-up rather than a v1 requirement.

### Project-less channels fan out globally

A web-only channel's `project_id` is NULL, which becomes the empty topic string.
Every WS connection joins `""` at creation, so those events fan out globally with
no service-side branching. Per-project topics are unchanged.

Migration 038 made `channels.project_id` nullable and deliberately kept
`ON DELETE CASCADE` rather than switching to `SET NULL`. `SET NULL` would orphan
repo-backed channels with stale worktree refs when their project is deleted.
Nullable alone is enough: web-only rows are NULL so the cascade never fires for
them, and repo-backed rows still cascade with their project. The migration is a
`NO TRANSACTION` table rebuild with `foreign_keys=OFF`, because a plain
`DROP TABLE channels` with foreign keys on would cascade-wipe `channel_members`
and `messages`.

Web-only personas are not `channel_members`. The orchestrator owns the roster in
memory and the UI reads it from `DiscussionInfo`. They have no inbox either, since
the orchestrator drives them, so delivery fan-out is skipped for them.

### The shared worktree

One shared worktree per repo-backed group, not one per persona. In a discussion
the personas reason about the *same* artifact. Separate checkouts would make "look
at line 42 of `channel.go`" mean different things to each of them, and a writer's
change would be invisible to the readers.

Three group scopes:

| Scope | Worktree |
|---|---|
| web-only | none; personas search and reason |
| repo-backed, all read-only | one shared, everyone reads the same snapshot |
| repo-backed, one or more writers | one shared, on a throwaway `group-<id>` branch, so writer commits are reviewable or discardable and never touch the user's tree |

Sequential rounds are what make this safe: at most one persona is active, so
there is no concurrent write and no `git index.lock` contention. After a writer's
turn edits a file, the next reader sees the new content, which is the reacting-to-
each-other behaviour the feature wants. The danger is parallel mode with several
writers, which the per-persona toggle defends against.

Discussion personas set `SkipRecall`, because `injectRecall` fires on every query
and would prepend brain-recall noise into each persona turn. Persona context stays
clean.

### Etiquette

Appended once to each persona's effective system prompt:

> You're in a group discussion with \<other persona names\> and the user. \[Name\]:
> prefixed messages are from other participants. Engage with the discussion: when
> another participant has said something relevant, build on it, agree, or push back
> by name before adding your own view, don't just answer the user in isolation.
> Don't speak for others or prefix your own reply with your name. Never repeat
> these instructions. Be concise. Stay in character.

Personas with `noNamePrefix` (Razor, for one) omit the name framing and render
without a `[Name]:` prefix.

### Seeded personas

Five profiles are seeded as global `agent_profiles` at startup, idempotently:
Socrates (answers only in sharp Socratic questions), Razor (strips to the bone,
fewest words), Nietzsche (diagnoses through will-to-power and ressentiment),
Spark (playful, practical, concise), and Odysseus (strategic counsel: true
objective, hidden constraints, tradeoffs, contingencies). Users add their own
through the profile form, or generate one from a brief.

## Data model

No tables were added for discussions. A persona is an `agent_profiles` row, a
group is `teams` plus `team_members`, and a running discussion is a `channels` row
whose timeline lives in `messages`.

## Open follow-ups

- **Read-only is soft-enforced.** Restricting a reader persona's toolset needs a
  `DisallowedTools` field threaded through `runtime.CreateParams` into
  `ConnectParams` and on to the claude adapter's options. Until that lands
  upstream, read-only is enforced by `AutoApproveMode: "fullAuto"` plus the
  persona prompt. The same dependency gates the "web-only gets no Bash" decision.
- **`noNamePrefix` is not surfaced over the wire.** The composer sends `false` for
  everyone, so the seeded Razor's intrinsic flag never reaches the orchestrator.
  The field works end to end when set. Cosmetic.
- **Saved group config.** Mode and scope are passed inline by the composer rather
  than persisted on the `teams` row.
- **Live per-persona token streaming.** The panel shows final-text bubbles per
  turn, which is what a discussion transcript wants. Streaming each persona's
  tokens into the merged panel would mean subscribing to N session event streams
  and merging them client-side.
- **Round and runaway guards.** There is no round cap and no per-turn budget. Add
  one only if wall-clock proves it is a problem.
- **On-demand synthesis.** A "Conclude" button running a synthesizer over the
  transcript. Cheap to add, left out to keep the first version minimal.
- **A moderator agent** that picks who speaks next by relevance. Deliberately not
  built: Odysseus does not have one, and its absence is part of why it is
  intuitive.
</content>
