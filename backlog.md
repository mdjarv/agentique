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

### ~~[B/M] Merge cleanup unreliable — stale branches, loose objects~~ DONE
Fixed — remote branch deletion, standalone clean action, improved gc.

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

### ~~[F/S] Copy button: sticky in scroll + add to user messages~~ DONE
Fixed in `be9ba6f` — sticky copy button + added to user messages.

### ~~[F/S] Chat overlay buttons: move and rethink icon~~ DONE
Fixed in `53b2279` — repositioned to bottom-right.

### ~~[F/S] Smooth scroll: immediate jump on session select, smooth only on button press~~ DONE
Fixed in `2e8db64` — instant scroll on load, smooth on follow.

### ~~[F/M] Draft UI should match session layout~~ DONE
Fixed in `29ac2a1` — extracted DraftHeader, shared layout shell with live sessions.

### ~~[I/S] "Refresh git" button — manual git status update~~ DONE
Fixed in `4566cea` — refresh button added to session header.

---

## P3 — Features

### ~~[F/M] Multi-session delete~~ DONE
Added `session.delete-bulk` WS handler (reuses `DeleteSession` per-session, returns per-session results for partial failures). Frontend: checkbox multi-select with shift-click range, bulk action bar, confirmation dialog showing worktree count.
