package session

import (
	"context"
	"sync"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/google/uuid"
)

// Manager manages the lifecycle of claudecli-go sessions.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// Create starts a new claudecli-go session for the given project directory.
// The onEvent and onState callbacks receive the session ID as their first argument
// so callers can identify the session without capturing a pointer that isn't yet assigned.
func (m *Manager) Create(ctx context.Context, workDir string, onEvent func(string, any), onState func(string, string)) (*Session, error) {
	client := claudecli.New()
	cliSess, err := client.Connect(ctx,
		claudecli.WithWorkDir(workDir),
		claudecli.WithModel(claudecli.ModelOpus),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
	)
	if err != nil {
		return nil, err
	}

	id := uuid.New().String()
	sess := newSession(id, cliSess, onEvent, onState)

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	return sess, nil
}

// Get returns a session by ID, or nil if not found.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// CloseAll gracefully closes all sessions.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}
