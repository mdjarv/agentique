package project

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/allbin/agentique/backend/internal/gitops"
	"github.com/allbin/agentique/backend/internal/msggen"
	"github.com/allbin/agentique/backend/internal/store"
)

// Broadcaster sends push messages to all WebSocket clients for a project.
type Broadcaster interface {
	Broadcast(projectID, pushType string, payload any)
}

type gitQueries interface {
	GetProject(ctx context.Context, id string) (store.Project, error)
}

// ProjectGitStatus is the project-level git status sent to clients.
type ProjectGitStatus struct {
	ProjectID        string `json:"projectId"`
	Branch           string `json:"branch"`
	HasRemote        bool   `json:"hasRemote"`
	AheadRemote      int    `json:"aheadRemote"`
	BehindRemote     int    `json:"behindRemote"`
	UncommittedCount int    `json:"uncommittedCount"`
}

// CommitResult describes the outcome of a project-level commit.
type CommitResult struct {
	CommitHash string `json:"commitHash"`
}

// GitService handles git operations at the project level.
type GitService struct {
	queries gitQueries
	hub     Broadcaster
	git     projectGitOps
	runner  msggen.Runner
}

// NewGitService creates a new project-level GitService.
func NewGitService(queries gitQueries, hub Broadcaster, git projectGitOps, runner msggen.Runner) *GitService {
	return &GitService{queries: queries, hub: hub, git: git, runner: runner}
}

// getProject loads a project by ID, wrapping the error with a standard message.
func (g *GitService) getProject(ctx context.Context, projectID string) (store.Project, error) {
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return store.Project{}, fmt.Errorf("project not found")
	}
	return project, nil
}

// Status computes the git status for a project's root directory.
func (g *GitService) Status(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, err
	}
	return g.computeStatus(projectID, project.Path), nil
}

func (g *GitService) computeStatus(projectID, projectPath string) ProjectGitStatus {
	s := g.git.ProjectStatus(projectPath)
	return ProjectGitStatus{
		ProjectID:        projectID,
		Branch:           s.Branch,
		HasRemote:        s.HasRemote,
		AheadRemote:      s.AheadRemote,
		BehindRemote:     s.BehindRemote,
		UncommittedCount: s.UncommittedCount,
	}
}

// Fetch runs git fetch and returns the updated status.
func (g *GitService) Fetch(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, err
	}

	if err := g.git.Fetch(project.Path); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("fetch failed: %w", err)
	}

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// Push pushes the current branch to origin and returns the updated status.
func (g *GitService) Push(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, err
	}

	branch, err := g.git.CurrentBranch(project.Path)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("failed to get current branch: %w", err)
	}

	if err := g.git.PushBranch(project.Path, branch); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("push failed: %w", err)
	}

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// Commit stages all changes and commits in the project root.
func (g *GitService) Commit(ctx context.Context, projectID, message string) (CommitResult, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return CommitResult{}, err
	}

	dirty, err := g.git.HasUncommittedChanges(project.Path)
	if err != nil {
		return CommitResult{}, fmt.Errorf("failed to check changes: %w", err)
	}
	if !dirty {
		return CommitResult{}, fmt.Errorf("no uncommitted changes")
	}

	if err := g.git.AutoCommitAll(project.Path, message); err != nil {
		return CommitResult{}, fmt.Errorf("commit failed: %w", err)
	}

	hash, err := g.git.HeadCommitHash(project.Path)
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit succeeded but failed to get hash: %w", err)
	}

	slog.Info("project commit", "project_id", projectID, "commit", hash)

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)

	return CommitResult{CommitHash: hash}, nil
}

// GenerateCommitMessage uses Haiku to generate a commit message from the project's uncommitted diff.
func (g *GitService) GenerateCommitMessage(ctx context.Context, projectID string) (msggen.CommitMessageResult, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return msggen.CommitMessageResult{}, err
	}

	diff, summary, err := gitops.UncommittedDiff(project.Path)
	if err != nil {
		return msggen.CommitMessageResult{}, fmt.Errorf("failed to get diff: %w", err)
	}
	if diff == "" && summary == "" {
		return msggen.CommitMessageResult{}, fmt.Errorf("no uncommitted changes")
	}

	return msggen.CommitMsg(ctx, g.runner, project.Name, summary, diff)
}

// TrackedFilesResult contains the list of git-tracked files.
type TrackedFilesResult struct {
	Files []string `json:"files"`
}

// TrackedFiles returns all git-tracked files for a project.
func (g *GitService) TrackedFiles(ctx context.Context, projectID string) (TrackedFilesResult, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return TrackedFilesResult{}, err
	}
	files, err := g.git.ListTrackedFiles(project.Path)
	if err != nil {
		return TrackedFilesResult{}, fmt.Errorf("list tracked files: %w", err)
	}
	return TrackedFilesResult{Files: files}, nil
}

// CommandsResult contains the list of custom slash commands.
type CommandsResult struct {
	Commands []gitops.CommandFile `json:"commands"`
}

// Commands returns custom slash commands from .claude/commands/ dirs.
func (g *GitService) Commands(ctx context.Context, projectID string) (CommandsResult, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return CommandsResult{}, err
	}
	cmds, err := g.git.ListCommandFiles(project.Path)
	if err != nil {
		return CommandsResult{}, fmt.Errorf("list commands: %w", err)
	}
	return CommandsResult{Commands: cmds}, nil
}

// BranchListResult contains local and remote-only branch names.
type BranchListResult struct {
	Local  []string `json:"local"`
	Remote []string `json:"remote"`
}

// ListBranches returns local and remote-only branch names for a project.
func (g *GitService) ListBranches(ctx context.Context, projectID string) (BranchListResult, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return BranchListResult{}, err
	}
	local, remote, err := g.git.ListBranches(project.Path)
	if err != nil {
		return BranchListResult{}, fmt.Errorf("list branches: %w", err)
	}
	return BranchListResult{Local: local, Remote: remote}, nil
}

// Checkout switches to the given branch in the project root.
// Refuses if there are uncommitted changes.
func (g *GitService) Checkout(ctx context.Context, projectID, branch string) (ProjectGitStatus, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, err
	}

	dirty, err := g.git.HasUncommittedChanges(project.Path)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("failed to check changes: %w", err)
	}
	if dirty {
		return ProjectGitStatus{}, fmt.Errorf("cannot switch branches: uncommitted changes exist")
	}

	if err := g.git.CheckoutBranch(project.Path, branch); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("checkout failed: %w", err)
	}

	slog.Info("project checkout", "project_id", projectID, "branch", branch)

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// Pull fetches from remote and fast-forward merges the upstream tracking branch.
func (g *GitService) Pull(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, err
	}

	if err := g.git.Fetch(project.Path); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("fetch failed: %w", err)
	}

	upstream, err := g.git.UpstreamRef(project.Path)
	if err != nil || upstream == "" {
		return ProjectGitStatus{}, fmt.Errorf("no upstream tracking branch configured")
	}

	if _, err := g.git.MergeBranch(project.Path, upstream); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("pull failed (not fast-forwardable?): %w", err)
	}

	slog.Info("project pull", "project_id", projectID, "upstream", upstream)

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// UncommittedFilesResult describes the uncommitted files in a project's root directory.
type UncommittedFilesResult struct {
	Files []gitops.FileStatus `json:"files"`
}

// UncommittedFiles returns the list of uncommitted files for a project.
func (g *GitService) UncommittedFiles(ctx context.Context, projectID string) (UncommittedFilesResult, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return UncommittedFilesResult{}, err
	}
	files, err := g.git.UncommittedFiles(project.Path)
	if err != nil {
		return UncommittedFilesResult{}, fmt.Errorf("failed to get uncommitted files: %w", err)
	}
	if files == nil {
		files = []gitops.FileStatus{}
	}
	return UncommittedFilesResult{Files: files}, nil
}

// DiscardChanges discards all uncommitted changes in the project root.
func (g *GitService) DiscardChanges(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, err
	}

	if err := g.git.DiscardAll(project.Path); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("discard failed: %w", err)
	}

	slog.Info("project discard", "project_id", projectID)

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// BroadcastStatus computes and broadcasts the project git status.
// Safe to call from goroutines.
func (g *GitService) BroadcastStatus(ctx context.Context, projectID string) {
	project, err := g.getProject(ctx, projectID)
	if err != nil {
		return
	}
	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
}

// IsGitRepo returns true if the given path is inside a git repository.
func IsGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}
