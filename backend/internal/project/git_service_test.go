package project

import (
	"context"
	"fmt"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// --- Mocks ---

type mockQueries struct {
	project store.Project
	err     error
}

func (m *mockQueries) GetProject(_ context.Context, id string) (store.Project, error) {
	if m.err != nil {
		return store.Project{}, m.err
	}
	return m.project, nil
}

type broadcastMsg struct {
	projectID, pushType string
	payload             any
}

type mockBroadcaster struct {
	messages []broadcastMsg
}

func (b *mockBroadcaster) Broadcast(projectID, pushType string, payload any) {
	b.messages = append(b.messages, broadcastMsg{projectID, pushType, payload})
}

type mockGitOps struct {
	// Configurable returns
	projectStatus    gitops.ProjectStatusResult
	fetchErr         error
	currentBranch    string
	currentBranchErr error
	pushErr          error
	dirty            bool
	dirtyErr         error
	commitErr        error
	headHash         string
	headHashErr      error
	trackedFiles     []string
	trackedFilesErr  error
	commandFiles     []gitops.CommandFile
	commandFilesErr  error
	localBranches    []string
	remoteBranches   []string
	listBranchesErr  error
	checkoutErr      error
	upstreamRef      string
	upstreamRefErr   error
	mergeHash        string
	mergeErr         error
	uncommittedFiles []gitops.FileStatus
	uncommittedErr   error
	discardErr       error

	// Call tracking
	fetchCalled    bool
	pushBranch     string
	commitMsg      string
	checkoutBranch string
	discardCalled  bool
}

func (m *mockGitOps) Fetch(string) error {
	m.fetchCalled = true
	return m.fetchErr
}
func (m *mockGitOps) CurrentBranch(string) (string, error) {
	return m.currentBranch, m.currentBranchErr
}
func (m *mockGitOps) PushBranch(_, branch string) error {
	m.pushBranch = branch
	return m.pushErr
}
func (m *mockGitOps) HasUncommittedChanges(string) (bool, error) { return m.dirty, m.dirtyErr }
func (m *mockGitOps) AutoCommitAll(_, msg string) error {
	m.commitMsg = msg
	return m.commitErr
}
func (m *mockGitOps) HeadCommitHash(string) (string, error) { return m.headHash, m.headHashErr }
func (m *mockGitOps) ListTrackedFiles(string) ([]string, error) {
	return m.trackedFiles, m.trackedFilesErr
}
func (m *mockGitOps) ListCommandFiles(string) ([]gitops.CommandFile, error) {
	return m.commandFiles, m.commandFilesErr
}
func (m *mockGitOps) ListBranches(string) ([]string, []string, error) {
	return m.localBranches, m.remoteBranches, m.listBranchesErr
}
func (m *mockGitOps) CheckoutBranch(_, branch string) error {
	m.checkoutBranch = branch
	return m.checkoutErr
}
func (m *mockGitOps) UpstreamRef(string) (string, error) { return m.upstreamRef, m.upstreamRefErr }
func (m *mockGitOps) MergeBranch(string, string) (string, error) {
	return m.mergeHash, m.mergeErr
}
func (m *mockGitOps) UncommittedFiles(string) ([]gitops.FileStatus, error) {
	return m.uncommittedFiles, m.uncommittedErr
}
func (m *mockGitOps) DiscardAll(string) error {
	m.discardCalled = true
	return m.discardErr
}
func (m *mockGitOps) ProjectStatus(string) gitops.ProjectStatusResult { return m.projectStatus }

// --- Helpers ---

func newTestService(git *mockGitOps) (*GitService, *mockBroadcaster) {
	hub := &mockBroadcaster{}
	q := &mockQueries{project: store.Project{ID: "proj-1", Path: "/repo", Name: "test"}}
	return NewGitService(q, hub, git, nil), hub
}

// --- Tests ---

func TestStatus(t *testing.T) {
	git := &mockGitOps{
		projectStatus: gitops.ProjectStatusResult{Branch: "main", HasRemote: true, AheadRemote: 1},
	}
	svc, _ := newTestService(git)
	status, err := svc.Status(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Branch != "main" {
		t.Errorf("got branch %q, want main", status.Branch)
	}
	if status.AheadRemote != 1 {
		t.Errorf("got ahead %d, want 1", status.AheadRemote)
	}
}

func TestStatus_ProjectNotFound(t *testing.T) {
	svc := NewGitService(
		&mockQueries{err: fmt.Errorf("not found")},
		&mockBroadcaster{},
		&mockGitOps{},
		nil,
	)
	_, err := svc.Status(context.Background(), "bad-id")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetch(t *testing.T) {
	git := &mockGitOps{
		projectStatus: gitops.ProjectStatusResult{Branch: "main"},
	}
	svc, hub := newTestService(git)
	status, err := svc.Fetch(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if !git.fetchCalled {
		t.Error("expected Fetch to be called")
	}
	if status.Branch != "main" {
		t.Errorf("got branch %q, want main", status.Branch)
	}
	if len(hub.messages) != 1 || hub.messages[0].pushType != "project.git-status" {
		t.Errorf("expected 1 broadcast, got %d", len(hub.messages))
	}
}

func TestFetch_Error(t *testing.T) {
	git := &mockGitOps{fetchErr: fmt.Errorf("network error")}
	svc, _ := newTestService(git)
	_, err := svc.Fetch(context.Background(), "proj-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPush(t *testing.T) {
	git := &mockGitOps{
		currentBranch: "feature",
		projectStatus: gitops.ProjectStatusResult{Branch: "feature"},
	}
	svc, hub := newTestService(git)
	status, err := svc.Push(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if git.pushBranch != "feature" {
		t.Errorf("pushed branch %q, want feature", git.pushBranch)
	}
	if status.Branch != "feature" {
		t.Errorf("got branch %q, want feature", status.Branch)
	}
	if len(hub.messages) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(hub.messages))
	}
}

func TestCommit_Success(t *testing.T) {
	git := &mockGitOps{dirty: true, headHash: "abc123"}
	svc, hub := newTestService(git)
	result, err := svc.Commit(context.Background(), "proj-1", "fix: stuff")
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitHash != "abc123" {
		t.Errorf("got hash %q, want abc123", result.CommitHash)
	}
	if git.commitMsg != "fix: stuff" {
		t.Errorf("got msg %q, want 'fix: stuff'", git.commitMsg)
	}
	if len(hub.messages) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(hub.messages))
	}
}

func TestCommit_NothingToCommit(t *testing.T) {
	git := &mockGitOps{dirty: false}
	svc, _ := newTestService(git)
	_, err := svc.Commit(context.Background(), "proj-1", "msg")
	if err == nil {
		t.Fatal("expected error for clean tree")
	}
}

func TestCheckout_Success(t *testing.T) {
	git := &mockGitOps{
		dirty:         false,
		projectStatus: gitops.ProjectStatusResult{Branch: "develop"},
	}
	svc, hub := newTestService(git)
	status, err := svc.Checkout(context.Background(), "proj-1", "develop")
	if err != nil {
		t.Fatal(err)
	}
	if git.checkoutBranch != "develop" {
		t.Errorf("checked out %q, want develop", git.checkoutBranch)
	}
	if status.Branch != "develop" {
		t.Errorf("got branch %q, want develop", status.Branch)
	}
	if len(hub.messages) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(hub.messages))
	}
}

func TestCheckout_DirtyRefused(t *testing.T) {
	git := &mockGitOps{dirty: true}
	svc, _ := newTestService(git)
	_, err := svc.Checkout(context.Background(), "proj-1", "develop")
	if err == nil {
		t.Fatal("expected error for dirty tree")
	}
}

func TestPull_Success(t *testing.T) {
	git := &mockGitOps{
		upstreamRef:   "origin/main",
		mergeHash:     "def456",
		projectStatus: gitops.ProjectStatusResult{Branch: "main"},
	}
	svc, hub := newTestService(git)
	status, err := svc.Pull(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if !git.fetchCalled {
		t.Error("expected Fetch to be called")
	}
	if status.Branch != "main" {
		t.Errorf("got branch %q, want main", status.Branch)
	}
	if len(hub.messages) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(hub.messages))
	}
}

func TestPull_NoUpstream(t *testing.T) {
	git := &mockGitOps{upstreamRef: ""}
	svc, _ := newTestService(git)
	_, err := svc.Pull(context.Background(), "proj-1")
	if err == nil {
		t.Fatal("expected error for missing upstream")
	}
}

func TestTrackedFiles(t *testing.T) {
	git := &mockGitOps{trackedFiles: []string{"a.go", "b.go"}}
	svc, _ := newTestService(git)
	result, err := svc.TrackedFiles(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 2 {
		t.Errorf("got %d files, want 2", len(result.Files))
	}
}

func TestCommands(t *testing.T) {
	git := &mockGitOps{commandFiles: []gitops.CommandFile{{Name: "deploy", Source: "project"}}}
	svc, _ := newTestService(git)
	result, err := svc.Commands(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Commands) != 1 || result.Commands[0].Name != "deploy" {
		t.Errorf("unexpected commands: %v", result.Commands)
	}
}

func TestListBranches(t *testing.T) {
	git := &mockGitOps{localBranches: []string{"main", "dev"}, remoteBranches: []string{"feature"}}
	svc, _ := newTestService(git)
	result, err := svc.ListBranches(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Local) != 2 || len(result.Remote) != 1 {
		t.Errorf("got local=%d remote=%d, want 2/1", len(result.Local), len(result.Remote))
	}
}

func TestUncommittedFiles_NilToEmpty(t *testing.T) {
	git := &mockGitOps{uncommittedFiles: nil}
	svc, _ := newTestService(git)
	result, err := svc.UncommittedFiles(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Files == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(result.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(result.Files))
	}
}

func TestDiscardChanges(t *testing.T) {
	git := &mockGitOps{
		projectStatus: gitops.ProjectStatusResult{Branch: "main"},
	}
	svc, hub := newTestService(git)
	status, err := svc.DiscardChanges(context.Background(), "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if !git.discardCalled {
		t.Error("expected DiscardAll to be called")
	}
	if status.Branch != "main" {
		t.Errorf("got branch %q, want main", status.Branch)
	}
	if len(hub.messages) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(hub.messages))
	}
}

func TestBroadcastStatus(t *testing.T) {
	git := &mockGitOps{
		projectStatus: gitops.ProjectStatusResult{Branch: "main", UncommittedCount: 3},
	}
	svc, hub := newTestService(git)
	svc.BroadcastStatus(context.Background(), "proj-1")
	if len(hub.messages) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(hub.messages))
	}
	if hub.messages[0].pushType != "project.git-status" {
		t.Errorf("got push type %q", hub.messages[0].pushType)
	}
}
