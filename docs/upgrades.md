# In-app upgrades

A tagged release lands; every client says so, names which machines are behind, and
upgrades them one at a time on request. Without ending a turn that is mid-flight,
and without pretending to work on a platform nobody has ever run.

There are two channels, and a machine can be behind on both at once. The
**release** channel asks GitHub what the newest tag is. The **source** channel
asks a local git checkout whether the branch this server was built from has moved
past it. They are different claims with different costs, so neither hides the
other.

**Status: V1 through V5b shipped, and the source channel with them. V5c, the
button that updates a provider CLI, is the last phase.** It is specified at the
end of this document, in full, because it is not built.

## The contract

Each server answers two questions about itself, what am I running and what is
published, and exposes both. The client does no version arithmetic beyond
comparing strings it was handed.

```
GET /api/update/status        (authenticated, per machine)
{
  "current":   "v0.4.1",       // main.version, as stamped
  "latest":    "v0.5.0",       // cached tag from the GitHub releases API
  "behind":    true,
  "channel":   "release",      // or "dev" — a git-describe build never nags
  "asset":     "agentique-linux-amd64",
  "supported": true,           // false: no asset, or platform not yet verified
  "platform":  "linux/amd64",
  "checkedAt": "2026-08-23T12:04:11Z",
  "checkError": "",            // last check's failure; the cached answer stands
  "releaseUrl": "https://github.com/…/releases/tag/v0.5.0",
  "busy":      false,          // a turn is running here right now
  "armed":     false,          // waiting for idle to upgrade itself
  "progress":  null,           // or the live phase
  "notes":     "…release notes, truncated…",
  "clis":      [ … ]           // per-provider CLI rows
}

POST   /api/update/apply      body {"expect": "v0.5.0"}   (full access)
         → 202, progress events, then the socket drops
DELETE /api/update/apply      cancel: disarm, or abort before replacing
```

Reading the status needs only a session, because every client shows the mark.
Applying needs **full access**: it replaces this machine's binary and restarts its
service, and `force` ends every turn in flight. That is at least as privileged as
reading the machine catalog, so it carries the same guard.

Asset and checksum URLs come from the release document and must be HTTPS, loopback
excepted. The checksum proves the download matches what that document said, so the
transport carrying both is the part that has to be trustworthy. Releases are not
signed, which is the remaining gap.

`?refresh=1` forces a check instead of reading the hourly cache; without it the
request never touches the network. `checkedAt` is stamped on failure too, so a
stale answer can be dated. "As of 2h ago" is information; "unknown" is not.

The endpoint is off entirely when `[update] disabled` is set. `[update] api-url`
repoints the check at a fork's repo, or at a stub, which is how the apply path is
verified without touching a real release.

The check polls hourly per server, cached against the response ETag, refreshable
on demand. Unauthenticated GitHub allows 60 requests per hour per IP, so one per
hour per machine is nowhere near it. A failed check keeps the last cached answer
and its age; a version check never blocks the UI.

## Apply

It is `install.sh` written in Go, against the machine's own platform:

1. Resolve the asset for `GOOS/GOARCH`. Refuse if there is none, or if the
   platform is not on the verified allowlist.
2. Download to a temp file **beside the install dir**, so it is on the same
   filesystem and the rename is atomic.
3. Verify sha256 against `checksums.txt`. **A mismatch aborts.** This step is what
   makes the feature safe to have at all.
4. Keep the current binary as `agentique.prev`, then rename the new one over the
   target. On Windows, rename the busy target aside first, the trick `install.ps1`
   already uses.
5. Reply `202`, flush, **then** restart the service.

Success looks like a disconnect, because the process serving the reply is the
process being replaced. The client treats the drop as expected and confirms by
re-reading the version, which is also how it verifies the upgrade worked.

Cancel and the point of no return contend on one mutex, so an accepted cancel can
never be silently ignored by an install already under way.

`status.installable` is the full preflight (verified platform, published asset,
writable install dir, a service to restart) and is what the UI keys its button on;
`blocker` says why not. `supported` stays the platform-and-asset fact the contract
defines.

## Progress is state, not just events

An upgrade runs for tens of seconds and has to narrate itself. Each phase is
published on the WS global topic **and** held as server state. Events alone strand
anyone who reloads mid-upgrade or opens a second client; state alone makes the bar
lurch on a poll interval. Both, and first to arrive wins.

```
queued → downloading (bytes/total) → verifying → replacing → restarting
                                                   │
                         ─────── socket drops here ┘
                                                   ↓
                      reconnecting → confirmed (version re-read) | failed
```

After `restarting` nobody is left to report, so the client polls the
unauthenticated descriptor until the version changes or a deadline passes, then
shows the version it **actually found**. `reconnecting` renders as progress, not
error: on this one command a dropped socket means it worked.

Narration is not a follow-up. An unnarrated 30-second binary swap is the version
nobody trusts twice.

The byte counter belongs only to `downloading`, the one phase where "is it hung?"
arises, and stays hidden under a size and duration threshold. 33 MB over a fast
link finishes before a bar means anything.

## The drain gate

**A restart is not a pause.** On startup the server reaps orphaned CLI process
groups, the guard that stops a crashed server leaking `claude` and its Playwright
subtree. So restarting mid-turn does not suspend that turn: the new process comes
up and kills it.

Sessions survive, because worktrees, history and metadata are on disk. The cost of
a badly-timed restart is the **current turn**, not the session, and the UI has to
say exactly that. "Will this lose my work" is the question that stops someone
clicking.

Busy is answered by the runtime's turn lifecycle, `Manager.BusyTurns()` over
`runtime.Session.TurnInFlight`, never session state, which reports Idle for one
dispatch before the completion that caused it is broadcast.

- **idle** — upgrade now.
- **busy** — offer *upgrade when idle*, arming a one-shot. This is the default
  offer on a busy machine.
- **override** — allowed, but the button states the cost ("2 turns will be
  terminated"), is the secondary action, and takes a deliberate second click.

Armed state carries a **deadline** (4h by default, `[update] arm-deadline`) after
which it disarms and says so, and is **in-memory only**. If the server restarts
for any other reason the arming is forgotten. That is the fail-safe direction: an
upgrade armed on Tuesday must not fire on Thursday because a lid closed at the
wrong moment. A 30s ticker enforces the deadline, which has no event of its own;
it is a safety net, not the mechanism. Losing the race back to busy re-arms rather
than dropping the request.

**Where the gate listens matters more than it looks.** The obvious hook is the
idle transition, and it is the wrong one. agentkit flips the runtime to Idle from
inside the completion's own dispatch, *before* the turn-completed event is
broadcast, so an observer woken at that moment still sees the turn it is waiting
on as in flight and a gate wired there never fires.

That is now agentkit's own contract rather than something agentique reconstructs.
`runtime.WithOnTurnEnd` fires strictly after the completion broadcast and after
`TurnInFlight` has cleared, for every way a turn can stop: completed, died with
the CLI, or closed mid-flight. `Manager.AddTurnEndListener` fans that one hook out
to agentique's consumers.

## Cancelling

Two different things wear the word. An **armed** upgrade is cancellable for as
long as it is armed. An **in-flight** one is cancellable up to a line:

| Phase | Cancellable | Why |
|---|---|---|
| queued, downloading | yes | Nothing installed; delete the temp file. |
| verifying | yes | The installed binary is still untouched. |
| replacing | no | A single rename, over before a cancel lands. |
| restarting | no | The new binary is installed; "cancel" now means rollback. |

The Cancel button is real through verification and then **disappears**, replaced
by "no going back". That is more honest than a control that stays visible and
quietly stops working, and it covers the long phase anyway: download is where the
seconds go.

## Across machines

Every machine checks for itself, because only it knows its platform, its install
method and whether it is busy. The client fans status calls out through the
routing facade and merges the answers into one dialog, one row per machine.

**An offline machine is not a problem to solve.** Last-known version, greyed, no
action. It gets offered the upgrade when it returns.

**Only a client may trigger an upgrade.** Never a peer machine, never as a side
effect of anything else. If presentation sync ships, its scoped credential is
excluded from this route by construction.

**Mixed versions stay legal.** The descriptor carries capabilities and clients
treat a missing key as unsupported. An upgrade feature makes version skew routine
rather than exceptional, so nothing may start comparing version numbers to decide
behaviour.

A local build reports `channel: "dev"`. `git describe --tags --always --dirty`
yields something like `v0.4.1-7-gab12cd3-dirty`, and a machine you are actively
developing on must never be told it is behind.

The mark renders only when a machine is behind, so the popover and the dialog
behind it are the contextual surfaces. Settings › About is the permanent one and
is always reachable.

## The footer says it with a mark

`UpdateMark` is a dot on the sidebar footer's usage trigger, and it is neither a
pill nor an element of its own.

The footer is one 271px line already carrying identity, liveness and the usage
cluster — and the cluster **grows**, because the set of allowance windows is
never hardcoded (`docs/usage.md`). A pill spelling "Rebuild available" was the
longest string on that line: it wrapped to two rows and pushed the codex and disk
marks outside the sidebar. Everything right of the account name is now
`shrink-0`, because a mark's width is what it means, and the name is the only
thing on the line that can give ground.

The words were never the chip's to carry. `UpdatePopoverRows` renders the label,
the detail **and** the button in the popover one click away, so the pill spent a
third of the footer duplicating what it fronts. What is irreducible is "there is
something waiting, here".

So it is a glyph, and it **leads the usage cluster** from inside that cluster's
own trigger — the control that already opens the popover holding the verb.
Inline, not notched onto a corner: a mark overlapping the last vendor's logo
reads as a claim about that vendor, and one floating in the gap is dead pixels
beside the control it is about. Inline it also *costs width*, which is the honest
thing for it to do; the account name truncates to pay for it, the way everything
else on this line is arranged to.

`lib/update-mark.ts` owns what a kind looks like, and **both** surfaces read that
one table — the footer's mark and the popover's rows — on the `REST_GLYPH`
precedent: a mark that says "upgrade" in the footer cannot say something else in
the popover it opens.

`MARK_GLYPH` is deliberately not four different pictures. Downloading a release
and compiling a checkout are the same offer to a reader — there is a newer build,
and taking it costs the current turn — so both wear `CircleArrowUp` and the row's
words say which. A **restart** is the one genuinely different act (nothing to
fetch, nothing to compile, just bounce the process) and keeps `RotateCw`; that is
the same split `sourceVerdict` makes when it ranks `staged` above everything
else.

`CircleArrowUp` earns the slot by being a **closed round form**, which is what
makes it findable beside the usage cluster's field of vertical strokes — an arrow
drawn in strokes disappears into them, which is how `ArrowBigUpDash` lost. It is
drawn in the accent colour, because the cluster beside it is deliberately muted
and colour is what separates a thing asking for something from a thing merely
reporting, at 14px against the 11px vendor marks. `GitBranch` held the slot first
and was wrong twice over: it said "git" rather than "upgrade", and it was already
losing its branch node by 12px, the way `FolderGit2` does at 10px.

The sentence stays with the button, which is what a reader hovers and what a
screen reader announces: `useUpdateWaiting` is the one predicate behind both the
glyph and those words, so they can never disagree either. That trigger stays
mounted for a machine reporting no windows at all but behind, since it is what
the mark rides; with neither it would be an empty target and it goes.

The dismissal went with the pill. It existed because a sentence in the footer is
loud; a glyph is not, and an update that can be waved away is one nobody
applies.

## Build wide, enable narrow

Cross-compiling is free, and not having the hardware does not stop us publishing
assets. It stops us promising they self-upgrade.

**Publish** `linux-amd64`, `linux-arm64`, `windows-amd64` and `darwin-arm64`, so a
manual `install.sh` works anywhere. **Enable in-app apply** only on an explicit
allowlist of verified platforms, starting with `linux/amd64`. Everything else
reports `supported: false` and the row says "manual upgrade". A platform graduates
when someone actually runs it, not when it compiles.

## The source channel

A dev build never nags — about a *release*. That invariant is right, and it is
also why a machine someone develops on used to say nothing at all about its own
version. `Channel()` classifies a git-describe build as `dev`, `statusFrom`
refuses to set `behind` on one, and the question that machine actually has is a
different question: **is the server running what I wrote.**

That one is answerable offline, from a checkout already on disk. `[update]
source-dir` names it; unset leaves the channel off entirely, which is the state
on every machine that only installs releases. `internal/update/source.go` reads
it on the same hourly tick as the release check, never fetches, and never writes
to the checkout.

### The build has to say it is a local build

**`main.buildOrigin` is stamped, because it cannot be inferred.** A local build
sitting on an exact tag stamps the bare tag — `git describe` returns `v0.6.0`
with no suffix on a clean tree at that tag — which is byte-identical to what CI
stamps, and `main.commit` is set on both paths. So a binary downloaded from a
release page, on a machine that also happens to have a clone of the repo, is
indistinguishable from one built out of that clone. Without the stamp the source
channel would offer to rebuild over somebody's downloaded install.

The justfile's `backend-build` sets `local`; `just release` and
`.github/workflows/release.yml` set `release`; a plain `go build` leaves it
empty. **Only `local` produces a verdict**, and the other two do not even run the
staged probe — the relationship this channel describes does not exist for them.
A release install says so in `blocker` and its row renders nothing, because its
updates come from the release row directly above it.

That is also why the three values matter more than they look: `release` is a
*claim*, `""` is an *absence*, and both must fail closed, but only one of them is
worth wording as a fact about the install rather than a shortcoming of the build.

```
"source": {
  "dir": "…", "branch": "master",
  "head": "a1b2c3d", "headSubject": "…",   // the branch's HEAD
  "builtFrom": "1ae969a",                  // main.commit, stamped and until now unused
  "ahead": 5, "behind": true,
  "dirty": false, "checkedOut": "master",
  "staged": false, "installedVersion": "",
  "buildable": true, "blocker": ""
}
```

**The comparison is one command**, `git rev-list --count <builtFrom>..<branch>`,
and it answers both questions at once: how far the branch has moved, and whether
the running build is even an ancestor of it. A `builtFrom` this repo does not
recognise — a binary installed from a release, a rebased history, a shallow clone
— makes the count fail, and **unknown is not behind**. Same fail-closed rule
`docs/storage.md` applies to Delete.

**A dirty checkout says nothing.** Uncommitted work is not a version, and a tree
someone is typing in would light the mark permanently. The facts are still
reported (`ahead` stays true to the commits); it is the *verdict* that is
withheld, and `blocker` says why.

**A checkout on another branch says nothing either**, and that one is about
correctness rather than noise. The build runs **in place**, so it compiles what
is checked out. Reporting master's commit and then building a feature branch
would install a binary no part of the status described. Requiring clean-and-on-
branch is what makes the in-place build honest: the binary is, by construction,
the commit the row named.

### Three states, three costs

`lib/update-source.ts` is the one closed union that decides what the row says,
read by the row and by the mark. It ranks by the cheapest **complete** answer,
which is not the same as the cheapest one:

- A staged binary **built from the head** wins outright. A restart is seconds
  and there is nothing left to compile, so offering a rebuild would spend two
  minutes reproducing the identical commit. That is precisely what
  `just install` leaves behind, and it is the state this box was in while the
  feature was being verified.
- Otherwise the rebuild wins. A staged binary the branch has since moved past is
  itself stale, and restarting into it lands the operator one commit short and
  asking again.

`stagedIsCurrent` is what separates the two, and the **server** answers it, by
reading the commit a `git describe` version already carries (`describesCommit`).
A plain release tag names no commit and therefore never matches — the right
answer, since we cannot prove a release binary is the branch head. The client
does no version arithmetic, here or anywhere.

| State | What is true | The verb |
|---|---|---|
| `in-step` | the running build is the branch's HEAD | nothing |
| `staged` | a newer binary is at the install path, but not in the process | restart, seconds |
| `ready` | the branch has moved and this machine can build it | rebuild, minutes |
| `blocked` | it has moved, but building would be wrong or impossible | say why |
| `unknown` | git could not answer | say so |

`staged` is what `just install` leaves behind until the service restarts, and
what a failed restart leaves behind. It is detected by asking the binary at the
install path for its `--version` — **agentique running agentique**, which the
"never run a provider CLI" invariant does not cover: that rule is about binaries
another library owns, and this one is ours. Once an hour, not per request, and
any failure means "not staged" rather than a guess.

### Building

`just build`, not `just install`. The install recipe also rewrites the systemd
unit and regenerates completions, and rewriting the unit *from inside the
service* re-bakes `Environment=PATH` from the service's own environment — which
narrows PATH a little further on every upgrade. The swap is `installOver`, the
same one the release path uses, so `agentique.prev` and `agentique rollback`
keep working for a source build with no extra code.

`building` replaces `downloading`, and the log tail replaces the byte counter: a
build has no total to count against, and "is it hung?" is asked just as often —
usually while npm is silent for ninety seconds.

**The toolchain is resolved before the button is offered.** `just`, `go`, `git`,
`node` and `npm`, through `exec.LookPath`, from the server's PATH rather than a
login shell's. That is not defensive: on this dev box the unit's baked PATH
reaches node only through `/run/user/1000/fnm_multishells/<pid>_<stamp>/bin`, a
tmpfs directory created per login shell. After a reboot the entry dangles, and
nothing else on that PATH provides node. A missing tool is a `blocker`, never a
button that fails halfway.

**Busy is checked twice.** A download takes seconds and the drain gate's answer
at the start still holds when it lands. A build takes minutes, so a turn can
open underneath it — one that began *after* the operator agreed to the cost. So
a finished build that finds the machine busy holds at `waiting-idle` (which is
cancellable and deliberately **not** terminal) rather than either killing that
turn or throwing the build away. `force` skips it: an operator who already
accepted the cost is not asked twice by a different code path.

### Knowing and acting are separate, here too

The same split the CLI rows draw (C4). The verdict is read-only and safe
everywhere, so it ships on. The button compiles in the operator's own checkout
and restarts the service, so it waits for `[update] source-apply`, default off.

## Claude and Codex CLIs

**Nobody in this repo runs a CLI.** Each provider's Go library owns its own
command entirely. agentique never constructs, execs or shells out to `claude` or
`codex`, not to read a version, not to run `doctor`, not to update. It asks
agentkit's `runtime.InstallInspectable`; agentkit asks the adapter; the adapter
asks the library. Anything the product needs from a CLI is a gap in that library,
and the fix is to add it there rather than route around it.

**The target is the binary agentique itself would spawn**, resolved by the
connector, never by a PATH lookup in the product. Those agree today only because
nothing overrides the binary path. The connector owns the client options, so it is
the only thing that stays right the moment something does, which is why the
capability hangs off `CLIConnector` rather than being a helper anyone can call.

**Detect how each CLI was installed. Never assume.** Showing the wrong update
command does not fail cleanly: `npm install -g` against a native install writes a
second complete copy into an npm prefix, whichever copy PATH reaches first answers
`--version` from then on, and the copy actually in use stays stale. **An empty
update command means "tell the user to update manually". It never means "fall back
to npm".**

**The install method never gates behaviour.** `Method` is a label to display.
`InstallNative` means the standalone layout only in codexcli-go, but includes a
bare executable in claudecli-go; codex updates its own npm-global installs while
claude's hand back a command. Branch on the library's verdict — `SelfManaged`, a
non-empty `UpdateCmd`, a passing preflight — never on a method name. Same rule as
the model catalog, one level down: versions and enums never gate behaviour,
capabilities do.

**Knowing and acting are separate questions.** Report that an update exists
whenever a trustworthy source for *that* install can be named. Offer to perform it
only where the library manages that install itself and its preflight passes. Never
let "we cannot act" suppress "you should know": an npm-global install into a
root-owned prefix is knowable and untouchable at once, and that is a common case,
not an edge one.

Where no source can be named (brew, winget, mise, asdf, unknown) the row says so.
It never borrows another channel's number, because the channels disagree: npm and
the native `latest` channel tracked 2.1.241 on a day the native `stable` channel
was ten patches behind at 2.1.231.

**Preflight is the library's, not ours.** The directory that must be writable is
not the one holding the binary on PATH. For an npm install it is the managed
package root; for a codex standalone install it is
`$CODEX_HOME/packages/standalone`. Neither is derivable from the resolved path, so
a check in the product would test the wrong directory and offer a button that
cannot work.

**Exit codes from CLI updaters are not evidence.** `codex update` was observed
exiting 0 and printing success after its updater command was missing entirely. An
update is verified by re-reading the version, exactly as an agentique upgrade is
verified by re-reading the descriptor rather than trusting the response.

**Auto-update state is what makes "updates itself" honest.** A self-managed
install whose updater is switched off does not update itself, and saying it does is
the most reassuring possible way to be wrong. So the row reports what the tool says
(enabled, what disabled it, which channel) and shows the command anyway when the
updater is off. A tool that reports nothing gets the plain phrase: "did not say"
and "said no" are different claims.

### A CLI update is not a restart

The drain gate does not apply. The server keeps running, running turns keep their
already-exec'd binary, and the new version applies to the next session, which is
what the UI says.

That is observed, not reasoned. A real `claude update` ran on the dev box at 07:40
on 2026-08-24 while three CLI sessions were mid-turn. All three continued, and all
three still had `/proc/<pid>/exe` pointing at `versions/2.1.239` afterwards while
the symlink had moved to `2.1.241`. The native layout keeps every version as its
own file and repoints a symlink, so a running turn is not reading anything the
update touches. A gate would have suspended those turns to prevent nothing.

The claude CLI also self-updates on the same mechanism, four versions in the
thirteen days to 2026-08-24, installed without anyone asking. This happens whether
or not agentique offers a button. The gate returns as a question only if a library
starts calling a shared-tree rewrite self-managed.

## Invariants

- **A restart is not a pause.** Anything that restarts the server consults the
  turn registry first.
- **Checksum before replace, always.** No path installs an unverified binary.
- **The previous binary is kept** as `agentique.prev`, and rollback stays a
  deliberate command. Nothing auto-reverts, because an automatic rollback that
  also fails is a worse place to be.
- **Never offer a button that cannot work.** Unsupported platform, unwritable
  install dir and no-service-installed are all detected before the row offers an
  action.
- **Only a client triggers an upgrade.** Never a peer, never a schedule unless
  auto-upgrade is explicitly enabled on that machine.
- **Version numbers never gate behaviour**; capabilities do. For the CLIs the same
  holds of install-method enums: they are labels, and the two provider libraries
  define them differently on purpose.
- **A dev build never nags about a release.** It may nag about its own checkout,
  and only when one is configured, clean, and on the branch it names.
- **The source channel never fetches, and never writes to the checkout.** The
  check is a read; the build is the only thing that touches the tree, and only
  behind an explicit flag.
- **Unknown is not behind.** A commit git does not recognise, an unstamped
  build, an unreadable checkout — all withhold the verdict rather than guessing
  either way.
- **agentique never runs a provider CLI.** Versions, install methods and updates
  all come through `runtime.InstallInspectable`. A missing fact is a gap in the
  provider library, not a reason to shell out.
- **An empty update command means "manually", never "use npm".**

## Settled decisions

| # | Decision | Why |
|---|---|---|
| U1 | Each server checks for itself | A machine that cannot reach GitHub cannot upgrade anyway. |
| U2 | A dot on the usage trigger, no dismissal | A mark is quiet enough not to need silencing, and costs no width on a line that has none. The words are in the popover it opens. |
| U3 | Per-row action, no bulk | One machine, one button, one visible outcome. |
| U4 | Arm when idle; override on a second click | See the drain gate. |
| U5 | Build wide, enable narrow | No Mac or ARM hardware to verify against. |
| U6 | CLI updates deferred to V5 | Install method has to be detected first. |
| U7 | Auto-upgrade per machine, default off | Ships as a setting; stays off until apply is exercised by hand. |
| U8 | No pre-release channel | Everything goes to master and out; `releases/latest` is all of it. |
| S0 | The build stamps its own origin | Nothing else can tell a local build from a downloaded one: building at an exact tag stamps the same bare tag, and `main.commit` is set both ways. Only `local` gets a verdict. |
| S1 | The checkout is named explicitly, never auto-detected | One place to look. Deriving it from the project registry would make the feature depend on a coincidence. |
| S2 | Local branch only, no fetch | The question is "is the server running what I wrote", and that is answerable offline. A background service doing network git in your repo is a different feature. |
| S3 | Build in place, refused unless clean and on the branch | Fast, and it reuses `node_modules`. The refusal is what makes it honest: an in-place build compiles what is checked out. |
| S4 | `just build`, not `just install` | `install` rewrites the systemd unit from inside the service, re-baking its PATH. |
| S5 | Restart-only is its own verb | "The binary on disk is newer than the process" is a real state with a two-second fix; charging a rebuild for it would be wrong. |
| S6 | Both channels show, neither wins | Different claims, different costs. Picking one for the operator hides a true statement. |
| S7 | The cheapest COMPLETE answer wins | A staged binary built from the head needs only a restart, so it outranks a rebuild that would recompile the identical commit. A staged binary the branch has moved past is itself stale, so there the rebuild wins. `stagedIsCurrent` is the server's answer; the client does no version arithmetic. |
| C1 | The target is the binary agentique spawns | Anything else describes a binary nobody here executes. |
| C2 | claudecli-go owns the claude command | Detection already exists there, read-only and network-free. |
| C3 | codexcli-go owns the codex command | Its own report beats our inference. |
| C4 | Knowing and acting are separate | Root-owned installs are knowable and untouchable at the same time. |
| C5 | Only the tools' own updaters, run by their own libraries | The server has no npm prefix, and never should. |
| C6 | No drain gate for CLI updates | Not a restart; the CLI already self-updates under live sessions. |
| C7 | CLIs never drive the footer mark | They ship most days; a permanently lit mark is one nobody reads. |
| C8 | `clis` rides `/api/update/status` | Detection is offline and cheap; a second endpoint buys nothing. |
| C9 | Shadowing is reported, symmetrically | A warning that works for one CLI and not the other teaches false trust. |
| C10 | `internal/doctor` does not run the CLI | Two answers to "how do I update this" must not differ. |
| C11 | Run-it button ships off | Mirrors U7: the capability ships, the trigger waits for a hand-run. |
| C12 | Show the version a session reported | The only field derived from what happened rather than from inspection. |
| C13 | The connector answers, not the PATH | Keeps detection and execution from drifting apart. |
| C14 | The install method never gates behaviour | The two libraries' enums deliberately disagree. |
| C15 | V5a shipped without a "behind" verdict | Nothing in the stack could compute one; a stub would be wrong, not small. |

## What shipped

- **V1, know.** `/api/update/status`, hourly ETag-cached check, per-machine
  version kept client-side, versions in Settings › About. The poll loop starts
  from serve's production block, same precedent as the scheduler, so a unit test
  never reaches the network. `release.yml` gained `linux-arm64` and `darwin-arm64`
  and `install.sh` accepts them.
- **V2, tell.** The footer mark and the dialog, fanned out across machines.
  `useUpdateChecks` re-reads every machine's cached answer on a 15-minute beat and
  immediately when the catalog changes; the servers do the hourly GitHub check and
  the client only re-reads. Nothing about it persists, and nothing can be waved
  away.
- **V3, apply.** Preflight, download, verify, replace, restart, plus
  reconnect-and-confirm, per-phase progress, cancel through verification, and
  `agentique rollback`. Verified on throwaway servers with an isolated
  `AGENTIQUE_HOME`, a stub releases endpoint and a `systemctl` shim that could only
  ever signal the throwaway, before it went near a real one.
- **V4, wait for idle.** The drain gate, the armed one-shot with deadline and
  cancel, and the override with its honest warning.
- **V5a, what is installed.** `internal/update/cli.go` asks the capability per
  provider, caches on the hourly tick, and adds `clis[]` to the status: tool,
  version, path and real path, method, source, self-managed, update command,
  version manager, package manager, warnings. `internal/doctor` converts to the
  same source and gained its missing codex check.
- **V5b, the rows.** One expandable machine row, local expanded by default. The
  machine icon stays the icon and the disclosure gets its own control, because with
  a fleet those icons are how you tell rows apart. `lastRan` comes from
  `runtime.SessionInitEvent.CLIVersion` through the pipeline and is folded in on
  read rather than at refresh, so a session starting between two hourly probes does
  not wait an hour to be visible.

## V5c, the button (not built)

Unblocked: `runtime.InstallUpdatable`, `UpdateOutcome` and the three-valued
`VersionStatus` all ship in agentkit v0.2.0. It ships behind `[update]
cli-updates`, default off, and is verified against a throwaway server before it
goes near a real one.

agentkit settled on **six** outcomes, making `unverified` its own value rather
than a flag on `updated`. Render that as success with different words: the
affordance is identical, nothing to retry and nothing for the user to do, and only
the copy differs.

The five below are the reasoning behind them, which still holds.

- **updated**, with before and after.
- **already current.**
- **manual** — not ours to update, carrying the command to show and where to run
  it. A normal result, not an error.
- **blocked** — it would be ours, but preflight refuses.
- **failed.**

Manual and blocked are both "no button" and are not interchangeable. One is about
ownership, the other about permission, and the user's next action differs.

**"Reported success but the version did not change" must be reachable as its own
state and must be impossible to render as success.** That is the observed
`codex update` failure, and the one a naive implementation calls a win.

Its twin is **unknown**. Both libraries report an empty version when the probe
fails, so `"" == ""` is a probe that could not see, not a binary that did not move.
Equal-and-known is a failure; unknown is an update we cannot confirm, rendered as
success with different words. Collapsing the two would make the honest case wear
the accusation meant for the dishonest one.

One consequence to carry into the copy: **"reported success and nothing happened"
is not distinguishable from "already up to date"** without a published version to
prove an update was due. Both are a nil error with an unchanged version. So
`failed` plus `version unchanged` is only reachable when the updater *also* exits
non-zero, and the first round of copy must not claim to catch the updater that
lied. That is the strongest argument for wiring the published version in early
rather than treating it as a badge.

**Outside this repo, in dependency order.** `claudecli-go` needs an `Update`, a
published-version lookup (only it knows whether an install tracks `latest` or
`stable`), and a PATH-entries report so C9 can be symmetric. `codexcli-go` needs
its `Update`. `agentkit` needs the capability extended to perform, not just report.

## Known risks and unverified claims

- **Windows is the least-exercised path.** Replacing a running executable and
  restarting a scheduled task have never been verified on real hardware. Windows
  reports "manual" until it can be tested on the machine itself.
- **The restart hand-off** — reply 202, flush, close listeners, let the service
  manager take over — is easy to get subtly wrong and wants a real test on
  throwaway servers, not a unit test.
- **macOS quarantine.** A binary fetched by our own Go code should carry no
  `com.apple.quarantine` attribute, since that is applied by browsers and not by
  plain HTTP clients, which would make darwin self-upgrade viable. That is
  inference, not something anyone has run, which is exactly why apply is gated
  behind verified platforms.
- **Release notes** are auto-generated from commits and may be noise rather than
  signal. A link may beat rendering them.
- **No CLI updater has been run by the service**, as opposed to by a person in a
  shell. That difference is what V3 learned to respect with throwaway servers, and
  it is why V5c's button ships off.
- **Updating an npm-global CLI under a live session is untested.** A running node
  process may lazily load from the tree being rewritten. Currently unreachable,
  because codex reports `SelfManaged: false` for npm installs and claude's
  npm-global installs hand back a command. It becomes live the moment any library
  calls such an install self-managed, which is also when C6's "no gate" needs
  revisiting.
- **`codex doctor --json` is a stringly-keyed details map** at schema version 1.
  That is absorbed inside codexcli-go rather than here, but an upstream rename
  still degrades a row to "unknown", which is the correct failure and the one to
  keep.
</content>
