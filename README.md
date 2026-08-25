# Agentique

A GUI for running and supervising concurrent coding agents across many projects.
Every session drives a provider CLI (Claude Code or OpenAI Codex) inside its own
git worktree, so parallel agents never clobber each other's working tree.

The whole thing is one Go binary with the React frontend embedded. It runs on
your own machine, keeps its data in one directory, and needs nothing remote at
runtime. One UI can drive several machines at once, so a laptop, a desktop and a
VPS behave like one control surface.

- Go backend driving [agentkit/runtime](https://github.com/allbin/agentkit),
  with adapters for [claudecli-go](https://github.com/allbin/claudecli-go) and
  [codexcli-go](https://github.com/allbin/codexcli-go)
- React SPA over WebSocket, installable as a PWA
- SQLite (pure Go, no cgo), WebAuthn passkeys, systemd/launchd/Scheduled Task
  service integration

**Automating an install?** Skip to [Scripted install](#scripted-install), which
is written for an agent doing an unattended setup and names the two steps a human
still has to perform.

## Requirements

| Dependency | Required | Notes |
|---|---|---|
| `claude` >= 2.0.0 | yes | The default provider. `npm install -g @anthropic-ai/claude-code`, then `claude auth login`. |
| `git` | yes | Worktrees, branches, diffs. |
| `codex` | no | Only for sessions created with `provider: "codex"`. |
| `gh` | no | PR creation from the UI. Needs `gh auth login`. |
| `node` | no | Only to upgrade an npm-installed provider CLI. |

`agentique doctor` checks all of these plus data-dir permissions, free disk,
`claude`/`gh` auth state, and whether a server is already answering. It exits
non-zero when a required check fails, which makes it usable as a gate in a
script.

Published binaries, and where in-app self-upgrade is enabled:

| Platform | Binary published | In-app upgrade |
|---|---|---|
| linux/amd64 | yes | yes |
| linux/arm64 | yes | manual |
| darwin/arm64 | yes | manual |
| windows/amd64 | yes | manual |

Everything else needs a [source build](#development). "Manual" means the release
downloads and installs fine, the in-app **Upgrade** button just reports the
platform as unverified and tells you to re-run the installer. A platform
graduates when someone has actually run agentique on it, not when it compiles.

## Install

### Linux and macOS

```bash
curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash
```

Installs to `~/.local/bin/agentique`. Set `INSTALL_DIR` to put it elsewhere:

```bash
INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash
```

The installer verifies the release checksum and refuses to install if it cannot
fetch or match one. It then installs shell completions for whatever `$SHELL`
says you use, re-writes the systemd unit if one is already enabled, and runs
`agentique doctor`. If the install directory is not on `PATH` it prints the line
to add.

### Windows

```powershell
irm https://raw.githubusercontent.com/mdjarv/agentique/master/install.ps1 | iex
```

Installs `agentique.exe` to `%LOCALAPPDATA%\Programs\agentique` (override with
`$env:INSTALL_DIR`), verifies the checksum, adds the directory to your user
`PATH`, re-registers an existing scheduled-task service, and runs `doctor`.
Completions and the background service come from `agentique setup` and
`agentique service install`. The service is a per-user Scheduled Task, so no
admin elevation.

### Upgrading

Re-run the installer, or use the in-app **Upgrade** button on a verified
platform. If you run as a service, restart it afterwards:

```bash
agentique service restart
```

An in-app upgrade keeps the binary it replaced, so `agentique rollback` puts it
back.

A restart is not a pause. The new process reaps the CLI process groups the old
one left behind, so restarting mid-turn ends that turn. Sessions themselves
survive: worktrees, history and metadata are on disk. Both the UI and
`agentique rollback` say this before they act.

## First run

The guided path:

```bash
agentique setup
```

The wizard asks about the listen address, TLS (it can generate a self-signed
`localhost` cert), authentication, an initial project, and whether to install the
background service. It writes the answers to
[`config.toml`](#configuration-file) so they survive restarts.

Then start it and register:

```bash
agentique serve
```

Open the printed URL. **The first browser to complete registration becomes the
admin.** Do that immediately, especially on a network-reachable listener,
because otherwise whoever reaches the page first claims the account.

## Scripted install

Everything below is non-interactive and safe to run from an agent or a
provisioning script. Two steps cannot be scripted, and they are called out where
they fall.

`agentique setup` is a terminal wizard with no non-interactive mode. Write
`config.toml` directly instead.

### 1. Install the binary

```bash
curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"
agentique --version
```

### 2. Check dependencies before going further

```bash
agentique doctor || exit 1
```

Non-zero means a required dependency is missing or broken. Fix it before
continuing. The usual cause on a fresh box is a missing or unauthenticated
`claude`.

### 3. Write the config file

The service unit runs a bare `agentique serve` with no flags, so **every setting
the service uses has to be in `config.toml`.** Flags you pass by hand do not
reach it.

Linux path is `~/.config/agentique/config.toml`; macOS and Windows keep it in the
data directory. Create the directory yourself if it does not exist, and
`chmod 600` the file afterwards — the server does not tighten a config it did not
write, and this one can hold an embeddings API key.

```bash
mkdir -p ~/.config/agentique
cat > ~/.config/agentique/config.toml <<'TOML'
...
TOML
chmod 600 ~/.config/agentique/config.toml
```

A localhost-only machine:

```toml
[server]
addr = "localhost:9201"

[setup]
initial-project = "/home/you/git/some-project"
```

A machine reachable over a tailnet or LAN, which is what you want for anything
that will be paired into another machine's UI:

```toml
[server]
addr      = "0.0.0.0:9201"
tls-cert  = "/home/you/.config/agentique/tls/cert.pem"
tls-key   = "/home/you/.config/agentique/tls/key.pem"
rp-id     = "box.tail1234.ts.net"
rp-origin = "https://box.tail1234.ts.net:9201"
machine-label = "vps"

[setup]
initial-project = "/home/you/git/some-project"
```

Three things go wrong here often enough to name:

- `rp-id` and `rp-origin` must match the hostname users type. WebAuthn refuses
  to validate otherwise, and the failure looks like a broken passkey rather than
  a config error.
- Any non-`localhost` origin needs real HTTPS. `tailscale cert <name>` produces a
  usable pair. Browsers treat `localhost` as secure, so plain HTTP is fine there
  and nowhere else.
- `initial-project` only fires when the database holds zero projects. With it
  unset, the first start registers the server's working directory (or its git
  root) instead, which for a service is your home directory.

### 4. Install and start the service

```bash
agentique service install
agentique service status
```

On Linux this writes `~/.config/systemd/user/agentique.service`, enables it,
starts it, and calls `loginctl enable-linger` so it survives SSH logout. The unit
pins `PATH` to whatever `PATH` was at install time, which matters when `claude`
lives somewhere non-standard: install the service from a shell where `claude` is
on `PATH`. macOS gets a launchd agent, Windows a logon Scheduled Task.

The unit references the binary by absolute path, so install the binary to a
stable location first.

### 5. Verify it answers

```bash
curl -fsS http://localhost:9201/api/health
```

Returns `{"status":"ok", "version": ..., "machineId": ..., "machineLabel": ...}`.
`machineId` is stable for the life of the data directory and is what the
multi-machine catalog keys on.

### 6. Register the first passkey (human required)

Open the machine's URL in a browser and register. WebAuthn needs a real
authenticator, so no script can do this. It takes about ten seconds and only
happens once per machine.

Verify from the shell:

```bash
agentique auth status
```

Until a user exists, `agentique pair` fails with `no admin user registered yet`.

### 7. Pair it into an existing UI (human required for the last click)

Scriptable half, on the new machine:

```bash
agentique pair --ttl 15m
```

This prints the machine label, every address a client could plausibly reach it
on (listen address, configured public origin, tailnet name), a single-use token,
and an expiry. It authorizes itself with the `admin-secret` file in the data
directory rather than by opening a second writer to the live database, so it
works over SSH with the server running.

Human half, in the primary's UI: sidebar footer, server icon, **Add machine**.
Agentique servers on the same tailnet show up as suggestions; otherwise paste the
HTTPS address. Then paste the token. Pairing pins the remote's signing identity
before any credential is sent, which is why there is no supported curl recipe for
it.

### 8. Confirm the pairing

On the new machine:

```bash
agentique auth sessions
```

The paired client appears with kind `pair`. Revoke it later with
`agentique auth revoke <id>`.

### Idempotency

Re-running any of this is safe. The installer no-ops when the version already
matches, `service install` never restarts a running service behind your back, and
`initial-project` is ignored once any project exists. The one non-idempotent step
is pairing: each token is single-use, and pairing twice creates two catalog
entries.

## Multi-machine

One UI drives agentique servers on several machines. The server whose page you
opened is the **primary**; the others are paired to it. Their projects and
sessions appear alongside local ones, and same-repo projects merge into one entry
matched by canonical git remote (SSH and HTTPS clones of one repo count as the
same repo). Starting a session gets a **Run on** picker, and every session row
carries a machine indicator.

Paired machines live on the primary, so every device signing into it (phone PWA,
desktop browser) sees the same set. Pair once, use everywhere. Bearer credentials
stay in browser memory and never reach `localStorage`.

Machines come and go. A suspended laptop's projects and sessions stay visible
from cache, marked with their connection state, and re-sync when it returns.
Removing a machine revokes its credential on the remote before deleting the local
entry, so the remote has to be reachable. A remote whose credential was rejected
says so and offers a re-pair.

Remotes must listen on an address the browser can reach, over HTTPS for anything
but `localhost`. Creating projects, browsing files and git operations all work
across machines. Teams, schedules and the brain stay per-machine.

See [docs/multi-machine.md](docs/multi-machine.md) for the architecture.

## Running

### Foreground

```bash
agentique serve                       # localhost:9201
agentique serve --addr 0.0.0.0:9201   # all interfaces
agentique                             # status: address, TLS/auth, health, sessions
agentique doctor                      # dependency and health check
```

### As a service

```bash
agentique service install     # install, enable, start
agentique service status      # running? PID? unit path
agentique service restart     # after an upgrade
agentique service logs        # journald, launchd, or the JSON log on Windows
agentique service stop
agentique service uninstall
```

The Linux unit puts every session CLI and its Playwright/Chromium subtree in one
cgroup, with `KillMode=control-group` so a stop or restart takes the whole tree
down rather than orphaning subprocesses, `OOMPolicy=kill`, and
`MemoryHigh=70%` / `MemoryMax=85%` so a burst of warm sessions cannot exhaust
host memory. Edit the unit or add a drop-in to tune those.

### System tray

`agentique tray` runs a notification-area controller: status dot, **Open**,
**Start**, **Stop**, **Restart**, **Quit**. It does not host the server. With a
service installed it drives that, so it never fights restart-on-failure;
otherwise it launches a detached `serve` that outlives the tray. Quitting the
tray leaves the server up.

`agentique service install --tray` also autostarts it on login (an XDG autostart
entry on Linux, a second logon task on Windows). Headless Linux has no tray. The
tray is pure Go on Windows and Linux; macOS needs a `CGO_ENABLED=1` build.

## Authentication and security

Authentication is on by default and uses WebAuthn passkeys. There are no
passwords.

**Agentique is a single-operator trust domain, not a multi-tenant service.** Any
authenticated user can create a session, and a session runs agents with tool
access, which is code execution on the host. So every user you invite must be as
trusted as the machine owner. The admin bit covers credential and catalog
administration; it is not a sandbox around ordinary users, and no read-only role
exists.

- **First visitor becomes admin.** No invite needed. Register right after first
  start.
- **Further operators join by invite.** The admin generates tokens in the UI;
  they are valid seven days.
- **Manage auth from the CLI.** `agentique auth status` lists users,
  credentials and sessions. `agentique auth rekey` clears credentials so
  everyone re-registers. `agentique auth reset` wipes all users. Stop the server
  before `rekey` or `reset`; the CLI enforces this so no live socket outlives a
  direct database reset.
- **Recovery needs a code, not a name.** `agentique auth rekey` prints a
  one-time code per user, valid fifteen minutes, and registering the replacement
  passkey requires it. Without that, the window where no credentials exist would
  be an open door for anyone watching. Lost the code? `agentique pair` against
  the running server mints another.

Deployment guidance:

| Scenario | Config |
|---|---|
| Local, single user | Defaults: `localhost:9201`, auth on. Or `--disable-auth` for zero friction on that machine only. |
| LAN, tailnet, remote | Keep auth on, enable TLS, set `rp-id` and `rp-origin` to the hostname users connect to. |

`--disable-auth` grants anonymous full machine access, so it is accepted only on
a loopback listener, and requests must carry a loopback `Host` too. That second
check stops a hostile DNS name from rebinding a browser onto the local server.
Network listeners always require authentication, and pairing refuses to work with
auth off.

Session, pairing and invite tokens are stored as SHA-256 digests. A copy of the
database yields no usable credential. The exception is each paired machine's
bearer token, which this server presents to a remote and therefore has to stay
recoverable; it is protected by the data directory's owner-only mode.

> Upgrading across the hashed-token migration signs everyone out. It replaces the
> plaintext columns rather than converting them, so browsers log in again and each
> paired machine needs `agentique pair` once.

## Configuration

Precedence, highest first:

1. An explicitly-passed CLI flag
2. An environment variable, for the settings that have one
3. `config.toml`
4. Built-in default

A missing config file is not an error.

`agentique setup` writes the file `0600`, because it can carry an embeddings API
key. **The server does not tighten a config file you wrote yourself**, and on
Linux the config directory sits outside the owner-only data directory, so a
hand-written or scripted config keeps whatever your umask gave it. `chmod 600` it.

### Configuration file

`~/.config/agentique/config.toml` on Linux. `XDG_CONFIG_HOME` moves it;
`AGENTIQUE_HOME` moves both config and data. On macOS and Windows it lives in the
data directory.

Every section, with defaults:

```toml
[server]
addr          = "localhost:9201"  # listen address
disable-auth  = false             # anonymous access; loopback listener only
tls-cert      = ""                # with tls-key, enables HTTPS
tls-key       = ""
rp-id         = ""                # WebAuthn relying party ID (default: host from addr)
rp-origin     = ""                # WebAuthn origin (default: derived from addr)
machine-label = ""                # name shown to paired clients. Default:
                                  # PRETTY_HOSTNAME from /etc/machine-info,
                                  # else the OS hostname.

[session]
idle-evict-timeout = ""           # e.g. "30m": stop idle sessions to reclaim the
                                  # CLI process and its Chromium subtree. The
                                  # session resumes transparently on the next
                                  # message. "" disables eviction.

[scheduler]
disabled                 = false  # schedules persist but never fire
tick-interval            = "20s"  # due-schedule poll cadence
min-interval             = "1m"   # floor for cron cadence and dynamic delays
max-run-duration         = "30m"  # past this a run is overdue (attention, not error)
max-consecutive-failures = 3      # auto-pause after this many error terminals
run-history              = 200    # retained runs per schedule
once-catchup-window      = "1h"   # how stale a missed one-shot may still fire
dynamic-max-delay        = "6h"   # clamp on ScheduleNext delays
dynamic-fallback         = "20m"  # next fire if a dynamic run never reschedules

[logging]
level  = "info"   # trace, debug, info, warn, error
output = "auto"   # auto, journald, file, stdout

[backup]
interval = "15m"  # database snapshot interval
retain   = 7      # days of daily backups kept
disabled = false

[update]
disabled     = false  # no version check at all
interval     = "1h"   # how often this machine asks GitHub for the latest release
api-url      = ""     # override the releases endpoint (a fork, or a test stub)
arm-deadline = "4h"   # how long "upgrade when idle" waits before giving up

[setup]
initial-project = ""  # absolute path registered as a project when none exist

[experimental]
teams   = false  # Teams tab and multi-agent channel coordination
browser = false  # the in-app browser panel

[claude]
autocompact = ""     # "auto", or a token count between 100000 and 1000000.
                     # "" leaves the CLI's own behaviour alone. A bad value is
                     # rejected at startup, because the CLI rejects it at spawn
                     # and that surfaces as a session that dies.
forward-subagent-text = false  # surface what subagents say, not just that they
                               # ran. Off by default: real event-volume increase
                               # on subagent-heavy turns.
exclude-dynamic-system-prompt-sections = false  # move cwd/env/git-status out of
                               # the system prompt so the cached prefix is shared
                               # between sessions instead of diverging per worktree.

[brain]
# Semantic recall. Without these, recall and clustering fall back to
# keyword/Jaccard over the markdown files, which works but is weaker.
chroma-url  = ""
embed-url   = ""
embed-model = ""
embed-key   = ""
# Both thresholds are embedding-model specific. Recalibrate when you change
# models, or set autocal to derive them from the corpus at boot.
semantic-threshold = 0.45
vector-veto        = 0.15
autocal            = false
recall             = "on"   # "off" disables per-turn fact injection
# Optional LLM helpers. Unset means off. Values: haiku, sonnet, opus.
learn-model          = ""   # distil memories from a finished session on delete
outcome-model        = ""   # session-end judge: did recalled facts help?
consolidate-model    = ""   # unset falls back to deterministic dedup
consolidate-interval = ""   # e.g. "6h"; unset disables scheduled consolidation
# 0 and "" mean "use the built-in default", noted after each.
snapshot-retain          = 0     # kept snapshots under brain/.snapshots/ (7)
retry-max                = 0     # retries before a learn/outcome job is dead-lettered (5)
archive-after            = ""    # e.g. "720h": disuse-aging archival. "" is off:
                                 # no recall fade-out, no archive.
archive-confidence-floor = 0.0   # effective confidence below which a faded fact
                                 # is archived (0.35)

[brain.graph]
edge-cap             = 6      # semantic kNN edge density
edge-threshold       = 0.0    # defaults to semantic-threshold
link-strength-base   = 0.04   # frontend force layout
link-strength-span   = 0.32
link-distance-base   = 90
link-distance-span   = 55
gravity              = 0.045

# Replace a provider's auto-detected model list. A non-empty list replaces that
# provider's generated list entirely. This is the escape hatch for anything
# auto-detection misses; you should not normally need it, because model labels
# are derived from what the CLI reports.
# [[models.claude]]
# slug        = "opus"
# display     = "Opus 5"
# description = ""

# Publicly-routable dev-URL slots a session can lease to expose a Vite dev
# server. Each slot needs a unique slot name, port and public host.
# [[dev-urls]]
# slot        = "a"
# port        = 19301
# public-host = "myhost.example.ts.net"
```

### Server flags

These belong to `serve`, except `--addr`, which is global. Each has a config-file
equivalent above.

| Flag | Default | Description |
|---|---|---|
| `--addr` | `localhost:9201` | Listen address. `0.0.0.0:9201` binds all interfaces. |
| `--db` | platform data dir | Database file path. |
| `--disable-auth` | `false` | Anonymous access. Loopback listeners only. |
| `--tls-cert`, `--tls-key` | | Enable HTTPS. Both required. |
| `--rp-id` | host from `--addr` | WebAuthn relying party ID. |
| `--rp-origin` | derived from `--addr` | WebAuthn relying party origin. |
| `--log-level` | `info` | `trace`, `debug`, `info`, `warn`, `error`. |
| `--log-output` | `auto` | `auto`, `journald`, `file`, `stdout`. |
| `--backup-interval` | `15m` | Database snapshot interval. |
| `--backup-retain` | `7` | Days of daily backups kept. |
| `--disable-backup` | `false` | Turn off automatic backups. |
| `--test-mode` | `false` | Mock CLI connector and test endpoints. Not a sandbox flag: it does not isolate the data directory. |

### Environment variables

| Variable | Effect |
|---|---|
| `AGENTIQUE_HOME` | Overrides both the data and config directories. The only reliable way to isolate an instance. |
| `XDG_DATA_HOME`, `XDG_CONFIG_HOME` | Override the data or config directory. |
| `AGENTIQUE_DB` | Database file path. `--db` wins over it. |
| `LOG_LEVEL`, `JSON_LOG` | Log level, and the path of the JSONL log file. |
| `AGENTIQUE_MACHINE_LABEL` | Name shown to paired clients. |
| `AGENTIQUE_SESSION_IDLE_EVICT_TIMEOUT` | Same as `[session] idle-evict-timeout`. |
| `AGENTIQUE_SCHEDULER_*` | One per `[scheduler]` key, upper-snake-cased: `AGENTIQUE_SCHEDULER_DISABLED`, `_TICK_INTERVAL`, `_MIN_INTERVAL`, `_MAX_RUN_DURATION`, `_MAX_CONSECUTIVE_FAILURES`, `_RUN_HISTORY`, `_ONCE_CATCHUP_WINDOW`, `_DYNAMIC_MAX_DELAY`, `_DYNAMIC_FALLBACK`. |
| `AGENTIQUE_CLAUDE_*` | `_AUTOCOMPACT`, `_FORWARD_SUBAGENT_TEXT`, `_EXCLUDE_DYNAMIC_SYSTEM_PROMPT_SECTIONS`. |
| `AGENTIQUE_UPDATE_*` | `_DISABLED`, `_INTERVAL`, `_API_URL`, `_ARM_DEADLINE`. `_DISABLED` also silences provider-CLI version detection. |
| `AGENTIQUE_BRAIN_*` | One per `[brain]` key, plus `AGENTIQUE_BRAIN_GRAPH_*` for `[brain.graph]`. One name does not follow the pattern: `archive-confidence-floor` is `AGENTIQUE_BRAIN_ARCHIVE_FLOOR`. |

Every environment variable above wins over the config file and loses to an
explicitly-passed flag.

## Data and locations

| Platform | Data directory | Config directory |
|---|---|---|
| Linux | `~/.local/share/agentique` | `~/.config/agentique` |
| macOS | `~/Library/Application Support/agentique` | same as data |
| Windows | `%LOCALAPPDATA%\agentique` | same as data |

The data directory is created `0700`, and an existing one is tightened on the
next start. The database and its sidecars are `0600`.

Inside it:

- `agentique.db` — SQLite: sessions, projects, events, auth, machines. Plus its
  `-wal` and `-shm` sidecars.
- `backups/` — automatic snapshots. `agentique restore` lists and restores them.
- `brain/` — persistent agent memory, markdown as the source of truth.
- `worktrees/` — one git worktree per session. Created on first session.
- `session-files/` — files agents attach or produce, served back at
  `/api/sessions/{id}/files/…`. Only provably inert types render inline. HTML,
  SVG and anything unrecognized download as attachments, because these bytes come
  from agents and the app's own origin is where they would otherwise execute.
- `machine-id`, `machine-identity-key.pem` — this server's stable identity and
  its P-256 signing key. Paired clients pin both. Corrupt key material is fatal
  rather than silently replaced.
- `admin-secret` — what `agentique pair` and `agentique auth sessions` present to
  the running server, so the CLI never becomes a second writer to the live
  database.
- `agentique.lock`, `agentique.pid` — the single-instance lock and the running
  server's pid.
- `agentique.log.jsonl` — the structured log, when `--log-output file` is in
  effect (the default on Windows, and whenever `JSON_LOG` is set).

`worktrees/` and `session-files/` do not exist until the first session needs
them.

Projects point at local filesystem paths, so a database does not move between
machines. Pair the machines instead.

## CLI reference

The binary is both the server and a client to a running server.

| Command | Purpose |
|---|---|
| `agentique` | Status: address, TLS/auth, health, session summary. |
| `agentique serve` | Start the server. |
| `agentique doctor` | Dependency and health check. Non-zero on a required failure. |
| `agentique setup` | Interactive first-time configuration wizard. |
| `agentique service <install\|start\|stop\|restart\|status\|logs\|uninstall>` | Background service. |
| `agentique tray` | Notification-area controller. |
| `agentique upgrade` | Check for and install updates. |
| `agentique rollback` | Swap back to the binary an upgrade replaced. `--no-restart` to leave the service alone. |
| `agentique pair` | Mint a single-use pairing token. `--ttl` to change its lifetime. |
| `agentique auth <status\|sessions\|revoke\|rekey\|reset>` | Users, credentials, paired clients. |
| `agentique projects` | List projects. |
| `agentique sessions` | List sessions. |
| `agentique worktrees` | List sessions with active worktrees. |
| `agentique logs <id>` | A session's turn history. |
| `agentique follow <id>` | Stream live events for a session. |
| `agentique query <id> <prompt>` | Send a prompt to a session. |
| `agentique stop <id>` | Stop a running session. |
| `agentique export <id>` | Export a session as a Playwright test fixture. |
| `agentique cleanup` | Delete merged, terminal sessions. |
| `agentique prune` | Reclaim disk from finished and orphaned worktrees, Chrome profiles and scratchpads. Dry-run by default; `--apply` to delete, `--orphans-only` for the zero-risk subset. |
| `agentique restore [name\|index]` | List or restore database backups. |
| `agentique completion <shell>` | Shell completion script. |

Session arguments accept a unique ID prefix.

### Brain commands

| Command | Purpose |
|---|---|
| `brain list` | List memories. Filter `--scope`/`--category`, `--sort uses\|new`, `--json`. |
| `brain show <id>` | One memory's full text and frontmatter. Accepts an id prefix. |
| `brain search <query>` | Search through the live recall path, hybrid or keyword. |
| `brain stats` | Totals, per-scope counts, trust tiers, graph connectivity, semantic edges. |
| `brain snapshot` / `brain restore <id>` | Filesystem snapshot and restore. Restore writes a safety snapshot first. |
| `brain consolidate` | Consolidate one scope. `--project`/`--scope`, optional `--model`. |
| `brain assign-areas` | Recompute cross-scope topic areas. `--dry-run` to preview. |
| `brain calibrate` | Derive model-specific semantic thresholds from the corpus's own cosine distribution. |
| `brain reindex` | Rebuild the vector index from the markdown source of truth. |
| `brain backfill` | Extract durable memories from past transcripts. |
| `brain backfill-labels` / `backfill-subsumed` | One-time migrations. |
| `brain export <file>` / `brain import <file>` | Portable JSON bundles. Import maps projects interactively, or with `--map`/`-y`. |

## Architecture

```
   React SPA  <--- WebSocket / HTTP --->  Go server
   Vite                                   session.Manager (singleton)
   Zustand                                       |
   shadcn/ui                              agentkit/runtime
                                          neutral CLIEvent / CLISession
                                                 |
                                    +------------+------------+
                                    |                         |
                             claude adapter            codex adapter
                             (claudecli-go)            (codexcli-go)
                                    |                         |
                              claude processes          codex processes
```

Each session owns one provider CLI subprocess in its own process group, spawned
with a background context so it outlives the request that created it, plus one
git worktree. The server talks to every provider through agentkit's neutral
event contract, so provider choice is a per-session decision and neither adapter
leaks its native types into the session pipeline.

The Go binary embeds the built frontend through `embed.FS`. In development the
two run as separate servers.

Backend packages live under `backend/internal/`, frontend code under
`frontend/src/`. Both are organised by subsystem and readable directly; the
subsystem docs below cover the parts whose design is not obvious from the code.

### Stack

Backend: net/http and gorilla/websocket, agentkit/runtime with claudecli-go and
codexcli-go adapters, WebAuthn, SQLite through modernc.org/sqlite (pure Go),
sqlc for queries, goose for migrations, TOML config.

Frontend: React 19, Vite, TanStack Router, Zustand, Tailwind 4, shadcn/ui,
react-markdown, Biome, PWA with an auto-updating service worker.

## Development

```bash
just dev            # both servers (stops any previous pair first)
just dev-frontend   # Vite HMR on :9200
just dev-backend    # Go server on 127.0.0.1:9201, auth disabled
just dev-mock       # frontend against MSW mocks on :9210, no backend needed
just dev-tls        # both with TLS, needs certs/server.{crt,key}
```

In development the frontend opens its WebSocket straight at `:9201` rather than
through the Vite proxy.

| Command | Purpose |
|---|---|
| `just build` | Production build: one binary with the frontend embedded. |
| `just install` | Build and install to `~/.local/bin`. |
| `just upgrade` | Install, restart the service, run doctor. |
| `just check` | Biome lint and `tsc --noEmit`. Must pass. |
| `just test-backend` | `go test ./... -race -short`. |
| `just test-frontend` | Vitest. |
| `just test-e2e` | Playwright. |
| `just sqlc` | Regenerate query code after editing SQL. |
| `just typegen` | Refresh generated frontend types after changing Go wire types. |
| `just release` | Cross-compile release binaries. |
| `just reset` | Delete local dev `.db` files. Never touches the production DB. |

Two things to know before running a second server locally:

- Single-instance enforcement is a lock on the **data directory**, not the listen
  address. Two servers on different ports still share one data dir's database,
  worktrees and CLI subprocesses. Isolate with `AGENTIQUE_HOME=<tmpdir>`. Passing
  `--db` or `--addr` alone does not isolate anything.
- An unstamped build (no `-X main.version`) writes `agentique.db` in the working
  directory instead of the data directory. `just build` stamps the version;
  `go run` does not.

[CLAUDE.md](CLAUDE.md) holds the engineering conventions and the invariants a
change must not break. Read it before changing anything.

## Documentation

| Where | What |
|---|---|
| [CLAUDE.md](CLAUDE.md) | Conventions, code-gen workflow, subsystem invariants. |
| [ROADMAP.md](ROADMAP.md) | What shipped, what is next, what was dropped. |
| [PRODUCT.md](PRODUCT.md), [DESIGN.md](DESIGN.md) | Product positioning and the design system. Generated and consumed by the `impeccable` skill. |
| [docs/tech-debt.md](docs/tech-debt.md) | Open debt by severity. Closed items are deleted, not struck through. |

Subsystem docs, all describing what is built today:

| Doc | Subsystem |
|---|---|
| [process-lifecycle.md](docs/process-lifecycle.md) | CLI subprocess lifecycle, the orphan reaper, idle eviction. |
| [multi-machine.md](docs/multi-machine.md) | Pairing, routing, offline behaviour, and the designed-not-built presentation sync. |
| [upgrades.md](docs/upgrades.md) | In-app upgrades across machines. |
| [scheduled-loops.md](docs/scheduled-loops.md) | Recurring prompts with run history and health. |
| [model-catalog.md](docs/model-catalog.md) | Listing models without shipping a release per upstream model. |
| [brain.md](docs/brain.md) | Persistent cross-session agent memory, and why it works that way. |
| [channels.md](docs/channels.md) | Channels, teams, `@spawn` delegation, and web-only personas. |
| [agent-browser.md](docs/agent-browser.md) | The browser an agent can drive. |
| [prompt-handoffs.md](docs/prompt-handoffs.md) | Runnable prompt blocks and structured session suggestions. |
| [workflows.md](docs/workflows.md) | Multi-agent workflow orchestration. |
| [agentkit-extraction.md](docs/agentkit-extraction.md) | Playbook for lifting the memory core into agentkit. |

## Notes

- The first session on a fresh CLI takes 30 to 40 seconds to start, which is
  provider CLI init.
- Codex supports resume and rate-limit events. Mid-turn send is emulated by
  queueing and replaying at the next idle boundary, so the live composer works.
  Codex does not natively support fork, plan mode, thinking, subagents,
  compaction events, MCP reconnect, or tool-progress ticks. The runtime
  advertises what each provider can do through `Capabilities()` and the UI gates
  features on that. The default `claude` provider has the full set.
</content>
</invoke>
