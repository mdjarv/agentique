package session

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// Broadcaster sends push messages to all WebSocket clients for a project.
type Broadcaster interface {
	Broadcast(projectID, pushType string, payload any)
}

// CreateParams holds the parameters for creating a new session.
type CreateParams struct {
	ID              string // optional; generated if empty
	ProjectID       string
	Name            string
	WorkDir         string
	WorktreePath    string
	WorktreeBranch  string
	WorktreeBaseSHA string
	Model           string
	PlanMode        bool
	AutoApprove     bool
	Effort          string
	MaxBudget       float64
	MaxTurns        int
}

// Manager manages the lifecycle of claudecli-go sessions.
type Manager struct {
	mu          sync.Mutex
	sessions    map[string]*Session
	queries     *store.Queries
	broadcaster Broadcaster
}

// NewManager creates a new session manager.
func NewManager(queries *store.Queries, broadcaster Broadcaster) *Manager {
	return &Manager{
		sessions:    make(map[string]*Session),
		queries:     queries,
		broadcaster: broadcaster,
	}
}

// Create starts a new claudecli-go session, persists metadata to DB, and returns the session.
func (m *Manager) Create(_ context.Context, params CreateParams) (*Session, error) {
	id := params.ID
	if id == "" {
		id = uuid.New().String()
	}

	// Build Session first (without cliSess) so the permission callback can capture it.
	sess := newSession(sessionParams{
		id:        id,
		projectID: params.ProjectID,
		queries:   m.queries,
		broadcast: m.broadcastFunc(params.ProjectID),
		turnIndex: -1, // first Query() will increment to 0
		workDir:   params.WorkDir,
	})

	permMode := "default"
	if params.PlanMode {
		permMode = "plan"
	}
	var autoApproveInt int64
	if params.AutoApprove {
		autoApproveInt = 1
	}

	// Set auto-approve and permission mode before connecting so the callback has it immediately.
	if params.AutoApprove {
		sess.SetAutoApprove(true)
	}
	sess.mu.Lock()
	sess.permissionMode = permMode
	sess.mu.Unlock()

	model := resolveModel(params.Model)
	connectOpts := []claudecli.Option{
		claudecli.WithWorkDir(params.WorkDir),
		claudecli.WithModel(model),
		claudecli.WithCanUseTool(sess.handleToolPermission),
		claudecli.WithUserInput(sess.handleUserInput),
		claudecli.WithIncludePartialMessages(),
		claudecli.WithAppendSystemPrompt(agentiquePreamble),
	}
	if effort := resolveEffort(params.Effort); effort != "" {
		connectOpts = append(connectOpts, claudecli.WithEffort(effort))
	}
	if params.MaxBudget > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxBudget(params.MaxBudget))
	}
	if params.MaxTurns > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxTurns(params.MaxTurns))
	}
	// Pass plan mode as CLI flag to avoid post-connect control request race.
	if params.PlanMode {
		connectOpts = append(connectOpts, claudecli.WithPermissionMode(claudecli.PermissionPlan))
	}

	client := claudecli.New()
	// Use background context: the CLI process must outlive the WS connection
	// that triggered session creation. The WS conn context cancels on
	// disconnect (e.g. page refresh), which would SIGTERM the CLI process.
	cliSess, err := client.Connect(context.Background(), connectOpts...)
	if err != nil {
		return nil, err
	}

	_, dbErr := m.queries.CreateSession(context.Background(), store.CreateSessionParams{
		ID:        id,
		ProjectID: params.ProjectID,
		Name:      params.Name,
		WorkDir:   params.WorkDir,
		WorktreePath: sql.NullString{
			String: params.WorktreePath,
			Valid:  params.WorktreePath != "",
		},
		WorktreeBranch: sql.NullString{
			String: params.WorktreeBranch,
			Valid:  params.WorktreeBranch != "",
		},
		WorktreeBaseSha: sql.NullString{
			String: params.WorktreeBaseSHA,
			Valid:  params.WorktreeBaseSHA != "",
		},
		State:          string(StateIdle),
		Model:          params.Model,
		PermissionMode: permMode,
		AutoApprove:    autoApproveInt,
		Effort:         params.Effort,
		MaxBudget:      params.MaxBudget,
		MaxTurns:       int64(params.MaxTurns),
	})
	if dbErr != nil {
		cliSess.Close()
		return nil, dbErr
	}

	sess.setCLISession(cliSess)

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	return sess, nil
}

// ResumeParams holds the parameters for resuming an existing session.
type ResumeParams struct {
	SessionID       string
	ClaudeSessionID string
	ProjectID       string
	WorkDir         string
	Model           string
	PermissionMode  string
	AutoApprove     bool
	Effort          string
	MaxBudget       float64
	MaxTurns        int
}

// Resume reconnects to an existing Claude session using WithResume().
func (m *Manager) Resume(_ context.Context, p ResumeParams) (*Session, error) {
	// Continue turn numbering from where we left off.
	maxTurn, _ := m.queries.MaxTurnIndex(context.Background(), p.SessionID)
	turnIndex := int(maxTurn)

	// Build Session first (without cliSess) so the permission callback can capture it.
	sess := newSession(sessionParams{
		id:        p.SessionID,
		projectID: p.ProjectID,
		queries:   m.queries,
		broadcast: m.broadcastFunc(p.ProjectID),
		turnIndex: turnIndex,
		workDir:   p.WorkDir,
	})
	sess.mu.Lock()
	sess.queryCount = turnIndex + 1
	sess.claudeSessionID = p.ClaudeSessionID
	sess.autoApprove = p.AutoApprove
	if p.PermissionMode != "" {
		sess.permissionMode = p.PermissionMode
	}
	sess.mu.Unlock()

	connectOpts := []claudecli.Option{
		claudecli.WithWorkDir(p.WorkDir),
		claudecli.WithModel(resolveModel(p.Model)),
		claudecli.WithCanUseTool(sess.handleToolPermission),
		claudecli.WithUserInput(sess.handleUserInput),
		claudecli.WithIncludePartialMessages(),
		claudecli.WithResume(p.ClaudeSessionID),
		claudecli.WithAppendSystemPrompt(agentiquePreamble),
	}
	if effort := resolveEffort(p.Effort); effort != "" {
		connectOpts = append(connectOpts, claudecli.WithEffort(effort))
	}
	if p.MaxBudget > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxBudget(p.MaxBudget))
	}
	if p.MaxTurns > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxTurns(p.MaxTurns))
	}
	if p.PermissionMode == "plan" {
		connectOpts = append(connectOpts, claudecli.WithPermissionMode(claudecli.PermissionPlan))
	}

	client := claudecli.New()
	// Use background context: the CLI process must outlive the WS connection
	// that triggered the resume. See Create() for rationale.
	cliSess, err := client.Connect(context.Background(), connectOpts...)
	if err != nil {
		return nil, err
	}

	_ = m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(StateIdle),
		ID:    p.SessionID,
	})

	sess.setCLISession(cliSess)

	// Restore non-plan permission modes post-connect.
	if p.PermissionMode != "" && p.PermissionMode != "default" && p.PermissionMode != "plan" {
		_ = sess.SetPermissionMode(p.PermissionMode)
	}

	m.mu.Lock()
	m.sessions[p.SessionID] = sess
	m.mu.Unlock()

	slog.Info("session resumed", "session_id", p.SessionID, "claude_session_id", p.ClaudeSessionID)
	return sess, nil
}

// Get returns a live session by ID, or nil if not found.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// IsLive reports whether a session has a connected CLI process.
func (m *Manager) IsLive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[id]
	return ok
}

// Evict removes a dead session from the in-memory map and closes it.
// Unlike Stop, it does not change the DB state.
func (m *Manager) Evict(id string) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

// Stop closes a live session and marks it as stopped in DB.
// Does not handle worktree cleanup — callers (Service) are responsible for that.
func (m *Manager) Stop(_ context.Context, id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if sess != nil {
		sess.Close()
	}

	return m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(StateStopped),
		ID:    id,
	})
}

// ListByProject returns session metadata from DB.
func (m *Manager) ListByProject(ctx context.Context, projectID string) ([]store.Session, error) {
	sessions, err := m.queries.ListSessionsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	m.fixStates(sessions)
	return sessions, nil
}

// ListAll returns all sessions across all projects from DB.
func (m *Manager) ListAll(ctx context.Context) ([]store.Session, error) {
	sessions, err := m.queries.ListAllSessions(ctx)
	if err != nil {
		return nil, err
	}
	m.fixStates(sessions)
	return sessions, nil
}

// fixStates corrects DB state for sessions based on live state.
func (m *Manager) fixStates(sessions []store.Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range sessions {
		if live, ok := m.sessions[sessions[i].ID]; ok {
			sessions[i].State = string(live.State())
		} else if sessions[i].State == string(StateRunning) || sessions[i].State == string(StateMerging) {
			sessions[i].State = string(StateStopped)
		}
	}
}

// CloseAll gracefully closes all live sessions with a per-session timeout.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	slog.Info("closing all sessions", "count", len(sessions))

	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			done := make(chan struct{})
			go func() {
				s.Close()
				close(done)
			}()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				slog.Warn("session close timed out", "session_id", s.ID)
			}
		}(s)
	}
	wg.Wait()
}

func (m *Manager) broadcastFunc(projectID string) func(string, any) {
	return func(pushType string, payload any) {
		m.broadcaster.Broadcast(projectID, pushType, payload)
	}
}

// resolveEffort maps a string effort level to a claudecli.EffortLevel constant.
// Returns empty string for unknown/empty values (CLI default).
func resolveEffort(level string) claudecli.EffortLevel {
	switch level {
	case "low":
		return claudecli.EffortLow
	case "medium":
		return claudecli.EffortMedium
	case "high":
		return claudecli.EffortHigh
	default:
		return ""
	}
}

// resolveModel maps a string model name to a claudecli.Model constant.
func resolveModel(name string) claudecli.Model {
	switch name {
	case "haiku":
		return claudecli.ModelHaiku
	case "sonnet":
		return claudecli.ModelSonnet
	default:
		return claudecli.ModelOpus
	}
}
