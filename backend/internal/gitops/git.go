package gitops

import (
	"context"
	"encoding/json"
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

// MergeBranch fast-forward merges branch into the current branch in projectDir.
// Returns the resulting HEAD commit hash on success.
func MergeBranch(projectDir, branch string) (string, error) {
	args := []string{"merge", "--ff-only", branch}
	out, err := gitRun(projectDir, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "Not possible to fast-forward") {
			return "", ErrNotFastForward
		}
		return "", fmt.Errorf("git merge failed: %w: %s", err, msg)
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

// ErrDirtyWorktree is returned when an operation requires a clean working tree
// but the project root has uncommitted changes.
var ErrDirtyWorktree = errors.New("project has uncommitted changes — commit or stash them before merging")

// ErrNotFastForward is returned when a --ff-only merge fails because branches have diverged.
var ErrNotFastForward = errors.New("branches have diverged — rebase required")

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
// DiscardAll resets all tracked files and removes untracked files/dirs.
func DiscardAll(dir string) error {
	if _, err := gitRun(dir, "checkout", "--", "."); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}
	if _, err := gitRun(dir, "clean", "-fd"); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}
	return nil
}

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

// ForceDeleteBranch deletes a local branch unconditionally (uses -D).
// Use for ephemeral branches (e.g. channel workers) where unmerged state is expected.
func ForceDeleteBranch(projectDir, branch string) error {
	out, err := gitRun(projectDir, "branch", "-D", branch)
	if err != nil {
		return fmt.Errorf("git branch -D failed: %w: %s", err, strings.TrimSpace(string(out)))
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

// PRStatusResult describes the status of a GitHub pull request.
type PRStatusResult struct {
	Number       int    `json:"number"`
	State        string `json:"state"`        // OPEN, MERGED, CLOSED
	IsDraft      bool   `json:"isDraft"`
	ChecksStatus string `json:"checksStatus"` // "pass", "fail", "pending", "none"
}

// PRStatus fetches PR metadata for the given branch using gh CLI.
// Returns number, state (OPEN/MERGED/CLOSED), draft status, and aggregated CI checks status.
func PRStatus(projectDir, branch string) (PRStatusResult, error) {
	out, err := ghRun(projectDir, "pr", "view", branch, "--json", "number,state,isDraft,statusCheckRollup")
	if err != nil {
		return PRStatusResult{}, fmt.Errorf("gh pr view failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	var raw struct {
		Number             int    `json:"number"`
		State              string `json:"state"`
		IsDraft            bool   `json:"isDraft"`
		StatusCheckRollup  []struct {
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return PRStatusResult{}, fmt.Errorf("parse gh pr view output: %w", err)
	}

	result := PRStatusResult{
		Number:       raw.Number,
		State:        raw.State,
		IsDraft:      raw.IsDraft,
		ChecksStatus: aggregateChecks(raw.StatusCheckRollup),
	}
	return result, nil
}

// aggregateChecks reduces a list of check statuses to a single summary.
func aggregateChecks(checks []struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}) string {
	if len(checks) == 0 {
		return "none"
	}
	hasFailure := false
	hasPending := false
	for _, c := range checks {
		if c.Status != "COMPLETED" {
			hasPending = true
			continue
		}
		if c.Conclusion != "SUCCESS" && c.Conclusion != "NEUTRAL" && c.Conclusion != "SKIPPED" {
			hasFailure = true
		}
	}
	if hasFailure {
		return "fail"
	}
	if hasPending {
		return "pending"
	}
	return "pass"
}

// HeadCommitHash returns the HEAD commit hash for the given directory.
func HeadCommitHash(dir string) (string, error) {
	out, err := gitRun(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ListBranches returns local and remote-only branch names.
// Remote branches that already have a local counterpart are excluded from the remote list.
func ListBranches(dir string) (local []string, remote []string, err error) {
	localOut, err := gitStdout(dir, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, nil, fmt.Errorf("git branch failed: %w", err)
	}
	localSet := make(map[string]struct{})
	for line := range strings.SplitSeq(strings.TrimSpace(string(localOut)), "\n") {
		if b := strings.TrimSpace(line); b != "" {
			local = append(local, b)
			localSet[b] = struct{}{}
		}
	}

	remoteOut, err := gitStdout(dir, "branch", "-r", "--format=%(refname:short)")
	if err != nil {
		// No remotes is fine — just return local branches.
		return local, nil, nil
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(remoteOut)), "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || strings.HasSuffix(ref, "/HEAD") {
			continue
		}
		// Strip remote prefix to get the short name.
		short := ref
		if _, after, ok := strings.Cut(ref, "/"); ok {
			short = after
		}
		if _, exists := localSet[short]; exists {
			continue
		}
		remote = append(remote, short)
	}
	return local, remote, nil
}

// CheckoutBranch switches to the given branch.
// Handles both local branches and auto-creation of tracking branches from remote.
func CheckoutBranch(dir, branch string) error {
	out, err := gitRun(dir, "checkout", branch)
	if err != nil {
		return fmt.Errorf("git checkout failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UpstreamRef returns the upstream tracking ref for the current branch (e.g. "origin/main").
// Returns empty string and nil if no upstream is configured.
func UpstreamRef(dir string) (string, error) {
	out, err := gitStdout(dir, "rev-parse", "--abbrev-ref", "@{u}")
	if err != nil {
		return "", nil
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

// CommitLogEntry represents a single commit in a log listing.
type CommitLogEntry struct {
	Hash      string `json:"hash"`
	Message   string `json:"message"`
	Body      string `json:"body,omitempty"`
	Timestamp string `json:"timestamp"`
}

// CommitLog returns the commits that branch has but the default branch (HEAD) does not.
// Runs in the project dir (not the worktree) — same scope as CommitsAhead.
func CommitLog(dir, branch string, limit int) ([]CommitLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	// Use %x1e (record separator) between commits so multiline bodies don't break parsing.
	// Fields within a record are separated by %x00.
	out, err := gitRun(dir, "log", fmt.Sprintf("HEAD..%s", branch),
		fmt.Sprintf("--format=%%H%%x00%%s%%x00%%b%%x00%%aI%%x1e"), fmt.Sprintf("--max-count=%d", limit))
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	records := strings.Split(strings.TrimSpace(string(out)), "\x1e")
	var entries []CommitLogEntry
	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x00", 4)
		if len(parts) < 4 {
			continue
		}
		entries = append(entries, CommitLogEntry{
			Hash:      parts[0][:min(len(parts[0]), 8)],
			Message:   parts[1],
			Body:      strings.TrimSpace(parts[2]),
			Timestamp: parts[3],
		})
	}
	if entries == nil {
		entries = []CommitLogEntry{}
	}
	return entries, nil
}

// Fetch runs git fetch for the default remote (origin).
func Fetch(dir string) error {
	out, err := gitRun(dir, "fetch")
	if err != nil {
		return fmt.Errorf("git fetch failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// AheadBehindRemote returns how many commits the current branch is ahead of and
// behind its upstream tracking branch. Returns (0, 0, nil) if no upstream is set.
func AheadBehindRemote(dir string) (ahead int, behind int, err error) {
	out, err := gitRun(dir, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		// No upstream configured — not an error, just no remote tracking.
		return 0, 0, nil
	}
	s := strings.TrimSpace(string(out))
	fmt.Sscanf(s, "%d\t%d", &ahead, &behind)
	return ahead, behind, nil
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

// ListTrackedFiles returns all tracked files via git ls-files, capped at 10,000.
func ListTrackedFiles(dir string) ([]string, error) {
	out, err := gitStdout(dir, "ls-files")
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		if l != "" {
			files = append(files, l)
		}
		if len(files) >= 10000 {
			break
		}
	}
	return files, nil
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

// ProjectStatusResult describes the git status of a project root directory.
type ProjectStatusResult struct {
	Branch           string
	UncommittedCount int
	HasRemote        bool
	AheadRemote      int
	BehindRemote     int
}

// ProjectStatus computes the git status for a project root directory.
// Returns a zero-value result if the path is not a git repo.
func ProjectStatus(projectPath string) ProjectStatusResult {
	var r ProjectStatusResult

	branch, err := CurrentBranch(projectPath)
	if err != nil {
		return r
	}
	r.Branch = branch

	if files, err := UncommittedFiles(projectPath); err == nil {
		r.UncommittedCount = len(files)
	}

	hasRemote, err := HasRemote(projectPath, "origin")
	if err != nil || !hasRemote {
		return r
	}
	r.HasRemote = true

	if ahead, behind, err := AheadBehindRemote(projectPath); err == nil {
		r.AheadRemote = ahead
		r.BehindRemote = behind
	}

	return r
}
