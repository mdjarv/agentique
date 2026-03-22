package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/store"
)

func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// Session state constants.
const (
	StateIdle    = "idle"
	StateRunning = "running"
	StateFailed  = "failed"
	StateDone    = "done"
	StateStopped = "stopped"
)

// Session wraps a single claudecli-go interactive session.
type Session struct {
	ID        string
	ProjectID string

	mu              sync.Mutex
	state           string
	cliSess         *claudecli.Session
	queryCount      int
	claudeSessionID string
	turnIndex       int
	seqInTurn       int
	queries         *store.Queries
	broadcast       func(pushType string, payload any)
}

type sessionParams struct {
	id        string
	projectID string
	cliSess   *claudecli.Session
	queries   *store.Queries
	broadcast func(pushType string, payload any)
	turnIndex int
}

func newSession(p sessionParams) *Session {
	s := &Session{
		ID:        p.id,
		ProjectID: p.projectID,
		state:     StateIdle,
		cliSess:   p.cliSess,
		queries:   p.queries,
		broadcast: p.broadcast,
		turnIndex: p.turnIndex,
	}
	s.broadcastState(StateIdle)
	s.startEventLoop()
	return s
}

// State returns the current session state.
func (s *Session) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// ClaudeSessionID returns the Claude CLI session ID, if available.
func (s *Session) ClaudeSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claudeSessionID
}

// QueryCount returns the number of queries sent to this session.
func (s *Session) QueryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryCount
}

// Query sends a prompt to the Claude session and starts streaming events.
func (s *Session) Query(ctx context.Context, prompt string) error {
	s.mu.Lock()
	if s.state != StateIdle && s.state != StateDone {
		st := s.state
		s.mu.Unlock()
		return fmt.Errorf("session is %s, not %s", st, StateIdle)
	}
	s.state = StateRunning
	s.queryCount++
	s.turnIndex++
	s.seqInTurn = 0
	s.mu.Unlock()

	// Persist prompt as seq 0 of the new turn.
	promptData, _ := json.Marshal(map[string]string{"prompt": prompt})
	_ = s.queries.InsertEvent(context.Background(), store.InsertEventParams{
		SessionID: s.ID,
		TurnIndex: int64(s.turnIndex),
		Seq:       0,
		Type:      "prompt",
		Data:      string(promptData),
	})
	s.mu.Lock()
	s.seqInTurn = 1
	s.mu.Unlock()

	s.broadcastState(StateRunning)

	if err := s.cliSess.Query(prompt); err != nil {
		s.setState(StateFailed)
		return err
	}

	return nil
}

// startEventLoop reads events from the claudecli-go session, persists them
// to the database, and broadcasts them to all project WebSocket clients.
func (s *Session) startEventLoop() {
	go func() {
		for event := range s.cliSess.Events() {
			// Capture Claude session ID from InitEvent.
			if initEv, ok := event.(*claudecli.InitEvent); ok {
				s.mu.Lock()
				if s.claudeSessionID == "" && initEv.SessionID != "" {
					s.claudeSessionID = initEv.SessionID
					_ = s.queries.UpdateClaudeSessionID(context.Background(), store.UpdateClaudeSessionIDParams{
						ClaudeSessionID: sqlNullString(initEv.SessionID),
						ID:              s.ID,
					})
					log.Printf("session %s: captured claude session ID %s", s.ID, initEv.SessionID)
				}
				s.mu.Unlock()
				continue
			}

			wireEvent := ToWireEvent(event)
			if wireEvent == nil {
				continue
			}

			// Persist to DB.
			s.mu.Lock()
			seq := s.seqInTurn
			turnIdx := s.turnIndex
			s.seqInTurn++
			s.mu.Unlock()

			if data, err := json.Marshal(wireEvent); err == nil {
				typed, _ := wireEvent.(interface{ WireType() string })
				_ = s.queries.InsertEvent(context.Background(), store.InsertEventParams{
					SessionID: s.ID,
					TurnIndex: int64(turnIdx),
					Seq:       int64(seq),
					Type:      typed.WireType(),
					Data:      string(data),
				})
			}

			// Broadcast to all project clients.
			s.broadcast("session.event", map[string]any{
				"sessionId": s.ID,
				"event":     wireEvent,
			})

			// ResultEvent marks the end of a turn.
			if _, ok := event.(*claudecli.ResultEvent); ok {
				s.setState(StateIdle)
			}

			// Fatal error ends the session.
			if errEv, ok := event.(*claudecli.ErrorEvent); ok && errEv.Fatal {
				s.setState(StateFailed)
			}
		}

		// Channel closed means session process ended.
		s.setState(StateDone)
	}()
}

func (s *Session) setState(state string) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
	s.broadcastState(state)
	_ = s.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: state,
		ID:    s.ID,
	})
}

func (s *Session) broadcastState(state string) {
	s.broadcast("session.state", map[string]any{
		"sessionId": s.ID,
		"state":     state,
	})
}

// Close gracefully shuts down the claudecli-go session.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cliSess != nil {
		s.cliSess.Close()
		s.cliSess = nil
	}
	s.state = StateDone
}
