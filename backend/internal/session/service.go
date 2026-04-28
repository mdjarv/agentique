package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/msggen"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// Sentinel errors for session operations.
var (
	ErrNotFound          = errors.New("session not found")
	ErrNotLive           = errors.New("session not live")
	ErrNoClaudeID        = errors.New("session has no Claude session ID")
	ErrSessionLimitReached = errors.New("project session limit reached")
)

// WireQuestionOption is a selectable option within a question.
type WireQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// WireQuestion is a single question in an AskUserQuestion request.
type WireQuestion struct {
	Question    string             `json:"question"`
	Header      string             `json:"header,omitempty"`
	Options     []WireQuestionOption `json:"options,omitempty"`
	MultiSelect bool               `json:"multiSelect,omitempty"`
}

// WirePendingApproval is a snapshot of a pending tool-permission request.
type WirePendingApproval struct {
	ApprovalID string          `json:"approvalId"`
	ToolName   string          `json:"toolName"`
	Input      json.RawMessage `json:"input"`
}

// WirePendingQuestion is a snapshot of a pending AskUserQuestion request.
type WirePendingQuestion struct {
	QuestionID string         `json:"questionId"`
	Questions  []WireQuestion `json:"questions"`
}

// SessionInfo is the wire type for session metadata sent to clients.
type SessionInfo struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"projectId"`
	Name            string  `json:"name"`
	State           string  `json:"state"`
	Connected       bool    `json:"connected"`
	Model           string  `json:"model"`
	PermissionMode  string  `json:"permissionMode"`
	AutoApproveMode string  `json:"autoApproveMode"`
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
	BehaviorPresets    BehaviorPresets      `json:"behaviorPresets"`
	ChannelIDs         []string             `json:"channelIds,omitempty"`
	ChannelRoles       map[string]string    `json:"channelRoles,omitempty"`
	Icon               string               `json:"icon,omitempty"`
	PendingApproval    *WirePendingApproval `json:"pendingApproval,omitempty"`
	PendingQuestion    *WirePendingQuestion `json:"pendingQuestion,omitempty"`
	AgentProfileID     string `json:"agentProfileId,omitempty"`
	AgentProfileName   string `json:"agentProfileName,omitempty"`
	AgentProfileAvatar string `json:"agentProfileAvatar,omitempty"`
	ParentSessionID    string `json:"parentSessionId,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	LastQueryAt     string  `json:"lastQueryAt,omitempty"`
}

// CreateSessionParams holds client-provided parameters for creating a session.
type CreateSessionParams struct {
	ProjectID       string
	Name            string
	Worktree        bool
	Branch          string
	Model           string
	PlanMode        bool
	AutoApproveMode string
	RequestID       string // used as fallback branch name suffix
	Effort          string
	MaxBudget       float64
	MaxTurns        int
	BehaviorPresets BehaviorPresets
	AgentProfileID  string // optional: bind session to a persistent agent profile
	ParentSessionID string // optional: lead session that spawned this one (for hierarchy tree)
	IdempotencyKey  string // optional: if set, duplicate creates return the cached result
}

// CreateSessionResult is the wire type returned after session creation.
type CreateSessionResult struct {
	SessionID       string          `json:"sessionId"`
	Name            string          `json:"name"`
	State           string          `json:"state"`
	Connected       bool            `json:"connected"`
	Model           string          `json:"model"`
	PermissionMode  string          `json:"permissionMode"`
	AutoApproveMode string          `json:"autoApproveMode"`
	Effort          string          `json:"effort,omitempty"`
	MaxBudget       float64         `json:"maxBudget,omitempty"`
	MaxTurns        int             `json:"maxTurns,omitempty"`
	WorktreePath    string          `json:"worktreePath,omitempty"`
	WorktreeBranch     string          `json:"worktreeBranch,omitempty"`
	BehaviorPresets    BehaviorPresets `json:"behaviorPresets"`
	AgentProfileID     string          `json:"agentProfileId,omitempty"`
	AgentProfileName   string          `json:"agentProfileName,omitempty"`
	AgentProfileAvatar string          `json:"agentProfileAvatar,omitempty"`
	CreatedAt          string          `json:"createdAt"`
}

// ListSessionsResult is the wire type for session list responses.
type ListSessionsResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

// HistoryResult is the wire type for session history responses.
type HistoryResult struct {
	Turns      []HistoryTurn `json:"turns"`
	HasMore    bool          `json:"hasMore"`
	TotalTurns int           `json:"totalTurns"`
}

// idempotencyEntry stores a cached CreateSession result with an expiry time.
type idempotencyEntry struct {
	result    CreateSessionResult
	expiresAt time.Time
}

const idempotencyTTL = 5 * time.Minute

// Service encapsulates session lifecycle business logic.
type Service struct {
	mgr            *Manager
	queries        serviceQueries
	hub            Broadcaster
	gitSvc         *GitService
	runner         msggen.Runner
	worktree       worktreeOps
	personaQuerier PersonaQuerier  // optional; set when teams feature is enabled
	browserSvc     *BrowserService // optional; set when browser support is available

	idempotencyMu    sync.Mutex
	idempotencyCache map[string]idempotencyEntry

	done chan struct{}
}

// NewService creates a new session Service.
func NewService(mgr *Manager, queries serviceQueries, hub Broadcaster, runner msggen.Runner) *Service {
	svc := &Service{
		mgr:              mgr,
		queries:          queries,
		hub:              hub,
		runner:           runner,
		worktree:         RealWorktreeOps(),
		idempotencyCache: make(map[string]idempotencyEntry),
		done:             make(chan struct{}),
	}
	go svc.sweepIdempotencyCache()
	return svc
}

// sweepIdempotencyCache removes expired entries every minute.
func (s *Service) sweepIdempotencyCache() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			now := time.Now()
			s.idempotencyMu.Lock()
			for k, v := range s.idempotencyCache {
				if now.After(v.expiresAt) {
					delete(s.idempotencyCache, k)
				}
			}
			s.idempotencyMu.Unlock()
		}
	}
}

// Close stops background goroutines. Safe to call multiple times.
func (s *Service) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// SetGitService injects the GitService for snapshot broadcasts.
// Called after both Service and GitService are constructed.
func (s *Service) SetGitService(gs *GitService) {
	s.gitSvc = gs
}

// SetPersonaQuerier injects the persona querier for AskTeammate support.
// Called after Service + persona.Service are constructed.
func (s *Service) SetPersonaQuerier(pq PersonaQuerier) {
	s.personaQuerier = pq
}

// SetBrowserService injects the BrowserService for per-session Chrome management.
func (s *Service) SetBrowserService(bs *BrowserService) {
	s.browserSvc = bs
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
// If IdempotencyKey is set and a cached result exists, the cached result is returned.
// worktreeInfo bundles the result of provisionWorktree: the working directory
// the session will run in, plus optional worktree metadata when p.Worktree was
// requested.
type worktreeInfo struct {
	workDir string // either project.Path or the worktree path
	path    string // worktree path; empty when worktree was not requested
	branch  string // worktree branch; empty when worktree was not requested
	baseSHA string // base SHA at worktree creation time
}

// sessionConfig holds the resolved per-session knobs after applying the
// explicit > persona-config > project-default precedence cascade.
type sessionConfig struct {
	model           string
	effort          string
	autoApproveMode string
	presets         BehaviorPresets
}

func (s *Service) CreateSession(ctx context.Context, p CreateSessionParams) (CreateSessionResult, error) {
	if cached, hit := s.checkIdempotency(p.IdempotencyKey); hit {
		return cached, nil
	}

	project, err := s.queries.GetProject(ctx, p.ProjectID)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("project not found: %w", err)
	}
	if err := s.checkProjectQuota(ctx, project); err != nil {
		return CreateSessionResult{}, err
	}

	sessionID := uuid.New().String()
	wt, err := s.provisionWorktree(p, project, sessionID)
	if err != nil {
		return CreateSessionResult{}, err
	}

	allProjects, _ := s.queries.ListProjects(ctx)
	projectInfos := ProjectInfoFromStore(allProjects)
	tc := s.resolveTeamContext(ctx, p.AgentProfileID)

	var pc PersonaConfig
	if tc != nil {
		pc = parsePersonaConfig(tc.profile.Config)
	}
	cfg := resolveSessionConfig(p, pc, project)

	browserPort, mcpConfigs := s.allocateBrowserPort(sessionID)

	sess, err := s.mgr.Create(ctx, CreateParams{
		ID:                    sessionID,
		ProjectID:             p.ProjectID,
		Name:                  p.Name,
		WorkDir:               wt.workDir,
		WorktreePath:          wt.path,
		WorktreeBranch:        wt.branch,
		WorktreeBaseSHA:       wt.baseSHA,
		Model:                 cfg.model,
		PlanMode:              p.PlanMode,
		AutoApproveMode:       cfg.autoApproveMode,
		Effort:                cfg.effort,
		MaxBudget:             p.MaxBudget,
		MaxTurns:              p.MaxTurns,
		Projects:              projectInfos,
		BehaviorPresets:       cfg.presets,
		TeamPreambles:         tc.toPreambles(),
		AgentProfileID:        p.AgentProfileID,
		ParentSessionID:       p.ParentSessionID,
		MCPConfigs:            mcpConfigs,
		BrowserEnabled:        s.browserSvc != nil,
		SystemPromptAdditions: pc.SystemPromptAdditions,
	})
	if err != nil {
		if wt.path != "" {
			s.worktree.RemoveWorktree(project.Path, wt.path)
		}
		return CreateSessionResult{}, fmt.Errorf("failed to create session: %w", err)
	}

	if browserPort > 0 {
		sess.SetBrowserPort(browserPort)
	}

	slog.Info("session created", "session_id", sess.ID, "project", project.Name, "model", cfg.model, "worktree", p.Worktree)

	// Wire persona context for AskTeammate if session is team-bound.
	s.wirePersonaContext(sess, p.AgentProfileID, tc)
	// Wire spawn-workers callback so the session can delegate to workers.
	s.wireSpawnWorkersCallback(sess, p.ProjectID)

	createdAt := ""
	if dbSess, dbErr := s.queries.GetSession(ctx, sess.ID); dbErr == nil {
		createdAt = dbSess.CreatedAt
	}

	profileName, profileAvatar := s.resolveAgentProfileMeta(ctx, p.AgentProfileID, tc)

	s.hub.Broadcast(p.ProjectID, "session.created", SessionInfo{
		ID:                 sess.ID,
		ProjectID:          p.ProjectID,
		Name:               p.Name,
		State:              string(sess.State()),
		Connected:          true,
		Model:              cfg.model,
		PermissionMode:     sess.PermissionMode(),
		AutoApproveMode:    sess.AutoApproveMode(),
		Effort:             cfg.effort,
		MaxBudget:          p.MaxBudget,
		MaxTurns:           p.MaxTurns,
		WorktreePath:       wt.path,
		WorktreeBranch:     wt.branch,
		BehaviorPresets:    cfg.presets,
		AgentProfileID:     p.AgentProfileID,
		AgentProfileName:   profileName,
		AgentProfileAvatar: profileAvatar,
		ParentSessionID:    p.ParentSessionID,
		CreatedAt:          createdAt,
		UpdatedAt:          createdAt,
	})

	result := CreateSessionResult{
		SessionID:          sess.ID,
		Name:               p.Name,
		State:              string(sess.State()),
		Connected:          true,
		Model:              cfg.model,
		PermissionMode:     sess.PermissionMode(),
		AutoApproveMode:    sess.AutoApproveMode(),
		Effort:             cfg.effort,
		MaxBudget:          p.MaxBudget,
		MaxTurns:           p.MaxTurns,
		WorktreePath:       wt.path,
		WorktreeBranch:     wt.branch,
		BehaviorPresets:    cfg.presets,
		AgentProfileID:     p.AgentProfileID,
		AgentProfileName:   profileName,
		AgentProfileAvatar: profileAvatar,
		CreatedAt:          createdAt,
	}

	s.cacheIdempotency(p.IdempotencyKey, result)
	return result, nil
}

// checkIdempotency returns a previously-issued result when the same key was
// seen within the TTL. Empty key disables the cache.
func (s *Service) checkIdempotency(key string) (CreateSessionResult, bool) {
	if key == "" {
		return CreateSessionResult{}, false
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	entry, ok := s.idempotencyCache[key]
	if !ok || !time.Now().Before(entry.expiresAt) {
		return CreateSessionResult{}, false
	}
	slog.Debug("idempotent session create hit", "key", key, "session_id", entry.result.SessionID)
	return entry.result, true
}

func (s *Service) cacheIdempotency(key string, result CreateSessionResult) {
	if key == "" {
		return
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	s.idempotencyCache[key] = idempotencyEntry{
		result:    result,
		expiresAt: time.Now().Add(idempotencyTTL),
	}
}

// checkProjectQuota enforces project.MaxSessions. A zero limit means unlimited.
func (s *Service) checkProjectQuota(ctx context.Context, project store.Project) error {
	if project.MaxSessions <= 0 {
		return nil
	}
	activeCount, err := s.queries.CountActiveSessionsByProject(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("count active sessions: %w", err)
	}
	if activeCount >= project.MaxSessions {
		return fmt.Errorf("%w: %d/%d active sessions for project %q", ErrSessionLimitReached, activeCount, project.MaxSessions, project.Name)
	}
	return nil
}

// provisionWorktree adopts/restores/creates the session's worktree as
// appropriate. Returns wt.path == "" when p.Worktree is false.
func (s *Service) provisionWorktree(p CreateSessionParams, project store.Project, sessionID string) (worktreeInfo, error) {
	wt := worktreeInfo{workDir: project.Path}
	if !p.Worktree {
		return wt, nil
	}

	branch := p.Branch
	if branch == "" {
		branch = "session-" + sessionID[:8]
	}
	wt.branch = branch
	wt.path = s.worktree.WorktreePath(project.Name, branch)

	if baseSHA, err := s.worktree.GetWorktreeBaseSHA(project.Path); err == nil {
		wt.baseSHA = baseSHA
	}

	switch {
	case worktreeDirExists(wt.path):
		// Adopt: directory already exists, no-op.
	case s.worktree.BranchExists(project.Path, branch):
		if err := s.worktree.RestoreWorktree(project.Path, branch, wt.path); err != nil {
			return worktreeInfo{}, fmt.Errorf("failed to restore worktree: %w", err)
		}
	default:
		if err := s.worktree.CreateWorktree(project.Path, branch, wt.path); err != nil {
			return worktreeInfo{}, fmt.Errorf("failed to create worktree: %w", err)
		}
	}
	wt.workDir = wt.path
	return wt, nil
}

func worktreeDirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// resolveSessionConfig applies the explicit > persona-config > project-default
// cascade to model/effort/autoApprove/presets. Pure function — no IO.
func resolveSessionConfig(p CreateSessionParams, pc PersonaConfig, project store.Project) sessionConfig {
	cfg := sessionConfig{
		model:           p.Model,
		effort:          p.Effort,
		autoApproveMode: p.AutoApproveMode,
		presets:         p.BehaviorPresets,
	}

	if cfg.model == "" {
		if pc.Model != "" {
			cfg.model = pc.Model
		} else {
			cfg.model = "opus"
		}
	}
	if cfg.effort == "" {
		cfg.effort = pc.Effort
	}
	if cfg.autoApproveMode == "" {
		cfg.autoApproveMode = pc.AutoApproveMode
	}
	if cfg.presets.IsZero() && !pc.BehaviorPresets.IsZero() {
		cfg.presets = BehaviorPresets{
			AutoCommit:         pc.BehaviorPresets.AutoCommit,
			SuggestParallel:    pc.BehaviorPresets.SuggestParallel,
			PlanFirst:          pc.BehaviorPresets.PlanFirst,
			Terse:              pc.BehaviorPresets.Terse,
			CustomInstructions: pc.BehaviorPresets.CustomInstructions,
		}
	}
	if cfg.presets.IsZero() {
		cfg.presets = ParsePresets(project.DefaultBehaviorPresets)
	}
	return cfg
}

// allocateBrowserPort pre-allocates a Playwright MCP port so the CLI process
// can be started with the Chrome MCP config baked in. Chrome itself is not
// started yet — it launches when the user clicks "Open Browser" and the
// session reconnects via ReconnectMCPServer.
//
// Returns 0 / nil when the browser service is unavailable or allocation fails.
func (s *Service) allocateBrowserPort(sessionID string) (int, []string) {
	if s.browserSvc == nil {
		return 0, nil
	}
	port, err := s.browserSvc.AllocatePort(sessionID)
	if err != nil {
		slog.Warn("browser port allocation failed", "session_id", sessionID, "error", err)
		return 0, nil
	}
	return port, []string{PlaywrightMCPConfig(port)}
}

// resolveAgentProfileMeta returns the (name, avatar) for the bound agent
// profile. Reuses tc.profile when available to avoid a redundant DB hit.
func (s *Service) resolveAgentProfileMeta(ctx context.Context, agentProfileID string, tc *teamContextData) (string, string) {
	if tc != nil {
		return tc.profile.Name, tc.profile.Avatar
	}
	if agentProfileID == "" {
		return "", ""
	}
	ap, err := s.queries.GetAgentProfile(ctx, agentProfileID)
	if err != nil {
		return "", ""
	}
	return ap.Name, ap.Avatar
}

// QuerySession performs lazy resume if needed and delegates to session.Query.
func (s *Service) QuerySession(ctx context.Context, sessionID, prompt string, attachments []QueryAttachment) error {
	sess, err := s.ensureLive(ctx, sessionID)
	if err != nil {
		return err
	}

	slog.Info("session query", "session_id", sessionID, "prompt_len", len(prompt), "attachments", len(attachments))

	if err := sess.Query(ctx, prompt, attachments); err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	s.postQuery(ctx, sessionID, sess, prompt)
	return nil
}

// EnqueueMessage sends a prompt as a new turn if idle, or injects mid-turn if running.
// Uses SendMessage for mid-turn injection so the CLI picks it up at the next safe
// boundary (between tool calls) without waiting for the current turn to complete.
// Performs lazy resume for dead/stopped sessions (same as QuerySession).
func (s *Service) EnqueueMessage(ctx context.Context, sessionID, prompt string, attachments []QueryAttachment) error {
	sess, err := s.ensureLive(ctx, sessionID)
	if err != nil {
		return err
	}

	// If running, inject mid-turn via SendMessage.
	if sess.State() == StateRunning {
		if err := sess.SendMessage(prompt, attachments); err != nil {
			return fmt.Errorf("send message failed: %w", err)
		}
		return nil
	}

	// Not running — send as a new turn (same path as QuerySession).
	slog.Info("session query", "session_id", sessionID, "prompt_len", len(prompt), "attachments", len(attachments))
	if err := sess.Query(ctx, prompt, attachments); err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	s.postQuery(ctx, sessionID, sess, prompt)
	return nil
}

// ResumeSession reconnects a stopped/done/failed session without sending a query.
// If the session is already live and healthy, returns its current info (idempotent).
func (s *Service) ResumeSession(ctx context.Context, sessionID string) (SessionInfo, error) {
	sess := s.mgr.Get(sessionID)

	// CLI process dead — evict and resume with a fresh connection.
	if sess != nil && (sess.State() == StateDone || sess.State() == StateFailed) {
		s.evictForResume(sessionID)
		sess = nil
	}

	// Already live and healthy — return current info.
	if sess != nil {
		return s.GetSessionInfo(ctx, sessionID)
	}

	if _, err := s.resumeSession(ctx, sessionID); err != nil {
		return SessionInfo{}, fmt.Errorf("resume failed: %w", err)
	}

	return s.GetSessionInfo(ctx, sessionID)
}

// evictForResume preserves the git version counter before evicting a dead session.
// Without this, the resumed session starts from version 0 and all state broadcasts
// are rejected by the frontend's monotonic version guard.
func (s *Service) evictForResume(sessionID string) {
	if live := s.mgr.Get(sessionID); live != nil && s.gitSvc != nil {
		s.gitSvc.SeedVersion(sessionID, live.GitVersion())
	}
	s.mgr.Evict(sessionID)
}

// StopSession stops a live session. The worktree is preserved so the session
// can be resumed later. Worktree cleanup happens only on DeleteSession or
// Merge with cleanup=true.
func (s *Service) StopSession(ctx context.Context, sessionID string) error {
	slog.Info("stopping session", "session_id", sessionID)

	// Stop the browser before the CLI session so Chrome is cleaned up.
	if s.browserSvc != nil {
		s.browserSvc.StopBrowser(sessionID)
	}

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

// ResetConversation stops the CLI process, clears the claude_session_id, and evicts
// the session from memory. The next query will start a fresh Claude conversation
// in the same worktree with recovery context injected via preamble.
func (s *Service) ResetConversation(ctx context.Context, sessionID string) error {
	slog.Info("resetting conversation", "session_id", sessionID)

	// Stop and evict the live session if present.
	if live := s.mgr.Get(sessionID); live != nil {
		if s.gitSvc != nil {
			s.gitSvc.SeedVersion(sessionID, live.GitVersion())
		}
		_ = s.mgr.Stop(ctx, sessionID)
	}

	// Clear the claude_session_id so the next resume creates a fresh CLI.
	if err := s.queries.UpdateClaudeSessionID(ctx, store.UpdateClaudeSessionIDParams{
		ClaudeSessionID: sql.NullString{Valid: false},
		ID:              sessionID,
	}); err != nil {
		return fmt.Errorf("failed to clear claude session ID: %w", err)
	}

	// Update state so the frontend shows the session as stopped.
	if err := s.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: string(StateStopped),
		ID:    sessionID,
	}); err != nil {
		slog.Error("failed to update session state after reset", "session_id", sessionID, "error", err)
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
	profileCache := s.prefetchProfiles(sessions)

	infos := make([]SessionInfo, 0, len(sessions))
	for _, ss := range sessions {
		infos = append(infos, s.enrichSession(ss, costMap, profileCache, projectPaths))
	}
	return infos
}

// prefetchProfiles loads every distinct AgentProfile referenced by the given
// sessions in a single pass so the per-session loop below avoids repeated DB
// hits for the common case where many sessions share the same profile.
func (s *Service) prefetchProfiles(sessions []store.Session) map[string]store.AgentProfile {
	profileCache := make(map[string]store.AgentProfile)
	for _, ss := range sessions {
		pid := nullStr(ss.AgentProfileID)
		if pid == "" {
			continue
		}
		if _, ok := profileCache[pid]; ok {
			continue
		}
		if p, err := s.queries.GetAgentProfile(context.Background(), pid); err == nil {
			profileCache[pid] = p
		}
	}
	return profileCache
}

// enrichSession builds a single SessionInfo by composing the persisted row
// with cost summary, profile metadata, live-session state, git status, and
// channel memberships. Each layer is its own helper so the orchestration
// stays a flat list of named steps.
func (s *Service) enrichSession(
	ss store.Session,
	costMap map[string]costSummary,
	profileCache map[string]store.AgentProfile,
	projectPaths map[string]string,
) SessionInfo {
	info := baseSessionInfo(ss)
	info.Connected = s.mgr.IsLive(ss.ID)

	if pid := nullStr(ss.AgentProfileID); pid != "" {
		info.AgentProfileID = pid
		if p, ok := profileCache[pid]; ok {
			info.AgentProfileName = p.Name
			info.AgentProfileAvatar = p.Avatar
		}
	}

	if summary, ok := costMap[ss.ID]; ok {
		info.TotalCost = summary.TotalCost
		info.TurnCount = int(summary.TurnCount)
	}

	s.applyLiveState(&info, ss.ID)
	enrichGitStatus(s.mgr.gitStatus, &info, projectPaths[ss.ProjectID], ss.WorkDir)
	s.applyChannelMemberships(&info, ss.ID)

	return info
}

// baseSessionInfo copies persisted fields from a store.Session into a
// SessionInfo. Pure projection — no external lookups.
func baseSessionInfo(ss store.Session) SessionInfo {
	return SessionInfo{
		ID:              ss.ID,
		ProjectID:       ss.ProjectID,
		Name:            ss.Name,
		State:           ss.State,
		Model:           ss.Model,
		PermissionMode:  ss.PermissionMode,
		AutoApproveMode: ss.AutoApproveMode,
		Effort:          ss.Effort,
		MaxBudget:       ss.MaxBudget,
		MaxTurns:        int(ss.MaxTurns),
		WorktreePath:    nullStr(ss.WorktreePath),
		WorktreeBranch:  nullStr(ss.WorktreeBranch),
		WorktreeMerged:  ss.WorktreeMerged != 0,
		CompletedAt:     nullStr(ss.CompletedAt),
		PrUrl:           ss.PrUrl,
		BehaviorPresets: ParsePresets(ss.BehaviorPresets),
		ParentSessionID: nullStr(ss.ParentSessionID),
		CreatedAt:       ss.CreatedAt,
		UpdatedAt:       ss.UpdatedAt,
		LastQueryAt:     nullStr(ss.LastQueryAt),
	}
}

// applyLiveState stamps fields that only the in-memory Session knows about
// (current git version, git operation in flight, pending approvals/questions).
// When the session is offline, falls back to the GitService's last known
// version so the frontend can detect drift after a server restart.
func (s *Service) applyLiveState(info *SessionInfo, sessionID string) {
	if live := s.mgr.Get(sessionID); live != nil {
		info.GitVersion = live.GitVersion()
		_, _, _, _, gitOp := live.liveState()
		info.GitOperation = gitOp
		info.PendingApproval, info.PendingQuestion = live.PendingState()
		return
	}
	if s.gitSvc != nil {
		info.GitVersion = s.gitSvc.LastVersion(sessionID)
	}
}

// applyChannelMemberships populates ChannelIDs and ChannelRoles from the
// session_channels join table. Errors are silently ignored — channel
// metadata is non-essential for the wire payload.
func (s *Service) applyChannelMemberships(info *SessionInfo, sessionID string) {
	channels, err := s.queries.ListSessionChannels(context.Background(), sessionID)
	if err != nil || len(channels) == 0 {
		return
	}
	info.ChannelIDs = make([]string, 0, len(channels))
	info.ChannelRoles = make(map[string]string, len(channels))
	for _, ch := range channels {
		info.ChannelIDs = append(info.ChannelIDs, ch.ChannelID)
		if ch.Role != "" {
			info.ChannelRoles[ch.ChannelID] = ch.Role
		}
	}
}

// enrichGitStatus populates git-related fields on a SessionInfo.
func enrichGitStatus(q branchStatusQuerier, info *SessionInfo, projectPath, workDir string) {
	if info.WorktreeMerged || projectPath == "" {
		return
	}
	if info.WorktreeBranch != "" {
		// Worktree session: full branch status.
		bs := computeBranchStatus(q, projectPath, info.WorktreeBranch, info.WorktreePath)
		info.BranchMissing = bs.BranchMissing
		info.CommitsAhead = bs.CommitsAhead
		info.CommitsBehind = bs.CommitsBehind
		info.HasUncommitted = bs.HasUncommitted
		info.HasDirtyWorktree = bs.HasUncommitted
		info.MergeStatus = bs.MergeStatus
		info.MergeConflictFiles = bs.MergeConflictFiles
	} else if workDir != "" {
		// Local (non-worktree) session: only check uncommitted changes.
		if dirty, err := q.HasUncommittedChanges(workDir); err == nil {
			info.HasUncommitted = dirty
		}
	}
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

// GetHistory returns turn history for a session. If limit > 0, returns only
// the most recent N turns plus metadata for the frontend to request the rest.
func (s *Service) GetHistory(ctx context.Context, sessionID string, limit int) (HistoryResult, error) {
	if limit > 0 {
		totalTurns, err := s.queries.CountTurnsBySession(ctx, sessionID)
		if err != nil {
			return HistoryResult{}, err
		}
		turns, err := RecentHistoryFromDB(ctx, s.queries, sessionID, limit)
		if err != nil {
			return HistoryResult{}, err
		}
		return HistoryResult{
			Turns:      turns,
			HasMore:    int(totalTurns) > len(turns),
			TotalTurns: int(totalTurns),
		}, nil
	}

	turns, err := HistoryFromDB(ctx, s.queries, sessionID)
	if err != nil {
		return HistoryResult{}, err
	}
	return HistoryResult{
		Turns:      turns,
		TotalTurns: len(turns),
	}, nil
}

// DeleteSession stops a live session, removes its worktree/branch, and deletes
// from DB. Child sessions (workers spawned under a lead) are deleted first,
// depth-first, so each one gets a full cleanup pass (stop, worktree, branch,
// files, broadcast) before the parent row disappears. The ON DELETE CASCADE
// on parent_session_id is a safety net for rows we fail to enumerate.
func (s *Service) DeleteSession(ctx context.Context, sessionID string) error {
	slog.Info("deleting session", "session_id", sessionID)

	// Depth-first: delete descendants first so each child's worktree/branch
	// gets cleaned up with the usual machinery. Failures are logged but do
	// not block the parent delete — the FK cascade will wipe any leftover
	// rows.
	children, cerr := s.queries.ListChildSessions(ctx, sql.NullString{String: sessionID, Valid: true})
	if cerr != nil {
		slog.Warn("list children failed during delete", "session_id", sessionID, "error", cerr)
	}
	for _, child := range children {
		if err := s.DeleteSession(ctx, child.ID); err != nil {
			slog.Warn("child delete failed", "parent_id", sessionID, "child_id", child.ID, "error", err)
		}
	}

	if s.browserSvc != nil {
		s.browserSvc.StopBrowser(sessionID)
	}

	if live := s.mgr.Get(sessionID); live != nil {
		if err := s.mgr.Stop(ctx, sessionID); err != nil {
			slog.Warn("stop failed during delete", "session_id", sessionID, "error", err)
		}
	}

	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}

	// Clean up channel memberships before deleting.
	if live := s.mgr.Get(sessionID); live != nil {
		live.ClearAllAgentMessageCallbacks()
	}
	if channels, err := s.queries.ListSessionChannels(ctx, sessionID); err == nil {
		for _, ch := range channels {
			s.hub.Broadcast(dbSess.ProjectID, "channel.member-left", PushChannelMemberLeft{
				ChannelID: ch.ChannelID, SessionID: sessionID,
			})
		}
	}
	// ON DELETE CASCADE on channel_members handles the actual row cleanup.

	// Always clean up worktree/branch — operations are idempotent if already removed.
	project, projErr := s.queries.GetProject(ctx, dbSess.ProjectID)
	if projErr == nil {
		if wtPath := nullStr(dbSess.WorktreePath); wtPath != "" {
			s.worktree.RemoveWorktree(project.Path, wtPath)
		}
		if branch := nullStr(dbSess.WorktreeBranch); branch != "" {
			if delErr := s.worktree.DeleteBranch(project.Path, branch); delErr != nil {
				slog.Warn("branch delete failed", "session_id", sessionID, "error", delErr)
			}
		}
	}

	// Clean up persistent session files directory.
	filesDir := filepath.Join(paths.SessionFilesDir(), sessionID)
	if err := os.RemoveAll(filesDir); err != nil {
		slog.Warn("session files cleanup failed", "session_id", sessionID, "error", err)
	}

	if err := s.queries.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("db delete failed: %w", err)
	}

	if s.gitSvc != nil {
		s.gitSvc.CleanupVersion(sessionID)
	}

	s.hub.Broadcast(dbSess.ProjectID, "session.deleted", PushSessionDeleted{SessionID: sessionID})

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

// InterruptSession stops the current generation without killing the session.
func (s *Service) InterruptSession(sessionID string) error {
	sess, err := s.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	return sess.Interrupt()
}

// recoverWorktree restores or recreates a session's worktree if its work directory
// is missing. Returns the (possibly updated) workDir, extra preamble, and dbSess.
func (s *Service) recoverWorktree(ctx context.Context, sessionID string, dbSess store.Session) (workDir, extraPreamble string, updated store.Session, err error) {
	workDir = dbSess.WorkDir
	updated = dbSess

	if _, statErr := os.Stat(workDir); statErr == nil {
		return workDir, "", updated, nil
	}

	project, projErr := s.queries.GetProject(ctx, dbSess.ProjectID)
	if projErr != nil {
		return "", "", updated, fmt.Errorf("project not found: %w", projErr)
	}

	oldBranch := nullStr(dbSess.WorktreeBranch)
	switch {
	case oldBranch != "" && s.worktree.BranchExists(project.Path, oldBranch):
		if err := s.worktree.RestoreWorktree(project.Path, oldBranch, nullStr(dbSess.WorktreePath)); err != nil {
			return "", "", updated, fmt.Errorf("worktree restore failed for branch %s: %w", oldBranch, err)
		}
	case oldBranch != "":
		workDir, updated, err = s.recoverWorktreeFresh(ctx, sessionID, dbSess, project)
		if err != nil {
			return "", "", updated, err
		}
		extraPreamble = preambleFreshWorktreeResume
	default:
		workDir = project.Path
	}
	return workDir, extraPreamble, updated, nil
}

// recoverWorktreeFresh creates a new branch+worktree when the original branch has
// been deleted. Updates the session DB record and returns the new workDir.
func (s *Service) recoverWorktreeFresh(ctx context.Context, sessionID string, dbSess store.Session, project store.Project) (string, store.Session, error) {
	oldBranch := nullStr(dbSess.WorktreeBranch)
	newBranch := "session-" + sessionID[:8] + "-r" + strconv.FormatInt(time.Now().Unix(), 10)
	newWorktreePath := s.worktree.WorktreePath(project.Name, newBranch)
	if err := s.worktree.CreateWorktree(project.Path, newBranch, newWorktreePath); err != nil {
		return "", dbSess, fmt.Errorf("fresh worktree creation failed: %w", err)
	}
	baseSHA, _ := s.worktree.GetWorktreeBaseSHA(project.Path)
	if err := s.queries.UpdateSessionWorktree(ctx, store.UpdateSessionWorktreeParams{
		WorkDir:         newWorktreePath,
		WorktreePath:    sql.NullString{String: newWorktreePath, Valid: true},
		WorktreeBranch:  sql.NullString{String: newBranch, Valid: true},
		WorktreeBaseSha: sql.NullString{String: baseSHA, Valid: baseSHA != ""},
		ID:              sessionID,
	}); err != nil {
		slog.Warn("persist worktree recovery failed", "session_id", sessionID, "error", err)
	}
	dbSess.WorktreeBranch = sql.NullString{String: newBranch, Valid: true}
	dbSess.WorktreePath = sql.NullString{String: newWorktreePath, Valid: true}
	dbSess.WorkDir = newWorktreePath
	dbSess.WorktreeMerged = 0
	slog.Info("resumed session on fresh worktree", "session_id", sessionID, "old_branch", oldBranch, "new_branch", newBranch)
	return newWorktreePath, dbSess, nil
}

// resumeSession attempts to resume a non-live session from its Claude session ID.
func (s *Service) resumeSession(ctx context.Context, sessionID string) (*Session, error) {
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return nil, ErrNotFound
	}
	claudeSessID := nullStr(dbSess.ClaudeSessionID)
	freshStart := claudeSessID == ""

	if !freshStart {
		slog.Debug("resuming session", "session_id", sessionID, "claude_session_id", claudeSessID)
	} else {
		slog.Info("reconnecting session (conversation was reset)", "session_id", sessionID)
	}

	workDir, extraPreamble, dbSess, err := s.recoverWorktree(ctx, sessionID, dbSess)
	if err != nil {
		return nil, err
	}

	if freshStart {
		extraPreamble += preambleConversationReset
	}

	var initialVersion int64
	if s.gitSvc != nil {
		initialVersion = s.gitSvc.LastVersion(sessionID)
	}

	resumeProjects, _ := s.queries.ListProjects(ctx)

	// Build channel preamble(s) for all channels the session belongs to.
	channelMemberships, _ := s.queries.ListSessionChannels(ctx, sessionID)
	channelPreambles := s.buildAllChannelPreambles(ctx, sessionID)
	resumeTC := s.resolveTeamContext(ctx, nullStr(dbSess.AgentProfileID))

	// Re-apply persona config on resume so SystemPromptAdditions persist.
	var resumePC PersonaConfig
	if resumeTC != nil {
		resumePC = parsePersonaConfig(resumeTC.profile.Config)
	}

	params := ResumeParams{
		SessionID:         sessionID,
		ClaudeSessionID:   claudeSessID,
		ProjectID:         dbSess.ProjectID,
		Name:              dbSess.Name,
		WorkDir:           workDir,
		WorktreeBranch:    nullStr(dbSess.WorktreeBranch),
		Model:             dbSess.Model,
		PermissionMode:    dbSess.PermissionMode,
		AutoApproveMode:   dbSess.AutoApproveMode,
		Effort:            dbSess.Effort,
		MaxBudget:         dbSess.MaxBudget,
		MaxTurns:          int(dbSess.MaxTurns),
		InitialGitVersion: initialVersion,
		Projects:          ProjectInfoFromStore(resumeProjects),
		BehaviorPresets:   ParsePresets(dbSess.BehaviorPresets),
		ChannelPreambles:  channelPreambles,
		TeamPreambles:     resumeTC.toPreambles(),
		ExtraPreamble:         extraPreamble,
		BrowserEnabled:        s.browserSvc != nil,
		SystemPromptAdditions: resumePC.SystemPromptAdditions,
	}

	// Re-add browser MCP config on resume so the Playwright server starts with the CLI.
	if s.browserSvc != nil {
		port, portErr := s.browserSvc.AllocatePort(sessionID)
		if portErr == nil {
			params.MCPConfigs = append(params.MCPConfigs, PlaywrightMCPConfig(port))
		}
	}

	var sess *Session
	var resumeErr error
	if freshStart {
		sess, resumeErr = s.mgr.Reconnect(ctx, params)
	} else {
		sess, resumeErr = s.mgr.Resume(ctx, params)
	}
	if resumeErr != nil {
		return nil, resumeErr
	}

	s.wirePostResumeCallbacks(sess, dbSess, channelMemberships, resumeTC)
	applyPostResumeFlags(sess, dbSess)

	if len(channelMemberships) > 0 {
		go s.replayPendingDeliveries(context.Background(), sess)
	}
	return sess, nil
}

// wirePostResumeCallbacks attaches the channel/spawn/persona callbacks the
// session needs after a successful resume. Must be called once per resume,
// before message replay starts.
func (s *Service) wirePostResumeCallbacks(
	sess *Session,
	dbSess store.Session,
	channelMemberships []store.ListSessionChannelsRow,
	resumeTC *teamContextData,
) {
	for _, cm := range channelMemberships {
		s.wireAgentMessageCallback(sess, cm.ChannelID)
		if cm.Role == "lead" {
			s.wireDissolveChannelCallback(sess, cm.ChannelID)
		}
	}
	s.wireSpawnWorkersCallback(sess, dbSess.ProjectID)
	s.wirePersonaContext(sess, nullStr(dbSess.AgentProfileID), resumeTC)
}

// applyPostResumeFlags re-applies persisted lifecycle flags (merged/completed)
// onto the live Session so the in-memory state matches what the DB reflects.
func applyPostResumeFlags(sess *Session, dbSess store.Session) {
	if dbSess.WorktreeMerged != 0 {
		sess.MarkMerged()
	}
	if dbSess.CompletedAt.Valid {
		sess.MarkCompleted()
	}
}

// replayPendingDeliveries delivers messages that arrived while the session was
// offline. Must be called after the CLI is connected and channel callbacks are
// wired. Messages are replayed in chronological order (created_at ASC from the
// query). Each successful delivery updates the record to "delivered".
func (s *Service) replayPendingDeliveries(ctx context.Context, sess *Session) {
	pending, err := s.queries.ListPendingDeliveriesForSession(ctx, sess.ID)
	if err != nil {
		slog.Warn("failed to list pending deliveries", "session_id", sess.ID, "error", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	sess.mu.Lock()
	cli := sess.cliSess
	sess.mu.Unlock()
	if cli == nil {
		slog.Warn("replay skipped: no CLI session", "session_id", sess.ID)
		return
	}

	slog.Info("replaying pending deliveries", "session_id", sess.ID, "count", len(pending))

	for _, d := range pending {
		formatted := formatChannelMessageForCLI(d.SenderName, d.Content, d.MessageType)
		if err := cli.SendMessage(formatted); err != nil {
			slog.Warn("pending delivery replay failed",
				"session_id", sess.ID, "message_id", d.MessageID, "error", err)
			continue
		}
		if err := s.queries.UpdateDeliveryStatus(ctx, store.UpdateDeliveryStatusParams{
			Status:             "delivered",
			MessageID:          d.MessageID,
			RecipientSessionID: sess.ID,
		}); err != nil {
			slog.Warn("pending delivery status update failed",
				"session_id", sess.ID, "message_id", d.MessageID, "error", err)
		}
	}
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
	s.hub.Broadcast(dbSess.ProjectID, "session.renamed", PushSessionRenamed{SessionID: sessionID, Name: name})
	return nil
}

// autoName calls Haiku to generate a short title and broadcasts the rename.
// Skips if the session already has a user-provided name (e.g. from a prompt block).
func (s *Service) autoName(sessionID, projectID, prompt string) {
	dbSess, err := s.queries.GetSession(context.Background(), sessionID)
	if err == nil && dbSess.Name != "" {
		return
	}

	name, err := msggen.SessionTitle(context.Background(), s.runner, []string{prompt})
	if err != nil || name == "" {
		if err != nil {
			slog.Warn("auto-rename haiku failed", "error", err)
		}
		return
	}
	if err := s.queries.UpdateSessionName(context.Background(), store.UpdateSessionNameParams{
		Name: name,
		ID:   sessionID,
	}); err != nil {
		slog.Warn("auto-rename db update failed", "session_id", sessionID, "error", err)
		return
	}
	s.hub.Broadcast(projectID, "session.renamed", PushSessionRenamed{SessionID: sessionID, Name: name})
}

// GenerateSessionName regenerates a title from the full session transcript.
// Reads the parent CLI's jsonl, extracts user-role messages, and asks Haiku
// for a concise title. Does NOT persist — caller passes the returned name
// through RenameSession if they accept it.
func (s *Service) GenerateSessionName(ctx context.Context, sessionID string) (string, error) {
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return "", ErrNotFound
	}
	claudeSessionID := ""
	if dbSess.ClaudeSessionID.Valid {
		claudeSessionID = dbSess.ClaudeSessionID.String
	}
	workDir := dbSess.WorkDir
	if dbSess.WorktreePath.Valid && dbSess.WorktreePath.String != "" {
		workDir = dbSess.WorktreePath.String
	}

	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return "", fmt.Errorf("user home: %w", homeErr)
	}

	var turns []string
	if claudeSessionID != "" && workDir != "" {
		turns, _ = readUserTurns(home, workDir, claudeSessionID, 20, 8000)
	}

	if len(turns) == 0 {
		return "", fmt.Errorf("no user messages available yet")
	}

	name, err := msggen.SessionTitle(ctx, s.runner, turns)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("empty title from model")
	}
	return name, nil
}

// PersonaConfig holds session-creation defaults parsed from an agent profile's
// config JSON. Fields mirror CreateSessionParams where applicable — non-zero
// values act as defaults that explicit params override.
type PersonaConfig struct {
	Model                 string          `json:"model,omitempty"`
	Effort                string          `json:"effort,omitempty"`
	AutoApproveMode       string          `json:"autoApproveMode,omitempty"`
	BehaviorPresets       BehaviorPresets `json:"behaviorPresets"`
	SystemPromptAdditions string          `json:"systemPromptAdditions,omitempty"`
	CommunicationMode     string          `json:"communicationMode,omitempty"`
	Capabilities          []string        `json:"capabilities,omitempty"`
}

// parsePersonaConfig unmarshals the profile's config JSON into a PersonaConfig.
// Returns zero-value PersonaConfig on empty/malformed JSON.
func parsePersonaConfig(raw string) PersonaConfig {
	var pc PersonaConfig
	if raw == "" || raw == "{}" {
		return pc
	}
	if err := json.Unmarshal([]byte(raw), &pc); err != nil {
		slog.Warn("malformed persona config JSON", "error", err)
	}
	return pc
}

// resolvedTeam holds a team and its members (excluding the profile itself).
type resolvedTeam struct {
	team    store.Team
	members []store.AgentProfile
}

// teamContextData holds pre-resolved team data for a given agent profile.
type teamContextData struct {
	profile store.AgentProfile
	teams   []resolvedTeam
}

// resolveTeamContext fetches the profile, its teams, and each team's members
// (excluding the profile itself). Returns nil if not team-bound.
func (s *Service) resolveTeamContext(ctx context.Context, agentProfileID string) *teamContextData {
	if agentProfileID == "" {
		return nil
	}
	profile, err := s.queries.GetAgentProfile(ctx, agentProfileID)
	if err != nil {
		slog.Warn("resolve team context: profile not found", "profile_id", agentProfileID, "error", err)
		return nil
	}
	teams, err := s.queries.ListTeamsForAgent(ctx, agentProfileID)
	if err != nil {
		slog.Warn("resolve team context: list teams failed", "profile_id", agentProfileID, "error", err)
		return nil
	}
	if len(teams) == 0 {
		return nil
	}
	tc := &teamContextData{profile: profile}
	for _, t := range teams {
		members, err := s.queries.ListTeamMembers(ctx, t.ID)
		if err != nil {
			continue
		}
		var filtered []store.AgentProfile
		for _, m := range members {
			if m.ID != agentProfileID {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) > 0 {
			tc.teams = append(tc.teams, resolvedTeam{team: t, members: filtered})
		}
	}
	if len(tc.teams) == 0 {
		return nil
	}
	return tc
}

// toPreambles converts resolved team data to the preamble format used by the
// system prompt builder. Nil-safe: returns nil on nil receiver.
func (tc *teamContextData) toPreambles() []*TeamPreambleInfo {
	if tc == nil {
		return nil
	}
	var result []*TeamPreambleInfo
	for _, rt := range tc.teams {
		var teammates []TeamPreambleMember
		for _, m := range rt.members {
			teammates = append(teammates, TeamPreambleMember{Name: m.Name, Role: m.Role})
		}
		result = append(result, &TeamPreambleInfo{
			TeamName:    rt.team.Name,
			ProfileName: tc.profile.Name,
			ProfileRole: tc.profile.Role,
			Teammates:   teammates,
		})
	}
	return result
}

// wirePersonaContext sets up AskTeammate support on a session using pre-resolved
// team data. Pass the teamContextData from resolveTeamContext to avoid redundant
// DB queries.
func (s *Service) wirePersonaContext(sess *Session, agentProfileID string, tc *teamContextData) {
	if s.personaQuerier == nil || tc == nil {
		return
	}
	teammates := make(map[string]teammateRef)
	for _, rt := range tc.teams {
		for _, m := range rt.members {
			teammates[strings.ToLower(m.Name)] = teammateRef{
				profileID: m.ID,
				teamID:    rt.team.ID,
			}
		}
	}
	if len(teammates) == 0 {
		return
	}
	sess.SetPersonaContext(s.personaQuerier, agentProfileID, tc.profile.Name, teammates)
}
