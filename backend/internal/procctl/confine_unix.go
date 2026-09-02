//go:build !windows

package procctl

// ConfineProcessTree is a no-op on unix. The equivalent guarantees come from
// the POSIX process-group model: each session CLI is spawned into its own
// group, the shutdown backstop kills surviving groups (KillCLIChildrenOf), and
// the startup reaper claims anything an ungraceful exit left behind
// (ReapOrphanedCLIProcesses). On Windows those two are no-ops and this is the
// mechanism instead — see confine_windows.go.
func ConfineProcessTree() error { return nil }
