package session

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeBasePath returns the base directory for storing git worktrees.
func WorktreeBasePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".agentique", "worktrees")
}

// WorktreePath returns the full path for a worktree given a project name and branch.
func WorktreePath(projectName, branch string) string {
	return filepath.Join(WorktreeBasePath(), sanitizeBranch(projectName), sanitizeBranch(branch))
}

// CreateWorktree creates a new git worktree at worktreePath branching from HEAD.
func CreateWorktree(projectDir, branch, worktreePath string) error {
	safeBranch := sanitizeBranch(branch)
	cmd := exec.Command("git", "worktree", "add", "-b", safeBranch, worktreePath, "HEAD")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add failed: %w: %s", err, string(out))
	}
	return nil
}

// RemoveWorktree removes a git worktree. This is best-effort: errors are logged
// but not returned.
func RemoveWorktree(projectDir, worktreePath string) {
	cmd := exec.Command("git", "worktree", "remove", worktreePath)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("warning: git worktree remove failed (best-effort): %v: %s", err, string(out))
	}
}

// sanitizeBranch replaces characters that are problematic in branch names and paths.
func sanitizeBranch(name string) string {
	return strings.ReplaceAll(name, "/", "-")
}
