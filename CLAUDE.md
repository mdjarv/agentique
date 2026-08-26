# CLAUDE.md

How to work in this repo. What the product is and how to run it lives in
README.md; subsystem designs live in `docs/*.md`. This file holds only what you
cannot infer by reading the code: conventions, invariants, and gotchas.

## Before calling a task done

- `just check` (biome + tsc) passes.
- `cd backend && go test ./... -count=1 -race -short` passes. Run it directly
  rather than through the justfile. `-short` skips the test that needs a live
  provider CLI.
- After editing SQL under `backend/db/`, run `just sqlc`. After changing Go wire
  types, run `just typegen`. Generated files are only ever regenerated, never
  hand-edited.
- Doc comments and README/API surface are part of the feature, not a follow-up.

Use `just` rather than raw `npx`/`tsc`. The recipes `cd` into the right
directory; `npx biome` from the repo root fails silently and reports success.

Keep `backend/db/**.sql` **ASCII**, comments included. sqlc expands `SELECT *` by
byte offset, so one multi-byte character shifts those offsets and corrupts the
generated code for *later* queries. The error points at the victim, not the
cause ("extraneous input 'SELECid'").

## Priorities

Performance and reliability come first, and behaviour stays predictable under
load and during failures: session restarts, reconnects, partial streams. When
something structural is broken, fix the structure rather than routing around it.
Given a tradeoff, take correctness over convenience.

## Costs are irrelevant

We use API subscriptions. Keep costs and prices out of the UI, CLI output and
mockups. `totalCost` exists in the data model and stays there.

## Database access

The live SQLite database is at `~/.local/share/agentique/agentique.db`, shared
with the running server. Read from it freely with `sqlite3`; it is the best
source of realistic data. **Writes need explicit user approval** — a bad write
loses data for every running session. Test writes against a copy or an in-memory
DB.

## Engineering practices

**One job per module, function and component.** Keep IO out of logic, state
management out of rendering, transport out of business rules.

**Guard clauses and early returns.** Handle the error and edge cases at the top
and return; keep the happy path unnested.

**Errors propagate with context.** Wrap with `%w` (`%v` for non-errors),
accumulate cleanup failures with `errors.Join`. Recoverable failures return
errors rather than panicking.

**Constructors have no destructive side effects.** Startup sweeps, reapers,
identity and secret file creation, and boot reconciliation run from the serve
command's production block, never from `server.New` or any constructor a test
might call. A stray sweep in a constructor once deleted real worktrees during a
test run.

**Zustand selectors return stable references.** Never return `{}`, `[]`, or the
result of `.map()`/`.filter()`/`Object.values()` from a selector, as a fallback
or as a computed value. Each call makes a new reference and the component
re-renders forever. Use a module-level constant for fallbacks, and `useShallow`
or memoisation outside the selector for computed values.

**Go constructors:** `New()` for a package's single type, `NewTypeName()` when
there are several. Functional options once a constructor takes two or more
optional parameters, or looks likely to grow.

**Deleting a session goes through `Service.DeleteSession`**, which recurses.
`queries.DeleteSession` is a raw single-row delete and leaves orphaned children
and worktrees behind.

## Running a second server locally

Single-instance is enforced by a lock on the **data directory**, not the listen
address. Two servers on different ports still share one data dir's database,
worktrees and CLI subprocesses, and a server pointed at a scratch DB has an empty
picture of what that data dir owns. A sandboxed verify server once reaped every
live session of the running service. Isolate with `AGENTIQUE_HOME=<tmpdir>`;
`--db` and `--addr` do **not** isolate. `--test-mode` swaps in the mock CLI
connector and is not a sandbox flag either.

## Security invariants

An authenticated operator has arbitrary code execution by design, because they
run agents. So judge a change against the boundaries that actually hold: an
unauthenticated caller, a cross-origin page, another local user, and **a
prompt-injected agent**. Agents act on untrusted repo, web and tool content, and
are not a trusted principal. "Is the caller logged in" is not the question.

**A `{id}` route parameter is not one path segment.** Go's `ServeMux` matches on
the escaped path and unescapes the capture, so `%2F` arrives as a real separator:
`/api/x/..%2F..%2Fy` yields `../../y` from `r.PathValue`. Validate any path
parameter for what it is (a UUID, a timestamp) *before* the join, and never by
comparing the joined result against a root derived from the same untrusted value.
Record ids are also glob patterns in `filestore`, so `*` and `?` are rejected too.

**Agent-written bytes are never served as active content.** Session files come
from agents and are served from the app's own origin, where script can drive the
whole authenticated API. `internal/session/files_content_type.go` allowlists
provably inert types; everything else is an octet-stream attachment. Adding
`.html`, `.svg`, or any sniffable type to that list is a stored-XSS hole.
Response headers (`nosniff`, `frame-ancestors`, `Referrer-Policy`, the SPA CSP)
live in one middleware so new routes inherit them, and `script-src` allows
index.html's bootstrap by **hash computed from the embedded bundle**, never
`'unsafe-inline'`.

**Credentials never reach argv or a group-readable file.** The data dir is
owner-only (`paths.SecureDataDir`, called from serve, never a constructor) and
the DB plus sidecars are 0600. The per-session MCP bearer goes to the CLI as a
0600 *file path*, because `/proc/<pid>/cmdline` is world-readable. If that write
fails, fall back to the stdio transport, never to inline JSON.

**Inbound credentials are stored as digests.** `auth_sessions`, `pairing_tokens`
and `invite_tokens` hold `token_hash` (`auth.HashToken`, plain SHA-256, correct
because every token is crypto/rand output rather than a password). Anything
writing those rows directly hashes first; there is no plaintext column to fall
back to.

`machines.token` is the exception and stays plaintext: it is an OUTBOUND
credential this server presents to a remote. Hashing cannot protect it, and
neither can encryption at rest, because the key would live in the same directory
at the same uid the agents run as. **That is the open boundary.** A
prompt-injected agent can read the data dir, so it can read every paired
machine's bearer. The fix is a privilege split (separate uid or sandbox per
session), not another storage trick. Never describe the data dir as protected
from agents.

**Downloads fail closed.** A missing or unfetchable checksum aborts an install
(`install.sh`, `install.ps1`). Release asset and checksum URLs must be HTTPS,
loopback excepted. Unclassified 500s return a fixed message; `err.Error()` goes
in the log line, not the response body.

**No user row is written before its ceremony verifies.** `credentialCount == 0`
puts the server in rekey mode, so persisting a user at `register/begin` meant one
abandoned registration held that window open forever. Registration creates the
row in `finish` and re-checks the first-user precondition there. The rekey path
takes a one-time code from `agentique auth rekey`, never a display name.

**`is_admin` is not a containment boundary.** Any authenticated user can create a
session, and a session runs agents with tool access, which is code execution on
the host. A non-admin already has everything `requireFullAccess` guards, by a
longer route. The schema agrees: `sessions` and `projects` have no owner column,
and no list or WS topic filters by user.

Keep the existing guards, since they are consistent and cost nothing. But do not
add a feature whose safety *depends* on them, and do not describe the role to
users as a restriction. A real boundary means adding ownership to every entity
and scoping every list, subscription and command. That is a feature, not a check.
Until then, "can log in" means "owns the machine".

## Subsystem invariants

Each subsystem's design lives in its doc. What follows is only the rules a change
must not break.

### CLI subprocess lifecycle — `docs/process-lifecycle.md`

Each session's provider CLI runs in its own process group, spawned with a
background context so it outlives requests, and is only ever closed
cooperatively. The orphan reaper matches a process only when the CLI marker,
group leadership, and this server's data-dir owner stamp all hold. Matching fails
closed, and "orphan" means reparented away from us, not `PPID == 1` (systemd is a
subreaper). Idle eviction is opt-in and lazy-resumes on the next message.

**A restart is not a pause.** That startup reap is why: the new process comes up
and kills the CLI groups the old one left, so restarting mid-turn does not
suspend the turn, it ends it. Sessions survive, because worktrees, history and
metadata are on disk; the current turn does not. Anything that restarts the
server (in-app upgrade, rollback, a future self-restart) consults
`Manager.BusyTurns()` first and says, in those words, that the cost is the turn.

Busy comes from the runtime's own turn lifecycle (`runtime.Session.TurnInFlight`
via `Session.TurnInFlight`), never from session state: `State()` reports Idle for
one dispatch before the completion that caused it is broadcast. The pipeline's
`turnOpen` is a different concern, outcome attribution, and is not a busy signal.

### Archived vs done

Three independent facts, three owners. `state` is what the CLI process is doing
and belongs to the runtime; `done` means it exited cleanly, never "the user is
finished with this". `archived_at` is the user filing a session away, and is
written **only** by an explicit gesture: `ArchiveSession`, merge
`complete`/`delete`, a local session's commit. The runtime's `StateDone` seam
never writes it — that let a subprocess exiting hide a session inside a collapsed
section, and it is the same line `SetOnSessionFinished` already draws.
`worktree_merged` is the git outcome.

So archiving never transitions state: it releases an *idle* CLI through
`StopSession`, leaving a state that actually happened. Unarchive clears
`archived_at` and leaves no residue. Archiving is refused while a turn is in
flight, judged by `TurnInFlight`, not `State()`.

Archiving also **releases the pin**. "Keep this at the top" and "stow this away"
are contradictory claims, so no row is both. The release lives in the
`SetSessionArchived` query so no archive path can forget it, and unarchive does
not restore the pin, because guessing that the user still wants it at the top
would re-create the contradiction. The sidebar decides Archived before Pinned
(`sectionFor`) because the state push carries `archived_at` but not `pinned`.

Bulk *destructive* actions key on `worktree_merged`, never on archived. Archiving
is a one-click tidy, up to a whole-shelf sweep; only merged work is safe to delete
in bulk. UI copy says "Archive"; `done` reads as "finished" wherever it surfaces.

### Drafts are client-local

An unsent New-session prompt lives only in this browser's localStorage
(`ui-store.drafts`, keyed `new:<projectId>`), and that is why its rows sit
**above** Pinned: everything below them is server state, reachable from any
client, while a draft nobody can find is text nobody gets back. A draft is not a
session — no state, no outcome, no pin, nothing to archive — so it gets its own
row (`DraftRow`, `useDraftRows`) rather than a `ThreadRowVM` with the session
fields left blank. It also carries no timestamp, so the section sorts by project
rather than inventing a recency.

`NewChatPanel`'s composer keeps its own copy of the text and writes it back on
the next keystroke **and on unmount**. So any gesture that changes that store key
from outside the panel — the row's Discard, and the undo on its toast — has to
reach the composer too, or that write brings the draft straight back. The restore
half applies only while the composer is empty: every keystroke also lands there
(the composer is what persisted it), and writing then would fight the typist.

### Colour is filing, not liveness

A session row carries its project hue while the work is **still the user's to
deal with**, and goes grey only once it is **filed** — archived, or merged with
the run ended (`isHued`). Losing the CLI is not an outcome and must never grey a
row: idle eviction reclaims processes, a restart reaps every process group, a
crash takes one down, and all three end in a session that the next message wakes.
Greying those filed the work away on the user's behalf, beside something merged
last week.

So `awake` buys only the third line, never the colour — keep the two separate.
The state instead rides a **mark**: `REST_GLYPH` in `lib/session/rest-state.ts`
pairs each `RestToken` with one glyph, and the token union is closed so a new
outcome must choose its mark rather than inherit a blank. Both surfaces that
show sessions read that one table — the sidebar rows and the landing deck's
cards — because a session that says "evicted" in the rail cannot say "stopped"
on the overview. Grey stays the shelf's and Archived's language: both render
`compact`, which is grey and collapsed by construction.

The deck's "Needs you" band holds all three reasons a session waits on the
operator — approval, question, unread completion — ordered so the two that hold
a process come first. Unread is **server state**: `sessions.unseen_completed_at`
is set at turn completion (schedule-origin turns never set it), cleared only by
the `session.markSeen` RPC, and rides the wire as the optional
`unseenCompletedAt`. The client keeps an optimistic set in `apply-event.ts` for
snappiness, reconciled by pushes through `readUnseenCompletedAt` in
`wire-compat.ts` — which is deliberately three-valued: an absent field from a
peer that has never spoken it means "not reported", never "not unread"
(`serverSpeaksUnseen` in `chat-store.ts`). Viewing a session is what clears it;
nothing else does — archiving or merging leaves the mark in place.

### Wire compatibility across peers

A client talks to **several servers at once**, the primary plus one per paired
machine, each on whatever release that machine happens to run. So a rename to the
wire vocabulary is never atomic, and shipping one without a transition breaks
every machine that has not upgraded.

Renames go out as **expand/contract**, both halves at once:

- **Server expands.** Emit the new name *and* the old one. Derive the alias in one
  place, a `MarshalJSON` on the wire struct rather than at each construction site,
  so no broadcast path can forget it. Keep accepting the old op name in
  `handlerRegistry`.
- **Client accepts either**, through `frontend/src/lib/wire-compat.ts`, which owns
  every alias: `readArchivedAt` for fields, `LEGACY_OP` for renamed RPCs.
  `ws-rpc.define` retries under the old op name when a peer rejects the new one
  and remembers that peer, so it costs one round-trip per socket rather than one
  per click. Never spell an alias at a call site; the next rename needs one place
  to look.
- **Contract later**, once no supported release predates the rename.

Wire fields stay **optional**. The generated Zod schema mirrors the Go tags, so
dropping `omitempty` makes a field required, and the client then rejects the
*whole* payload from any peer that does not send it: every `session.state` push
from that machine dropped, its rows frozen. An absent field means "not set".

The per-machine offline cache (`lib/machines/cache.ts`) is the same kind of
boundary. It serialises an internal type, so it drifts whenever that type changes,
and a stale cache renders wrong rather than failing loudly. It carries
`CACHE_VERSION`: bump it on any rename, migrate the previous shape, and refuse a
version from the future rather than hydrating a guess.

### Channels and teams — `docs/channels.md`

The `messages` table is the source of truth for channel timelines. Informational
channel metadata (introductions, spawn notices) is **not** mirrored into session
events; new informational message types extend the existing skip list.

**Additive principle.** Channel features leave session rendering, event-pipeline
mutations and turn management alone for any session outside a channel.

Web-only discussion personas are sessionless and claude-only. Drive them through
`runtime.Manager` — a bare connector bypasses the permission pump and tools block
forever. They post as the third `sender_type: "persona"` (skipped by the legacy
event mirror) and live in project-less channels whose WS events fan out on the
global topic.

### Scheduled loops — `docs/scheduled-loops.md`

Delivery is idle-gated and fresh-turn-only, never mid-turn injection. Busy
refusals requeue without consuming an attempt, and evicted sessions lazy-resume
on fire. Turn identity comes from the turn registry, atomically subscribed with
turn start, never from state polling. Run lifecycle is one-way to a terminal;
late reports never rewrite terminals; auto-pause counts only real error
terminals.

All schedule timestamps are UTC RFC3339 seconds, because SQLite compares TEXT
lexicographically. Schedule-origin turns skip brain recall, activity bumps and
unseen-completion: schedule attention is its own channel, not the orange pulse.
The MCP schedule-create tool stays non-blocking, since CLI MCP clients time out.
The boot sweep runs from serve strictly before the scheduler starts.

### The session dock — `docs/session-dock.md`

Everything in a session that is not the transcript is in one collapsible panel
behind one header control: **Work** (Todos + Agents), **Changes**, **Loops**,
**Browser**. The chat is the page and is always rendered; the dock sits beside
it, or takes the pane when maximized.

Three unrelated things are called a dock, so name the component, never "the
dock", in anything a reader might land on cold: `SessionDock` (this, on the
right of a session), `VoiceDock` (the live call's surface in the sidebar), and
`SyncDock` (the sidebar's git summary).

Tabs are **derived, never curated** — a view exists because the session has the
thing, and `availableDockViews` is the only place that decides. State is per
session (`ui-store.dock`), so it is pruned at a cap and reconciled:
`resolveDockView` falls back when a stored view's subject is gone rather than
collapsing the dock, because collapsing reads as the user's own gesture.

`?dock=` is written; `?tab=` is still **read** through `legacyTabToDock` and
never written, because those links are in clipboards and in deep-links this app
minted. Mobile renders the same `SessionDock` in a sheet — one navigation model,
two presentations — and simply omits `onMaximizedChange`, since a control that
does nothing when pressed is worse than no control.

**Workflow is not a peer of Agents.** A workflow's agents ride its own progress
events as `WorkflowProgressEntry` (a phase, a label, a state — no report, no
narration), and `collectAgentRuns` skips `local_workflow` deliberately. They
cannot share a row type, so `WorkflowActivity` renders whole *inside* the Agents
section. One subject, one heading, two renderings.

### Subagent roster

**Only subagents are in the roster.** The CLI carries three unrelated things on
one `task` stream — a subagent, a backgrounded shell command, a workflow — told
apart solely by `taskType`, and background shells outnumber agents roughly forty
to one. `isSubagentRun` judges a run **once**, from its sticky `taskType` plus
its spawning tool name, never per event: older CLIs stamp `taskType` on
`task_started` and leave it empty afterwards, so a per-event rule lets a
workflow's terminal notification through and invents a row for it. Unknown on
both counts is excluded — a stray `make check` is a worse row than a missing one.

A badge is a claim on attention, so it carries only facts that can still be
acted on and returns to nothing when there are none. Todos resets each
`TodoWrite`; Changes clears on commit. The Agents badge shows agents **in
flight** and nothing else (`agentBadgeState`) — never a lifetime spawn count,
and deliberately **not** failures. A subagent that failed is the session's
problem, not the operator's: the parent reads the outcome and usually retries,
so raising it said "this needs you" about a turn that was proceeding fine. The
row still carries the outcome for anyone who opens Work.

`stopped`/`killed`/`cancelled` are their own state, never `failed`. The CLI
reports them when the agent shut a run down on purpose, and painting that red
teaches the reader to ignore red. A preview identical to the row's title is
dropped rather than printed twice.

Collapsing the dock is the one gesture that costs information, since the per-tab
badges go with it. `dockAlertState` compensates with **one** aggregate mark on
the toggle, ranked as `lib/session/priority.ts` ranks everything: waiting-on-you,
then failed, then live. Never a summary — a control reporting three states at
once reports none of them. Its `failed` means a **loop** that auto-paused, which
stays paused until a person acts; agent failures never reach it.

Live flight status is *not* behind a tab. `AgentFlightStrip` renders the same
runs at three densities: `rail` (chips), `board` (cards, top of the Agents
section), `line` (pips plus the oldest agent's clock, mobile). The rail mounts at
panel level in `ChatPanel` so it survives navigation; it is suppressed only while
the dock is open on Work, where the board says it louder.

The roster groups by the only two states a reader acts on, still out and came
back, never by turn — but it **scopes** to the current turn (`scopeAgentRuns`),
folding older runs behind one disclosure. Same argument as the badge: a list that
only grows stops being read, and in a 300px column a lifetime roster is a wall in
front of the two agents you came for. Nothing is discarded, and lifetime totals
stay in the footer. A run still streaming has no `turnIndex` and belongs to the
latest turn; attributing it to "earlier" would fold away the agents the dock is
for.

The strip owns its own 1s clock. Lifting it to `ChatPanel` would re-render the
whole session view every second an agent is out.

**An agent is readable, not just reportable.** A roster row and a board card open
into the same `AgentRunDetail`: the whole report it returned (`AgentRun.report`,
unflattened — `preview` is the one-line form and steps aside when the row is
open), then its own narration (`AgentRun.steps`) folded away behind it. The
report goes through the chat's `Markdown`, because an agent's headings and code
blocks have to read the same wherever you meet them. Narration opens by default
only when there is no report — an agent still out has nothing else to show, and
"Watch" that opens onto a second button is not watching. Both come from events
the session already has, so opening a row is a render, not a fetch: `steps` are the
forwarded events carrying `parentToolUseId`, folded in `collectAgentRuns` exactly
as `segments.ts` folds them for the transcript. A subagent's own tool call is
never a spawn and its own tool result is never the agent's return value — that is
what the `parentToolUseId` branch is for.

Those events exist only where the provider forwards them (Claude, with `[claude]
forward-subagent-text`), so an empty `steps` is normal and says so in words
rather than rendering a blank. The strip decides only *that* a card is open; what
open shows is injected (`renderDetail`), because reading an agent is the roster's
subject and the rail and line stay a glance.

**The report comes from `agent_result`, joined by `agentId`.** The spawn's
`tool_result` carries the same text but is truncated for the DB past
`maxToolResultDBSize`, so a long report reloads from history with its middle
missing — and the roster promises the whole one. `agent_result` is persisted
whole, but its `parentToolUseId` is empty, so it reaches its spawn only through
the task stream: `agentId` **is** `task.taskId`, and that task names the
`toolUseId`. Keep `taskId` on the wire and on `TaskEvent`; without it the join
silently degrades to the truncated copy. The transcript still skips
`agent_result` explicitly (`classifyEvent`) — the report is already there as the
`Agent` call's result, and printing it twice detaches it from the call.

**An `agent_result` that describes nothing never leaves `ToWireEvent`.** The
claude adapter derives one from *every* user event carrying a `tool_use_result`,
not just subagent spawns — claudecli's `parseAgentResult` returns non-nil for any
JSON object, so an ordinary Bash or Read result becomes an all-zero
`AgentResultEvent`. Those are dropped in `wire.go` (`emptyAgentResult`) rather
than filtered at persist time, because an event with no outcome, no agent and no
report is not news to a client either. A real one always carries a `Status`
(`completed`, `async_launched`); do not add emptiness filtering to `isTransient`,
which would still broadcast them.

The header's `SessionStatusPill` reports a blocked session from wherever you are.
It stays plain text now that the chat branch always renders and the approval
banner is always pinned above the composer: `onActivate` is for when there is
somewhere to go, and a control that does nothing when clicked is worse than a
label.

Dock badges share the sidebar's glyph vocabulary (`ThreadRow`), because one mark
must mean one thing across surfaces: **X is "it failed", the triangle is
"someone is waiting on you"**, and a pulse means live activity, never
blocked-ness. The Loops badge (`loopBadgeState`) reports `blocked`
(schedule parked for approval, or a run waiting/overdue) above `paused` (loop
auto-paused on repeated failures) — the same ranking as
`lib/session/priority.ts`, where approval outranks failure. Note the asymmetry
with agents: a loop's `failed` attention **survives being viewed** and clears
only on an explicit act (edit or re-enable). That is the scheduler's rule
(`schedule/api.go`), so the badge must not invent a local seen-state for it.

### Provider abstraction

Sessions are driven through agentkit/runtime's neutral contract. Never import a
provider-native event type inside the session pipeline; the two legitimate
exceptions are marked at their sites and gated on `provider == claude`. New
consumer code switches on neutral events and gates provider-specific features
through `runtime.Capabilities()`. Codex mid-turn send is emulated, queued and
replayed at idle, so the wire capability is deliberately `true` while the adapter
capability is `false`.

**A message's delivery is reported, never inferred.** `session.enqueue` decides
between three outcomes — the message opens a turn, joins the running one, or is
buffered for the next — and only the server can know which: the client reads
session state from a push that may be a round trip behind that decision. So
`EnqueueMessage` returns a `session.MessageDelivery` and the reply carries it,
because a client that guesses draws its own optimistic turn *and* renders the
echo of the same message. A client treats an absent `delivery` (an older peer)
as unknown, not as "turn". The composer's optimistic turn is rolled back **by
turn id** — matching on prompt text can delete a genuinely running turn that
happens to repeat the words.

**agentique never runs a provider CLI.** No `exec` of `claude` or `codex`
anywhere in this repo: not for a version, not for `doctor`, not to update.
Install facts come from the connector through `runtime.InstallInspectable`, which
answers for the binary that connector would actually spawn; a PATH lookup here
would be right only by coincidence.

Install-method enums are labels, not branches. The two provider libraries define
`native` differently on purpose, so gate on the library's verdict
(`SelfManaged`, a non-empty `UpdateCmd`). An empty update command means "tell the
user to update manually", never "fall back to npm": the wrong command installs a
second copy, and the version we probe stops describing the one that runs. See
`docs/upgrades.md`.

### Live voice — `docs/voice.md`

**Audio never rides `/ws`.** That socket is `ReadJSON`/`WriteJSON` both ways, so
one binary frame closes it for every subscription on it. Voice gets its own
endpoint, and the frame type is the discriminator there: binary is PCM, text is
JSON control. The client reads its playback rate from `ready` rather than
assuming one, because the echo engine and a speech model return different rates.

**A new socket path is unauthenticated until you make it otherwise.**
`requiresAuth` covers the `/api/` prefix and the exact string `/ws`; anything
else falls through as an SPA asset. That is why the voice socket is
`/api/voice/live` and not `/ws/voice`. `auth.wsUpgradePaths` enumerates every
path that may redeem a one-time `wsTicket`, and a test asserts each member is
also a path `requiresAuth` covers — a cross-origin paired machine has no cookie,
so a missing entry means it cannot connect at all. The upgrade origin rule lives
once, in `httpsecurity.WebSocketOriginAllowed`.

**The audio worklet must be an emitted file, never an inlined one.**
`audioWorklet.addModule()` is judged under `script-src`, which is `'self'` plus
index.html's hash — no `data:`, no `blob:`. Vite inlines small assets as `data:`
URIs by default, which produced a worklet that worked in dev (no CSP) and was
silently blocked in production. `build.assetsInlineLimit` in `vite.config.ts`
forces this one to a real file; a change there reintroduces the fault, and the
symptom appears only in a CSP-serving build.

`Engine` is the seam: caller audio in, a sealed `Event` union out. A speech
backend is another implementation, never a change to the transport, and the
loopback `EchoEngine` is how the browser audio path gets verified without
credentials. `Send` drops frames rather than blocking — the caller is a
microphone that will not wait. **One engine per call**; sharing one delivers the
first caller's result to whoever asked most recently.

Backends differ in credentials and data terms, not protocol, so nothing outside
`handler.go`/`config.go` names one. A backend missing its credential degrades to
echo and logs it, rather than refusing to mount the route.

**Idle timeout is a billing guard, not a nicety** — a live session bills for
wall-clock time with the mic open, so an abandoned tab keeps spending. But
**what silence means depends on the phase**: quiet while gathering is
abandonment, quiet while a run works is the expected state, so the working
ceiling is a backstop rather than a conversational timeout. The short rule
during a run hangs up in the middle of every real task. Following a session
starts work; `finished`/`failed` ends it; `blocked` does not, because the run is
stuck rather than done. Frame arrival is the weak activity signal (the mic
streams from an empty room); an engine with VAD exposes the real one through
`SpeechIdler` — and neither is the whole story: control frames, tool calls and
async deliveries bump an interaction clock, and **a pending async answer holds
the line** (`pendingAsync` raises the ceiling to the working rule), because a
call that hangs up while computing the summary it was asked for delivers it
into a closed socket. Slow work is visible on the wire (`activity` frames), and
a delivered summary gets its screen copy (`summary` frame) before it is spoken.
Costs still never appear in the UI.

**Speaking is `TextInjector`, type-asserted and best-effort, and it happens
after the screen copy** — a call whose engine has no voice still shows the
message. A notice is the server's own words; a report is agent-written text
about untrusted repo content and carries explicit quotation framing, so a
hostile repo cannot steer the conversation that queues the next prompt.

**The dialog agent drafts, it never sends.** Its output lands in the composer
through `ComposerTextareaHandle` and stops. One path into the session pipeline,
the visible send button. Hands-free does not change that contract, only the
confirmation channel: spoken verbatim readback, an explicit affirmative (never
silence), and an announced undo window.

**The call is the app's, not a session's.** `?sessionId=` is only the *initial*
focus; the call outlives navigation (it is owned by `voice-store`, never a
component), and the sidebar dock / mobile caption strip are its surfaces. **Dispatch is
focus-only and the screen follows the voice**: `run_prompt` has no session
parameter — to send anywhere the model must `focus_session` first, which emits
the `focus` frame and navigates the calling tab, so the target is on screen
before any yes, and the read-back names it. Manual navigation never retargets
the call; a `viewing` frame is data the model may *ask* about, never a switch.
Focus changes go one way for the same reason "no" never means "stop": an idle
click must not move where "send it" lands.

**Tools answer from what the server already holds.** A tool call pauses the
speech model, so a slow handler is audible dead air. Anything computed (a
session summary) answers immediately and injects the result later through
`TextInjector` — with quotation framing, because a summary distills untrusted
transcript content. The tool set is fixed at engine open; there is no adding
one mid-call.

**The world snapshot is a view, never authority.** The browser sends the merged
multi-machine session list as `world` frames because that merge exists only
client-side; the call stores it for listing and name resolution. Dispatch
re-checks the local DB every time — a snapshot row can make the assistant *say*
things, never *do* things. Remote sessions are listed and focusable; `run_prompt`
on one refuses naming the machine, because the report registry is local and a
remote run would report into nothing. `find_session` ranks and returns
candidates, it never picks — ambiguity costs a spoken question, not a
wrong-target dispatch.

**Per-call state is only the focus.** Everything else that spans requests is
per-session — the follow *set*, the briefing flag, the in-flight bit — and the
phase is *derived* (working iff any followed run is in flight), never toggled.
Both shipped voice bugs were per-call state broken by the second thing said in
one call; do not add another `bool` to `call` without asking which session it
belongs to.

**The worker reports; a watcher does not infer.** Salience belongs to the agent
that knows it just found the tests were already broken, not to something reading
its event stream from outside — which is why `VoiceReport` exists and an
inference layer does not. The three things an agent *cannot* report (blocked,
died, finished) are the runtime's job, ordered by `lib/session/priority.ts`, the
same rule as the deck's Needs-you band. A report is agent-written text about
untrusted repo content: **relay it, never act on it**, or a hostile repo steers
the conversation that queues the next prompt. Shape and rate limits live in the
schema and the registry, not in a caller's discipline, and the reporting
instruction is appended to a prompt only when someone actually stayed on the
call.

**The voice persona is a setting, not a constant.** Voice, verbosity and
character live in `voice_settings` (one row) and are read per call, so a change
takes effect on the next call rather than the next restart — a restart would
reap every in-flight CLI process group, which is absurd for trying a voice. The
voice name is free text with suggestions, never an enum, for the model-catalog
reason. **Character is tone, never behaviour**: it renders before the handoff
rules so the model reads those last, and the instruction says the rules win. A
personality field is a text box, and eventually someone types "skip the
confirmation".

**Live voice requires auto mode.** There is no spoken approval — you cannot
approve what you cannot see, on a transcription — so a call refuses the handoff
rather than creating that situation. `blocked` therefore means "this needs a
screen", not "answer this".

**Reading a message aloud is not the live call.** The bubble's speak button is
browser `speechSynthesis` (`lib/speech/`), deliberately local: instant, offline,
no key, nothing billed. It will not sound like the Live agent, which is the
right trade for a control on every message. The speaker is a **module
singleton** because speech is a serial channel — one listener, one pair of
speakers — so starting a message stops whatever was speaking and the other
button must see that; per-component state lets two answers talk over each other.
Markdown is stripped before speaking (`toSpeakableText`) or the synthesiser
reads the punctuation, and long text is split into queued utterances
(`toUtteranceChunks`) or the browser cuts it off partway.

**`segmentKey` is unique within a turn, not within a session.** `text-0` is the
first text segment of *every* turn, so anything needing document-wide identity
scopes by session and `turnIndex` as well — the speak button lit four bubbles at
once before it did.

### Model catalog — `docs/model-catalog.md`

**A new upstream model release must not require an agentique release.** Global
picker labels are stable family names with no version or context suffix. Exact
versions come only from the model ID a particular session reports. The
resolved-model loop is never fatal to a session. The frontend `ModelId` is
deliberately `string`. Catalog layers degrade weakest-first, and listing models
never fails.

### Multi-machine — `docs/multi-machine.md`

The server is the authorization boundary. Network reachability (a tailnet) and
peer discovery never substitute for auth, auth-disabled listeners are
loopback-only, and bearer tokens never ride URLs — sockets redeem bounded
one-time tickets, re-checked against the DB. An explicit credential never falls
back to another. Clients pin `machineId` and the signing identity, then verify a
fresh signed challenge before sending credentials, on every pair and connect
path. Revoking a session closes its established sockets, and unpairing revokes
the remote bearer before deleting the local catalog row.

Requests route by owning entity through the routing facade. Only `Project`
carries a client-side machine tag; sessions derive theirs from the project.
Remote slugs get a machine suffix, primary slugs are never rewritten.
Cross-machine grouping by canonical git remote is display-only — commands always
target one physical entity. Every surface that LISTS projects lists logical ones
(`useLogicalProjects`), never checkouts, and the representative owns presentation
(name, colour, icon, star); a remote's own favorite flag is ignored. A
session-scoped surface holds only a physical project id and so goes through
`useProjectPresentation` — read that row directly and the session wears the hue
of whichever machine happens to run it, leaving one repo red in the sidebar and
green in the chat pane it opens.

Listing is logical, **launching is physical**: `launchTargets` flattens the rows
into the checkouts a launch can name, so a repo on three machines offers three
targets and the machine is a searchable part of the choice, not an assumption. A
target's `slug` routes; only `rowSlug` may feed presentation. Where nobody picked,
`preferredMember` prefers a reachable checkout over the representative — the
representative is chosen for presentation and can be the one machine that is
asleep.

The machine catalog is full-access account state on the primary. localStorage
never persists bearer credentials, and per-machine data caches sanitize
live-ness. A flaky remote re-syncs only itself and never resets primary state.
Per-machine WS clients reconnect in place and are never replaced.

**A refused credential never opens a socket.** It fails at the ws-ticket mint, so
`ws.onclose`, and therefore `onDisconnect`, never runs. Diagnosis hangs off
`WsClient.onAttemptFailed` as well, or the one fault worth naming
(`credential-rejected`) can never be recorded and the machine pulses
"reconnecting" forever with no Re-pair button.

For the same reason a passing identity proof clears only the faults it disproves
(`clearedByIdentityProof`). Identity says who answered, never whether they still
accept our credential, and `machineFetch` re-proves identity on every retry, so
clearing that fault there would erase the diagnosis a second after it is made. A
rejected credential is cleared by proof of the opposite: a connection that
authenticated, or a re-pair.

Symmetrically, **removal tolerates a refused revoke.** A credential the remote
already rejects is already revoked, and failing there strands an entry that can
be neither used nor removed.

### Brain and memory — `docs/brain.md`

The liftable core lives in `internal/memory` (stdlib plus yaml/uuid only);
agentique policy lives in `internal/brain`. Markdown is the source of truth, and
everything else (graph, areas, vectors) is a rebuildable index.

Recall is fluid and per-turn with a session seen-set for delta injection. Do not
reintroduce first-turn-only recall. Semantic similarity is pluggable and
everything degrades cleanly to keyword/Jaccard without an embedder; the recall
thresholds (veto and vouch) are embedding-model specific. Stopwords drop
conversational filler but never domain terms — `just` is the build tool.

Strength changes on outcome, not injection. Human confirmation outranks
corroboration, which is capped below it. Model choice is a required caller
parameter in the memory core, never a library default.
</content>
