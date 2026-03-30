package gitops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeBranch(t *testing.T) {
	repoDir := initGitRepo(t)

	// Create a feature branch with a commit.
	testGitRun(t, repoDir, "checkout", "-b", "feature")
	writeFile(t, repoDir, "feature.txt", "feature content")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "feature commit")

	// Switch back to main and merge.
	testGitRun(t, repoDir, "checkout", "main")

	hash, err := MergeBranch(repoDir, "feature")
	if err != nil {
		t.Fatalf("MergeBranch failed: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty commit hash")
	}

	// Verify the merge commit exists.
	out := testGitOutput(t, repoDir, "log", "--oneline", "-1")
	if out == "" {
		t.Fatal("expected merge commit in log")
	}

	// Verify the feature file exists on main.
	if _, err := os.Stat(filepath.Join(repoDir, "feature.txt")); err != nil {
		t.Fatalf("expected feature.txt to exist after merge: %v", err)
	}
}

func TestMergeConflictFiles(t *testing.T) {
	repoDir := initGitRepo(t)

	// Create conflicting changes on two branches.
	testGitRun(t, repoDir, "checkout", "-b", "conflict-branch")
	writeFile(t, repoDir, "README", "conflict branch content")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "conflict branch change")

	testGitRun(t, repoDir, "checkout", "main")
	writeFile(t, repoDir, "README", "main branch content")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "main branch change")

	// Attempt a regular merge (not ff-only) to produce conflict markers.
	_, err := gitRun(repoDir, "merge", "conflict-branch")
	if err == nil {
		t.Fatal("expected merge to fail with conflict")
	}

	files, err := MergeConflictFiles(repoDir)
	if err != nil {
		t.Fatalf("MergeConflictFiles failed: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one conflict file")
	}

	found := false
	for _, f := range files {
		if f == "README" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected README in conflict files, got %v", files)
	}

	// Clean up.
	if err := AbortMerge(repoDir); err != nil {
		t.Fatalf("AbortMerge failed: %v", err)
	}
}

func TestDeleteBranch(t *testing.T) {
	repoDir := initGitRepo(t)

	// Create a branch, merge it, then delete.
	testGitRun(t, repoDir, "checkout", "-b", "to-delete")
	writeFile(t, repoDir, "delete.txt", "content")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "branch commit")
	testGitRun(t, repoDir, "checkout", "main")
	testGitRun(t, repoDir, "merge", "--no-ff", "to-delete")

	if err := DeleteBranch(repoDir, "to-delete"); err != nil {
		t.Fatalf("DeleteBranch failed: %v", err)
	}

	// Verify branch no longer exists.
	out := testGitOutput(t, repoDir, "branch", "--list", "to-delete")
	if out != "" {
		t.Fatalf("expected branch to be deleted, but got: %s", out)
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	repoDir := initGitRepo(t)

	dirty, err := HasUncommittedChanges(repoDir)
	if err != nil {
		t.Fatalf("HasUncommittedChanges failed: %v", err)
	}
	if dirty {
		t.Fatal("expected clean repo")
	}

	// Create an uncommitted file.
	writeFile(t, repoDir, "uncommitted.txt", "hello")

	dirty, err = HasUncommittedChanges(repoDir)
	if err != nil {
		t.Fatalf("HasUncommittedChanges failed: %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty repo")
	}
}

func TestCurrentBranch(t *testing.T) {
	repoDir := initGitRepo(t)

	branch, err := CurrentBranch(repoDir)
	if err != nil {
		t.Fatalf("CurrentBranch failed: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected branch 'main', got %q", branch)
	}

	testGitRun(t, repoDir, "checkout", "-b", "test-branch")
	branch, err = CurrentBranch(repoDir)
	if err != nil {
		t.Fatalf("CurrentBranch failed: %v", err)
	}
	if branch != "test-branch" {
		t.Fatalf("expected branch 'test-branch', got %q", branch)
	}
}

func TestHasRemote(t *testing.T) {
	repoDir := initGitRepo(t)

	has, err := HasRemote(repoDir, "origin")
	if err != nil {
		t.Fatalf("HasRemote failed: %v", err)
	}
	if has {
		t.Fatal("expected no origin remote")
	}
}

func TestHasGhCli(t *testing.T) {
	// Just verify it doesn't panic. Result depends on environment.
	_ = HasGhCli()
}

func TestMergeTreeCheck_Clean(t *testing.T) {
	repoDir := initGitRepo(t)

	testGitRun(t, repoDir, "checkout", "-b", "feature")
	writeFile(t, repoDir, "feature.txt", "new file")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "add feature file")
	testGitRun(t, repoDir, "checkout", "main")

	result, err := MergeTreeCheck(repoDir, "feature")
	if err != nil {
		t.Fatalf("MergeTreeCheck failed: %v", err)
	}
	if !result.Clean {
		t.Fatalf("expected clean merge, got conflicts: %v", result.ConflictFiles)
	}
}

func TestMergeTreeCheck_Conflict(t *testing.T) {
	repoDir := initGitRepo(t)

	testGitRun(t, repoDir, "checkout", "-b", "branch-a")
	writeFile(t, repoDir, "README", "branch-a content")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "change on branch-a")

	testGitRun(t, repoDir, "checkout", "main")
	writeFile(t, repoDir, "README", "main content")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "change on main")

	result, err := MergeTreeCheck(repoDir, "branch-a")
	if err != nil {
		t.Fatalf("MergeTreeCheck failed: %v", err)
	}
	if result.Clean {
		t.Fatal("expected conflicts, got clean")
	}
	if len(result.ConflictFiles) == 0 {
		t.Fatal("expected conflict files list")
	}
	found := false
	for _, f := range result.ConflictFiles {
		if f == "README" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected README in conflict files, got %v", result.ConflictFiles)
	}
}

func TestStashPush_CleanRepo(t *testing.T) {
	repoDir := initGitRepo(t)

	stashed, err := StashPush(repoDir)
	if err != nil {
		t.Fatalf("StashPush failed: %v", err)
	}
	if stashed {
		t.Fatal("expected nothing to stash on clean repo")
	}
}

func TestStashPush_DirtyRepo(t *testing.T) {
	repoDir := initGitRepo(t)
	writeFile(t, repoDir, "dirty.txt", "uncommitted")

	stashed, err := StashPush(repoDir)
	if err != nil {
		t.Fatalf("StashPush failed: %v", err)
	}
	if !stashed {
		t.Fatal("expected changes to be stashed")
	}

	// Tree should be clean now.
	dirty, _ := HasUncommittedChanges(repoDir)
	if dirty {
		t.Fatal("expected clean tree after stash push")
	}

	// Pop to restore.
	if err := StashPop(repoDir); err != nil {
		t.Fatalf("StashPop failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dirty.txt")); err != nil {
		t.Fatal("expected dirty.txt restored after stash pop")
	}
}

func TestStashPush_IncludesUntracked(t *testing.T) {
	repoDir := initGitRepo(t)
	writeFile(t, repoDir, "untracked.txt", "new file")

	stashed, err := StashPush(repoDir)
	if err != nil {
		t.Fatalf("StashPush failed: %v", err)
	}
	if !stashed {
		t.Fatal("expected untracked file to be stashed")
	}

	// File should be gone.
	if _, err := os.Stat(filepath.Join(repoDir, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatal("expected untracked.txt to be stashed away")
	}

	// Pop restores it.
	if err := StashPop(repoDir); err != nil {
		t.Fatalf("StashPop failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "untracked.txt")); err != nil {
		t.Fatal("expected untracked.txt restored after pop")
	}
}

func TestWithCleanWorktree_CleanRepo(t *testing.T) {
	repoDir := initGitRepo(t)

	called := false
	stashConflict, err := WithCleanWorktree(repoDir, func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithCleanWorktree failed: %v", err)
	}
	if stashConflict {
		t.Fatal("unexpected stash conflict")
	}
	if !called {
		t.Fatal("expected fn to be called")
	}
}

func TestWithCleanWorktree_DirtyRepo(t *testing.T) {
	repoDir := initGitRepo(t)
	writeFile(t, repoDir, "wip.txt", "work in progress")

	var wasDirty bool
	stashConflict, err := WithCleanWorktree(repoDir, func() error {
		wasDirty, _ = HasUncommittedChanges(repoDir)
		return nil
	})
	if err != nil {
		t.Fatalf("WithCleanWorktree failed: %v", err)
	}
	if stashConflict {
		t.Fatal("unexpected stash conflict")
	}
	if wasDirty {
		t.Fatal("expected clean tree inside fn")
	}

	// Changes should be restored.
	data, err := os.ReadFile(filepath.Join(repoDir, "wip.txt"))
	if err != nil {
		t.Fatal("expected wip.txt restored after WithCleanWorktree")
	}
	if string(data) != "work in progress" {
		t.Fatalf("expected original content, got %q", string(data))
	}
}

func TestWithCleanWorktree_FnError(t *testing.T) {
	repoDir := initGitRepo(t)
	writeFile(t, repoDir, "wip.txt", "work in progress")

	fnErr := errors.New("operation failed")
	stashConflict, err := WithCleanWorktree(repoDir, func() error {
		return fnErr
	})
	if !errors.Is(err, fnErr) {
		t.Fatalf("expected fn error propagated, got %v", err)
	}
	if stashConflict {
		t.Fatal("unexpected stash conflict")
	}

	// Changes should still be restored despite fn error.
	if _, err := os.Stat(filepath.Join(repoDir, "wip.txt")); err != nil {
		t.Fatal("expected wip.txt restored even when fn fails")
	}
}

func TestWithCleanWorktree_MergeThenPop(t *testing.T) {
	repoDir := initGitRepo(t)

	// Create a feature branch with a commit.
	testGitRun(t, repoDir, "checkout", "-b", "feature")
	writeFile(t, repoDir, "feature.txt", "feature content")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "feature commit")
	testGitRun(t, repoDir, "checkout", "main")

	// Dirty the project root with an unrelated file.
	writeFile(t, repoDir, "wip.txt", "local work")

	var hash string
	stashConflict, err := WithCleanWorktree(repoDir, func() error {
		var mergeErr error
		hash, mergeErr = MergeBranch(repoDir, "feature")
		return mergeErr
	})
	if err != nil {
		t.Fatalf("merge inside WithCleanWorktree failed: %v", err)
	}
	if stashConflict {
		t.Fatal("unexpected stash conflict")
	}
	if hash == "" {
		t.Fatal("expected merge commit hash")
	}

	// Feature file should exist (merged).
	if _, err := os.Stat(filepath.Join(repoDir, "feature.txt")); err != nil {
		t.Fatal("expected feature.txt after merge")
	}

	// WIP file should be restored (from stash).
	data, err := os.ReadFile(filepath.Join(repoDir, "wip.txt"))
	if err != nil {
		t.Fatal("expected wip.txt restored after merge")
	}
	if string(data) != "local work" {
		t.Fatalf("expected wip content preserved, got %q", string(data))
	}
}

func TestWithCleanWorktree_StashPopConflict(t *testing.T) {
	repoDir := initGitRepo(t)

	// Create a feature branch that modifies README.
	testGitRun(t, repoDir, "checkout", "-b", "feature")
	writeFile(t, repoDir, "README", "feature version")
	testGitRun(t, repoDir, "add", ".")
	testGitRun(t, repoDir, "commit", "-m", "feature changes README")
	testGitRun(t, repoDir, "checkout", "main")

	// Dirty the project root with conflicting changes to the same file.
	writeFile(t, repoDir, "README", "local uncommitted version")

	stashConflict, err := WithCleanWorktree(repoDir, func() error {
		_, mergeErr := MergeBranch(repoDir, "feature")
		return mergeErr
	})
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if !stashConflict {
		t.Fatal("expected stash conflict when merged file overlaps with stashed changes")
	}

	// Stash should still exist for manual recovery.
	out := testGitOutput(t, repoDir, "stash", "list")
	if !strings.Contains(out, "agentique: auto-stash") {
		t.Fatal("expected stash entry preserved for recovery")
	}
}

func TestPorcelainStatus(t *testing.T) {
	tests := []struct {
		xy   string
		want string
	}{
		{"??", "untracked"},
		{"A ", "added"},
		{" A", "added"},
		{"D ", "deleted"},
		{" D", "deleted"},
		{"R ", "renamed"},
		{"M ", "modified"},
		{" M", "modified"},
		{"MM", "modified"},
	}

	for _, tt := range tests {
		if got := porcelainStatus(tt.xy); got != tt.want {
			t.Errorf("porcelainStatus(%q) = %q, want %q", tt.xy, got, tt.want)
		}
	}
}

func TestUncommittedFiles_Clean(t *testing.T) {
	repoDir := initGitRepo(t)
	files, err := UncommittedFiles(repoDir)
	if err != nil {
		t.Fatalf("UncommittedFiles failed: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected empty, got %v", files)
	}
}

func TestUncommittedFiles_Untracked(t *testing.T) {
	repoDir := initGitRepo(t)
	writeFile(t, repoDir, "new.txt", "content")

	files, err := UncommittedFiles(repoDir)
	if err != nil {
		t.Fatalf("UncommittedFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "new.txt" || files[0].Status != "untracked" {
		t.Errorf("got %+v, want {Path:new.txt Status:untracked}", files[0])
	}
}

func TestUncommittedFiles_Modified(t *testing.T) {
	repoDir := initGitRepo(t)
	writeFile(t, repoDir, "README", "modified content")

	files, err := UncommittedFiles(repoDir)
	if err != nil {
		t.Fatalf("UncommittedFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Status != "modified" {
		t.Errorf("expected modified, got %q", files[0].Status)
	}
}

func TestUncommittedDiff_Clean(t *testing.T) {
	repoDir := initGitRepo(t)
	diff, summary, err := UncommittedDiff(repoDir)
	if err != nil {
		t.Fatalf("UncommittedDiff failed: %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff, got %q", diff)
	}
	if summary != "" {
		t.Errorf("expected empty summary, got %q", summary)
	}
}

func TestUncommittedDiff_Modified(t *testing.T) {
	repoDir := initGitRepo(t)
	writeFile(t, repoDir, "README", "modified content")

	diff, summary, err := UncommittedDiff(repoDir)
	if err != nil {
		t.Fatalf("UncommittedDiff failed: %v", err)
	}
	if diff == "" {
		t.Error("expected non-empty diff")
	}
	if summary == "" {
		t.Error("expected non-empty summary")
	}
	if !strings.Contains(diff, "README") {
		t.Errorf("expected diff to mention README, got %q", diff)
	}
}
