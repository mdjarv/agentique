package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/allbin/agentique/backend/internal/gitops"
	"github.com/allbin/agentique/backend/internal/msggen"
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

// autoCommit commits all uncommitted changes in a worktree directory with a
// Haiku-generated commit message. Returns nil if clean or on check failure.
func autoCommit(ctx context.Context, sessionID, sessionName, wtPath string) error {
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

	msg := msggen.AutoCommitMsg(ctx, sessionName, wtPath)
	return gitops.AutoCommitAll(wtPath, msg)
}

// GitService handles git operations (merge, PR, diff, commit) for sessions.
type GitService struct {
	mgr     *Manager
	queries gitServiceQueries
	hub     Broadcaster
}

// NewGitService creates a new GitService.
func NewGitService(mgr *Manager, queries gitServiceQueries, hub Broadcaster) *GitService {
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

	live := g.mgr.Get(sessionID)
	if live != nil {
		if err := live.TryLockForGitOp("merging"); err != nil {
			return MergeResult{}, err
		}
		defer func() {
			if err := live.UnlockGitOp(StateIdle); err != nil {
				slog.Error("unlock git op failed", "session_id", sessionID, "error", err)
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
	if err := autoCommit(ctx, sessionID, dbSess.Name, wtPath); err != nil {
		return MergeResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	if live != nil {
		live.broadcastState(StateMerging)
	}

	hash, mergeErr := gitops.MergeBranch(project.Path, branch, "Merge: "+dbSess.Name)
	if mergeErr != nil {
		files, _ := gitops.MergeConflictFiles(project.Path)
		_ = gitops.AbortMerge(project.Path)
		if len(files) > 0 {
			slog.Warn("merge conflict", "session_id", sessionID, "branch", branch, "conflict_files", len(files))
			g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
				"sessionId":          sessionID,
				"state":              dbSess.State,
				"mergeStatus":        "conflicts",
				"mergeConflictFiles": files,
			})
			return MergeResult{Status: "conflict", ConflictFiles: files}, nil
		}
		slog.Error("merge failed", "session_id", sessionID, "branch", branch, "error", mergeErr)
		return MergeResult{Status: "error", Error: mergeErr.Error()}, nil
	}

	slog.Info("merge completed", "session_id", sessionID, "branch", branch, "commit", hash)

	if err := g.queries.SetWorktreeMerged(ctx, sessionID); err != nil {
		slog.Warn("persist worktree merged failed", "session_id", sessionID, "error", err)
	}
	if live != nil {
		live.MarkMerged()
	}

	if cleanup {
		if wtPath != "" {
			gitops.RemoveWorktree(project.Path, wtPath)
		}
		if delErr := gitops.DeleteBranch(project.Path, branch); delErr != nil {
			slog.Warn("branch delete after merge failed", "session_id", sessionID, "error", delErr)
		}
		gitops.DeleteRemoteBranch(project.Path, branch)
		go gitops.GC(project.Path)
		if err := g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
			State: string(StateStopped),
			ID:    sessionID,
		}); err != nil {
			slog.Warn("persist session state after merge cleanup failed", "session_id", sessionID, "error", err)
		}
		g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
			"sessionId":      sessionID,
			"state":          string(StateStopped),
			"worktreeMerged": true,
		})
	} else {
		// Use live state if available (UnlockGitOp just set it to idle),
		// fall back to DB snapshot for dead sessions.
		state := dbSess.State
		if live != nil {
			state = string(live.State())
		}
		g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
			"sessionId":      sessionID,
			"state":          state,
			"worktreeMerged": true,
		})
	}

	go g.broadcastSiblingGitStatus(ctx, dbSess.ProjectID, sessionID, project.Path)

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

	live := g.mgr.Get(sessionID)
	if live != nil {
		if err := live.TryLockForGitOp("rebasing"); err != nil {
			return RebaseResult{}, err
		}
		defer func() {
			if err := live.UnlockGitOp(StateIdle); err != nil {
				slog.Error("unlock git op failed", "session_id", sessionID, "error", err)
			}
		}()
	}

	if err := autoCommit(ctx, sessionID, dbSess.Name, wtPath); err != nil {
		return RebaseResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	mainHead, err := gitops.HeadCommitHash(project.Path)
	if err != nil {
		return RebaseResult{Status: "error", Error: "failed to get main HEAD: " + err.Error()}, nil
	}

	if live != nil {
		live.broadcastState(StateMerging)
	}

	if rebaseErr := gitops.RebaseBranch(wtPath, mainHead); rebaseErr != nil {
		files, _ := gitops.RebaseConflictFiles(wtPath)
		_ = gitops.AbortRebase(wtPath)
		if len(files) > 0 {
			slog.Warn("rebase conflict", "session_id", sessionID, "branch", branch, "conflict_files", len(files))
			g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
				"sessionId":          sessionID,
				"state":              dbSess.State,
				"mergeStatus":        "conflicts",
				"mergeConflictFiles": files,
			})
			return RebaseResult{Status: "conflict", ConflictFiles: files}, nil
		}
		slog.Error("rebase failed", "session_id", sessionID, "branch", branch, "error", rebaseErr)
		return RebaseResult{Status: "error", Error: rebaseErr.Error()}, nil
	}

	if err := g.queries.UpdateWorktreeBaseSHA(ctx, store.UpdateWorktreeBaseSHAParams{
		WorktreeBaseSha: sql.NullString{String: mainHead, Valid: true},
		ID:              sessionID,
	}); err != nil {
		slog.Warn("persist worktree base SHA failed", "session_id", sessionID, "error", err)
	}

	slog.Info("rebase completed", "session_id", sessionID, "branch", branch, "newBase", mainHead)

	rebasePayload := map[string]any{
		"sessionId":     sessionID,
		"state":         dbSess.State,
		"commitsBehind": 0,
	}
	if ahead, aErr := gitops.CommitsAhead(project.Path, branch); aErr == nil {
		rebasePayload["commitsAhead"] = ahead
	}
	appendMergeStatus(rebasePayload, project.Path, branch)
	g.hub.Broadcast(dbSess.ProjectID, "session.state", rebasePayload)

	go g.broadcastSiblingGitStatus(ctx, dbSess.ProjectID, sessionID, project.Path)

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
	if err := autoCommit(ctx, p.SessionID, dbSess.Name, wtPath); err != nil {
		return CreatePRResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	if url, prErr := gitops.GetExistingPR(project.Path, branch); prErr == nil && url != "" {
		slog.Info("PR already exists", "session_id", p.SessionID, "url", url)
		g.savePRUrl(ctx, dbSess.ProjectID, p.SessionID, url)
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
	g.savePRUrl(ctx, dbSess.ProjectID, p.SessionID, url)
	return CreatePRResult{Status: "created", URL: url}, nil
}

// savePRUrl persists the PR URL and broadcasts the update to clients.
func (g *GitService) savePRUrl(ctx context.Context, projectID, sessionID, url string) {
	if err := g.queries.UpdateSessionPRUrl(ctx, store.UpdateSessionPRUrlParams{
		PrUrl: url,
		ID:    sessionID,
	}); err != nil {
		slog.Warn("persist PR URL failed", "session_id", sessionID, "error", err)
	}
	g.hub.Broadcast(projectID, "session.pr-updated", map[string]any{
		"sessionId": sessionID,
		"prUrl":     url,
	})
}

// Diff returns the diff for a session.
// Worktree sessions diff against their base SHA; non-worktree sessions diff HEAD.
func (g *GitService) Diff(ctx context.Context, sessionID string) (gitops.DiffResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return gitops.DiffResult{}, fmt.Errorf("session not found")
	}

	noDiff := gitops.DiffResult{HasDiff: false, Files: []gitops.DiffStat{}}

	// Merged sessions have no active worktree — nothing to diff.
	if dbSess.WorktreeMerged != 0 {
		return noDiff, nil
	}

	// Worktree session: diff against base SHA.
	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		if _, statErr := os.Stat(wtPath); statErr != nil {
			return noDiff, nil
		}
		return gitops.WorktreeDiff(wtPath, nullStr(dbSess.WorktreeBaseSha))
	}

	// Local session: diff work dir against HEAD.
	workDir := dbSess.WorkDir
	if _, statErr := os.Stat(workDir); statErr != nil {
		return noDiff, nil
	}
	return gitops.WorktreeDiff(workDir, "HEAD")
}

// Commit stages all changes and commits in the session's work directory.
// For non-worktree (local) sessions, transitions to StateDone after a successful commit.
func (g *GitService) Commit(ctx context.Context, sessionID, message string) (CommitResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return CommitResult{}, fmt.Errorf("session not found")
	}

	isWorktree := nullStr(dbSess.WorktreePath) != ""

	// Use worktree path if available, otherwise work dir.
	dir := dbSess.WorkDir
	if isWorktree {
		dir = nullStr(dbSess.WorktreePath)
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

	if isWorktree {
		// Broadcast updated dirty state + new commits-ahead count.
		payload := map[string]any{
			"sessionId":        sessionID,
			"state":            dbSess.State,
			"hasDirtyWorktree": false,
			"hasUncommitted":   false,
		}
		project, projErr := g.queries.GetProject(ctx, dbSess.ProjectID)
		if projErr == nil {
			if ahead, aErr := gitops.CommitsAhead(project.Path, nullStr(dbSess.WorktreeBranch)); aErr == nil {
				payload["commitsAhead"] = ahead
			}
			appendMergeStatus(payload, project.Path, nullStr(dbSess.WorktreeBranch))
		}
		g.hub.Broadcast(dbSess.ProjectID, "session.state", payload)
	} else {
		// Local sessions are done after commit — their changes are on the main branch.
		if err := g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
			State: string(StateDone),
			ID:    sessionID,
		}); err != nil {
			slog.Warn("persist session state after commit failed", "session_id", sessionID, "error", err)
		}
		g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
			"sessionId":      sessionID,
			"state":          string(StateDone),
			"hasUncommitted": false,
		})
	}

	return CommitResult{CommitHash: hash}, nil
}

// UncommittedFilesResult describes the uncommitted files in a session's working directory.
type UncommittedFilesResult struct {
	Files []gitops.FileStatus `json:"files"`
}

// UncommittedFiles returns the list of uncommitted files for a session.
func (g *GitService) UncommittedFiles(ctx context.Context, sessionID string) (UncommittedFilesResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return UncommittedFilesResult{}, fmt.Errorf("session not found")
	}

	dir := dbSess.WorkDir
	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		dir = wtPath
	}

	if _, statErr := os.Stat(dir); statErr != nil {
		return UncommittedFilesResult{Files: []gitops.FileStatus{}}, nil
	}

	files, err := gitops.UncommittedFiles(dir)
	if err != nil {
		return UncommittedFilesResult{}, fmt.Errorf("failed to get uncommitted files: %w", err)
	}
	if files == nil {
		files = []gitops.FileStatus{}
	}
	return UncommittedFilesResult{Files: files}, nil
}

func (g *GitService) GeneratePRDescription(ctx context.Context, sessionID string) (msggen.PRDescriptionResult, error) {
	diff, err := g.Diff(ctx, sessionID)
	if err != nil {
		return msggen.PRDescriptionResult{}, fmt.Errorf("failed to get diff: %w", err)
	}
	if !diff.HasDiff {
		return msggen.PRDescriptionResult{}, fmt.Errorf("no changes to describe")
	}

	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return msggen.PRDescriptionResult{}, fmt.Errorf("session not found")
	}

	return msggen.PRDescription(ctx, dbSess.Name, diff.Summary, diff.Diff)
}

func (g *GitService) GenerateCommitMessage(ctx context.Context, sessionID string) (msggen.CommitMessageResult, error) {
	diff, err := g.Diff(ctx, sessionID)
	if err != nil {
		return msggen.CommitMessageResult{}, fmt.Errorf("failed to get diff: %w", err)
	}
	if !diff.HasDiff {
		return msggen.CommitMessageResult{}, fmt.Errorf("no changes to describe")
	}

	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return msggen.CommitMessageResult{}, fmt.Errorf("session not found")
	}

	return msggen.CommitMsg(ctx, dbSess.Name, diff.Summary, diff.Diff)
}

// CleanResult describes the outcome of a clean operation.
type CleanResult struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Clean removes git artifacts (worktree, local branch, remote branch) for a session
// that was merged manually or abandoned. Transitions the session to stopped.
func (g *GitService) Clean(ctx context.Context, sessionID string) (CleanResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return CleanResult{}, fmt.Errorf("session not found")
	}

	branch := nullStr(dbSess.WorktreeBranch)
	if branch == "" {
		return CleanResult{}, fmt.Errorf("session has no worktree branch")
	}

	project, err := g.queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return CleanResult{}, fmt.Errorf("project not found")
	}

	if live := g.mgr.Get(sessionID); live != nil {
		if state := live.State(); state == StateRunning {
			return CleanResult{}, fmt.Errorf("session is running")
		}
	}

	slog.Info("clean started", "session_id", sessionID, "branch", branch)

	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		gitops.RemoveWorktree(project.Path, wtPath)
	}
	if gitops.BranchExists(project.Path, branch) {
		if delErr := gitops.DeleteBranch(project.Path, branch); delErr != nil {
			slog.Warn("branch delete during clean failed", "session_id", sessionID, "error", delErr)
		}
	}
	gitops.DeleteRemoteBranch(project.Path, branch)
	go gitops.GC(project.Path)

	if err := g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: string(StateStopped),
		ID:    sessionID,
	}); err != nil {
		slog.Warn("persist session state after clean failed", "session_id", sessionID, "error", err)
	}
	g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
		"sessionId": sessionID,
		"state":     string(StateStopped),
	})

	slog.Info("clean completed", "session_id", sessionID, "branch", branch)
	return CleanResult{Status: "cleaned"}, nil
}

// RefreshGitStatus recomputes and broadcasts git status for a single session.
func (g *GitService) RefreshGitStatus(ctx context.Context, sessionID string) error {
	ss, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	project, err := g.queries.GetProject(ctx, ss.ProjectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}

	payload := map[string]any{
		"sessionId": ss.ID,
		"state":     ss.State,
	}

	branch := nullStr(ss.WorktreeBranch)
	wtPath := nullStr(ss.WorktreePath)

	if wtPath != "" {
		if dirty, err := gitops.HasUncommittedChanges(wtPath); err == nil {
			payload["hasDirtyWorktree"] = dirty
			payload["hasUncommitted"] = dirty
		}
	}

	if ss.WorktreeMerged != 0 {
		payload["worktreeMerged"] = true
	} else if branch != "" {
		if !gitops.BranchExists(project.Path, branch) {
			payload["branchMissing"] = true
		} else {
			if ahead, err := gitops.CommitsAhead(project.Path, branch); err == nil {
				payload["commitsAhead"] = ahead
			}
			if behind, err := gitops.CommitsBehind(project.Path, branch); err == nil {
				payload["commitsBehind"] = behind
			}
			appendMergeStatus(payload, project.Path, branch)
		}
	}

	g.hub.Broadcast(ss.ProjectID, "session.state", payload)
	return nil
}

// broadcastSiblingGitStatus recomputes and broadcasts git status for all worktree sessions
// in the same project except excludeID. Called after operations that advance the main branch.
func (g *GitService) broadcastSiblingGitStatus(ctx context.Context, projectID, excludeID, projectPath string) {
	sessions, err := g.queries.ListSessionsByProject(ctx, projectID)
	if err != nil {
		return
	}
	for _, ss := range sessions {
		if ss.ID == excludeID || ss.WorktreeMerged != 0 {
			continue
		}
		branch := nullStr(ss.WorktreeBranch)
		if branch == "" {
			continue
		}
		payload := map[string]any{
			"sessionId": ss.ID,
			"state":     ss.State,
		}
		if !gitops.BranchExists(projectPath, branch) {
			payload["branchMissing"] = true
			g.hub.Broadcast(projectID, "session.state", payload)
			continue
		}
		if ahead, err := gitops.CommitsAhead(projectPath, branch); err == nil {
			payload["commitsAhead"] = ahead
		}
		if behind, err := gitops.CommitsBehind(projectPath, branch); err == nil {
			payload["commitsBehind"] = behind
		}
		appendMergeStatus(payload, projectPath, branch)
		g.hub.Broadcast(projectID, "session.state", payload)
	}
}

// appendMergeStatus runs an in-memory merge-tree check and adds mergeStatus +
// mergeConflictFiles to the broadcast payload. Failures are treated as "unknown".
func appendMergeStatus(payload map[string]any, projectDir, branch string) {
	result, err := gitops.MergeTreeCheck(projectDir, branch)
	if err != nil {
		payload["mergeStatus"] = "unknown"
		return
	}
	if result.Clean {
		payload["mergeStatus"] = "clean"
	} else {
		payload["mergeStatus"] = "conflicts"
		payload["mergeConflictFiles"] = result.ConflictFiles
	}
}
