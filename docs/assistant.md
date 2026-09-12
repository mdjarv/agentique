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
  where they are, reached from the thread. `[brain] enabled` stays the switch
  for the store, its routes and its page; the assistant is its only reader,
  so `[brain] enabled` with the assistant off logs a line at boot saying that
  memory is stored and browsable and nothing recalls it. Nothing refuses to
  boot over it.

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

**The thread** is `/assistant`, opened from the rail's row above the footer,
which is the assistant's row: the slot the Live row held, on the argument that
placed it (always-true state that is the operator's, not the machine's). The
orb with an empty core is the assistant's mark and a call is that mark awake;
the row wears the unread notch and nothing else; `⌥A` opens it. The thread's
header carries the call button, because a call is one way of talking to the
thing you are looking at, and the composer's phone stays in sessions because
it is about the next message. It renders the channel, proposal cards from
their rows, and a recent-updates strip. Its composer is the ordinary composer
sending to the head. It renders on the phone, in the same shape. (Design
round 1, 2026-09-12, option B; D — the thread as the landing page — is the
direction once proposals exist.)

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
- Frontend: route `/assistant`, the rail's row above the footer when
  `features.assistant` is true (it was a ⋯ menu row until design round 1
  moved it), a page that renders the channel's messages,
  the head's streaming reply, a recent-updates strip from `SinceLast`, and
  the ordinary composer sending `assistant.say`. Mobile renders the same page.
  State in a `assistant-store` with stable selectors.
- Voice: `internal/voice` imports `internal/assistant` for the directory, the
  registry, the dispatcher's delivery mapping and the reporting instruction,
  and mirrors each call's turns into the conversation with
  `metadata.surface = "voice"`. Its greeting reads `SinceLast("voice")`.
  Nothing visible from a call changes.

## The M2 contract

Memory. The brain becomes the assistant's long-term memory and stops reaching
coding sessions. The names below are the ones a build uses; anything not
named is the builder's call, recorded under Build notes.

**What goes.** Session-side recall and session-side memory writing are
removed, not switched off: the `if cfg.BrainRecall` block in `server.go`, the
`Manager.Memory*` hooks and their composers (`memoryPreamble`,
`memoryContract`, `memoryRecallPreamble`, `wireRecall`), `Session.recallFn`,
`recalledIDs`, `SetRecallFn`, `injectRecall`, the `SkipRecall` parameter on
`CreateParams` and `CreateSessionParams`, `brain.RecallPreamble`, the
session-end learning path (`SetOnSessionEnd`, `HandleSessionComplete`,
`Manager.OnSessionComplete`, `LearnFromTranscript`,
`ApplyOutcomesFromTranscript`, the outcome judge, the brain job queue and its
`brain_jobs` use — the table stays; a migration is not worth an empty one),
the session-facing memory MCP tools (`registerMemoryTools`, `MemoryStore`,
`brain.MCPAdapter`, and the `mem` parameter of `mcphttp.NewHandler`), and the
tests that covered them (`recall_inject_test.go`, `recall_wiring_test.go`, the
MCP adapter tests in `brain_test.go`, `outcome_test.go`,
`capture_ingest_test.go` where it exercises transcripts). The `[brain]` keys
`recall`, `learn-model`, `outcome-model` and `retry-max`, and
`AGENTIQUE_BRAIN_RECALL`, become no-ops: a config that carries one logs a
line at boot naming the key and why, and never refuses to boot. The
`<brain>` envelope renderer (`BrainCard`, the peel in `UserMessage`) stays,
because old transcripts carry it.

**What the assistant gets.** A `Memory` collaborator on `assistant.Service`
(`WithMemory`), an interface in the assistant package implemented in the
server package over `brain.Service`:

- `Index(ctx) ([]IndexLine, error)`: one line per area with its size and
  scopes, from `memory.PreviewAreas`, plus one line per scope with its count.
  This is what the head's preamble carries, under "What you remember": the
  pinned set and the index, never the bodies.
- `Pinned(ctx) ([]Fact, error)`: pinned records across every scope.
- `Search(ctx, query string, k int) ([]Fact, error)`: `brain.Recall` over
  every scope, `k` clamped server-side.
- `Remember(ctx, text, category, provenance) (Fact, error)`: `brain.Add` into
  the global scope, or a project scope when the head names a project.
- `Confirm(ctx, id) error` and `Flag(ctx, id, reason) error`: the
  conversational outcome signal, over `brain.Confirm` and `brain.Flag`.
- `Capture(ctx, scope, text, source) error`: a staged capture for
  consolidation to judge.

`Fact` is `{ID, Text, Category, Source, Scope, Pinned, Confidence}`. The head's
preamble renders the index and the pinned set and says, in words, that
everything else is behind `recall` and when to reach for it: before answering
about a project, a decision or a preference.

**Verbs.** Read: `recall(query)`. Contained: `remember(text, category,
provenance)` where `provenance` is the enum `operator` (the operator said it,
`memory.SourceHuman`) or `assistant` (the head concluded it from server facts,
`memory.SourceAgent`), and `category` is the closed `memory.Category` set;
`confirm_memory(id)`; `flag_memory(id, reason)`. The instruction tells the
head to `remember` only what the operator stated or confirmed, and to
`confirm_memory` when the operator agrees with a recalled fact and
`flag_memory` when they contradict it. Nothing else writes facts. Every write
is journaled (`note`-shaped, notable) so the conversation carries a record.

**Provenance.** `memory.Source` gains `SourceReported = "reported"`: a capture
whose text came from a session's report or an untrusted journal entry. A
notable journal entry becomes a capture when it is written (`note` verb, and
any entry marked notable), with `SourceReported` when the entry is untrusted
and `SourceCapture` otherwise, into the project scope when the entry has one.
No background extraction over the conversation: what the operator says is
remembered through the visible `remember` call or not at all.

**Frontend.** The Brain page moves under the assistant: route
`/assistant/memory` renders it with the title "Memory" and the assistant's
header; `/brain` stays as a redirect. The thread's header gains a Memory
control (the Brain glyph, when `features.brain` is on) beside the call. The
⋯ menu's Brain row goes, on the one-home rule. The brain flare moves to the
assistant's row: on `brain.updated` the orb's track pulses once, as the menu
trigger used to. `features.brain` is unchanged and still gates the page.

**Docs.** `docs/brain.md`'s "Recall" and "Agent surface" sections say what is
true now; CLAUDE.md's brain paragraph replaces per-turn delta recall with the
pull rule; `README.md`'s `[brain]` block drops the removed keys.

## Build notes

**The session.state observer journals transitions against a primed baseline,
never states.** Found in the real app on the first boot with the flag on: the
thread opened on 111 lines of "archived", one per session archived in the
last year, because a state push is a whole snapshot and the observer's dedupe
asked "is this fact journaled yet" rather than "did this fact just change".
`PrimeSessionStates` loads every session's `worktree_merged` and
`archived_at` into the observer's baseline, and the wiring calls it
immediately before subscribing to the bus, which is the only exact order: a
push announces a write that has already happened, so a baseline read after
subscribing can already hold the transition it is about to be told of. The
observer also primes itself lazily if it was never primed, and writes nothing
until it has; that fallback can miss the one transition that arrived first
and cannot flood, which is the fail-closed direction. An unarchive writes
nothing and moves the baseline, so archiving the same session again is news
again. The count query the dedupe used is gone with it. The entries carry no
summary, because every renderer already says "was archived" from the kind and
the strip printed "archived — archived" on every row.

Calls the M1 contract left open, recorded as they were made. Each says what was
decided and why, so a later round can disagree with the reason rather than
rediscover the problem. The core package is `backend/internal/assistant`.

**The journal has an eleventh kind, `note`.** The contract's closed set has no
kind for its own `note` verb, and none of the ten fits: `report` is untrusted by
construction, where a note is the assistant's own sentence, and marking it
untrusted would teach every surface to quote the operator's own words back at
them. So the set is ten plus `note`. It is the one deviation from a binding list
and the cheapest to reverse.

**`assistant_state` has an eighth column, `surface_marks`.** `SinceLast` has two
halves and they need two mechanisms. The journal half is per ROW (`seen_by`), so
an entry written while nobody was looking is still unseen when a surface
arrives. The conversation half has no row to stamp — a message nobody read is
just a message — and a surface that has never looked has no journal row to carry
its mark either. `surface_marks` is a JSON object of surface name to the RFC3339
second it last looked, written only by `SinceLast`. A fourth table for one
string per surface was the alternative.

The mark is whole seconds and a message's stamp is fractional, so a message
written inside the same second as a look reads as already seen. That is the
conservative direction: showing news twice is a nuisance, where re-delivering
what the reader was looking at is a bug.

**The conversation's messages carry a nanosecond `created_at`, passed in.**
`messages.created_at` defaults to milliseconds, and an ask and its reply can
land inside one. The only other tiebreak in the row is a uuid, so two turns in
one millisecond read back in no order — which is also what made the history
cursor lossy. `InsertAssistantMessage` writes a fixed-width nanosecond stamp
(lexicographic order is chronological order), and paging compares the pair
`(created_at, id)` as a row value. Nothing else writes to an assistant channel,
so the sharper format is consistent inside the timeline that reads it.
`History` therefore returns a `Page` with an opaque `Before` cursor rather than
a bare slice, and a malformed cursor is an error — a client that silently gets
the newest page back loops without ever reaching the start.

**`sqlc` does not rewrite `sqlc.arg()` inside an upsert's `DO UPDATE` clause.**
It copies the text through, and `sqlc.arg(surface)` reaching SQLite is a
runtime error in a statement whose failure the caller only logs — which is
exactly how it was found. `SetAssistantSurfaceMark` names its parameters in the
`VALUES` clause and refers to them positionally (`?1`, `?2`) in `DO UPDATE`;
mixing the two is what produces both readable parameter names and correct SQL.

**Moved out of voice, with the aliases that keep the tree building.** The
directory (and `SessionRow`, `ProjectRow`, the filters, the attention ranks,
`UnknownModelError`), the registry, `Follower`, `Report`, `Notice`, `Delivery`,
`Dispatcher`, `ReportingInstructions` and the spoken-name matcher are now in
`internal/assistant`, with their tests. A few names had to be exported for
voice to keep using them: `SpokenList`, `DisplayFor`, `NormalizeFilter`,
`ProjectRow.DisplayName`, `SessionRow.HasAttention`, `StateRunning`,
`NoticeKind.Priority`, `NoticeKind.EndsWork`, and from the matcher
`NormalizeTokens`, `ScoreRow` and `NamesRow` — the last three because voice's
`judgeTarget` scores a focused row against the same rule `find_session` uses,
and two matchers is how a session comes back found by one surface and
unrecognised by the next. `namesRow` moved out of `target.go` for that reason.

`internal/voice/assistant.go` is a TRANSITIONAL alias file, and the wiring that
re-points `internal/server` at the core should delete it. It exists because
`internal/server` names `voice.SessionRow`, `voice.Directory`,
`voice.Registry`, `voice.Notice`, `voice.Delivery` and
`voice.ReportingInstructions` in twenty-odd places, and moving the types
without it would leave the backend not compiling for whoever landed next. They
are Go type aliases, so `voice.SessionRow` and `assistant.SessionRow` are one
type and the server can be re-pointed a file at a time.

`ReportingInstructions` keeps its wording, which still says "a person is on a
live voice call". Rewording it is a prompt change that wants its own
verification, and today every follower is a call; the M1 change is that the
report is now also kept in the journal, which is what the tool's answer says
when nobody is following.

**The collaborators are interfaces, and every one of them is optional.**
`New(store, opts...)` takes the store and functional options
(`WithDirectory`, `WithDispatcher`, `WithHeadManager`, `WithAllowances`,
`WithTurnFacts`, `WithRegistry`, `WithBroadcaster`, `WithLogger`,
`WithClock`). Nothing starts in the constructor — no subprocess, no ticker, no
sweep — and a half-wired assistant degrades in words rather than refusing to
boot, which is the rule `Directory` already had. `Close()` stops what it holds;
the serve command calls it.

Two interfaces exist so the package never imports `internal/session`:

- `HeadManager.StartHead(ctx, HeadParams) (HeadRuntime, error)`, which the
  server implements over `Manager.StartPersonaRuntime`. `HeadParams` has no
  working directory and no id: the head has no project and no worktree, so
  where its subprocess runs is the manager's business.
- `TurnFacts`, which is `voiceTurnWatcher`'s reads (`PendingHumanInput`, plus a
  `TurnOutcome` carrying failed/project/name/closing-words) behind one
  interface.

`Allowances` is one method over `usage.Collector` — the collector answers from
cache and never touches the network, which is what makes it safe inside a verb.

**What the wiring still has to do.** The core is deliberately not self-wiring:

- `internal/session/persona_runtime.go` gained three additive fields on
  `PersonaRuntimeParams`: `ID` (so a caller can mint an MCP bearer for an id it
  chose — `StartPersonaRuntime` otherwise invents one and nothing can put a
  token in the store for it), `MCPConfigs`, and `OnText`, which opts the
  provider's partial messages in and forwards `AssistantTextDeltaEvent`. The
  head needs all three: **the persona subprocess needs an MCP token minted for
  its persona id**, and the bearer goes to the CLI as a 0600 file path, never
  inline.
- The verb table is exposed twice: `Verbs()` for building tool schemas
  (`Verb{Name, Tier, Description, Input []Param}`) and
  `ToolHandler(ctx, name, args) map[string]any` for running one. `ToolHandler`
  never returns an error — the caller is a model paused until it is answered —
  and it is the one place a refusal's `_reason` is logged and stripped.
- `Service.Report(sessionID, kind, headline) (string, error)` is
  signature-compatible with what `mcphttp` already expects, so the
  `AssistantReport` tool can be handed the service.
- `Service.OnTurnEnd(sessionID)` fits `Manager.AddTurnEndListener` directly.
- The `session.state` subscriber is three lines at the wiring site, because the
  payload is `session.GitSnapshot` and this package does not import it: map the
  snapshot onto `assistant.SessionState{SessionID, WorktreeMerged, ArchivedAt,
  Version}` and call `ObserveSessionState`. Repeats are deduped here, by an
  in-memory (session, kind) set with a count query behind it — the journal, not
  the memory, is the authority, so a restart does not double-write.
- `Service.LoopPaused` is there for the scheduler's `loop_paused` entries; the
  scheduler owns the rule that a paused loop stays paused.

**Verb decisions.** `create_session` takes an optional `prompt` and dispatches
it in the same call, on the same argument the call's version makes: the session
is created and the work is not, and half of that story is how somebody comes
back to an empty session believing it ran. Both halves are reported. Containment
is the `Directory` implementation's, which already forces `Worktree: true` and
local-only creation. `run_prompt` refuses a remote session naming the machine,
follows before it sends, and journals `dispatched`. `follow_session` refuses a
remote session too — the report registry is local, and the follow row
references a session on this machine. `summarize_session` WAITS for the
summary rather than delivering it later: that trade is about dead air on a
call, and a screen has none.

Uncontained verbs are in the table with their tier and no handler, so a head can
see that they exist and are not its to perform. `Invoke` answers
`*ProposalRequiredError` (unwrapping to `ErrProposalRequired`), which M3 turns
into a card.

**The head.** Started lazily by the first `Say`, kept up across turns because a
first connect costs thirty to forty seconds, evicted after 30 minutes idle by a
timer armed on use rather than in the constructor. A failed turn stops it, so
the next message starts a fresh one; a restart loses it and nothing else. Its
preamble is composed from the stores (orientation, `SinceLast("head")`, the last
20 conversation messages) and is a SCREEN instruction rather than voice's with
the speech removed — lists are allowed, there is no read-back, and the ask in
the conversation is the consent. `SurfaceHead` is not a surface; it is the
head's own seen-mark key, so news reaches an already-running head on the next
turn instead of only at start.

**Journal volume is not solved here.** Every turn end on every session writes
an entry, followed or not, because "what happened while you were away" is the
journal's whole job and the assistant did not start most of the work it should
know about. On a busy machine that is a row per turn per session; compaction
(M4) is what answers it, and until then the reads are bounded (200 an entry
page, 50 for a look).

**Channel lists exclude the conversation in SQL.** `ListChannelsByProject` gained
`AND kind = ''`, so `Service.ListChannels` and the `channel.list` WS op need no
change. A project-less channel was never returned by that query anyway (it
compares `project_id = ?`), but the filter is where the rule belongs rather than
in whichever caller remembers it.

**A turn-end signal is per PROCESS, not per session.** `runtime.Manager`'s
`WithOnTurnEnd` hook fires for every CLI subprocess it owns, so a discussion
persona's turn and **the assistant's own head's turn** both reach
`Manager.AddTurnEndListener`. Journaling one would be the assistant reporting on
itself, so `recordTurnEnd` asks the directory whether the id is a session this
machine owns and returns silently when it is not — the same test dispatch uses.
Worth knowing when anything else hangs off that listener.

**`Say` blocks; `assistant.say` should not.** The contract's `Say(ctx, surface,
text)` runs the turn and returns the reply, which is right for a caller that
wants the answer. A socket RPC is not that caller: `dispatchLoop` runs a
mutation serially in arrival order, so a blocking say holds that connection's
whole mutation lane for the length of a turn — seconds to minutes — with every
later mutation queued behind it. `SayAsync` stores and pushes the ask, starts
the turn on a BACKGROUND context (a request context is cancelled the moment the
RPC answers, which would kill the turn it just started), and returns the ask.
The thread renders from `assistant.delta` and `assistant.message` either way.

### The thread (frontend)

**`lib/assistant/wire.ts` is a transitional stand-in for the generated
schemas.** The page was built beside the core rather than after it, so the Zod
schemas for `Message`, `Delta`, `JournalEntry`, `Page` and `Update` are
hand-written there, mirroring the Go json tags, every field optional as the
wire rule requires. Nothing else in the tree imports those shapes from anywhere
else, so once `just typegen` emits them the file's body becomes one re-export
line. The exports a swap has to keep: `AssistantMessageSchema`,
`AssistantDeltaSchema`, `AssistantJournalEntrySchema`, `AssistantPageSchema`,
`AssistantUpdateSchema` and the matching types, plus
`ASSISTANT_JOURNAL_KINDS`/`AssistantJournalKind` and `ASSISTANT_ROLES`.

**The journal read is tolerated in two spellings, in one place.** The contract
names three WS ops and no fourth, while the Go surface has both `Journal(since,
limit)` (a slice) and `SinceLast(surface)` (an `Update` with `since` and
`lookedAt` marks). The thread wants the second — its strip is `SinceLast`'s
journal half — so `assistant.journal` is read as an `Update` whose array may
arrive as `journal` or as `entries`, through `journalEntriesOf` in
`lib/assistant/rpc.ts`. One reader, on `wire-compat`'s rule that an alias is
never spelled at a call site. If the op settles on one shape, delete the other
branch there.

**Everything the strip holds is unseen, so the strip does not filter.** The
server's answer to "what have I missed" is already scoped to what this surface
has not seen, and pushes that arrive afterwards are newer still. So the band
renders the store's whole journal, newest first, and disappears when it is
empty; `since` is kept for a later digest rather than used as a client-side
cutoff. Filtering client-side would have meant two answers to one question.

**A report is a quotation with a visible author, and so is any untrusted row.**
`report` is untrusted by construction and `untrusted` covers the rest, so
`JournalRow` draws either as a `blockquote` with "reported by <session>" under
it. The session's name comes from the list this client already holds and falls
back to the short id, which is what the rest of the app calls a session it
cannot name.

**`streaming` is the gate as well as the text.** One nullable string in
`assistant-store`: null means nothing is owed, a string means the head is
answering, and the composer's disabled state reads the same field the
transcript renders — so they cannot disagree. It is armed by `beginReply()` at
the send rather than by the first delta, because the seconds before the first
token are exactly when a second Enter would double-send, and it is released by
the stored `assistant.message` with `role: "assistant"`, by a failed send, or by
a reconnect (the turn that partial text belonged to is gone, and nothing will
replace it).

**The pushes are subscribed from the app shell, not the page**, on
`useBrainSubscriptions`' precedent: a reply streams for as long as a turn takes,
and navigating away mid-turn must not lose it. They are subscribed whether or
not `features.assistant` is set — the server pushes nothing when the assistant
is unbuilt, where gating on the flag would drop every push landing before
`/api/health` answers. `lib/assistant/apply-push.ts` does the applying and is
tested without a socket.

**The conversation is rendered here rather than by `ChannelPanel`.** That
component is a channel's rendering and says so: per-member colours, a member's
live status badge, a session id behind every sender. This conversation has two
speakers and no members, and reusing it would have meant inventing a membership.
What is reused is the thing that must look the same wherever you meet it, the
chat's `Markdown`. The composer, by contrast, IS the ordinary one:
`ComposerTextarea`, the same component a session's `MessageComposer` is built
on, with `projectId` empty — its autocomplete fetches already fail silently, so
`@file` and `/command` simply offer nothing where there is no project.
Attachments and dictation are not wired: the thread's head takes text.

**The store lives at `stores/assistant-store.ts`**, with the other stores,
rather than under `lib/assistant/` where the rest of the thread's plumbing sits.
Every other store in this app is in that directory and a reader looks there.

**The row is in the ⋯ menu and leads the group.** The menu lists places where
work lives and the assistant is the one that reports on all the others, so it
sits above Teams, Brain and Schedules, behind `features.assistant`. Reaching
`/assistant` on a server where the flag is off renders one line naming the
switch rather than a page with no ops behind it.

### Wiring notes: server, socket, MCP, and the voice re-point

A second round of calls, made while wiring the core into `internal/server`,
`internal/ws`, `internal/mcphttp` and `internal/config`. The M1 contract named
the ops, the tool and the gate; everything below is what naming them left open.

**`[experimental] assistant` gates the SERVICE, and the collaborators are built
for whoever sits on them.** The switch is declared beside `voice` and `teams`
(`AGENTIQUE_EXPERIMENTAL_ASSISTANT` overrides it) and reaches
`features.assistant` in `/api/health` **and** `capabilities.assistant` in the
machine descriptor — both, because a client talks to several servers at once and
reads whichever it gets to first. But the directory, the report registry, the
dispatcher and the runtime facts are built whenever **voice or the assistant** is
on, because a call is a head on this core and needs all four whether or not the
assistant itself is switched on. What the gate withholds is the service: no
conversation, no journal, no head, no MCP tools, and nothing written to the three
new tables. Off really does mean unbuilt.

**One turn-end listener, and a fallback that shares its ranking.** With the core
built, `Manager.AddTurnEndListener` gets `Service.OnTurnEnd` and nothing else: it
journals the fact and notifies the followers, and a live call is one of those
followers. With the assistant off and voice on, a call must not go deaf, so a
thin `voiceTurnWatcher` delivers the same fact to the same registry with nothing
written down. The ranking is **not** duplicated — `assistant.TurnNotice` is
exported for exactly this, and it is the only place blocked-before-failed-before-
finished is spelled. That is the one addition to the core this wiring needed, and
`journalKindFor` beside it is the second (the journal's vocabulary is not the
notice's).

The summariser's cache invalidation became **its own listener**. A cached summary
describes a session as it was before the turn that just ended, so it is stale the
moment one does — but dropping a map entry is not a fact about the turn, it is
bookkeeping that has to happen whether or not anybody is following. It was bolted
onto the notice watcher, where it only ran if voice was configured.

**`server.voiceDirectory` is `server.assistantDirectory`, and `voiceDispatcher`
is `assistantDispatcher`.** Both moved file (`assistant_directory.go`,
`assistant_dispatch.go`) with their tests and no behaviour change. The runtime
reads that were inside the old watcher are now `assistantTurnFacts`, which is
`assistant.TurnFacts` — the seam that keeps the core off the session pipeline.

**`internal/voice/assistant.go` is gone.** The transitional alias file was
deleted and every caller re-pointed: `internal/voice` and `internal/server` now
spell `assistant.SessionRow`, `assistant.Directory`, `assistant.Registry`,
`assistant.Notice`, `assistant.Delivery`, `assistant.DisplayFor` and the rest.
Aliases re-exporting one package's vocabulary under another's name are the second
name for one thing this repo keeps deleting, and leaving them would have let the
next surface pick either.

**Voice gained one narrow interface, not the service.** `voice.Options` takes
`Conversation` — `Mirror` and `SinceLast`, satisfied by `*assistant.Service` —
so what a call needs of the shared conversation is two methods, and a test can
drive it with twenty lines. It is passed as an interface the wiring already
narrowed, because a typed-nil `*assistant.Service` would arrive looking present
and panic on the first turn it tried to mirror.

**A turn is mirrored when it completes, never per fragment.** Transcription
arrives in pieces — the engine emits a chunk per server message and marks only
the last of a turn final — so mirroring per event would write a conversation of
syllables. The pieces accumulate per speaker (`utterances`) and flush once, on
`TurnCompleteEvent`, which is also the moment both halves exist: the operator's
ask and the answer to it, in that order. An **interrupted** turn counts, because
what was said was said and a record that skipped every barge-in would omit most
of a real call. The write happens on its own goroutine: the engine pump is also
carrying audio, and a failed write costs the record, never the call.

Each call mints a uuid for itself, which is what rides `metadata.callId`. A call
is not a session and has no row to borrow an identity from.

Two things worth knowing. Injected text (a notice, a report relay, the greeting
cue) is sent as a text turn rather than audio, so it produces no input
transcription and does not pollute the conversation with the server's own
instructions — but that is a property of the backend, not a guard here. And the
assistant's spoken greeting **is** mirrored, as the engine's own transcription of
it, which is correct: it is the first thing said on the call.

**The greeting reads `SinceLast("voice")` and folds it in.** `greetingCue` takes
a second argument, and an empty one adds no words at all — a greeting announcing
that there is no news would be a bulletin about the absence of one. The news is
read at greeting time rather than at connect because reading it STAMPS the
surface as having looked: a call that opened and never greeted would otherwise
consume news nobody heard. Its rendering is voice's own (`spokenNews`, six lines
at most, an untrusted line marked and quoted in the line itself), not the head
prompt's, because a prompt tuned for speech must never read a list aloud.

**The WS ops are registered unconditionally and refuse in words when the
assistant is off.** `handlerRegistry` is a package-level table and the read
lane's own test asserts every member of `concurrentOps` is a registered handler,
so "off" cannot be an absence — it is a sentence that names the switch, which is
what separates it from a bug or an unknown op. `assistant.say` stays on the
serial mutation lane and calls `SayAsync`; `assistant.history` and
`assistant.journal` are on the read lane, and that membership is the claim that
they mutate nothing a later request could observe out of order.

`assistant.journal` answers `{entries: [...]}` rather than a bare array, because
an object is what a later field (a cursor, a count) can be added to and a
top-level array cannot grow at all. `assistant.say` answers the **stored ask**,
not the reply: the reply arrives on `assistant.delta` and then
`assistant.message`, which is what the thread renders from either way.

Generated: `AssistantSayPayload`, `AssistantHistoryPayload`,
`AssistantJournalPayload`, `AssistantMessage`, `AssistantDelta`,
`AssistantJournalEntry`, `AssistantPage`, `AssistantUpdate`,
`AssistantJournalResult`, and the three pushes in `PushEventMap`.

**`VoiceReport` is `AssistantReport`, and the head's verbs are MCP tools.** Both
are registered from `registerAssistantTools`, the fourth group, behind one
`mcphttp.Assistant` interface (`Report`, `Verbs`, `ToolHandler`) that
`*assistant.Service` satisfies as it stands. Every verb is exposed, uncontained
ones included: the table is what the head may KNOW about as well as what it may
do, and a verb it cannot see is one it invents a way around. `mcphttp` imports
`internal/assistant` rather than mirroring the verb struct — the dependency runs
one way here, unlike `SessionModelReport`'s.

**The two callers share one endpoint and are separated by the injected id.** A
session's id is a UUID; the head's carries `assistant.HeadIDPrefix`
(`assistant-head-`), and `assistant.IsHeadID` is the test. It is enforced at
REGISTRATION — `registerSessionTool` for every tool that acts on the calling
session, `registerHeadTool` for the verbs — rather than at the top of each
handler, where the next tool would forget it. Both directions are tested: a
session calling a verb is refused, and the head calling `AssistantReport` or
`SetSessionName` is refused. The prefix is deliberately **not** the sessionless
persona's own `persona-`, which a discussion persona also wears; this test has to
name the head and nothing else.

**The head's bearer goes to the CLI as a 0600 file path.** `assistantHeads`
implements `HeadManager` over `Manager.StartPersonaRuntime`: it chooses the id
(the token has to be in the store before the subprocess starts, which nothing can
do for an id the manager invents), mints the token, and writes
`AgentiqueMCPHTTPConfig` to `<datadir>/mcp/<head id>.json`. It gets its own
writer rather than the session one because that one validates its id is a UUID
and the head's deliberately is not — the id becomes a filename, so the check of
what the id IS is what keeps a caller from writing outside the directory; this
validates the prefix and a UUID behind it. Every failure degrades to a head with
**no tools** rather than to no head, and never to inline JSON: `/proc/<pid>/cmdline`
is world-readable. The runtime manager revokes a terminated session's token on its
own; the file is this wrapper's, so `Close` removes it.

**`session.state` is observed with `SubscribeAll`, filtered before anything is
spawned.** The bus delivers synchronously on whichever goroutine published, so
the mapping runs on a goroutine of its own — but only for a snapshot that
actually carries a merge or an archive, which is the rare one. The subscription
is released on shutdown, and `Server.Shutdown` closes the service, which is what
stops the head and drops its credential file.

**`AssistantReport` is auto-allowed.** One entry in `internal/session`'s
interceptor map, on `ScheduleReport`'s grounds: the instruction teaching the tool
rides every prompt the assistant dispatches, and the assistant — unlike a call —
does not pre-check that the target session runs without stopping for approval. So
a followed run in any other mode would stop on a permission prompt for a report,
which the surface that asked for it cannot answer.

**Registration happens in `server.New`, not the serve command.** Both
registrations — the turn-end listener and the bus subscription — are inert
in-process bookkeeping with no destructive side effect, and they sit beside the
construction they belong to, which is where voice's already was. Nothing in the
assistant block touches the filesystem or starts a process: the head's subprocess
and its 0600 credential file are created lazily by the first turn, and the idle
stop is armed on use. Shutdown is `Server.Shutdown`, which the serve command
calls.

**Not done here.** There is no HTTP route for the assistant at all — the thread
is reached over the socket — so `features.assistant` is the whole of what a
client can check. (The two other items once listed here, `CLAUDE.md`'s stale
`VoiceReport` and the preamble's unprefixed verb names, were settled during
integration; see below.)

### Integration notes: the seams the three builds had to agree on

M1 was built by three agents in parallel — the core, the wiring, the thread —
and these are the four places their assumptions met, plus what each was settled
to. Nothing below changes a name the M1 contract binds.

**`assistant.journal` answers the journal, not a look.** The contract names it a
READ on the concurrent lane, and `SinceLast` — the half that stamps what a
surface has seen — writes. A write cannot go on that lane (a read there claims
it mutates nothing a later request could observe out of order), so the op is
`Journal(since, limit)`: a pure read, answering `{entries: [...]}` rather than an
`Update`. The consequence is honest rather than hidden — the thread's strip is
*the recent journal*, not *what you have not seen*, so its subtitle counts rows
("the last 12") instead of claiming "since you last looked", and nothing in the
thread stamps `seen_by`. `SinceLast` stays in-process, read by the one surface
that can write on its own behalf: a call's greeting. A thread-side look is a
mutation op and therefore its own decision, not a shape this one can grow into.

`AssistantUpdate` is still generated (typegen registers it), and no M1 op carries
it. That is the trap that caught this build once: a generated schema is not proof
of an op. `frontend/src/lib/assistant/rpc.ts` is the only file that names a
request or response shape, so the next op has one place to look.

**`frontend/src/lib/assistant/wire.ts` is now re-exports.** Typegen emits every
assistant shape, so the transitional hand-written Zod is gone; what stays local
is the two closed vocabularies typegen cannot emit (`ASSISTANT_ROLES`,
`ASSISTANT_JOURNAL_KINDS`), both of which are `string` on the wire so a peer one
release ahead cannot reject a payload. The eleven journal kinds are spelled in
two places — `journal.go` and that file — and the renderer's table
(`journal-marks.ts`) is keyed on the union, so a kind added on one side and not
the other renders as the neutral mark rather than as nothing.

**`AssistantReport` and the verb table have separate gates.** They were one
`mcphttp.Assistant` interface, which quietly broke the shipped feature: the
dispatcher appends `ReportingInstructions` to every prompt a CALL sends, whether
or not `[experimental] assistant` is on, so gating the report tool on the
assistant service named a tool that was not registered on every voice dispatch.
Now `mcphttp.AssistantReporter` (one method, satisfied by `*assistant.Service`
and by `*assistant.Registry`) is registered wherever a report has somewhere to
go, and `mcphttp.AssistantHead` (the verbs) exists only with the service — which
is right, because the head is the only thing that calls one. That split is the
same one `registerSessionTool`/`registerHeadTool` enforces per call; the gates
just stopped contradicting it.

**The preamble does not spell the tool prefix it does not own.** The head's
instruction lists the verbs by the table's own names and says the tool list puts
a server prefix in front of each. `internal/assistant` cannot name
`mcp__agentique__` — that constant belongs to the wiring layer, which imports
this package and not the other way round — and a prompt claiming "only these
exist" beside a tool list spelling them differently is the one ambiguity worth
one clause.

### Review notes: what the first M1 build got wrong

A review of the uncommitted M1 build found ten blocker/major faults and ten
minor ones. What follows is what each was and what it was fixed to, because
several of them are the same mistake wearing different clothes — a read that
writes, a consumption that happens before the thing consuming it exists — and
the next surface on this core will be tempted by both.

**The head has no native tools and a working directory of its own.** The verb
table was the whole containment claim, and underneath it the head ran a fullAuto
Claude CLI with Bash, Write, Edit, Read and the web fetchers, in the server
process's own cwd, fed untrusted agent text on every turn (news, reports, turn
summaries). "It cannot merge, delete, archive, reach a main worktree or reach a
paired machine" was false three ways over: `Bash` is all of them, and `Read` is
every paired machine's outbound bearer, which lives in the data dir at the uid
the CLI runs as.

So `session.PersonaRuntimeParams` gained `DisallowedTools` (additive, forwarded
to `runtime.CreateParams`, claude-only like the rest of that path), and
`server.headDisallowedTools` denies every file, shell, web and subagent tool —
`Task` included, because a subagent is a second context with its own tool set
and a deny list a spawn can step around is not one. `headWorkDir` points the
subprocess at `<datadir>/assistant-head`, 0700, created lazily at start;
failing to create it is a hard error rather than a degrade, because the fallback
is the server's cwd, which is the thing being fixed. Both live in
`internal/server`, not in the core: the names are claude's, and
`internal/assistant` is provider-neutral by construction — a containment claim
spelled in a neutral vocabulary is a claim nothing enforces. `HeadParams` says
so where it says it carries no working directory, for the same reason.

**The verb table is on the head's own MCP endpoint.** Every verb was registered
on the one shared `/mcp` handler, and agentkit answers `tools/list` from
everything registered on a handler with no reference to the caller — so with the
assistant on, every coding session's tool list grew by the whole table, the
eight uncontained verbs that exist only to be refused included. Context those
sessions pay for on every turn, tool names they can never call, and, for a
prompt-injected one, the assistant's vocabulary to aim a crafted report at.
`registerHeadTool` gated the CALL, never the listing.

`mcphttp.NewAssistantHandler(tokens, head)` is the head's endpoint, mounted at
`/mcp/assistant` (under the session endpoint's prefix, so the security-headers
rule and anything else reasoning about `/mcp` needs one rule) and only with the
service. It gets its OWN `TokenStore`: a coding session's bearer is not in it,
which is what makes "a session never sees the table" true of the listing rather
than only of the calls. `assistantMCPURL` derives the head's URL from
`MCPInternalURL` — one listener, so a second setting for the same port is a
second thing to get wrong. `registerHeadTool` stays as the second belt, since
the id is what a tool would act AS and a URL is not.

**`SinceLast` split into a pure read and a stamp.** Two faults shared one cause.
The head composed its preamble from `SinceLast`, which stamps, and only then
called `StartHead` — so a missing CLI or a connect timeout marked the news seen
for a head that never existed, and the next successful start read none. And
`headNews` stamped and then dropped the whole look whenever its journal half was
empty, which is exactly what a purely conversational call leaves behind: two
mirrored messages and no journal row, gone forever on the next thread turn.

`unseen(surface)` is now the read — pure, and it creates nothing — and
`markLooked(surface, update)` is the stamp. `SinceLast` is the two together and
is unchanged for its one caller, a call's greeting. `ensureHead` stamps after
`StartHead` returns; `headNews` gates on the look having carried ANYTHING, not
on its journal half, and renders both halves. That is the same rule voice
already followed by reading its news at greeting time rather than at connect.

**A look means "caught up to here".** `MarkAssistantJournalSeen` stamped the 50
rows a look returned, and the query returns the 50 NEWEST unseen — so a backlog
bigger than one page stayed unseen and the next look answered with the next
fifty OLDER rows, announcing last week after today, for as many looks as it took
to drain. It is now one statement, `MarkAssistantJournalSeenThrough`, which
stamps every unseen row at or before the newest row the look carried. Monotonic
in time, one UPDATE instead of fifty, and the per-row query is gone rather than
kept beside it.

**Reading the conversation does not create it.** `History` and `SinceLast`'s
conversation half went through `EnsureConversation`, which inserts the channel
row and upserts `assistant_state` on first use — and `assistant.history` is on
the socket's read lane, whose membership is the claim that a handler mutates
nothing a later request could observe out of order. `conversationID` is the
read-only lookup (cached id, state row, oldest `kind = 'assistant'` channel, then
give up), and a missing conversation reads as an empty one. Creation belongs to
`appendMessage` and `Mirror`, on the mutation lane, which is what they are for.

**A report takes the budget and the watch list before the journal.** The token
bucket lived inside `Registry.Report` and is reached only when somebody is live,
so the durable half — the half that rides every head turn as untrusted news —
had no ceiling and no follow check at all. Any session could pump unbounded
agent-written text into the assistant's attention, and the tool is registered
for every session whenever a report has somewhere to go.

`Service.Report` now refuses a run that is neither followed (`assistant_follows`)
nor live-listened (the registry, which is how a call follows), in words and with
nothing written, then spends one token for the report as a whole. `Registry`
grew `Take` and `Deliver` so the one bucket covers both halves; `Registry.Report`
is those two plus the parse, unchanged for the voice-without-the-assistant path.

**The idle eviction is held off for the duration of a turn.** The timer measured
from the last turn's END and nothing armed or stopped it at a turn's start, so a
turn opening in the last minutes of the window had its subprocess closed under
it — and a `Query` whose process is gone never sees a `TurnCompletedEvent`, so
it blocked until the 10-minute turn budget expired. The operator got nothing, for
ten minutes, with the composer shut. `beginHeadTurn`/`endHeadTurn` bracket every
turn (`defer`, so a failed one re-arms too — arming only on success left a live
head with no timer on it whenever the failure path did not reach `StopHead`), and
`evictIdleHead` re-arms instead of stopping when a turn is in flight, because a
timer can already have fired and be waiting on the mutex.

**A turn that says nothing still stores a turn.** A failed head turn logged and
pushed nothing, and an empty reply stored nothing by design — while the thread
arms its composer at the send and releases it only on a stored assistant
message. So the likeliest M1 failures (no provider CLI, a connect timeout, a
crash, the eviction above) showed the operator their own ask with the field
locked and no way to learn why. `Service.answer` now stores the assistant's own
sentence in both cases (`turnFailedText`, `turnSilentText`) — the server's words,
so not untrusted and not a quotation, with the CLI's error in the log where it
belongs. No new push type and no client change: the thread already renders a
stored message and releases on it, and a reload shows the same thing.

**The state observer claims its key before it counts.** `observeOnce` read the
in-memory guard, released the lock, and only then ran the count query, so two
`session.state` pushes arriving together gave two goroutines that both saw
`known == false`, both counted zero — neither had inserted yet — and both
appended. The claim now happens under the one lock before the read, and is
released only on the paths that wrote nothing. The count is what covers a
restart, which has no window; the claim is the whole dedupe inside one.

**`Close` shuts the door, and the credential files are swept.** `Close` stopped
the current head but set no flag, so a `SayAsync` queued behind a long turn could
reach `ensureHead` after shutdown and start a subprocess that outlives the
process — with its 0600 credential file unreachable forever, since the cleanup
that removes it is the one thing a dead server cannot run. There is a `closed`
flag now, and `server.SweepHeadCredentials` removes what a SIGKILL left behind,
called from the serve command's production block beside the other startup
sweeps, never from a constructor.

Two smaller ones, same review: `stateSeen` is bounded at `maxStateSeen` and
dropped whole past it (the count query is the authority, so a lost key costs one
indexed read), and a call now flushes the turn in progress from its teardown —
`mirrorTurn` before `unfollowAll` — so a socket that drops between the last
transcript fragment and `TurnCompleteEvent` does not discard the exchange a
greeting would most want to have.

**The greeting speaks from both halves of the look.** `spokenNews` rendered only
the journal, so the conversation half was fetched, stamped and thrown away: the
head heard what was said on a call, and a call never heard what was agreed in
the thread — which is the reverse of what this document promises. It now renders
thread-side turns too (`saidLine`, clamped, a call's own turns skipped as this
surface's own history), inside the same six-line cap.

**Three things the review found that are records rather than fixes.**

- The head's model. `assistant_state.model` and `SetAssistantModel` exist, no
  caller writes the column, and `headModel` hands the raw string to the CLI
  without passing it through `providers.Catalog`. So M1 ships the default-model
  path only: the column is always empty and the resolution the Decided section
  describes is M2 surface with no writer yet. Wiring the catalog into the head
  manager is where it goes when something can set it.
- The surface contract. Nothing calls `RegisterSurface` in production, so
  `Service.deliver` and the `Item` union are exercised only by tests: the thread
  renders from the global pushes and a call receives reports and notices through
  `Registry`/`Follower`. M1 defines the contract and wires no registrant. That
  is deliberate — a second delivery path into the thread beside the pushes would
  be two answers to one question — and it is recorded so nobody reads `Deliver`
  as a live path.
- The thread's strip reads `Journal`, not `SinceLast`, and nothing in the thread
  stamps `seen_by`. The reasoning is in the integration notes above; the M1
  contract's own line ("a recent-updates strip from `SinceLast`") is the one it
  amends, and a thread-side look is a mutation op and therefore its own
  decision.
