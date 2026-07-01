# Design: runnable prompt blocks + reliable session hand-offs

Status: **Implemented.** Phases 1–3 shipped; Phase 4 (inline-parser retirement) deferred as
planned. Verified end-to-end against the mock backend: a plain fenced block → Run dropdown
(all projects, current floated + tagged) → cross-project navigation to `/project/the-pint/
session/new` with the block pre-filled into the composer, **unsent**, no session created.

Covers two features that together make "surface a launchable prompt to the user" reliable
instead of dependent on the model volunteering structure:

- **Feature 2 — user-driven "run block as a new session"** (the deterministic reliability
  floor): a Run dropdown on chat code blocks that opens a *new session* — same or another
  project — with the block pre-filled into its composer, **without sending**.
- **Feature 1 — enforce + improve the existing `SuggestSessionPrompt` card**: strengthen the
  preamble so the model reaches for the tool on cross-project hand-offs (not just
  "parallelizable work"), and add an *Edit-before-running* path to the card so it can share
  Feature 2's prefill-and-edit primitive.

## Decisions taken (from review)

| # | Decision | Choice | Consequence |
| - | -------- | ------ | ----------- |
| Q1 | Feature 2 target scope | **No "Run here." A block Run button always creates a *new* session** (same or another project) and pre-fills it. | The active session already has the block in its context, so re-running there is meaningless. This also removes all coupling to a *mounted* composer — the hard part of the original tension evaporates. |
| Q2 | Should `PromptCard`'s spawn behavior change? | **Keep one-click spawn (and "Start All"); add an "Edit before running" option** that pre-fills instead of spawning. | Card retains its frictionless parallel-launch value; gains a review path that reuses Feature 2's primitive. |
| Q3 | Enforcement approach | **Feature 2 as the deterministic floor + strengthen the preamble** for hand-offs / specs-for-another-repo. **No output lint / intent-guessing.** | Reliability no longer depends on the model volunteering structure; the preamble nudge is a cheap probabilistic uplift on top. |

## Background — current state

Agentique surfaces launchable prompts two ways:

1. **Structured (`SuggestSessionPrompt`)** — an in-process MCP tool the model *chooses* to
   call; its `tool_use` renders as a `PromptCard` (`segments.ts:185`, `SegmentRenderer.tsx`
   → `SuggestSessionView`). Card launch = `createSession` + `submitQuery`
   (`PromptCard.tsx:103`), with a project picker already present when >1 project exists
   (`PromptCard.tsx:311,408`).
2. **Legacy inline** — `<agentique type="prompt">` / ` ```prompt ` markup parsed by
   `prompt-parsing.ts` (three fence state machines + recovery). Kept only as a fallback and
   for rendering old messages.

**The weakness the task targets:** both paths require the model to *volunteer* structure. When
the model instead writes a plain markdown code fence (a bash snippet, a spec, a hand-off
prompt "for the mobilix repo"), there is **no button and no fallback** — the affordance
silently doesn't exist. Reliability must not depend on the model choosing to structure output.

## Feature 2 — run a code block as a new session (the floor)

### Behavior

Every chat code block gets a small **Run** control next to the existing Copy button
(`Markdown.tsx` `PreBlock`, the `.code-block-wrapper`). Activating it:

1. Opens the **new-session view** for the chosen project (`/project/$slug/session/new`, already
   rendered by `NewChatPanel`).
2. **Pre-fills** that view's composer with the block's contents.
3. **Does not send.** The user edits/augments, picks model/worktree/etc. in the composer
   toolbar, then sends — which is when the session is actually created.

Target selection (dropdown): **New session in `<current project>`** (first) and **New session
in `<other project>`…** (one per project). With a single project, it degrades to one plain
action. "Current project" is resolved from the active session
(`useChatStore.getState().activeSessionId` → its `projectId` → slug).

### Why this is the reliability floor

It is **deterministic** and **model-independent**: any fenced block the model emits — however
it chose to format it — is one click from becoming a real, editable session prompt. No parser,
no schema, no model judgment. That is the whole point.

### Mechanism — reuse `NewChatPanel`'s deferred creation (orphan-free)

`NewChatPanel` **already** defers session creation until first send and pre-fills from
`drafts["new:<projectId>"]` (`newSessionDraftKey`, `NewChatPanel.tsx:27,88`) with
`composerInitialText = persistedDraft || initialPrompt` (`:89`). The dedicated route already
validates a `prompt` search param (`project.$projectSlug.session.new.tsx:6-9`).

So the entire prefill primitive is:

```ts
// lib/session/new-session-draft.ts  (new focused file)
export function openPrefilledNewSession(
  navigate: NavigateFn,
  { projectId, projectSlug, text }: { projectId: string; projectSlug: string; text: string },
) {
  const key = newSessionDraftKey(projectId);            // "new:<projectId>"
  const existing = useUIStore.getState().drafts[key] ?? "";
  // Never clobber an in-progress new-session draft: append with a separator.
  const next = existing.trim() ? `${existing.trimEnd()}\n\n${text}` : text;
  useUIStore.getState().setDraft(key, next);
  navigate({ to: "/project/$projectSlug/session/new", params: { projectSlug } });
}
```

`NewChatPanel` mounts, reads the draft as its composer's initial text, and creates the session
only on send. **No session/worktree exists until the user commits** — abandoning the draft
costs nothing. Cross-project is the same call with a different slug.

Chosen over the two alternatives:
- *`?prompt=` search param* (route already supports it): simplest, but a large block becomes a
  multi-KB URL (history noise, length limits). Rejected for arbitrary block sizes.
- *Set draft on a freshly-`createSession`'d session, then navigate*: creates a real
  worktree/session on every click → orphans on abandonment. Rejected.

`setDraft` uses precedence `persistedDraft || initialPrompt`, so the draft we set wins. We
append (not replace) to protect any half-written new-session prompt; the composer's autosize +
caret-at-end already handle prefilled text (`ComposerTextarea.tsx:113-118`).

### UI notes

- Control lives in `PreBlock` beside `CopyButton`. It is a `DropdownMenu` (project list) when
  `>1` project, else a single icon button. Skip **mermaid** blocks (rendered as diagrams, not
  code) and empty blocks.
- Reads `projects` from `app-store`. **Zustand stable-ref rule (CLAUDE.md):** never return a
  fresh `.map()`/array from the selector — subscribe to the stable `projects` array (and the
  active `sessionId`) and derive slugs in render or via `useShallow`.
- `PreBlock` is a generic markdown renderer used outside sessions too (agent messages,
  discussions). When no active session resolves a "current project," omit the current-project
  entry and just show the project list; if there are zero projects, hide the control.

## Feature 1 — enforce + improve the structured suggestion

### Enforce (preamble + the Feature 2 floor)

`presetSuggestParallel` is worded around "independent work that could run as its own parallel
session," which misses the **hand-off / spec-for-another-repo** case — exactly when the model
tends to emit a plain fence. Broaden it (`preamble.go:19-31`):

> When you write a **self-contained task or spec meant to run as its own session** — parallel
> work you're offloading, **or a hand-off/spec written for another repo's agent** — surface it
> by calling `SuggestSessionPrompt` (one call per suggestion) instead of pasting it as a plain
> code block. `title` becomes the session name; `prompt` is the full task (the new session sees
> only this). Pass `project` (a slug) to target another project.
>
> Don't force it, and don't suggest work you can finish faster yourself right now.

The cross-project block (`crossProjectInstructions`, `:28-31`) similarly gains an explicit
"writing a prompt/spec for another repo → set `project`" line. Backend preamble test asserts
the hand-off wording is present.

This is **probabilistic** (model-dependent) and that is acceptable *because Feature 2 is the
deterministic backstop*: if the model still emits a fence, the Run dropdown makes it runnable
anyway. We deliberately **do not** add an output-scanning lint that guesses whether an arbitrary
fence "was meant to be a prompt" — false positives, and it re-grows the parser surface we want
to shed.

### Improve — one prefill primitive, shared by card and code block

Add an **"Edit before running"** item to `PromptCard`'s action dropdown. It calls the same
`openPrefilledNewSession` helper (targeting the card's resolved project), so a card's prompt can
be reviewed/edited in the new-session composer before it spawns — consistent with Feature 2 and
with "do not autosubmit."

- The primary **Start Session** button and **Start All** keep today's one-click
  `createSession`+`submitQuery` / `createSwarm` behavior (`PromptCard.tsx`, `PromptGroupProvider`).
- The existing per-project picker (`showProjectPicker`) is unchanged; "Edit before running"
  is added as a trailing item. When only one project exists (no dropdown today), introduce a
  caret that opens a one-item menu with "Edit before running."
- Cross-project targeting reliability is already handled by the picker + slug resolution
  (`PromptCard.tsx:301-315`); no change needed beyond the added edit path.

### Inline parser — harden by retiring, not patching

With a structured path (`SuggestSessionPrompt`) **and** a deterministic floor (Feature 2), the
inline `<agentique type="prompt">` authoring format and its recovery machinery
(`preprocessAgentiqueTags`, `findRecoveryBoundary`, `RECOVERY_WARNINGS`, the warning chip) are
redundant for **new** output. Recommendation (deferred, not blocking either feature):

- Keep `findRawPromptBlocks` + `parsePromptFromCode` + `splitByPromptBlocks` for **legacy DB
  message rendering** (old `session_events` still contain inline blocks).
- Retire the **recovery machinery** first (it only ever mattered for malformed *authoring*,
  which the model no longer needs to do), then the `<agentique>` preprocessor — gated on
  telemetry showing no new inline blocks, and aligned with the planned decomposition of
  `prompt-parsing.ts` into `fence-scan.ts` / `agentique-tags.ts` / `prompt-blocks.ts` so the
  `<agentique>` handling sits in one deletable module.

## Phased plan

1. **Feature 2 — code-block Run dropdown + `openPrefilledNewSession`.** Deterministic floor,
   frontend-only, no backend/wire changes. Ship first.
2. **Feature 1 enforce — preamble wording** (`preamble.go`) + backend test. Tiny.
3. **Feature 1 improve — "Edit before running"** on `PromptCard`, reusing the phase-1 helper.
4. **Cleanup (deferred)** — inline-parser decomposition + recovery-machinery retirement, per
   the existing `docs/structured-prompt-suggestions.md` phase-out.

## Testing & rollout

- `just check` (biome + tsc) and `cd backend && go test ./... -count=1 -short` must pass.
- **No wire/schema types change** (no new tool, no new RPC — reuses the existing new-session
  route/draft; the preamble is a plain string), so `just typegen` is **not** required.
- Vitest: `openPrefilledNewSession` (append-vs-set draft + navigate target, incl. cross-project);
  a `PreBlock`/Markdown test asserting the Run control appears on a code block, is absent on
  mermaid, and navigates to `/project/$slug/session/new` for the chosen project; a `PromptCard`
  test for the "Edit before running" item.
- Go: `preamble_test.go` asserts the hand-off wording (and that the cross-project line mentions
  writing a prompt for another repo).
- Manual verify in an isolated worktree (per project practice): emit a plain fence → Run → lands
  in the target project's new-session composer prefilled, unsent; send creates the session.

## Risks / deferred / defaulted choices

- **Draft precedence.** We append to any existing `new:<projectId>` draft rather than replace,
  to avoid clobbering a user's in-progress new-session prompt. (Defaulted; trivially tunable.)
- **Session naming.** New-session creation stays name-empty → auto-named on send (today's
  `NewChatPanel` behavior). Code blocks have no title, so nothing is lost.
- **Non-session contexts.** Code blocks in project-less views (discussions/personas) either show
  a bare project list or hide the control; they never resolve a "current project."
- **Enforcement is probabilistic by design.** The preamble nudge can't guarantee the model calls
  the tool; the Feature 2 floor is what makes the affordance guaranteed.

## File-change map (implementation)

| Area | File | Change |
| ---- | ---- | ------ |
| Prefill helper | `frontend/src/lib/session/new-session-draft.ts` (new) | `openPrefilledNewSession` + re-export `newSessionDraftKey` |
| Code-block Run | `frontend/src/components/chat/Markdown.tsx` (`PreBlock`) | add Run dropdown beside `CopyButton`; skip mermaid/empty |
| New-session key | `frontend/src/components/chat/NewChatPanel.tsx` | export `newSessionDraftKey` (move to helper) for reuse |
| Card edit path | `frontend/src/components/chat/PromptCard.tsx` | add "Edit before running" item → `openPrefilledNewSession` |
| Preamble | `backend/internal/session/preamble.go:19-31` | broaden `presetSuggestParallel` + `crossProjectInstructions` for hand-offs |
| Tests | `preamble_test.go`, frontend vitest | as above |
