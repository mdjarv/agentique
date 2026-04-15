package gitops

import (
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
