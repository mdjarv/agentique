# Agentique

A GUI for managing concurrent Claude Code agents across multiple projects.

## Prerequisites

- **Claude Code CLI** >= 2.0.0 (`npm install -g @anthropic-ai/claude-code`)
- **git** on PATH
- **gh** (optional -- needed for PR creation from the UI)

## Quick Start

1. Download the binary for your platform
2. Make it executable: `chmod +x agentique-*`
3. Run: `./agentique-linux-amd64 serve`
4. Open http://localhost:9201
5. Add your projects via the + button in the sidebar

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `localhost:9201` | Listen address |
| `--db` | `~/.agentique/agentique.db` | Database path |
| `--disable-auth` | false | Skip WebAuthn authentication |
| `--tls-cert` / `--tls-key` | -- | Enable HTTPS (both required) |

## Data

- **Database:** `~/.agentique/agentique.db`
- **Session worktrees:** `~/.agentique/worktrees/`

## Notes

- Projects point to local filesystem paths -- not portable between machines.
- WebAuthn auth requires HTTPS for non-localhost origins. Use `--disable-auth` for local/trusted networks.
- No auto-updates. Grab new binaries when available.
