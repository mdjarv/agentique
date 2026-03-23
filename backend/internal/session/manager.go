package session

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
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
	})

	model := resolveModel(params.Model)
	client := claudecli.New()
	cliSess, err := client.Connect(ctx,
		claudecli.WithWorkDir(params.WorkDir),
		claudecli.WithModel(model),
		claudecli.WithCanUseTool(sess.handleToolPermission),
	)
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
		State: StateIdle,
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
	})
	sess.mu.Lock()
	sess.claudeSessionID = claudeSessionID
	sess.mu.Unlock()

	client := claudecli.New()
	cliSess, err := client.Connect(ctx,
		claudecli.WithWorkDir(workDir),
		claudecli.WithModel(resolveModel(model)),
		claudecli.WithCanUseTool(sess.handleToolPermission),
		claudecli.WithResume(claudeSessionID),
	)
	if err != nil {
		return nil, err
	}

	_ = m.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: StateIdle,
		ID:    sessionID,
	})

	sess.setCLISession(cliSess)

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

	log.Printf("session %s: resumed claude session %s", sessionID, claudeSessionID)
	return sess, nil
}

// Get returns a live session by ID, or nil if not found.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Stop closes a session, updates DB state to "stopped", and removes worktree if present.
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

	_ = m.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: StateStopped,
		ID:    id,
	})

	dbSess, err := m.queries.GetSession(ctx, id)
	if err == nil && dbSess.WorktreePath.Valid && dbSess.WorktreePath.String != "" {
		project, projErr := m.queries.GetProject(ctx, dbSess.ProjectID)
		if projErr == nil {
			RemoveWorktree(project.Path, dbSess.WorktreePath.String)
		}
	}

	return nil
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
			sessions[i].State = live.State()
		}
	}

	return sessions, nil
}

// GetDiff returns the diff for a worktree session.
func (m *Manager) GetDiff(ctx context.Context, sessionID string) (DiffResult, error) {
	dbSess, err := m.queries.GetSession(ctx, sessionID)
	if err != nil {
		return DiffResult{}, fmt.Errorf("session not found: %w", err)
	}

	if !dbSess.WorktreePath.Valid || dbSess.WorktreePath.String == "" {
		return DiffResult{}, fmt.Errorf("session has no worktree")
	}

	worktreePath := dbSess.WorktreePath.String
	if _, err := os.Stat(worktreePath); err != nil {
		return DiffResult{}, fmt.Errorf("worktree path not accessible: %w", err)
	}

	baseSHA := ""
	if dbSess.WorktreeBaseSha.Valid {
		baseSHA = dbSess.WorktreeBaseSha.String
	}

	return WorktreeDiff(worktreePath, baseSHA)
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
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				log.Printf("session %s: close timed out, abandoning", s.ID)
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
