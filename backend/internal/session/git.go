package session

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const gitTimeout = 30 * time.Second

// gitRun executes a git command with a timeout and returns combined stdout+stderr.
func gitRun(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git %s timed out after %s", args[0], gitTimeout)
	}
	return out, err
}

// gitStdout executes a git command with a timeout and returns only stdout.
func gitStdout(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git %s timed out after %s", args[0], gitTimeout)
	}
	return out, err
}

// ghRun executes a gh CLI command with a timeout and returns combined output.
func ghRun(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("gh %s timed out after %s", args[0], gitTimeout)
	}
	return out, err
}

// MergeBranch performs a --no-ff merge of branch into the current branch in projectDir.
// Returns the merge commit hash on success.
func MergeBranch(projectDir, branch string) (string, error) {
	out, err := gitRun(projectDir, "merge", "--no-ff", branch)
	if err != nil {
		return "", fmt.Errorf("git merge failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	hash, err := headCommitHash(projectDir)
	if err != nil {
		return "", fmt.Errorf("merge succeeded but failed to get commit hash: %w", err)
	}
	return hash, nil
}

// MergeConflictFiles returns the list of files with merge conflicts.
func MergeConflictFiles(projectDir string) ([]string, error) {
	out, err := gitRun(projectDir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	lines := strings.TrimSpace(string(out))
	if lines == "" {
		return nil, nil
	}
	return strings.Split(lines, "\n"), nil
}

// AbortMerge aborts an in-progress merge.
func AbortMerge(projectDir string) error {
	out, err := gitRun(projectDir, "merge", "--abort")
	if err != nil {
		return fmt.Errorf("git merge --abort failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CurrentBranch returns the current branch name.
func CurrentBranch(projectDir string) (string, error) {
	out, err := gitRun(projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// HasUncommittedChanges returns true if the working tree has uncommitted changes.
func HasUncommittedChanges(dir string) (bool, error) {
	out, err := gitRun(dir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// AutoCommitAll stages all changes and creates a commit in the given directory.
func AutoCommitAll(dir, message string) error {
	if out, err := gitRun(dir, "add", "-A"); err != nil {
		return fmt.Errorf("git add failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := gitRun(dir, "commit", "-m", message); err != nil {
		return fmt.Errorf("git commit failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteBranch safely deletes a local branch (uses -d, not -D).
func DeleteBranch(projectDir, branch string) error {
	out, err := gitRun(projectDir, "branch", "-d", branch)
	if err != nil {
		return fmt.Errorf("git branch -d failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PushBranch pushes a branch to origin with upstream tracking.
func PushBranch(projectDir, branch string) error {
	out, err := gitRun(projectDir, "push", "-u", "origin", branch)
	if err != nil {
		return fmt.Errorf("git push failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// HasRemote returns true if the named remote exists.
func HasRemote(projectDir, remoteName string) (bool, error) {
	out, err := gitRun(projectDir, "remote")
	if err != nil {
		return false, fmt.Errorf("git remote failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == remoteName {
			return true, nil
		}
	}
	return false, nil
}

// HasGhCli returns true if the gh CLI is on PATH.
func HasGhCli() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// CreatePR creates a GitHub pull request using the gh CLI.
func CreatePR(projectDir, branch, title, body string) (string, error) {
	out, err := ghRun(projectDir, "pr", "create", "--head", branch, "--title", title, "--body", body)
	if err != nil {
		return "", fmt.Errorf("gh pr create failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// GetExistingPR checks if a PR already exists for the given branch.
func GetExistingPR(projectDir, branch string) (string, error) {
	out, err := ghRun(projectDir, "pr", "view", branch, "--json", "url", "-q", ".url")
	if err != nil {
		return "", err
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", fmt.Errorf("no PR found")
	}
	return url, nil
}

func headCommitHash(dir string) (string, error) {
	out, err := gitRun(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
