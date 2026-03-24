# Plan: Divergence indicator + rebase button

Show "N commits behind main" on worktree sessions, with a manual rebase button.

## What changes

### Backend — gitops layer (`backend/internal/gitops/git.go`)

1. **`CommitsBehind(dir, branch string) (int, error)`** — `git rev-list --count <branch>..HEAD` (inverse of existing `CommitsAhead`).
2. **`RebaseBranch(dir, onto string) error`** — runs `git rebase <onto>` in the given directory. On failure, checks for unmerged files (conflict), aborts with `git rebase --abort`, returns structured error.

### Backend — session layer (`backend/internal/session/git_service.go`)

3. **Add `commitsBehind` to `enrichGitStatus`** — call `CommitsBehind` alongside existing `CommitsAhead` call, include in broadcast payload.
4. **Add `commitsBehind` to `ListSessions`** response (`service.go`) — same logic as enrichGitStatus, populate `CommitsBehind` field on `SessionInfo`.
5. **`Rebase(ctx, sessionID) (RebaseResult, error)`** on `GitService`:
   - Get session + project from DB
   - Require worktree branch, not merged
   - Lock session via `TryLockForMerge` (reuses `StateMerging` — operation is sub-second, separate state not worth the churn)
   - Auto-commit dirty worktree changes
   - Get current main HEAD SHA from project dir
   - Run `git rebase <main-HEAD-SHA>` in the worktree dir
   - On conflict: abort rebase, return conflict file list
   - On success: update `worktree_base_sha` in DB to new main HEAD (so diffs stay correct), broadcast state update
   - Unlock session back to idle

### Backend — WS layer (`backend/internal/ws/`)

6. **New handler `handleSessionRebase`** + payload type + route registration. Calls `GitService.Rebase`.

### Backend — DB (`backend/db/queries/sessions.sql`)

7. **`UpdateWorktreeBaseSHA` query** — `UPDATE sessions SET worktree_base_sha = ? WHERE id = ?`. Run `just sqlc` after.

### Frontend — types & store

8. **Add `commitsBehind?: number`** to `SessionMetadata` in `chat-store.ts`, add to the git patch type in `setSessionState`.

### Frontend — session list (`SessionRow.tsx`)

9. **Show "N behind" indicator** — down arrow + count, warning color (e.g. `text-[#e0af68]/80`). Show alongside existing ahead/dirty indicators.

### Frontend — session header (`SessionHeader.tsx`)

10. **"Rebase" button** — shown when `isWorktree && !isBusy && commitsBehind > 0`. Same style as merge button. Shows spinner while in progress, toast on success/failure, renders `ConflictPanel` on conflict (same as merge).

### Frontend — actions (`session-actions.ts`)

11. **`rebaseSession(ws, sessionId)`** — sends `session.rebase` WS message, returns `RebaseResult`.

## Edge cases

- **Session is running**: rebase button hidden (same as merge). Agent might be mid-write; rebasing under it would corrupt state.
- **Rebase conflicts**: abort rebase, show conflict panel with file list. User can resolve manually or try again after the session makes different changes.
- **No commits behind**: button hidden, no indicator shown.
- **Already merged sessions**: `enrichGitStatus` already skips merged sessions — `commitsBehind` won't be computed.

## Files touched

| File | Change |
|------|--------|
| `backend/internal/gitops/git.go` | +`CommitsBehind`, +`RebaseBranch`, +`RebaseConflictFiles`, +`AbortRebase` |
| `backend/internal/session/git_service.go` | +`Rebase`, +`RebaseResult` |
| `backend/internal/session/session.go` | `enrichGitStatus` adds `commitsBehind` |
| `backend/internal/session/service.go` | `SessionInfo` adds `CommitsBehind`, `ListSessions` populates it |
| `backend/internal/ws/handlers.go` | +`handleSessionRebase` |
| `backend/internal/ws/types.go` | +`SessionRebasePayload` |
| `backend/internal/ws/conn.go` | route registration |
| `backend/db/queries/sessions.sql` | +`UpdateWorktreeBaseSHA` |
| `backend/db/sqlc/` | regenerated |
| `frontend/src/stores/chat-store.ts` | +`commitsBehind` field |
| `frontend/src/hooks/useChatSession.ts` | pass `commitsBehind` through |
| `frontend/src/components/layout/SessionRow.tsx` | behind indicator |
| `frontend/src/components/chat/SessionHeader.tsx` | rebase button |
| `frontend/src/lib/session-actions.ts` | +`rebaseSession` |
