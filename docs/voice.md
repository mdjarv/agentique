# Live voice

A spoken interface to agentique — the switchboard. A conversational agent works
out *what to ask* with the operator, and it can see the state of the app: list
the sessions that need attention, resolve a spoken name, put a session on
screen, summarize it, then draft the prompt and hand it to the session that
does the work. The agent never runs the coding job and never sends the message.

The feature is gated by `[experimental] voice`. The call belongs to the app,
not to a session: it is owned by the frontend's `voice-store` and survives
navigation, the sidebar's voice dock (desktop) and the floating bubble (mobile)
are its surfaces, and the composer's Live button is a shortcut that opens the
same call with that session as its initial focus. From there you converse, it
drafts, reads back naming the target, dispatches, follows the runs, and tells
you what happened.

## Shape

```
Browser ──WS(binary PCM ⇄ JSON control)──▶ /api/voice/live ──▶ Engine (speech model)
   │                                                │
   │ world / viewing ──▶ call: focus + follow set   │ tools: list_sessions,
   │ ◀── focus (the screen navigates) ──────────────┘ find_session, focus_session,
   │                                                  summarize_session, run_prompt
   │                                      run_prompt │ Dispatcher (focus only)
   │                                                 ▼
   │                                      Service.EnqueueMessage ──▶ the session
   │                                                 │
   │                 VoiceReport (agent) + Notice (runtime) ──▶ back to the call
```

Two participants, not three:

- **The drafter** — the realtime speech model. It owns voice activity detection,
  barge-in, turn-taking and synthesis, *and* it writes the prompt. It has one
  tool and never runs anything itself.
- **The workhorse** — the existing session, unchanged. It receives the prompt
  through the same path the composer's send button uses.

An earlier design had a third participant, a separate Claude agent doing the
drafting behind the speech model, on the reasoning that a model which has never
seen the codebase should not write prompts for one that has. In practice the
speech model drafts well from a short project context, and the extra hop cost a
round trip on every turn of a latency-critical conversation. What survives from
that reasoning is the limit on context: the drafter gets a summary, not the file
tree and session history, because everything handed to it goes to the speech
vendor.

## Transport

One WebSocket per call. **The frame type is the discriminator** — no envelope,
no framing header:

| Frame | Direction | Payload |
|---|---|---|
| Binary | browser → server | Int16 PCM, 16 kHz, mono, little-endian |
| Binary | server → browser | Int16 PCM at the rate announced in `ready`, mono |
| Text | both | JSON control messages |

That keeps audio out of a JSON encoder, which matters at ~30 frames a second.

Control messages are in `messages.go`. The client learns its playback rate from
`ready` rather than assuming one: the echo engine returns audio at the input
rate and a speech model returns it at `OutputSampleRate`, and the same client
code has to play both.

`turn_complete` carries `interrupted`. Both cases must reach the browser,
because both mean *flush the playback queue*. A barge-in that does not flush
leaves seconds of stale speech playing over the person who interrupted.

## Why a separate socket

The main `/ws` control plane is JSON in both directions (`ReadJSON`/`WriteJSON`
over a `chan any`). A binary audio frame arriving there fails to decode, returns
an error from the read loop, and closes the connection for every other
subscription riding it — sessions, projects, the activity wire. Audio gets its
own endpoint so a voice fault cannot take down the control plane.

## Why it is mounted under `/api/`

`auth.requiresAuth` protects the `/api/` prefix and the exact string `/ws`;
everything else falls through as an SPA asset. A socket at `/ws/voice` would be
**unauthenticated**, streaming a live microphone to a paid API with no
credential.

Ticket redemption is enumerated the same way. `auth.wsUpgradePaths` lists every
path that may present a one-time `wsTicket`, and a test asserts each member is
also a path `requiresAuth` covers. A cross-origin paired machine has no cookie
on this origin, so without its entry there it cannot connect at all.

The upgrade origin decision lives once, in `httpsecurity.WebSocketOriginAllowed`,
so a second socket endpoint cannot arrive with a subtly different rule.

## The browser side

Capture is an `AudioWorklet`, not a `ScriptProcessorNode`: the latter is
deprecated, runs on the main thread, and glitches under exactly the load a busy
app applies. The worklet batches 512 samples (~32 ms at 16 kHz) before posting.
A render quantum is 128 frames, so posting per quantum would mean 125 messages
and 125 socket frames a second carrying 8 ms each — all overhead.

Capture and playback use **separate AudioContexts**, at 16 kHz and the engine's
announced rate. One shared context would resample every played frame down to the
capture rate and throw the difference away.

`echoCancellation: true` is load-bearing, not a nicety: it is the only thing
stopping the agent hearing itself through the speakers and interrupting itself.
There is no server-side echo cancellation anywhere in this design.

### The worklet must be an emitted file

`audioWorklet.addModule()` is judged under `script-src`, which here is `'self'`
plus index.html's hash — no `data:`, no `blob:`.

Vite inlines small assets as `data:` URIs by default, and the worklet is small
enough to qualify. That produced a build that worked in dev, where no CSP
applies, and was blocked in production. `build.assetsInlineLimit` in
`vite.config.ts` forces this one file to be emitted; everything else keeps the
default size rule.

The failure is invisible in dev and invisible behind an earlier microphone
error, so it is worth re-checking after any Vite upgrade: build, then confirm
`dist/assets/` contains a `mic-worklet-*.js`.

## The engine seam

`Engine` is caller audio in, `Event`s out. `Event` is a sealed union — a type
switch over it carries a default case, and a new outcome is added to the union
rather than arriving untyped at a call site.

`EchoEngine` implements it as a loopback. It exists so the browser audio path —
capture, worklet batching, framing, upload, download, playback scheduling — can
be verified end to end with no credentials and no model. If audio is wrong
there, it is wrong in the browser, which is a much smaller place to look than a
live model session.

A real speech backend is another implementation of `Engine`, not a change to the
transport.

`Send` must not block. The caller is a microphone that will not wait, so a full
buffer drops the frame; blocking would stall capture and turn a transient reader
stall into permanent added latency.

**One engine per call.** Sharing one across calls means the second caller's
stream overwrites the first's and results are delivered to whoever asked most
recently.

## Idle is a billing guard, and it is phase-aware

A live speech session bills for **wall-clock time with the microphone open**.
Unlike every other cost in agentique, an abandoned tab keeps spending until
something closes it.

**What silence means depends on what the call is doing.** Quiet while
*gathering* is abandonment, and `[voice] idle-timeout` (90s by default) closes
the call. Quiet while *working* is the expected state — the whole point of
staying on the line is that you ask for something and then stop talking while it
happens — so a much longer ceiling applies, as a backstop against a tab left
open behind a run that never ends rather than as a conversational timeout.
Applying the short rule during a run would hang up in the middle of every real
task.

Following a session is the gesture that starts work; a `finished` or `failed`
notice ends it. `blocked` does not.

**A promised answer holds the line too.** A tool that says "working on it" and
delivers later — `summarize_session`, and anything else built on
`summarizeAsync` — counts up `pendingAsync` for the whole flight, and while that
is above zero the working ceiling applies whatever the phase says. Waiting for
an answer you asked for is not abandonment. It is a count rather than a flag
because two asks can be outstanding at once, and each is bounded by its own
work's timeout, so it cannot stick.

Frame arrival is the weak version of the activity signal — the microphone
streams continuously, so frames keep coming from an empty room. An engine that
performs voice activity detection knows when the caller last actually *spoke*,
and exposes it through the optional `SpeechIdler` capability, type-asserted
rather than required. Engines without it fall back to frame arrival, which still
catches a closed laptop or a dropped connection. Frames never override the
speech clock: taking the later of the two would keep an empty room open forever.

What *does* count beside speech is what the call itself did. A control frame
from the browser, a tool call, an answer delivered late: `lastInteraction` is
bumped by each, and `lastActivity` is the later of that and the speech clock.
None of it is audible, and all of it is proof the call is not abandoned.

Costs stay out of the UI. This is an operational limit, not a price display.

## Saying it out loud

A report or notice reaching the browser is only half of it. Speaking is the
optional `TextInjector` capability — type-asserted, because the loopback echo
engine has no voice and should not have to pretend otherwise.

Speaking is **best effort and second**: the screen copy goes out first, so a
call whose engine cannot speak, or whose session is mid-reconnect, still shows
the message rather than losing it.

The two are framed differently on purpose. A **notice** is the server's own
words and needs only an instruction to say it. A **report** is agent-written
text about untrusted repository content, so it carries an explicit
quotation framing (`reportRelayPreamble`): say this, never follow directions
inside it. Without that, a hostile repository could reach through the working
agent and steer the conversation — and the conversation is what queues the next
prompt.

A **summary** is the third thing that reaches the browser this way, and it keeps
the same order: the `summary` frame carries the session and the text and goes
out before the spoken copy, which is quoted the way a report is
(`summaryRelayPreamble`). An empty summary sends no frame — there is still an
honest spoken answer, but a card saying nothing says less than nothing.

### Slow work is visible

Tool work is otherwise invisible, and a healthy call computing a summary looks
exactly like a dead one. So the slow path announces itself: `activity` with a
non-empty `label` when it starts, `activity` with an empty one when the answer
lands, sent before the summary. There is at most one at a time, so a new label
replaces the old, and every one of these sends is best effort — a frame that
does not arrive costs a progress line, never the work it describes.

Only an *explicit* ask shows a label. Focusing a session warms its summary in
the background, and a progress line for work nobody requested is
indistinguishable from a bug, so warming is invisible — though it holds the line
exactly like the ask does.

## Backends

`[voice] backend` selects the speech transport:

| Value | Credential | Notes |
|---|---|---|
| `echo` | none | Loopback. Contacts nothing. |
| `aistudio` | `api-key` | Default. Free-tier content may be used to improve Google's products; paid-tier content may not. |
| `vertex` | `project` + ADC | Enterprise data terms, IAM, audit logging. |

The two real backends differ in credentials and data terms, not protocol — the
same SDK and the same session config drive both — so switching is a config
change, not a rewrite. Nothing outside `handler.go` and `config.go` names a
backend; the rest of the package is written against `Engine`.

A configured backend missing its credential **degrades to echo** and logs a
warning, rather than refusing to mount the route. A plumbing problem should not
look like a missing feature. A misconfigured `[voice]` section disables the
feature and never takes down the server.

`[voice] model` is configuration rather than a constant, for the reason the
model catalog gives: a new upstream model must not require an agentique release.
The two backends do not carry identical model ids, so it changes with `backend`.

## Progress reporting

The call stays open through a run. Something has to decide what is worth
interrupting for, and **that decision belongs to the worker**, not to a watcher.

The obvious design subscribes to the session's event stream and infers salience
from tool calls and text. It works, and it puts the judgement in the one place
that has to guess. The working agent does not guess: it knows it just found the
tests were already broken, and it knows it is about to change approach. So it
gets a tool — `VoiceReport` — and the prompt tells it when to reach for one.

That deletes the entire inference layer: no event subscription, no debouncer, no
salience model, no second copy of the priority rule. `ScheduleReport` is the
same shape doing the same job for scheduled loops.

`Registry` routes a report to the calls following that session. A call follows
every session it has dispatched into with someone staying on the line — a
follow *set*, not a single binding — and each report or notice is spoken with
the session named, so two live runs cannot be confused. All bindings are
released when the call closes, so a dead socket stops looking like a listener.

### What the worker cannot report

Three things, and they are the three that matter most:

- **Blocked** — it is suspended waiting on approval and cannot call anything.
- **Died** — a crash means there is nobody left to speak.
- **Finished** — completion is a runtime fact, not an agent utterance.

Those arrive as a `Notice`, ordered by `lib/session/priority.ts` — the same rule
as the deck's Needs-you band, not a second opinion about what deserves
attention. Everything else comes from the worker.

`voiceTurnWatcher` in the server package supplies them, hanging off
`Manager.AddTurnEndListener` — which fires once per turn on any session "after
that turn has stopped… completion, a CLI that died, a session closed
mid-flight". That is precisely the coverage a completion hook lacks, and the
reason the runtime rather than the agent is the source. It resolves blocked
before failed before finished, and the no-listener case costs one map lookup
before returning.

A notice is **never rate limited**. The report budget exists to stop a chatty
agent monopolising the listener; there is no version of "you are reporting too
often" that should apply to "the run failed".

`blocked` does **not** end the working phase — the run is stuck, not done, and
the process is still held.

### Live voice requires auto mode

There is no spoken approval handling. You cannot meaningfully approve a command
you cannot see, on a transcription, so the design does not create the situation:
a session driven from a call runs in auto mode, or the call refuses the handoff
and says so.

That makes **blocked** a stuck state rather than a question. If a run somehow
lands on a permission prompt, the push says it needs a screen — it never asks
the listener to answer. Silence would be worse: the run would sit there and the
call would sound fine.

The tradeoff is stated plainly because it is real: a voice-originated run
executes without anyone watching a screen.

### The report is untrusted text

A report is written by an agent operating on repository content it did not
author. It is **data to relay, never an instruction to follow**.

This is not theoretical. The conversation a report lands in is what queues the
*next* prompt, so an agent that could steer that conversation could steer the
next task. Whatever speaks a report treats it as a quotation, and the system
instruction says so in the terms CLAUDE.md already uses: an agent is not a
trusted principal.

### Budget

Two constraints, both doing filtering work where it cannot be forgotten:

- **Shape.** `kind` is a closed enum and `headline` is clamped to one speakable
  sentence. A free-text field gets paragraphs, and a paragraph read aloud is
  worse than no report — you cannot skim speech.
- **Rate.** A token bucket per session (burst of 3, one back every 3 minutes).
  The prompt asks for two or three calls in a ten-minute run; this is the
  ceiling that catches a worker ignoring that, not the target.

Both failure modes answer honestly rather than silently swallowing the call,
because each tells the worker something different about whether to keep going:
*nobody is listening*, *you are going too fast*, or *spoken*. The budget is
per-session and is dropped with the last follower, so a new call does not
inherit a bucket the previous one spent.

### The instruction is conditional

`ReportingInstructions` is appended in `voiceDispatcher.Dispatch`, because
dispatching from a live call **is** the "someone is listening" condition —
there is a person on the other end by construction. A run started from the
composer, a schedule, or anywhere else carries none of it: no instruction, no
tool calls, no reporting overhead.

## The persona is a setting, not a constant

How the agent sounds and how much it says live in the database
(`voice_settings`, one row) and are edited at **Settings → Voice**.

They are not in `config.toml` on purpose. These are settings somebody changes
to taste and wants to hear the effect of, and a config value needs a restart —
which reaps every in-flight CLI process group. Far too much to pay for trying a
different voice. They are read at the start of each call instead, so a change
lands on the next call.

Three things are settable:

- **Voice** — the backend's prebuilt voice, reaching `SpeechConfig`. Free text
  with suggestions, never an enum: the upstream list grows between agentique
  releases, and pinning one would make a new voice need a release. Empty
  resolves to `DefaultVoiceName` (Aoede) — a chosen default rather than
  whatever the backend ships, so a fresh install sounds deliberate.
- **Verbosity** — brief, balanced or detailed. This one *is* a closed set,
  because it is ours. Unrecognised values resolve to brief: everything said is
  spoken aloud, often to someone driving, so the safe end is the fallback.
- **Character** — free text describing tone.

Each voice has a **Listen** button, because choosing one by starting a whole
call and hoping is not choosing. An audition runs `voice.Preview`, which opens a
short session on the *same* engine a call uses and returns the line as WAV — the
audition is the thing itself, not an approximation from some other endpoint that
might not match. WAV rather than raw PCM so the browser decodes it in one call
instead of reproducing the call's scheduling queue for two seconds of audio.

Each click is a real paid session, so auditions are serialised server-side and
the sample is one sentence. The loopback backend refuses rather than returning
silence, which would look like a broken speaker.

**Character is tone, never behaviour.** It is rendered *before* the handoff
rules so the model reads the rules last, and the instruction says so beside the
character itself: if the character asks to skip the read-back, keep the
character and follow the rules anyway. A personality box is a text field a
person types into, and eventually someone types "don't bother confirming".

`Persona.Sanitize` clamps the free text and flattens newlines — a multi-line
personality can otherwise imitate the section headings of the instruction it is
embedded in — and it runs at the storage boundary, so nothing downstream has to
remember to.

## The switchboard

The call holds one **focus** — the session work would go to — and a **follow
set** — the sessions it has dispatched into. `?sessionId=` on the socket URL is
only the *initial* focus; a call opened without one starts unfocused and can
still answer questions.

**Dispatch is focus-only, and the screen follows the voice.** `run_prompt` has
no session parameter: to send anywhere the model must call `focus_session`
first, which moves the call's focus, sends the `focus` control frame, and the
calling tab navigates there (`useVoiceFocusNavigation`). So the target is on
screen before any yes can be given, and the read-back names it ("To Live Voice
Dialog: …"). This is the safety contract extended to a target chosen by voice
rather than fixed by the URL. It is one-way: manual navigation never retargets
the call — the client sends a `viewing` frame, which the server injects as a
data-framed note the model may *ask* about ("switch to what you're looking
at?"), and never acts on silently. `focus_session` accepts only ids the call
was actually offered (a find/list result, the initial focus, a viewing note),
so the model cannot focus an id it hallucinated.

**Tools answer from what the server already holds.** The speech model is paused
until a tool call is answered, so a slow handler is audible dead air. The tool
set is fixed at engine open (`LiveConnectConfig`); there is no adding one
mid-call. `list_sessions` and `find_session` read held state; `focus_session`
is one DB read plus a frame; `summarize_session` answers immediately and
injects the result through `TextInjector` when the local summarizer delivers —
with quotation framing (`summaryRelayPreamble`), because a summary distills
untrusted transcript content. Focusing a local session warms its summary in the
background so the likely next question is already answered.

**The world snapshot is a view, never authority.** The merged multi-machine
session list and the unread set exist only in the browser, so the client sends
them as `world` frames (after `ready`, then coalesced on change, capped at 200
rows, every field clamped server-side). Listing and name resolution merge the
snapshot with the local DB — the DB wins for local rows. Dispatch re-checks the
local DB every time: a snapshot row can make the assistant *say* things, never
*do* things. Remote sessions are listed and focusable like any other; a
`run_prompt` on one refuses naming the machine, because dispatch and the report
registry are local — a remote run would report into nothing. Routed dispatch
remains the multi-machine routing facade's feature, not the voice socket's.

**Resolution never guesses.** `find_session` (match.go) does normalized fuzzy
matching over name, project and machine — built for transcription mangling
("live voice dialogue" finds "Live Voice Dialog") — and returns up to five
ranked candidates with disambiguators and a `top_is_clear` flag. A clear winner
is confirmed by full name while focusing; several candidates become a spoken
question naming what distinguishes them. Ambiguity costs one exchange, never a
wrong-target dispatch.

**Per-call state is only the focus.** Everything else that spans requests is
per-session: the follow set (a "no" to staying never releases an existing
follow), the reporting briefing (each session is taught once — a per-call flag
would leave the second session reporting into silence), the in-flight bit. The
phase is *derived* — working iff any followed run is in flight — never toggled
by events. Reports and notices are spoken with the session named, so two live
runs cannot be confused. Both shipped voice bugs were per-call state broken by
the second thing said in one call; this is the structural answer.

**The directory seam.** The voice package stays independent of the session
pipeline: it sees the app through `voice.Directory`
(orientation/list/brief/summarize), implemented in the server package
(`voice_directory.go`) over `ListAllSessions`/`GetSessionInfo` and the
summarizer. The system instruction opens with a one-paragraph orientation —
counts and the names of sessions needing attention — built through the same
seam, so "do I have unread sessions?" is answered before the first tool call.
Unread itself is server state since the switchboard
(`sessions.unseen_completed_at`, cleared by `session.markSeen`; see CLAUDE.md).

## The drafter

`SystemInstruction` turns the speech model into a drafter, and it carries most
of the feature. It is built **per call**, because project context belongs to the
session the call is attached to — `Dispatcher.ProjectContext` supplies the
session's name and branch plus the head of the project's `CLAUDE.md`, bounded,
since everything in it goes to the speech vendor on every call. Nearly every way this goes wrong is a prompt failure rather than
a transport one, so it is written against the specific failures:

- **It never answers the question itself.** Asked "why does the reconnect keep
  dropping?", a helpful assistant speculates. This one has not read the code and
  cannot, so it turns the question into a prompt for the agent that can. This is
  the likeliest failure and gets the loudest rule.
- **It talks like a person on a call.** One or two sentences, no lists, no
  headings, no code, no file paths read aloud.
- **Silence is not consent**, and neither is being told to skip the read-back.

The model's only *acting* tool is `run_prompt`; the switchboard's other four
(list, find, focus, summarize) look and never touch. It never runs anything
itself: the prompt goes through `Dispatcher` to `Service.EnqueueMessage`, the
same path the composer's send button uses. One route into the session pipeline
whether the gesture was a click or a sentence.

Use the typed `Parameters` schema on the declaration, not
`ParametersJsonSchema` — they are mutually exclusive and the Live API honours
the former.

### Every tool call is answered

The model is **paused** until the response arrives, so an unanswered call is
indistinguishable from the call having died. Every path through `runTool`
returns a payload, including the failures, and dispatch runs off the event pump
so a session that has to be woken does not stall audio.

### The refusal is real

`AutoRunnable` is checked before dispatch. Live voice has no spoken approval, so
a session that would stop and ask is refused at the handoff rather than stalling
invisibly while the call sounds healthy. Only `fullAuto` qualifies — under
accept-edits a Bash prompt still blocks, and nobody would be told.

### Delivery is spoken, not guessed

`EnqueueMessage` returns which of three things happened, and the tool result
says it: "Started", "Added to what it is already doing", "Queued — it will start
that when the current work finishes". Only the server knows which, which is why
the contract reports it rather than letting the model infer.

### Recent history reaches it as a summary, not a transcript

The drafter needs to know what this session has been doing. Handing over the
transcript would ship whole files, tool output and prior answers to the speech
vendor on every call.

`sessionSummarizer` distils the last few turns into a paragraph **locally**,
through the provider CLI — subscription-billed rather than metered — and only
that paragraph leaves the machine. It is also better context: the drafter needs
orientation, not the record.

Configured by `[voice] summary-model`; empty disables it and the drafter then
knows the project but not the session. The result is cached for ten minutes and
dropped whenever a turn ends, since the summary describes the session as it was
before that turn. A summariser that misses its budget returns nothing and the
call opens without it — a slow summary must never become a silent microphone.

The transcript is untrusted input: it is repository content, tool output and
model text, none of it authored here. The summariser is told so explicitly,
because one that followed instructions found in its input would launder them
straight into the drafter's system prompt.

### Verified against the real service

`TestGeminiToolCallLive` drives a real conversation to a real tool call.
`TestGeminiRefusesToSkipTheReadbackLive` asks it to delete all the tests without
confirmation and asserts no dispatch happens — it answers "I must read the
prompt back before I can proceed."

Both are skipped by `-short` and without `AGENTIQUE_VOICE_API_KEY`. The number
of turns before a dispatch is not fixed: the drafter may clarify first, and
always reads back, so the test keeps agreeing until the tool is called.

### The handoff asks both questions at once

The read-back and the stay-on-the-line question are one utterance, not two
turns: *"Does that sound right, and do you want to stay on while it runs?"*
Two questions in a row is one too many for someone driving, and the answers
arrive together anyway.

## The handoff contract

**The dialog agent drafts. It never sends.**

A spoken instruction is the least reliable input in the product — transcribed by
one model, interpreted by a second, rewritten by a third. Its output lands in
the composer through `ComposerTextareaHandle.setText` and stops there. There is
exactly one path into the session pipeline, the existing send button, and it
stays visible and interruptible.

Saying "send it" is allowed and fills the composer, then presses the button that
is already there. It does not open a second route.

This keeps the feature additive in the sense the channels invariant already
uses: no change to session rendering, the event pipeline, or turn management for
anything not using it.

### Hands-free is a different confirmation, not a different contract

The primary target is Android, hands-free. A contract that terminates in a
silent text box terminates in nothing for someone who cannot look at a screen.

The invariant was never "you must read it" — it is *one send path, explicitly
confirmed, always undoable*. Hands-free keeps all three by making the
confirmation audible: the agent reads the drafted prompt back verbatim, the
operator confirms with an explicit affirmative (never silence), and the send is
announced with an undo window. Auto-approve is forced off for turns originated
hands-free, whatever the session's setting.

## The Gemini engine

`geminiEngine` implements `Engine` against the Live API. Three things shape it.

**`v1alpha`, not `v1beta`.** Session resumption is absent from `v1beta`, and
without resumption a call cannot outlive ten minutes.

**Resumption is a precondition, not a refinement.** The connection drops at
roughly ten minutes regardless of session length, so a call spanning a coding
run hits it every time. On `SessionResumptionUpdate` the handle is stored; on
`GoAway` the session is closed from our side so `Receive` returns promptly and
the reconnect happens on our schedule rather than mid-sentence; the reconnect
swaps the session pointer under the send mutex. The browser never learns
anything happened.

**One mutex guards sends and the session pointer together**, because the
transport rejects concurrent writes *and* reconnect replaces the pointer.

The VAD numbers are tuned rather than tasteful: 50 ms prefix padding is what
barge-in latency is made of, 800 ms of silence tolerates a real thinking pause,
and low sensitivity at both ends survives a noisy car. Context window
compression is on from the start — without it a call dies at about fifteen
minutes of audio.

`Send` drops frames when the session is nil rather than queueing them. Audio
held across a reconnect would play late, and late audio is worse than none.

The engine implements the optional `SpeechIdler` capability from the model's own
voice activity detection, which is the real idle signal the frame-arrival
fallback only approximates.

### Verifying it

`TestGeminiEngineLive` talks to the real service. It is skipped by `-short` and
without `AGENTIQUE_VOICE_API_KEY`, the same way this repo gates its other
live-provider test. It exists because the things most likely to be wrong — the
API version, the model id, the audio MIME type, the shape of a server message —
cannot be checked by reasoning, only by asking:

```
AGENTIQUE_VOICE_API_KEY=… go test ./internal/voice/ -run TestGeminiEngineLive -v
```

## Not implemented yet

- **The handoff question.** The call always stays open; it never asks "shall I
  stay on the line, or ping you when it's done?". Answering "ping me" would end
  the call and notify instead — a real saving, since only an open call bills.
- **Dispatch is local-only.** A paired machine's sessions are listed, resolved,
  focused and navigated to like any other (they arrive in the world snapshot),
  but `run_prompt` on one refuses naming the machine: dispatch goes through
  *this* server's session service and the report registry is local, so a remote
  run would report into nothing. The composer's Live button stays hidden on
  remote sessions for the same reason — a call opened there could never send.
  Routed dispatch is the multi-machine routing facade's feature, not the voice
  socket's.
- **The confirming phase is a prompt rule, not a state.** The read-back and its
  affirmative are enforced by the system instruction — and hold up well in
  testing — but nothing in the call machinery would stop a model that ignored
  them.
- The `vertex` backend is wired but unverified — it shares the engine, so only
  credentials and the model id differ.
- Android specifics: wake lock, audio-focus interruption, and echo cancellation
  over Bluetooth hands-free — the last of which is the biggest open risk and is
  meant to be tested against the real handset and head unit while the only
  moving part is an echo.
