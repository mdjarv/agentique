# Design: structured prompt suggestions — a `SuggestSessionPrompt` tool

Status: **MVP implemented** (commit `20d62bd`). Supersedes the inline-XML authoring path for
parallel-work suggestions. Pairs with the stopgap shipped in commit `3cef7d2`.

**Shipped (MVP, `20d62bd`):** the tool (backend registration + auto-allow), the preamble
flip to tool-only guidance, and the frontend `suggest_session` segment rendering as a
`PromptCard`. Inline parser kept as a silent fallback. Tests: backend preamble + frontend
`segments` interception.

**Polish shipped (`8fbdf6a`):**
- *Provider decoupling (was: extract `useStartPrompt`).* `PromptGroupProvider` now takes
  `prompts: PromptBlock[]` instead of parsing markdown `content` itself — the caller parses
  (inline) or maps from tool calls (suggestions). This removes the `content=""` hack and is
  cleaner than a separate hook (both paths already share the launch context via the
  provider), so the hook extraction is moot.
- *Multi-suggestion grouping.* `buildSegments` groups consecutive `SuggestSessionPrompt`
  calls into one `suggest_session` segment (deduping repeated tool_use ids), so 2+ render
  under one "Start All" footer; intervening content breaks the group.

**Still deferred:** retiring the inline parser + recovery machinery — gated on adoption data
*and* legacy-message rendering (old messages in `session_events` still contain inline
blocks). **Not yet done:** real-codex-session verification + adoption measurement (the open
risk below) — both need the branch deployed; the rendering/grouping is unit-verified only.

## Context

Agentique asks the agent to surface parallelizable work as clickable cards. Today the
agent emits that as **freeform markup inside its reply text**:

```
<agentique type="prompt" title="…">
…body…
</agentique>
```

A frontend state machine (`frontend/src/lib/prompt-parsing.ts`) extracts those blocks and
renders each as a `PromptCard`. This is the **only** structured payload agentique asks the
model to produce as hand-written markup — everything else structured (send a message,
rename the session, add a memory, lease a dev URL) goes through the in-process MCP server
as a schema-validated tool call.

That asymmetry is the bug source. Agents **semi-regularly malform the closer**, closing the
block with `</parameter>` (or `</prompt>`) instead of `</agentique>` — a bleed from
function-calling syntax, where attribute-bearing tags close with `</parameter>`. The parser
recovers every known variant into a clickable warned card (`preprocessAgentiqueTags`), and
the stopgap (`3cef7d2`) reframed the preamble ("plain text, not a tool call") and silenced
the warning for the two understood closers. But recovery is a **mitigation, not a cure** —
the malformed class still exists, and the parser/recovery machinery is non-trivial
(state machines, lookahead, depth tracking, recovery boundaries).

**The structural fix:** make the suggestion a tool call. Args are JSON-schema-validated by
the SDK; a malformed call is rejected and the model auto-retries. The entire malformed class
becomes impossible, and the parser/recovery code can eventually retire.

## Decision

Add **`SuggestSessionPrompt`** as an agentique in-process MCP tool. The agent calls it (one
call per suggestion) with `{ title, prompt, project? }`. The tool_use event is rendered as
the existing `PromptCard` via the established custom-segment interception pattern.

### 1. Tool contract (backend)

Register alongside the other agentique tools in `backend/internal/mcphttp/setup.go`, using
the `SetSessionName` template (typed args + `akmcp.TypedHandler`, `setup.go:141-155`):

```go
// constants — setup.go:27-48
ToolSuggestPrompt        = "SuggestSessionPrompt"
SuggestPromptToolFullName = "mcp__" + ServerName + "__" + ToolSuggestPrompt

type suggestPromptArgs struct {
    Title   string `json:"title"`
    Prompt  string `json:"prompt"`
    Project string `json:"project,omitempty"` // project slug; omit for current project
}

register(h, akmcp.Tool{
    Name: ToolSuggestPrompt,
    Description: "Surface a ready-to-launch session prompt to the user as a clickable " +
        "card. Use when you spot independent work that could run as its own parallel " +
        "session. Call once per suggestion. `title` becomes the session name; `prompt` " +
        "is the full self-contained task (the new session sees only this, not the " +
        "current conversation). `project` targets a different project by slug; omit it " +
        "for the current project.",
    InputSchema: akmcp.ObjectProp{
        Properties: map[string]akmcp.Property{
            "title":   akmcp.StringProp{Description: "Short session name (max ~80 chars)."},
            "prompt":  akmcp.StringProp{Description: "Full, self-contained task for the new session."},
            "project": akmcp.StringProp{Description: "Optional project slug to target a different project."},
        },
        Required: []string{"title", "prompt"},
    },
    Handler: akmcp.TypedHandler(func(_ context.Context, _ string, _ suggestPromptArgs) akmcp.Result {
        // Pure UI affordance — no server-side side effect. The card renders from the
        // persisted tool_use event on the frontend; this handler just acks the model.
        return akmcp.TextResult("Suggestion surfaced to the user.")
    }),
})
```

**Simpler than `SendMessage`.** `SendMessage` uses a deny-with-success interceptor
(`messaging.go:319-349`) because it must (a) stop the CLI's MCP handler from acting and
(b) route the message through the event pipeline (`event_pipeline.go:485-493`).
`SuggestSessionPrompt` has **no side effect to route** — the card is rendered purely from
the tool_use event. So it just **auto-allows** and lets the benign handler ack.

Auto-allow by adding it to the interceptor map in `session.go:247-270`
(`agentiqueInterceptors`), next to the dev-url/memory tools, using the existing `allow`
func:

```go
AgentiqueSuggestPromptTool: allow,   // mcp__agentique__SuggestSessionPrompt
```

`classifyTool` (`wire.go:476-506`) already maps any `mcp__*` name to category `"mcp"`, so no
change is required there; optionally add an explicit case for a dedicated icon.

**Persistence is free.** The tool_use event is already written to `session_events` and
rebuilt on reload — so the card survives refresh with no extra storage, unlike inline text
which must be re-parsed from prose on every render.

### 2. Rendering (frontend)

Reuse the **`channel_send` interception pattern** (`frontend/src/lib/segments.ts:14,135-159`),
which already intercepts a specific MCP tool by full name and turns it into a custom segment
instead of a generic tool block:

1. **Segment build** (`segments.ts`): before the generic tool_use case, match
   `event.toolName === "mcp__agentique__SuggestSessionPrompt"` and emit a
   `suggest_session` segment carrying `{ title, prompt, project, toolId }` read off
   `event.toolInput` (`ToolUseEvent.toolInput`, `chat-types.ts:35-41`).
2. **Segment type**: add `suggest_session` to the segment union (mirrors `channel_send`).
3. **Render** (`SegmentRenderer.tsx`): map `suggest_session` → `PromptCard`. `PromptCard`
   already resolves a `projectSlug` to a project and supports cross-project launch
   (`PromptCard.tsx:273-315`); the `warning` prop is simply never set for tool-sourced
   cards.

**Refactor for reuse — extract `useStartPrompt`.** Today the launch logic lives in
`PromptGroupProvider.startPrompt` (`PromptCard.tsx:98-138`): `createSession(ws, pid, …)`
→ `submitQuery(ws, sid, prompt)` (`frontend/src/lib/session/actions.ts:106-179`, RPCs
`session.create` + `session.query`). Lift that into a focused `useStartPrompt()` hook so
both the inline `PromptCard` and the new tool-sourced renderer share one launch path,
instead of nesting `PromptGroupProvider` inside the activity pipeline. (Aligns with the
separation-of-concerns + focused-file conventions.)

### 3. System prompt (preamble)

Replace the `presetSuggestParallel` snippet (`backend/internal/session/preamble.go:18-48`)
— the entire XML-authoring block, closer rule, meta-prompt escaping, and self-verify — with
a short tool instruction:

```
When you spot independent work that could run as its own parallel session, call the
SuggestSessionPrompt tool — one call per suggestion — with a `title` (session name) and a
`prompt` (the full, self-contained task; the new session sees only this). Pass `project`
(a slug) to target a different project. Only suggest genuinely parallelizable work.
```

The cross-project block (`preamble.go:50-59`) collapses into "available project slugs: …"
since there's no example markup to show anymore. Net: the preamble **shrinks**, and every
failure mode it currently warns about disappears.

### 4. Coexistence & migration

This is **additive and reversible**, not a big-bang swap:

- **Keep the inline parser** as a fallback. It's low-noise after `3cef7d2`, and historical
  messages already in `session_events` contain inline blocks that must keep rendering.
- **New sessions** get the tool-only preamble and use the tool. Any stray inline block
  (e.g. a model quoting old habits) still renders via the existing path.
- **Phase-out:** once telemetry shows agents call the tool reliably, retire the
  recovery machinery (`findRecoveryBoundary`, `RECOVERY_WARNINGS`, the warning chip) first,
  then the inline preprocessor — keeping `findRawPromptBlocks`/`parsePromptFromCode` only
  for legacy DB rendering.

## Open questions (validate before/after rollout)

1. **Model propensity.** Inline text is "free" (part of the reply); a tool call is a
   deliberate act. Will agents call an optional tool as readily as they emit inline cards?
   *Mitigation:* a clear tool description + the preamble nudge. *Measure:* suggestion rate
   per eligible turn, before vs. after. This is the one thing the code can't predict.
2. **Inline interleaving.** Cards become tool blocks in the activity timeline rather than
   woven mid-prose ("parallelize these: [card][card]"). Recommend accepting this — uniform
   rendering, and the model can still introduce them in prose.
3. **Codex parity.** Both providers receive the same MCP config (`manager.go:746-759`,
   injected for Create/Resume/Reconnect at `:351,:503,:592`; no provider-conditional MCP
   logic). Still, **verify in a real codex session** that the tool surfaces and the card
   renders + launches — per the standing "verify tool contract via real codex sessions"
   practice. Codex lacks native subagents but this is a UI affordance, not a subagent.
4. **Grouping.** N suggestions = N `suggest_session` segments. Inline cards currently group
   visually via `PromptGroupProvider`; decide whether consecutive tool-sourced cards should
   group too. Defer to polish.
5. **Streaming.** A tool call is atomic — no progressive "pending" card. Acceptable.

## Consequences

- **Pro:** the malformed-closer class is eliminated *structurally*; clean typed data; the
  preamble shrinks; richer metadata is now trivial to add later (`agentType`, `dependsOn`,
  `priority`) as schema fields instead of new markup; cards persist as first-class events.
- **Con:** larger surface than the stopgap (backend tool + interceptor + frontend segment +
  preamble + a launch-hook refactor); two rendering paths during the transition.
- **Neutral:** cross-provider for free via the existing in-process MCP server.

## Key additions (code)

| Area | File | Change |
| --- | --- | --- |
| Tool def | `backend/internal/mcphttp/setup.go:27-48,141-155` | constant + `register(SuggestSessionPrompt)` (~25 lines) |
| Tool name | `backend/internal/session/messaging.go:21-43` | `AgentiqueSuggestPromptTool` constant |
| Auto-allow | `backend/internal/session/session.go:247-270` | add `allow` entry to `agentiqueInterceptors` |
| Preamble | `backend/internal/session/preamble.go:18-59` | replace XML block with tool instruction; shrink cross-project block |
| Category (opt) | `backend/internal/session/wire.go:476-506` | optional explicit case for a dedicated icon |
| Segment build | `frontend/src/lib/segments.ts:135-159` | intercept tool name → `suggest_session` segment |
| Segment type | frontend segment union / `chat-types.ts:35-41` | add `suggest_session` kind |
| Render | `frontend/src/components/chat/SegmentRenderer.tsx` | map `suggest_session` → `PromptCard` |
| Launch hook | `frontend/src/components/chat/PromptCard.tsx:98-138` → new hook | extract `useStartPrompt()` shared by both paths |

## Rollout / verification

- Run `just typegen` if any wire type changes; `just check` + `go test ./... -short` +
  the `prompt-parsing` / `segments` vitest suites must pass.
- Tests: backend tool-registration + interceptor test; frontend `segments` test for the
  interception; preamble test update (assert tool instruction present, XML guidance gone).
- **End-to-end in an isolated worktree** with real **claude and codex** sessions: agent
  calls `SuggestSessionPrompt` → card renders → "Start Session" launches a new session with
  the right prompt in the right project (current and cross-project).
