# Claude CLI — Autocomplete Data Sources

The Claude CLI exposes no autocomplete API. All completion data must be
derived by the client at the application layer using the sources below.

---

## Slash Commands

### Built-in commands

There is no machine-readable listing. Known commands must be maintained as a
static list in the client. These include (non-exhaustive):

`/help`, `/clear`, `/compact`, `/review`, `/tools`, `/mcp`, `/permissions`,
`/cost`, `/history`, `/quit`

Verify against `claude --help` or `claude /help` for the installed version,
as the set changes between CLI releases.

### Custom commands

Custom slash commands are markdown files on disk. The filename (without `.md`)
becomes the command name, e.g. `review.md` → `/review`.

Two locations are scanned, in priority order:

| Priority             | Path                      | Scope         |
| -------------------- | ------------------------- | ------------- |
| 1 (overrides global) | `.claude/commands/*.md`   | Project-local |
| 2                    | `~/.claude/commands/*.md` | Global (user) |

Project-local commands shadow global commands with the same name.

**Implementation:** scan both directories at startup, union the results
(project-local wins on conflict). Optionally watch for filesystem changes
with inotify/FSEvents to keep the list live.

---

## @ File References

The `@` prefix lets users attach file contents to a message. There is no
server-provided file list — the client must resolve candidates from the
filesystem.

**Suggested sources, in order of relevance:**

1. Any directories passed via `--add-dirs` (these are explicitly in Claude's
   context window)
2. The current working directory tree
3. Git-tracked files (`git ls-files`) if the project is a repo — avoids
   surfacing ignored/build artifacts

Filter to files only (not directories), and consider excluding binary files
and common noise patterns (`node_modules`, `vendor`, `.git`, build outputs).

---

## Models

Model names are a fixed set, not queried at runtime. Current known values:

| Alias    | Full model ID               |
| -------- | --------------------------- |
| `haiku`  | `claude-haiku-4-5-20251001` |
| `sonnet` | `claude-sonnet-4-6`         |
| `opus`   | `claude-opus-4-6`           |

The CLI also accepts full model IDs directly. This list changes with new
releases; treat it as a minimum set and allow free-text entry as a fallback.

---

## Summary

| Feature              | Source                                     | Notes                                     |
| -------------------- | ------------------------------------------ | ----------------------------------------- |
| Built-in `/commands` | Static list                                | Verify per CLI version                    |
| Custom `/commands`   | `~/.claude/commands/`, `.claude/commands/` | Markdown filenames                        |
| `@` file references  | Filesystem walk                            | Filter by `--add-dirs`, git-tracked files |
| Model names          | Static list                                | Allow free-text fallback                  |
