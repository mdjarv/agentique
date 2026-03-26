package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

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
	ID              string  `json:"id"`
	ProjectID       string  `json:"projectId"`
	Name            string  `json:"name"`
	State           string  `json:"state"`
	Connected       bool    `json:"connected"`
	Model           string  `json:"model"`
	PermissionMode  string  `json:"permissionMode"`
	AutoApprove     bool    `json:"autoApprove"`
	Effort          string  `json:"effort,omitempty"`
	MaxBudget       float64 `json:"maxBudget,omitempty"`
	MaxTurns        int     `json:"maxTurns,omitempty"`
	TotalCost       float64 `json:"totalCost"`
	TurnCount       int     `json:"turnCount"`
	WorktreePath    string  `json:"worktreePath,omitempty"`
	WorktreeBranch  string  `json:"worktreeBranch,omitempty"`
	WorktreeMerged  bool    `json:"worktreeMerged,omitempty"`
	CompletedAt     string  `json:"completedAt,omitempty"`
	HasDirtyWorktree   bool     `json:"hasDirtyWorktree,omitempty"`
	HasUncommitted     bool     `json:"hasUncommitted,omitempty"`
	CommitsAhead       int      `json:"commitsAhead"`
	CommitsBehind      int      `json:"commitsBehind"`
	BranchMissing      bool     `json:"branchMissing,omitempty"`
	MergeStatus        string   `json:"mergeStatus,omitempty"`
	MergeConflictFiles []string `json:"mergeConflictFiles,omitempty"`
	GitOperation       string   `json:"gitOperation,omitempty"`
	GitVersion         int64    `json:"gitVersion"`
	PrUrl              string   `json:"prUrl,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	LastQueryAt     string  `json:"lastQueryAt,omitempty"`
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
	Effort      string
	MaxBudget   float64
	MaxTurns    int
}

// CreateSessionResult is the wire type returned after session creation.
type CreateSessionResult struct {
	SessionID      string  `json:"sessionId"`
	Name           string  `json:"name"`
	State          string  `json:"state"`
	Connected      bool    `json:"connected"`
	Model          string  `json:"model"`
	PermissionMode string  `json:"permissionMode"`
	AutoApprove    bool    `json:"autoApprove"`
	Effort         string  `json:"effort,omitempty"`
	MaxBudget      float64 `json:"maxBudget,omitempty"`
	MaxTurns       int     `json:"maxTurns,omitempty"`
	WorktreePath   string  `json:"worktreePath,omitempty"`
	WorktreeBranch string  `json:"worktreeBranch,omitempty"`
	CreatedAt      string  `json:"createdAt"`
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
	queries serviceQueries
	hub     Broadcaster
	gitSvc  *GitService
}

// NewService creates a new session Service.
func NewService(mgr *Manager, queries serviceQueries, hub Broadcaster) *Service {
	return &Service{mgr: mgr, queries: queries, hub: hub}
}

// SetGitService injects the GitService for snapshot broadcasts.
// Called after both Service and GitService are constructed.
func (s *Service) SetGitService(gs *GitService) {
	s.gitSvc = gs
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

	sessionID := uuid.New().String()
	workDir := project.Path
	var worktreePath, worktreeBranch string

	var worktreeBaseSHA string
	if p.Worktree {
		branch := p.Branch
		if branch == "" {
			branch = "session-" + sessionID[:8]
		}
		worktreeBranch = branch
		worktreePath = gitops.WorktreePath(project.Name, branch)

		baseSHA, shaErr := gitops.GetWorktreeBaseSHA(project.Path)
		if shaErr == nil {
			worktreeBaseSHA = baseSHA
		}

		if err := gitops.CreateWorktree(project.Path, branch, worktreePath); err != nil {
			return CreateSessionResult{}, fmt.Errorf("failed to create worktree: %w", err)
		}
		workDir = worktreePath
	}

	name := p.Name

	model := p.Model
	if model == "" {
		model = "opus"
	}

	sess, err := s.mgr.Create(ctx, CreateParams{
		ID:              sessionID,
		ProjectID:       p.ProjectID,
		Name:            name,
		WorkDir:         workDir,
		WorktreePath:    worktreePath,
		WorktreeBranch:  worktreeBranch,
		WorktreeBaseSHA: worktreeBaseSHA,
		Model:           model,
		PlanMode:        p.PlanMode,
		AutoApprove:     p.AutoApprove,
		Effort:          p.Effort,
		MaxBudget:       p.MaxBudget,
		MaxTurns:        p.MaxTurns,
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
		Connected:      true,
		Model:          model,
		PermissionMode: sess.PermissionMode(),
		AutoApprove:    sess.AutoApprove(),
		Effort:         p.Effort,
		MaxBudget:      p.MaxBudget,
		MaxTurns:       p.MaxTurns,
		WorktreePath:   worktreePath,
		WorktreeBranch: worktreeBranch,
		CreatedAt:      createdAt,
	}, nil
}

// QuerySession performs lazy resume if needed and delegates to session.Query.
func (s *Service) QuerySession(ctx context.Context, sessionID, prompt string, attachments []QueryAttachment) error {
	sess := s.mgr.Get(sessionID)

	// CLI process dead — evict and resume with a fresh connection.
	if sess != nil && (sess.State() == StateDone || sess.State() == StateFailed) {
		slog.Debug("evicting dead session for resume", "session_id", sessionID, "state", string(sess.State()))
		s.mgr.Evict(sessionID)
		sess = nil
	}

	if sess == nil {
		var err error
		sess, err = s.resumeSession(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		}
	}

	slog.Info("session query", "session_id", sessionID, "prompt_len", len(prompt), "attachments", len(attachments))

	if err := sess.Query(ctx, prompt, attachments); err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	_ = s.queries.UpdateSessionLastQueryAt(ctx, sessionID)

	if sess.QueryCount() == 1 {
		go s.autoName(sessionID, sess.ProjectID, prompt)
	}

	return nil
}

// StopSession stops a live session. The worktree is preserved so the session
// can be resumed later. Worktree cleanup happens only on DeleteSession or
// Merge with cleanup=true.
func (s *Service) StopSession(ctx context.Context, sessionID string) error {
	slog.Info("stopping session", "session_id", sessionID)

	// Preserve the git version counter so a future resume continues from
	// the correct version instead of resetting to 0.
	if live := s.mgr.Get(sessionID); live != nil && s.gitSvc != nil {
		s.gitSvc.SeedVersion(sessionID, live.GitVersion())
	}

	if err := s.mgr.Stop(ctx, sessionID); err != nil {
		return fmt.Errorf("stop failed: %w", err)
	}
	return nil
}

// costSummary holds cost/turn data for a session (unifies sqlc row types).
type costSummary struct {
	TurnCount int64
	TotalCost float64
}

// ListSessions returns session info for a project.
func (s *Service) ListSessions(ctx context.Context, projectID string) (ListSessionsResult, error) {
	sessions, err := s.mgr.ListByProject(ctx, projectID)
	if err != nil {
		return ListSessionsResult{}, err
	}

	costMap := make(map[string]costSummary)
	if summaries, err := s.queries.SessionSummariesByProject(ctx, projectID); err == nil {
		for _, row := range summaries {
			costMap[row.SessionID] = costSummary{TurnCount: row.TurnCount, TotalCost: row.TotalCost}
		}
	}

	projectPaths := make(map[string]string)
	if project, err := s.queries.GetProject(ctx, projectID); err == nil {
		projectPaths[projectID] = project.Path
	}

	infos := s.enrichSessions(sessions, costMap, projectPaths)
	return ListSessionsResult{Sessions: infos}, nil
}

// ListAllSessions returns session info across all projects.
func (s *Service) ListAllSessions(ctx context.Context) (ListSessionsResult, error) {
	sessions, err := s.mgr.ListAll(ctx)
	if err != nil {
		return ListSessionsResult{}, err
	}

	costMap := make(map[string]costSummary)
	if summaries, err := s.queries.AllSessionSummaries(ctx); err == nil {
		for _, row := range summaries {
			costMap[row.SessionID] = costSummary{TurnCount: row.TurnCount, TotalCost: row.TotalCost}
		}
	}

	projectPaths := s.resolveProjectPaths(ctx, sessions)
	infos := s.enrichSessions(sessions, costMap, projectPaths)
	return ListSessionsResult{Sessions: infos}, nil
}

// GetSessionInfo returns info for a single session.
func (s *Service) GetSessionInfo(ctx context.Context, sessionID string) (SessionInfo, error) {
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return SessionInfo{}, ErrNotFound
	}

	costMap := make(map[string]costSummary)
	if summaries, err := s.queries.SessionSummariesByProject(ctx, dbSess.ProjectID); err == nil {
		for _, row := range summaries {
			if row.SessionID == sessionID {
				costMap[sessionID] = costSummary{TurnCount: row.TurnCount, TotalCost: row.TotalCost}
				break
			}
		}
	}

	// Prefer live in-memory state over DB (may lag due to async persistence).
	if live := s.mgr.Get(sessionID); live != nil {
		dbSess.State = string(live.State())
	}

	projectPaths := make(map[string]string)
	if project, err := s.queries.GetProject(ctx, dbSess.ProjectID); err == nil {
		projectPaths[dbSess.ProjectID] = project.Path
	}

	infos := s.enrichSessions([]store.Session{dbSess}, costMap, projectPaths)
	return infos[0], nil
}

// enrichSessions converts store.Session rows into SessionInfo with cost and git data.
func (s *Service) enrichSessions(sessions []store.Session, costMap map[string]costSummary, projectPaths map[string]string) []SessionInfo {
	infos := make([]SessionInfo, 0, len(sessions))
	for _, ss := range sessions {
		info := SessionInfo{
			ID:             ss.ID,
			ProjectID:      ss.ProjectID,
			Name:           ss.Name,
			State:          ss.State,
			Connected:      s.mgr.IsLive(ss.ID),
			Model:          ss.Model,
			PermissionMode: ss.PermissionMode,
			AutoApprove:    ss.AutoApprove != 0,
			Effort:         ss.Effort,
			MaxBudget:      ss.MaxBudget,
			MaxTurns:       int(ss.MaxTurns),
			WorktreePath:   nullStr(ss.WorktreePath),
			WorktreeBranch: nullStr(ss.WorktreeBranch),
			WorktreeMerged: ss.WorktreeMerged != 0,
			CompletedAt:    nullStr(ss.CompletedAt),
			PrUrl:          ss.PrUrl,
			CreatedAt:      ss.CreatedAt,
			UpdatedAt:      ss.UpdatedAt,
			LastQueryAt:    nullStr(ss.LastQueryAt),
		}

		if summary, ok := costMap[ss.ID]; ok {
			info.TotalCost = summary.TotalCost
			info.TurnCount = int(summary.TurnCount)
		}

		// Stamp live session fields so the frontend has current state.
		if live := s.mgr.Get(ss.ID); live != nil {
			info.GitVersion = live.GitVersion()
			_, _, _, _, gitOp := live.liveState()
			info.GitOperation = gitOp
		} else if s.gitSvc != nil {
			info.GitVersion = s.gitSvc.LastVersion(ss.ID)
		}

		if branch := info.WorktreeBranch; branch != "" && !info.WorktreeMerged {
			if projectPath, ok := projectPaths[ss.ProjectID]; ok {
				if gitops.BranchExists(projectPath, branch) {
					info.CommitsAhead, _ = gitops.CommitsAhead(projectPath, branch)
					info.CommitsBehind, _ = gitops.CommitsBehind(projectPath, branch)
					if info.WorktreePath != "" {
						dirty, _ := gitops.HasUncommittedChanges(info.WorktreePath)
						info.HasUncommitted = dirty
						info.HasDirtyWorktree = dirty
					}
					result, mergeErr := gitops.MergeTreeCheck(projectPath, branch)
					if mergeErr != nil {
						info.MergeStatus = "unknown"
					} else if result.Clean {
						info.MergeStatus = "clean"
					} else {
						info.MergeStatus = "conflicts"
						info.MergeConflictFiles = result.ConflictFiles
					}
				} else {
					info.BranchMissing = true
				}
			}
		}

		infos = append(infos, info)
	}
	return infos
}

// resolveProjectPaths builds a projectID -> path map for sessions.
func (s *Service) resolveProjectPaths(ctx context.Context, sessions []store.Session) map[string]string {
	paths := make(map[string]string)
	for _, ss := range sessions {
		if _, ok := paths[ss.ProjectID]; ok {
			continue
		}
		if project, err := s.queries.GetProject(ctx, ss.ProjectID); err == nil {
			paths[ss.ProjectID] = project.Path
		}
	}
	return paths
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

	// Merged sessions already had their worktree/branch cleaned up during merge.
	if dbSess.WorktreeMerged == 0 {
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
	}

	if err := s.queries.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("db delete failed: %w", err)
	}

	if s.gitSvc != nil {
		s.gitSvc.CleanupVersion(sessionID)
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
	if err := s.queries.UpdateSessionModel(ctx, store.UpdateSessionModelParams{
		Model: model,
		ID:    sessionID,
	}); err != nil {
		return newPersistError("update session model", err)
	}
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

// SetAutoApprove enables or disables automatic tool approval for a session and persists it.
func (s *Service) SetAutoApprove(sessionID string, enabled bool) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	sess.SetAutoApprove(enabled)
	var v int64
	if enabled {
		v = 1
	}
	if err := s.queries.UpdateSessionAutoApprove(context.Background(), store.UpdateSessionAutoApproveParams{
		AutoApprove: v,
		ID:          sessionID,
	}); err != nil {
		return newPersistError("update auto-approve", err)
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
				return nil, fmt.Errorf("worktree restore failed for branch %s: %w", branch, err)
			}
		} else {
			workDir = project.Path
		}
	}

	var initialVersion int64
	if s.gitSvc != nil {
		initialVersion = s.gitSvc.LastVersion(sessionID)
	}

	sess, resumeErr := s.mgr.Resume(ctx, ResumeParams{
		SessionID:         sessionID,
		ClaudeSessionID:   claudeSessID,
		ProjectID:         dbSess.ProjectID,
		WorkDir:           workDir,
		Model:             dbSess.Model,
		PermissionMode:    dbSess.PermissionMode,
		AutoApprove:       dbSess.AutoApprove != 0,
		Effort:            dbSess.Effort,
		MaxBudget:         dbSess.MaxBudget,
		MaxTurns:          int(dbSess.MaxTurns),
		InitialGitVersion: initialVersion,
	})
	if resumeErr != nil {
		return nil, resumeErr
	}
	if dbSess.WorktreeMerged != 0 {
		sess.MarkMerged()
	}
	if dbSess.CompletedAt.Valid {
		sess.MarkCompleted()
	}
	return sess, nil
}

// RenameSession updates the session name in DB and broadcasts the change.
func (s *Service) RenameSession(ctx context.Context, sessionID, name string) error {
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}
	if err := s.queries.UpdateSessionName(ctx, store.UpdateSessionNameParams{
		Name: name,
		ID:   sessionID,
	}); err != nil {
		return fmt.Errorf("rename failed: %w", err)
	}
	s.hub.Broadcast(dbSess.ProjectID, "session.renamed", map[string]any{
		"sessionId": sessionID,
		"name":      name,
	})
	return nil
}

// autoName calls Haiku to generate a short title and broadcasts the rename.
// Skips if the session already has a user-provided name (e.g. from a prompt block).
func (s *Service) autoName(sessionID, projectID, prompt string) {
	dbSess, err := s.queries.GetSession(context.Background(), sessionID)
	if err == nil && dbSess.Name != "" {
		return
	}

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
