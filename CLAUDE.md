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

On Windows the model inverts: orphans are prevented, not reaped. Serve confines
itself and every descendant in a kill-on-close job object
(`procctl.ConfineProcessTree`), so the CLI subtree dies with the server however
it exits, and both reapers stay no-ops there by design. Graceful stop is the
named stop event (`procctl.RequestStop`), never `schtasks /End` first — /End is
a hard TerminateProcess that skips the drain.

**A reclaim reports itself, because it borrows the stop button's mechanism.**
The sweep goes through `StopSession`, so what it leaves behind is a row no
different from one somebody stopped on purpose — and every surface read it that
way, announcing "Session interrupted" for a session nothing had interrupted.
`sessions.evicted_at` is the only thing that separates them, so the sweep writes
it and nothing else does: a restart's reap and a person's stop both really did
end something. It is stamped **before** the stop, because the mirror on
`Session` exists for exactly one snapshot — the `stopped` push the eviction
itself broadcasts — and a reason arriving a refresh later means drawing the
banner and taking it back. Resuming clears it, so it always describes the most
recent stop. Suggest a TTL in hours: shorter than a break, and the sweep
reclaims sessions somebody is still working in.

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

Bulk *destructive* actions key on whether the branch's commits already exist on
the project's HEAD, never on archived. Archiving is a one-click tidy, up to a
whole-shelf sweep; only work that survives the deletion is safe to delete in
bulk. `worktree_merged` is **not** that test — it records that agentique itself
performed the merge, so it is false for every branch merged from a terminal, and
gating on it left the affordance unreachable on repos worked that way. Ask git
(`storage.Evaluate`, `docs/storage.md`); the flag stays as a fast path only.

The reversible verb needs none of this. Reclaim frees a session's disk and keeps
its row and branch, so it applies to any finished, clean session — archived ones
included. Reach for it before widening what Delete accepts.

UI copy says "Archive"; `done` reads as "finished" wherever it surfaces.

### Where a destination lives

Three homes, and what a thing **is** decides which one it gets, never how often
it is opened.

The sidebar's ⋯ menu lists **places where work lives** — somewhere that reports
something on its own: channels with traffic, a brain that flares, loops that
run. Settings holds **registrations**: a fact you set once and leave, which is
why Projects (a path, a name, a colour, which machines hold a checkout) sits
beside Machines, and Templates (saved text plus saved settings) beside them. The
sidebar footer holds **this machine's housekeeping** — Storage behind the disk
meter, Settings behind the account button.

A destination gets **one** home. Storage and Settings were in the menu as well
as the footer, which was length, not reach. And an action taken *on* a listed
thing belongs on the surface that lists it, never as a peer of it — that is why
Discussions is a control on the Teams page's profile section.

"Behind the disk meter" is literal: `splitMetered` draws the compact indicator
with two controls, because its halves lead different places. The allowances open
the usage popover; the disk gauge is a `Link` to `/storage`, so that popover
carries **no** Storage *nav* row. A level is a reading *of* a page, which makes
the meter the better home — it is also the reason you would go — where an
allowance has nowhere to go beyond its own reset.

Which means **every** disk meter is that link, the popover's own disk section
included (`UsagePanel`, keyed on `STORAGE_AGENT_ID`). It draws a section per
renderable agent, so it draws one for the gauge too, and an identical-looking
reading that answers nothing when tapped differs from the gauge only in the
respect you cannot see. It is also the target that works on touch, where the
compact gauge is 24px wide. Allowance sections stay inert.

A moved route keeps its old path as a `redirect`: `/projects` and `/templates`
are in bookmarks and in deep links this app minted.

**Navigating dismisses the mobile sidebar, and that rule lives at the router.**
On mobile the sidebar is a Sheet over the whole viewport, so it *is* the
navigation surface and arriving somewhere finishes its job; leaving it up hides
the page it just opened, which reads as a dead link. `useSidebarDismissOnNavigate`
(`lib/sidebar-nav.ts`) watches the location rather than each control, because
the ⋯ menu and the footer popover render into portals — a delegated handler on
the sheet never sees them, and a link added to either would silently miss a
per-control dismiss. What it cannot see is a link naming the page you are
already on; those few controls call `dismissSidebar` directly.

### The footer line indicates with marks, not sentences

That row is 271px and already carries identity, liveness and the usage cluster —
which **grows**, since the set of allowance windows is never hardcoded. So
everything right of the account name is `shrink-0` and the name truncates: a
mark's width is what it means, and the name is the only thing on the line that
can give ground. A pill spelling "Rebuild available" wrapped to two rows and
pushed the codex and disk marks outside the sidebar.

`UpdateMark` is therefore a glyph, on the same argument the sidebar row settled
for parked sessions. The words are not lost — `UpdatePopoverRows` renders the
label, the detail and the button one click away, so the pill was spending a third
of the footer duplicating what it fronts.

It **leads the usage cluster** from inside that cluster's own trigger, inline
rather than notched onto a corner: a mark overlapping the last vendor's logo
reads as a claim about that vendor, and one in the gap is dead pixels beside the
control it is about. Inline it costs width, which is honest — the name pays.
`lib/update-mark.ts` owns what a kind looks like and **both** surfaces read it —
the mark and `UpdatePopoverRows` — on the `REST_GLYPH` precedent, so a row cannot
show a different picture from the mark that opened it. `MARK_GLYPH` is not four
pictures: a release and a rebuild are the same offer (a newer build, costing the
current turn) and share `ArrowBigUpDash`; only **restart** differs, because it
fetches and compiles nothing, which is the split `sourceVerdict` already makes.
That glyph means **only** upgrade, where a circled up-arrow is also scroll-to-top
and collapse — and since it shares the stroke vocabulary of the meters beside it,
it carries its own weight instead: heavier strokes, 14px against their 11px
marks, in the accent colour, which is what separates asking from reporting.
`GitBranch` held the slot and was wrong twice: it named the channel rather than
the offer, and it lost its branch node by 12px the way `FolderGit2` does at 10px.

`useUpdateWaiting` is the one predicate behind both the glyph and the button's
tooltip and accessible name, and that trigger stays mounted for a machine with no
windows but a waiting update, because it is what the mark rides.

Nothing here can be dismissed. The old chip's × existed because a sentence in the
footer is loud; a glyph is not, and an update that can be waved away is one
nobody applies.

### A row names a project by its name

`lib/project-label.ts` is the one place that turns a project into the label and
initials a row shows, and it reads the **name**. A slug is an identity — it
routes, it is unique per server, and it is derived from the name *once*, at
creation — so it lags a rename and can only spell `[a-z0-9-]`. A row showing it
reported the wrong thing twice: the name the project used to have, and an ASCII
flattening of it always.

The name also needs no `displaySlug`: only a slug carries the `~machineid`
qualifier, so nothing has to be stripped before it is shown.

Both slugifiers must agree, because two of them derive slugs from the same
names: `Slugify` in `backend/internal/project/slug.go` (create) and `slugify`
in `frontend/src/lib/utils.ts` (the rename dialog). Both **transliterate** a
letter rather than treating it as punctuation — replacing everything outside
`[a-z0-9]` with a separator turned "Träffbild" into `tr-ffbild`.

### The header is the session; the composer is the next message

One seam, stated once: the top bar is about **the session as an object** — where
it lives, and what you do to it — and the bottom bar is about **what happens
when you press send**. Nothing that changes by itself belongs on either.

That is why run state left the header. The transcript streams it, Send reports
it by becoming Queue, and the approval banner is pinned above the composer
regardless of scroll — which was the status pill's whole argument for existing.
`SessionStatusPill` survives for the dev gallery; the header does not use it.
For the same reason the project checkout's push/pull went back to the sync dock
(a different working directory, and the dock reports how stale its count is),
and effort came *down* to sit beside the model.

**Model and effort are one control** (`BrainControl`): which brain, and how hard
it thinks. The trigger is the model name plus a five-bar meter, because a meter
reads as a *quantity* — that is what stops the level looking like a second
dropdown, and it is the only form where Max differs visibly from XHigh at 11px.
Inside, models are a list and effort is a **ramp**, drawn locked or live from one
flag: there is no `session.set-effort` anywhere, and the provider did not accept
a mid-session change when last checked. That flag is the only difference between
this and the new-session panel's copy, so both surfaces render one component.

**The permission mode is a mark, not a label.** It is almost always Full Auto,
which argues for demoting the word and never for dropping the fact —
`ui-store.lastUsed` refuses to carry the mode between sessions on exactly that
reasoning. The glyphs are transport controls, so the silhouette carries the
meaning and nothing rides on a 4px interior detail (which is how the shield
family failed at 12px): a **hand** stops, **play** runs, **fast-forward** does
not stop. Not `TriangleAlert`, which is the obvious glyph and is already spoken
for — it means "someone is waiting on you" in `ThreadRow`, `DockToggle` and
`DockTabBar`, and one mark means one thing across surfaces.

There is no Chat/Plan toggle. Plan *review* is untouched — `PlanReviewBanner`
runs off an approval the CLI raises when the agent exits plan mode itself — and
`caps.planMode` plus the template field stay, so a template can still start a
session in plan mode.

**On mobile the composer is one flush row, and everything that is not the next
message is behind the `+`.** No card, no outer padding, no second row: the tray
toggle, the field and Send share a line, and the model, the effort and the
permission mode live inside the tray with attach and templates. That reverses
what this file used to say — that the two of them stay outside the tray,
because a phone hiding both "answers neither" — and the reversal is the
decision, not an oversight. The row cost 48px on a screen with 427, the mode is
almost always Full Auto, and neither is read on the way to sending a message;
they are settings, and settings are what a tray is for.

Nothing took their place on the header's metadata line either. That line
reports (`sublineSubject`: live work, then a parked loop, then the resting
state) and its right-hand side carries the branch cluster; a metadata line that
also carried controls stopped being one. The desktop is unchanged — it has the
width, and the toolbar stays.

`ComposerTextarea` grows an `inline` layout for this rather than a second
component: one composer, two arrangements. Its textarea is `display: block` on
both, because a textarea is inline-block by default and the line box its parent
opens adds ~6px of leading under it — which is what put the placeholder half a
line above the `+` beside it.

### The New-session panel remembers model and effort

`ui-store.lastUsed` carries model and effort from the last session **created**
into the next New-session panel, and nothing else does. The other three session
defaults are safety-shaped — `autoApproveMode` decides whether an agent asks
before acting — and a mode silently inherited from days ago is the kind of
default nobody reads. Model and effort are a working habit.

It is written at creation, never on selection: opening a dropdown and closing it
is not a use. It is read **once**, as the panel's initial state, so a session
created in another tab cannot move the dropdowns under someone mid-compose. One
pair, not one per project — which model you want belongs to the task, not the
repo, and a per-project map would need the pruning `dock` needs.

Nothing marks it in the dropdowns. The trigger already shows the carried-over
value as the current selection, which is the whole of what there is to report;
a second mark saying "and this is also what you had last time" is the same fact
twice. `ModelId` is a bare string by design (`docs/model-catalog.md`), so a
remembered id can name a model this build no longer offers — the catalog
resolves what it can, and a stale one reads as a visibly wrong dropdown rather
than a broken one.

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
outcome must choose its mark rather than inherit a blank. Every surface that
shows sessions reads that one table — the sidebar rows, the landing deck's
cards and the Storage page's rows — because a session that says "evicted" in
the rail cannot say "stopped" on the overview. Grey stays the shelf's and
Archived's language: both render `compact`, which is grey and collapsed by
construction.

**A session whose machine is unreachable is filed, and that is not a
contradiction** — it is the one shelf keyed on something other than the work.
Pin, archive, merge and open all route to the machine that owns the session
(`getRoutingClient`), so while that machine is away the row cannot be acted on
at all, where an evicted or reaped one wakes on the next message. `isAway`
decides it from the same reachability reading `deriveRestToken` turns into the
`away` token, so the shelf can only file what the row already says; if that
reading proves twitchy the fix belongs there, in the one place both surfaces
read, never in a second timing rule. Pinned outranks Away (a gesture outranks a
passing fact) and Away outranks "Finished earlier" (whose Archive-all would
fail on every such row). Three rows are exempt, all still owed a look: a
blocked one, an unread completion, and the session open right now — this
filing is not a gesture and lands the instant a machine drops, so without the
last one the row you are reading collapses underneath you. Its rows carry no
pin or archive button either, on the rule that already drops Archive mid-turn:
no button that can only fail.

**Live is a mark that travels, and it is the only thing in the rail that
moves.** `LiveMarks.tsx` draws one shape at two radii — a bright arc on a faint
track — around the chip (`ChipComet`) and in the time slot (`OrbitArc`). Travel
rather than pulse is forced by the size: at 10px an in-place mark signals
through one property at one point, which peripheral vision does not resolve,
where a path 60px long is caught before it is read. **The track is
load-bearing**, not decoration: it is what the mark looks like at rest, so a
glance landing on the arc's empty side still sees something, and
`prefers-reduced-motion` holds the head still on the track rather than removing
the state. One period across every row and no per-row stagger — marks that drift
into phase read as one system, six phases read as noise.

`isRunning` gates it, and is deliberately narrower than `isAwake`: a row blocked
on an approval or a question is awake and emphatically not running, and
animating it would contradict the amber triangle beside it. `merging` counts,
because git is working. Colour stays the project's, so hue means filing and
motion alone means live.

**The orbit replaces the age rather than crowding it**, and only while running,
which costs nothing — a running session's recency is "now", so the number is
least useful at exactly the moment the mark is most useful. The clock keeps the
slot everywhere it still answers something, which is what makes Open's recency
sort readable.

**It replaces the clock without taking its corner.** That corner is also where
the row's pin and archive come in — on hover, and for as long as the row is the
focused one — so anything sitting there has to be able to yield, and a mark
meaning *running* cannot: it went dark on the row under the cursor and the row
you were inside, the two the question is asked about. A reservation is worse
either way round. Hold the corner open and the buttons ride a target that moves
on its own when the turn ends, one of which archives; slide the orbit aside as
they arrive and the row's one moving mark moves for a reason that is not
liveness. So it sits at the right edge of the **title** line, the shelf the row
already keeps for marks that cannot yield (`NewMark`, on the same argument),
directly below the clock's column and clear of the buttons at both densities —
`RowActions` takes `max-md:top-0` to keep that true where the touch targets grow
to 24px. `new` is the one thing that can share the line and steps left, because
the orbit is drawn last and its x never moves.

**A parked row wears its mark on the chip, not in words.** `stopped`,
`evicted` and `away` are one concept — `isParked`, the process is gone, the
work is not, and the next message resumes it either way — so in the rail they
are one mark: `PARKED_GLYPH`, a moon, in a notch cut from the chip's
bottom-right. The word is dropped. It was the row's longest string for its
least consequential fact, and giving it up is what lets `finished` and `merged`
read louder without being made louder; those keep glyph *and* word, because an
outcome is worth a read. The word survives in the chip's tooltip and the row's
aria-label, and the deck's cards — which have the room — still print both from
`REST_GLYPH`.

**The Storage page's rows follow the same rule** (`StateMark`), where the
argument is stronger still: `stopped` is most of what that page lists, and it
is routinely the wrong reading of *why* — a session evicted to reclaim memory
or reaped by a restart is indistinguishable there from one somebody stopped on
purpose. So parked is the moon and nothing else, while `done` prints as
"finished" (never "done" — that verdict is Archive, the badge beside it) and a
live state keeps its word, because that is what answers the question the page
is read for: why this row cannot be reclaimed. `merged` stays its own badge, a
fact about the branch rather than the process, which is why `StateMark` asks
`deriveRestToken` with `merged: false`.

It rides the chip for the same reason the unread notch does: the chip is the
one element at a constant x on **every** row shape, so a mark there is found by
glancing down a column instead of read. Anywhere on the repo line, its position
moves with the project's name. The two notches are cut at **opposite** corners
so one row can wear both, and `.chip-notched.chip-rest-notched` must
`mask-composite: intersect` — mask layers compose with `add`, and each gradient
is opaque outside its own hole, so their union fills both holes back in.

The glyph is a moon and not `CircleStop`, which is a stop *button*: it offered
an action on a row where nothing is running. One moon covers all three parked
tokens where `REST_GLYPH` has three, and that is forced as well as principled —
the corner mark is 9px, where `Unplug` and `CloudOff` turn to mush.

### Where a session's code lives

Which machine, and which worktree on it, are two segments of one address, so
they are **one element** — `SessionLocation`, two zones, reading
`lib/session/location.ts`. As two chips with four other things between them
nothing said the case that costs most: the *main* worktree on a *remote*
machine, which is the only state that lights both zones.

The vocabulary is git's own, from `git-worktree(1)`: a repository has a **main**
worktree and zero or more **linked** worktrees. Never "local" — that word now
means the machine — and never an invented one ("live repo", "root"). Both zones
name a **branch**, so the kind rides the glyph and the colour rather than a
word: a linked worktree shows its own branch quietly, the main worktree shows
the project's branch in amber. `projectGitStatus.branch` arrives on a push that
can land after first render, so the main case falls back to the words "main
worktree" rather than an empty zone. `worktreeKind` reads `worktreeBranch`, not
the path, which is set for both. `FolderOpen`, not `FolderGit2` — at 10px the
branch node inside the folder collapses into noise.

**Zone 1 is always present, including for this machine.** Absence is not a
signal you can trust: it reads the same as a bar that has not loaded, and an
address that is sometimes two segments and sometimes one cannot be compared
between two sessions at a glance. The local host is named in neutral ink with no
hue and no dot — stated, not announced.

The hue is **derived, not stored** (`lib/machine-colors.ts`): `getProjectColor`'s
rule — explicit wins, else sort the ids and index the palette — pointed at
machine ids, so no schema, no catalog replication and no picker. The primary
gets none, because the header's wash means "somewhere else". The wash **drains
to grey when the machine is away**, in the same moment the composer disables
itself: two quiet signals agreeing is what makes a pane visibly go cold. It is
recognition, never identification — a colour cannot be named, so zone 1 says it
in words for anyone who has not learned it.

**One subject, one popover.** Each zone opens only what it is about; the name's
own popover keeps rename, icon, pin, ref and archive. The exception is the
phone, where a 22px zone is under the 44px touch target: there the whole pill is
one target opening one sheet with both sections.

Before a session exists the same element is the **picker** (`LocationPicker`, in
the new-session hero) — same shape, same zones, so what you choose is what you
will see in the header. The machine zone is a menu (n machines); the worktree
zone is a **toggle**, because worktree-vs-main is one bit with no third option
coming, and a dropdown was two clicks and a read for it. It replaced a host picker in the hero
and a Worktree/Local toggle in the composer, 400px apart. It is deliberately not
a composer control: that would fork the new-session composer from the in-session
one, and at `tray` density the phone's row cannot hold a two-zone pill, so it
would fall into the tools tray — the one place a location must never be.

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

### Storage and reclamation — `docs/storage.md`

Two verbs, two bars. **Reclaim** frees a session's disk and keeps its row and
branch, so it needs only finished-and-clean and applies to archived sessions.
**Delete** is irreversible and asks git whether the branch's commits are already
on the project's HEAD — never `worktree_merged`, which only records that
agentique performed the merge.

Verdicts fail closed: anything git cannot answer is `unknown`, and unknown is not
safe. The dirty check runs *before* the merged fast path, because a merged branch
with uncommitted edits still has something to lose. It sees untracked files but
not ignored ones, so Delete's copy must admit that a local `.env` goes with the
worktree.

`internal/janitor` is a pure planner and the single mechanism behind the page,
the startup sweep and `agentique prune`. Its guards are load-bearing: an empty
session set reaps nothing, an unrecognised project is spared, a live session is
never touched. `prune` opens the database read-only — a command that reports what
it *would* delete must not migrate the live database to do it.

`POST /api/storage/reclaim` re-plans server-side and intersects with the request,
so a stale client narrows the set and never widens it. Reclamation never runs on
a timer; the startup sweep stays orphans-only.

**The breakdown is grouped by what you can do about a row, not by where it
lives** (`lib/storage/breakdown.ts`): `live`, `sweep`, `policy` decide the
colour and the order. Worktrees split three ways — live, finished, orphaned —
and the split reconciles exactly to the backend's category total rather than
being estimated. `reclaimableBytes` on the wire is worktree **plus** temp, so
the finished row sums `session.bytes` itself; reading the wire figure there
double-counts the temp rows below it.

A row carries a verb only where the server can perform it *and* it would do
something. There are three, and two of them are irreversible, so only Reclaim
wears the green:

**Trimming backups** touches the periodic namespace only. Pre-migration
snapshots (`agentique-pre-*`) are deliberate safety copies that agentkit's own
retention exempts, and `keep` counts periodic files alone — otherwise a
directory full of snapshots would satisfy `keep` while every periodic backup was
deleted. `keep` is clamped **up** to `minBackupsKept` server-side, because the
number comes from a client that could ask for zero.

**Scratchpads agentique does not own** are reported (`TempKindForeignScratchpad`),
never swept. The kind is the guard: they are attributed to no session, so no
reclaim can reach them. Removal is per directory, from a list, because it is the
only verb here that touches something agentique did not create — and
`safeForeignScratchpadPath` accepts only a *direct child* of the scratchpad root
that does **not** carry the worktree prefix. One that does belongs to a session,
goes when that session is reclaimed, and would break a running CLI if removed
underneath it.

### Channels and teams — `docs/channels.md`

The `messages` table is the source of truth for channel timelines. Informational
channel metadata (introductions, spawn notices) is **not** mirrored into session
events; new informational message types extend the existing skip list.

**Additive principle.** Channel features leave session rendering, event-pipeline
mutations and turn management alone for any session outside a channel.

**Teardown is two verbs, reversible before destructive** — the same split
`docs/storage.md` draws for disk. `@release` archives the lead's own idle workers
through `ArchiveSession` (keeps branch, worktree and row; refuses a busy worker by
`TurnInFlight`; skips a multi-channel one, since archiving is global); `@dissolve`
removes worktrees and force-deletes branches. A lead's `@release` counts as an
explicit archive gesture — the one exception to "the runtime never writes
`archived_at`" is a person's gesture, and the lead is the operator's delegate here
the same way `@spawn` already is. Neither verb is a containment boundary: workers
share the lead's uid and worktree root, so a prompt-injected lead can already
reach a sibling worktree through `Bash`. `@release` exists to be the *reversible*
teardown, not to fence out a hostile lead.

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

**Changes is one scroll, and its scope is picked rather than inferred.** Every
changed file is a foldable section with a sticky header in a single column
(`FileDiffList`); the file-list-beside-a-diff-pane arrangement is gone, and with
it the separate mobile layout. The `session` scope is everything since the
worktree's base commit and `working` is only what is uncommitted — and note that
`session.diff` is a *working-tree-vs-base* diff, so it already carries
uncommitted edits to tracked files and lacks only untracked ones.
`filesForScope` is the one place that reconciles the two RPCs.

**A diff selection drafts into the composer; it never sends.** Selecting lines
and choosing "Ask about this" writes a fenced block with the path, range and
enclosing hunk into the composer and stops — the same contract the live call
honours. Lines are addressed by delegation from the container, never one button
per line, or a long diff becomes a wall of tab stops in front of the composer.

**Discarding a file is allowlisted, not merely validated.** `session.discard-file`
refuses any path git does not already report as changed for that session;
`gitops.SafeRelativePath` runs as well, but it is the allowlist that makes the
operation safe. It is offered only in the `working` scope, only behind a
confirmation, and is reachable by no agent, schedule or voice call.

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

**The state word is not the work, and the header says both.** The pill (mobile:
the subline) reports what the CLI process is doing; `SessionWorkLine` reports
what is happening, from the same `formatPulse` the sidebar row narrates with, so
the surface you are *inside* never says less than the one you have to open. Its
agent clause is separate from the state on purpose: a background subagent
outlives the turn that spawned it, so the run settles to idle while agents are
still out, and that was exactly when the header looked most like nothing was
happening. `hasLiveWork` is the one predicate, so a caller choosing what to show
*instead* (the mobile subline's branch line) cannot drift from what the line
renders.

Every control the header carries is carried on **both** layouts. The dock toggle
lived only in the desktop branch once, which left the mobile sheet reachable by
nothing but a `?dock=` link — and took the dock's aggregate live mark with it.

**The branch gets one control, and it names the verb the branch needs.** Merge
and rebase are not two options for one job: merge applies when the branch is
*ahead*, rebase when it is *behind*. Being behind with nothing committed yet is
ordinary, so rebase can never live inside merge's dropdown — there would be no
merge control to open. `lib/session/branch-sync.ts` is the one closed union that
decides, read by the desktop header, the mobile header and the dock's
`GitStatusBar`; before it, each computed its own eligibility and they disagreed
(the header counted `merging` as busy, the Changes bar did not, so it offered a
rebase while one was already running).

On mobile it rides the header's metadata line (`SessionFinishAction dense`),
beside the branch facts it acts on — the location pill and the commits-ahead
count. It had a band of its own under the header, 37px and mobile-only, for a
control the desktop keeps *in* its header; on the line it costs 8. A verb about
the branch belongs with the branch, and a third bar on a 427px screen belongs
nowhere.

Colour says what a body click does, on both layouts: **orange acts, green opens
a menu.** On a diverged branch the control is a split — Rebase on the body,
merge behind the caret — and that demotion is the design, not a compromise: the
server's merge is `--ff-only`, so merging a branch that is behind can only ever
answer `needs_rebase`. Conflicts outrank both verbs and the control becomes
"Resolve", which hands the named files to the agent through
`resolveConflictsPrompt` — the same prompt the Changes tab has always sent, now
spelled once. Rebase auto-commits the worktree server-side before replaying;
that is invisible everywhere else, so the tooltip is where it gets said.

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

**Two channels, and a dev build nags only about its own checkout.** The release
channel asks GitHub for a tag; the **source** channel asks a local git checkout
whether the branch this build came from has moved past it. A machine can be
behind on both, and neither hides the other — different claims, different costs.
The source check never fetches and never writes to the checkout.

**`main.buildOrigin` decides whether the source channel speaks at all, and it is
stamped because it cannot be inferred.** A local build on an exact tag produces
the same bare `v1.2.3` CI produces, and `main.commit` is set on both paths, so a
downloaded release beside a clone of the repo looks exactly like a build from
that clone. `just build` stamps `local`, `just release` and `release.yml` stamp
`release`, a plain `go build` leaves it empty — and only `local` gets a verdict.
Keep those three sites in step; a missing stamp silently disables the feature,
and a wrong one offers to rebuild over somebody's downloaded install.

Its verdict is withheld far more often than it is given, and each refusal is
load-bearing. A **dirty** tree says nothing, because uncommitted work is not a
version. A checkout **on another branch** says nothing either, and that one is
correctness rather than noise: the build runs *in place*, so it compiles what is
checked out, and reporting master's commit before building a feature branch
would install a binary no part of the status described. A `builtFrom` commit git
does not recognise is **unknown, which is not behind** — the same fail-closed
rule `docs/storage.md` applies to Delete. `lib/update-source.ts` is the one
closed union that turns those facts into a row, read by the row and the chip
alike, and rebuild outranks restart because a staged binary can itself be stale.

Building is `just build`, never `just install`: the install recipe rewrites the
systemd unit, and doing that *from inside the service* re-bakes
`Environment=PATH` from the service's own environment, narrowing it a little
further every time. The swap stays `installOver`, so `agentique rollback` covers
a source build for free.

**Busy is checked twice on the source channel.** A download lands before the
gate's answer goes stale; a build takes minutes, so a turn can open underneath
it — one that started *after* the operator accepted the cost. A finished build
that finds the machine busy holds at `waiting-idle`, which is cancellable and
deliberately not terminal.

The one exception to "agentique never runs a provider CLI" is agentique running
**agentique**: the staged-binary check asks the install path for its `--version`.
That rule is about binaries another library owns. Any failure there means "not
staged", never a guess.

### Subscription usage — `docs/usage.md`

One server-side collector per vendor, one normalized record, and a client that
never learns which vendor is HTTP and which is a subprocess. Polling is
server-side on the update checker's precedent, so five tabs cost one probe.

**`percent` is a fraction 0..1 and `< 0` means unknown, not zero.** Unknown is
filtered from every surface by `usableLimits`, the one filter the indicator and
the panel share — so they can never disagree about which windows exist. The set
of limits is never hardcoded: model-scoped allowances come and go.

The Claude payload has four traps, all verified live. **`limits[]` is the source
of truth and the flat buckets are only a fallback** — the model-scoped window
exists nowhere else, and the legacy per-model buckets sit at null. **Never
iterate the buckets**: codenamed ones (`nimbus_quill`) carry real utilizations
with no `limits[]` entry, so a thorough collector invents a meter nobody can
name. **Scale is decided once per payload**, or a genuine `1.0` renders as 100%.
**Windows are named from `kind`**, never a model name — "Opus 5 (1M context)"
parses to a one-minute window. Severity comes from the vendor where it gives
one; a client threshold is a guess about somebody else's limit.

**`kind: "gauge"` is what stops disk pretending to be an allowance.** An
allowance resets, so it may escalate and shows a countdown. A gauge is a level:
a small disk at 88% is its normal state, not news, so it never escalates, never
counts down, and shows its absolute figure. A permanent warning teaches the
reader to ignore warnings.

Today's tokens come from agentique's own `result` events, not a JSONL scan — it
ran those turns, and it can answer for every provider rather than the one that
writes transcripts.

**Codex goes through `runtime.AccountInspectable`, type-asserted off the
connector** — the same seam `InstallInspectable` uses, because agentique never
runs a provider CLI. That makes `connector.go` vendor-neutral: a provider whose
connector implements the capability becomes a record with no collector of its
own. A failed probe splits **structural from transient**: `ErrNotSupported` is
what an uninstalled CLI looks like, so the record is forgotten entirely, while a
timeout or a dial failure keeps the last numbers with a line saying why they are
old. A meter kept alive for a provider that is gone never stops being wrong.
Collectors run in parallel, each bounded, and the provider id rides the result —
a collector that says "nothing at all" returns a zero record, and deleting by
its empty id would leave the stale one in place.

**Every failure is a state on the record, never an error that blanks the
component**, on both sides of the wire. A transport failure and an HTTP status
are different: nothing-answered retries fast, a server that answered is left
alone. The four auth states each need their own words, because only the CLI can
mint a token. A cached percentage expires **when its window rolls over**, not on
a timer — but an unreadable timestamp is kept, not discarded.

The token reaches an `Authorization` header and nothing else: not the cache, not
the response, not a log. A test asserts it does not survive into the marshalled
record.

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

**The socket upgrades before anything slow.** `ServeHTTP` upgrades first and
builds the persona, project context, orientation and engine after, because a
browser waiting on an HTTP response cannot be told anything — the pre-upgrade
version opened seconds late and sometimes never, leaving a call that transcribed
speech and answered in silence. So every failure past the upgrade is reported
*on the socket* (an `error` frame with a fixed reason, detail in the log), the
gathering is bounded by `briefingBudget`, the engine handshake by
`engineDialBudget` — expiry refuses down the same path, which is what stops the
client ringing, and an engine that arrives late is closed rather than abandoned,
since the dial cannot be bounded by a deadline on the call's own context — and
the session summary is off the path entirely: `ProjectContext` reads
`sessionSummarizer.Cached` and warms in the background. A provider-CLI
subprocess is never between a click and a microphone.

**Neither audio context asks for a sample rate.** A requested rate is a request:
a hands-free Bluetooth route is already at 8 or 16 kHz and grants it, every media
route (A2DP, projection, a laptop's speakers) is at 44.1 or 48 and does not. So
capture built at 16 kHz worked on the telephony profile and only there — on a
media route the worklet posted 48 kHz samples the socket labelled 16 kHz, three
times too fast, and the microphone looked dead on exactly the routes that sound
best. Both contexts take the hardware's rate and convert at the edge:
`playback.ts` builds each buffer at the announced source rate, `mic-worklet.js`
converts to `INPUT_SAMPLE_RATE` — box-averaging each output sample's window
downward (the samples being skipped are already in hand, and it is the only
anti-alias guard that fits in a render quantum), interpolating upward, both
carrying state across render quanta or a reset writes a 125 Hz artefact into the
stream. `CaptureRoute` reports **both** rates because the gap is the reading:
`contextSampleRate` names the Bluetooth profile, `uploadSampleRate` is what the
socket carries.

**The playback AudioContext is created in the user gesture.** Built and resumed
inside the click that placed the call, never when `ready` arrives: a context
created outside a gesture stays suspended, so control frames render and nothing
is ever heard. The engine's rate no longer gates that — the context takes the
hardware's rate and each buffer is built at the announced source rate
(`ctx.createBuffer(1, n, sourceRate)`). A context that will not run is reported
in words, never left mute, and retried on the next gesture. The call's three
tones (`lib/voice/tones.ts`) are synthesised, not assets, and the dial tone
rides that same context on purpose: it is the unlock *and* the proof the audio
path works. A context that resume alone will not revive is **rebuilt** on the
next gesture, since a route switch wedges one in a way resume never fixes; the
audio it missed is not replayed.

**The ringback never sounds over a live call and never outlives the call
object.** One owner in `VoiceCall`, stopped on every exit from connecting —
live, `error`, closed, hangup, teardown — and an `error` frame stops it without
waiting for the close behind it. Bursts are scheduled against `ctx.currentTime`
when their timer fires, never queued ahead, or a burst outlives the state it
reports. It is a probe as much as a status: it is the same context playback
uses, playing across the moment the microphone opens, which is when Bluetooth
switches profile.

**Silence has three causes and the call names one.** `lib/voice/health.ts` is a
pure verdict over evidence the client already holds — mic level, engine
transcripts, PCM frames — ranked (`cannot-play`, then `mic-silent`, then
`no-audio`) because the status line holds one message and a line reporting three
faults reports none. The set is closed: a fourth state is a fourth thing to
learn by ear. Each clears when its condition stops holding, and clearing hands
the line back to the server's activity label rather than blanking it.

**Rendering is not audibility, and health cannot see the difference.** Every
verdict in `health.ts` judges whether the path works; a car can render a whole
call correctly into a device nobody is listening to, and that looks healthy by
construction. `lib/voice/audio-route.ts` reads the *route* instead — latency and
rate, each turned into one hedged reading, every field degrading to a stated
unknown, because `0 ms` printed for a latency the browser withheld says the
opposite of what happened. The call keeps two readings, one from the placing
gesture and one from just after the microphone opens, since the suspected fault
is a route that *moves* and one reading cannot show a move. **A probe may guess,
the call may not**: the candidate fixes are three tone probes in
`lib/voice/audio-check.ts`, one variable apart so the one you hear names the
fix, and `playback.ts` is unchanged so the control stays a control.

**A probe sounds until stopped and never goes quiet while it does.** A Bluetooth
or projection sink takes most of a second to start passing audio and suspends
again after about a second of silence, so a fixed-length tone can be inaudible on
a route that works and a tone with gaps pays that wake-up on every burst.
`startCheckTone` is one oscillator whose frequency steps between two pitches —
the gain ramps up once, down once, and never reaches zero in between. The same
argument applies to the call's own short sounds, which is why a swallowed dial
tone is not evidence the route was wrong; keeping the sink awake across a call is
listed as not implemented in `docs/voice.md`, not assumed.

**A diagnostic for a phone lives where a phone can reach it.** The audio check's
home is Settings → Voice, not `/dev/voice`: on the phone this app is an
installed PWA with no address bar, so a page reachable only by typing a path is
a page that does not exist there. It is not a setting, and it sits among them on
the rule that an action taken *on* a listed thing belongs on the surface that
lists it — the voice preview beside it is the same shape. It renders on that
page's loading and error branches too, because it needs no server to answer and
the moment it is most needed is the one where the server cannot. Both hosts
render the same parts and read their words from `AUDIO_CHECK_COPY`, so the page
you can reach and the page you can type cannot describe the check differently.

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

**Hanging up is a verb, and it is the operator's.** The idle guard is a billing
limit, never how a call ends when someone says "that's all" — before `hang_up`
the assistant could only *say* it was hanging up, and the call then sat on the
working ceiling for half an hour. The tool **arms** and answers with the
farewell to speak; the goodbye's turn completing closes the call, bounded by
`goodbyeGrace` because an engine mid-reconnect never completes one. An
interrupted goodbye still closes it, and so does a `turn_complete` that failed
to send: a socket that cannot be written to is a reason to end a call, never to
hold one open. Arming is idempotent, `endCall` sends `closed` exactly once, and
the overdue check runs **before** the idle rule — an explicit ask outranks a
phase. Nothing else ever hangs up: not a finished run, not an assistant out of
things to do.

**Speaking is `TextInjector`, type-asserted and best-effort, and it happens
after the screen copy** — a call whose engine has no voice still shows the
message. A notice is the server's own words; a report is agent-written text
about untrusted repo content and carries explicit quotation framing, so a
hostile repo cannot steer the conversation that queues the next prompt.

**The pickup greeting is once per *call*, never per engine connection.** The
model has no "call opened" event, so the server's own cue (`greetingCue`,
injected from `call.greet` when the call goes live) is the trigger that makes it
speak first — and the `sync.Once` guarding it lives at the call layer because a
Gemini session resumption mid-run is invisible there and must not re-greet over
the work being followed. It names the initial focus, or, unfocused, folds in and
**replaces** the instruction's one line of orientation rather than doubling it.
It is also the downlink's proof of life: the client's health watchdog cannot
compare engine transcripts against PCM arrival until the assistant has replied
to something.

**The dialog agent drafts, it never sends.** Its output lands in the composer
through `ComposerTextareaHandle` and stops. One path into the session pipeline,
the visible send button. Hands-free does not change that contract, only the
confirmation channel: spoken verbatim readback, an explicit affirmative (never
silence), and an announced undo window.

**A send never lands in silence, and the handoff asks one question.**
`run_prompt` answers with `Delivery.Confirmation` — the sentence to say, naming
the session, the work and which of the three deliveries it was — because the
read-back is a question and a question answered with quiet is indistinguishable
from one that was never heard. It is a statement, never another question: the
consent gate is behind it, and asking again sounds like the send did not happen.
"Silence is fine" is scoped to *while work runs* for the same reason. The second
question is gone: staying on the line is the default, `stay_on_line` is
optional, and only an explicit false stops the follow — so `runPrompt` reads the
argument's **presence**, since a model that omits it must not silently hang up
on someone still listening.

**Hands-free is a different screen, not a bigger strip.** `DrivingCall` replaces
the phone's strip while `ui-store.handsFree` is set, and answers three questions
— is it hearing me (the orb at 132px, whose halo is the only proof of the mic a
driver can check), where will this land (the focused session, second-largest type
on screen), what is it doing — then offers **one** 92px control, drawn at the
same size and place live or ended, because a call can end between the look and
the press. Fills are solid, not the app's 10% tint, which is unfindable through
glass; nothing scrolls, because a scroll needs a second look and the call speaks
the whole line anyway. The switch is **chosen and remembered**, never inferred:
`audio-route.ts` may read a car and the app still may not decide it is in one,
and the preference describes the journey rather than the call.

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
one mid-call. Questions about the switchboard itself are answered from the
system instruction, never a tool and never a `run_prompt` — the carve-out is
worded inside the never-answer rule, because read apart the two contradict.

**A session the call creates goes through `session.Service.CreateSession`**,
the same path the composer's new-session flow takes, and only into a **local**
project — creation is local because the service is. It is born `fullAuto`: any
other mode would be refused at its own first dispatch, since there is no spoken
approval. That does not move the consent gate, which was never the session's
mode. Creation is deferred to the one yes that sends the prompt, and the
dispatch read-back names the new session — so nothing exists until the operator
has agreed to the work that goes in it. A spoken model name resolves through the
picker's own catalog (`Catalog.ResolveFamily`); unresolvable names the families
that exist and creates nothing, never a guessed model id.

**Creating and sending are one tool call.** `create_session` takes the `prompt`
and dispatches it itself, through the same `dispatchPrompt` `run_prompt` uses,
answering with the same `Delivery.Confirmation`. As two calls the agreed prompt
lived nowhere but the model's own turn between them, so anything that ended that
turn — the caller speaking over it, a live-session reconnect, a model taking a
tool result's second sentence as permission to stop — left an empty session and
an operator told the work had started. Only "make me one, I'll use it later"
omits the prompt, and that answer says in words that nothing is running there. A
refused send reports **both** halves; half of it is how somebody comes back to an
empty session believing it ran. `run_prompt` stays the recovery path.

**Tool calls run one at a time, in the order the model asked for them.** One
live message may carry several function calls, arriving as separate events; a
goroutine each made that order meaningless. They queue through `pumpTools`. The
queue never drops one — an unanswered tool call leaves the model paused forever,
which sounds exactly like the call having died — so an overflow is answered with
a refusal. Every refusal in the dispatch path is logged, because "an empty
session and no log line" is the same picture for five different causes.

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

**The subsystem is off unless asked for, and off means unbuilt.** `[brain]
enabled` gates the whole block in `server.go` — no routes, no memory MCP tools,
no recall, no loops — so anything new hanging off `brainSvc` goes *inside* that
block and needs no switch of its own. A surface that reads the brain checks
`features.brain` from `/api/health` first, because an unmounted `/api/` path
does not 404: it falls through to the SPA and answers `text/html` with a 200, so
a nav row added without that check leads somewhere that looks alive and is not.
It is a plain bool only because it defaults false; `recall` defaults **on**, so
it is a quoted string and `recall = false` is a decode error that refuses to
boot.

Recall is fluid and per-turn with a session seen-set for delta injection. Do not
reintroduce first-turn-only recall. Semantic similarity is pluggable and
everything degrades cleanly to keyword/Jaccard without an embedder; the recall
thresholds (veto and vouch) are embedding-model specific. Stopwords drop
conversational filler but never domain terms — `just` is the build tool.

Strength changes on outcome, not injection. Human confirmation outranks
corroboration, which is capped below it. Model choice is a required caller
parameter in the memory core, never a library default.
</content>
