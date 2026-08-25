# Roadmap

Where agentique is going, what it has already done, and what was dropped along
the way. README.md covers what it is and how to run it; CLAUDE.md holds the
conventions and invariants; `docs/*.md` are the subsystem designs.

## Vision

A GUI for supervising concurrent coding agents across many projects and, now,
many machines. Inspired by [t3code](https://github.com/pingdotgg/t3code) but
built on [agentkit/runtime](https://github.com/allbin/agentkit)'s neutral
provider contract, with Claude and Codex support and the door open for more.

**Why not just use t3code?** Its Node.js and Effect-TS backend had stalling and
crashing process-management problems. A Go runtime with adapters behind one
neutral surface means feature parity moves at SDK pace and provider choice is
per session.

The direction the last year of work points in: the operator's attention is the
scarce resource, and every surface should answer "what needs me, and where?"
before it answers anything else.

## Shipped

Well past MVP. Full detail is in git history; these are the headlines.

**Core chat.** Multi-session chat over WebSocket, a git worktree per session,
tool-permission approve and deny, resume with event persistence, a worktree diff
viewer, reconnect with backoff, keyboard shortcuts.

**Agent workflow.** Merge worktree branches and open PRs, todo visualization,
partial-message streaming, a rate-limit banner, tool classification, state-machine
enforcement, session templates, a hung-session watchdog, and prompt hand-off
cards. See [docs/prompt-handoffs.md](docs/prompt-handoffs.md).

**Channels and hierarchy.** First-class channels with structured agent
introductions, `@spawn` worker delegation with lead auto-approval, a session
hierarchy, and the teams tree with cascade-delete and dissolve. Discussions
(roundtable personas, including sessionless web-only ones) followed. Behind
`[experimental] teams`. See [docs/channels.md](docs/channels.md).

**The brain.** Persistent cross-session agent memory: recall, encode, consolidate,
with a semantic graph, per-turn delta recall, an outcome signal, and a full
management UI. Semantic recall runs in production against Chroma and Ollama. See
[docs/brain.md](docs/brain.md).

**Dynamic workflows.** The CLI runtime's multi-agent orchestration, rendered as a
phase-and-agent tree in a right panel. See [docs/workflows.md](docs/workflows.md).

**The agent's browser.** Headless Playwright in every session, launched lazily on
first use, self-provisioning on a fresh host. The panel became a live view of it
rather than a separate browser. See [docs/agent-browser.md](docs/agent-browser.md).

**Scheduled loops.** Recurring prompts with run history, health, attention
semantics and turn deep-links. M1 and M2 shipped; M2 was built by its own
scheduled loop. See [docs/scheduled-loops.md](docs/scheduled-loops.md).

**Multi-machine.** One UI driving several sovereign servers: pairing with pinned
identity, tailnet discovery, cross-machine project merging by canonical git
remote, per-machine offline caches, a Run-on picker. See
[docs/multi-machine.md](docs/multi-machine.md).

**In-app upgrades.** V1 through V5b: every machine knows what it runs and what is
published, a chip and a dialog across machines, apply with narrated progress and
rollback, a drain gate that waits for idle, and per-machine provider-CLI rows. See
[docs/upgrades.md](docs/upgrades.md).

**Session-first sidebar and the landing page.** The folder sidebar was replaced
with a flat thread list where sessions pin, not projects; plus the stale shelf,
the instrument-cluster footer, the sync dock, work-kind glyphs, unread marks, and
the archived-versus-done split. `/` became a command deck plus a durable
cross-project activity feed; the old grid lives at `/projects`.

**Subagent roster.** An Agents tab whose badge shows agents in flight and failures
from the latest unopened turn, never a lifetime count, plus a flight strip that
survives a tab switch at three densities.

**Live voice.** Spoken dialog that drafts a prompt and hands it to a session
through the composer's own send path, on its own audio socket, with a loopback
echo engine for verifying the browser path and a Gemini Live engine for real
conversation. The loop is closed end to end: the composer's Live button opens a
call bound to that session, which converses, reads the draft back, dispatches on
an explicit yes, follows the run and says what happened. Gated by
`[experimental] voice`. See [docs/voice.md](docs/voice.md).

**A security audit round.** Inbound credentials stored as digests, passkey
recovery gated behind a one-time code, the rekey window closed, path-escaping and
glob record ids rejected, agent-written files never served as active content,
downloads that fail closed, credentials kept out of argv and group-readable
files, and multi-machine communication hardened. The invariants those established
are in CLAUDE.md.

## What's next

### V5c — the CLI update button

The last phase of in-app upgrades, and the only one with a settled contract and no
code. Its dependencies now ship in agentkit v0.2.0. It goes out behind
`[update] cli-updates`, default off, verified against a throwaway server first.
The six-outcome contract and the traps (an updater that reports success and does
nothing, an unknown version that must not wear that accusation) are specified in
[docs/upgrades.md](docs/upgrades.md).

### Presentation sync

Stars, names and colours replicating two-way between machines, so the same repo
reads the same from any UI. Seven decisions settled, five phases planned, no code.
M1 is registers with no network: invisible, and it commits to nothing. The design
is the second half of [docs/multi-machine.md](docs/multi-machine.md).

### Persistent teams, phases 3 to 5

The one coherent unbuilt feature arc in the experimental teams work. Swarms are
ephemeral and hierarchical; teams are persistent and peer to peer. Phases 0 to 2
shipped.

| Phase | Focus | Remaining |
|---|---|---|
| 3: cross-project DMs | Cross-project messaging | `channels.project_id` is nullable now, so the structural blocker is gone. Needs metadata-driven 1:1 routing, or auto-created 1:1 channels. The offline message queue already exists. |
| 4: topology presets | Named preamble modes | `communicationMode` is defined but unread. Inject per-mode routing instructions: spoke (default), mesh (workers talk directly), spoke-plus-request (ask the lead before messaging a peer). Preamble text only, no routing enforcement. |
| 5: autonomy tuning | Gated auto-spawn | Persona confidence threshold as the autonomy dial (the concurrent cap is already enforced), a spawn idempotency key, and partial-failure reporting in spawn audit messages. |

The stance carried from phases 0 to 2: user-gated for now, personas are the
autonomy gateway through a confidence threshold rather than binary flags, and
observe discovery patterns before widening automation.

### Scheduled loops M3

Composer entry point, `CronCreate` interception and promotion, codex context
rotation, server-side history elision, and groundwork for channel-targeted and
fresh-session-per-run modes.

## Maybe

Optional and unscheduled. Pick up if the itch is real.

- **Split-pane session layout**, two sessions side by side.
- **MCP server management UI**, surfacing reconnect, toggle and status over WS.
  The claude adapter already reconnects MCP live; this just exposes it.
- **Desktop app via Tauri**, or an **xterm.js terminal**. Larger bets that diverge
  from the current web-plus-embedded-binary shape. Parked unless the product pivots
  toward an IDE-like surface.

## Open investigations

Design questions, not committed work.

### Teams surface redesign

The session-first sidebar rebuild removed the Sessions and Teams tab strip and the
sidebar teams tree; `/teams` survives as a page behind the More menu. Teams was
underused in practice, and several surfaces the folder sidebar carried are now
gone or different: project folders, drag-to-folder, focus mode,
pin-project-to-focus, per-project expand and collapse, and the activity stream
(partly replaced by the wire on `/`).

A dedicated design session should decide what teams and channel visibility look
like on the flat sidebar — worker counts on lead rows are the only remnant — and
which of the removed surfaces deserve a new home rather than resurrection.

### Sibling-session awareness

Sessions knowing about other active sessions in the same project, to avoid
duplicate work and align on shared interfaces. Build the preamble dynamically at
connect time with a summary of active siblings.

The hard parts: descriptors go stale, because the initial prompt is not the
current focus; the preamble is fixed at connect, so mid-conversation changes are
invisible without per-query system-prompt updates; token cost grows with sibling
count; and over-coordination wastes tokens. Likely opt-in, and only when more than
one sibling exists.

### Cross-session delegation for housekeeping

A merge needs a clean project root. If a local worktree-less session owns the
uncommitted changes, the merge UI could message it — "commit so I can merge" —
now that inter-session messaging exists. The simpler alternative is a "commit all
and merge" compound action that auto-commits with a generated message.

## Dropped

- **`/btw` for auto-naming.** Auto-naming already ships through a Haiku blocking
  runner, so the protocol is not needed.
- **File checkpointing and rewind.** Redundant now that every session runs in an
  isolated git worktree. Git already provides rollback.

## Provider notes

Sessions are driven through agentkit/runtime's neutral contract, with adapters
under `agentkit/runtime/cli/{claude,codex}`.

Provider CLI init takes 30 to 40 seconds on first connect, so the frontend needs a
long timeout for session creation. Turn boundaries come from
`runtime.TurnCompletedEvent`.

Codex supports resume and rate-limit events, and mid-turn send is emulated by
queueing and replaying at the next idle boundary. It does **not** natively support
fork, plan mode, thinking, subagents, compaction events, MCP reconnect, or
tool-progress ticks. The UI checks `Capabilities()` rather than assuming; a
missing capability key means unsupported, so features degrade one at a time
without version comparisons.

Two known gaps, tracked in [docs/tech-debt.md](docs/tech-debt.md): `reasoning_delta`
and `turn_diff` events flow through the backend pipeline with no frontend
renderers, and codex error classification is generic because codexcli-go lacks the
error sentinels claudecli exposes.

Sources: [agentkit](https://github.com/allbin/agentkit),
[claudecli-go](https://github.com/allbin/claudecli-go),
[codexcli-go](https://github.com/allbin/codexcli-go).
</content>
