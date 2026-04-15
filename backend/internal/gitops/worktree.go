package gitops

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/allbin/agentique/backend/internal/paths"
)

const maxDiffBytes = 500 * 1024 // 500KB

// WorktreeBasePath returns the base directory for storing git worktrees.
func WorktreeBasePath() string {
	return paths.WorktreeDir()
}

// WorktreePath returns the full path for a worktree given a project name and branch.
func WorktreePath(projectName, branch string) string {
	base := filepath.Join(WorktreeBasePath(), SanitizeBranch(projectName))
	result := filepath.Join(base, SanitizeBranch(branch))
	cleanBase := filepath.Clean(base) + string(filepath.Separator)
	cleanResult := filepath.Clean(result)
	if !strings.HasPrefix(cleanResult, cleanBase) {
		result = filepath.Join(base, "safe-"+SanitizeBranch(branch))
	}
	return result
}

// CreateWorktree creates a new git worktree at worktreePath branching from HEAD.
func CreateWorktree(projectDir, branch, worktreePath string) error {
	safeBranch := SanitizeBranch(branch)
	out, err := gitRun(projectDir, "worktree", "add", "-b", safeBranch, worktreePath, "HEAD")
	if err != nil {
		return fmt.Errorf("git worktree add failed: %w: %s", err, string(out))
	}
	return nil
}

// RestoreWorktree re-creates a worktree for an existing branch.
func RestoreWorktree(projectDir, branch, worktreePath string) error {
	safeBranch := SanitizeBranch(branch)
	out, err := gitRun(projectDir, "worktree", "add", worktreePath, safeBranch)
	if err != nil {
		return fmt.Errorf("git worktree restore failed: %w: %s", err, string(out))
	}
	return nil
}

// RemoveWorktree removes a git worktree. This is best-effort: errors are logged
// but not returned.
func RemoveWorktree(projectDir, worktreePath string) {
	out, err := gitRun(projectDir, "worktree", "remove", worktreePath)
	if err != nil {
		slog.Warn("worktree remove failed (best-effort)", "error", err, "output", strings.TrimSpace(string(out)))
	}
}

// SanitizeBranch replaces characters that are problematic in branch names and paths.
func SanitizeBranch(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "..", "")
	name = strings.TrimLeft(name, ".-")
	if name == "" {
		name = "unnamed"
	}
	return name
}

// DiffStat represents a single file's change summary.
type DiffStat struct {
	Path       string `json:"path"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Status     string `json:"status"` // "modified", "added", "deleted", "renamed"
}

// DiffResult holds the complete diff output for a worktree.
type DiffResult struct {
	HasDiff   bool       `json:"hasDiff"`
	Summary   string     `json:"summary"`
	Files     []DiffStat `json:"files"`
	Diff      string     `json:"diff"`
	Truncated bool       `json:"truncated"`
}

// GetWorktreeBaseSHA returns the current HEAD SHA in a directory.
func GetWorktreeBaseSHA(dir string) (string, error) {
	out, err := gitStdout(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WorktreeDiff computes the diff between baseSHA and current state in worktreePath.
// When includeUntracked is true, untracked files are included in the result as added files.
func WorktreeDiff(worktreePath, baseSHA string, includeUntracked bool) (DiffResult, error) {
	if baseSHA == "" {
		baseSHA = "HEAD"
	}

	// Get numstat for structured file data.
	numstatOut, err := gitStdout(worktreePath, "diff", "--numstat", baseSHA)
	if err != nil {
		return DiffResult{}, fmt.Errorf("git diff --numstat: %w", err)
	}

	// Get name-status for file status (added/modified/deleted/renamed).
	nameStatusOut, err := gitStdout(worktreePath, "diff", "--name-status", baseSHA)
	if err != nil {
		return DiffResult{}, fmt.Errorf("git diff --name-status: %w", err)
	}

	statusMap := parseNameStatus(string(nameStatusOut))
	files := parseNumstat(string(numstatOut), statusMap)

	// Get full unified diff with bounded memory.
	diffOut, truncated, err := diffLimited(worktreePath, baseSHA, maxDiffBytes)
	if err != nil {
		return DiffResult{}, fmt.Errorf("git diff: %w", err)
	}
	diffText := string(diffOut)

	// Augment with untracked files when requested.
	if includeUntracked {
		uFiles, uDiff := untrackedDiffs(worktreePath)
		files = append(files, uFiles...)
		if uDiff != "" {
			if diffText != "" {
				diffText += "\n"
			}
			diffText += uDiff
		}
	}

	if len(files) == 0 {
		return DiffResult{HasDiff: false, Files: []DiffStat{}}, nil
	}

	// Get --stat summary for display.
	statOut, err := gitStdout(worktreePath, "diff", "--stat", baseSHA)
	if err != nil {
		return DiffResult{}, fmt.Errorf("git diff --stat: %w", err)
	}

	return DiffResult{
		HasDiff:   true,
		Summary:   strings.TrimSpace(string(statOut)),
		Files:     files,
		Diff:      diffText,
		Truncated: truncated,
	}, nil
}

// untrackedDiffs returns DiffStat entries and unified diff text for untracked files.
func untrackedDiffs(dir string) ([]DiffStat, string) {
	out, err := gitStdout(dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, ""
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, ""
	}

	var stats []DiffStat
	var diffs []string

	for _, path := range strings.Split(raw, "\n") {
		if path == "" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			continue
		}
		lines := countLines(content)
		stats = append(stats, DiffStat{
			Path:       path,
			Insertions: lines,
			Status:     "added",
		})

		// Generate unified diff for this file. git diff --no-index exits 1
		// when files differ, so we capture output regardless of exit code.
		ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
		cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--", "/dev/null", path)
		cmd.Dir = dir
		diffOut, _ := cmd.Output()
		cancel()
		if len(diffOut) > 0 {
			diffs = append(diffs, string(diffOut))
		}
	}

	return stats, strings.Join(diffs, "\n")
}

// countLines returns the number of lines in content.
func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// diffLimited runs git diff and reads at most limit bytes from stdout.
func diffLimited(dir, baseSHA string, limit int64) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "diff", baseSHA)
	cmd.Dir = dir
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}

	lr := &io.LimitedReader{R: pipe, N: limit + 1}
	data, readErr := io.ReadAll(lr)
	truncated := lr.N <= 0
	if truncated {
		data = data[:limit]
	}

	// Drain remaining output so the process can exit cleanly.
	_, _ = io.Copy(io.Discard, pipe)
	_ = cmd.Wait()

	if readErr != nil {
		return nil, false, readErr
	}
	if ctx.Err() == context.DeadlineExceeded {
		return nil, false, fmt.Errorf("git diff timed out after %s", gitTimeout)
	}
	return data, truncated, nil
}

// parseNameStatus parses `git diff --name-status` output into a file->status map.
func parseNameStatus(output string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		code := parts[0]
		file := parts[1]
		// Renames have format "R100\told\tnew"
		if strings.Contains(file, "\t") {
			file = strings.SplitN(file, "\t", 2)[1]
		}
		switch {
		case strings.HasPrefix(code, "A"):
			m[file] = "added"
		case strings.HasPrefix(code, "D"):
			m[file] = "deleted"
		case strings.HasPrefix(code, "R"):
			m[file] = "renamed"
		default:
			m[file] = "modified"
		}
	}
	return m
}

// parseNumstat parses `git diff --numstat` output into DiffStat structs.
func parseNumstat(output string, statusMap map[string]string) []DiffStat {
	var files []DiffStat
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		ins, _ := strconv.Atoi(parts[0])
		del, _ := strconv.Atoi(parts[1])
		path := parts[2]
		status := statusMap[path]
		if status == "" {
			status = "modified"
		}
		files = append(files, DiffStat{
			Path:       path,
			Insertions: ins,
			Deletions:  del,
			Status:     status,
		})
	}
	if files == nil {
		files = []DiffStat{}
	}
	return files
}
