package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCreateWorktree(t *testing.T) {
	repoDir := initGitRepo(t)
	wtPath := filepath.Join(t.TempDir(), "my-worktree")

	if err := CreateWorktree(repoDir, "test-branch", wtPath); err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Verify the worktree directory was created.
	info, err := os.Stat(wtPath)
	if err != nil {
		t.Fatalf("worktree directory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected worktree path to be a directory")
	}

	// Verify it contains a .git file (worktrees have a .git file, not a directory).
	gitPath := filepath.Join(wtPath, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		t.Fatalf("expected .git in worktree: %v", err)
	}
}

func TestRemoveWorktree(t *testing.T) {
	repoDir := initGitRepo(t)
	wtPath := filepath.Join(t.TempDir(), "to-remove")

	if err := CreateWorktree(repoDir, "remove-branch", wtPath); err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Verify it exists.
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree should exist before removal: %v", err)
	}

	// Remove it.
	RemoveWorktree(repoDir, wtPath)

	// Verify it was removed.
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree to be removed, but it still exists (err=%v)", err)
	}
}

func TestSanitizeBranch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"feature/branch", "feature-branch"},
		{"simple", "simple"},
		{"a/b/c", "a-b-c"},
	}
	for _, tt := range tests {
		got := SanitizeBranch(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeBranch(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWorktreePath(t *testing.T) {
	base := WorktreeBasePath()
	got := WorktreePath("myproject", "feature/test")
	want := filepath.Join(base, "myproject", "feature-test")
	if got != want {
		t.Errorf("WorktreePath = %q, want %q", got, want)
	}
}

func TestGetWorktreeBaseSHA(t *testing.T) {
	dir := initGitRepo(t)

	sha, err := GetWorktreeBaseSHA(dir)
	if err != nil {
		t.Fatalf("GetWorktreeBaseSHA: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("expected 40-char SHA, got %q (len=%d)", sha, len(sha))
	}
}

func TestWorktreeDiff_NoChanges(t *testing.T) {
	dir := initGitRepo(t)

	baseSHA, err := GetWorktreeBaseSHA(dir)
	if err != nil {
		t.Fatal(err)
	}

	result, err := WorktreeDiff(dir, baseSHA, false)
	if err != nil {
		t.Fatalf("WorktreeDiff: %v", err)
	}
	if result.HasDiff {
		t.Error("expected no diff for unchanged repo")
	}
}

func TestWorktreeDiff_WithChanges(t *testing.T) {
	repoDir := initGitRepo(t)

	baseSHA, err := GetWorktreeBaseSHA(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	wtPath := filepath.Join(t.TempDir(), "diff-wt")
	if err := CreateWorktree(repoDir, "diff-test", wtPath); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Make changes in the worktree.
	wtGitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = wtPath
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

	if err := os.WriteFile(filepath.Join(wtPath, "README"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("new file\n"), 0644); err != nil {
		t.Fatal(err)
	}
	wtGitRun("add", ".")
	wtGitRun("commit", "-m", "changes")

	result, err := WorktreeDiff(wtPath, baseSHA, false)
	if err != nil {
		t.Fatalf("WorktreeDiff: %v", err)
	}
	if !result.HasDiff {
		t.Fatal("expected diff")
	}
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(result.Files))
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if result.Diff == "" {
		t.Error("expected non-empty diff")
	}

	statusByPath := make(map[string]string)
	for _, f := range result.Files {
		statusByPath[f.Path] = f.Status
	}
	if statusByPath["README"] != "modified" {
		t.Errorf("README: expected modified, got %q", statusByPath["README"])
	}
	if statusByPath["new.txt"] != "added" {
		t.Errorf("new.txt: expected added, got %q", statusByPath["new.txt"])
	}
}

func TestWorktreeDiff_EmptyBaseSHA(t *testing.T) {
	dir := initGitRepo(t)

	// Empty baseSHA falls back to HEAD — no diff when comparing HEAD to HEAD.
	result, err := WorktreeDiff(dir, "", false)
	if err != nil {
		t.Fatalf("WorktreeDiff: %v", err)
	}
	if result.HasDiff {
		t.Error("expected no diff when comparing HEAD to HEAD")
	}
}

func TestWorktreeDiff_IncludesUntracked(t *testing.T) {
	dir := initGitRepo(t)

	// Create an untracked file.
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Without includeUntracked — no diff (no tracked changes).
	result, err := WorktreeDiff(dir, "HEAD", false)
	if err != nil {
		t.Fatalf("WorktreeDiff: %v", err)
	}
	if result.HasDiff {
		t.Error("expected no diff without includeUntracked")
	}

	// With includeUntracked — should include the new file.
	result, err = WorktreeDiff(dir, "HEAD", true)
	if err != nil {
		t.Fatalf("WorktreeDiff: %v", err)
	}
	if !result.HasDiff {
		t.Fatal("expected diff with includeUntracked")
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	f := result.Files[0]
	if f.Path != "untracked.txt" {
		t.Errorf("expected path untracked.txt, got %q", f.Path)
	}
	if f.Status != "added" {
		t.Errorf("expected status added, got %q", f.Status)
	}
	if f.Insertions != 2 {
		t.Errorf("expected 2 insertions, got %d", f.Insertions)
	}
}
