package session

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/store"
)

// SessionInfo is the wire type for session metadata sent to clients.
type SessionInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	State          string `json:"state"`
	WorktreePath   string `json:"worktreePath,omitempty"`
	WorktreeBranch string `json:"worktreeBranch,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

// CreateSessionParams holds client-provided parameters for creating a session.
type CreateSessionParams struct {
	ProjectID string
	Name      string
	Worktree  bool
	Branch    string
	RequestID string // used as fallback branch name suffix
}

// CreateSessionResult is the wire type returned after session creation.
type CreateSessionResult struct {
	SessionID      string `json:"sessionId"`
	Name           string `json:"name"`
	State          string `json:"state"`
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

// Service encapsulates all session business logic.
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

// CreateSession validates the project, creates a worktree if requested,
// generates a default name, calls mgr.Create, and cleans up on failure.
func (s *Service) CreateSession(ctx context.Context, p CreateSessionParams) (CreateSessionResult, error) {
	project, err := s.queries.GetProject(ctx, p.ProjectID)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("project not found")
	}

	workDir := project.Path
	var worktreePath, worktreeBranch string

	var worktreeBaseSHA string
	if p.Worktree {
		branch := p.Branch
		if branch == "" {
			branch = "session-" + p.RequestID
			if len(branch) > 30 {
				branch = branch[:30]
			}
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

	sess, err := s.mgr.Create(ctx, CreateParams{
		ProjectID:       p.ProjectID,
		Name:            name,
		WorkDir:         workDir,
		WorktreePath:    worktreePath,
		WorktreeBranch:  worktreeBranch,
		WorktreeBaseSHA: worktreeBaseSHA,
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
		State:          sess.State(),
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
			return fmt.Errorf("session not found: %w", err)
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

// StopSession delegates to mgr.Stop.
func (s *Service) StopSession(ctx context.Context, sessionID string) error {
	return s.mgr.Stop(ctx, sessionID)
}

// ListSessions returns session info for a project.
func (s *Service) ListSessions(ctx context.Context, projectID string) (ListSessionsResult, error) {
	sessions, err := s.mgr.ListByProject(ctx, projectID)
	if err != nil {
		return ListSessionsResult{}, err
	}

	infos := make([]SessionInfo, 0, len(sessions))
	for _, ss := range sessions {
		info := SessionInfo{
			ID:        ss.ID,
			Name:      ss.Name,
			State:     ss.State,
			CreatedAt: ss.CreatedAt,
		}
		if ss.WorktreePath.Valid {
			info.WorktreePath = ss.WorktreePath.String
		}
		if ss.WorktreeBranch.Valid {
			info.WorktreeBranch = ss.WorktreeBranch.String
		}
		infos = append(infos, info)
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

// GetDiff returns the diff of a worktree session against its base commit.
func (s *Service) GetDiff(ctx context.Context, sessionID string) (DiffResult, error) {
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return DiffResult{}, fmt.Errorf("session not found")
	}
	if !dbSess.WorktreePath.Valid || dbSess.WorktreePath.String == "" {
		return DiffResult{}, fmt.Errorf("session has no worktree")
	}
	if _, statErr := os.Stat(dbSess.WorktreePath.String); statErr != nil {
		return DiffResult{}, fmt.Errorf("worktree directory not found")
	}

	baseSHA := ""
	if dbSess.WorktreeBaseSha.Valid {
		baseSHA = dbSess.WorktreeBaseSha.String
	}

	return WorktreeDiff(dbSess.WorktreePath.String, baseSHA)
}

// resumeSession attempts to resume a non-live session from its Claude session ID.
func (s *Service) resumeSession(ctx context.Context, sessionID string) (*Session, error) {
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not in DB")
	}
	if !dbSess.ClaudeSessionID.Valid || dbSess.ClaudeSessionID.String == "" {
		return nil, fmt.Errorf("session has no Claude session ID")
	}

	workDir := dbSess.WorkDir
	if _, statErr := os.Stat(workDir); statErr != nil {
		project, projErr := s.queries.GetProject(ctx, dbSess.ProjectID)
		if projErr != nil {
			return nil, fmt.Errorf("project not found")
		}
		if dbSess.WorktreeBranch.Valid && dbSess.WorktreeBranch.String != "" {
			if err := RestoreWorktree(project.Path, dbSess.WorktreeBranch.String, dbSess.WorktreePath.String); err != nil {
				log.Printf("session %s: worktree restore failed, falling back to project root: %v", sessionID, err)
				workDir = project.Path
			}
		} else {
			workDir = project.Path
		}
	}

	return s.mgr.Resume(ctx, sessionID, dbSess.ClaudeSessionID.String, dbSess.ProjectID, workDir)
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
