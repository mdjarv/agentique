# Changelog

Notable changes per release. The canonical, complete list of commits for any
version is its [GitHub release](https://github.com/mdjarv/agentique/releases);
this file carries the curated summary and, above all, what a release asks of you
before you upgrade.

Starts at v0.4.0. Releases v0.1.0 through v0.3.0 are listed at the bottom with
links to their own notes, rather than reconstructed here after the fact.

## v0.4.0

### Breaking

**Everyone signs out, and every paired machine must be re-paired.**

Migration 049 stores inbound bearer credentials as SHA-256 digests instead of as
the credentials themselves. It *replaces* the `auth_sessions`, `pairing_tokens`
and `invite_tokens` tables rather than converting them — a digest does not
invert, and backfilling would have kept the plaintext readable through one more
startup to remove it. So on first boot after upgrading:

- every browser session is invalidated; sign in again with your passkey
- every paired machine's bearer is gone; re-pair each one with `agentique pair`
- outstanding invite links stop working; issue new ones

`machines.token` is deliberately left plaintext: it is an *outbound* credential
this server presents to a remote, so it has to stay recoverable.

**Upgrading to v0.4.0 is manual.** v0.3.0 predates in-app upgrades entirely, so
there is no Upgrade button to press on the version you are running. Re-run
`install.sh` (or `install.ps1` on Windows). From v0.4.0 onward the in-app
Upgrade button works: it applies on linux/amd64, while linux/arm64,
windows/amd64 and darwin/arm64 get a published binary and a working installer but
report "manual upgrade" until each is verified on real hardware.

**New platforms.** v0.3.0 shipped a single `agentique-linux-amd64` asset, so
`install.sh` failed on linux/arm64 and darwin/arm64 and `install.ps1` failed
outright. v0.4.0 publishes all four binaries plus `checksums.txt`.

### Added

- **Multi-machine.** One UI driving several sovereign servers: pairing with
  pinned identity and a verified signed challenge, tailnet peer discovery,
  projects merged across machines by canonical git remote, per-machine offline
  caches, and a Run-on picker that chooses where a session executes. See
  [docs/multi-machine.md](docs/multi-machine.md).
- **Scheduled loops.** Recurring prompts with run history, health and attention
  semantics. Delivery is idle-gated and fresh-turn-only rather than mid-turn
  injection; busy refusals requeue without consuming an attempt; repeated
  failures auto-pause the loop. Agents can report their own outcomes and pace
  themselves. See [docs/scheduled-loops.md](docs/scheduled-loops.md).
- **In-app upgrades.** Every machine reports what it runs and what is published;
  one chip and dialog cover all of them. Apply narrates its progress, keeps a
  rollback copy, and waits for idle first. See [docs/upgrades.md](docs/upgrades.md).
- **Session-first sidebar and landing deck.** The folder sidebar is replaced by a
  flat thread list where *sessions* pin rather than projects, with a stale shelf,
  work-kind glyphs, unread marks, a sync dock and an instrument-cluster footer.
  `/` is now a command deck plus a durable cross-project activity feed; the old
  project grid moved to `/projects`.
- **Persistent agent memory (the brain).** Cross-session recall, encode and
  consolidate, with a semantic graph, per-turn delta recall, an outcome signal
  that strengthens what actually helped, archive-not-delete aging, snapshots with
  rollback, and a management UI. Semantic recall runs against Chroma and Ollama
  when configured and degrades to keyword matching when not. See
  [docs/brain.md](docs/brain.md).
- **Dynamic workflows.** Multi-agent orchestration from the CLI runtime, rendered
  as a live phase-and-agent tree in a right panel. See
  [docs/workflows.md](docs/workflows.md).
- **Subagent roster.** An Agents tab whose badge counts agents in flight and
  failures from the latest unopened turn — never a lifetime spawn count — plus a
  flight strip that survives a tab switch.
- A self-updating model catalog that learns labels from the CLI rather than
  hardcoding version numbers. See [docs/model-catalog.md](docs/model-catalog.md).
- Idle-session eviction that reclaims CLI and browser processes, with lazy resume
  on the next message. See [docs/process-lifecycle.md](docs/process-lifecycle.md).
- A mobile two-tier session header and an input-forward composer.
- Sessionless web-only discussion personas. See [docs/channels.md](docs/channels.md).

### Security

- Inbound credentials (`auth_sessions`, `pairing_tokens`, `invite_tokens`) stored
  as digests rather than as the credentials themselves — the breaking change above.
- Passkey recovery gated behind a one-time code from `agentique auth rekey`.
- The first-user rekey window closed: no user row is written before its ceremony
  verifies.
- Path-escaping and glob record ids rejected in brain record lookups.
- Agent-written session files never served as active content; response headers
  centralised so new routes inherit them.
- Downloads fail closed — a missing or unfetchable checksum aborts an install.
- Credentials kept out of argv and group-readable files; the data dir is
  owner-only and the database and sidecars are 0600.
- Multi-machine transport hardened: bearer tokens never ride URLs, sockets redeem
  bounded one-time tickets, and revoking a session closes its established sockets.
- CI actions pinned to commit SHAs.
- A systemd unit with cgroup memory bounds and OOM containment.

### Fixed

- The orphan reaper is scoped to its own data dir and fails closed, so it can
  never touch another instance's process groups or worktrees.
- Message delivery is reported by the server rather than inferred by the client,
  so a mid-turn send no longer draws both an optimistic turn and an echo.
- Archived and done are separate facts with separate owners; a subprocess exiting
  can no longer file a session away on the user's behalf.
- A chat pane wears its repo's colour rather than the colour of whichever machine
  happens to run the session.

## Earlier releases

Release notes for v0.1.0 through v0.3.0 live with their tags:

- [v0.3.0](https://github.com/mdjarv/agentique/releases/tag/v0.3.0)
- [v0.2.2](https://github.com/mdjarv/agentique/releases/tag/v0.2.2) ·
  [v0.2.1](https://github.com/mdjarv/agentique/releases/tag/v0.2.1) ·
  [v0.2.0](https://github.com/mdjarv/agentique/releases/tag/v0.2.0)
- [v0.1.8](https://github.com/mdjarv/agentique/releases/tag/v0.1.8) ·
  [v0.1.7](https://github.com/mdjarv/agentique/releases/tag/v0.1.7) ·
  [v0.1.6](https://github.com/mdjarv/agentique/releases/tag/v0.1.6) ·
  [v0.1.5](https://github.com/mdjarv/agentique/releases/tag/v0.1.5) ·
  [v0.1.4](https://github.com/mdjarv/agentique/releases/tag/v0.1.4) ·
  [v0.1.3](https://github.com/mdjarv/agentique/releases/tag/v0.1.3) ·
  [v0.1.2](https://github.com/mdjarv/agentique/releases/tag/v0.1.2) ·
  [v0.1.1](https://github.com/mdjarv/agentique/releases/tag/v0.1.1) ·
  [v0.1.0](https://github.com/mdjarv/agentique/releases/tag/v0.1.0)
