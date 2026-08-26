# The session dock

Everything in a session that is not the transcript lives in one collapsible
panel on the right, reached by one control in the header. The chat is the page;
the dock is what sits beside it.

`SessionDock` is the component. Two other surfaces are also called docks and are
unrelated to this one: `VoiceDock` (the live call, in the sidebar) and `SyncDock`
(the sidebar's git summary). Name the component rather than "the dock" anywhere
the reader might arrive cold.

## What it replaced

Three mechanisms had grown separately, each solving the same problem a
different way:

- **`SessionTabBar`** — Chat, Todos, Changes, Agents, Loops as header tabs, each
  *replacing* the transcript, with the choice in the URL as `?tab=`.
- **A root-level right panel** (`__root.tsx`) with two views, Browser and
  Workflow, reached by two separate header buttons.
- **A todo sidebar** inside `ChatPanel`, above 1024px only, which hid the Todos
  tab when it appeared and could not coexist with the right panel — they wanted
  the same edge of the screen.

## The views

Four, in a fixed order (`DOCK_VIEWS`). Only one is a group.

| View | Holds | Why it is its own tab |
| --- | --- | --- |
| **Work** | Todos, Agents | Both answer "how far along is this turn", and both are true at once |
| **Changes** | Diff and git actions | The output, not the activity; the only view with irreversible buttons |
| **Loops** | Schedules | A different axis: every future turn, not this one |
| **Browser** | Live page | Not session state at all — an external surface the agent drives |

`Work` is the only one with sections, and that is the point: structure encodes
content. Three things being simultaneously true is what makes stacking right
there and wrong everywhere else.

## Derived, not curated

A tab exists because the session has the thing — changes because there are
changes, loops because a schedule exists. Appearing *is* the notification. The
alternative (a launcher, surfaces the user opens and closes) belongs to a
workspace with terminals and files in it; this is not that.

The cost is taken knowingly: derived means hidden, so a session that has never
run a schedule cannot teach you Loops exists. Revisit only if the dock grows a
view nobody stumbles into.

`availableDockViews` computes the set; **nothing else may decide a tab's
existence**, or two surfaces will disagree about what a session has.

## Changes is one scroll, not two panes

`Files` renders every changed file in a single column, each one a foldable
section with a header that sticks while you read its diff (`FileDiffList`,
`FileDiffSection`). There is no file list beside a diff pane any more.

The pane model needed width the dock does not have. At 380px the list took
three quarters of the column and the diff read through a slot; the mobile sheet
needed a whole second arrangement with a "Back to files" step. One column asks
nothing of the layout and reads the same docked, maximized and on a phone.

Files fold themselves once a change touches more than `COLLAPSE_ALL_ABOVE` of
them, because a scroll that opens on forty expanded diffs is not a list of what
changed. Folds are keyed by scope and reset when it changes — a fold is an
opinion about the files you were looking at.

**The scope is picked, not inferred.** `session` is everything since the
worktree's base commit; `working` is only what is not committed yet. The old
view merged both into one list where uncommitted silently overrode committed,
so a file that was committed *and* then edited appeared once, under whichever
label won, and neither question could be asked. The choice appears only for a
worktree session: without a base commit the two scopes are the same diff under
two names.

Note what the RPCs actually return, because the names mislead: `session.diff` is
a **working-tree-vs-base** diff, so it already contains uncommitted edits to
tracked files and lacks only untracked ones. `filesForScope` is where that is
reconciled — the session scope is the session diff plus the untracked files the
uncommitted diff found.

## A diff selection drafts; it never sends

Click a line, shift-click another, and "Ask about this" writes the range into
the composer as a fenced `diff` block with the path, the line numbers and the
enclosing hunk header — then stops. Same contract the live call honours
(`docs/voice.md`): one path into the session pipeline, and it is the send button
the reader can see.

Lines are addressed by **delegation** from the container rather than by making
each line a button: a 2000-line diff would otherwise put 2000 tab stops in front
of the composer. Keyboard readers get the file-level actions from the row menu,
which are reachable, and Escape clears a selection made by pointer. The action
bar renders under the last selected line, never at the end of the patch, and
never floating — in a narrow column a floating bar covers the code it is about.

Only one file holds a selection at a time. Two selections would mean two answers
to "ask about this", and the composer takes one thing at a time.

## Discarding a file is allowlisted, not just validated

`session.discard-file` is the one irreversible thing the view can do — an
uncommitted change has no reflog entry — so it is offered only in the
`working` scope, only from a row's own menu, and only behind a confirmation.

The path is untrusted. `gitops.SafeRelativePath` refuses traversal before the
path reaches a git argument list, but **the allowlist is what makes it safe**:
the path must already appear in git's own list of changed files for that
session, so a path git does not report as changed cannot be discarded whatever
it points at. What "discard" means then follows from the status git reported —
restore from HEAD, drop a staged addition, clean an untracked file, or undo
*both halves* of a rename, since porcelain reports one as a single
`old -> new` entry and undoing half of it leaves two copies.

Nothing else reaches it: not an agent, not a schedule, not a voice call.

## Three things ride one task stream

The provider CLI reports a subagent, a backgrounded shell command and a workflow
as the same kind of event, distinguished only by `taskType`
(`local_agent` / `local_bash` / `local_workflow`). Only the first is a subagent,
and in a long session the second outnumbers it roughly forty to one — a session
that ran `make check` in the background all afternoon and spawned two agents had
sixty-odd rows, sixty of which were shell commands wearing an agent's row.

`isSubagentRun` decides membership **once per run**, from two signals:

- the run's **sticky** `taskType` — the first non-empty one seen for that tool
  use, because older CLIs stamp it on `task_started` and leave it empty on every
  later event. Judged per event, a workflow's terminal notification arrives
  untyped, misses the exclusion, and invents a roster row for a workflow that
  `WorkflowActivity` is already rendering properly.
- the **spawning tool call's name**, which is ground truth when a task carries
  no type at all: a background shell's task points at `Bash`, an agent's at
  `Agent`/`Task`.

Unknown on both counts is excluded. Every `task_started` the CLI writes today
carries a type, so the ambiguous case is old history, and a stray background
command is a worse row than a missing one.

## Workflow is not a peer of Agents

A workflow's agents are not `AgentRun`s. They ride the workflow's own
`task_progress` events as `WorkflowProgressEntry` values, carrying a phase, a
label and a state but **no report and no narration**; `collectAgentRuns` drops
`local_workflow` deliberately so the synthetic workflow task does not sit in the
roster pretending to be an agent.

So the two cannot share a row type, and `WorkflowActivity` renders whole inside
the Agents section rather than being merged into it. Same subject at two
altitudes, one heading, two renderings. Anyone tempted to unify the row model
should re-read this paragraph first.

## The roster shows the turn, not the lifetime

`scopeAgentRuns` splits landed runs into this turn and everything earlier, and
the dock folds the second group behind one disclosure.

This is the same argument `agentBadgeState` already makes about the badge: a
number that only grows trains you to stop reading it, and in a 300px column a
lifetime roster is a wall in front of the two agents you came for. Nothing is
discarded — an old agent's report is one click away, which "an agent is
readable, not just reportable" requires. Lifetime totals stay in the footer.

Runs still streaming carry no `turnIndex`; they belong to the latest turn, and
the fallback in `scopeAgentRuns` puts them there. Attributing them to "earlier"
would fold away the very agents the dock exists for.

## A failed subagent is not the operator's problem

The Agents badge reports agents **in flight**, and nothing else
(`agentBadgeState`). A subagent that failed does not raise it, and neither does
the collapsed dock's aggregate mark.

The parent session reads the failure and acts on it — usually by trying again,
often within seconds. Raising it to the operator said "this session needs you"
about a turn that was proceeding fine, which is the way to teach someone to stop
reading badges. The outcome is not hidden: the row still carries its mark for
anyone who opens Work, and the footer still counts it.

`stopped`, `killed` and `cancelled` are a **state of their own**, never `failed`.
The CLI reports them when the agent shut a run down on purpose — the dev server
it started for a screenshot, the tail it no longer needs — and roughly half of
everything previously painted red was that. A grey dot and the word "Stopped"
report it honestly.

Loops are the other way round and keep their failure mark: an auto-paused
schedule stops running until a person acts, so it is worth interrupting for
(`loopBadgeState`, and the `failed` branch of `dockAlertState`).

## Collapsing costs information, so the toggle carries one mark

Per-tab badges go dark when the dock shuts. `dockAlertState` compensates with a
single aggregate on the toggle, ranked the way the app ranks everything else
(`lib/session/priority.ts`): waiting-on-you, then failed, then live.

**One mark, never a summary.** A button trying to report three states at once
reports none of them. The glyphs are the sidebar's (`ThreadRow`): X is "it
failed", the triangle is "someone is waiting on you", a pulsing dot is live
activity. Here `failed` can only be a paused loop; see above.

## State is per session, and therefore versioned

`ui-store.dock` keys `{open, view}` by session id: what you had open beside a
piece of work is a property of that work. Width and maximization are not — they
are viewport preferences and stay global.

Two consequences that must not be dropped:

- **The map is unbounded**, so it is pruned in insertion order at
  `MAX_DOCK_SESSIONS`. The oldest sessions to touch their dock forget first.
- **A stored view outlives its subject.** `resolveDockView` is the reconciler: a
  view whose thing is gone falls back to another available one rather than
  collapsing the dock, because collapsing would read as the user's own gesture.
  It returns null only when the session has nothing at all to show.

The persisted shape is versioned with the rest of `agentique:ui` (v6, which
folded away `rightPanelCollapsed`, `rightPanelView` and `todoSidebarCollapsed`
and carried `browserPanelWidth` forward as `dockWidth`).

## URL

`?dock=work|changes|loops|browser` opens the dock on that view. Closing clears
the param rather than spelling out "closed": the dock remembers its own state
per session, so an empty URL means "as I left it", not "shut".

`?tab=` is still **read**, never written — those links sit in clipboards and in
deep-links this app minted itself. `legacyTabToDock` owns the mapping so the next
rename has one place to look. `?tab=chat` maps to nothing, because chat is the
page now rather than a view.

## Mobile

The same `SessionDock` component, rendered inside a right-side `Sheet`. One
navigation model, two presentations — a second model on mobile is how the chrome
fragmented in the first place.

Maximize is offered only where there is a pane to take over, so
`onMaximizedChange` is simply not passed on mobile and the control does not
render. A button that does nothing when pressed is worse than no button.

## The flight rail still follows you

`AgentFlightStrip` at `rail` density stays mounted beside the composer and is
suppressed only while the dock is **open on Work**, where the board says it
louder. The invariant it was written for — a live-status surface that does not
disappear when you navigate — gets easier to hold here than it was with tabs,
because the chat branch is now always rendered.
