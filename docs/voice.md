# Live voice

Spoken dialog in the composer. A conversational agent works out *what to ask*
with the operator, drafts the prompt, and hands it to the session that does the
work. The agent never runs the coding job and never sends the message.

The feature is gated by `[experimental] voice`. The loop is closed end to end:
the composer's Live button opens a call bound to that session, and from there
you converse, it drafts, reads back, dispatches, follows the run, and tells you
what happened.

## Shape

```
Browser ──WS(binary PCM ⇄ JSON control)──▶ /api/voice/live ──▶ Engine (speech model)
                                                    │
                                          run_prompt │ Dispatcher
                                                    ▼
                                          Service.EnqueueMessage ──▶ the session
                                                    │
                     VoiceReport (agent) + Notice (runtime) ──▶ back to the call
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

Frame arrival is the weak version of the activity signal — the microphone
streams continuously, so frames keep coming from an empty room. An engine that
performs voice activity detection knows when the caller last actually *spoke*,
and exposes it through the optional `SpeechIdler` capability, type-asserted
rather than required. Engines without it fall back to frame arrival, which still
catches a closed laptop or a dropped connection.

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

`Registry` routes a report to the calls following that session. A call binds
with the `follow` control message and holds at most one session at a time; the
binding is released when the call closes, so a dead socket stops looking like a
listener.

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

The model gets exactly one tool, `run_prompt`. It never runs anything itself:
the prompt goes through `Dispatcher` to `Service.EnqueueMessage`, the same path
the composer's send button uses. One route into the session pipeline whether the
gesture was a click or a sentence.

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

### Verified against the real service

`TestGeminiToolCallLive` drives a real conversation to a real tool call.
`TestGeminiRefusesToSkipTheReadbackLive` asks it to delete all the tests without
confirmation and asserts no dispatch happens — it answers "I must read the
prompt back before I can proceed."

Both are skipped by `-short` and without `AGENTIQUE_VOICE_API_KEY`. The number
of turns before a dispatch is not fixed: the drafter may clarify first, and
always reads back, so the test keeps agreeing until the tool is called.

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
- **Recent transcript is not in the context.** The drafter gets the session's
  identity and the project's CLAUDE.md, but not what the session has been doing,
  so it cannot pick up a thread mid-conversation.
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
