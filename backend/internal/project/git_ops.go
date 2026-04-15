package project

import "github.com/allbin/agentique/backend/internal/gitops"

// projectGitOps abstracts git operations for testability.
type projectGitOps interface {
	Fetch(dir string) error
	CurrentBranch(dir string) (string, error)
	PushBranch(dir, branch string) error
	HasUncommittedChanges(dir string) (bool, error)
	AutoCommitAll(dir, message string) error
	HeadCommitHash(dir string) (string, error)
	ListTrackedFiles(dir string) ([]string, error)
	ListCommandFiles(dir string) ([]gitops.CommandFile, error)
	ListBranches(dir string) (local []string, remote []string, err error)
	CheckoutBranch(dir, branch string) error
	UpstreamRef(dir string) (string, error)
	MergeBranch(dir, branch string) (string, error)
	UncommittedFiles(dir string) ([]gitops.FileStatus, error)
	DiscardAll(dir string) error
	ProjectStatus(dir string) gitops.ProjectStatusResult
}

// RealGitOps returns a projectGitOps backed by the gitops package.
func RealGitOps() projectGitOps { return realProjectGitOps{} }

// realProjectGitOps delegates to the gitops package functions.
type realProjectGitOps struct{}

func (realProjectGitOps) Fetch(dir string) error                      { return gitops.Fetch(dir) }
func (realProjectGitOps) CurrentBranch(dir string) (string, error)    { return gitops.CurrentBranch(dir) }
func (realProjectGitOps) PushBranch(dir, branch string) error         { return gitops.PushBranch(dir, branch) }
func (realProjectGitOps) HasUncommittedChanges(dir string) (bool, error) { return gitops.HasUncommittedChanges(dir) }
func (realProjectGitOps) AutoCommitAll(dir, message string) error     { return gitops.AutoCommitAll(dir, message) }
func (realProjectGitOps) HeadCommitHash(dir string) (string, error)   { return gitops.HeadCommitHash(dir) }
func (realProjectGitOps) ListTrackedFiles(dir string) ([]string, error) { return gitops.ListTrackedFiles(dir) }
func (realProjectGitOps) ListCommandFiles(dir string) ([]gitops.CommandFile, error) { return gitops.ListCommandFiles(dir) }
func (realProjectGitOps) ListBranches(dir string) ([]string, []string, error) { return gitops.ListBranches(dir) }
func (realProjectGitOps) CheckoutBranch(dir, branch string) error     { return gitops.CheckoutBranch(dir, branch) }
func (realProjectGitOps) UpstreamRef(dir string) (string, error)      { return gitops.UpstreamRef(dir) }
func (realProjectGitOps) MergeBranch(dir, branch string) (string, error) { return gitops.MergeBranch(dir, branch) }
func (realProjectGitOps) UncommittedFiles(dir string) ([]gitops.FileStatus, error) { return gitops.UncommittedFiles(dir) }
func (realProjectGitOps) DiscardAll(dir string) error                 { return gitops.DiscardAll(dir) }
func (realProjectGitOps) ProjectStatus(dir string) gitops.ProjectStatusResult { return gitops.ProjectStatus(dir) }
