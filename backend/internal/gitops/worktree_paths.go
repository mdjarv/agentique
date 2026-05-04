package gitops

import (
	"path/filepath"
	"strings"

	"github.com/allbin/agentkit/worktree"
	"github.com/mdjarv/agentique/backend/internal/paths"
)

// WorktreeBasePath returns the base directory for storing git worktrees.
func WorktreeBasePath() string {
	return paths.WorktreeDir()
}

// WorktreePath returns the full path for a worktree given a project name and branch.
// Encodes agentique's per-project naming convention; both segments are sanitized to
// guard against traversal.
func WorktreePath(projectName, branch string) string {
	base := filepath.Join(WorktreeBasePath(), worktree.SanitizeBranch(projectName))
	result := filepath.Join(base, worktree.SanitizeBranch(branch))
	cleanBase := filepath.Clean(base) + string(filepath.Separator)
	cleanResult := filepath.Clean(result)
	if !strings.HasPrefix(cleanResult, cleanBase) {
		result = filepath.Join(base, "safe-"+worktree.SanitizeBranch(branch))
	}
	return result
}
