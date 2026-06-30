# Sessionless discussion personas (web-only)

Design doc / hand-off. Status: **proposed** (not yet implemented). Owner: TBD —
intended to be picked up as its own session.

## Problem

Discussion groups currently require a project. The coupling is structural at
every layer (`channels.project_id NOT NULL`, `sessions.project_id NOT NULL`, WS
validation, UI gate) — but it is **inherited, not intrinsic**. It exists only
because every persona is a full agentique `sessions` row, and sessions are
hard-coupled to a project.

The coupling is genuinely load-bearing for **repo-backed** discussions: the
shared worktree needs `project.Path`, and writer personas edit files / commit.
It is **dead weight** for **web-only** discussions (the default scope, the
"general discussion" use case): no repo, no worktree, no file edits — yet today
they still mint project-scoped sessions in a fake home.

Goal: web-only discussion personas should be **sessionless** — not an agentique
`sessions` row, not project-scoped, not worktree-backed — while repo-backed
discussions keep today's session path unchanged.

## Non-goals / hard constraints

- **No direct/metered LLM API.** Execution stays on `claudecli-go`. MAX/Teams
  auth flows through the Claude CLI subprocess; `anthropic-sdk-go` is the
  metered Messages API and is not available to us. "Sessionless" means *not an
  agentique session row* — **not** "an API call". The persona is still a Claude
  CLI subprocess. If the lightweight/rootless mode needs a capability the
  library doesn't expose, we add it **in `claudecli-go`**, not around it.
- Repo-backed discussions are untouched.
- No durable resume of web-only discussions across a server restart in v1
  (ephemeral — see Decisions).

## Principle: split execution by scope

| Scope | Persona is… | Project | Worktree | CWD |
|-------|-------------|---------|----------|-----|
| repo-backed | a `sessions` row (today's path) | required (correct) | shared, one per group | the worktree |
| web-only | a sessionless CLI subprocess, orchestrator-owned | **none** | none | per-discussion `MkdirTemp` |

This is the line that has been causing all the friction: the project was only
ever real for repo-backed. The split removes the coupling at the root instead of
papering over it (no sentinel "General" project needed).

## Pillar 1 — lightweight persona runtime (the real new build)

A web-only persona is a raw `runtime.CLISession` (claude adapter) connected
**directly**, bypassing `Manager.Create` and its DB-row/worktree/project/recall
machinery. The discussion orchestrator owns its lifecycle.

`runtime.ConnectParams` (agentkit `runtime/cli.go`) already carries everything
needed:

```
ConnectParams{
    WorkDir:     scratchDir,          // per-discussion os.MkdirTemp — NOT a project
    Preamble:    personaPreamble,     // persona system prompt (see below)
    Model:       spec.Model,
    Effort:      spec.Effort,
    AutoApprove: AutoApproveAll,      // headless: fullAuto, no human approver
}
```

- **Tools come free.** It is still Claude Code, so web-search and repo-read work
  unchanged; the read-only etiquette in `etiquetteLocked` stays valid. The
  earlier "wire tools into a direct-API agent" fork is moot.
- **Persona preamble** = persona system-prompt additions (from the
  `agent_profiles` / `PersonaConfig`, today threaded as
  `CreateParams.SystemPromptAdditions`) **minus** the project/memory/recall/
  devURL preambles. This matches today's `SkipRecall: true` intent: clean
  per-persona context, no brain blocks. Build a leaner preamble path rather than
  reusing `buildPreamble`'s full stack (`manager.go:348`).
- **Orchestrator owns:** connect → drive a turn (capture turn-complete text;
  stream partials to the channel timeline like `recordContribution` does today)
  → teardown (close subprocess, `RemoveAll` the scratch dir).

### Factoring (do this, don't duplicate)

Today the raw CLISession lifecycle is buried inside `Manager.Create` /
`ensureLive` / `QuerySession`. Extract the shared core —
`connect(preamble, workdir, model, effort, autoApprove)` + event-pump +
`Query(prompt) → text` + `Close()` — behind a small internal interface so
DB-sessions and sessionless personas use **one** runtime path:

```go
type personaRuntime interface {
    Query(ctx context.Context, prompt string) (string, error)
    Close() error
}
```

DB-sessions get this via the existing `Session`/`Manager` wrapper; web-only
personas get a thin direct implementation over `connector.Connect` +
`capturingConnector.pop()` (`manager.go:176`).

## Pillar 2 — data model (one migration; everything else is free)

The timeline is already session-independent — this is what makes the whole thing
cheap.

- **`messages`** (`migration 030`): typed + denormalized
  (`sender_type`, `sender_id`, `sender_name`), **no FK to sessions**. Personas
  post `sender_type: "persona"`, `sender_id: <agent_profile id>`,
  `sender_name: <persona name>`. **No schema change.** Confirm `"persona"` is
  handled wherever `sender_type` is switched (and stays out of
  `writeLegacyAgentMessageEvents`, per CLAUDE.md channels notes).
- **`channel_members`** (FK→`sessions`, `migration 022`): personas are **not**
  members. The orchestrator already owns the roster in memory
  (`Discussion.participants`) and the UI reads it from `DiscussionInfo.personas`.
  Replace the per-persona `JoinChannel` (which writes `channel_members` + emits
  an intro) with an in-memory roster + a `sender_type:"persona"` intro message.
  **No schema change.**
- **`message_deliveries`** (FK→`sessions` recipient): web-only personas have no
  inbox — the orchestrator drives them. Skip delivery fan-out for persona
  participants; verify no path requires a delivery row for them.
- **`channels.project_id`** → **nullable** (new migration
  `038_channels_project_nullable.sql`, `ON DELETE SET NULL`). A web-only
  channel has no project; repo-backed channels keep theirs. This is now
  *coherent* — there are no project-scoped persona sessions left to contradict a
  null channel project (the objection that killed this earlier). Run `just sqlc`
  after.

## Pillar 3 — orchestrator + validation + UI

- **`StartDiscussion`** (`discussion.go:152`): branch on `Scope`.
  - web-only: `ProjectID` optional → skip `GetProject`; `CreateChannel` with a
    null project; `os.MkdirTemp` scratch root; spawn N persona runtimes; no
    `CreateSession`, no `JoinChannel`.
  - repo-backed: today's path verbatim (project required, shared worktree,
    sessions, `JoinChannel`).
- **`runPersonaTurn`** (`discussion.go:382`): branch — web-only →
  `personaRuntime.Query`; repo-backed → `ensureLive` + `QuerySession`.
- **`recordContribution`** (`discussion.go:441`): `SenderType: "persona"` for
  web-only personas (currently hard-coded `"session"`).
- **teardown** (`teardownDiscussion`, `discussion.go:488`): web-only → close
  persona runtimes + `RemoveAll` scratch dir + dissolve the (empty) channel; no
  sessions/worktrees to reap.
- **Validation**: `validateProjectID` (`ws/messages_session.go:211`) allows empty
  for web-only discussions; `DiscussionStartPayload.Validate`
  (`ws/messages_discussion.go`) only requires `projectId` when scope is
  repo-backed.
- **Frontend** (`routes/discussions.tsx`): project picker shown/required only for
  repo-backed; `canSubmit` drops the `projectId !== ""` gate for web-only;
  write-access / autoCommit toggles disabled for web-only (no shared worktree).
- **UI audit ("audit all callers")**: any Teams/participant view that joins
  `channel_members`→`sessions` must tolerate persona participants (render from
  `DiscussionInfo` + `sender_type:"persona"` messages). Message rendering already
  works via the denormalized `sender_name`.

## Decisions

- **Persistence: ephemeral for v1.** A persona runtime is a live subprocess held
  in memory; a server restart ends in-flight web-only discussions. Durable
  resume is a follow-up (would need persona reconnect or a small
  `discussion_participants` table). Accepted.
- **Provider: claude-only for v1.** The sessionless treatment is claude-adapter
  specific; codex personas could get the same later via its adapter.
- **No file writes for web-only personas** — write access is a repo-backed
  concept (there is no shared worktree to write into).

## Phasing

1. Migration `038` — `channels.project_id` nullable + `just sqlc`.
2. Lightweight persona runtime + the `Manager`/`Session` factoring (the core,
   biggest piece). Run with `-race`.
3. Orchestrator scope-branch (`StartDiscussion` / `runPersonaTurn` /
   `teardownDiscussion`).
4. Validation + frontend (optional project, persona-participant rendering).
5. UI audit + tests + CLAUDE.md update (channels section + provider note).

## Touch points

- `backend/db/migrations/038_channels_project_nullable.sql`
- `backend/internal/session/discussion.go` — orchestrator scope-branch
- `backend/internal/session/{manager,service,helpers}.go` — runtime factoring
- `backend/internal/ws/{messages_discussion,messages_session}.go` — validation
- `frontend/src/routes/discussions.tsx` + project pickers + Teams views
- `CLAUDE.md` — channels + provider-abstraction notes
- Possibly `claudecli-go` — only if rootless/lightweight mode needs a new knob
