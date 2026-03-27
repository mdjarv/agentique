// Package testmode provides mock implementations for hybrid E2E testing.
// Used when the server starts with --test-mode: real HTTP, real WebSocket,
// real SQLite, real state machine — only the Claude CLI is mocked.
package testmode

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/session"
)

// Connector implements session.CLIConnector with mock sessions.
// Test endpoints use it to inject events into live sessions.
type Connector struct {
	mu       sync.Mutex
	pending  []*Session            // connected but not yet associated
	sessions map[string]*Session   // agentique session ID → mock session
	behavior map[string][]Scenario // session ID → scripted event sequences
}

// NewConnector creates a Connector ready for test use.
func NewConnector() *Connector {
	return &Connector{
		sessions: make(map[string]*Session),
		behavior: make(map[string][]Scenario),
	}
}

// Connect implements session.CLIConnector.
func (c *Connector) Connect(_ context.Context, _ ...claudecli.Option) (session.CLISession, error) {
	s := NewSession()
	c.mu.Lock()
	c.pending = append(c.pending, s)
	c.mu.Unlock()
	return s, nil
}

// Associate maps the most recently connected mock session to an Agentique
// session ID. Returns the mock session, or nil if no pending sessions exist.
func (c *Connector) Associate(sessionID string) *Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) == 0 {
		return nil
	}
	s := c.pending[0]
	c.pending = c.pending[1:]
	c.sessions[sessionID] = s

	// Attach scripted behavior if configured.
	if scenarios, ok := c.behavior[sessionID]; ok {
		s.mu.Lock()
		s.scenarios = scenarios
		s.mu.Unlock()
	}
	return s
}

// Get returns the mock session for an Agentique session ID.
func (c *Connector) Get(sessionID string) *Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[sessionID]
}

// SetBehavior pre-configures scripted event sequences for a session ID.
// When the session receives a Query, it replays the next scenario.
func (c *Connector) SetBehavior(sessionID string, scenarios []Scenario) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.behavior[sessionID] = scenarios

	// If session is already associated, update it directly.
	if s, ok := c.sessions[sessionID]; ok {
		s.mu.Lock()
		s.scenarios = scenarios
		s.mu.Unlock()
	}
}

// Reset clears all sessions and behaviors.
func (c *Connector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.sessions {
		s.Close()
	}
	c.pending = nil
	c.sessions = make(map[string]*Session)
	c.behavior = make(map[string][]Scenario)
}

// Scenario is a scripted sequence of events replayed on Query.
type Scenario struct {
	Events []ScriptedEvent `json:"events"`
}

// ScriptedEvent is a single event with an optional delay.
type ScriptedEvent struct {
	Delay int             `json:"delay"` // milliseconds
	Event json.RawMessage `json:"event"` // wire event JSON
}

// Session implements session.CLISession for testing.
type Session struct {
	events chan claudecli.Event

	mu          sync.Mutex
	queries     []string
	closed      bool
	model       claudecli.Model
	permMode    claudecli.PermissionMode
	interrupted bool
	scenarios   []Scenario
	scenarioIdx int
}

// NewSession creates a mock CLI session with a buffered event channel.
func NewSession() *Session {
	return &Session{
		events: make(chan claudecli.Event, 64),
	}
}

func (s *Session) Events() <-chan claudecli.Event { return s.events }

func (s *Session) Query(prompt string) error {
	s.mu.Lock()
	s.queries = append(s.queries, prompt)
	// Check for scripted scenario.
	var scenario *Scenario
	if s.scenarioIdx < len(s.scenarios) {
		scenario = &s.scenarios[s.scenarioIdx]
		s.scenarioIdx++
	}
	s.mu.Unlock()

	if scenario != nil {
		go s.replayScenario(scenario)
	}
	return nil
}

func (s *Session) QueryWithContent(prompt string, _ ...claudecli.ContentBlock) error {
	return s.Query(prompt)
}

func (s *Session) SetPermissionMode(mode claudecli.PermissionMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.permMode = mode
	return nil
}

func (s *Session) SetModel(model claudecli.Model) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = model
	return nil
}

func (s *Session) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interrupted = true
	return nil
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
	return nil
}

// InjectEvent pushes a claudecli.Event into the session's channel.
func (s *Session) InjectEvent(event claudecli.Event) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.mu.Unlock()

	select {
	case s.events <- event:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("event channel full or blocked")
	}
}

// replayScenario pushes scripted events with delays.
func (s *Session) replayScenario(sc *Scenario) {
	for _, se := range sc.Events {
		if se.Delay > 0 {
			time.Sleep(time.Duration(se.Delay) * time.Millisecond)
		}
		event, err := parseWireToClaudeEvent(se.Event)
		if err != nil {
			continue
		}
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		s.events <- event
	}
}

// parseWireToClaudeEvent converts wire event JSON to a claudecli.Event.
func parseWireToClaudeEvent(raw json.RawMessage) (claudecli.Event, error) {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, err
	}

	switch base.Type {
	case "text":
		var e struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		return &claudecli.TextEvent{Content: e.Content}, nil

	case "thinking":
		var e struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		return &claudecli.ThinkingEvent{Content: e.Content}, nil

	case "tool_use":
		var e struct {
			ToolID    string          `json:"toolId"`
			ToolName  string          `json:"toolName"`
			ToolInput json.RawMessage `json:"toolInput"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		return &claudecli.ToolUseEvent{ID: e.ToolID, Name: e.ToolName, Input: e.ToolInput}, nil

	case "tool_result":
		var e struct {
			ToolID  string `json:"toolId"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		var blocks []claudecli.ToolContent
		for _, b := range e.Content {
			blocks = append(blocks, claudecli.ToolContent{Type: b.Type, Text: b.Text})
		}
		return &claudecli.ToolResultEvent{ToolUseID: e.ToolID, Content: blocks}, nil

	case "result":
		var e struct {
			StopReason string  `json:"stopReason"`
			Cost       float64 `json:"cost"`
			Duration   int64   `json:"duration"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		return &claudecli.ResultEvent{
			StopReason: e.StopReason,
			CostUSD:    e.Cost,
			Duration:   time.Duration(e.Duration) * time.Millisecond,
		}, nil

	case "error":
		var e struct {
			Message string `json:"message"`
			Fatal   bool   `json:"fatal"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		return &claudecli.ErrorEvent{
			Err:   fmt.Errorf("%s", e.Message),
			Fatal: e.Fatal,
		}, nil

	case "compact_boundary":
		var e struct {
			Trigger   string `json:"trigger"`
			PreTokens int    `json:"preTokens"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		return &claudecli.CompactBoundaryEvent{
			Trigger:   e.Trigger,
			PreTokens: e.PreTokens,
		}, nil

	default:
		return nil, fmt.Errorf("unknown event type: %s", base.Type)
	}
}

// BlockingRunner implements session.BlockingRunner for testing.
type BlockingRunner struct {
	mu     sync.Mutex
	result *claudecli.BlockingResult
	err    error
}

// NewBlockingRunner creates a mock runner that returns a default result.
func NewBlockingRunner() *BlockingRunner {
	return &BlockingRunner{}
}

func (r *BlockingRunner) RunBlocking(_ context.Context, _ string, _ ...claudecli.Option) (*claudecli.BlockingResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	if r.result != nil {
		return r.result, nil
	}
	return &claudecli.BlockingResult{Text: "TITLE: test commit\nDESCRIPTION:\ntest description"}, nil
}

// SetResult configures the next RunBlocking return value.
func (r *BlockingRunner) SetResult(result *claudecli.BlockingResult, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.result = result
	r.err = err
}
