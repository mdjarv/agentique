package session

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// Sentinel errors for session operations.
var (
	ErrNotFound   = errors.New("session not found")
	ErrNotLive    = errors.New("session not live")
	ErrNoClaudeID = errors.New("session has no Claude session ID")
)

// SessionInfo is the wire type for session metadata sent to clients.
type SessionInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	State          string `json:"state"`
	Model          string `json:"model"`
	WorktreePath   string `json:"worktreePath,omitempty"`
	WorktreeBranch string `json:"worktreeBranch,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

// CreateSessionParams holds client-provided parameters for creating a session.
type CreateSessionParams struct {
	ProjectID   string
	Name        string
	Worktree    bool
	Branch      string
	Model       string
	PlanMode    bool
	AutoApprove bool
	RequestID   string // used as fallback branch name suffix
}

// CreateSessionResult is the wire type returned after session creation.
type CreateSessionResult struct {
	SessionID      string `json:"sessionId"`
	Name           string `json:"name"`
	State          string `json:"state"`
	Model          string `json:"model"`
	WorktreePath   string `json:"worktreePath,omitempty"`
	WorktreeBranch string `json:"worktreeBranch,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

// ListSessionsResult is the wire type for session list responses.
type ListSessionsResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

// HistoryResult is the wire type for session history responses.
type HistoryResult struct {
	Turns []HistoryTurn `json:"turns"`
}

// Service encapsulates session lifecycle business logic.
type Service struct {
	mgr     *Manager
	queries *store.Queries
	hub     Broadcaster
}

// NewService creates a new session Service.
func NewService(mgr *Manager, queries *store.Queries, hub Broadcaster) *Service {
	return &Service{mgr: mgr, queries: queries, hub: hub}
}

// Manager returns the underlying Manager (needed for CloseAll on shutdown).
func (s *Service) Manager() *Manager {
	return s.mgr
}

// getLiveSession returns a live session or ErrNotLive.
func (s *Service) getLiveSession(sessionID string) (*Session, error) {
	sess := s.mgr.Get(sessionID)
	if sess == nil {
		return nil, ErrNotLive
	}
	return sess, nil
}

// CreateSession validates the project, creates a worktree if requested,
// generates a default name, calls mgr.Create, and cleans up on failure.
func (s *Service) CreateSession(ctx context.Context, p CreateSessionParams) (CreateSessionResult, error) {
	project, err := s.queries.GetProject(ctx, p.ProjectID)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("project not found: %w", err)
	}

	workDir := project.Path
	var worktreePath, worktreeBranch string

	var worktreeBaseSHA string
	if p.Worktree {
		branch := p.Branch
		if branch == "" {
			suffix := uuid.New().String()[:8]
			branch = "session-" + suffix
		}
		worktreeBranch = branch
		worktreePath = WorktreePath(project.Name, branch)

		baseSHA, shaErr := GetWorktreeBaseSHA(project.Path)
		if shaErr == nil {
			worktreeBaseSHA = baseSHA
		}

		if err := CreateWorktree(project.Path, branch, worktreePath); err != nil {
			return CreateSessionResult{}, fmt.Errorf("failed to create worktree: %w", err)
		}
		workDir = worktreePath
	}

	name := p.Name
	if name == "" {
		sessions, listErr := s.mgr.ListByProject(ctx, p.ProjectID)
		count := 0
		if listErr == nil {
			count = len(sessions)
		}
		name = fmt.Sprintf("Session %d", count+1)
	}

	model := p.Model
	if model == "" {
		model = "opus"
	}

	sess, err := s.mgr.Create(ctx, CreateParams{
		ProjectID:       p.ProjectID,
		Name:            name,
		WorkDir:         workDir,
		WorktreePath:    worktreePath,
		WorktreeBranch:  worktreeBranch,
		WorktreeBaseSHA: worktreeBaseSHA,
		Model:           model,
		PlanMode:        p.PlanMode,
		AutoApprove:     p.AutoApprove,
	})
	if err != nil {
		if worktreePath != "" {
			RemoveWorktree(project.Path, worktreePath)
		}
		return CreateSessionResult{}, fmt.Errorf("failed to create session: %w", err)
	}

	dbSess, dbErr := s.queries.GetSession(ctx, sess.ID)
	createdAt := ""
	if dbErr == nil {
		createdAt = dbSess.CreatedAt
	}

	return CreateSessionResult{
		SessionID:      sess.ID,
		Name:           name,
		State:          string(sess.State()),
		Model:          model,
		WorktreePath:   worktreePath,
		WorktreeBranch: worktreeBranch,
		CreatedAt:      createdAt,
	}, nil
}

// QuerySession performs lazy resume if needed and delegates to session.Query.
func (s *Service) QuerySession(ctx context.Context, sessionID, prompt string, attachments []QueryAttachment) error {
	sess := s.mgr.Get(sessionID)

	if sess == nil {
		var err error
		sess, err = s.resumeSession(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		}
	}

	if err := sess.Query(ctx, prompt, attachments); err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if sess.QueryCount() == 1 {
		go s.autoName(sessionID, sess.ProjectID, prompt)
	}

	return nil
}

// StopSession stops a live session and cleans up its worktree.
func (s *Service) StopSession(ctx context.Context, sessionID string) error {
	if err := s.mgr.Stop(ctx, sessionID); err != nil {
		return fmt.Errorf("stop failed: %w", err)
	}

	// Clean up worktree now that the session is stopped.
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return nil // session stopped, DB lookup failure is non-fatal
	}
	if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
		project, projErr := s.queries.GetProject(ctx, dbSess.ProjectID)
		if projErr == nil {
			RemoveWorktree(project.Path, wtPath)
		}
	}
	return nil
}

// ListSessions returns session info for a project.
func (s *Service) ListSessions(ctx context.Context, projectID string) (ListSessionsResult, error) {
	sessions, err := s.mgr.ListByProject(ctx, projectID)
	if err != nil {
		return ListSessionsResult{}, err
	}

	infos := make([]SessionInfo, 0, len(sessions))
	for _, ss := range sessions {
		infos = append(infos, SessionInfo{
			ID:             ss.ID,
			Name:           ss.Name,
			State:          ss.State,
			Model:          ss.Model,
			WorktreePath:   nullStr(ss.WorktreePath),
			WorktreeBranch: nullStr(ss.WorktreeBranch),
			CreatedAt:      ss.CreatedAt,
		})
	}

	return ListSessionsResult{Sessions: infos}, nil
}

// GetHistory returns turn history for a session.
func (s *Service) GetHistory(ctx context.Context, sessionID string) (HistoryResult, error) {
	turns, err := HistoryFromDB(ctx, s.queries, sessionID)
	if err != nil {
		return HistoryResult{}, err
	}
	return HistoryResult{Turns: turns}, nil
}

// DeleteSession stops a live session, removes its worktree/branch, and deletes from DB.
func (s *Service) DeleteSession(ctx context.Context, sessionID string) error {
	if live := s.mgr.Get(sessionID); live != nil {
		_ = s.mgr.Stop(ctx, sessionID)
	}

	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}

	project, projErr := s.queries.GetProject(ctx, dbSess.ProjectID)
	if projErr == nil {
		if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
			RemoveWorktree(project.Path, wtPath)
		}
		if branch := nullStr(dbSess.WorktreeBranch); branch != "" {
			if delErr := DeleteBranch(project.Path, branch); delErr != nil {
				log.Printf("session %s: branch delete: %v", sessionID, delErr)
			}
		}
	}

	if err := s.queries.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("db delete failed: %w", err)
	}

	s.hub.Broadcast(dbSess.ProjectID, "session.deleted", map[string]any{
		"sessionId": sessionID,
	})

	return nil
}

// SetSessionModel changes the model for a live session.
func (s *Service) SetSessionModel(ctx context.Context, sessionID, model string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	if err := sess.SetModel(model); err != nil {
		return err
	}
	_ = s.queries.UpdateSessionModel(ctx, store.UpdateSessionModelParams{
		Model: model,
		ID:    sessionID,
	})
	return nil
}

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

// SetPermissionMode changes the permission mode for a live session.
func (s *Service) SetPermissionMode(sessionID, mode string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	return sess.SetPermissionMode(mode)
}

// SetAutoApprove enables or disables automatic tool approval for a session.
func (s *Service) SetAutoApprove(sessionID string, enabled bool) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	sess.SetAutoApprove(enabled)
	return nil
}

// InterruptSession stops the current generation without killing the session.
func (s *Service) InterruptSession(sessionID string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	return sess.Interrupt()
}

// resumeSession attempts to resume a non-live session from its Claude session ID.
func (s *Service) resumeSession(ctx context.Context, sessionID string) (*Session, error) {
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return nil, ErrNotFound
	}
	claudeSessID := nullStr(dbSess.ClaudeSessionID)
	if claudeSessID == "" {
		return nil, ErrNoClaudeID
	}

	workDir := dbSess.WorkDir
	if _, statErr := os.Stat(workDir); statErr != nil {
		project, projErr := s.queries.GetProject(ctx, dbSess.ProjectID)
		if projErr != nil {
			return nil, fmt.Errorf("project not found: %w", projErr)
		}
		if branch := nullStr(dbSess.WorktreeBranch); branch != "" {
			if err := RestoreWorktree(project.Path, branch, nullStr(dbSess.WorktreePath)); err != nil {
				log.Printf("session %s: worktree restore failed, falling back to project root: %v", sessionID, err)
				workDir = project.Path
			}
		} else {
			workDir = project.Path
		}
	}

	return s.mgr.Resume(ctx, sessionID, claudeSessID, dbSess.ProjectID, workDir, dbSess.Model)
}

// autoName calls Haiku to generate a short title and broadcasts the rename.
func (s *Service) autoName(sessionID, projectID, prompt string) {
	name := generateSessionName(prompt)
	if name == "" {
		return
	}
	if err := s.queries.UpdateSessionName(context.Background(), store.UpdateSessionNameParams{
		Name: name,
		ID:   sessionID,
	}); err != nil {
		log.Printf("auto-rename db update error: %v", err)
		return
	}
	s.hub.Broadcast(projectID, "session.renamed", map[string]any{
		"sessionId": sessionID,
		"name":      name,
	})
}

// generateSessionName calls Haiku to generate a short title from a prompt.
func generateSessionName(prompt string) string {
	p := prompt
	if len(p) > 500 {
		p = p[:500]
	}
	namePrompt := "Generate a short 2-4 word title for this coding task. " +
		"Respond with ONLY the title, no quotes or punctuation:\n\n" + p

	client := claudecli.New()
	result, err := client.RunBlocking(context.Background(), namePrompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
	)
	if err != nil {
		log.Printf("auto-rename haiku error: %v", err)
		return ""
	}

	name := strings.TrimSpace(result.Text)
	name = strings.Trim(name, "\"'")
	if name == "" {
		return ""
	}
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}
