package session

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/allbin/agentique/backend/internal/store"
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

// MarkSessionDone transitions a session to StateDone.
// Works for both live (idle) and non-live (stopped/failed) sessions.
func (s *Service) MarkSessionDone(ctx context.Context, sessionID string) error {
	if sess := s.mgr.Get(sessionID); sess != nil {
		return sess.MarkDone()
	}

	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}

	from := State(dbSess.State)
	if err := validateTransition(from, StateDone, sessionID); err != nil {
		return err
	}

	if err := s.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: string(StateDone),
		ID:    sessionID,
	}); err != nil {
		return fmt.Errorf("update state failed: %w", err)
	}
	if err := s.queries.SetSessionCompleted(ctx, sessionID); err != nil {
		slog.Warn("persist session completed failed", "session_id", sessionID, "error", err)
	}

	if s.gitSvc != nil {
		if snap, err := s.gitSvc.computeGitSnapshot(ctx, sessionID); err == nil {
			s.hub.Broadcast(dbSess.ProjectID, "session.state", snap)
		}
	}

	return nil
}
