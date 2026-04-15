package session

import (
	"log/slog"
	"sync"
)

// gitOpGuard manages TryLockForGitOp/UnlockGitOp lifecycle for a git operation.
// Guarantees exactly-once unlock: Release() sets the target state on the happy
// path; Ensure() (called via defer) is a no-op if Release() already fired.
type gitOpGuard struct {
	session  *Session
	fallback State
	once     sync.Once
}

// tryLockForGitOp acquires the git op lock on a live session. Returns a guard
// that must be deferred with Ensure(). If session is nil (not live), returns a
// no-op guard.
func tryLockForGitOp(session *Session, operation string, fallback State) (*gitOpGuard, error) {
	if session == nil {
		return &gitOpGuard{}, nil
	}
	if err := session.TryLockForGitOp(operation); err != nil {
		return nil, err
	}
	return &gitOpGuard{session: session, fallback: fallback}, nil
}

// Release explicitly unlocks the git op with the given target state.
// Idempotent — second and subsequent calls are no-ops.
func (g *gitOpGuard) Release(state State) {
	if g.session == nil {
		return
	}
	g.once.Do(func() {
		if err := g.session.UnlockGitOp(state); err != nil {
			slog.Error("git guard: failed to unlock git op", "session_id", g.session.ID, "target_state", state, "error", err)
		}
	})
}

// Ensure is the deferred safety net. If Release() was never called (early
// return, error path), it unlocks with the fallback state. No-op if Release()
// already fired.
func (g *gitOpGuard) Ensure() {
	if g.session == nil {
		return
	}
	g.once.Do(func() {
		if err := g.session.UnlockGitOp(g.fallback); err != nil {
			slog.Error("git guard: failed to unlock git op", "session_id", g.session.ID, "target_state", g.fallback, "error", err)
		}
	})
}
