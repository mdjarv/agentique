package session

import (
	"context"
	"log/slog"
	"os"

	"github.com/allbin/agentkit/worktree"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/gitops"
)

// BlockingRunner runs a single blocking Claude CLI invocation. Used by the
// auto-title path — separate from the runtime.Manager-managed sessions.
type BlockingRunner interface {
	RunBlocking(ctx context.Context, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error)
}

// RealBlockingRunner returns a BlockingRunner that wraps claudecli.New().RunBlocking().
func RealBlockingRunner() BlockingRunner { return realBlockingRunner{} }

type realBlockingRunner struct{}

func (realBlockingRunner) RunBlocking(ctx context.Context, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error) {
	return claudecli.New().RunBlocking(ctx, prompt, opts...)
}

// RealBranchStatusQuerier returns a branchStatusQuerier backed by the gitops package.
func RealBranchStatusQuerier() branchStatusQuerier { return realBranchStatusQuerier{} }

type realBranchStatusQuerier struct{}

func (realBranchStatusQuerier) BranchExists(dir, branch string) bool {
	return gitops.BranchExists(dir, branch)
}
func (realBranchStatusQuerier) CommitsAhead(dir, branch string) (int, error) {
	return gitops.CommitsAhead(dir, branch)
}
func (realBranchStatusQuerier) CommitsBehind(dir, branch string) (int, error) {
	return gitops.CommitsBehind(dir, branch)
}
func (realBranchStatusQuerier) HasUncommittedChanges(dir string) (bool, error) {
	return gitops.HasUncommittedChanges(dir)
}
func (realBranchStatusQuerier) MergeTreeCheck(dir, branch string) (gitops.MergeTreeResult, error) {
	return gitops.MergeTreeCheck(dir, branch)
}

// worktreeOps abstracts worktree/branch operations used by Service and Channel.
//
// Worktree provisioning and removal go through agentkit/worktree. ProvisionWorktree
// auto-detects whether to attach to an existing branch or create a new one from
// HEAD. RemoveWorktree adopts the on-disk worktree (best-effort) and tears it down.
type worktreeOps interface {
	WorktreePath(projectName, branch string) string
	ProvisionWorktree(ctx context.Context, projectDir, branch, wtPath string) error
	RemoveWorktree(ctx context.Context, projectDir, branch, wtPath string)
	HeadSHA(ctx context.Context, dir string) (string, error)
	BranchExists(dir, branch string) bool
	DeleteBranch(dir, branch string) error
	ForceDeleteBranch(dir, branch string) error
	DeleteRemoteBranch(dir, branch string)
}

// RealWorktreeOps returns a worktreeOps backed by agentkit/worktree (for
// provisioning) and the gitops package (for branch ops).
func RealWorktreeOps() worktreeOps { return realWorktreeOps{} }

type realWorktreeOps struct{}

func (realWorktreeOps) WorktreePath(projectName, branch string) string {
	return gitops.WorktreePath(projectName, branch)
}

func (realWorktreeOps) ProvisionWorktree(ctx context.Context, projectDir, branch, wtPath string) error {
	repo, err := worktree.NewLocalRepo(projectDir)
	if err != nil {
		return err
	}
	_, err = repo.Worktree(ctx, worktree.WorktreeSpec{
		Path:   wtPath,
		Branch: worktree.SanitizeBranch(branch),
	})
	return err
}

func (realWorktreeOps) RemoveWorktree(ctx context.Context, projectDir, branch, wtPath string) {
	if wtPath == "" {
		return
	}
	if _, err := os.Stat(wtPath); err != nil {
		return
	}
	repo, err := worktree.NewLocalRepo(projectDir)
	if err != nil {
		slog.Warn("worktree remove: NewLocalRepo failed", "project", projectDir, "error", err)
		return
	}
	ws, err := repo.Worktree(ctx, worktree.WorktreeSpec{
		Path:   wtPath,
		Branch: worktree.SanitizeBranch(branch),
	})
	if err != nil {
		slog.Warn("worktree remove: adopt failed, removing directly", "path", wtPath, "error", err)
		_ = os.RemoveAll(wtPath)
		return
	}
	if err := ws.Close(ctx); err != nil {
		slog.Warn("worktree close failed", "path", wtPath, "error", err)
	}
}

func (realWorktreeOps) HeadSHA(ctx context.Context, dir string) (string, error) {
	return worktree.HeadSHA(ctx, dir)
}
func (realWorktreeOps) BranchExists(dir, branch string) bool { return gitops.BranchExists(dir, branch) }
func (realWorktreeOps) DeleteBranch(dir, branch string) error {
	return gitops.DeleteBranch(dir, branch)
}
func (realWorktreeOps) ForceDeleteBranch(dir, branch string) error {
	return gitops.ForceDeleteBranch(dir, branch)
}
func (realWorktreeOps) DeleteRemoteBranch(dir, branch string) { gitops.DeleteRemoteBranch(dir, branch) }

// sessionGitOps abstracts git operations used by session.GitService.
type sessionGitOps interface {
	HasUncommittedChanges(dir string) (bool, error)
	AutoCommitAll(dir, message string) error
	MergeBranch(dir, branch string) (string, error)
	MergeConflictFiles(dir string) ([]string, error)
	AbortMerge(dir string) error
	RemoveWorktree(ctx context.Context, projectDir, branch, wtPath string)
	DeleteBranch(dir, branch string) error
	DeleteRemoteBranch(dir, branch string)
	GC(dir string)
	HeadCommitHash(dir string) (string, error)
	RebaseBranch(dir, onto string) error
	RebaseConflictFiles(dir string) ([]string, error)
	AbortRebase(dir string) error
	HasGhCli() bool
	HasRemote(dir, remote string) (bool, error)
	GetExistingPR(dir, branch string) (string, error)
	PushBranch(dir, branch string) error
	CreatePR(dir, branch, title, body string) (string, error)
	WorktreeDiff(ctx context.Context, wtPath, baseSHA string, includeUntracked bool) (worktree.DiffResult, error)
	UncommittedFiles(dir string) ([]gitops.FileStatus, error)
	ProjectStatus(dir string) gitops.ProjectStatusResult
	BranchExists(dir, branch string) bool
	CommitLog(dir, branch string, limit int) ([]gitops.CommitLogEntry, error)
	PRStatus(dir, branch string) (gitops.PRStatusResult, error)
}

// RealSessionGitOps returns a sessionGitOps backed by the gitops and
// agentkit/worktree packages.
func RealSessionGitOps() sessionGitOps { return realSessionGitOps{} }

type realSessionGitOps struct{}

func (realSessionGitOps) HasUncommittedChanges(dir string) (bool, error) {
	return gitops.HasUncommittedChanges(dir)
}
func (realSessionGitOps) AutoCommitAll(dir, msg string) error { return gitops.AutoCommitAll(dir, msg) }
func (realSessionGitOps) MergeBranch(dir, branch string) (string, error) {
	return gitops.MergeBranch(dir, branch)
}
func (realSessionGitOps) MergeConflictFiles(dir string) ([]string, error) {
	return gitops.MergeConflictFiles(dir)
}
func (realSessionGitOps) AbortMerge(dir string) error { return gitops.AbortMerge(dir) }
func (realSessionGitOps) RemoveWorktree(ctx context.Context, projectDir, branch, wtPath string) {
	realWorktreeOps{}.RemoveWorktree(ctx, projectDir, branch, wtPath)
}
func (realSessionGitOps) DeleteBranch(dir, branch string) error {
	return gitops.DeleteBranch(dir, branch)
}
func (realSessionGitOps) DeleteRemoteBranch(dir, branch string) {
	gitops.DeleteRemoteBranch(dir, branch)
}
func (realSessionGitOps) GC(dir string) { gitops.GC(dir) }
func (realSessionGitOps) HeadCommitHash(dir string) (string, error) {
	return gitops.HeadCommitHash(dir)
}
func (realSessionGitOps) RebaseBranch(dir, onto string) error { return gitops.RebaseBranch(dir, onto) }
func (realSessionGitOps) RebaseConflictFiles(dir string) ([]string, error) {
	return gitops.RebaseConflictFiles(dir)
}
func (realSessionGitOps) AbortRebase(dir string) error { return gitops.AbortRebase(dir) }
func (realSessionGitOps) HasGhCli() bool               { return gitops.HasGhCli() }
func (realSessionGitOps) HasRemote(dir, remote string) (bool, error) {
	return gitops.HasRemote(dir, remote)
}
func (realSessionGitOps) GetExistingPR(dir, branch string) (string, error) {
	return gitops.GetExistingPR(dir, branch)
}
func (realSessionGitOps) PushBranch(dir, branch string) error { return gitops.PushBranch(dir, branch) }
func (realSessionGitOps) CreatePR(dir, branch, title, body string) (string, error) {
	return gitops.CreatePR(dir, branch, title, body)
}
func (realSessionGitOps) WorktreeDiff(ctx context.Context, wtPath, baseSHA string, includeUntracked bool) (worktree.DiffResult, error) {
	return worktree.DiffWorking(ctx, wtPath, baseSHA, worktree.DiffOptions{IncludeUntracked: includeUntracked})
}
func (realSessionGitOps) UncommittedFiles(dir string) ([]gitops.FileStatus, error) {
	return gitops.UncommittedFiles(dir)
}
func (realSessionGitOps) ProjectStatus(dir string) gitops.ProjectStatusResult {
	return gitops.ProjectStatus(dir)
}
func (realSessionGitOps) BranchExists(dir, branch string) bool {
	return gitops.BranchExists(dir, branch)
}
func (realSessionGitOps) CommitLog(dir, branch string, limit int) ([]gitops.CommitLogEntry, error) {
	return gitops.CommitLog(dir, branch, limit)
}
func (realSessionGitOps) PRStatus(dir, branch string) (gitops.PRStatusResult, error) {
	return gitops.PRStatus(dir, branch)
}
