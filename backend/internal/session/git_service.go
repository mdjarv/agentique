package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/msggen"
	"github.com/mdjarv/agentique/backend/internal/store"
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
func autoCommit(ctx context.Context, git sessionGitOps, runner msggen.Runner, sessionID, sessionName, wtPath string) error {
	if wtPath == "" {
		return nil
	}
	dirty, err := git.HasUncommittedChanges(wtPath)
	if err != nil {
		slog.Warn("failed to check worktree changes", "session_id", sessionID, "error", err)
		return nil
	}
	if !dirty {
		return nil
	}

	msg := msggen.AutoCommitMsg(ctx, runner, sessionName, wtPath)
	return git.AutoCommitAll(wtPath, msg)
}

// GitService handles git operations (merge, PR, diff, commit) for sessions.
type GitService struct {
	mgr         *Manager
	queries     gitServiceQueries
	hub         Broadcaster
	runner      msggen.Runner
	git         sessionGitOps
	gitVersions sync.Map // sessionID → *atomic.Int64
}

// NewGitService creates a new GitService.
func NewGitService(mgr *Manager, queries gitServiceQueries, hub Broadcaster, runner msggen.Runner) *GitService {
	return &GitService{mgr: mgr, queries: queries, hub: hub, runner: runner, git: RealSessionGitOps()}
}

// SetGitOps overrides the default git operations (for testing).
func (g *GitService) SetGitOps(ops sessionGitOps) { g.git = ops }

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

	if !snap.WorktreeMerged {
		branch := nullStr(dbSess.WorktreeBranch)
		wtPath := nullStr(dbSess.WorktreePath)
		if branch != "" {
			// Worktree session: full branch status (ahead/behind, merge, uncommitted).
			bs := computeBranchStatus(g.mgr.gitStatus, project.Path, branch, wtPath)
			snap.BranchMissing = bs.BranchMissing
			snap.CommitsAhead = bs.CommitsAhead
			snap.CommitsBehind = bs.CommitsBehind
			snap.HasDirtyWorktree = bs.HasUncommitted
			snap.HasUncommitted = bs.HasUncommitted
			snap.MergeStatus = bs.MergeStatus
			snap.MergeConflictFiles = bs.MergeConflictFiles
		} else if dbSess.WorkDir != "" {
			// Local (non-worktree) session: only check uncommitted changes.
			if dirty, err := g.mgr.gitStatus.HasUncommittedChanges(dbSess.WorkDir); err == nil {
				snap.HasUncommitted = dirty
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

// Merge mode constants.
const (
	MergeModeMerge    = "merge"    // merge only, session stays active
	MergeModeComplete = "complete" // merge + mark completed
	MergeModeDelete   = "delete"   // merge + mark completed + cleanup worktree/branch
)

// Merge merges a worktree session's branch into the project's main branch.
func (g *GitService) Merge(ctx context.Context, sessionID string, mode string) (MergeResult, error) {
	switch mode {
	case MergeModeMerge, MergeModeComplete, MergeModeDelete:
	default:
		return MergeResult{}, fmt.Errorf("invalid merge mode: %q", mode)
	}

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

	slog.Info("merge started", "session_id", sessionID, "branch", branch, "mode", mode)

	live := g.mgr.Get(sessionID)
	guard, err := tryLockForGitOp(live, "merging", StateIdle)
	if err != nil {
		return MergeResult{}, err
	}
	defer guard.Ensure()

	wtPath := nullStr(dbSess.WorktreePath)
	if err := autoCommit(ctx, g.git, g.runner, sessionID, dbSess.Name, wtPath); err != nil {
		return MergeResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	if live != nil {
		live.broadcastState(StateMerging)
	}

	// Refuse to merge if the project root has uncommitted changes.
	dirty, err := g.git.HasUncommittedChanges(project.Path)
	if err != nil {
		slog.Warn("failed to check project root for uncommitted changes", "session_id", sessionID, "error", err)
	}
	if dirty {
		guard.Release(StateIdle)
		return MergeResult{Status: "dirty_worktree", Error: gitops.ErrDirtyWorktree.Error()}, nil
	}

	hash, mergeErr := g.git.MergeBranch(project.Path, branch)
	if mergeErr != nil {
		if errors.Is(mergeErr, gitops.ErrNotFastForward) {
			slog.Info("merge needs rebase", "session_id", sessionID, "branch", branch)
			guard.Release(StateIdle)
			g.broadcastSnapshot(dbSess, project)
			return MergeResult{Status: "needs_rebase"}, nil
		}
		files, _ := g.git.MergeConflictFiles(project.Path)
		_ = g.git.AbortMerge(project.Path)
		guard.Release(StateIdle)
		if len(files) > 0 {
			slog.Warn("merge conflict", "session_id", sessionID, "branch", branch, "conflict_files", len(files))
			g.broadcastSnapshot(dbSess, project)
			return MergeResult{Status: "conflict", ConflictFiles: files}, nil
		}
		slog.Error("merge failed", "session_id", sessionID, "branch", branch, "error", mergeErr)
		g.broadcastSnapshot(dbSess, project)
		return MergeResult{Status: "error", Error: mergeErr.Error()}, nil
	}

	slog.Info("merge completed", "session_id", sessionID, "branch", branch, "commit", hash)

	g.finalizeMerge(ctx, mode, live, sessionID, project, branch, wtPath)

	var unlockState State
	switch mode {
	case MergeModeMerge:
		unlockState = StateIdle
	case MergeModeComplete:
		unlockState = StateDone
	case MergeModeDelete:
		unlockState = StateStopped
	}
	guard.Release(unlockState)
	g.broadcastSnapshot(dbSess, project)

	go func() {
		g.broadcastSiblingGitStatus(context.Background(), dbSess.ProjectID, sessionID, project.Path)
		g.broadcastProjectGitStatus(dbSess.ProjectID, project.Path)
	}()

	return MergeResult{Status: "merged", CommitHash: hash}, nil
}

// finalizeMerge handles mode-specific post-merge steps (mark completed, cleanup worktree).
func (g *GitService) finalizeMerge(ctx context.Context, mode string, live *Session, sessionID string, project store.Project, branch, wtPath string) {
	if mode == MergeModeComplete || mode == MergeModeDelete {
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
	}

	if mode == MergeModeDelete {
		if wtPath != "" {
			g.git.RemoveWorktree(project.Path, wtPath)
		}
		if delErr := g.git.DeleteBranch(project.Path, branch); delErr != nil {
			slog.Warn("branch delete after merge failed", "session_id", sessionID, "error", delErr)
		}
		g.git.DeleteRemoteBranch(project.Path, branch)
		go g.git.GC(project.Path)
		if err := g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
			State: string(StateStopped),
			ID:    sessionID,
		}); err != nil {
			slog.Warn("persist session state after merge cleanup failed", "session_id", sessionID, "error", err)
		}
	}
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
	guard, err := tryLockForGitOp(live, "rebasing", StateIdle)
	if err != nil {
		return RebaseResult{}, err
	}
	defer guard.Ensure()

	if err := autoCommit(ctx, g.git, g.runner, sessionID, dbSess.Name, wtPath); err != nil {
		return RebaseResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	mainHead, err := g.git.HeadCommitHash(project.Path)
	if err != nil {
		return RebaseResult{Status: "error", Error: "failed to get main HEAD: " + err.Error()}, nil
	}

	if live != nil {
		live.broadcastState(StateMerging)
	}

	if rebaseErr := g.git.RebaseBranch(wtPath, mainHead); rebaseErr != nil {
		files, _ := g.git.RebaseConflictFiles(wtPath)
		_ = g.git.AbortRebase(wtPath)
		guard.Release(StateIdle)
		if len(files) > 0 {
			slog.Warn("rebase conflict", "session_id", sessionID, "branch", branch, "conflict_files", len(files))
			g.broadcastSnapshot(dbSess, project)
			return RebaseResult{Status: "conflict", ConflictFiles: files}, nil
		}
		slog.Error("rebase failed", "session_id", sessionID, "branch", branch, "error", rebaseErr)
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

	guard.Release(StateIdle)
	g.broadcastSnapshot(dbSess, project)

	go g.broadcastSiblingGitStatus(context.Background(), dbSess.ProjectID, sessionID, project.Path)

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

	live := g.mgr.Get(p.SessionID)
	guard, err := tryLockForGitOp(live, "creating_pr", StateIdle)
	if err != nil {
		return CreatePRResult{}, err
	}
	defer guard.Ensure()

	slog.Info("creating PR", "session_id", p.SessionID, "branch", branch)

	if !g.git.HasGhCli() {
		return CreatePRResult{Status: "error", Error: "gh CLI not installed"}, nil
	}

	hasOrigin, err := g.git.HasRemote(project.Path, "origin")
	if err != nil {
		return CreatePRResult{Status: "error", Error: err.Error()}, nil
	}
	if !hasOrigin {
		return CreatePRResult{Status: "error", Error: "no origin remote configured"}, nil
	}

	if live != nil {
		live.broadcastState(StateMerging)
	}

	wtPath := nullStr(dbSess.WorktreePath)
	if err := autoCommit(ctx, g.git, g.runner, p.SessionID, dbSess.Name, wtPath); err != nil {
		return CreatePRResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	if url, prErr := g.git.GetExistingPR(project.Path, branch); prErr == nil && url != "" {
		slog.Info("PR already exists", "session_id", p.SessionID, "url", url)
		g.savePRUrl(ctx, dbSess.ProjectID, p.SessionID, url)
		return CreatePRResult{Status: "existing", URL: url}, nil
	}

	if err := g.git.PushBranch(project.Path, branch); err != nil {
		return CreatePRResult{Status: "error", Error: err.Error()}, nil
	}

	title := p.Title
	if title == "" {
		title = dbSess.Name
	}

	url, err := g.git.CreatePR(project.Path, branch, title, p.Body)
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
	g.hub.Broadcast(projectID, "session.pr-updated", PushPRUpdated{SessionID: sessionID, PrUrl: url})
}

// PRStatusResult describes the outcome of a PR status check.
type PRStatusResult struct {
	Number       int    `json:"number"`
	State        string `json:"state"`        // OPEN, MERGED, CLOSED
	IsDraft      bool   `json:"isDraft"`
	ChecksStatus string `json:"checksStatus"` // pass, fail, pending, none
}

// PRStatus fetches PR metadata for a session's branch.
func (g *GitService) PRStatus(ctx context.Context, sessionID string) (PRStatusResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return PRStatusResult{}, fmt.Errorf("session not found")
	}
	project, err := g.queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return PRStatusResult{}, fmt.Errorf("project not found")
	}

	if !dbSess.WorktreeBranch.Valid || dbSess.WorktreeBranch.String == "" {
		return PRStatusResult{}, fmt.Errorf("session has no worktree branch")
	}

	raw, err := g.git.PRStatus(project.Path, dbSess.WorktreeBranch.String)
	if err != nil {
		return PRStatusResult{}, err
	}

	return PRStatusResult{
		Number:       raw.Number,
		State:        raw.State,
		IsDraft:      raw.IsDraft,
		ChecksStatus: raw.ChecksStatus,
	}, nil
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
		return g.git.WorktreeDiff(wtPath, nullStr(dbSess.WorktreeBaseSha), false)
	}

	// Local session: diff work dir against HEAD (include untracked files).
	workDir := dbSess.WorkDir
	if _, statErr := os.Stat(workDir); statErr != nil {
		return noDiff, nil
	}
	return g.git.WorktreeDiff(workDir, "HEAD", true)
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

	return g.git.WorktreeDiff(dir, "HEAD", true)
}

// Commit stages all changes and commits in the session's work directory.
// For non-worktree (local) sessions, transitions to StateDone after a successful commit.
func (g *GitService) Commit(ctx context.Context, sessionID, message string) (CommitResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return CommitResult{}, fmt.Errorf("session not found")
	}

	isWorktree := nullStr(dbSess.WorktreePath) != ""

	targetState := StateIdle
	if !isWorktree {
		targetState = StateDone
	}

	live := g.mgr.Get(sessionID)
	guard, err := tryLockForGitOp(live, "committing", targetState)
	if err != nil {
		return CommitResult{}, err
	}
	defer guard.Ensure()

	// Use worktree path if available, otherwise work dir.
	dir := dbSess.WorkDir
	if isWorktree {
		dir = nullStr(dbSess.WorktreePath)
	}

	if _, statErr := os.Stat(dir); statErr != nil {
		return CommitResult{}, fmt.Errorf("work directory not found")
	}

	dirty, err := g.git.HasUncommittedChanges(dir)
	if err != nil {
		return CommitResult{}, fmt.Errorf("failed to check changes: %w", err)
	}
	if !dirty {
		return CommitResult{}, fmt.Errorf("no uncommitted changes")
	}

	if live != nil {
		live.broadcastState(StateMerging)
	}

	if err := g.git.AutoCommitAll(dir, message); err != nil {
		return CommitResult{}, fmt.Errorf("commit failed: %w", err)
	}

	hash, err := g.git.HeadCommitHash(dir)
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
		guard.Release(targetState)
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

	files, err := g.git.UncommittedFiles(dir)
	if err != nil {
		return UncommittedFilesResult{}, fmt.Errorf("failed to get uncommitted files: %w", err)
	}
	if files == nil {
		files = []gitops.FileStatus{}
	}
	return UncommittedFilesResult{Files: files}, nil
}

// CommitLogResult describes the commits a session's branch has ahead of main.
type CommitLogResult struct {
	Commits []gitops.CommitLogEntry `json:"commits"`
}

// CommitLog returns the commit log for a session's branch (commits ahead of main).
func (g *GitService) CommitLog(ctx context.Context, sessionID string) (CommitLogResult, error) {
	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return CommitLogResult{}, fmt.Errorf("session not found")
	}
	project, err := g.queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return CommitLogResult{}, fmt.Errorf("project not found")
	}
	branch := nullStr(dbSess.WorktreeBranch)
	if branch == "" {
		return CommitLogResult{Commits: []gitops.CommitLogEntry{}}, nil
	}
	entries, err := g.git.CommitLog(project.Path, branch, 50)
	if err != nil {
		return CommitLogResult{}, fmt.Errorf("failed to get commit log: %w", err)
	}
	return CommitLogResult{Commits: entries}, nil
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
	guard, err := tryLockForGitOp(live, "cleaning", StateStopped)
	if err != nil {
		return CleanResult{}, err
	}
	defer guard.Ensure()

	slog.Info("clean started", "session_id", sessionID, "branch", branch)

	if live != nil {
		live.broadcastState(StateMerging)
	}

	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		g.git.RemoveWorktree(project.Path, wtPath)
	}
	if g.git.BranchExists(project.Path, branch) {
		if delErr := g.git.DeleteBranch(project.Path, branch); delErr != nil {
			slog.Warn("branch delete during clean failed", "session_id", sessionID, "error", delErr)
		}
	}
	g.git.DeleteRemoteBranch(project.Path, branch)
	go g.git.GC(project.Path)

	if err := g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: string(StateStopped),
		ID:    sessionID,
	}); err != nil {
		slog.Warn("persist session state after clean failed", "session_id", sessionID, "error", err)
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
	s := g.git.ProjectStatus(projectPath)
	if s.Branch == "" {
		return // not a git repo
	}
	g.hub.Broadcast(projectID, "project.git-status", PushProjectGitStatus{
		ProjectID:        projectID,
		Branch:           s.Branch,
		UncommittedCount: s.UncommittedCount,
		HasRemote:        s.HasRemote,
		AheadRemote:      s.AheadRemote,
		BehindRemote:     s.BehindRemote,
	})
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
