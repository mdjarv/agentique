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
	ProjectID       string
	Name            string
	WorkDir         string
	WorktreePath    string
	WorktreeBranch  string
	WorktreeBaseSHA string
	Model           string
	PlanMode        bool
	AutoApprove     bool
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
func (m *Manager) Create(ctx context.Context, params CreateParams) (*Session, error) {
	id := uuid.New().String()

	// Build Session first (without cliSess) so the permission callback can capture it.
	sess := newSession(sessionParams{
		id:        id,
		projectID: params.ProjectID,
		queries:   m.queries,
		broadcast: m.broadcastFunc(params.ProjectID),
		turnIndex: -1, // first Query() will increment to 0
		workDir:   params.WorkDir,
	})

	// Set auto-approve before connecting so the callback has it immediately.
	if params.AutoApprove {
		sess.SetAutoApprove(true)
	}

	model := resolveModel(params.Model)
	connectOpts := []claudecli.Option{
		claudecli.WithWorkDir(params.WorkDir),
		claudecli.WithModel(model),
		claudecli.WithCanUseTool(sess.handleToolPermission),
		claudecli.WithUserInput(sess.handleUserInput),
		claudecli.WithIncludePartialMessages(),
	}
	// Pass plan mode as CLI flag to avoid post-connect control request race.
	if params.PlanMode {
		connectOpts = append(connectOpts, claudecli.WithPermissionMode(claudecli.PermissionPlan))
	}

	client := claudecli.New()
	cliSess, err := client.Connect(ctx, connectOpts...)
	if err != nil {
		return nil, err
	}

	_, dbErr := m.queries.CreateSession(ctx, store.CreateSessionParams{
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
		State: string(StateIdle),
		Model: params.Model,
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

// Resume reconnects to an existing Claude session using WithResume().
func (m *Manager) Resume(ctx context.Context, sessionID, claudeSessionID, projectID, workDir, model string) (*Session, error) {
	// Continue turn numbering from where we left off.
	maxTurn, _ := m.queries.MaxTurnIndex(ctx, sessionID)
	turnIndex := int(maxTurn)

	// Build Session first (without cliSess) so the permission callback can capture it.
	sess := newSession(sessionParams{
		id:        sessionID,
		projectID: projectID,
		queries:   m.queries,
		broadcast: m.broadcastFunc(projectID),
		turnIndex: turnIndex,
		workDir:   workDir,
	})
	sess.mu.Lock()
	sess.claudeSessionID = claudeSessionID
	sess.mu.Unlock()

	client := claudecli.New()
	cliSess, err := client.Connect(ctx,
		claudecli.WithWorkDir(workDir),
		claudecli.WithModel(resolveModel(model)),
		claudecli.WithCanUseTool(sess.handleToolPermission),
		claudecli.WithUserInput(sess.handleUserInput),
		claudecli.WithIncludePartialMessages(),
		claudecli.WithResume(claudeSessionID),
	)
	if err != nil {
		return nil, err
	}

	_ = m.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: string(StateIdle),
		ID:    sessionID,
	})

	sess.setCLISession(cliSess)

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

	slog.Info("session resumed", "session_id", sessionID, "claude_session_id", claudeSessionID)
	return sess, nil
}

// Get returns a live session by ID, or nil if not found.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
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
func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if sess != nil {
		sess.Close()
	}

	return m.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
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

	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range sessions {
		if live, ok := m.sessions[sessions[i].ID]; ok {
			sessions[i].State = string(live.State())
		}
	}

	return sessions, nil
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
