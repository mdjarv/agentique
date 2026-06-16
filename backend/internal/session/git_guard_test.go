package session

import "testing"

// TestGitOpLockExcludesResume verifies the per-session exclusive-operation lock:
// while a git op holds it, a lazy resume (which TryLocks the same mutex) and a
// second git op are both refused, and the lock frees on guard release. This is
// what prevents a resumed turn from writing a worktree while a git op mutates
// it on an offline session (which has no in-memory StateMerging).
func TestGitOpLockExcludesResume(t *testing.T) {
	mgr := &Manager{}
	const sid = "sess-1"

	// Offline git op (session == nil): claims the per-session lock.
	guard, err := tryLockForGitOp(mgr, sid, nil, "merging", StateIdle)
	if err != nil {
		t.Fatalf("first git-op lock failed: %v", err)
	}

	// resumeSession TryLocks this same mutex — must fail while the op holds it.
	if mgr.gitOpLock(sid).TryLock() {
		t.Fatal("gitOpLock acquirable while a git op holds it — resume not excluded")
	}

	// A second concurrent git op on the same session is refused.
	if _, err := tryLockForGitOp(mgr, sid, nil, "rebasing", StateIdle); err == nil {
		t.Fatal("second concurrent git op was not refused")
	}

	// A different session is unaffected.
	other := mgr.gitOpLock("other")
	if !other.TryLock() {
		t.Fatal("unrelated session's lock should be free")
	}
	other.Unlock()

	// Releasing frees the lock for a subsequent resume / git op.
	guard.Ensure()
	if !mgr.gitOpLock(sid).TryLock() {
		t.Fatal("gitOpLock not released after guard.Ensure()")
	}
	mgr.gitOpLock(sid).Unlock()

	// Ensure() is idempotent (defer safety net after an explicit Release).
	guard.Ensure()
}
