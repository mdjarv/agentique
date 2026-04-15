package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/allbin/agentique/backend/internal/gitops"
)

// GitVersion returns the current git version counter.
func (s *Session) GitVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.git.gitVersion
}

// MarkMerged sets the worktreeMerged flag on a live session.
func (s *Session) MarkMerged() {
	s.mu.Lock()
	s.git.worktreeMerged = true
	s.mu.Unlock()
}

// nextGitVersion returns a monotonically increasing version for this session.
// Must NOT be called under s.mu.
func (s *Session) nextGitVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.git.gitVersion++
	return s.git.gitVersion
}

func (s *Session) broadcastState(state State) {
	snap := s.buildLocalSnapshot(state)
	s.broadcast("session.state", snap)
}

// buildLocalSnapshot constructs a GitSnapshot from the session's own state.
// Used by the Session itself (e.g. on running→idle transitions).
func (s *Session) buildLocalSnapshot(state State) GitSnapshot {
	snap := GitSnapshot{
		SessionID: s.ID,
		State:     string(state),
		Connected: true,
	}

	s.mu.Lock()
	snap.WorktreeMerged = s.git.worktreeMerged
	snap.CompletedAt = s.completedAt
	snap.GitOperation = s.git.gitOperation
	s.git.gitVersion++
	snap.Version = s.git.gitVersion
	s.mu.Unlock()

	if s.git.workDir != "" && !snap.WorktreeMerged && (state == StateIdle || state == StateDone) {
		if s.git.gitStatus != nil {
			if dirty, err := s.git.gitStatus.HasUncommittedChanges(s.git.workDir); err == nil {
				snap.HasDirtyWorktree = dirty
				snap.HasUncommitted = dirty
			}
		}
		s.enrichSnapshot(&snap)
	}

	return snap
}

// enrichSnapshot adds branch-level git info (ahead/behind, merge status) to a snapshot.
func (s *Session) enrichSnapshot(snap *GitSnapshot) {
	if s.git.gitStatus == nil {
		return
	}
	ctx := context.Background()
	row, err := s.queries.GetSession(ctx, s.ID)
	if err != nil {
		return
	}
	branch := nullStr(row.WorktreeBranch)
	if branch == "" || row.WorktreeMerged != 0 {
		return
	}
	project, err := s.queries.GetProject(ctx, s.ProjectID)
	if err != nil {
		return
	}
	bs := computeBranchStatus(s.git.gitStatus, project.Path, branch, "")
	snap.BranchMissing = bs.BranchMissing
	snap.CommitsAhead = bs.CommitsAhead
	snap.CommitsBehind = bs.CommitsBehind
	snap.MergeStatus = bs.MergeStatus
	snap.MergeConflictFiles = bs.MergeConflictFiles
}

// scheduleGitRefresh debounces a lightweight git status check during a running turn.
// Each call resets the timer; the check fires once after gitRefreshDebounce of quiet.
func (s *Session) scheduleGitRefresh() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.git.gitRefreshTimer != nil {
		s.git.gitRefreshTimer.Stop()
	}

	s.git.gitRefreshTimer = time.AfterFunc(gitRefreshDebounce, func() {
		s.mu.Lock()
		s.git.gitRefreshTimer = nil
		if s.state != StateRunning {
			s.mu.Unlock()
			return
		}
		workDir := s.git.workDir
		merged := s.git.worktreeMerged
		s.mu.Unlock()

		if workDir == "" || merged {
			return
		}

		dirty, err := gitops.HasUncommittedChanges(workDir)
		if err != nil {
			slog.Warn("mid-turn git status check failed", "session_id", s.ID, "error", err)
			return
		}

		s.broadcastMidTurnGitStatus(dirty)
	})
}

// broadcastMidTurnGitStatus sends a lightweight git snapshot with dirty/uncommitted
// state. Skips expensive branch-level checks (ahead/behind, merge status).
func (s *Session) broadcastMidTurnGitStatus(dirty bool) {
	s.mu.Lock()
	s.git.gitVersion++
	snap := GitSnapshot{
		SessionID:        s.ID,
		State:            string(s.state),
		Connected:        true,
		HasDirtyWorktree: dirty,
		HasUncommitted:   dirty,
		WorktreeMerged:   s.git.worktreeMerged,
		CompletedAt:      s.completedAt,
		GitOperation:     s.git.gitOperation,
		Version:          s.git.gitVersion,
	}
	s.mu.Unlock()

	s.broadcast("session.state", snap)
}

// stopGitRefreshTimer cancels any pending mid-turn git refresh.
func (s *Session) stopGitRefreshTimer() {
	s.mu.Lock()
	if s.git.gitRefreshTimer != nil {
		s.git.gitRefreshTimer.Stop()
		s.git.gitRefreshTimer = nil
	}
	s.mu.Unlock()
}
