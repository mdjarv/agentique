package storage

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// The verdict rules are unit-tested against a fake probe. This exercises the
// other half: the real probe, wired through ComputeUsage, against a real
// repository with real worktrees. It is the half that was wrong in production —
// not the rules, but which question got asked of which directory.

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func commit(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "add "+name)
}

// repo builds a project checkout with one commit on the default branch.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	commit(t, dir, "README.md", "base\n")
	return dir
}

// worktreeFor creates a branch, checks it out as a worktree under the data
// dir's worktree tree (where ComputeUsage looks), and returns its path.
func worktreeFor(t *testing.T, repoDir, projectName, branch string) string {
	t.Helper()
	wt := filepath.Join(paths.WorktreeDir(), projectName, branch)
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		t.Fatalf("mkdir worktree parent: %v", err)
	}
	git(t, repoDir, "worktree", "add", "-b", branch, wt)
	return wt
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// attach points a seeded session row at a worktree and branch.
func attach(t *testing.T, q *store.Queries, id, wt, branch string) {
	t.Helper()
	err := q.UpdateSessionWorktree(context.Background(), store.UpdateSessionWorktreeParams{
		WorkDir:        wt,
		WorktreePath:   nullStr(wt),
		WorktreeBranch: nullStr(branch),
		ID:             id,
	})
	if err != nil {
		t.Fatalf("attach worktree: %v", err)
	}
}

func findSession(t *testing.T, u *StorageUsage, id string) SessionStorage {
	t.Helper()
	for _, p := range u.Projects {
		for _, s := range p.Sessions {
			if s.SessionID == id {
				return s
			}
		}
	}
	t.Fatalf("session %s not in usage breakdown", id)
	return SessionStorage{}
}

func TestComputeUsageVerdictsAgainstARealRepo(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	_, q := testutil.SetupDB(t)
	repoDir := repo(t)
	project := testutil.SeedProject(t, q, "proj", repoDir)

	// 1. Merged into main and clean — the case the merged flag gets wrong.
	//    Its branch is merged from the repo side, exactly as a terminal would,
	//    so worktree_merged stays 0 throughout.
	mergedSess := testutil.SeedSession(t, q, project.ID, "stopped")
	mergedWT := worktreeFor(t, repoDir, "proj", "session-merged")
	commit(t, mergedWT, "feature.txt", "done\n")
	git(t, repoDir, "merge", "--no-ff", "-m", "merge feature", "session-merged")
	attach(t, q, mergedSess.ID, mergedWT, "session-merged")

	// 2. Real unmerged work.
	aheadSess := testutil.SeedSession(t, q, project.ID, "stopped")
	aheadWT := worktreeFor(t, repoDir, "proj", "session-ahead")
	commit(t, aheadWT, "wip.txt", "not merged\n")
	attach(t, q, aheadSess.ID, aheadWT, "session-ahead")

	// 3. Merged, but with an uncommitted edit still in the tree.
	dirtySess := testutil.SeedSession(t, q, project.ID, "stopped")
	dirtyWT := worktreeFor(t, repoDir, "proj", "session-dirty")
	if err := os.WriteFile(filepath.Join(dirtyWT, "scratch.txt"), []byte("unsaved\n"), 0o644); err != nil {
		t.Fatalf("write scratch: %v", err)
	}
	attach(t, q, dirtySess.ID, dirtyWT, "session-dirty")

	// 4. Merged and clean, but still running.
	runningSess := testutil.SeedSession(t, q, project.ID, "running")
	runningWT := worktreeFor(t, repoDir, "proj", "session-running")
	attach(t, q, runningSess.ID, runningWT, "session-running")

	usage, err := ComputeUsage(context.Background(), q, UsageOptions{Probe: RealSafetyProbe()})
	if err != nil {
		t.Fatalf("ComputeUsage: %v", err)
	}

	cases := []struct {
		name        string
		id          string
		wantSafety  DeleteSafety
		wantReclaim bool
	}{
		{"merged outside agentique is still safe", mergedSess.ID, DeleteSafe, true},
		{"unmerged commits block delete only", aheadSess.ID, DeleteBlockedAhead, true},
		{"an uncommitted file blocks both", dirtySess.ID, DeleteBlockedDirty, false},
		{"a running session is untouchable", runningSess.ID, DeleteBlockedLive, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findSession(t, usage, tc.id)
			if got.Merged {
				t.Fatalf("fixture broken: worktree_merged should be 0 for every session here")
			}
			if got.Safety != tc.wantSafety {
				t.Errorf("Safety = %q (%s), want %q", got.Safety, got.SafetyReason, tc.wantSafety)
			}
			if got.Reclaimable != tc.wantReclaim {
				t.Errorf("Reclaimable = %v, want %v", got.Reclaimable, tc.wantReclaim)
			}
		})
	}

	if usage.ReclaimableCount != 2 {
		t.Errorf("ReclaimableCount = %d, want 2", usage.ReclaimableCount)
	}
}

// An untracked file is not "no changes". Reclaiming would delete it, and it
// exists nowhere else — git status --porcelain lists it, and the verdict must
// act on that.
func TestComputeUsageTreatsAnUntrackedFileAsDirty(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	_, q := testutil.SetupDB(t)
	repoDir := repo(t)
	project := testutil.SeedProject(t, q, "proj", repoDir)

	sess := testutil.SeedSession(t, q, project.ID, "stopped")
	wt := worktreeFor(t, repoDir, "proj", "session-untracked")
	if err := os.WriteFile(filepath.Join(wt, "notes.md"), []byte("only copy\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	attach(t, q, sess.ID, wt, "session-untracked")

	usage, err := ComputeUsage(context.Background(), q, UsageOptions{Probe: RealSafetyProbe()})
	if err != nil {
		t.Fatalf("ComputeUsage: %v", err)
	}
	got := findSession(t, usage, sess.ID)
	if got.Safety != DeleteBlockedDirty || got.Reclaimable {
		t.Errorf("untracked file: Safety = %q, Reclaimable = %v; want dirty and not reclaimable",
			got.Safety, got.Reclaimable)
	}
}

// The counterpart, and the reason the dirty check cannot simply be "is the
// directory empty of extras": an ignored node_modules is why every worktree on
// a real machine looks clean, and must not block either verb.
func TestComputeUsageIgnoresGitignoredFiles(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	_, q := testutil.SetupDB(t)
	repoDir := repo(t)
	commit(t, repoDir, ".gitignore", "node_modules/\n")
	project := testutil.SeedProject(t, q, "proj", repoDir)

	sess := testutil.SeedSession(t, q, project.ID, "stopped")
	wt := worktreeFor(t, repoDir, "proj", "session-deps")
	if err := os.MkdirAll(filepath.Join(wt, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, "node_modules", "pkg", "index.js"), make([]byte, 4096), 0o644); err != nil {
		t.Fatalf("write dep: %v", err)
	}
	attach(t, q, sess.ID, wt, "session-deps")

	usage, err := ComputeUsage(context.Background(), q, UsageOptions{Probe: RealSafetyProbe()})
	if err != nil {
		t.Fatalf("ComputeUsage: %v", err)
	}
	got := findSession(t, usage, sess.ID)
	if !got.Reclaimable || got.Safety != DeleteSafe {
		t.Errorf("ignored deps: Safety = %q, Reclaimable = %v; want safe and reclaimable",
			got.Safety, got.Reclaimable)
	}
	if got.Bytes < 4096 {
		t.Errorf("Bytes = %d, want the ignored tree counted (>= 4096)", got.Bytes)
	}
}

// A live session must not be probed at all, and must never be offered either
// verb, whatever its persisted state says.
func TestComputeUsageSparesASessionTheRuntimeHolds(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	_, q := testutil.SetupDB(t)
	repoDir := repo(t)
	project := testutil.SeedProject(t, q, "proj", repoDir)

	sess := testutil.SeedSession(t, q, project.ID, "stopped") // persisted as finished
	wt := worktreeFor(t, repoDir, "proj", "session-held")
	attach(t, q, sess.ID, wt, "session-held")

	usage, err := ComputeUsage(context.Background(), q, UsageOptions{
		Probe:   RealSafetyProbe(),
		LiveIDs: map[string]bool{sess.ID: true},
	})
	if err != nil {
		t.Fatalf("ComputeUsage: %v", err)
	}
	got := findSession(t, usage, sess.ID)
	if got.Safety != DeleteBlockedLive || got.Reclaimable {
		t.Errorf("held session: Safety = %q, Reclaimable = %v; want live and not reclaimable",
			got.Safety, got.Reclaimable)
	}
}

// Without a probe the page cannot establish anything, and must offer nothing
// rather than defaulting to permission.
func TestComputeUsageWithoutAProbeOffersNothing(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	_, q := testutil.SetupDB(t)
	repoDir := repo(t)
	project := testutil.SeedProject(t, q, "proj", repoDir)

	sess := testutil.SeedSession(t, q, project.ID, "stopped")
	wt := worktreeFor(t, repoDir, "proj", "session-noprobe")
	attach(t, q, sess.ID, wt, "session-noprobe")

	usage, err := ComputeUsage(context.Background(), q, UsageOptions{})
	if err != nil {
		t.Fatalf("ComputeUsage: %v", err)
	}
	got := findSession(t, usage, sess.ID)
	if got.Safety != DeleteBlockedUnknown || got.Reclaimable {
		t.Errorf("no probe: Safety = %q, Reclaimable = %v; want unknown and not reclaimable",
			got.Safety, got.Reclaimable)
	}
}
