package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMergeBranch(t *testing.T) {
	repoDir := initGitRepo(t)

	// Create a feature branch with a commit.
	gitRun(t, repoDir, "checkout", "-b", "feature")
	writeFile(t, repoDir, "feature.txt", "feature content")
	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "feature commit")

	// Switch back to main and merge.
	gitRun(t, repoDir, "checkout", "main")

	hash, err := MergeBranch(repoDir, "feature")
	if err != nil {
		t.Fatalf("MergeBranch failed: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty commit hash")
	}

	// Verify the merge commit exists.
	out := gitOutput(t, repoDir, "log", "--oneline", "-1")
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
	gitRun(t, repoDir, "checkout", "-b", "conflict-branch")
	writeFile(t, repoDir, "README", "conflict branch content")
	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "conflict branch change")

	gitRun(t, repoDir, "checkout", "main")
	writeFile(t, repoDir, "README", "main branch content")
	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "main branch change")

	// Attempt merge (should fail).
	_, err := MergeBranch(repoDir, "conflict-branch")
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
	gitRun(t, repoDir, "checkout", "-b", "to-delete")
	writeFile(t, repoDir, "delete.txt", "content")
	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "branch commit")
	gitRun(t, repoDir, "checkout", "main")
	gitRun(t, repoDir, "merge", "--no-ff", "to-delete")

	if err := DeleteBranch(repoDir, "to-delete"); err != nil {
		t.Fatalf("DeleteBranch failed: %v", err)
	}

	// Verify branch no longer exists.
	out := gitOutput(t, repoDir, "branch", "--list", "to-delete")
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

	gitRun(t, repoDir, "checkout", "-b", "test-branch")
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

// --- helpers ---

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(out))
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(out))
	}
	return string(out)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
