package session

import (
	"context"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/gitops"
)

// CLISession abstracts a claudecli-go interactive session for testability.
// The real *claudecli.Session satisfies this interface.
type CLISession interface {
	Events() <-chan claudecli.Event
	Query(prompt string) error
	QueryWithContent(prompt string, blocks ...claudecli.ContentBlock) error
	SendMessage(prompt string) error
	SendMessageWithContent(prompt string, blocks ...claudecli.ContentBlock) error
	SetPermissionMode(mode claudecli.PermissionMode) error
	SetModel(model claudecli.Model) error
	ReconnectMCPServer(serverName string) error
	ReconnectMCPServerWait(serverName string, timeout time.Duration) error
	Interrupt() error
	Close() error
}

// CLIConnector creates new CLI sessions.
type CLIConnector interface {
	Connect(ctx context.Context, opts ...claudecli.Option) (CLISession, error)
}

// BlockingRunner runs a single blocking Claude CLI invocation.
type BlockingRunner interface {
	RunBlocking(ctx context.Context, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error)
}

// RealConnector returns a CLIConnector that wraps claudecli.New().Connect().
func RealConnector() CLIConnector { return realConnector{} }

type realConnector struct{}

func (realConnector) Connect(ctx context.Context, opts ...claudecli.Option) (CLISession, error) {
	return claudecli.New().Connect(ctx, opts...)
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
type worktreeOps interface {
	WorktreePath(projectName, branch string) string
	CreateWorktree(projectDir, branch, wtPath string) error
	RestoreWorktree(projectDir, branch, wtPath string) error
	RemoveWorktree(projectDir, wtPath string)
	GetWorktreeBaseSHA(dir string) (string, error)
	BranchExists(dir, branch string) bool
	DeleteBranch(dir, branch string) error
	ForceDeleteBranch(dir, branch string) error
	DeleteRemoteBranch(dir, branch string)
}

// RealWorktreeOps returns a worktreeOps backed by the gitops package.
func RealWorktreeOps() worktreeOps { return realWorktreeOps{} }

type realWorktreeOps struct{}

func (realWorktreeOps) WorktreePath(projectName, branch string) string { return gitops.WorktreePath(projectName, branch) }
func (realWorktreeOps) CreateWorktree(projectDir, branch, wtPath string) error { return gitops.CreateWorktree(projectDir, branch, wtPath) }
func (realWorktreeOps) RestoreWorktree(projectDir, branch, wtPath string) error { return gitops.RestoreWorktree(projectDir, branch, wtPath) }
func (realWorktreeOps) RemoveWorktree(projectDir, wtPath string)       { gitops.RemoveWorktree(projectDir, wtPath) }
func (realWorktreeOps) GetWorktreeBaseSHA(dir string) (string, error)  { return gitops.GetWorktreeBaseSHA(dir) }
func (realWorktreeOps) BranchExists(dir, branch string) bool          { return gitops.BranchExists(dir, branch) }
func (realWorktreeOps) DeleteBranch(dir, branch string) error          { return gitops.DeleteBranch(dir, branch) }
func (realWorktreeOps) ForceDeleteBranch(dir, branch string) error     { return gitops.ForceDeleteBranch(dir, branch) }
func (realWorktreeOps) DeleteRemoteBranch(dir, branch string)          { gitops.DeleteRemoteBranch(dir, branch) }

// sessionGitOps abstracts git operations used by session.GitService.
type sessionGitOps interface {
	HasUncommittedChanges(dir string) (bool, error)
	AutoCommitAll(dir, message string) error
	MergeBranch(dir, branch string) (string, error)
	MergeConflictFiles(dir string) ([]string, error)
	AbortMerge(dir string) error
	RemoveWorktree(projectDir, wtPath string)
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
	WorktreeDiff(wtPath, baseSHA string, includeUntracked bool) (gitops.DiffResult, error)
	UncommittedFiles(dir string) ([]gitops.FileStatus, error)
	ProjectStatus(dir string) gitops.ProjectStatusResult
	BranchExists(dir, branch string) bool
	CommitLog(dir, branch string, limit int) ([]gitops.CommitLogEntry, error)
	PRStatus(dir, branch string) (gitops.PRStatusResult, error)
}

// RealSessionGitOps returns a sessionGitOps backed by the gitops package.
func RealSessionGitOps() sessionGitOps { return realSessionGitOps{} }

type realSessionGitOps struct{}

func (realSessionGitOps) HasUncommittedChanges(dir string) (bool, error) { return gitops.HasUncommittedChanges(dir) }
func (realSessionGitOps) AutoCommitAll(dir, msg string) error            { return gitops.AutoCommitAll(dir, msg) }
func (realSessionGitOps) MergeBranch(dir, branch string) (string, error) { return gitops.MergeBranch(dir, branch) }
func (realSessionGitOps) MergeConflictFiles(dir string) ([]string, error) { return gitops.MergeConflictFiles(dir) }
func (realSessionGitOps) AbortMerge(dir string) error                    { return gitops.AbortMerge(dir) }
func (realSessionGitOps) RemoveWorktree(projectDir, wtPath string)       { gitops.RemoveWorktree(projectDir, wtPath) }
func (realSessionGitOps) DeleteBranch(dir, branch string) error          { return gitops.DeleteBranch(dir, branch) }
func (realSessionGitOps) DeleteRemoteBranch(dir, branch string)          { gitops.DeleteRemoteBranch(dir, branch) }
func (realSessionGitOps) GC(dir string)                                  { gitops.GC(dir) }
func (realSessionGitOps) HeadCommitHash(dir string) (string, error)      { return gitops.HeadCommitHash(dir) }
func (realSessionGitOps) RebaseBranch(dir, onto string) error            { return gitops.RebaseBranch(dir, onto) }
func (realSessionGitOps) RebaseConflictFiles(dir string) ([]string, error) { return gitops.RebaseConflictFiles(dir) }
func (realSessionGitOps) AbortRebase(dir string) error                   { return gitops.AbortRebase(dir) }
func (realSessionGitOps) HasGhCli() bool                                 { return gitops.HasGhCli() }
func (realSessionGitOps) HasRemote(dir, remote string) (bool, error)     { return gitops.HasRemote(dir, remote) }
func (realSessionGitOps) GetExistingPR(dir, branch string) (string, error) { return gitops.GetExistingPR(dir, branch) }
func (realSessionGitOps) PushBranch(dir, branch string) error            { return gitops.PushBranch(dir, branch) }
func (realSessionGitOps) CreatePR(dir, branch, title, body string) (string, error) { return gitops.CreatePR(dir, branch, title, body) }
func (realSessionGitOps) WorktreeDiff(wtPath, baseSHA string, includeUntracked bool) (gitops.DiffResult, error) { return gitops.WorktreeDiff(wtPath, baseSHA, includeUntracked) }
func (realSessionGitOps) UncommittedFiles(dir string) ([]gitops.FileStatus, error) { return gitops.UncommittedFiles(dir) }
func (realSessionGitOps) ProjectStatus(dir string) gitops.ProjectStatusResult { return gitops.ProjectStatus(dir) }
func (realSessionGitOps) BranchExists(dir, branch string) bool          { return gitops.BranchExists(dir, branch) }
func (realSessionGitOps) CommitLog(dir, branch string, limit int) ([]gitops.CommitLogEntry, error) { return gitops.CommitLog(dir, branch, limit) }
func (realSessionGitOps) PRStatus(dir, branch string) (gitops.PRStatusResult, error) { return gitops.PRStatus(dir, branch) }
