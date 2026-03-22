package session

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// CreateParams holds the parameters for creating a new session.
type CreateParams struct {
	ProjectID      string
	Name           string
	WorkDir        string
	WorktreePath   string // empty if no worktree
	WorktreeBranch string
}

// Manager manages the lifecycle of claudecli-go sessions.
// It is a server-level singleton that persists session metadata to the database.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	queries  *store.Queries
}

// NewManager creates a new session manager backed by the given database queries.
func NewManager(queries *store.Queries) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		queries:  queries,
	}
}

// Create starts a new claudecli-go session, persists metadata to DB, and returns the session.
func (m *Manager) Create(ctx context.Context, params CreateParams, onEvent func(string, any), onState func(string, string)) (*Session, error) {
	client := claudecli.New()
	cliSess, err := client.Connect(ctx,
		claudecli.WithWorkDir(params.WorkDir),
		claudecli.WithModel(claudecli.ModelOpus),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
	)
	if err != nil {
		return nil, err
	}

	id := uuid.New().String()

	// Persist to DB.
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
		State: StateIdle,
	})
	if dbErr != nil {
		cliSess.Close()
		return nil, dbErr
	}

	// Wrap the onState callback to also persist state changes to DB.
	wrappedOnState := func(sessionID string, state string) {
		_ = m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
			State: state,
			ID:    sessionID,
		})
		onState(sessionID, state)
	}

	sess := newSession(id, cliSess, onEvent, wrappedOnState)

	// Persist Claude session ID when it arrives from the CLI.
	sess.SetClaudeIDCallback(func(sessionID, claudeSessionID string) {
		_ = m.queries.UpdateClaudeSessionID(context.Background(), store.UpdateClaudeSessionIDParams{
			ClaudeSessionID: sql.NullString{String: claudeSessionID, Valid: true},
			ID:              sessionID,
		})
		log.Printf("session %s: captured claude session ID %s", sessionID, claudeSessionID)
	})

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	return sess, nil
}

// Resume reconnects to an existing Claude session using WithResume().
func (m *Manager) Resume(ctx context.Context, sessionID, claudeSessionID, workDir string, onEvent func(string, any), onState func(string, string)) (*Session, error) {
	client := claudecli.New()
	cliSess, err := client.Connect(ctx,
		claudecli.WithWorkDir(workDir),
		claudecli.WithModel(claudecli.ModelOpus),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
		claudecli.WithResume(claudeSessionID),
	)
	if err != nil {
		return nil, err
	}

	// Update DB state back to idle.
	_ = m.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: StateIdle,
		ID:    sessionID,
	})

	// Wrap the onState callback to also persist state changes to DB.
	wrappedOnState := func(sid string, state string) {
		_ = m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
			State: state,
			ID:    sid,
		})
		onState(sid, state)
	}

	sess := newSession(sessionID, cliSess, onEvent, wrappedOnState)
	sess.mu.Lock()
	sess.claudeSessionID = claudeSessionID
	sess.mu.Unlock()

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

	// Update DB state to stopped.
	_ = m.queries.UpdateSessionState(ctx, store.UpdateSessionStateParams{
		State: StateStopped,
		ID:    id,
	})

	// Clean up worktree if present.
	dbSess, err := m.queries.GetSession(ctx, id)
	if err == nil && dbSess.WorktreePath.Valid && dbSess.WorktreePath.String != "" {
		// Look up the project to get its path for the git worktree remove command.
		project, projErr := m.queries.GetProject(ctx, dbSess.ProjectID)
		if projErr == nil {
			RemoveWorktree(project.Path, dbSess.WorktreePath.String)
		} else {
			log.Printf("warning: could not look up project %s for worktree cleanup: %v", dbSess.ProjectID, projErr)
		}
	}

	if !ok {
		// Session was not live, but we still updated DB -- not an error.
		return nil
	}

	return nil
}

// ListByProject returns session metadata from DB.
// For live sessions, the persisted state is overridden with the actual live state.
// For non-live sessions with a Claude session ID, state is shown as "idle" (resumable).
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
		} else if sessions[i].ClaudeSessionID.Valid && sessions[i].ClaudeSessionID.String != "" {
			// Not live but has a Claude session ID -- resumable.
			if sessions[i].State != StateStopped {
				sessions[i].State = StateIdle
			}
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
