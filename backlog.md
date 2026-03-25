# Backlog

Legend: **B**=Bug **F**=Feature **I**=Investigation | **P1**=soon **P2**=normal **P3**=low | **S**=small **M**=medium **L**=large

---

## P1 — Bugs

### [B/S] Commit on local repo doesn't mark session complete
After clicking Commit on a local (non-worktree) session, the session stays in its current state.
`session/git_service.go` `Commit()` returns only a hash — no state transition. `SessionHeader.tsx` `handleCommit` toasts success but never updates state.
**Fix:** After commit succeeds, transition session to `StateDone` (or expose an explicit "mark done" step).

### [B/M] Rebase conflict warning visible on all sessions
Conflict panel appears on every session you navigate to after a rebase conflict, not just the affected one.
`SessionHeader.tsx` stores `conflictFiles` in local `useState` — should reset on session change. Component likely persists across navigations if it isn't unmounted.
**Fix:** Reset conflict state in `useEffect` on `sessionId` change, or move conflict state into the per-session store slice.

### [B/M] Plan mode — agent makes changes while Plan is still active
Session appears to accept edits and make file changes while Plan mode is still shown as active in the UI.
Likely cause: we auto-approve all permission checks (`autoApprove` bypass in `session.go:handleToolPermission`), so the CLI never sends a mode-switch event back to the frontend. Frontend shows "Plan" but the agent has already transitioned.
**Fix:** Needs investigation — either propagate permission mode state from CLI back to frontend, or disable auto-approve for plan/chat transitions.

---

## P1 — Maintenance

### [I/S] claudecli-go module update available
`backend/go.mod` pins `claudecli-go` at `v0.0.0-20260324082320-92fb882c72a6`.
Check what changed, whether it's safe to update, and whether it fixes any known issues.
**Action:** Review changelog/commits, update and run `go test ./... -short`.

---

## P2 — Features & UX

### [F/S] Merge should navigate to nearest active session
After merging (and the session is deleted), the user stays on the dead session page.
`useGlobalSubscriptions.ts` removes the session from the store on `session.deleted` but doesn't trigger navigation. `ChatPanel.tsx` only redirects when `sessionListLoaded && !session`.
**Fix:** On merge/delete, find the nearest `idle`/`running` session in the same project and navigate to it; fall back to "new chat" if none exists.

### [F/S] Manually mark session as done
No UI to mark a session done without deleting it. State machine already supports `StateDone` from `idle`/`running`/`failed`.
**Fix:** Add a "Mark done" option (e.g., in session header menu). New WS handler `session.mark-done` → `SetState(StateDone)`.

### [F/S] Rebase conflict: button to have Claude resolve it
When a rebase conflict is detected, show a button alongside the conflict panel that sends a pre-written message to Claude (e.g., "There are merge conflicts in the following files: X, Y, Z. Please resolve them.").
**Note:** This is a convenience wrapper — it just sends a chat message. Keep it simple.

### [F/S] Chat overlay buttons: move and rethink icon
"Scroll to bottom" and "toggle tool calls" buttons overlap chat content.
The eye icon for toggling tool calls is too generic.
**Fix:** Move buttons to a less obtrusive position (e.g., pinned to chat panel edge, not floating over messages). Pick a more specific icon for tool call toggle (e.g., wrench, terminal, or similar).

### [F/S] Smooth scroll: immediate jump on session select, smooth only on button press
Navigating to a session with long history does a slow smooth-scroll to the bottom.
**Fix:** On session select → instant jump. Only use smooth-scroll when the user presses the "jump to bottom" button.

### [F/M] Draft UI should match session layout
When a draft is promoted to a real session, the layout shift is jarring because draft uses a different header/structure.
**Fix:** Draft view should use the same `SessionHeader` and layout shell as a live session, just with disabled/hidden fields that don't apply yet.

### [I/S] "Refresh git" button — manual git status update
Users may want to force a refresh of git status (ahead/behind counts, dirty state) without waiting for the next poll cycle.
**Investigate:** Current polling interval and trigger points. Where would the button live (session header next to rebase/merge)? Does the backend need a new handler or can the frontend just re-request session state?

---

## P3 — Features

### [F/M] Multi-session delete
Currently only single-session delete exists (`DeleteSession` takes one ID, no bulk endpoint).
**Fix:** Add multi-select to session list (checkbox or shift-click), bulk delete endpoint in backend, confirmation dialog showing count + worktrees to be removed.
