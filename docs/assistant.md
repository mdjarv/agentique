# The assistant

A durable principal that owns the bigger picture. It remembers what you told
it, what happened while you were away, and what it proposed and you decided.
It can be talked to from a thread in the app, from a live voice call, and later
from a messaging gateway, and every one of those is the same assistant with the
same memory. It watches the sessions, keeps you in the loop, and within limits
it starts and drives work on its own.

Nothing here exists yet. This document settles the layering before code, so
that voice-specific things stay in `internal/voice` and everything else lands
one layer down, in `internal/assistant`, where a thread, a call, a gateway, or
something not yet imagined can attach.

Gated by `[experimental] assistant`, and surfaced through `features.assistant`
in `/api/health`, on the brain's precedent: an unmounted `/api/` path falls
through to the SPA and answers `text/html` with a 200, so a nav row added
without that check leads somewhere that looks alive and is not.

## Two products hide under "assistant", and this is the first

1. A supervisor for the agents. Its world is sessions, projects, machines,
   branches and allowances. Its job is to keep work moving and to spend the
   operator's attention well.
2. A personal assistant that happens to run agents. That is the OpenClaw and
   Hermes shape: an always-on gateway daemon, messaging channels, markdown
   memory, skills, a heartbeat that reads a checklist, and coding agents as one
   tool among many.

This is the first, built into the server. The second can sit on top of it
later as a client, and the parts of it worth borrowing (the heartbeat, standing
instructions as editable text, messaging as a surface) are taken here on their
own terms. The reasons it lives in the server rather than outside it:

- Every fact a good decision needs is server-internal and fails closed there:
  whether a turn is in flight (`Session.TurnInFlight`), whether a branch is safe
  to delete (`storage.Evaluate`), which delivery a message got
  (`session.MessageDelivery`), which allowance window is nearly spent
  (`usage.Collector.Document`). CLAUDE.md names "the client derived a second
  reading" as the failure mode again and again. An outside assistant reading
  the socket is that client.
- There are no scoped API credentials. An outside assistant is a full operator,
  and its bearer reaches every paired machine.
- Multi-machine forces it. The assistant is per primary and routes through the
  same facade the UI does.
- One memory. The brain already holds the operator's facts, and a second store
  beside it would drift.

## Shape

```
  Thread (/assistant)      Voice call            Gateway (later)     Notifier
  transport: renders       HEAD: Gemini Live     transport: text     digest, push
  the conversation,        brings its own        in, text out
  forwards text to         model; calls the
  the core's head          core's verbs
        │                       │                     │                  │
        └───────────────────────┴──────────┬──────────┴──────────────────┘
                                           │  Surface contract
                                ┌──────────▼───────────┐
                                │  internal/assistant   │
                                │                       │
                                │  head (Claude persona)│  the default model,
                                │  conversation         │  a project-less channel
                                │  journal              │  episodic, weeks
                                │  brain                │  semantic, durable
                                │  directory  (reads)   │  lifted from voice
                                │  verbs      (tiered)  │  closed table
                                │  proposals            │  the yes lives here
                                │  policies + budgets   │  standing instructions
                                │  follows + reports    │  lifted from voice
                                │  heartbeat            │  journal-gated
                                └──────────┬───────────┘
                                           │
        session.Service · schedule · usage · storage · brain/memory · eventbus
```

Two kinds of attachment, and the difference is who thinks.

A **head** brings its own model and calls the core's verbs directly. Voice is a
head, because speech has to be native: the realtime model owns voice activity,
barge-in and synthesis, and routing a call through a second model adds a hop
you can hear (`docs/voice.md` tried that and took it out). A **transport**
brings no model. It renders the shared conversation and forwards the operator's
text to the core's own head, a Claude persona. The thread is a transport. A
messaging gateway is a transport. The notifier is neither: it reads the journal
and writes a digest, and no model need run.

Two heads on one body is safe on one condition, which the voice design already
states: **every rule that matters lives in the tools, not in either prompt.**
Refusals, target checks, rate limits, tier gates and budgets are enforced in
`internal/assistant`, where both heads hit them. The prompts differ per surface
on purpose, because a prompt tuned for speech must never read a list of twelve
sessions aloud, and a prompt tuned for a screen has no read-back to give.

## What moves out of voice, and what stays

`docs/voice.md` already says which state is per call: "per-call state is only
the focus", and everything else is per session. That everything-else is the
assistant's state, and it moves.

| Moves to `internal/assistant` | Today | Stays in `internal/voice` |
|---|---|---|
| The directory (orientation, list, brief, summarize, list projects, create) | `voice.Directory`, implemented by `server.voiceDirectory` | The engine seam, the Gemini backend, the echo engine |
| The follow set and the briefed flag | `call.follows`, `followState{release, inFlight, name, briefed}`, process memory only | The focus, and `judgeTarget`: the read-back check exists because the target is invisible from a car |
| The report registry, its shape and its rate | `voice.Registry`, `Report{Kind, Headline}`, burst 3 and one back every 3 minutes | `hang_up`, `stay_on_line`, the idle billing guard, ringback, tones, health, the worklets |
| The runtime notices (blocked, died, finished) | `voiceTurnWatcher` on `Manager.AddTurnEndListener` | The `world` and `viewing` frames as transport, and the caption strip, the dock, the driving screen |
| The reporting instruction appended to a dispatched prompt | `ReportingInstructions`, appended in `voiceDispatcher.Dispatch` when someone listens | The speech-shaped system instruction (`SystemInstruction`), minus the sections that were really the core's |
| The dispatcher's delivery mapping | `voice.Delivery` mirrors `session.MessageDelivery` | The `Live` button, `voice-store`, everything in `lib/voice/` |
| Character and verbosity from the persona settings | `voice_settings` row, read per call | The voice name |

The session-facing MCP tool `VoiceReport` becomes `AssistantReport`. It is
in-process and its name reaches a session only through the instruction that
teaches it, so the rename needs no wire transition. The instruction is appended
whenever the assistant follows the run, and the assistant follows everything it
dispatches and anything it is asked to follow, so a run started from the
composer still carries nothing unless someone asks.

The call keeps a `Follower` and keeps speaking reports with the session named.
What changes is that the registry outlives the call: a report that arrives
between calls lands in the journal, and the next call's greeting can say it.

## The core's state

Five stores, and each has one lifetime and one writer.

**The conversation is a channel.** One project-less channel per assistant,
`sender_type` `user` and `persona`, the `messages` table as the source of truth
the way it is for every channel timeline. A `channels.kind` column (`''` for an
ordinary channel, `assistant` for this one) keeps it out of channel lists. A
voice call mirrors its turns into the same channel with
`metadata.surface = "voice"` and the call id, so "what did I agree to on the
drive" is in the thread, and the thread's history is what greets the next call.
Project-less channels already fan out on the global topic, so the thread's
pushes need no new routing.

**The head's own context is a cache of the conversation, never the memory.**
A Gemini call cannot resume a Claude CLI transcript, so the shared history has
to be ours regardless, and keeping a CLI-resumed transcript beside it would be
two conversation memories that drift. So the Claude persona is started the way
`sessionlessPersona` is today, through `runtime.Manager.StartPersonaRuntime`,
with a preamble composed from the stores: the pinned facts and the memory
index, the journal since the last turn, and the tail of the conversation. It stays up across turns,
because a CLI's first connect costs 30 to 40 seconds, and it is idle-evicted
like any session. A restart reaps it, on the rule that a restart is not a
pause, and the next turn starts a fresh one from the same stores with nothing
lost but the turn. No `sessions` row, no worktree, no project, and so no
migration making `sessions.project_id` nullable. The price is that a channel
message is whole where a session's turn streams, so the head's in-progress
reply rides a global `assistant.delta` push and is stored on completion.

**The journal is episodic.** `assistant_journal`, append-only, one row per
thing that happened: a session finished, failed or blocked (from the turn-end
listener, ordered blocked before failed before finished as the notices already
are), a branch merged or a session archived (from the `session.state` push on
the bus), a loop auto-paused, a report received, a prompt dispatched, a
proposal made and decided. Each row names its subjects (session, project,
machine), carries a one-line summary and a payload, and is `untrusted` when
its text was agent-written. A `notable` flag, set by the operator on a message
or by the assistant on an entry, marks what consolidation should look at.
Rows compact by age into a summary row, weeks not months. The journal answers
"what happened"; the brain answers "what is true".

**The brain is the assistant's long-term memory and nothing else's.** The
brain was built for the wrong consumer. A coding agent has the repo, CLAUDE.md
and git history in front of it, and facts injected beside those competed with
them, arrived without provenance, and were judged by an outcome signal a
session never gives cleanly. It was disabled for adding noise. The facts it
holds are about the operator's world, and that is what the assistant needs on
every turn. `internal/memory` is the liftable core and its scope strings are
opaque so "another consumer can map its own concepts"; `internal/brain` is the
policy layer, and the policy is what changes:

- **Knowledge is pulled; only news is pushed.** The noise was push: a
  retriever guessing relevance from keyword or cosine overlap and injecting
  its guess into every turn, judged by a model that had not asked. Pushing
  into a long-lived assistant is the same fault one layer up. So the head's
  preamble carries the pinned facts (what the operator said to always keep in
  mind) and an **index**: one line per area or label with a count, on the
  precedent of a memory directory whose index is loaded and whose bodies are
  read on demand. Facts arrive in a turn only through the `recall` verb,
  which the head calls because it knows what it is trying to do, and the
  instruction says when: before answering about a project, a decision, or a
  preference. `RecallBlock` becomes the implementation behind that verb, and
  the per-turn delta machinery and its seen-set are not used by this
  consumer. The one push is `SinceLast`, and it is journal, not brain: what
  happened since this surface last looked, once, because news is news.
  Corroboration improves for free: a fact the head pulled and then used is a
  real signal, where an injected fact that was maybe read was not.
- Session-side recall, the `MemoryAdd` family of session tools and
  `LearnFromTranscript` go. `registerMemoryTools` gets nil. A session that
  learns something says so through `AssistantReport`, which is untrusted text
  in the journal. Sessions never write facts.
- Memory reaches a coding session only through the prompt the assistant
  drafts, in text the operator can read and edit before it goes. Visible,
  never injected.
- Captures come from the conversation and from notable journal entries, never
  from transcripts. Consolidation stays as it is. Every fact carries a
  provenance in `memory.Source`: the operator said it, the assistant concluded
  it from server facts, or a session reported it and the operator confirmed.
- The outcome signal becomes conversational. "Yes" and "no, we changed that"
  are in-band, which is the human confirmation the design ranks above
  corroboration and could never get from a session.
- The Brain page becomes the assistant's memory page, review queue and flags
  where they are. `[brain] enabled` without `[assistant] enabled` logs a line
  naming the switch and builds nothing.

**Proposals are where the yes lives.** `assistant_proposals`: a verb, a target,
arguments, the rationale, the server facts it was judged on, and a status
(`open`, `accepted`, `declined`, `expired`, `stale`). Accepting re-evaluates
against the live facts before acting, on the storage page's precedent: the
server re-plans and intersects with the request, so a stale card narrows what
happens and never widens it. A proposal renders as a card in the thread, sits
in the deck's Needs-you band wearing the waiting-on-you triangle, and is
spoken on a call with the read-back rule. The row records which surface
decided it.

**Policies are standing instructions, and follows are the watch list.**
`assistant_policies` holds markdown text the operator writes, enabled or not,
with a budget: at most so many sessions in flight that this policy started,
at most so many per day. The heartbeat reads them. `assistant_follows` is the
old follow set made durable, one row per session with `since`, `briefed` and
who asked.

## Verbs, and the tier is on the verb

A closed table in code, `assistant.Verb`, each with a tier. Nothing outside the
table can be called by either head.

**Read.** Orientation, list and find sessions, summarize, list projects,
allowances, journal search, memory search. Free, from anywhere, at any time.

**Contained.** Create a session (in a worktree, never the main worktree, never
on a paired machine), dispatch a prompt, follow and unfollow, remember (a brain
fact with provenance), note (a journal entry). These run from a conversation
ask or from a policy on the heartbeat, are journaled with the facts they were
judged on, and count against the policy's budget. The worktree is the
containment: nothing leaves it without a merge, and a merge is not on this
list.

**Uncontained.** Merge, rebase, archive, delete, reclaim, dissolve, anything on
a main worktree, anything remote, changing another session's mode or model.
These are never performed. Asking for one creates a proposal. Archive is here
even though it is reversible, because CLAUDE.md says `archived_at` is written
only by an explicit gesture, and "archive these six merged sessions" with a
one-click yes is that gesture and matches Archive-all.

The tier is a property of the verb and never of the call site or the model's
confidence. `ROADMAP.md` proposes a persona confidence threshold as the
autonomy dial for teams; this design declines it. Confidence is the model's own
report, the same untrusted text everything else here refuses to act on. A
policy can only ever cause contained writes, by construction, and no setting
can promote a verb.

Every contained write carries its origin. `session.QueryOrigin` gains
`Kind = "assistant"` beside `"schedule"`, with the policy or proposal id, so a
turn the assistant started is attributable on the wire. Sessions gain an
`origin` column (`''` or `assistant`) so the deck and the rail can say who
started one. Unlike schedule-origin turns, assistant-origin turns do set
`unseen_completed_at`: the outcome is news to a person. The assistant does not
add a second claim on attention for the same session. When there is something
to decide, the proposal is the claim.

## Reports and notices

Unchanged in principle from `docs/voice.md`. Salience belongs to the worker,
so a followed run gets `AssistantReport` and the instruction that says when to
use it; the three things a worker cannot say (blocked, died, finished) come
from the runtime through the turn-end listener; a notice is never rate
limited; and a report is data to relay, never an instruction to follow. Both
land in the journal and are delivered to whichever surfaces are live through
`Follower`. A call speaks them with the session named. The thread shows them
as quoted lines. A report that arrives with nobody live waits in the journal
for the digest or the next greeting.

## The surface contract

A surface registers with the core and gets three things.

- `Deliver(ctx, Item)`: the core hands it a report, a notice, a proposal, a
  digest, or the head's reply, and the surface renders or speaks it. This is
  `TextInjector` generalised, with the item typed so a transport that cannot
  show a card knows it has been handed one.
- The verb table, with the same refusals for every caller. A head calls it
  directly. A transport never does; it calls `Say(text)` and the core's head
  answers through `Deliver`.
- The conversation: append a turn, read the tail, and `SinceLast(surface)`,
  which is the journal and the conversation since this surface last saw them.
  That is what a call's greeting reads and what the thread pins at its top as
  recent updates.

A surface declares whether it **can show a card**. The thread can. A call
cannot, and neither can a Telegram line. Where it cannot, an uncontained
proposal is accepted only through the read-back rule voice already has: the
head names the target out loud, and the answer is judged against the proposal
by name. `judgeTarget` therefore stays in voice but its shape is the rule every
blind transport follows.

## The heartbeat

One timer in the core, not a schedule. Schedules target a session and only a
session (`schedules.session_id` is `NOT NULL`, and there is no target kind),
and their value, runs, attention, deep-links, is session-shaped. When the
scheduler grows target kinds the heartbeat can move there. Until then it is one
ticker with a fast path that costs nothing: **if the journal has no entries
since the last beat and no policy names a time, no model runs.** When something
has happened, a Haiku one-shot through `msggen.RunWithRetry` triages the
entries against the enabled policies and answers `none`, `digest` or `act`, and
only `act` wakes the head. OpenClaw's default heartbeat is thirty minutes; this
one can afford to be shorter because the common tick is a row count.

The digest is the notifier's job and is deterministic first: the journal since
the last digest, grouped by the Needs-you ranking (`lib/session/priority.ts`
is the client's copy of the same order the notices use), rendered as a
conversation message at a configured time or on demand. A one-line narration
from the head is optional and off by default. Costs never appear in it;
allowances do.

## Surfaces

**The thread** is `/assistant`, a place where work lives, so it belongs in the
sidebar's ⋯ menu beside channels and loops. It renders the channel, proposal
cards from their rows, and a recent-updates strip from `SinceLast`. Its
composer is the ordinary composer sending to the head. It renders on the
phone, in the same shape.

**The call** is unchanged from outside. Inside, its directory, registry and
dispatcher come from the core, it mirrors turns into the conversation, and its
greeting reads `SinceLast(voice)`. Live voice still requires auto mode, and a
`blocked` run still means "this needs a screen", which is now a proposal-shaped
item the thread can act on.

**The deck** shows open proposals in the Needs-you band, ranked with approvals.

**Later**: a messaging gateway as a transport, push notifications from the
notifier, and the personal-assistant product as a client on top.

## Multi-machine

One assistant per primary. Remote sessions are listed and followed from the
browser-fed world snapshot, as the call does today, and the snapshot is a view:
it can make the assistant say things, never do things. Create and dispatch on
a remote refuse naming the machine, because dispatch and the report registry
are local and a remote run would report into nothing.

The structural fix is a server-to-server subscription from the primary to each
paired machine, which the primary is already positioned for: it holds
`machines.token` and dials remotes for revoke. That is a new subsystem and is
named here, not built. Until it exists, machine reachability is not a server
fact, so the journal carries no machine-away entries.

## Security

The assistant is an agent and is not a trusted principal. It reads untrusted
text on every turn: reports, summaries, anything a session derived from a repo.
Containment is the tier table, the budgets, the journal with the facts each
action was judged on, and a yes given on a surface that shows the card or reads
the target back. A hostile report can make the assistant say something wrong,
and can cause a contained write within a policy's budget: a session in a
worktree with a bad prompt, visibly assistant-origin, spending allowance. It
cannot merge, delete, archive, reach a main worktree or reach a paired machine.
That residual is stated so nobody widens a tier to save a click.

## Phasing

- **M1, the core and the thread.** `internal/assistant` with the directory,
  the verb table at read and contained, the conversation channel, the journal,
  reports and notices, the surface contract, and the head. `/assistant` as a
  transport. Voice re-pointed at the core with no change visible from a call.
- **M2, memory.** Session-side recall and memory tools off, the index and
  pinned set in the head's preamble, `recall` as a verb, provenance on facts,
  captures from conversation and notable entries, the Brain page moved under
  the assistant.
- **M3, proposals.** The uncontained tier as proposals, cards in the thread,
  the deck's band, spoken acceptance on a call, the digest.
- **M4, autonomy.** The heartbeat with its journal gate and Haiku triage,
  policies with budgets, `origin` on sessions and turns.
- **Later.** A gateway transport. Server-to-server follow for remote sessions.
  The scheduler absorbing the heartbeat once it has target kinds.

## Decided

Four questions were open when this document was first written. They are
settled here so that a build does not have to guess.

**The head runs the service's default model, and triage runs Haiku.** No
family is hardcoded, on the model-catalog rule: the head takes whatever
`session.Service` would give a new session, overridable in the assistant's
settings row by family name, resolved through `providers.Catalog`. Effort is
the service default. Triage on the heartbeat is a Haiku one-shot through
`msggen`, on the auto-namer's precedent, and if policies grow past what one
shot can judge the answer is a second shot per policy, never the head.

**The conversation is a channel, not a `sessions` row.** A `sessions` row
would buy resume, streaming and a transcript for free, at the cost of a
nullable `sessions.project_id` and a `kind` every list query must filter on.
It is declined because the memory must be ours either way: a Gemini call
cannot resume a Claude transcript, so a CLI-resumed history would be a second
conversation memory beside the channel. The head's context is a cache.

**Machine reachability is not a server fact yet, and the journal does not
pretend.** No machine entries until the primary subscribes to its peers.

**The journal compacts by day.** A row older than fourteen days folds into one
`day_summary` row for its day, written by the same Haiku one-shot, and the raw
rows go. Notable rows are exempt and stay whole. Summary rows are kept for
ninety days. Reports keep their `untrusted` mark through compaction: a summary
of untrusted text is untrusted text.

## The M1 contract

The names below are the ones a build uses. Anything not named here is the
builder's call, and should be recorded in this document when it is made.

- Package `internal/assistant`, wired in `server.go` inside a block gated by
  `[experimental] assistant`, on the brain's precedent: off means unbuilt,
  and `features.assistant` in `/api/health` is false.
- Migration: `channels.kind TEXT NOT NULL DEFAULT ''` (`''` or `assistant`);
  `assistant_state` (single row `id = 1`: `channel_id`, `model`,
  `last_heartbeat_at`, `last_digest_at`, `created_at`, `updated_at`);
  `assistant_journal` (`id`, `at`, `kind`, `session_id`, `project_id`,
  `summary`, `payload` JSON, `untrusted` INTEGER, `notable` INTEGER,
  `seen_by` JSON, `created_at`); `assistant_follows` (`session_id` PK,
  `since`, `briefed` INTEGER, `source`). Timestamps are UTC RFC3339 seconds,
  as the scheduler's are. SQL stays ASCII.
- Journal kinds, a closed set: `session_finished`, `session_failed`,
  `session_blocked`, `session_merged`, `session_archived`, `loop_paused`,
  `report`, `dispatched`, `session_created`, `day_summary`. Proposals add
  theirs in M3.
- The core's Go surface: `assistant.Service` with `Say(ctx, surface, text)`,
  `History(ctx, before, limit)`, `SinceLast(ctx, surface)`, `Journal(ctx,
  since, limit)`, `Follow`/`Unfollow`, and `Verbs()` returning the closed
  table. `assistant.Surface` is the contract above. `assistant.Registry` is
  `voice.Registry` moved, `Follower` unchanged. The directory interface moves
  with its methods and its nil-is-valid rule; `server.voiceDirectory` becomes
  `server.assistantDirectory` and voice receives it through the core.
- Session-facing MCP tool `AssistantReport`, same schema as `VoiceReport`
  had, registered from a fourth group in `mcphttp` behind a one-method
  interface. `VoiceReport` is not kept.
- WS ops, all on the `assistant.*` prefix: `assistant.say` (mutation),
  `assistant.history` and `assistant.journal` (reads, on the concurrent lane).
  Pushes on the global topic: `assistant.message` (a stored message),
  `assistant.delta` (the head's in-progress text), `assistant.journal` (a new
  entry). Wire fields optional, generated Zod schemas through `just typegen`.
- Frontend: route `/assistant`, a row in the sidebar's ⋯ menu shown only when
  `features.assistant` is true, a page that renders the channel's messages,
  the head's streaming reply, a recent-updates strip from `SinceLast`, and
  the ordinary composer sending `assistant.say`. Mobile renders the same page.
  State in a `assistant-store` with stable selectors.
- Voice: `internal/voice` imports `internal/assistant` for the directory, the
  registry, the dispatcher's delivery mapping and the reporting instruction,
  and mirrors each call's turns into the conversation with
  `metadata.surface = "voice"`. Its greeting reads `SinceLast("voice")`.
  Nothing visible from a call changes.
