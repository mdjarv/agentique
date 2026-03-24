package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/gitops"
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
	mgr        *Manager
	queries    *store.Queries
	hub        Broadcaster
	activating sync.Map // guards concurrent activation of the same deferred session
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
// When Worktree=true with no explicit Branch, creation is deferred: only a DB
// record is inserted. The worktree and CLI connection are set up on first query
// (see activateSession).
func (s *Service) CreateSession(ctx context.Context, p CreateSessionParams) (CreateSessionResult, error) {
	project, err := s.queries.GetProject(ctx, p.ProjectID)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("project not found: %w", err)
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

	// Deferred worktree: insert DB record only, no worktree or CLI connection.
	// The worktree and CLI are created on first query when we have the prompt for naming.
	if p.Worktree && p.Branch == "" {
		return s.createDeferredSession(ctx, project.Path, p.ProjectID, name, model)
	}

	workDir := project.Path
	var worktreePath, worktreeBranch, worktreeBaseSHA string

	if p.Worktree {
		worktreeBranch = p.Branch
		worktreePath = gitops.WorktreePath(project.Name, p.Branch)

		baseSHA, shaErr := gitops.GetWorktreeBaseSHA(project.Path)
		if shaErr == nil {
			worktreeBaseSHA = baseSHA
		}

		if err := gitops.CreateWorktree(project.Path, p.Branch, worktreePath); err != nil {
			return CreateSessionResult{}, fmt.Errorf("failed to create worktree: %w", err)
		}
		workDir = worktreePath
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
			gitops.RemoveWorktree(project.Path, worktreePath)
		}
		return CreateSessionResult{}, fmt.Errorf("failed to create session: %w", err)
	}

	slog.Info("session created", "session_id", sess.ID, "project", project.Name, "model", model, "worktree", p.Worktree)

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

// createDeferredSession inserts a lightweight DB record for a worktree session.
// No worktree is created and no CLI connection is established.
func (s *Service) createDeferredSession(ctx context.Context, projectPath, projectID, name, model string) (CreateSessionResult, error) {
	id := uuid.New().String()

	dbSess, err := s.queries.CreateSession(ctx, store.CreateSessionParams{
		ID:                id,
		ProjectID:         projectID,
		Name:              name,
		WorkDir:           projectPath,
		State:             string(StateIdle),
		Model:             model,
		WorktreeRequested: 1,
	})
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("failed to create deferred session: %w", err)
	}

	slog.Info("deferred session created", "session_id", id, "project_id", projectID, "model", model)

	return CreateSessionResult{
		SessionID: id,
		Name:      name,
		State:     string(StateIdle),
		Model:     model,
		CreatedAt: dbSess.CreatedAt,
	}, nil
}

// QuerySession performs lazy resume if needed and delegates to session.Query.
// For deferred worktree sessions, this triggers activation (name generation,
// worktree creation, CLI connection) before the first query.
func (s *Service) QuerySession(ctx context.Context, sessionID, prompt string, attachments []QueryAttachment) error {
	sess := s.mgr.Get(sessionID)

	// CLI process dead — evict and resume with a fresh connection.
	if sess != nil && (sess.State() == StateDone || sess.State() == StateFailed) {
		slog.Debug("evicting dead session for resume", "session_id", sessionID, "state", string(sess.State()))
		s.mgr.Evict(sessionID)
		sess = nil
	}

	activated := false
	if sess == nil {
		dbSess, err := s.queries.GetSession(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		}

		if isDeferred(dbSess) {
			sess, err = s.activateSession(ctx, dbSess, prompt)
			if err != nil {
				return fmt.Errorf("activation failed: %w", err)
			}
			activated = true
		} else {
			sess, err = s.resumeSession(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrNotFound, err)
			}
		}
	}

	slog.Info("session query", "session_id", sessionID, "prompt_len", len(prompt), "attachments", len(attachments))

	if err := sess.Query(ctx, prompt, attachments); err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	// Skip auto-naming for activated sessions — they were already named during activation.
	if sess.QueryCount() == 1 && !activated {
		go s.autoName(sessionID, sess.ProjectID, prompt)
	}

	return nil
}

// isDeferred returns true if the session was created with deferred worktree
// and hasn't been activated yet.
func isDeferred(dbSess store.Session) bool {
	return dbSess.WorktreeRequested == 1 &&
		nullStr(dbSess.ClaudeSessionID) == "" &&
		nullStr(dbSess.WorktreeBranch) == ""
}

// activateSession performs deferred worktree setup: generates a name via Haiku,
// creates the worktree with a kebab-case branch, connects CLI, and updates DB.
func (s *Service) activateSession(ctx context.Context, dbSess store.Session, prompt string) (*Session, error) {
	// Concurrency guard — prevent two clients racing on the same session.
	if _, loaded := s.activating.LoadOrStore(dbSess.ID, true); loaded {
		return nil, fmt.Errorf("session %s is already being activated", dbSess.ID)
	}
	defer s.activating.Delete(dbSess.ID)

	project, err := s.queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	// Broadcast "starting" so the frontend shows immediate feedback.
	s.hub.Broadcast(dbSess.ProjectID, "session.state", map[string]any{
		"sessionId": dbSess.ID,
		"state":     "starting",
	})

	// Generate name via Haiku.
	name := generateSessionName(prompt)
	branch := ""
	if name != "" {
		branch = gitops.ToKebabCase(name)
	} else {
		// Fallback: keep placeholder name, use UUID-based branch.
		name = dbSess.Name
		branch = "session-" + uuid.New().String()[:8]
	}

	// Create worktree.
	worktreePath := gitops.WorktreePath(project.Name, branch)
	baseSHA, _ := gitops.GetWorktreeBaseSHA(project.Path)

	if err := gitops.CreateWorktree(project.Path, branch, worktreePath); err != nil {
		// Branch collision — retry with UUID suffix.
		branch = branch + "-" + uuid.New().String()[:4]
		worktreePath = gitops.WorktreePath(project.Name, branch)
		if err := gitops.CreateWorktree(project.Path, branch, worktreePath); err != nil {
			return nil, fmt.Errorf("failed to create worktree: %w", err)
		}
	}

	// Update DB with worktree info and name.
	if err := s.queries.UpdateSessionWorktree(ctx, store.UpdateSessionWorktreeParams{
		Name:    name,
		WorkDir: worktreePath,
		WorktreePath: sql.NullString{
			String: worktreePath,
			Valid:  true,
		},
		WorktreeBranch: sql.NullString{
			String: branch,
			Valid:  true,
		},
		WorktreeBaseSha: sql.NullString{
			String: baseSHA,
			Valid:  baseSHA != "",
		},
		ID: dbSess.ID,
	}); err != nil {
		gitops.RemoveWorktree(project.Path, worktreePath)
		return nil, fmt.Errorf("db update failed: %w", err)
	}

	// Connect CLI via Manager (reuse existing ID, skip DB insert).
	sess, err := s.mgr.Create(ctx, CreateParams{
		ID:              dbSess.ID,
		ProjectID:       dbSess.ProjectID,
		WorkDir:         worktreePath,
		WorktreePath:    worktreePath,
		WorktreeBranch:  branch,
		WorktreeBaseSHA: baseSHA,
		Model:           dbSess.Model,
	})
	if err != nil {
		gitops.RemoveWorktree(project.Path, worktreePath)
		return nil, fmt.Errorf("failed to connect CLI: %w", err)
	}

	slog.Info("deferred session activated", "session_id", dbSess.ID, "name", name, "branch", branch)

	// Broadcast the real name and branch to all clients.
	s.hub.Broadcast(dbSess.ProjectID, "session.renamed", map[string]any{
		"sessionId":      dbSess.ID,
		"name":           name,
		"worktreeBranch": branch,
	})

	return sess, nil
}

// StopSession stops a live session and cleans up its worktree.
func (s *Service) StopSession(ctx context.Context, sessionID string) error {
	slog.Info("stopping session", "session_id", sessionID)

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
			gitops.RemoveWorktree(project.Path, wtPath)
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
	slog.Info("deleting session", "session_id", sessionID)

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
			gitops.RemoveWorktree(project.Path, wtPath)
		}
		if branch := nullStr(dbSess.WorktreeBranch); branch != "" {
			if delErr := gitops.DeleteBranch(project.Path, branch); delErr != nil {
				slog.Warn("branch delete failed", "session_id", sessionID, "error", delErr)
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
	slog.Debug("session model changed", "session_id", sessionID, "model", model)
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

	slog.Debug("resuming session", "session_id", sessionID, "claude_session_id", claudeSessID)

	workDir := dbSess.WorkDir
	if _, statErr := os.Stat(workDir); statErr != nil {
		project, projErr := s.queries.GetProject(ctx, dbSess.ProjectID)
		if projErr != nil {
			return nil, fmt.Errorf("project not found: %w", projErr)
		}
		if branch := nullStr(dbSess.WorktreeBranch); branch != "" {
			if err := gitops.RestoreWorktree(project.Path, branch, nullStr(dbSess.WorktreePath)); err != nil {
				slog.Warn("worktree restore failed, falling back to project root", "session_id", sessionID, "error", err)
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
		slog.Warn("auto-rename db update failed", "session_id", sessionID, "error", err)
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
		slog.Warn("auto-rename haiku failed", "error", err)
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
