package session

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const maxDiffBytes = 500 * 1024 // 500KB

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

// RestoreWorktree re-creates a worktree for an existing branch.
func RestoreWorktree(projectDir, branch, worktreePath string) error {
	safeBranch := sanitizeBranch(branch)
	cmd := exec.Command("git", "worktree", "add", worktreePath, safeBranch)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree restore failed: %w: %s", err, string(out))
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
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WorktreeDiff computes the diff between baseSHA and current state in worktreePath.
func WorktreeDiff(worktreePath, baseSHA string) (DiffResult, error) {
	if baseSHA == "" {
		baseSHA = "HEAD"
	}

	// Get numstat for structured file data.
	numstatCmd := exec.Command("git", "diff", "--numstat", baseSHA)
	numstatCmd.Dir = worktreePath
	numstatOut, err := numstatCmd.Output()
	if err != nil {
		return DiffResult{}, fmt.Errorf("git diff --numstat: %w", err)
	}

	// Get name-status for file status (added/modified/deleted/renamed).
	nameStatusCmd := exec.Command("git", "diff", "--name-status", baseSHA)
	nameStatusCmd.Dir = worktreePath
	nameStatusOut, err := nameStatusCmd.Output()
	if err != nil {
		return DiffResult{}, fmt.Errorf("git diff --name-status: %w", err)
	}

	// Parse name-status into a map.
	statusMap := parseNameStatus(string(nameStatusOut))

	// Parse numstat into DiffStat structs.
	files := parseNumstat(string(numstatOut), statusMap)

	if len(files) == 0 {
		return DiffResult{HasDiff: false, Files: []DiffStat{}}, nil
	}

	// Get --stat summary for display.
	statCmd := exec.Command("git", "diff", "--stat", baseSHA)
	statCmd.Dir = worktreePath
	statOut, err := statCmd.Output()
	if err != nil {
		return DiffResult{}, fmt.Errorf("git diff --stat: %w", err)
	}

	// Get full unified diff.
	diffCmd := exec.Command("git", "diff", baseSHA)
	diffCmd.Dir = worktreePath
	diffOut, err := diffCmd.Output()
	if err != nil {
		return DiffResult{}, fmt.Errorf("git diff: %w", err)
	}

	result := DiffResult{
		HasDiff: true,
		Summary: strings.TrimSpace(string(statOut)),
		Files:   files,
		Diff:    string(diffOut),
	}

	if len(diffOut) > maxDiffBytes {
		result.Diff = string(diffOut[:maxDiffBytes])
		result.Truncated = true
	}

	return result, nil
}

// parseNameStatus parses `git diff --name-status` output into a file→status map.
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
