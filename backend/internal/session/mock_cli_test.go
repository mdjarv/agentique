package session

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	claudecli "github.com/allbin/claudecli-go"
)

// mockCLISession implements CLISession for tests.
type mockCLISession struct {
	events chan claudecli.Event

	mu           sync.Mutex
	queries      []string
	sentMessages []string
	closed       bool
	model        claudecli.Model
	permMode     claudecli.PermissionMode
	interrupted  bool
	cliState     claudecli.State // tests can flip this to simulate process death
}

func newMockCLISession() *mockCLISession {
	return &mockCLISession{
		events:   make(chan claudecli.Event, 64),
		cliState: claudecli.StateRunning,
	}
}

func (m *mockCLISession) State() claudecli.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cliState
}

func (m *mockCLISession) setCLIState(st claudecli.State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cliState = st
}

func (m *mockCLISession) Events() <-chan claudecli.Event { return m.events }

func (m *mockCLISession) Query(prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queries = append(m.queries, prompt)
	return nil
}

func (m *mockCLISession) QueryWithContent(prompt string, _ ...claudecli.ContentBlock) error {
	return m.Query(prompt)
}

func (m *mockCLISession) SendMessage(prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, prompt)
	return nil
}

func (m *mockCLISession) SendMessageWithContent(prompt string, _ ...claudecli.ContentBlock) error {
	return m.SendMessage(prompt)
}

func (m *mockCLISession) SetPermissionMode(mode claudecli.PermissionMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permMode = mode
	return nil
}

func (m *mockCLISession) SetModel(model claudecli.Model) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = model
	return nil
}

func (m *mockCLISession) ReconnectMCPServer(_ string) error                    { return nil }
func (m *mockCLISession) ReconnectMCPServerWait(_ string, _ time.Duration) error { return nil }

func (m *mockCLISession) Interrupt() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interrupted = true
	return nil
}

func (m *mockCLISession) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.events)
	}
	return nil
}

// sendEvents pushes events into the channel then closes it.
func (m *mockCLISession) sendEvents(events ...claudecli.Event) {
	for _, e := range events {
		m.events <- e
	}
	m.Close()
}

// mockConnector implements CLIConnector for tests.
type mockConnector struct {
	mu       sync.Mutex
	sessions []*mockCLISession
	nextIdx  int
	err      error
}

func newMockConnector(sessions ...*mockCLISession) *mockConnector {
	return &mockConnector{sessions: sessions}
}

func (c *mockConnector) Connect(_ context.Context, _ ...claudecli.Option) (CLISession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	if c.nextIdx >= len(c.sessions) {
		s := newMockCLISession()
		c.sessions = append(c.sessions, s)
		c.nextIdx++
		return s, nil
	}
	s := c.sessions[c.nextIdx]
	c.nextIdx++
	return s, nil
}

// last returns the most recently connected mock session.
func (c *mockConnector) last() *mockCLISession {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sessions) == 0 {
		return nil
	}
	return c.sessions[len(c.sessions)-1]
}

// mockBroadcaster implements Broadcaster for tests.
type mockBroadcaster struct {
	mu       sync.Mutex
	messages []broadcastMsg
}

type broadcastMsg struct {
	ProjectID string
	PushType  string
	Payload   any
}

func (b *mockBroadcaster) Broadcast(projectID, pushType string, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, broadcastMsg{
		ProjectID: projectID,
		PushType:  pushType,
		Payload:   payload,
	})
}

func (b *mockBroadcaster) messagesOfType(pushType string) []broadcastMsg {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []broadcastMsg
	for _, m := range b.messages {
		if m.PushType == pushType {
			out = append(out, m)
		}
	}
	return out
}

// mockBlockingRunner implements msggen.Runner for tests.
type mockBlockingRunner struct {
	result *claudecli.BlockingResult
	err    error
}

func (r *mockBlockingRunner) RunBlocking(_ context.Context, _ string, _ ...claudecli.Option) (*claudecli.BlockingResult, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.result != nil {
		return r.result, nil
	}
	return &claudecli.BlockingResult{Text: "mock result"}, nil
}

// Helper to create a ResultEvent for tests.
func testResultEvent(cost float64) *claudecli.ResultEvent {
	return &claudecli.ResultEvent{
		CostUSD:    cost,
		Duration:   100 * time.Millisecond,
		StopReason: "end_turn",
	}
}

// Helper to create a TextEvent for tests.
func testTextEvent(text string) *claudecli.TextEvent {
	return &claudecli.TextEvent{Content: text}
}

// Helper to create a ToolUseEvent for tests.
func testToolUseEvent(id, name string, input any) *claudecli.ToolUseEvent {
	raw, _ := json.Marshal(input)
	return &claudecli.ToolUseEvent{
		ID:    id,
		Name:  name,
		Input: raw,
	}
}
