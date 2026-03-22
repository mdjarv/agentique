package session

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/store"
)

func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// QueryAttachment represents a base64-encoded file (image or PDF) attached to a query.
type QueryAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	DataUrl  string `json:"dataUrl"`
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

// Query sends a prompt (with optional images) to the Claude session and starts streaming events.
func (s *Session) Query(ctx context.Context, prompt string, attachments []QueryAttachment) error {
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

	// Persist prompt (and images) as seq 0 of the new turn.
	promptPayload := map[string]any{"prompt": prompt}
	if len(attachments) > 0 {
		promptPayload["attachments"] = attachments
	}
	promptData, _ := json.Marshal(promptPayload)
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

	if len(attachments) == 0 {
		if err := s.cliSess.Query(prompt); err != nil {
			s.setState(StateFailed)
			return err
		}
		return nil
	}

	blocks, err := toContentBlocks(attachments)
	if err != nil {
		s.setState(StateFailed)
		return fmt.Errorf("parse attachments: %w", err)
	}
	if err := s.cliSess.QueryWithContent(prompt, blocks...); err != nil {
		s.setState(StateFailed)
		return err
	}
	return nil
}

// toContentBlocks converts frontend attachments (data URLs) to claudecli ContentBlocks.
func toContentBlocks(attachments []QueryAttachment) ([]claudecli.ContentBlock, error) {
	blocks := make([]claudecli.ContentBlock, 0, len(attachments))
	for _, a := range attachments {
		mediaType, data, err := parseDataUrl(a.DataUrl)
		if err != nil {
			return nil, fmt.Errorf("attachment %q: %w", a.Name, err)
		}
		if strings.HasPrefix(mediaType, "image/") {
			blocks = append(blocks, claudecli.ImageBlock(mediaType, data))
		} else {
			blocks = append(blocks, claudecli.DocumentBlock(mediaType, data))
		}
	}
	return blocks, nil
}

// parseDataUrl extracts the media type and decoded bytes from a data URL.
func parseDataUrl(dataUrl string) (mediaType string, data []byte, err error) {
	// Format: data:<mediaType>;base64,<data>
	if !strings.HasPrefix(dataUrl, "data:") {
		return "", nil, fmt.Errorf("not a data URL")
	}
	rest := dataUrl[5:]
	semi := strings.Index(rest, ";")
	if semi < 0 {
		return "", nil, fmt.Errorf("missing ;base64 separator")
	}
	mediaType = rest[:semi]
	after := rest[semi+1:]
	if !strings.HasPrefix(after, "base64,") {
		return "", nil, fmt.Errorf("not base64-encoded")
	}
	b64 := after[7:]
	data, err = base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", nil, fmt.Errorf("decode base64: %w", err)
	}
	return mediaType, data, nil
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
