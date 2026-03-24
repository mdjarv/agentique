package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/allbin/agentique/backend/internal/gitops"
	"github.com/allbin/agentique/backend/internal/store"
)

// MergeResult describes the outcome of a merge operation.
type MergeResult struct {
	Status        string   `json:"status"`
	CommitHash    string   `json:"commitHash,omitempty"`
	ConflictFiles []string `json:"conflictFiles,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// CreatePRResult describes the outcome of a PR creation.
type CreatePRResult struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CreatePRParams holds parameters for creating a PR.
type CreatePRParams struct {
	SessionID string
	Title     string
	Body      string
}

// CommitResult describes the outcome of a commit operation.
type CommitResult struct {
	CommitHash string `json:"commitHash"`
}

// autoCommitWorktree commits all uncommitted changes in a worktree directory.
// Returns nil if the directory is empty, clean, or on check failure (logged, not fatal).
func autoCommitWorktree(sessionID, wtPath, reason string) error {
	if wtPath == "" {
		return nil
	}
	dirty, err := gitops.HasUncommittedChanges(wtPath)
	if err != nil {
		slog.Warn("failed to check worktree changes", "session_id", sessionID, "error", err)
		return nil
	}
	if !dirty {
		return nil
	}
	return gitops.AutoCommitAll(wtPath, "agentique: auto-commit before "+reason)
}

// GitService handles git operations (merge, PR, diff, commit) for sessions.
type GitService struct {
	mgr     *Manager
	queries *store.Queries
	hub     Broadcaster
}

// NewGitService creates a new GitService.
func NewGitService(mgr *Manager, queries *store.Queries, hub Broadcaster) *GitService {
	return &GitService{mgr: mgr, queries: queries, hub: hub}
}

// Merge merges a worktree session's branch into the project's main branch.
func (g *GitService) Merge(ctx context.Context, sessionID string, cleanup bool) (MergeResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return MergeResult{}, fmt.Errorf("session not found")
	}
	branch := nullStr(dbSess.WorktreeBranch)
	if branch == "" {
		return MergeResult{}, fmt.Errorf("session has no worktree branch")
	}

	project, err := g.queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return MergeResult{}, fmt.Errorf("project not found")
	}

	slog.Info("merge started", "session_id", sessionID, "branch", branch, "cleanup", cleanup)

	if live := g.mgr.Get(sessionID); live != nil {
		if err := live.TryLockForMerge(); err != nil {
			return MergeResult{}, err
		}
		defer func() {
			if err := live.UnlockMerge(StateIdle); err != nil {
				slog.Error("unlock merge failed", "session_id", sessionID, "error", err)
			}
		}()
	}

	dirty, err := gitops.HasUncommittedChanges(project.Path)
	if err != nil {
		return MergeResult{Status: "error", Error: err.Error()}, nil
	}
	if dirty {
		return MergeResult{Status: "error", Error: "project root has uncommitted changes"}, nil
	}

	wtPath := nullStr(dbSess.WorktreePath)
	if err := autoCommitWorktree(sessionID, wtPath, "merge"); err != nil {
		return MergeResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	hash, mergeErr := gitops.MergeBranch(project.Path, branch)
	if mergeErr != nil {
		files, _ := gitops.MergeConflictFiles(project.Path)
		_ = gitops.AbortMerge(project.Path)
		if len(files) > 0 {
			slog.Warn("merge conflict", "session_id", sessionID, "branch", branch, "conflict_files", len(files))
			return MergeResult{Status: "conflict", ConflictFiles: files}, nil
		}
		slog.Error("merge failed", "session_id", sessionID, "branch", branch, "error", mergeErr)
		return MergeResult{Status: "error", Error: mergeErr.Error()}, nil
	}

	slog.Info("merge completed", "session_id", sessionID, "branch", branch, "commit", hash)

	_ = g.queries.SetWorktreeMerged(ctx, sessionID)
	if live := g.mgr.Get(sessionID); live != nil {
		live.MarkMerged()
	}

	if cleanup {
		if wtPath != "" {
			gitops.RemoveWorktree(project.Path, wtPath)
		}
		if delErr := gitops.DeleteBranch(project.Path, branch); delErr != nil {
			slog.Warn("branch delete after merge failed", "session_id", sessionID, "error", delErr)
		}
		_ = g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
			State: string(StateStopped),
			ID:    sessionID,
		})
		g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
			"sessionId":      sessionID,
			"state":          string(StateStopped),
			"worktreeMerged": true,
		})
	} else {
		g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
			"sessionId":      sessionID,
			"state":          dbSess.State,
			"worktreeMerged": true,
		})
	}

	return MergeResult{Status: "merged", CommitHash: hash}, nil
}

// RebaseResult describes the outcome of a rebase operation.
type RebaseResult struct {
	Status        string   `json:"status"`
	ConflictFiles []string `json:"conflictFiles,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// Rebase rebases a worktree session's branch onto the project's current HEAD.
func (g *GitService) Rebase(ctx context.Context, sessionID string) (RebaseResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return RebaseResult{}, fmt.Errorf("session not found")
	}
	branch := nullStr(dbSess.WorktreeBranch)
	wtPath := nullStr(dbSess.WorktreePath)
	if branch == "" || wtPath == "" {
		return RebaseResult{}, fmt.Errorf("session has no worktree")
	}

	project, err := g.queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return RebaseResult{}, fmt.Errorf("project not found")
	}

	slog.Info("rebase started", "session_id", sessionID, "branch", branch)

	if live := g.mgr.Get(sessionID); live != nil {
		if err := live.TryLockForMerge(); err != nil {
			return RebaseResult{}, err
		}
		defer func() {
			if err := live.UnlockMerge(StateIdle); err != nil {
				slog.Error("unlock merge failed", "session_id", sessionID, "error", err)
			}
		}()
	}

	if err := autoCommitWorktree(sessionID, wtPath, "rebase"); err != nil {
		return RebaseResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	mainHead, err := gitops.HeadCommitHash(project.Path)
	if err != nil {
		return RebaseResult{Status: "error", Error: "failed to get main HEAD: " + err.Error()}, nil
	}

	if rebaseErr := gitops.RebaseBranch(wtPath, mainHead); rebaseErr != nil {
		files, _ := gitops.RebaseConflictFiles(wtPath)
		_ = gitops.AbortRebase(wtPath)
		if len(files) > 0 {
			slog.Warn("rebase conflict", "session_id", sessionID, "branch", branch, "conflict_files", len(files))
			return RebaseResult{Status: "conflict", ConflictFiles: files}, nil
		}
		slog.Error("rebase failed", "session_id", sessionID, "branch", branch, "error", rebaseErr)
		return RebaseResult{Status: "error", Error: rebaseErr.Error()}, nil
	}

	_ = g.queries.UpdateWorktreeBaseSHA(ctx, store.UpdateWorktreeBaseSHAParams{
		WorktreeBaseSha: sql.NullString{String: mainHead, Valid: true},
		ID:              sessionID,
	})

	slog.Info("rebase completed", "session_id", sessionID, "branch", branch, "newBase", mainHead)

	g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
		"sessionId":     sessionID,
		"state":         dbSess.State,
		"commitsBehind": 0,
	})

	return RebaseResult{Status: "rebased"}, nil
}

// CreatePR pushes the session branch and creates a GitHub PR.
func (g *GitService) CreatePR(ctx context.Context, p CreatePRParams) (CreatePRResult, error) {
	dbSess, err := g.queries.GetSession(ctx, p.SessionID)
	if err != nil {
		return CreatePRResult{}, fmt.Errorf("session not found")
	}
	branch := nullStr(dbSess.WorktreeBranch)
	if branch == "" {
		return CreatePRResult{}, fmt.Errorf("session has no worktree branch")
	}

	project, err := g.queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return CreatePRResult{}, fmt.Errorf("project not found")
	}

	slog.Info("creating PR", "session_id", p.SessionID, "branch", branch)

	if !gitops.HasGhCli() {
		return CreatePRResult{Status: "error", Error: "gh CLI not installed"}, nil
	}

	hasOrigin, err := gitops.HasRemote(project.Path, "origin")
	if err != nil {
		return CreatePRResult{Status: "error", Error: err.Error()}, nil
	}
	if !hasOrigin {
		return CreatePRResult{Status: "error", Error: "no origin remote configured"}, nil
	}

	wtPath := nullStr(dbSess.WorktreePath)
	if err := autoCommitWorktree(p.SessionID, wtPath, "PR"); err != nil {
		return CreatePRResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	if url, prErr := gitops.GetExistingPR(project.Path, branch); prErr == nil && url != "" {
		slog.Info("PR already exists", "session_id", p.SessionID, "url", url)
		return CreatePRResult{Status: "existing", URL: url}, nil
	}

	if err := gitops.PushBranch(project.Path, branch); err != nil {
		return CreatePRResult{Status: "error", Error: err.Error()}, nil
	}

	title := p.Title
	if title == "" {
		title = dbSess.Name
	}

	url, err := gitops.CreatePR(project.Path, branch, title, p.Body)
	if err != nil {
		return CreatePRResult{Status: "error", Error: err.Error()}, nil
	}

	slog.Info("PR created", "session_id", p.SessionID, "url", url)
	return CreatePRResult{Status: "created", URL: url}, nil
}

// Diff returns the diff for a session.
// Worktree sessions diff against their base SHA; non-worktree sessions diff HEAD.
func (g *GitService) Diff(ctx context.Context, sessionID string) (gitops.DiffResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return gitops.DiffResult{}, fmt.Errorf("session not found")
	}

	// Worktree session: diff worktree against base SHA.
	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		if _, statErr := os.Stat(wtPath); statErr != nil {
			return gitops.DiffResult{}, fmt.Errorf("worktree directory not found")
		}
		return gitops.WorktreeDiff(wtPath, nullStr(dbSess.WorktreeBaseSha))
	}

	// Non-worktree session: diff work dir against HEAD.
	workDir := dbSess.WorkDir
	if _, statErr := os.Stat(workDir); statErr != nil {
		return gitops.DiffResult{}, fmt.Errorf("work directory not found")
	}
	return gitops.WorktreeDiff(workDir, "HEAD")
}

// Commit stages all changes and commits in the session's work directory.
func (g *GitService) Commit(ctx context.Context, sessionID, message string) (CommitResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return CommitResult{}, fmt.Errorf("session not found")
	}

	// Use worktree path if available, otherwise work dir.
	dir := dbSess.WorkDir
	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		dir = wtPath
	}

	if _, statErr := os.Stat(dir); statErr != nil {
		return CommitResult{}, fmt.Errorf("work directory not found")
	}

	dirty, err := gitops.HasUncommittedChanges(dir)
	if err != nil {
		return CommitResult{}, fmt.Errorf("failed to check changes: %w", err)
	}
	if !dirty {
		return CommitResult{}, fmt.Errorf("no uncommitted changes")
	}

	if err := gitops.AutoCommitAll(dir, message); err != nil {
		return CommitResult{}, fmt.Errorf("commit failed: %w", err)
	}

	hash, err := gitops.HeadCommitHash(dir)
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit succeeded but failed to get hash: %w", err)
	}

	slog.Info("commit created", "session_id", sessionID, "commit", hash)
	return CommitResult{CommitHash: hash}, nil
}
