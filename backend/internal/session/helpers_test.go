package session

import (
	"fmt"
	"testing"

	"github.com/allbin/agentique/backend/internal/gitops"
)

type mockBranchQuerier struct {
	branchExists bool
	ahead        int
	aheadErr     error
	behind       int
	behindErr    error
	dirty        bool
	dirtyErr     error
	mergeResult  gitops.MergeTreeResult
	mergeErr     error
}

func (m *mockBranchQuerier) BranchExists(string, string) bool     { return m.branchExists }
func (m *mockBranchQuerier) CommitsAhead(string, string) (int, error) { return m.ahead, m.aheadErr }
func (m *mockBranchQuerier) CommitsBehind(string, string) (int, error) { return m.behind, m.behindErr }
func (m *mockBranchQuerier) HasUncommittedChanges(string) (bool, error) { return m.dirty, m.dirtyErr }
func (m *mockBranchQuerier) MergeTreeCheck(string, string) (gitops.MergeTreeResult, error) {
	return m.mergeResult, m.mergeErr
}

func isZeroBranchStatus(bs branchStatus) bool {
	return !bs.BranchMissing && bs.CommitsAhead == 0 && bs.CommitsBehind == 0 &&
		!bs.HasUncommitted && bs.MergeStatus == "" && len(bs.MergeConflictFiles) == 0
}

func TestComputeBranchStatus_EmptyBranch(t *testing.T) {
	bs := computeBranchStatus(&mockBranchQuerier{}, "/repo", "", "/wt")
	if !isZeroBranchStatus(bs) {
		t.Errorf("expected zero value, got %+v", bs)
	}
}

func TestComputeBranchStatus_EmptyProjectPath(t *testing.T) {
	bs := computeBranchStatus(&mockBranchQuerier{}, "", "main", "/wt")
	if !isZeroBranchStatus(bs) {
		t.Errorf("expected zero value, got %+v", bs)
	}
}

func TestComputeBranchStatus_BranchMissing(t *testing.T) {
	q := &mockBranchQuerier{branchExists: false}
	bs := computeBranchStatus(q, "/repo", "gone-branch", "")
	if !bs.BranchMissing {
		t.Error("expected BranchMissing=true")
	}
	if bs.CommitsAhead != 0 || bs.CommitsBehind != 0 {
		t.Error("expected zero ahead/behind for missing branch")
	}
}

func TestComputeBranchStatus_FullStatus(t *testing.T) {
	q := &mockBranchQuerier{
		branchExists: true,
		ahead:        3,
		behind:       1,
		dirty:        true,
		mergeResult:  gitops.MergeTreeResult{Clean: true},
	}
	bs := computeBranchStatus(q, "/repo", "feature", "/wt")
	if bs.BranchMissing {
		t.Error("expected BranchMissing=false")
	}
	if bs.CommitsAhead != 3 {
		t.Errorf("got ahead=%d, want 3", bs.CommitsAhead)
	}
	if bs.CommitsBehind != 1 {
		t.Errorf("got behind=%d, want 1", bs.CommitsBehind)
	}
	if !bs.HasUncommitted {
		t.Error("expected HasUncommitted=true")
	}
	if bs.MergeStatus != "clean" {
		t.Errorf("got merge status %q, want clean", bs.MergeStatus)
	}
}

func TestComputeBranchStatus_AheadBehindErrors(t *testing.T) {
	q := &mockBranchQuerier{
		branchExists: true,
		aheadErr:     fmt.Errorf("git error"),
		behindErr:    fmt.Errorf("git error"),
		mergeResult:  gitops.MergeTreeResult{Clean: true},
	}
	bs := computeBranchStatus(q, "/repo", "feature", "")
	if bs.CommitsAhead != 0 || bs.CommitsBehind != 0 {
		t.Errorf("expected 0/0 on error, got %d/%d", bs.CommitsAhead, bs.CommitsBehind)
	}
}

func TestComputeBranchStatus_EmptyWtPath_SkipsDirtyCheck(t *testing.T) {
	q := &mockBranchQuerier{
		branchExists: true,
		dirty:        true, // should be ignored since wtPath is empty
		mergeResult:  gitops.MergeTreeResult{Clean: true},
	}
	bs := computeBranchStatus(q, "/repo", "feature", "")
	if bs.HasUncommitted {
		t.Error("expected HasUncommitted=false when wtPath is empty")
	}
}

func TestComputeBranchStatus_MergeConflicts(t *testing.T) {
	q := &mockBranchQuerier{
		branchExists: true,
		mergeResult: gitops.MergeTreeResult{
			Clean:         false,
			ConflictFiles: []string{"file1.go", "file2.go"},
		},
	}
	bs := computeBranchStatus(q, "/repo", "feature", "")
	if bs.MergeStatus != "conflicts" {
		t.Errorf("got merge status %q, want conflicts", bs.MergeStatus)
	}
	if len(bs.MergeConflictFiles) != 2 {
		t.Errorf("got %d conflict files, want 2", len(bs.MergeConflictFiles))
	}
}

func TestComputeBranchStatus_MergeCheckError(t *testing.T) {
	q := &mockBranchQuerier{
		branchExists: true,
		mergeErr:     fmt.Errorf("merge-tree failed"),
	}
	bs := computeBranchStatus(q, "/repo", "feature", "")
	if bs.MergeStatus != "unknown" {
		t.Errorf("got merge status %q, want unknown", bs.MergeStatus)
	}
}
