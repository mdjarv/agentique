package project

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/allbin/agentique/backend/internal/gitops"
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
}

// NewGitService creates a new project-level GitService.
func NewGitService(queries gitQueries, hub Broadcaster) *GitService {
	return &GitService{queries: queries, hub: hub}
}

// Status computes the git status for a project's root directory.
func (g *GitService) Status(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("project not found")
	}
	return g.computeStatus(projectID, project.Path), nil
}

func (g *GitService) computeStatus(projectID, projectPath string) ProjectGitStatus {
	status := ProjectGitStatus{ProjectID: projectID}

	branch, err := gitops.CurrentBranch(projectPath)
	if err != nil {
		// Not a git repo or other error — return empty status.
		return status
	}
	status.Branch = branch

	if files, err := gitops.UncommittedFiles(projectPath); err == nil {
		status.UncommittedCount = len(files)
	}

	hasRemote, err := gitops.HasRemote(projectPath, "origin")
	if err != nil || !hasRemote {
		return status
	}
	status.HasRemote = true

	ahead, behind, err := gitops.AheadBehindRemote(projectPath)
	if err == nil {
		status.AheadRemote = ahead
		status.BehindRemote = behind
	}

	return status
}

// Fetch runs git fetch and returns the updated status.
func (g *GitService) Fetch(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("project not found")
	}

	if err := gitops.Fetch(project.Path); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("fetch failed: %w", err)
	}

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// Push pushes the current branch to origin and returns the updated status.
func (g *GitService) Push(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("project not found")
	}

	branch, err := gitops.CurrentBranch(project.Path)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("failed to get current branch: %w", err)
	}

	if err := gitops.PushBranch(project.Path, branch); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("push failed: %w", err)
	}

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// Commit stages all changes and commits in the project root.
func (g *GitService) Commit(ctx context.Context, projectID, message string) (CommitResult, error) {
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return CommitResult{}, fmt.Errorf("project not found")
	}

	dirty, err := gitops.HasUncommittedChanges(project.Path)
	if err != nil {
		return CommitResult{}, fmt.Errorf("failed to check changes: %w", err)
	}
	if !dirty {
		return CommitResult{}, fmt.Errorf("no uncommitted changes")
	}

	if err := gitops.AutoCommitAll(project.Path, message); err != nil {
		return CommitResult{}, fmt.Errorf("commit failed: %w", err)
	}

	hash, err := gitops.HeadCommitHash(project.Path)
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit succeeded but failed to get hash: %w", err)
	}

	slog.Info("project commit", "project_id", projectID, "commit", hash)

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)

	return CommitResult{CommitHash: hash}, nil
}

// TrackedFilesResult contains the list of git-tracked files.
type TrackedFilesResult struct {
	Files []string `json:"files"`
}

// TrackedFiles returns all git-tracked files for a project.
func (g *GitService) TrackedFiles(ctx context.Context, projectID string) (TrackedFilesResult, error) {
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return TrackedFilesResult{}, fmt.Errorf("project not found")
	}
	files, err := gitops.ListTrackedFiles(project.Path)
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
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return CommandsResult{}, fmt.Errorf("project not found")
	}
	cmds, err := gitops.ListCommandFiles(project.Path)
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
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return BranchListResult{}, fmt.Errorf("project not found")
	}
	local, remote, err := gitops.ListBranches(project.Path)
	if err != nil {
		return BranchListResult{}, fmt.Errorf("list branches: %w", err)
	}
	return BranchListResult{Local: local, Remote: remote}, nil
}

// Checkout switches to the given branch in the project root.
// Refuses if there are uncommitted changes.
func (g *GitService) Checkout(ctx context.Context, projectID, branch string) (ProjectGitStatus, error) {
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("project not found")
	}

	dirty, err := gitops.HasUncommittedChanges(project.Path)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("failed to check changes: %w", err)
	}
	if dirty {
		return ProjectGitStatus{}, fmt.Errorf("cannot switch branches: uncommitted changes exist")
	}

	if err := gitops.CheckoutBranch(project.Path, branch); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("checkout failed: %w", err)
	}

	slog.Info("project checkout", "project_id", projectID, "branch", branch)

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// Pull fetches from remote and fast-forward merges the upstream tracking branch.
func (g *GitService) Pull(ctx context.Context, projectID string) (ProjectGitStatus, error) {
	project, err := g.queries.GetProject(ctx, projectID)
	if err != nil {
		return ProjectGitStatus{}, fmt.Errorf("project not found")
	}

	if err := gitops.Fetch(project.Path); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("fetch failed: %w", err)
	}

	upstream, err := gitops.UpstreamRef(project.Path)
	if err != nil || upstream == "" {
		return ProjectGitStatus{}, fmt.Errorf("no upstream tracking branch configured")
	}

	if _, err := gitops.MergeBranch(project.Path, upstream); err != nil {
		return ProjectGitStatus{}, fmt.Errorf("pull failed (not fast-forwardable?): %w", err)
	}

	slog.Info("project pull", "project_id", projectID, "upstream", upstream)

	status := g.computeStatus(projectID, project.Path)
	g.hub.Broadcast(projectID, "project.git-status", status)
	return status, nil
}

// BroadcastStatus computes and broadcasts the project git status.
// Safe to call from goroutines.
func (g *GitService) BroadcastStatus(ctx context.Context, projectID string) {
	project, err := g.queries.GetProject(ctx, projectID)
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
