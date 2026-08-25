# Prompt hand-offs

Turning "here is a task someone should run" into a session the user can start with
one click. Two mechanisms, deliberately layered.

- **`SuggestSessionPrompt`** — an MCP tool the agent calls. Renders as a
  `PromptCard`. Structured, typed, persistent.
- **Run on any code block** — a control on every fenced block in chat that opens a
  prefilled new-session composer. Deterministic, model-independent.

The second exists because the first is probabilistic. An agent can always choose
to write a plain fence instead of calling a tool, and when it does, the Run
control makes that fence runnable anyway. Reliability does not depend on the model
volunteering structure.

## Why the suggestion is a tool call

Agents used to emit suggestions as freeform markup in their reply text:

```
<agentique type="prompt" title="…">
…body…
</agentique>
```

A frontend state machine parsed those out. It was the only structured payload
agentique asked a model to hand-write; everything else structured (send a message,
rename the session, add a memory, lease a dev URL) went through the in-process MCP
server as a schema-validated tool call.

That asymmetry was the bug source. Agents semi-regularly malformed the closer,
ending the block with `</parameter>` or `</prompt>` instead of `</agentique>` — a
bleed from function-calling syntax, where attribute-bearing tags close with
`</parameter>`. The parser recovered every known variant into a clickable but
warned card, which is a mitigation rather than a cure: the malformed class still
existed, and the recovery machinery was non-trivial (state machines, lookahead,
depth tracking, recovery boundaries).

Making it a tool call removes the class structurally. Arguments are
JSON-schema-validated, so a malformed call is rejected and the model retries.

Two things came free. The `tool_use` event is already written to `session_events`
and rebuilt on reload, so a card survives a refresh with no extra storage, unlike
inline text that must be re-parsed from prose on every render. And both providers
get the same MCP config with no provider-conditional logic, so the tool works on
codex without a second implementation.

The tool is simpler than `SendMessage`, which uses a deny-with-success interceptor
because it has to stop the CLI's own MCP handler from acting *and* route the
message through the event pipeline. `SuggestSessionPrompt` has no side effect to
route: the card renders purely from the tool_use event. So it auto-allows and lets
the benign handler acknowledge.

Rendering reuses the `channel_send` interception pattern in `segments.ts`: match
the tool by full name before the generic tool_use case and emit a
`suggest_session` segment instead of a tool block. Consecutive calls with nothing
between them group into one segment, so several suggestions render under a single
"Start All" footer; intervening content breaks the group.

## Run a code block as a new session

Every chat code block gets a **Run** control beside Copy. Activating it opens the
new-session view for the chosen project, prefills its composer with the block's
contents, and **does not send**. The user edits, picks model and worktree options,
then sends, and that is when the session is created.

The dropdown lists the current project first, then every other project. With one
project it collapses to a plain button. Mermaid blocks and empty blocks are
skipped.

### There is no "run here"

A block's Run always creates a *new* session. The active session already has the
block in its context, so re-running it there means nothing. This also removes all
coupling to a mounted composer, which was the hard part of the original design.

### Deferred creation is what makes it orphan-free

`NewChatPanel` already defers session creation until first send and prefills from
a draft keyed `new:<projectId>`. So the whole primitive is: set that draft, then
navigate to the new-session route. The panel mounts, reads the draft as its
composer's initial text, and creates the session only on send. **No session and no
worktree exist until the user commits**, so abandoning a draft costs nothing.
Cross-project is the same call with a different slug.

The helper appends to an existing draft rather than replacing it, so a
half-written new-session prompt is never clobbered.

Two alternatives were rejected. A `?prompt=` search param (the route supports one)
turns a large block into a multi-KB URL, with history noise and length limits.
Creating a real session first and then navigating provisions a worktree on every
click, which orphans one on every abandonment.

`PreBlock` is a generic markdown renderer used outside sessions too, in agent
messages and discussions. When no active session resolves a current project, the
control shows the bare project list; with zero projects it hides.

## The preamble nudge

`presetSuggestParallel` was worded around "independent work that could run as its
own parallel session", which missed the hand-off case: a spec written for another
repo's agent, which is exactly when a model tends to emit a plain fence. The
wording now covers both, and the cross-project block says explicitly that writing
a prompt for another repo means setting `project`.

This is model-dependent, and that is acceptable because the Run control is the
deterministic backstop.

Deliberately not done: an output-scanning lint that guesses whether an arbitrary
fence was meant to be a prompt. False positives, and it regrows the parser surface
this design is trying to shed.

## One prefill primitive, two callers

`PromptCard` has an **Edit before running** item that calls the same
`openPrefilledNewSession` helper as the code-block Run, targeting the card's
resolved project. So a card's prompt can be reviewed and edited in the new-session
composer before it spawns.

The primary Start Session button and Start All keep their one-click behaviour.
Edit-before-running is a trailing item; with only one project, a caret opens a
one-item menu.

## Retiring the inline parser

The inline authoring format is redundant for new output now that there is a
structured path and a deterministic floor. Retirement is staged, and none of it is
done yet:

1. Retire the **recovery machinery** (`findRecoveryBoundary`, `RECOVERY_WARNINGS`,
   the warning chip). It only ever mattered for malformed *authoring*, which the
   model no longer has to do.
2. Retire the `<agentique>` preprocessor, gated on no new inline blocks appearing.
3. Keep `findRawPromptBlocks`, `parsePromptFromCode` and `splitByPromptBlocks`
   indefinitely for **legacy rendering**. Old `session_events` rows still contain
   inline blocks and have to keep rendering.

The retirement is easier if `prompt-parsing.ts` is first split so the
`<agentique>` handling sits in one deletable module.

## Open

- **Adoption is unmeasured.** Inline text is free, part of the reply; a tool call
  is a deliberate act. Whether agents reach for an optional tool as readily as
  they emitted inline cards is the one thing the code cannot predict. The measure
  is suggestion rate per eligible turn.
- **Cards are timeline blocks, not woven mid-prose.** A tool-sourced card appears
  in the activity timeline rather than inline in "parallelize these: [card]
  [card]". Uniform rendering is worth it, and the model can still introduce them
  in prose.
- **A tool call is atomic**, so there is no progressive pending card while it
  streams.
</content>
