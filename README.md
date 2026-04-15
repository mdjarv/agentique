# Agentique

A GUI for managing concurrent Claude Code agents across multiple projects. Go backend wraps [claudecli-go](https://github.com/mdjarv/claudecli-go), React frontend connects via WebSocket, deploys as a single embedded binary.

## Prerequisites

- **Claude Code CLI** >= 2.0.0 (`npm install -g @anthropic-ai/claude-code`)
- **git** on PATH
- **gh** (optional -- needed for PR creation from the UI)

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash
```

Installs to `~/.local/bin/agentique`. Override with `INSTALL_DIR`:

```bash
INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash
```

The install script verifies checksums and runs `agentique doctor` to check dependencies.

## Usage

```bash
agentique serve              # Start on localhost:9201
agentique serve --addr 0.0.0.0:9201  # Bind all interfaces
agentique doctor             # Check dependencies
```

Open http://localhost:9201 and add projects via the + button in the sidebar.

## Quick Start (Development)

```
just dev            # Run both servers in parallel (with auto-stop of previous)
just dev-frontend   # Vite HMR on :9200
just dev-backend    # Go server on :9201
```

Frontend connects WebSocket directly to `:9201` (bypasses Vite proxy for reliability). In production, the Go binary embeds the built frontend via `embed.FS`.

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
                                                     claudecli-go Sessions
                                                         (one per tab)
                                                              |
                                                       +------------------+
                                                       |  Claude CLI      |
                                                       |  processes       |
                                                       +------------------+
```

### Backend (Go)

| Module | Purpose |
|--------|---------|
| `backend/cmd/agentique` | Entry point, DB init, default project creation |
| `backend/internal/server` | HTTP mux, SPA handler, embedded frontend assets |
| `backend/internal/ws` | WebSocket handler, hub (connection registry + broadcasting), wire message types |
| `backend/internal/gitops` | Pure git/gh CLI wrappers (merge, branch, worktree, diff, PR), no session dependencies |
| `backend/internal/session` | Session lifecycle (Service), GitService (orchestrates gitops), event streaming, state machine |
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
- **Claude integration:** github.com/mdjarv/claudecli-go
- **Database:** SQLite via modernc.org/sqlite (pure Go, no CGO)
- **Query generation:** sqlc
- **Migrations:** goose

### Frontend

- **Framework:** React 19
- **Build tool:** Vite
- **Routing:** TanStack Router
- **State management:** Zustand
- **Styling:** Tailwind CSS 4 + shadcn/ui (Catppuccin Mocha theme)
- **Markdown:** react-markdown + @tailwindcss/typography + react-syntax-highlighter
- **Linting/Formatting:** Biome

### Deployment

- Single binary: Go backend embeds built frontend assets via `embed.FS`
- Separate dev servers during development (Vite dev server + Go backend)

## Key Commands

| Command | Purpose |
|---------|---------|
| `just dev` | Run both servers in parallel |
| `just dev-frontend` | Vite dev server (:9200) |
| `just dev-backend` | Go backend (:9201) |
| `just dev-mock` | Frontend with MSW mocks (:9210, no backend needed) |
| `just build` | Full production build (single binary) |
| `just check` | Biome lint + tsc typecheck |
| `just test-backend` | Go tests |
| `just test-e2e` | Playwright e2e tests |
| `just sqlc` | Regenerate sqlc query code |
| `just reset` | Delete all .db files |

## Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `localhost:9201` | Listen address |
| `--db` | (see Data below) | Database path |
| `--disable-auth` | false | Skip WebAuthn authentication |
| `--tls-cert` / `--tls-key` | -- | Enable HTTPS (both required) |

## Data

All data lives under a single directory:

| Platform | Default |
|----------|---------|
| Linux | `~/.local/share/agentique` |
| macOS | `~/Library/Application Support/agentique` |
| Windows | `%LOCALAPPDATA%\agentique` |

Override with `XDG_DATA_HOME` or `AGENTIQUE_HOME` (takes full precedence).

- **Database:** `<data dir>/agentique.db`
- **Session worktrees:** `<data dir>/worktrees/`

## Notes

- Projects point to local filesystem paths -- not portable between machines.
- WebAuthn auth requires HTTPS for non-localhost origins. Use `--disable-auth` for local/trusted networks.
- First session creation takes ~30-40s (Claude CLI subprocess init).
- See [ROADMAP.md](ROADMAP.md) for vision, milestones, and future plans.
