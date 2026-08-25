# Live voice

Spoken dialog in the composer. A conversational agent works out *what to ask*
with the operator, drafts the prompt, and hands it to the session that does the
work. The agent never runs the coding job and never sends the message.

The feature is gated by `[experimental] voice`. What exists today is the
transport and a loopback echo engine; the speech backend is not implemented yet.

## Shape

```
Browser ──WS(binary PCM ⇄ JSON control)──▶ /api/voice/live ──▶ Engine
```

Three participants, and keeping them separate is the design:

- **The ear and mouth** — a realtime speech engine. Owns voice activity
  detection, barge-in, turn-taking and speech synthesis. It has no tools, never
  sees a repository, and never reaches the session pipeline.
- **The drafter** — a warm, tool-less Claude agent that knows the project and
  turns the conversation into prompt text.
- **The workhorse** — the existing session, unchanged. It receives text through
  the composer like any other message.

The ear and the drafter are deliberately not the same agent. Merging them would
mean shipping `CLAUDE.md`, the file tree and session history to the speech
vendor on every call, and would still have a model that has never seen the
codebase writing prompts for one that has.

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

## Idle is a billing guard

A live speech session bills for **wall-clock time with the microphone open**.
Unlike every other cost in agentique, an abandoned tab keeps spending until
something closes it. `[voice] idle-timeout` closes a quiet call.

Frame arrival is the weak version of that signal — the microphone streams
continuously, so frames keep coming from an empty room. An engine that performs
voice activity detection knows when the caller last actually *spoke*, and
exposes it through the optional `SpeechIdler` capability, type-asserted rather
than required. Engines without it fall back to frame arrival, which still
catches a closed laptop or a dropped connection.

Costs stay out of the UI. This is an operational limit, not a price display.

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

Those need a runtime push, ordered by `lib/session/priority.ts` — the same rule
as the deck's Needs-you band, not a second opinion about what deserves
attention. Everything else comes from the worker.

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

`ReportingInstructions` is appended to a drafted prompt **only** when the
operator chooses to stay on the call. Decline, and the prompt carries none of
it: no instruction, no tool calls, no reporting overhead. That is the whole
reason the handoff asks rather than assuming.

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

## Not implemented yet

- The speech backends. `aistudio` and `vertex` parse and validate, and their
  engines return "not implemented".
- The drafter agent and its project context.
- The browser client: AudioWorklet capture, playback scheduling, the Live panel.
- Session resumption across the connection lifetime limit.
- Android specifics: wake lock, audio-focus interruption, and echo cancellation
  over Bluetooth hands-free — the last of which is the biggest open risk and is
  meant to be tested against the real handset and head unit while the only
  moving part is an echo.
