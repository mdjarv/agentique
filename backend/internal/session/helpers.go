package session

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mdjarv/agentique/backend/internal/gitops"
)

// branchStatusQuerier abstracts git queries needed by computeBranchStatus.
type branchStatusQuerier interface {
	BranchExists(dir, branch string) bool
	CommitsAhead(dir, branch string) (int, error)
	CommitsBehind(dir, branch string) (int, error)
	HasUncommittedChanges(dir string) (bool, error)
	MergeTreeCheck(dir, branch string) (gitops.MergeTreeResult, error)
}

// ensureLive returns a live session for sessionID, performing lazy resume if
// needed. If the session's CLI process has terminated (StateDone/StateFailed),
// it is evicted and resumed from its Claude session ID.
func (s *Service) ensureLive(ctx context.Context, sessionID string) (*Session, error) {
	sess := s.mgr.Get(sessionID)

	// CLI process dead — evict and resume with a fresh connection.
	if sess != nil && (sess.State() == StateDone || sess.State() == StateFailed) {
		slog.Debug("evicting dead session for resume", "session_id", sessionID, "state", string(sess.State()))
		s.evictForResume(sessionID)
		sess = nil
	}

	if sess == nil {
		var err error
		sess, err = s.resumeSession(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
		}
	}
	return sess, nil
}

// branchStatus holds computed git status for a session's worktree branch.
type branchStatus struct {
	BranchMissing      bool
	CommitsAhead       int
	CommitsBehind      int
	HasUncommitted     bool
	MergeStatus        string // "clean", "conflicts", "unknown", or ""
	MergeConflictFiles []string
}

// computeBranchStatus queries git for branch-level status of a worktree session.
// projectPath is the main repo path; branch is the worktree branch name;
// wtPath is the worktree directory (may be empty for non-worktree sessions).
func computeBranchStatus(q branchStatusQuerier, projectPath, branch, wtPath string) branchStatus {
	var bs branchStatus
	if branch == "" || projectPath == "" {
		return bs
	}
	if !q.BranchExists(projectPath, branch) {
		bs.BranchMissing = true
		return bs
	}

	if ahead, err := q.CommitsAhead(projectPath, branch); err == nil {
		bs.CommitsAhead = ahead
	} else {
		slog.Debug("CommitsAhead failed", "branch", branch, "error", err)
	}
	if behind, err := q.CommitsBehind(projectPath, branch); err == nil {
		bs.CommitsBehind = behind
	} else {
		slog.Debug("CommitsBehind failed", "branch", branch, "error", err)
	}

	if wtPath != "" {
		if dirty, err := q.HasUncommittedChanges(wtPath); err == nil {
			bs.HasUncommitted = dirty
		}
	}

	result, mergeErr := q.MergeTreeCheck(projectPath, branch)
	if mergeErr != nil {
		bs.MergeStatus = "unknown"
	} else if result.Clean {
		bs.MergeStatus = "clean"
	} else {
		bs.MergeStatus = "conflicts"
		bs.MergeConflictFiles = result.ConflictFiles
	}
	return bs
}

// postQuery performs common bookkeeping after a successful sess.Query() call:
// updates the last-query-at timestamp (best-effort) and auto-names the session
// on first query.
func (s *Service) postQuery(ctx context.Context, sessionID string, sess *Session, prompt string) {
	if err := s.queries.UpdateSessionLastQueryAt(ctx, sessionID); err != nil {
		slog.Debug("update last_query_at failed (best-effort)", "session_id", sessionID, "error", err)
	}
	if sess.QueryCount() == 1 {
		go s.autoName(sessionID, sess.ProjectID, prompt)
	}
}
