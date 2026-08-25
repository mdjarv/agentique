# In-app upgrades

A tagged release lands; every client says so, names which machines are behind, and
upgrades them one at a time on request. Without ending a turn that is mid-flight,
and without pretending to work on a platform nobody has ever run.

**Status: V1 through V5b shipped. V5c, the button that updates a provider CLI, is
the last phase.** It is specified at the end of this document, in full, because it
is not built.

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

Reading the status needs only a session, because every client shows the chip.
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

The chip that opens the dialog renders only when a machine is behind, so the
dialog is the contextual surface. Settings › About is the permanent one and is
always reachable.

## Build wide, enable narrow

Cross-compiling is free, and not having the hardware does not stop us publishing
assets. It stops us promising they self-upgrade.

**Publish** `linux-amd64`, `linux-arm64`, `windows-amd64` and `darwin-arm64`, so a
manual `install.sh` works anywhere. **Enable in-app apply** only on an explicit
allowlist of verified platforms, starting with `linux/amd64`. Everything else
reports `supported: false` and the row says "manual upgrade". A platform graduates
when someone actually runs it, not when it compiles.

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
- **A dev build never nags.**
- **agentique never runs a provider CLI.** Versions, install methods and updates
  all come through `runtime.InstallInspectable`. A missing fact is a gap in the
  provider library, not a reason to shell out.
- **An empty update command means "manually", never "use npm".**

## Settled decisions

| # | Decision | Why |
|---|---|---|
| U1 | Each server checks for itself | A machine that cannot reach GitHub cannot upgrade anyway. |
| U2 | Chip; dismissal dies on reload | Deliberate pressure to update. Nothing about it persists to storage. |
| U3 | Per-row action, no bulk | One machine, one button, one visible outcome. |
| U4 | Arm when idle; override on a second click | See the drain gate. |
| U5 | Build wide, enable narrow | No Mac or ARM hardware to verify against. |
| U6 | CLI updates deferred to V5 | Install method has to be detected first. |
| U7 | Auto-upgrade per machine, default off | Ships as a setting; stays off until apply is exercised by hand. |
| U8 | No pre-release channel | Everything goes to master and out; `releases/latest` is all of it. |
| C1 | The target is the binary agentique spawns | Anything else describes a binary nobody here executes. |
| C2 | claudecli-go owns the claude command | Detection already exists there, read-only and network-free. |
| C3 | codexcli-go owns the codex command | Its own report beats our inference. |
| C4 | Knowing and acting are separate | Root-owned installs are knowable and untouchable at the same time. |
| C5 | Only the tools' own updaters, run by their own libraries | The server has no npm prefix, and never should. |
| C6 | No drain gate for CLI updates | Not a restart; the CLI already self-updates under live sessions. |
| C7 | CLIs never drive the footer chip | They ship most days; a permanently lit chip is one nobody reads. |
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
- **V2, tell.** The footer chip and the dialog, fanned out across machines.
  `useUpdateChecks` re-reads every machine's cached answer on a 15-minute beat and
  immediately when the catalog changes; the servers do the hourly GitHub check and
  the client only re-reads. Dismissal is a field on an unpersisted store, so a
  reload brings the chip back and nothing lands in localStorage.
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
