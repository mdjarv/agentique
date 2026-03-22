package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo creates a temporary git repo with one commit so worktrees can be created.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
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

	run("init")
	run("checkout", "-b", "main")

	// Create a file and commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial")

	return dir
}

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
		got := sanitizeBranch(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeBranch(%q) = %q, want %q", tt.input, got, tt.want)
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
