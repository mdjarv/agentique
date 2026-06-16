package session

import (
	"fmt"
	"log/slog"
	"sync"
)

// gitOpGuard manages the lifecycle of a git operation's locks. It holds the
// manager's per-session exclusive-operation lock (opMu) — which also gates lazy
// resume so an offline session can't be resumed into a turn mid-operation — and,
// for a live session, the StateMerging lock. Guarantees exactly-once release:
// Release() sets the target state on the happy path; Ensure() (called via defer)
// is a no-op if Release() already fired.
type gitOpGuard struct {
	session  *Session
	fallback State
	opMu     *sync.Mutex // per-session exclusive-op lock; always held while the guard is live
	once     sync.Once
}

// tryLockForGitOp claims the session's exclusive-operation lock so a concurrent
// lazy resume / turn can't touch the worktree, then — for a live session — the
// StateMerging lock. Returns an error if either is already busy. The returned
// guard MUST be deferred with Ensure(); it releases both locks exactly once.
// Works uniformly whether or not the session is currently live: an offline
// session has no in-memory StateMerging, so the opMu (also taken by resume) is
// what protects its worktree.
func tryLockForGitOp(mgr *Manager, sessionID string, session *Session, operation string, fallback State) (*gitOpGuard, error) {
	opMu := mgr.gitOpLock(sessionID)
	if !opMu.TryLock() {
		return nil, fmt.Errorf("another operation is in progress for this session")
	}
	if session != nil {
		if err := session.TryLockForGitOp(operation); err != nil {
			opMu.Unlock()
			return nil, err
		}
	}
	return &gitOpGuard{session: session, fallback: fallback, opMu: opMu}, nil
}

// Release explicitly unlocks the git op with the given target state.
// Idempotent — second and subsequent calls are no-ops.
func (g *gitOpGuard) Release(state State) {
	g.release(state)
}

// Ensure is the deferred safety net. If Release() was never called (early
// return, error path), it unlocks with the fallback state. No-op if Release()
// already fired.
func (g *gitOpGuard) Ensure() {
	g.release(g.fallback)
}

func (g *gitOpGuard) release(state State) {
	g.once.Do(func() {
		if g.session != nil {
			if err := g.session.UnlockGitOp(state); err != nil {
				slog.Error("git guard: failed to unlock git op", "session_id", g.session.ID, "target_state", state, "error", err)
			}
		}
		if g.opMu != nil {
			g.opMu.Unlock()
		}
	})
}
