package session

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/allbin/agentkit/runtime"
	claudecli "github.com/allbin/claudecli-go"
)

// mockCLISession implements runtime.CLISession for tests.
type mockCLISession struct {
	events chan runtime.CLIEvent

	mu           sync.Mutex
	queries      []string
	sentMessages []string
	closed       bool
	model        string
	planMode     runtime.PlanMode
	interrupted  bool
	cliState     runtime.SessionState // tests can flip this to simulate process death
}

func newMockCLISession() *mockCLISession {
	return &mockCLISession{
		events:   make(chan runtime.CLIEvent, 64),
		cliState: runtime.SessionStateRunning,
	}
}

func (m *mockCLISession) State() runtime.SessionState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cliState
}

func (m *mockCLISession) setCLIState(st runtime.SessionState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cliState = st
}

func (m *mockCLISession) ProcessInfo() runtime.ProcessInfo {
	return runtime.ProcessInfo{
		LastStdoutAt: time.Now(),
		Lifecycle:    m.State(),
	}
}

func (m *mockCLISession) Ping(_ context.Context, _ time.Duration) error { return nil }

func (m *mockCLISession) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{Provider: "mock"}
}

func (m *mockCLISession) Events() <-chan runtime.CLIEvent { return m.events }

func (m *mockCLISession) Query(_ context.Context, prompt string, _ ...runtime.Attachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queries = append(m.queries, prompt)
	return nil
}

func (m *mockCLISession) SendMessage(_ context.Context, prompt string, _ ...runtime.Attachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, prompt)
	return nil
}

func (m *mockCLISession) SetPlanMode(_ context.Context, mode runtime.PlanMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.planMode = mode
	return nil
}

func (m *mockCLISession) SetModel(_ context.Context, model string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = model
	return nil
}

func (m *mockCLISession) Interrupt(_ context.Context) error {
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
func (m *mockCLISession) sendEvents(events ...runtime.CLIEvent) {
	for _, e := range events {
		m.events <- e
	}
	m.Close()
}

// mockConnector implements runtime.CLIConnector for tests.
type mockConnector struct {
	mu       sync.Mutex
	sessions []*mockCLISession
	nextIdx  int
	err      error
}

func newMockConnector(sessions ...*mockCLISession) *mockConnector {
	return &mockConnector{sessions: sessions}
}

func (c *mockConnector) Connect(_ context.Context, _ runtime.ConnectParams) (runtime.CLISession, error) {
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

// mockBroadcaster satisfies eventbus.Broadcaster for tests.
type mockBroadcaster struct {
	mu       sync.Mutex
	messages []broadcastMsg
}

type broadcastMsg struct {
	ProjectID string // empty for global broadcasts
	PushType  string
	Payload   any
}

func (b *mockBroadcaster) Publish(topic, eventType string, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, broadcastMsg{
		ProjectID: topic,
		PushType:  eventType,
		Payload:   payload,
	})
}

func (b *mockBroadcaster) Broadcast(eventType string, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, broadcastMsg{
		PushType: eventType,
		Payload:  payload,
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

// Helper to create a TurnCompletedEvent for tests.
func testResultEvent(cost float64) runtime.TurnCompletedEvent {
	return runtime.TurnCompletedEvent{
		Status:     runtime.TurnStatusCompleted,
		CostUSD:    cost,
		Duration:   100 * time.Millisecond,
		StopReason: "end_turn",
	}
}

// Helper to create an AssistantTextEvent for tests.
func testTextEvent(text string) runtime.AssistantTextEvent {
	return runtime.AssistantTextEvent{Content: text}
}

// Helper to create a ToolUseEvent for tests.
func testToolUseEvent(id, name string, input any) runtime.ToolUseEvent {
	raw, _ := json.Marshal(input)
	return runtime.ToolUseEvent{
		ID:    id,
		Name:  name,
		Input: raw,
	}
}
