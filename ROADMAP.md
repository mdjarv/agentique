# Agentique - Project Roadmap

## Vision

A lightweight GUI for managing concurrent Claude Code agents across multiple projects.
Go backend leveraging [claudecli-go](https://github.com/allbin/claudecli-go), React frontend via WebSocket, deploys as a single embedded binary.

See [README.md](README.md) for architecture, tech stack, and development setup.

## Planned

- Hung session watchdog — detect sessions with no events for N seconds, auto-fail after timeout
- File checkpointing / rewind — claudecli-go supports `WithFileCheckpointing()` + `RewindFiles()`
- MCP server management — expose reconnect/toggle/status via WS
- Terminal emulator (xterm.js)
- Desktop app via Tauri
- Session templates / saved prompts
- Split pane session layout
- Use `/btw` (side query) for auto-naming and PR description generation — requires claudecli-go support for the `/btw` protocol

---

## Investigations

### Prompt Handoff: Sessions Spawning Sessions

**Status:** Preamble infrastructure done, frontend parsing next.

Sessions can produce prompt suggestions that spawn new sessions with one click. A runtime preamble (`session/preamble.go`) injects Agentique context into every session so Claude suggests prompts via ` ```prompt ``` ` fenced blocks.

**Next step:** Parse ` ```prompt ` blocks in assistant text, render as cards with "Start Session" buttons.

**Future:** Parent-child session tracking. Batch spawning. Custom tool approach if markdown parsing proves fragile.

---

### Sibling Session Awareness

**Status:** Future investigation — depends on preamble infrastructure.

**Concept:** Sessions know about other active sessions in the same project. Enables coordination: avoiding duplicate work, aligning on shared interfaces, reporting sibling status.

**How it'd work:** Build the preamble dynamically at connect time by querying active sessions for the project. Inject a summary like: *"Other active sessions: [B: 'refactor auth' (running), C: 'avatar upload' (idle)]"*.

**Key challenge: session descriptors go stale.** A session's initial prompt doesn't reflect where it ends up after several turns of discussion. Needs a mechanism for sessions to self-describe their current focus — either:
- Claude periodically emits a structured "status" (like a tool_use convention)
- Backend infers a summary from recent assistant text
- Session name gets updated as the conversation evolves

**Other open questions:**
- **Staleness during conversation:** Preamble is set at connect time. Sibling state changes mid-conversation won't be visible unless we can update the system prompt per-query (need to check claudecli-go support).
- **Token cost:** Grows linearly with active sessions. May need to cap or summarize aggressively.
- **Over-coordination:** Claude might spend tokens reasoning about siblings when it should just focus. Probably opt-in or only injected when >1 sibling exists.

