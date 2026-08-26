package session

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// ResolveApproval sends a permission response for a pending tool approval.
func (s *Service) ResolveApproval(sessionID, approvalID string, allow bool, message string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	return sess.ResolveApproval(approvalID, allow, message)
}

// ResolveQuestion sends answers for a pending user question.
func (s *Service) ResolveQuestion(sessionID, questionID string, answers map[string]string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	return sess.ResolveQuestion(questionID, answers)
}

// DismissQuestion resolves a pending question with a dismissal sentinel so
// the user can redirect without answering. See Session.DismissQuestion.
func (s *Service) DismissQuestion(sessionID, questionID string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	return sess.DismissQuestion(questionID)
}

// SetPermissionMode changes the permission mode for a live session and persists it.
func (s *Service) SetPermissionMode(sessionID, mode string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	if err := sess.SetPermissionMode(mode); err != nil {
		return err
	}
	if err := s.queries.UpdateSessionPermissionMode(context.Background(), store.UpdateSessionPermissionModeParams{
		PermissionMode: sess.PermissionMode(),
		ID:             sessionID,
	}); err != nil {
		return newPersistError("update permission mode", err)
	}
	return nil
}

// SetAutoApproveMode sets the auto-approve mode for a session and persists it.
func (s *Service) SetAutoApproveMode(sessionID string, mode string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	sess.SetAutoApproveMode(mode)
	if err := s.queries.UpdateSessionAutoApproveMode(context.Background(), store.UpdateSessionAutoApproveModeParams{
		AutoApproveMode: sess.AutoApproveMode(), // use validated value
		ID:              sessionID,
	}); err != nil {
		return newPersistError("update auto-approve mode", err)
	}
	return nil
}

// ArchiveSession files a session away into the sidebar's Archived section.
// Works for both live (idle) and non-live (stopped/failed) sessions.
//
// Archiving is a placement decision, so it writes archived_at and never state —
// the session's state keeps describing what its CLI is actually doing. Two
// consequences worth stating, because they are the whole point:
//
//   - A turn in flight is refused. The turn would keep running behind a row the
//     user believes is filed away, and there is no honest state to show for
//     that. Stop the session first. Busy comes from the runtime's own turn
//     lifecycle, never from State() — which reads Idle for one dispatch before
//     the completion that caused it is broadcast.
//   - A live *idle* session's CLI is released, through the same StopSession the
//     idle-eviction sweep uses. "I'm done with this" is exactly when a CLI
//     process stops earning its keep, and the resulting Stopped state is one
//     that genuinely happened. Best effort: the archive stands either way, and
//     the next message lazy-resumes.
func (s *Service) ArchiveSession(ctx context.Context, sessionID string) error {
	live := s.mgr.Get(sessionID)
	if live != nil && live.TurnInFlight() {
		return fmt.Errorf("session %s: cannot archive a session running a turn: %w", sessionID, ErrBusy)
	}

	if live != nil {
		if err := live.Archive(); err != nil {
			return err
		}
		// Reclaim the process only from a settled session. A state that is not
		// Idle either has no CLI to release (stopped/failed) or has a git op
		// holding the worktree (merging), where stopping would race it.
		if live.State() == StateIdle {
			if err := s.StopSession(ctx, sessionID); err != nil {
				slog.Warn("archive: releasing the CLI failed; session stays live",
					"session_id", sessionID, "error", err)
			}
		}
		s.notifySessionFinished(sessionID)
		return nil
	}

	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}

	if err := s.queries.SetSessionArchived(ctx, sessionID); err != nil {
		return fmt.Errorf("persist session archived: %w", err)
	}

	if s.gitSvc != nil {
		if snap, err := s.gitSvc.computeGitSnapshot(ctx, sessionID); err == nil {
			s.hub.Publish(dbSess.ProjectID, "session.state", snap)
		}
	}

	s.notifySessionFinished(sessionID)
	return nil
}

// MarkSessionSeen clears the session's unread-completion mark: the operator has
// read what came back. The read receipt behind the session.markSeen RPC.
//
// Idempotent, and deliberately so — a client that opens the same session twice,
// or one catching up after a reconnect, should not have to know whether the mark
// was still set. Clearing an already-clear session writes a no-op row and
// broadcasts the same snapshot the client already has.
//
// Works for both live and non-live sessions, split the same way
// UnarchiveSession is: a live session clears its own mirror and broadcasts from
// it, and a stopped one is a row write plus a snapshot rebuilt from the row.
func (s *Service) MarkSessionSeen(ctx context.Context, sessionID string) error {
	if live := s.mgr.Get(sessionID); live != nil {
		return live.MarkSeen()
	}

	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}

	if err := s.queries.ClearSessionUnseenCompletedAt(ctx, sessionID); err != nil {
		return fmt.Errorf("clear session unseen completion: %w", err)
	}

	if s.gitSvc != nil {
		if snap, err := s.gitSvc.computeGitSnapshot(ctx, sessionID); err == nil {
			s.hub.Publish(dbSess.ProjectID, "session.state", snap)
		}
	}

	return nil
}

// UnarchiveSession clears the archive marker so the session returns to the
// sidebar's open list. The exact inverse of ArchiveSession — one field, no
// residue, whatever the session's state happens to be.
// Works for both live (idle) and non-live (stopped/failed) sessions.
func (s *Service) UnarchiveSession(ctx context.Context, sessionID string) error {
	if sess := s.mgr.Get(sessionID); sess != nil {
		return sess.Unarchive()
	}

	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}

	if err := s.queries.UnsetSessionArchived(ctx, sessionID); err != nil {
		return fmt.Errorf("unset session archived: %w", err)
	}

	if s.gitSvc != nil {
		if snap, err := s.gitSvc.computeGitSnapshot(ctx, sessionID); err == nil {
			s.hub.Publish(dbSess.ProjectID, "session.state", snap)
		}
	}

	return nil
}
