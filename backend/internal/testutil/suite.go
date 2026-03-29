package testutil

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	dbpkg "github.com/allbin/agentique/backend/db"
	"github.com/allbin/agentique/backend/internal/store"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/stretchr/testify/suite"
)

// DBSuite provides a fresh SQLite database per test method.
// Embed this in domain-specific suites.
type DBSuite struct {
	suite.Suite
	DB          *sql.DB
	Queries     *store.Queries
	Project     store.Project
	Broadcaster *RecordingBroadcaster
	Connector   *RecordingConnector
}

// SetupTest creates a fresh temp DB + default project before each test method.
func (s *DBSuite) SetupTest() {
	t := s.T()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	s.Require().NoError(err)
	t.Cleanup(func() { db.Close() })

	s.Require().NoError(store.RunMigrations(db, dbpkg.Migrations))

	s.DB = db
	s.Queries = store.New(db)
	s.Project = SeedProject(t, s.Queries, "test-project", t.TempDir())
	s.Broadcaster = NewRecordingBroadcaster()
	s.Connector = NewRecordingConnector()
}

// --- RecordingBroadcaster ---

// BroadcastMessage is a captured broadcast call.
type BroadcastMessage struct {
	ProjectID string
	PushType  string
	Payload   any
}

// RecordingBroadcaster captures all Broadcast calls for assertions.
type RecordingBroadcaster struct {
	mu       sync.Mutex
	messages []BroadcastMessage
	notify   chan struct{}
}

func NewRecordingBroadcaster() *RecordingBroadcaster {
	return &RecordingBroadcaster{
		notify: make(chan struct{}, 128),
	}
}

func (b *RecordingBroadcaster) Broadcast(projectID, pushType string, payload any) {
	b.mu.Lock()
	b.messages = append(b.messages, BroadcastMessage{
		ProjectID: projectID,
		PushType:  pushType,
		Payload:   payload,
	})
	b.mu.Unlock()

	select {
	case b.notify <- struct{}{}:
	default:
	}
}

// Messages returns a copy of all captured messages.
func (b *RecordingBroadcaster) Messages() []BroadcastMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BroadcastMessage, len(b.messages))
	copy(out, b.messages)
	return out
}

// MessagesOfType returns messages matching the given push type.
func (b *RecordingBroadcaster) MessagesOfType(pushType string) []BroadcastMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []BroadcastMessage
	for _, m := range b.messages {
		if m.PushType == pushType {
			out = append(out, m)
		}
	}
	return out
}

// WaitForType blocks until a message of the given type appears or timeout.
func (b *RecordingBroadcaster) WaitForType(pushType string, timeout time.Duration) (BroadcastMessage, bool) {
	deadline := time.After(timeout)
	for {
		if msgs := b.MessagesOfType(pushType); len(msgs) > 0 {
			return msgs[len(msgs)-1], true
		}
		select {
		case <-b.notify:
		case <-deadline:
			return BroadcastMessage{}, false
		}
	}
}

// Reset clears all captured messages.
func (b *RecordingBroadcaster) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = nil
}

// --- RecordingConnector ---

// RecordingConnector tracks MockCLISessions created during tests.
// It does NOT directly satisfy session.CLIConnector (different packages
// means different interface types). Test suites wrap it with a one-line
// adapter — see connectorAdapter in the lifecycle suite.
type RecordingConnector struct {
	mu       sync.Mutex
	sessions []*MockCLISession
	nextIdx  int
	Err      error // if set, NextSession returns this error
}

func NewRecordingConnector() *RecordingConnector {
	return &RecordingConnector{}
}

// NextSession returns the next pre-configured session or creates a new one.
// Called by the adapter's Connect method.
func (c *RecordingConnector) NextSession() (*MockCLISession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Err != nil {
		return nil, c.Err
	}
	if c.nextIdx >= len(c.sessions) {
		s := NewMockCLISession()
		c.sessions = append(c.sessions, s)
		c.nextIdx++
		return s, nil
	}
	s := c.sessions[c.nextIdx]
	c.nextIdx++
	return s, nil
}

// Last returns the most recently connected mock session.
func (c *RecordingConnector) Last() *MockCLISession {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sessions) == 0 {
		return nil
	}
	return c.sessions[len(c.sessions)-1]
}

// SessionCount returns the number of sessions created.
func (c *RecordingConnector) SessionCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sessions)
}

// --- MockCLISession ---

// MockCLISession implements the CLISession interface for tests.
type MockCLISession struct {
	Events_ chan claudecli.Event

	mu           sync.Mutex
	queries      []string
	sentMessages []string
	closed       bool
	model        claudecli.Model
	permMode     claudecli.PermissionMode
	interrupted  bool
}

func NewMockCLISession() *MockCLISession {
	return &MockCLISession{
		Events_: make(chan claudecli.Event, 64),
	}
}

func (m *MockCLISession) Events() <-chan claudecli.Event { return m.Events_ }

func (m *MockCLISession) Query(prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queries = append(m.queries, prompt)
	return nil
}

func (m *MockCLISession) QueryWithContent(prompt string, _ ...claudecli.ContentBlock) error {
	return m.Query(prompt)
}

func (m *MockCLISession) SendMessage(prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, prompt)
	return nil
}

func (m *MockCLISession) SendMessageWithContent(prompt string, _ ...claudecli.ContentBlock) error {
	return m.SendMessage(prompt)
}

func (m *MockCLISession) SetPermissionMode(mode claudecli.PermissionMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permMode = mode
	return nil
}

func (m *MockCLISession) SetModel(model claudecli.Model) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = model
	return nil
}

func (m *MockCLISession) Interrupt() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interrupted = true
	return nil
}

func (m *MockCLISession) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.Events_)
	}
	return nil
}

// Queries returns the list of prompts sent via Query.
func (m *MockCLISession) Queries() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.queries))
	copy(out, m.queries)
	return out
}

// SentMessages returns a copy of all messages sent via SendMessage.
func (m *MockCLISession) SentMessages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.sentMessages))
	copy(out, m.sentMessages)
	return out
}

// SendEvents pushes events then closes the channel.
func (m *MockCLISession) SendEvents(events ...claudecli.Event) {
	for _, e := range events {
		m.Events_ <- e
	}
	m.Close()
}

// Inject pushes a single event without closing.
func (m *MockCLISession) Inject(event claudecli.Event) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	m.mu.Unlock()

	select {
	case m.Events_ <- event:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("event channel full")
	}
}

// --- MockBlockingRunner ---

// MockBlockingRunner implements msggen.Runner for tests.
type MockBlockingRunner struct {
	mu     sync.Mutex
	result *claudecli.BlockingResult
	err    error
}

func NewMockBlockingRunner() *MockBlockingRunner {
	return &MockBlockingRunner{}
}

func (r *MockBlockingRunner) RunBlocking(_ context.Context, _ string, _ ...claudecli.Option) (*claudecli.BlockingResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	if r.result != nil {
		return r.result, nil
	}
	return &claudecli.BlockingResult{Text: "mock result"}, nil
}

// SetResult configures the next RunBlocking return value.
func (r *MockBlockingRunner) SetResult(result *claudecli.BlockingResult, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.result = result
	r.err = err
}

// --- Event helpers ---

func TextEvent(content string) *claudecli.TextEvent {
	return &claudecli.TextEvent{Content: content}
}

func ThinkingEvent(content string) *claudecli.ThinkingEvent {
	return &claudecli.ThinkingEvent{Content: content}
}

func ResultEvent(cost float64) *claudecli.ResultEvent {
	return &claudecli.ResultEvent{
		CostUSD:    cost,
		Duration:   100 * time.Millisecond,
		StopReason: "end_turn",
	}
}

func ToolUseEvent(id, name string, input any) *claudecli.ToolUseEvent {
	raw, _ := json.Marshal(input)
	return &claudecli.ToolUseEvent{
		ID:    id,
		Name:  name,
		Input: raw,
	}
}

func ToolResultEvent(toolUseID, text string) *claudecli.ToolResultEvent {
	return &claudecli.ToolResultEvent{
		ToolUseID: toolUseID,
		Content:   []claudecli.ToolContent{{Type: "text", Text: text}},
	}
}

func ErrorEvent(msg string, fatal bool) *claudecli.ErrorEvent {
	return &claudecli.ErrorEvent{
		Err:   fmt.Errorf("%s", msg),
		Fatal: fatal,
	}
}
