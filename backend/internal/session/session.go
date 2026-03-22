package session

import (
	"context"
	"fmt"
	"sync"

	claudecli "github.com/allbin/claudecli-go"
)

// Session wraps a single claudecli-go interactive session.
type Session struct {
	ID    string
	state string

	mu      sync.Mutex
	cliSess *claudecli.Session
	onEvent func(sessionID string, event any)
	onState func(sessionID string, state string)
}

func newSession(id string, cliSess *claudecli.Session, onEvent func(string, any), onState func(string, string)) *Session {
	s := &Session{
		ID:      id,
		state:   "idle",
		cliSess: cliSess,
		onEvent: onEvent,
		onState: onState,
	}
	onState(id, "idle")
	return s
}

// State returns the current session state.
func (s *Session) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Query sends a prompt to the Claude session and starts streaming events.
func (s *Session) Query(ctx context.Context, prompt string) error {
	s.mu.Lock()
	if s.state != "idle" {
		st := s.state
		s.mu.Unlock()
		return fmt.Errorf("session is %s, not idle", st)
	}
	s.state = "running"
	s.mu.Unlock()

	s.onState(s.ID, "running")

	if err := s.cliSess.Query(prompt); err != nil {
		s.setState("failed")
		return err
	}

	go s.streamEvents()
	return nil
}

func (s *Session) streamEvents() {
	for event := range s.cliSess.Events() {
		wireEvent := ToWireEvent(event)
		if wireEvent != nil {
			s.onEvent(s.ID, wireEvent)
		}
	}

	// Events channel closed -- turn complete.
	_, err := s.cliSess.Wait()
	if err != nil {
		s.setState("failed")
		return
	}
	s.setState("idle")
}

func (s *Session) setState(state string) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
	s.onState(s.ID, state)
}

// Close gracefully shuts down the claudecli-go session.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cliSess != nil {
		s.cliSess.Close()
		s.cliSess = nil
	}
	s.state = "done"
}
