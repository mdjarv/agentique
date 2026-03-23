package session

import (
	"fmt"
	"os/exec"
	"strings"
)

// MergeBranch performs a --no-ff merge of branch into the current branch in projectDir.
// Returns the merge commit hash on success.
func MergeBranch(projectDir, branch string) (string, error) {
	cmd := exec.Command("git", "merge", "--no-ff", branch)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
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
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
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
	cmd := exec.Command("git", "merge", "--abort")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge --abort failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CurrentBranch returns the current branch name.
func CurrentBranch(projectDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// HasUncommittedChanges returns true if the working tree has uncommitted changes.
func HasUncommittedChanges(dir string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git status failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// DeleteBranch safely deletes a local branch (uses -d, not -D).
func DeleteBranch(projectDir, branch string) error {
	cmd := exec.Command("git", "branch", "-d", branch)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch -d failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PushBranch pushes a branch to origin with upstream tracking.
func PushBranch(projectDir, branch string) error {
	cmd := exec.Command("git", "push", "-u", "origin", branch)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// HasRemote returns true if the named remote exists.
func HasRemote(projectDir, remoteName string) (bool, error) {
	cmd := exec.Command("git", "remote")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
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
	cmd := exec.Command("gh", "pr", "create", "--head", branch, "--title", title, "--body", body)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// GetExistingPR checks if a PR already exists for the given branch.
func GetExistingPR(projectDir, branch string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "url", "-q", ".url")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
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
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
