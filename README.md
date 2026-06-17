# Agentique

A GUI for managing concurrent coding agents across multiple projects. Go backend drives provider CLIs through [agentkit/runtime](https://github.com/allbin/agentkit) (Claude via [claudecli-go](https://github.com/allbin/claudecli-go), OpenAI Codex via [codexcli-go](https://github.com/allbin/codexcli-go)); React frontend connects via WebSocket; deploys as a single embedded binary.

Each session runs in its own git worktree, so concurrent agents never clobber each other's working tree.

## Prerequisites

- **Claude Code CLI** >= 2.0.0 (`npm install -g @anthropic-ai/claude-code`) — required for the default `claude` provider. Must be authenticated (`claude auth login`).
- **Codex CLI** >= 0.130 — required only when creating sessions with `provider: "codex"`. Not checked by `doctor`.
- **git** on PATH.
- **gh** (optional) — needed for PR creation from the UI; must be authenticated (`gh auth login`).
- **node** (optional) — only needed to upgrade the Claude CLI.

Run `agentique doctor` at any time to check all of the above (versions, PATH, auth, data dir, free disk).

> **Platform:** prebuilt binaries are published for **Linux x86_64** only. macOS and Windows are supported by the code (see [Data & locations](#data--locations)) but must be [built from source](#development).

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash
```

Installs to `~/.local/bin/agentique`. Override the target with `INSTALL_DIR`:

```bash
INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash
```

The installer verifies the release checksum, installs shell completions (fish/zsh/bash, detected from `$SHELL`), re-installs the systemd service unit if one is already enabled, and finishes by running `agentique doctor`. If `~/.local/bin` is not on your `PATH`, it prints the line to add.

Upgrade later by re-running the same command, or run `agentique upgrade` to see the instructions. If you run as a service, restart it afterward: `agentique service restart`.

## First-time setup

The fastest safe path is the guided wizard:

```bash
agentique setup
```

It walks you through:

- **Listen address** (localhost vs. binding to a LAN/Tailscale hostname),
- **TLS** — can generate a self-signed `localhost` certificate for you, or point at your own cert/key,
- **Authentication** (WebAuthn passkeys; see [below](#authentication--security)),
- an **initial project** to register, and
- optionally installing the **background service**.

It writes your choices to the [config file](#configuration-file) so they persist across restarts. You can skip the wizard and configure everything manually with flags or the config file instead.

## Running

### Foreground

```bash
agentique serve                       # start on localhost:9201
agentique serve --addr 0.0.0.0:9201   # bind all interfaces
agentique                             # status: address, TLS/auth state, health, session summary
agentique doctor                      # check dependencies and system health
```

Open the printed URL (default <http://localhost:9201>) and add projects via the **+** button in the sidebar.

### As a background service

Installs a systemd user unit (Linux) or launchd agent (macOS) that starts on login and restarts on crash:

```bash
agentique service install     # install + start
agentique service status      # running? PID? unit path
agentique service restart     # after an upgrade
agentique service logs        # stream logs (journald/launchd)
agentique service stop
agentique service uninstall
```

Install the binary to a stable location (`~/.local/bin`, `/usr/local/bin`) before installing the service — the unit file references the binary's current path.

## Authentication & security

Authentication is **on by default** and uses **WebAuthn passkeys** (Touch ID, security keys, platform authenticators) — there are no passwords.

- **First visitor becomes admin.** The first browser to complete registration is registered as the admin user, no invite required. **Register immediately after first start**, especially if the server is reachable beyond localhost — otherwise anyone who reaches the page first claims the admin account.
- **Additional users need an invite.** The admin generates invite tokens from the UI; new users register against a token (valid 7 days).
- **Manage auth from the CLI** with `agentique auth status` (list users/credentials/sessions), `agentique auth rekey` (clear credentials + sessions so everyone re-registers their passkey), and `agentique auth reset` (wipe all users — start over).

Guidelines for a safe deployment:

| Scenario | Recommended config |
|----------|--------------------|
| Local only, single user | Default (`localhost:9201`, auth on). Or `--disable-auth` for zero friction on a trusted machine. |
| LAN / Tailscale / remote | **Keep auth on, enable TLS.** WebAuthn requires a secure context (HTTPS) for any non-`localhost` origin. Set `--rp-id`/`--rp-origin` (or the config equivalents) to the hostname users connect to, or passkeys won't validate. |

`--disable-auth` allows **anonymous access** — only use it on a trusted, non-exposed host. `localhost` is treated as a secure context by browsers, so passkeys work there over plain HTTP; every other origin needs HTTPS.

## Configuration

Settings resolve in this order (highest precedence first): **CLI flags → config file → built-in defaults**.

### Configuration file

`~/.config/agentique/config.toml` on Linux (override location with `XDG_CONFIG_HOME` or `AGENTIQUE_HOME`; on macOS/Windows it lives in the data dir). A missing file is not an error — defaults apply. `agentique setup` writes this file for you. Full annotated example with defaults:

```toml
[server]
addr         = "localhost:9201"  # listen address
disable-auth = false             # true = anonymous access (trusted hosts only)
tls-cert     = ""                # path to TLS cert; with tls-key, enables HTTPS
tls-key      = ""
rp-id        = ""                # WebAuthn relying party ID (default: host from addr)
rp-origin    = ""                # WebAuthn origin (default: derived from addr)

[logging]
level  = "info"   # trace, debug, info, warn, error
output = "auto"   # auto, journald, file, stdout

[backup]
interval = "15m"  # database snapshot interval
retain   = 7      # days of daily backups to keep
disabled = false  # true disables automatic backups

[setup]
initial-project = ""  # absolute path auto-registered as a project on first run

[experimental]
teams   = false  # Teams tab / multi-agent channel coordination
browser = false  # in-app browser tooling

# Advanced: publicly-routable dev-URL slots a session can lease to expose a
# Vite dev server externally. Each slot needs a unique slot/port/public-host.
# [[dev-urls]]
# slot        = "a"
# port        = 19301
# public-host = "myhost.example.ts.net"
```

### Server flags

All flags below belong to `serve` (except `--addr`, which is global). Each has a config-file equivalent shown above.

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `localhost:9201` | Listen address (`host:port`). Use `0.0.0.0:9201` to bind all interfaces. |
| `--db` | platform data dir | Database file path. |
| `--disable-auth` | `false` | Disable WebAuthn — allow anonymous access. Trusted hosts only. |
| `--tls-cert` / `--tls-key` | — | Enable HTTPS (both required). |
| `--rp-id` | host from `--addr` | WebAuthn relying party ID. |
| `--rp-origin` | derived from `--addr` | WebAuthn relying party origin. |
| `--log-level` | `info` | `trace`, `debug`, `info`, `warn`, `error`. |
| `--log-output` | `auto` | `auto`, `journald`, `file`, `stdout`. |
| `--backup-interval` | `15m` | Interval between database snapshots. |
| `--backup-retain` | `7` | Days of daily backups to keep. |
| `--disable-backup` | `false` | Turn off automatic database backups. |

### Environment variables

| Variable | Effect |
|----------|--------|
| `AGENTIQUE_HOME` | Overrides **both** the data and config directories (full precedence). |
| `XDG_DATA_HOME` / `XDG_CONFIG_HOME` | Override the data / config directory (Linux). |
| `AGENTIQUE_DB` | Database file path (overridden by `--db`). |
| `LOG_LEVEL` / `JSON_LOG` | Log level / path of the JSONL log file. |
| `AGENTIQUE_BRAIN_CHROMA_URL`, `AGENTIQUE_BRAIN_EMBED_URL`, `AGENTIQUE_BRAIN_EMBED_MODEL`, `AGENTIQUE_BRAIN_EMBED_KEY` | Opt into semantic recall for the persistent agent memory ("brain"). Without them, recall falls back to keyword search over markdown files. |

## Data & locations

All persistent data lives under a single data directory:

| Platform | Data directory | Config directory |
|----------|----------------|------------------|
| Linux | `~/.local/share/agentique` | `~/.config/agentique` |
| macOS | `~/Library/Application Support/agentique` | (same as data dir) |
| Windows | `%LOCALAPPDATA%\agentique` | (same as data dir) |

Inside the data directory:

- `agentique.db` — SQLite database (sessions, projects, events, auth).
- `backups/` — automatic database snapshots (every `--backup-interval`, `--backup-retain` days kept). Manage with `agentique restore` (list/restore a snapshot).
- `worktrees/` — one git worktree per session.
- `session-files/` — files attached to or produced by sessions.
- `brain/` — persistent agent memory (markdown).
- `agentique.log.jsonl` — structured log.

> Projects point to local filesystem paths — a database is **not portable** between machines.

## CLI reference

Beyond `serve`/`doctor`/`setup`/`service`/`auth`/`upgrade`, the binary doubles as a client to a running server:

| Command | Purpose |
|---------|---------|
| `agentique` | Status: address, TLS/auth, health, session summary. |
| `agentique projects` | List projects. |
| `agentique sessions` | List sessions. |
| `agentique worktrees` | List sessions with active worktrees. |
| `agentique logs <id>` | Show a session's turn history. |
| `agentique follow <id>` | Stream live events for a session. |
| `agentique query <id> <prompt>` | Send a prompt to a session. |
| `agentique stop <id>` | Stop a running session. |
| `agentique export <id>` | Export a session as a Playwright test fixture. |
| `agentique cleanup` | Delete merged, terminal sessions. |
| `agentique restore [name|index]` | List or restore database backups. |
| `agentique brain backfill` | Extract durable memories from past transcripts. |

Session arguments accept a unique ID prefix.

## Architecture

```
+------------------+         WebSocket / HTTP          +------------------+
|                  | <-------------------------------> |                  |
|   React SPA      |                                   |   Go Backend     |
|   (Vite)         |                                   |                  |
|   Zustand        |                                   |  session.Manager |
|   shadcn/ui      |                                   |  (singleton)     |
+------------------+                                   +------------------+
                                                              |
                                                     agentkit/runtime
                                                     (neutral CLIEvent /
                                                      CLISession contract)
                                                              |
                                          +-------------------+-------------------+
                                          |                                       |
                                  claude adapter                          codex adapter
                                  (claudecli-go)                          (codexcli-go)
                                          |                                       |
                                  +---------------+                       +---------------+
                                  |  Claude CLI   |                       |   Codex CLI   |
                                  |  processes    |                       |   processes   |
                                  +---------------+                       +---------------+
```

### Backend (Go)

| Module | Purpose |
|--------|---------|
| `backend/cmd/agentique` | Entry point, CLI commands, DB init, default project creation |
| `backend/internal/server` | HTTP mux, SPA handler, embedded frontend assets |
| `backend/internal/ws` | WebSocket handler, hub (connection registry + broadcasting), wire message types |
| `backend/internal/gitops` | Pure git/gh CLI wrappers (merge, branch, worktree, diff, PR), no session dependencies |
| `backend/internal/session` | Session lifecycle (Service), GitService (orchestrates gitops), event streaming, state machine |
| `backend/internal/auth` | WebAuthn passkey registration/login, invites, sessions |
| `backend/internal/config` | TOML config file loading |
| `backend/internal/project` | Project CRUD routes |
| `backend/internal/store` | SQLite via sqlc -- generated query code, migrations via goose |

### Frontend (TypeScript + React)

| Module | Purpose |
|--------|---------|
| `frontend/src/components/chat/` | Chat UI -- message rendering, composer, turn blocks, tool display |
| `frontend/src/components/layout/` | Sidebar, project tree, session status |
| `frontend/src/hooks/` | useWebSocket (connection + reconnect), useChatSession, useProjects |
| `frontend/src/stores/` | Zustand -- app-store (projects), chat-store (sessions + turns), streaming-store (assistant text), selectors |
| `frontend/src/lib/` | Types, WS client (request/response correlation), event schemas, utils |

## Tech Stack

### Backend

- **HTTP/WS server:** net/http + gorilla/websocket
- **Provider runtime:** github.com/allbin/agentkit/runtime (neutral CLI surface; per-provider adapters)
- **Claude integration:** github.com/allbin/claudecli-go
- **Codex integration:** github.com/allbin/codexcli-go
- **Auth:** WebAuthn (passkeys)
- **Database:** SQLite via modernc.org/sqlite (pure Go, no CGO)
- **Query generation:** sqlc
- **Migrations:** goose
- **Config:** TOML (BurntSushi/toml)

### Frontend

- **Framework:** React 19
- **Build tool:** Vite
- **Routing:** TanStack Router
- **State management:** Zustand
- **Styling:** Tailwind CSS 4 + shadcn/ui (Catppuccin Mocha theme)
- **Markdown:** react-markdown + @tailwindcss/typography + react-syntax-highlighter
- **Linting/Formatting:** Biome

### Deployment

- Single binary: Go backend embeds built frontend assets via `embed.FS`.
- Separate dev servers during development (Vite dev server + Go backend).

## Development

```bash
just dev            # run both servers in parallel (auto-stops previous)
just dev-frontend   # Vite HMR on :9200
just dev-backend    # Go server on :9201 (binds 0.0.0.0, auth disabled)
just dev-mock       # frontend with MSW mocks on :9210 (no backend needed)
```

In development the frontend connects its WebSocket directly to `:9201` (bypassing the Vite proxy for reliability). In production the Go binary embeds the built frontend via `embed.FS`.

### Key commands

| Command | Purpose |
|---------|---------|
| `just build` | Full production build (single binary) |
| `just install` | Build and install locally from source |
| `just check` | Biome lint + tsc typecheck |
| `just test-backend` | Go tests |
| `just test-frontend` | Vitest |
| `just test-e2e` | Playwright e2e tests |
| `just sqlc` | Regenerate sqlc query code after editing SQL |
| `just typegen` | Refresh generated frontend types after changing Go wire types |
| `just reset` | Delete local dev `.db` files (not the production DB) |

See [CLAUDE.md](CLAUDE.md) for engineering conventions and the code-gen workflow.

## Notes

- First session creation takes ~30-40s (provider CLI subprocess init).
- **Provider capability differences.** Codex sessions support resume and rate-limit events, and mid-turn messages are emulated (queued and replayed at the next idle boundary, so the live composer works). Codex does **not** support natively: fork, plan mode, thinking, subagents, compaction events, MCP reconnect, or tool-progress ticks. The runtime advertises these via `Capabilities()` and the UI gates features accordingly. The default `claude` provider supports the full feature set.
- See [ROADMAP.md](ROADMAP.md) for vision, milestones, and future plans.
</content>
</invoke>
