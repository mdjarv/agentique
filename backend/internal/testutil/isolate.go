package testutil

import (
	"fmt"
	"os"
	"testing"
)

// MainWithIsolatedDataDir runs a package's test binary with AGENTIQUE_HOME
// pointed at a throwaway temp dir for the *entire* process, then removes it.
// Call it from TestMain in any package whose tests exercise the real worktree
// path (e.g. CreateSession/CreateSwarm via RealWorktreeOps):
//
//	func TestMain(m *testing.M) { os.Exit(testutil.MainWithIsolatedDataDir(m)) }
//
// Why TestMain and not per-test SetupTest: worktree creation can happen in a
// session goroutine that outlives the test that started it. A per-test
// t.Setenv would already have reverted AGENTIQUE_HOME by the time that
// goroutine resolves paths.WorktreeDir(), so it would fall back to the user's
// live data dir and leak real worktrees there (e.g.
// ~/.local/share/agentique/worktrees/test-project/session-*). Setting the env
// once for the whole process closes that race regardless of goroutine timing.
func MainWithIsolatedDataDir(m *testing.M) int {
	dir, err := os.MkdirTemp("", "agentique-test-home-*")
	if err != nil {
		panic(fmt.Sprintf("testutil: create temp data dir: %v", err))
	}
	defer os.RemoveAll(dir)
	if err := os.Setenv("AGENTIQUE_HOME", dir); err != nil {
		panic(fmt.Sprintf("testutil: set AGENTIQUE_HOME: %v", err))
	}
	return m.Run()
}
