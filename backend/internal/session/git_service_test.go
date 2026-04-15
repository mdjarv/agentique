package session

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/allbin/agentique/backend/internal/gitops"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// --- Mock sessionGitOps ---

type mockSessionGitOps struct {
	dirty      bool
	dirtyErr   error
	commitErr  error
	headHash   string
	headHashErr error
	mergeHash  string
	mergeErr   error
	conflictFiles []string
	abortMergeErr error
	rebaseErr  error
	rebaseConflictFiles []string
	diffResult gitops.DiffResult
	diffErr    error
	uncommittedFiles []gitops.FileStatus
	uncommittedErr   error
	hasGhCli   bool
	hasRemote  bool
	remoteErr  error
	existingPR string
	existingPRErr error
	pushErr    error
	createPRUrl string
	createPRErr error
	branchExists bool
	projectStatus gitops.ProjectStatusResult

	// call tracking
	removedWorktrees []string
	deletedBranches  []string
	gcCalled         bool
}

func (m *mockSessionGitOps) HasUncommittedChanges(string) (bool, error) { return m.dirty, m.dirtyErr }
func (m *mockSessionGitOps) AutoCommitAll(string, string) error         { return m.commitErr }
func (m *mockSessionGitOps) MergeBranch(string, string) (string, error) { return m.mergeHash, m.mergeErr }
func (m *mockSessionGitOps) MergeConflictFiles(string) ([]string, error) { return m.conflictFiles, nil }
func (m *mockSessionGitOps) AbortMerge(string) error                    { return m.abortMergeErr }
func (m *mockSessionGitOps) RemoveWorktree(_, wtPath string)            { m.removedWorktrees = append(m.removedWorktrees, wtPath) }
func (m *mockSessionGitOps) DeleteBranch(_, branch string) error        { m.deletedBranches = append(m.deletedBranches, branch); return nil }
func (m *mockSessionGitOps) DeleteRemoteBranch(string, string)          {}
func (m *mockSessionGitOps) GC(string)                                  { m.gcCalled = true }
func (m *mockSessionGitOps) HeadCommitHash(string) (string, error)      { return m.headHash, m.headHashErr }
func (m *mockSessionGitOps) RebaseBranch(string, string) error          { return m.rebaseErr }
func (m *mockSessionGitOps) RebaseConflictFiles(string) ([]string, error) { return m.rebaseConflictFiles, nil }
func (m *mockSessionGitOps) AbortRebase(string) error                   { return nil }
func (m *mockSessionGitOps) HasGhCli() bool                             { return m.hasGhCli }
func (m *mockSessionGitOps) HasRemote(string, string) (bool, error)     { return m.hasRemote, m.remoteErr }
func (m *mockSessionGitOps) GetExistingPR(string, string) (string, error) { return m.existingPR, m.existingPRErr }
func (m *mockSessionGitOps) PushBranch(string, string) error            { return m.pushErr }
func (m *mockSessionGitOps) CreatePR(string, string, string, string) (string, error) { return m.createPRUrl, m.createPRErr }
func (m *mockSessionGitOps) WorktreeDiff(string, string, bool) (gitops.DiffResult, error) { return m.diffResult, m.diffErr }
func (m *mockSessionGitOps) UncommittedFiles(string) ([]gitops.FileStatus, error) { return m.uncommittedFiles, m.uncommittedErr }
func (m *mockSessionGitOps) ProjectStatus(string) gitops.ProjectStatusResult { return m.projectStatus }
func (m *mockSessionGitOps) BranchExists(string, string) bool          { return m.branchExists }
func (m *mockSessionGitOps) CommitLog(string, string, int) ([]gitops.CommitLogEntry, error) { return nil, nil }
func (m *mockSessionGitOps) PRStatus(string, string) (gitops.PRStatusResult, error) { return gitops.PRStatusResult{}, nil }

// --- Suite ---

type GitServiceSuite struct {
	testutil.DBSuite
	gitSvc *GitService
	mgr    *Manager
	git    *mockSessionGitOps
}

func TestGitServiceSuite(t *testing.T) {
	suite.Run(t, new(GitServiceSuite))
}

func (s *GitServiceSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.git = &mockSessionGitOps{}
	s.gitSvc = NewGitService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
	s.gitSvc.SetGitOps(s.git)
}

func (s *GitServiceSuite) createWorktreeSession() string {
	id := "wt-" + s.T().Name()
	wtDir := s.T().TempDir()
	_, err := s.Queries.CreateSession(context.Background(), store.CreateSessionParams{
		ID:             id,
		ProjectID:      s.Project.ID,
		Name:           "test-wt",
		WorkDir:        wtDir,
		WorktreePath:   sql.NullString{String: wtDir, Valid: true},
		WorktreeBranch: sql.NullString{String: "session-abc", Valid: true},
		State:          "idle",
		Model:          "opus",
	})
	s.Require().NoError(err)
	return id
}

func (s *GitServiceSuite) createLocalSession() string {
	id := "local-" + s.T().Name()
	_, err := s.Queries.CreateSession(context.Background(), store.CreateSessionParams{
		ID:        id,
		ProjectID: s.Project.ID,
		Name:      "test-local",
		WorkDir:   s.Project.Path,
		State:     "idle",
		Model:     "opus",
	})
	s.Require().NoError(err)
	return id
}

// --- Merge tests ---

func (s *GitServiceSuite) TestMerge_InvalidMode() {
	id := s.createWorktreeSession()
	_, err := s.gitSvc.Merge(context.Background(), id, "invalid")
	s.Error(err)
	s.Contains(err.Error(), "invalid merge mode")
}

func (s *GitServiceSuite) TestMerge_SessionNotFound() {
	_, err := s.gitSvc.Merge(context.Background(), "nonexistent", MergeModeMerge)
	s.Error(err)
	s.Contains(err.Error(), "session not found")
}

func (s *GitServiceSuite) TestMerge_NoBranch() {
	id := s.createLocalSession()
	_, err := s.gitSvc.Merge(context.Background(), id, MergeModeMerge)
	s.Error(err)
	s.Contains(err.Error(), "no worktree branch")
}

func (s *GitServiceSuite) TestMerge_DirtyProjectRoot() {
	id := s.createWorktreeSession()
	s.git.dirty = true
	result, err := s.gitSvc.Merge(context.Background(), id, MergeModeMerge)
	s.NoError(err)
	s.Equal("dirty_worktree", result.Status)
}

func (s *GitServiceSuite) TestMerge_Success() {
	id := s.createWorktreeSession()
	s.git.mergeHash = "abc123"
	result, err := s.gitSvc.Merge(context.Background(), id, MergeModeMerge)
	s.NoError(err)
	s.Equal("merged", result.Status)
	s.Equal("abc123", result.CommitHash)
}

func (s *GitServiceSuite) TestMerge_Conflict() {
	id := s.createWorktreeSession()
	s.git.mergeErr = fmt.Errorf("merge conflict")
	s.git.conflictFiles = []string{"file1.go", "file2.go"}
	result, err := s.gitSvc.Merge(context.Background(), id, MergeModeMerge)
	s.NoError(err)
	s.Equal("conflict", result.Status)
	s.Len(result.ConflictFiles, 2)
}

func (s *GitServiceSuite) TestMerge_DeleteMode_CleansUp() {
	id := s.createWorktreeSession()
	s.git.mergeHash = "abc123"
	result, err := s.gitSvc.Merge(context.Background(), id, MergeModeDelete)
	s.NoError(err)
	s.Equal("merged", result.Status)
	s.Len(s.git.removedWorktrees, 1)
	s.Len(s.git.deletedBranches, 1)
}

// --- Commit tests ---

func (s *GitServiceSuite) TestCommit_NoChanges() {
	id := s.createWorktreeSession()
	s.git.dirty = false
	_, err := s.gitSvc.Commit(context.Background(), id, "msg")
	s.Error(err)
	s.Contains(err.Error(), "no uncommitted changes")
}

func (s *GitServiceSuite) TestCommit_WorktreeSession() {
	id := s.createWorktreeSession()
	s.git.dirty = true
	s.git.headHash = "def456"
	result, err := s.gitSvc.Commit(context.Background(), id, "fix: stuff")
	s.NoError(err)
	s.Equal("def456", result.CommitHash)
}

// --- Diff tests ---

func (s *GitServiceSuite) TestDiff_MergedSession() {
	id := s.createWorktreeSession()
	s.Require().NoError(s.Queries.SetWorktreeMerged(context.Background(), id))
	result, err := s.gitSvc.Diff(context.Background(), id)
	s.NoError(err)
	s.False(result.HasDiff)
}

func (s *GitServiceSuite) TestDiff_SessionNotFound() {
	_, err := s.gitSvc.Diff(context.Background(), "nonexistent")
	s.Error(err)
}

// --- Clean tests ---

func (s *GitServiceSuite) TestClean_Success() {
	id := s.createWorktreeSession()
	s.git.branchExists = true
	result, err := s.gitSvc.Clean(context.Background(), id)
	s.NoError(err)
	s.Equal("cleaned", result.Status)
	s.Len(s.git.removedWorktrees, 1)
	s.Len(s.git.deletedBranches, 1)
}

func (s *GitServiceSuite) TestClean_NoBranch() {
	id := s.createLocalSession()
	_, err := s.gitSvc.Clean(context.Background(), id)
	s.Error(err)
	s.Contains(err.Error(), "no worktree branch")
}
