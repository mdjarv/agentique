package session

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/mdjarv/agentique/backend/internal/gitops"
)

// DiscardFile reverts one file in a session's work tree to its committed state,
// deleting it if it was never committed.
//
// The path is not trusted, and syntactic validation is not what makes this
// safe. The real guard is the allowlist: the path must already appear in git's
// own list of changed files for this session, so a path git does not report as
// changed cannot be discarded whatever it points at. `SafeRelativePath` runs as
// well, because a traversal should be refused before it reaches a git argument
// list rather than only failing to match.
//
// Irreversible by construction — there is no reflog entry for an uncommitted
// change — so it takes the session's git lock like a merge, and the caller is
// responsible for confirming with a person first.
func (g *GitService) DiscardFile(
	ctx context.Context,
	sessionID, path string,
) (UncommittedFilesResult, error) {
	if err := gitops.SafeRelativePath(path); err != nil {
		return UncommittedFilesResult{}, err
	}

	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return UncommittedFilesResult{}, fmt.Errorf("session not found")
	}

	dir := dbSess.WorkDir
	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		dir = wtPath
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		return UncommittedFilesResult{}, fmt.Errorf("work directory not found")
	}

	live := g.mgr.Get(sessionID)
	guard, err := tryLockForGitOp(g.mgr, sessionID, live, "discarding", StateIdle)
	if err != nil {
		return UncommittedFilesResult{}, err
	}
	defer guard.Ensure()

	files, err := g.git.UncommittedFiles(dir)
	if err != nil {
		return UncommittedFilesResult{}, fmt.Errorf("failed to list changed files: %w", err)
	}

	if err := discardOne(dir, files, path); err != nil {
		return UncommittedFilesResult{}, err
	}
	slog.Info("discarded file", "session_id", sessionID, "path", path)

	remaining, err := g.git.UncommittedFiles(dir)
	if err != nil {
		return UncommittedFilesResult{}, fmt.Errorf("failed to list changed files: %w", err)
	}
	if remaining == nil {
		remaining = []gitops.FileStatus{}
	}

	if project, projErr := g.queries.GetProject(ctx, dbSess.ProjectID); projErr == nil {
		guard.Release(StateIdle)
		g.broadcastSnapshot(dbSess, project)
	}

	return UncommittedFilesResult{Files: remaining}, nil
}

// discardOne undoes whatever git says happened to this one path.
//
// A rename is the awkward case: porcelain reports it as a single `old -> new`
// entry, so undoing it means removing the destination *and* restoring the
// source. Either half alone leaves the work tree in a state neither git nor the
// reader expected.
func discardOne(dir string, files []gitops.FileStatus, path string) error {
	for _, file := range files {
		if oldPath, newPath, isRename := gitops.RenamePaths(file.Path); isRename {
			if newPath != path && oldPath != path {
				continue
			}
			if err := gitops.RemoveTrackedFile(dir, newPath); err != nil {
				return err
			}
			return gitops.RestoreFile(dir, oldPath)
		}
		if file.Path != path {
			continue
		}
		if file.Status == "untracked" {
			return gitops.RemoveUntrackedFile(dir, path)
		}
		if file.Status == "added" {
			// Staged but never committed: there is no HEAD version to restore.
			return gitops.RemoveTrackedFile(dir, path)
		}
		return gitops.RestoreFile(dir, path)
	}
	return fmt.Errorf("%s has no uncommitted changes", path)
}
