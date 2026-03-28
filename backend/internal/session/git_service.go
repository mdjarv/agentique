package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

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
func autoCommit(ctx context.Context, runner msggen.Runner, sessionID, sessionName, wtPath string) error {
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

	msg := msggen.AutoCommitMsg(ctx, runner, sessionName, wtPath)
	return gitops.AutoCommitAll(wtPath, msg)
}

// GitService handles git operations (merge, PR, diff, commit) for sessions.
type GitService struct {
	mgr         *Manager
	queries     gitServiceQueries
	hub         Broadcaster
	runner      msggen.Runner
	gitVersions sync.Map // sessionID → *atomic.Int64
}

// NewGitService creates a new GitService.
func NewGitService(mgr *Manager, queries gitServiceQueries, hub Broadcaster, runner msggen.Runner) *GitService {
	return &GitService{mgr: mgr, queries: queries, hub: hub, runner: runner}
}

// nextVersion returns a monotonically increasing version number for a session.
// Uses live session counter when available, falls back to an atomic counter per session.
func (g *GitService) nextVersion(sessionID string) int64 {
	if live := g.mgr.Get(sessionID); live != nil {
		return live.nextGitVersion()
	}
	val, _ := g.gitVersions.LoadOrStore(sessionID, &atomic.Int64{})
	return val.(*atomic.Int64).Add(1)
}

// SeedVersion stores a version baseline for a session that is no longer live.
// Called when a session is stopped so that subsequent broadcasts (or a future
// resume) continue from the correct version instead of resetting to 0.
func (g *GitService) SeedVersion(sessionID string, version int64) {
	v := &atomic.Int64{}
	v.Store(version)
	g.gitVersions.Store(sessionID, v)
}

// LastVersion returns the last known version for a session (from the fallback map).
// Returns 0 if no version is stored.
func (g *GitService) LastVersion(sessionID string) int64 {
	if val, ok := g.gitVersions.Load(sessionID); ok {
		return val.(*atomic.Int64).Load()
	}
	return 0
}

// CleanupVersion removes the version counter for a deleted session.
func (g *GitService) CleanupVersion(sessionID string) {
	g.gitVersions.Delete(sessionID)
}

// computeGitSnapshot builds a complete, versioned snapshot of a session's git state.
// It checks the live session for state/connected/merged flags and falls back to DB.
func (g *GitService) computeGitSnapshot(ctx context.Context, sessionID string) (GitSnapshot, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return GitSnapshot{}, fmt.Errorf("session not found: %w", err)
	}
	project, err := g.queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return GitSnapshot{}, fmt.Errorf("project not found: %w", err)
	}
	return g.buildSnapshot(dbSess, project), nil
}

// buildSnapshot constructs a GitSnapshot from pre-loaded DB session + project.
// Avoids redundant DB lookups when the caller already has these values.
func (g *GitService) buildSnapshot(dbSess store.Session, project store.Project) GitSnapshot {
	snap := GitSnapshot{SessionID: dbSess.ID}

	// Use live session state when available, fall back to DB.
	if live := g.mgr.Get(dbSess.ID); live != nil {
		state, connected, merged, completedAt, gitOp := live.liveState()
		snap.State = string(state)
		snap.Connected = connected
		snap.WorktreeMerged = merged
		snap.CompletedAt = completedAt
		snap.GitOperation = gitOp
	} else {
		snap.State = dbSess.State
		snap.WorktreeMerged = dbSess.WorktreeMerged != 0
		snap.CompletedAt = nullStr(dbSess.CompletedAt)
	}

	branch := nullStr(dbSess.WorktreeBranch)
	wtPath := nullStr(dbSess.WorktreePath)

	// Check dirty/uncommitted state.
	if wtPath != "" && !snap.WorktreeMerged {
		if dirty, err := gitops.HasUncommittedChanges(wtPath); err == nil {
			snap.HasDirtyWorktree = dirty
			snap.HasUncommitted = dirty
		}
	}

	// Branch-level checks: ahead/behind, merge status.
	if branch != "" && !snap.WorktreeMerged {
		if !gitops.BranchExists(project.Path, branch) {
			snap.BranchMissing = true
		} else {
			if ahead, err := gitops.CommitsAhead(project.Path, branch); err == nil {
				snap.CommitsAhead = ahead
			}
			if behind, err := gitops.CommitsBehind(project.Path, branch); err == nil {
				snap.CommitsBehind = behind
			}
			result, mergeErr := gitops.MergeTreeCheck(project.Path, branch)
			if mergeErr != nil {
				snap.MergeStatus = "unknown"
			} else if result.Clean {
				snap.MergeStatus = "clean"
			} else {
				snap.MergeStatus = "conflicts"
				snap.MergeConflictFiles = result.ConflictFiles
			}
		}
	}

	snap.Version = g.nextVersion(dbSess.ID)
	return snap
}

// broadcastSnapshot computes and broadcasts a full git snapshot for a session.
func (g *GitService) broadcastSnapshot(dbSess store.Session, project store.Project) {
	snap := g.buildSnapshot(dbSess, project)
	g.hub.Broadcast(dbSess.ProjectID, "session.state", snap)
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
			_ = live.UnlockGitOp(StateIdle) // safety net for early returns
		}()
	}

	wtPath := nullStr(dbSess.WorktreePath)
	if err := autoCommit(ctx, g.runner, sessionID, dbSess.Name, wtPath); err != nil {
		return MergeResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	if live != nil {
		live.broadcastState(StateMerging)
	}

	var hash string
	var mergeResult MergeResult
	stashConflict, stashErr := gitops.WithCleanWorktree(project.Path, func() error {
		var mergeErr error
		hash, mergeErr = gitops.MergeBranch(project.Path, branch, "Merge: "+dbSess.Name)
		if mergeErr != nil {
			files, _ := gitops.MergeConflictFiles(project.Path)
			_ = gitops.AbortMerge(project.Path)
			if len(files) > 0 {
				slog.Warn("merge conflict", "session_id", sessionID, "branch", branch, "conflict_files", len(files))
				mergeResult = MergeResult{Status: "conflict", ConflictFiles: files}
				return mergeErr
			}
			slog.Error("merge failed", "session_id", sessionID, "branch", branch, "error", mergeErr)
			mergeResult = MergeResult{Status: "error", Error: mergeErr.Error()}
			return mergeErr
		}
		return nil
	})
	if stashErr != nil {
		return MergeResult{Status: "error", Error: stashErr.Error()}, nil
	}
	if stashConflict {
		slog.Warn("stash pop conflict after merge — user's uncommitted changes are preserved in git stash", "session_id", sessionID)
	}
	if mergeResult.Status != "" {
		if live != nil {
			_ = live.UnlockGitOp(StateIdle)
		}
		g.broadcastSnapshot(dbSess, project)
		return mergeResult, nil
	}

	slog.Info("merge completed", "session_id", sessionID, "branch", branch, "commit", hash)

	if err := g.queries.SetWorktreeMerged(ctx, sessionID); err != nil {
		slog.Warn("persist worktree merged failed", "session_id", sessionID, "error", err)
	}
	if err := g.queries.SetSessionCompleted(ctx, sessionID); err != nil {
		slog.Warn("persist session completed on merge failed", "session_id", sessionID, "error", err)
	}
	if live != nil {
		live.MarkMerged()
		live.MarkCompleted()
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
	}

	if live != nil {
		unlockState := StateIdle
		if cleanup {
			unlockState = StateStopped
		}
		_ = live.UnlockGitOp(unlockState)
	}
	g.broadcastSnapshot(dbSess, project)

	go func() {
		g.broadcastSiblingGitStatus(ctx, dbSess.ProjectID, sessionID, project.Path)
		g.broadcastProjectGitStatus(dbSess.ProjectID, project.Path)
	}()

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
			_ = live.UnlockGitOp(StateIdle) // safety net for early returns
		}()
	}

	if err := autoCommit(ctx, g.runner, sessionID, dbSess.Name, wtPath); err != nil {
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
			if live != nil {
				_ = live.UnlockGitOp(StateIdle)
			}
			g.broadcastSnapshot(dbSess, project)
			return RebaseResult{Status: "conflict", ConflictFiles: files}, nil
		}
		slog.Error("rebase failed", "session_id", sessionID, "branch", branch, "error", rebaseErr)
		if live != nil {
			_ = live.UnlockGitOp(StateIdle)
		}
		g.broadcastSnapshot(dbSess, project)
		return RebaseResult{Status: "error", Error: rebaseErr.Error()}, nil
	}

	if err := g.queries.UpdateWorktreeBaseSHA(ctx, store.UpdateWorktreeBaseSHAParams{
		WorktreeBaseSha: sql.NullString{String: mainHead, Valid: true},
		ID:              sessionID,
	}); err != nil {
		slog.Warn("persist worktree base SHA failed", "session_id", sessionID, "error", err)
	}

	slog.Info("rebase completed", "session_id", sessionID, "branch", branch, "newBase", mainHead)

	if live != nil {
		_ = live.UnlockGitOp(StateIdle)
	}
	g.broadcastSnapshot(dbSess, project)

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

	if live := g.mgr.Get(p.SessionID); live != nil {
		if err := live.TryLockForGitOp("creating_pr"); err != nil {
			return CreatePRResult{}, err
		}
		defer func() {
			_ = live.UnlockGitOp(StateIdle)
		}()
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
	if err := autoCommit(ctx, g.runner, p.SessionID, dbSess.Name, wtPath); err != nil {
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

// UncommittedDiff returns the diff of uncommitted changes (working tree vs HEAD).
func (g *GitService) UncommittedDiff(ctx context.Context, sessionID string) (gitops.DiffResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return gitops.DiffResult{}, fmt.Errorf("session not found")
	}

	noDiff := gitops.DiffResult{HasDiff: false, Files: []gitops.DiffStat{}}

	dir := dbSess.WorkDir
	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		dir = wtPath
	}

	if _, statErr := os.Stat(dir); statErr != nil {
		return noDiff, nil
	}

	return gitops.WorktreeDiff(dir, "HEAD")
}

// Commit stages all changes and commits in the session's work directory.
// For non-worktree (local) sessions, transitions to StateDone after a successful commit.
func (g *GitService) Commit(ctx context.Context, sessionID, message string) (CommitResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return CommitResult{}, fmt.Errorf("session not found")
	}

	isWorktree := nullStr(dbSess.WorktreePath) != ""

	live := g.mgr.Get(sessionID)
	if live != nil {
		if err := live.TryLockForGitOp("committing"); err != nil {
			return CommitResult{}, err
		}
		defer func() {
			unlockState := StateIdle
			if !isWorktree {
				unlockState = StateDone
			}
			_ = live.UnlockGitOp(unlockState) // safety net for early returns
		}()
	}

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

	project, projErr := g.queries.GetProject(ctx, dbSess.ProjectID)

	if !isWorktree {
		// Local sessions are done after commit — their changes are on the main branch.
		if err := g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
			State: string(StateDone),
			ID:    sessionID,
		}); err != nil {
			slog.Warn("persist session state after commit failed", "session_id", sessionID, "error", err)
		}
		if err := g.queries.SetSessionCompleted(ctx, sessionID); err != nil {
			slog.Warn("persist session completed after commit failed", "session_id", sessionID, "error", err)
		}
		if projErr == nil {
			go g.broadcastProjectGitStatus(dbSess.ProjectID, project.Path)
		}
	}

	if projErr == nil {
		if live != nil {
			unlockState := StateIdle
			if !isWorktree {
				unlockState = StateDone
			}
			_ = live.UnlockGitOp(unlockState)
		}
		g.broadcastSnapshot(dbSess, project)
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

	return msggen.PRDescription(ctx, g.runner, dbSess.Name, diff.Summary, diff.Diff)
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

	return msggen.CommitMsg(ctx, g.runner, dbSess.Name, diff.Summary, diff.Diff)
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

	live := g.mgr.Get(sessionID)
	if live != nil {
		if err := live.TryLockForGitOp("cleaning"); err != nil {
			return CleanResult{}, err
		}
		defer func() {
			_ = live.UnlockGitOp(StateStopped) // safety net for early returns
		}()
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
	if live != nil {
		_ = live.UnlockGitOp(StateStopped)
	}
	g.broadcastSnapshot(dbSess, project)

	slog.Info("clean completed", "session_id", sessionID, "branch", branch)
	return CleanResult{Status: "cleaned"}, nil
}

// GitSnapshot is a complete, versioned snapshot of a session's git state.
// Every session.state broadcast uses this struct — no partial payloads.
// The Version field is monotonically increasing per session; the frontend
// discards any snapshot with a version lower than what it already has.
type GitSnapshot struct {
	SessionID          string   `json:"sessionId"`
	State              string   `json:"state"`
	Connected          bool     `json:"connected"`
	HasDirtyWorktree   bool     `json:"hasDirtyWorktree"`
	HasUncommitted     bool     `json:"hasUncommitted"`
	WorktreeMerged     bool     `json:"worktreeMerged"`
	CompletedAt        string   `json:"completedAt,omitempty"`
	CommitsAhead       int      `json:"commitsAhead"`
	CommitsBehind      int      `json:"commitsBehind"`
	BranchMissing      bool     `json:"branchMissing"`
	MergeStatus        string   `json:"mergeStatus,omitempty"`
	MergeConflictFiles []string `json:"mergeConflictFiles,omitempty"`
	GitOperation       string   `json:"gitOperation,omitempty"`
	Version            int64    `json:"version"`
}

// RefreshGitStatus recomputes, broadcasts, and returns git status for a session.
func (g *GitService) RefreshGitStatus(ctx context.Context, sessionID string) (GitSnapshot, error) {
	snap, err := g.computeGitSnapshot(ctx, sessionID)
	if err != nil {
		return GitSnapshot{}, err
	}
	dbSess, _ := g.queries.GetSession(ctx, sessionID)
	g.hub.Broadcast(dbSess.ProjectID, "session.state", snap)
	return snap, nil
}

// broadcastProjectGitStatus computes and broadcasts the project-level git status.
// Called after operations that change the main branch (merge, commit).
func (g *GitService) broadcastProjectGitStatus(projectID, projectPath string) {
	status := map[string]any{"projectId": projectID}

	branch, err := gitops.CurrentBranch(projectPath)
	if err != nil {
		return // not a git repo
	}
	status["branch"] = branch

	if files, err := gitops.UncommittedFiles(projectPath); err == nil {
		status["uncommittedCount"] = len(files)
	}

	hasRemote, err := gitops.HasRemote(projectPath, "origin")
	if err != nil || !hasRemote {
		status["hasRemote"] = false
		g.hub.Broadcast(projectID, "project.git-status", status)
		return
	}
	status["hasRemote"] = true

	if ahead, behind, err := gitops.AheadBehindRemote(projectPath); err == nil {
		status["aheadRemote"] = ahead
		status["behindRemote"] = behind
	}

	g.hub.Broadcast(projectID, "project.git-status", status)
}

// broadcastSiblingGitStatus recomputes and broadcasts git status for all worktree sessions
// in the same project except excludeID. Called after operations that advance the main branch.
func (g *GitService) broadcastSiblingGitStatus(ctx context.Context, projectID, excludeID, _ string) {
	sessions, err := g.queries.ListSessionsByProject(ctx, projectID)
	if err != nil {
		return
	}
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return
	}
	for _, ss := range sessions {
		if ss.ID == excludeID || ss.WorktreeMerged != 0 {
			continue
		}
		if nullStr(ss.WorktreeBranch) == "" {
			continue
		}
		g.broadcastSnapshot(ss, project)
	}
}
