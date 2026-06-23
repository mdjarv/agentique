package session

import (
	"os"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// TestMain isolates AGENTIQUE_HOME to a temp dir for the whole package run so
// tests that provision real worktrees (spawn/swarm via RealWorktreeOps) never
// create them under the user's live data dir. See testutil.MainWithIsolatedDataDir.
func TestMain(m *testing.M) {
	os.Exit(testutil.MainWithIsolatedDataDir(m))
}
