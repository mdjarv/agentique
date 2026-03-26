package gitops

import (
	"context"
	"errors"
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
func MergeBranch(projectDir, branch, message string) (string, error) {
	args := []string{"merge", "--no-ff"}
	if message != "" {
		args = append(args, "-m", message)
	}
	args = append(args, branch)
	out, err := gitRun(projectDir, args...)
	if err != nil {
		return "", fmt.Errorf("git merge failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	hash, err := HeadCommitHash(projectDir)
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

// UncommittedDiff returns the diff of uncommitted changes (staged + unstaged vs HEAD)
// and a short status summary. Used for generating commit messages.
func UncommittedDiff(dir string) (diff string, summary string, err error) {
	statusOut, sErr := gitStdout(dir, "status", "--short")
	if sErr != nil {
		return "", "", fmt.Errorf("git status failed: %w", sErr)
	}
	summary = strings.TrimSpace(string(statusOut))

	diffOut, dErr := gitStdout(dir, "diff", "HEAD")
	if dErr != nil {
		return "", summary, fmt.Errorf("git diff HEAD failed: %w", dErr)
	}
	return string(diffOut), summary, nil
}

// FileStatus describes a single file's status from git status --porcelain.
type FileStatus struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "modified", "added", "deleted", "renamed", "untracked"
}

// UncommittedFiles returns the list of uncommitted (tracked + untracked) files.
func UncommittedFiles(dir string) ([]FileStatus, error) {
	out, err := gitRun(dir, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git status failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	var files []FileStatus
	for _, line := range strings.Split(raw, "\n") {
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		path := line[3:]
		files = append(files, FileStatus{
			Path:   path,
			Status: porcelainStatus(xy),
		})
	}
	return files, nil
}

func porcelainStatus(xy string) string {
	switch {
	case xy == "??":
		return "untracked"
	case xy[0] == 'A' || xy[1] == 'A':
		return "added"
	case xy[0] == 'D' || xy[1] == 'D':
		return "deleted"
	case xy[0] == 'R' || xy[1] == 'R':
		return "renamed"
	default:
		return "modified"
	}
}

// GC runs a full git gc to pack loose objects and prune unreachable data.
func GC(projectDir string) {
	_, _ = gitRun(projectDir, "gc", "--quiet")
}

// DeleteRemoteBranch deletes a branch from the origin remote.
// Fails silently if no remote exists or the branch is not on the remote.
func DeleteRemoteBranch(projectDir, branch string) {
	hasOrigin, err := HasRemote(projectDir, "origin")
	if err != nil || !hasOrigin {
		return
	}
	_, _ = gitRun(projectDir, "push", "origin", "--delete", branch)
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

// HeadCommitHash returns the HEAD commit hash for the given directory.
func HeadCommitHash(dir string) (string, error) {
	out, err := gitRun(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// BranchExists returns true if the given local branch exists in the repo.
func BranchExists(dir, branch string) bool {
	_, err := gitRun(dir, "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

// CommitsAhead returns how many commits branch has that are not in the default branch.
// Uses the repo's HEAD branch (usually master/main) as the base.
func CommitsAhead(dir, branch string) (int, error) {
	out, err := gitRun(dir, "rev-list", "--count", "HEAD.."+branch)
	if err != nil {
		return 0, fmt.Errorf("rev-list failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	s := strings.TrimSpace(string(out))
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n, nil
}

// CommitsBehind returns how many commits the default branch has that branch does not.
func CommitsBehind(dir, branch string) (int, error) {
	out, err := gitRun(dir, "rev-list", "--count", branch+"..HEAD")
	if err != nil {
		return 0, fmt.Errorf("rev-list failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	s := strings.TrimSpace(string(out))
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n, nil
}

// RebaseBranch rebases the current branch in dir onto the given commit/ref.
func RebaseBranch(dir, onto string) error {
	out, err := gitRun(dir, "rebase", onto)
	if err != nil {
		return fmt.Errorf("git rebase failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RebaseConflictFiles returns the list of files with rebase conflicts.
func RebaseConflictFiles(dir string) ([]string, error) {
	return MergeConflictFiles(dir)
}

// AbortRebase aborts an in-progress rebase.
func AbortRebase(dir string) error {
	out, err := gitRun(dir, "rebase", "--abort")
	if err != nil {
		return fmt.Errorf("git rebase --abort failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// MergeTreeResult describes whether a merge would be clean or have conflicts.
type MergeTreeResult struct {
	Clean         bool
	ConflictFiles []string
}

// MergeTreeCheck performs an in-memory merge check using git merge-tree --write-tree.
// No working tree or index mutations — pure read-only operation.
func MergeTreeCheck(projectDir, branch string) (MergeTreeResult, error) {
	out, err := gitRun(projectDir, "merge-tree", "--write-tree", "--name-only", "HEAD", branch)
	if err == nil {
		return MergeTreeResult{Clean: true}, nil
	}

	// Exit code 1 = conflicts. Parse file names between tree hash and blank line.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		return MergeTreeResult{}, fmt.Errorf("git merge-tree failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	lines := strings.Split(string(out), "\n")
	var files []string
	// Skip first line (tree hash), collect file names until blank line.
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		files = append(files, trimmed)
	}
	return MergeTreeResult{Clean: false, ConflictFiles: files}, nil
}
