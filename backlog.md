# Backlog

Legend: **B**=Bug **F**=Feature **I**=Investigation | **P1**=soon **P2**=normal **P3**=low | **S**=small **M**=medium **L**=large

---

## P1 — Bugs

### ~~[B/S] Commit on local repo doesn't mark session complete~~ DONE
Fixed — `Commit()` now transitions non-worktree sessions to `StateDone` and broadcasts state change.

### ~~[B/S] Prompt-block session titles overwritten by Haiku auto-naming~~
Sessions created from ` ```prompt ` blocks had their extracted title overwritten by Haiku on first query. Also, empty session names got a placeholder "Session N" instead of letting Haiku name them properly.
**Fixed:** `autoName` now checks DB for existing name and skips if non-empty. `CreateSession` no longer generates "Session N" fallback — empty names pass through and get auto-titled. Frontend shows italic "Untitled" placeholder for empty names.

### ~~[B/M] Rebase conflict warning visible on all sessions~~ DONE
Conflict state properly scoped to component lifecycle; resets on session change.

### ~~[B/M] Plan mode — agent makes changes while Plan is still active~~ DONE
Fixed in `2d0f393` — auto-approval bypass disabled when `permissionMode == "plan"`.

### ~~[B/S] ExitPlanMode approval banner says "enter plan mode"~~ DONE
Fixed in `c5896cc` — distinct labels for enter/exit, EnterPlanMode auto-approved.

### [B/M] Merge cleanup unreliable — stale branches, loose objects
Merge with cleanup doesn't delete remote tracking branches. `git gc --auto` is too conservative and may not reclaim loose objects. No standalone "clean" option for sessions merged manually or abandoned.
**Fix:** Add remote branch deletion to merge cleanup. Consider `git gc` (not `--auto`) or `git prune`. Add a separate "clean" action for sessions without merge.

### [I/M] Worktree removed externally while session has active agent
If a user removes a worktree directory from a separate shell while the session's agent is still running, the session enters an undefined state. `resumeSession` handles missing worktrees on restart (fallback to project root), but mid-session removal while the CLI is active needs investigation.
**Investigate:** What happens to the running CLI? Does it crash, error, or silently break? What should the session's state transition be? Should we detect this proactively (fs watch, poll)?

---

## P1 — Maintenance

### ~~[I/S] claudecli-go module update available~~ DONE
Already on latest (`v0.0.0-20260324082320-92fb882c72a6`, 2026-03-24).

---

## P2 — Features & UX

### ~~[F/S] Merge should navigate to nearest active session~~ DONE
Fixed — `session.deleted` handler now navigates to the nearest idle/running sibling, or falls back to project index.

### ~~[F/S] Manually mark session as done~~ DONE
Fixed in `6143c3a` — mark-done handler + UI option added.

### ~~[F/S] Rebase conflict: button to have Claude resolve it~~ DONE
Fixed in `3904d5e` — resolve button sends conflict message to Claude.

### [F/S] Copy button: sticky in scroll + add to user messages
Copy button on assistant messages disappears when the top of the message scrolls out of view (absolute positioned at `top-2 right-2`). Should remain visible while any part of the message is on screen. Also add copy to user/sent messages.

### [F/S] Chat overlay buttons: move and rethink icon
"Scroll to bottom" and "toggle tool calls" buttons overlap chat content.
The eye icon for toggling tool calls is too generic.
**Fix:** Move buttons to a less obtrusive position (e.g., pinned to chat panel edge, not floating over messages). Pick a more specific icon for tool call toggle (e.g., wrench, terminal, or similar).

### ~~[F/S] Smooth scroll: immediate jump on session select, smooth only on button press~~ DONE
Fixed in `2e8db64` — instant scroll on load, smooth on follow.

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
