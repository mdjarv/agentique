package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"

	claudecli "github.com/allbin/claudecli-go"

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

// haikuCommitMsg calls Haiku to generate a commit message from a diff.
func haikuCommitMsg(ctx context.Context, sessionName, summary, diff string) (CommitMessageResult, error) {
	diffText := diff
	const maxDiffChars = 8000
	if len(diffText) > maxDiffChars {
		diffText = diffText[:maxDiffChars] + "\n... (truncated)"
	}

	prompt := fmt.Sprintf(
		"Generate a git commit message for these changes.\n"+
			"Session name: %s\n\n"+
			"Diff summary:\n%s\n\n"+
			"Full diff:\n%s\n\n"+
			"Respond in EXACTLY this format with no other text:\n"+
			"TITLE: <imperative mood, max 72 chars, no period>\n"+
			"DESCRIPTION:\n<optional longer explanation, 1-4 lines, explain why not what>",
		sessionName, summary, diffText,
	)

	client := claudecli.New()
	result, err := client.RunBlocking(ctx, prompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
	)
	if err != nil {
		return CommitMessageResult{}, fmt.Errorf("haiku generation failed: %w", err)
	}

	return parseCommitMessage(result.Text), nil
}

// generateAutoCommitMsg generates a commit message for uncommitted changes via Haiku,
// falling back to a session-name-based message on failure.
func generateAutoCommitMsg(ctx context.Context, sessionName, wtPath string) string {
	fallback := sessionName + ": save changes"

	diff, summary, err := gitops.UncommittedDiff(wtPath)
	if err != nil || (diff == "" && summary == "") {
		return fallback
	}

	result, err := haikuCommitMsg(ctx, sessionName, summary, diff)
	if err != nil || result.Title == "" {
		slog.Warn("haiku commit msg failed, using fallback", "error", err)
		return fallback
	}

	if result.Description != "" {
		return result.Title + "\n\n" + result.Description
	}
	return result.Title
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

	msg := generateAutoCommitMsg(ctx, sessionName, wtPath)
	return gitops.AutoCommitAll(wtPath, msg)
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
	if err := autoCommit(ctx, sessionID, dbSess.Name, wtPath); err != nil {
		return MergeResult{Status: "error", Error: "failed to commit worktree changes: " + err.Error()}, nil
	}

	hash, mergeErr := gitops.MergeBranch(project.Path, branch, "Merge: "+dbSess.Name)
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
	go gitops.AutoGC(project.Path)

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

	if err := autoCommit(ctx, sessionID, dbSess.Name, wtPath); err != nil {
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
	_ = g.queries.UpdateSessionPRUrl(ctx, store.UpdateSessionPRUrlParams{
		PrUrl: url,
		ID:    sessionID,
	})
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

	// Local sessions are done after commit — their changes are on the main branch.
	if !isWorktree {
		_ = g.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
			State: string(StateDone),
			ID:    sessionID,
		})
		g.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
			"sessionId": sessionID,
			"state":     string(StateDone),
		})
	}

	return CommitResult{CommitHash: hash}, nil
}

// PRDescriptionResult holds generated PR title and body.
type PRDescriptionResult struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// GeneratePRDescription uses Haiku to generate a PR title and body from the session diff.
func (g *GitService) GeneratePRDescription(ctx context.Context, sessionID string) (PRDescriptionResult, error) {
	diff, err := g.Diff(ctx, sessionID)
	if err != nil {
		return PRDescriptionResult{}, fmt.Errorf("failed to get diff: %w", err)
	}
	if !diff.HasDiff {
		return PRDescriptionResult{}, fmt.Errorf("no changes to describe")
	}

	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return PRDescriptionResult{}, fmt.Errorf("session not found")
	}

	// Build context: stat summary + truncated diff for Haiku.
	diffText := diff.Diff
	const maxDiffChars = 8000
	if len(diffText) > maxDiffChars {
		diffText = diffText[:maxDiffChars] + "\n... (truncated)"
	}

	prompt := fmt.Sprintf(
		"Generate a GitHub pull request title and description for these changes.\n"+
			"Session name: %s\n\n"+
			"Diff summary:\n%s\n\n"+
			"Full diff:\n%s\n\n"+
			"Respond in EXACTLY this format with no other text:\n"+
			"TITLE: <short PR title, max 70 chars>\n"+
			"BODY:\n<markdown description: what changed and why, use bullet points, 2-8 lines>",
		dbSess.Name, diff.Summary, diffText,
	)

	client := claudecli.New()
	result, err := client.RunBlocking(ctx, prompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
	)
	if err != nil {
		return PRDescriptionResult{}, fmt.Errorf("haiku generation failed: %w", err)
	}

	return parsePRDescription(result.Text), nil
}

// CommitMessageResult holds generated commit title and description.
type CommitMessageResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// GenerateCommitMessage uses Haiku to generate a commit title and description from the session diff.
func (g *GitService) GenerateCommitMessage(ctx context.Context, sessionID string) (CommitMessageResult, error) {
	diff, err := g.Diff(ctx, sessionID)
	if err != nil {
		return CommitMessageResult{}, fmt.Errorf("failed to get diff: %w", err)
	}
	if !diff.HasDiff {
		return CommitMessageResult{}, fmt.Errorf("no changes to describe")
	}

	dbSess, err := g.queries.GetSession(ctx, sessionID)
	if err != nil {
		return CommitMessageResult{}, fmt.Errorf("session not found")
	}

	return haikuCommitMsg(ctx, dbSess.Name, diff.Summary, diff.Diff)
}

// parseCommitMessage extracts title and description from Haiku's "TITLE: ...\nDESCRIPTION:\n..." response.
func parseCommitMessage(text string) CommitMessageResult {
	text = strings.TrimSpace(text)

	titleIdx := strings.Index(text, "TITLE:")
	descIdx := strings.Index(text, "DESCRIPTION:")

	var title, desc string
	if titleIdx >= 0 && descIdx > titleIdx {
		title = strings.TrimSpace(text[titleIdx+len("TITLE:") : descIdx])
		desc = strings.TrimSpace(text[descIdx+len("DESCRIPTION:"):])
	} else if titleIdx >= 0 {
		title = strings.TrimSpace(text[titleIdx+len("TITLE:"):])
	} else {
		lines := strings.SplitN(text, "\n", 2)
		title = strings.TrimSpace(lines[0])
		if len(lines) > 1 {
			desc = strings.TrimSpace(lines[1])
		}
	}

	if len(title) > 72 {
		title = title[:72]
	}

	return CommitMessageResult{Title: title, Description: desc}
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
		g.hub.Broadcast(projectID, "session.state", payload)
	}
}

// parsePRDescription extracts title and body from Haiku's "TITLE: ...\nBODY:\n..." response.
func parsePRDescription(text string) PRDescriptionResult {
	text = strings.TrimSpace(text)

	titleIdx := strings.Index(text, "TITLE:")
	bodyIdx := strings.Index(text, "BODY:")

	var title, body string
	if titleIdx >= 0 && bodyIdx > titleIdx {
		title = strings.TrimSpace(text[titleIdx+len("TITLE:") : bodyIdx])
		body = strings.TrimSpace(text[bodyIdx+len("BODY:"):])
	} else if titleIdx >= 0 {
		title = strings.TrimSpace(text[titleIdx+len("TITLE:"):])
	} else {
		// Fallback: first line is title, rest is body.
		lines := strings.SplitN(text, "\n", 2)
		title = strings.TrimSpace(lines[0])
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
	}

	if len(title) > 70 {
		title = title[:70]
	}

	return PRDescriptionResult{Title: title, Body: body}
}
