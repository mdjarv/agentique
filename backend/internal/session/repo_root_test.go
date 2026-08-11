package session

import (
	"errors"
	"testing"

	"github.com/allbin/agentkit/runtime"
)

// A session whose provider is not claude (or that has no live CLI at all) has
// no runtime add-dir equivalent. Callers registering teammate worktrees
// opportunistically match on this to stay quiet.
func TestRegisterRepoRootUnsupportedProvider(t *testing.T) {
	sess := &Session{ID: "s1"}

	err := sess.RegisterRepoRoot("/tmp/teammate")
	if !errors.Is(err, ErrRepoRootUnsupported) {
		t.Fatalf("want ErrRepoRootUnsupported, got %v", err)
	}
	if sess.repoRoots != nil {
		t.Fatalf("a failed registration must not record the directory, got %v", sess.repoRoots)
	}
}

// The registration set exists because the CLI's register_repo_root is not
// idempotent — it rejects a directory it already holds. A directory already in
// the set short-circuits before any control request is attempted, which is what
// keeps a repeated channel-context refresh from erroring on every teammate.
func TestRegisterRepoRootSkipsAlreadyRegistered(t *testing.T) {
	sess := &Session{ID: "s1", repoRoots: map[string]struct{}{"/tmp/teammate": {}}}

	// No CLI is attached, so reaching the control request would surface as
	// ErrRepoRootUnsupported. Getting nil proves the short-circuit ran first.
	if err := sess.RegisterRepoRoot("/tmp/teammate"); err != nil {
		t.Fatalf("already-registered directory should be a no-op, got %v", err)
	}
	if err := sess.RegisterRepoRoot("/tmp/other"); !errors.Is(err, ErrRepoRootUnsupported) {
		t.Fatalf("an unregistered directory should still attempt registration, got %v", err)
	}
}

// A new CLI process holds none of the previous one's workspace roots, so the
// set must not survive a resume/reconnect or an idle-evict round trip —
// otherwise the roots would be suppressed forever after the first eviction.
func TestSetRuntimeClearsRepoRoots(t *testing.T) {
	sess := &Session{ID: "s1", repoRoots: map[string]struct{}{"/tmp/teammate": {}}}

	sess.setRuntime(nil, runtime.CLISession(nil))

	if sess.repoRoots != nil {
		t.Fatalf("repo roots must reset with the CLI process, got %v", sess.repoRoots)
	}
}
