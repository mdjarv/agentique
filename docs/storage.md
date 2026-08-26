# Storage and reclamation

Where agentique's disk goes, and the two ways it comes back. Covers the Storage
page, `internal/storage`, `internal/janitor` and `agentique prune`.

## What agentique puts on the disk

Two places, and the second is easy to forget.

**The data directory** (`paths.DataDir()`) holds the worktrees, the database and
its WAL, timed backups, session files, the brain and the TLS certs. Worktrees
dominate: every session gets a full checkout, and a checkout of a JS project
carries its own `node_modules`. Nothing shares those between worktrees — the
volume is usually `ext4`, so there is no reflink to copy-on-write with, and npm's
cache is already shared per user, which makes a reinstall cache-warm and a
dedup scheme mostly ceremony.

**The OS temp dir** holds a Chrome profile per browsing session
(`/tmp/agentique-chrome-<session-id>`) and a Claude scratchpad per worktree
(`/tmp/claude-<uid>/<mangled-worktree-path>`). These are agentique's doing and
routinely larger than the worktree they belong to.

The two are reported as separate totals — *data directory* and *elsewhere* — and
never summed. The volume line names one directory; a figure labelled with that
directory has to mean it.

Backups are the one bounded category: `sqliteops` keeps everything from the last
two hours, one per day for seven days, and five pre-upgrade snapshots. The
ceiling floats with the database, since each backup is a compacted copy of it.

## Two verbs

A session's disk can be released two ways, and they answer to different bars.

**Reclaim** removes the checked-out tree (git-aware, keeping the branch), the
Chrome profile and the scratchpad. The session row and the branch survive, and
`Service.recoverWorktree` re-provisions from the branch on the next message. It
is reversible, so it needs only that the session is finished, not held by the
runtime, and has no uncommitted or untracked changes. Archived sessions included
— archiving is a filing gesture and never a safety claim, and the reversible verb
does not need a safety claim.

The cost is real but deferred: the session comes back to a repo that does not
build until dependencies reinstall. Say so in any copy that offers it.

**Delete** removes the session row, the branch and the tree. Irreversible, so it
keeps the bar CLAUDE.md sets for a bulk destructive action: the commits already
exist on the project's main line.

## The bar asks git, not a flag

`worktree_merged` records that *agentique itself* performed the merge, via the
merge `complete`/`delete` action. A branch merged from a terminal or through a
PR never sets it. On a repo worked that way the column is 0 on every row, and a
bulk action gated on it can never appear — which is exactly what happened: the
page's only bulk affordance had never rendered on the machine it was written for,
while 4.4 GiB sat reclaimable behind a CLI with no UI.

`storage.Evaluate` computes the fact instead, in this order:

1. **Live or not terminal → blocked.** Cheapest and most absolute, and it spares
   the git calls entirely for the sessions most likely to be hurt by them.
   Liveness is the union of the persisted state and `Service.LiveSessionIDs`.
2. **Uncommitted or untracked changes → blocked, for both verbs.** Before the
   merged fast path, deliberately: a fully merged branch with uncommitted edits
   still has something to lose, and reclaiming would throw those edits away.
3. **`worktree_merged` → safe.** A fast path that skips the exec, never the
   definition.
4. **`gitops.CommitsAhead(project, branch) == 0` → safe**, otherwise blocked.

Anything git cannot answer is `unknown`, and unknown is not safe. Without a probe
at all, every verdict is unknown — a page that cannot establish permission offers
nothing rather than defaulting to it.

**What the dirty check does not see.** `git status --porcelain` lists untracked
files but not *ignored* ones. That is what makes it usable — it is why a worktree
holding 591 MiB of `node_modules` reads clean — and it is also how a local
`.env`, a downloaded fixture or a scratch script survives right up until Delete
removes it. Reclaim is fine here (the tree comes back minus its ignored files,
the same trade as a fresh checkout). Delete's copy has to admit it rather than
imply the check is total.

**Stashes are repo-wide.** `refs/stash` is shared across every worktree of a
checkout, so there is no per-worktree stash list — only an entry's own "WIP on
session-xxxx" message to match a branch by. The verdict does not consult it.

**Ancestry is measured against the local HEAD**, not a remote. A branch merged
upstream while the local main is behind reads as ahead: a false negative, the
safe direction. The reverse case — local main holding unpushed commits — still
leaves those commits reachable in the local repo, so deleting the branch loses
nothing.

## The planner

`internal/janitor` is a pure planner. `Compute` takes an already-gathered
snapshot (sessions, projects, live ids, on-disk directories) and returns what is
safe to remove and what was deliberately spared, with reasons. All IO lives in
`Discover*`, `DirSize` and `Execute`, so the rules are testable without a disk.

Its guards, which no caller may skip:

- A live session's artifacts are never touched. Live is the union of the runtime
  registry, a non-terminal persisted state, and the caller's own session.
- **An empty session set reaps nothing.** A database with no sessions cannot
  declare anything an orphan; that is almost always a wrong or freshly
  initialised database pointed at a populated disk.
- An orphan worktree whose owning project is unrecognised is spared.
- A dirty worktree is spared unless `IncludeDirty`.
- Only scratchpads under the worktree base's mangled prefix are ours. Every other
  checkout on the machine writes into the same root.

A reaped scratchpad carries its session id, mapped forward from the session's
worktree path. Without it a caller reclaiming "this session" would leave behind
the artifact that is usually the largest thing the session owns.

Chrome profiles are named by session id alone, with no data-dir namespace. Two
agentique instances on one machine therefore see each other's profiles — the
data directory is the isolation boundary and the temp dir sits outside it. The
reclaim endpoint cannot act on them (it only ever touches artifacts whose session
id was requested), but the reported "elsewhere" total will include them.

## Surfaces

**The Storage page** is the one people use. Each session row carries a checkbox;
a sticky bar acts on the selection with both verbs. A verb that does not apply to
every selected row is disabled and names itself — "Can't delete — 1 of 2 are not
eligible" — because a verb that quietly skipped the ineligible rows would be a
different action than the one the button offered. The individual reasons live on
the rows.

`lib/storage/selection.ts` holds the rules. `canDelete` accepts only the server's
positive claim: a peer that predates the field sends nothing, and "not reported"
is never "safe". Selections are reconciled against every refresh, since the usage
walk is cached for a minute and a selection routinely outlives its rows.

A reclaimed session leaves the page, because the page enumerates worktrees on
disk and it no longer has one. It is still in the sidebar, still resumable.

**`POST /api/storage/reclaim`** takes session ids and re-plans server-side,
intersecting with the request. A stale client can narrow the set and never widen
it, and a session that woke up in between comes back in `skipped` rather than
being removed. Skips are a normal outcome, not an error; report what happened,
not what was asked for.

**`agentique prune`** is the offline path, dry-run by default. It opens the
database **read-only** — a command whose contract is "report what I would delete"
must not migrate the live database as a side effect of planning. It never writes
a row in any mode; it removes directories.

**The startup sweep** (`Service.SweepOrphans`, called from serve's production
block, never a constructor) is orphans-only. It is deliberately the zero-risk
subset: it reclaims nothing that still has a session row. Nothing runs on a timer
— reclaiming is something a person chose, and the page's reclaimable figure is
what makes the growth visible instead.

## Non-goals

- **Deduplicating `node_modules`.** No reflink on ext4, npm's cache is already
  shared, and reclaiming recovers all of it against dedup's slice.
- **Collecting `/tmp/go-build*`.** The Go toolchain's, and a live build owns one
  indistinguishable from a stranded one. `systemd-tmpfiles` is the right owner.
- **Changing backup retention.** Tiered, bounded and correct. If the total
  becomes a problem the lever is the database's size, not the retention count.
- **A bulk "delete all archived".** Archiving is a filing gesture; deleting on it
  would destroy unmerged work because someone tidied the sidebar. Reclaim answers
  that need without the bar.
