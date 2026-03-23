package session

import (
	"context"
	"fmt"
	"log"
	"os"

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
	dirty, err := HasUncommittedChanges(wtPath)
	if err != nil {
		log.Printf("session %s: failed to check worktree changes: %v", sessionID, err)
		return nil
	}
	if !dirty {
		return nil
	}
	return AutoCommitAll(wtPath, "agentique: auto-commit before "+reason)
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

	if live := g.mgr.Get(sessionID); live != nil {
		if err := live.TryLockForMerge(); err != nil {
			return MergeResult{}, err
		}
		defer live.UnlockMerge(StateIdle)
	}

	dirty, err := HasUncommittedChanges(project.Path)
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

	hash, mergeErr := MergeBranch(project.Path, branch)
	if mergeErr != nil {
		files, _ := MergeConflictFiles(project.Path)
		_ = AbortMerge(project.Path)
		if len(files) > 0 {
			return MergeResult{Status: "conflict", ConflictFiles: files}, nil
		}
		return MergeResult{Status: "error", Error: mergeErr.Error()}, nil
	}

	if cleanup {
		if wtPath != "" {
			RemoveWorktree(project.Path, wtPath)
		}
		if delErr := DeleteBranch(project.Path, branch); delErr != nil {
			log.Printf("session %s: branch delete after merge: %v", sessionID, delErr)
		}
		_ = g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
			State: string(StateStopped),
			ID:    sessionID,
		})
		g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
			"sessionId": sessionID,
			"state":     string(StateStopped),
		})
	}

	return MergeResult{Status: "merged", CommitHash: hash}, nil
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

	if !HasGhCli() {
		return CreatePRResult{Status: "error", Error: "gh CLI not installed"}, nil
	}

	hasOrigin, err := HasRemote(project.Path, "origin")
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

	if url, prErr := GetExistingPR(project.Path, branch); prErr == nil && url != "" {
		return CreatePRResult{Status: "existing", URL: url}, nil
	}

	if err := PushBranch(project.Path, branch); err != nil {
		return CreatePRResult{Status: "error", Error: err.Error()}, nil
	}

	title := p.Title
	if title == "" {
		title = dbSess.Name
	}

	url, err := CreatePR(project.Path, branch, title, p.Body)
	if err != nil {
		return CreatePRResult{Status: "error", Error: err.Error()}, nil
	}

	return CreatePRResult{Status: "created", URL: url}, nil
}

// Diff returns the diff for a session.
// Worktree sessions diff against their base SHA; non-worktree sessions diff HEAD.
func (g *GitService) Diff(ctx context.Context, sessionID string) (DiffResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return DiffResult{}, fmt.Errorf("session not found")
	}

	// Worktree session: diff worktree against base SHA.
	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		if _, statErr := os.Stat(wtPath); statErr != nil {
			return DiffResult{}, fmt.Errorf("worktree directory not found")
		}
		return WorktreeDiff(wtPath, nullStr(dbSess.WorktreeBaseSha))
	}

	// Non-worktree session: diff work dir against HEAD.
	workDir := dbSess.WorkDir
	if _, statErr := os.Stat(workDir); statErr != nil {
		return DiffResult{}, fmt.Errorf("work directory not found")
	}
	return WorktreeDiff(workDir, "HEAD")
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

	dirty, err := HasUncommittedChanges(dir)
	if err != nil {
		return CommitResult{}, fmt.Errorf("failed to check changes: %w", err)
	}
	if !dirty {
		return CommitResult{}, fmt.Errorf("no uncommitted changes")
	}

	if err := AutoCommitAll(dir, message); err != nil {
		return CommitResult{}, fmt.Errorf("commit failed: %w", err)
	}

	hash, err := headCommitHash(dir)
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit succeeded but failed to get hash: %w", err)
	}

	return CommitResult{CommitHash: hash}, nil
}
