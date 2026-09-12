package assistant

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/allbin/agentkit/eventbus"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/mdjarv/agentique/backend/internal/usage"
)

// newTestService builds a service over a real temp database, because the
// journal's dedupe, its ordering and the seen marks are SQL behaviour and a
// stub store would test the stub.
func newTestService(t *testing.T, opts ...Option) (*Service, *store.Queries, *eventbus.Recorder) {
	t.Helper()

	_, queries := testutil.SetupDB(t)
	recorder := &eventbus.Recorder{}
	opts = append([]Option{WithBroadcaster(recorder), WithLogger(testLogger())}, opts...)

	svc, err := New(queries, opts...)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc, queries, recorder
}

// primed builds a service whose session.state baseline is loaded, the way the
// wiring does before it subscribes to the bus.
func primed(t *testing.T, opts ...Option) (*Service, *store.Queries) {
	t.Helper()
	svc, queries, _ := newTestService(t, opts...)
	if err := svc.PrimeSessionStates(context.Background()); err != nil {
		t.Fatalf("PrimeSessionStates() = %v", err)
	}
	return svc, queries
}

// seedSession inserts a project and a session, which the follow list needs: a
// follow references a real session row.
func seedSession(t *testing.T, queries *store.Queries) store.Session {
	t.Helper()
	project := testutil.SeedProject(t, queries, "riff", t.TempDir())
	return testutil.SeedSession(t, queries, project.ID, "idle")
}

// seedSessionIn inserts a second session in a project seedSession already
// made; the seeded project's slug is unique, so a second project is not the
// way to get a second session.
func seedSessionIn(t *testing.T, queries *store.Queries, projectID string) store.Session {
	t.Helper()
	return testutil.SeedSession(t, queries, projectID, "idle")
}

// fakeDirectory answers the reads without a server behind it.
type fakeDirectory struct {
	orientation string
	sessions    []SessionRow
	projects    []ProjectRow
	summary     string
	created     SessionRow
	createErr   error

	mu          sync.Mutex
	lastFilter  string
	lastProject string
}

func (d *fakeDirectory) Orientation(context.Context) string { return d.orientation }

func (d *fakeDirectory) ListSessions(_ context.Context, filter string) []SessionRow {
	d.mu.Lock()
	d.lastFilter = filter
	d.mu.Unlock()
	return d.sessions
}

func (d *fakeDirectory) SessionBrief(_ context.Context, id string) (SessionRow, bool) {
	for _, row := range d.sessions {
		if row.ID == id {
			return row, row.MachineID == "" || row.MachineID == "local"
		}
	}
	return SessionRow{ID: id}, false
}

func (d *fakeDirectory) Summarize(_ context.Context, _ string, deliver func(string)) {
	deliver(d.summary)
}

func (d *fakeDirectory) ListProjects(context.Context) []ProjectRow { return d.projects }

func (d *fakeDirectory) CreateSession(_ context.Context, projectID, _ string) (SessionRow, error) {
	d.mu.Lock()
	d.lastProject = projectID
	d.mu.Unlock()
	if d.createErr != nil {
		return SessionRow{}, d.createErr
	}
	return d.created, nil
}

// fakeDispatcher records what was sent.
type fakeDispatcher struct {
	delivery Delivery
	err      error

	mu        sync.Mutex
	prompts   []string
	reporting []bool
}

func (d *fakeDispatcher) Dispatch(_ context.Context, _, prompt string, withReporting bool) (Delivery, error) {
	d.mu.Lock()
	d.prompts = append(d.prompts, prompt)
	d.reporting = append(d.reporting, withReporting)
	d.mu.Unlock()
	if d.err != nil {
		return "", d.err
	}
	if d.delivery == "" {
		return DeliveryTurn, nil
	}
	return d.delivery, nil
}

func (d *fakeDispatcher) ProjectContext(context.Context, string) string { return "" }

func (d *fakeDispatcher) AutoRunnable(context.Context, string) (bool, string, error) {
	return true, "", nil
}

func (d *fakeDispatcher) sent() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.prompts...)
}

// fakeHead is a head that answers with a canned reply and counts its starts.
type fakeHead struct {
	reply string
	err   error

	mu        sync.Mutex
	startErr  error
	starts    int
	closes    int
	prompts   []string
	preambles []string
	onText    func(string)
}

// startFails makes the next start fail, the way a missing provider CLI or a
// connect timeout does.
func (h *fakeHead) startFails(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.startErr = err
}

func (h *fakeHead) StartHead(_ context.Context, p HeadParams) (HeadRuntime, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.startErr != nil {
		return nil, h.startErr
	}
	h.starts++
	h.preambles = append(h.preambles, p.Preamble)
	h.onText = p.OnText
	return h, nil
}

func (h *fakeHead) Query(_ context.Context, prompt string) (string, error) {
	h.mu.Lock()
	h.prompts = append(h.prompts, prompt)
	onText := h.onText
	h.mu.Unlock()

	if h.err != nil {
		return "", h.err
	}
	if onText != nil {
		onText(h.reply)
	}
	return h.reply, nil
}

func (h *fakeHead) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closes++
	return nil
}

func (h *fakeHead) startCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.starts
}

func (h *fakeHead) lastPrompt() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.prompts) == 0 {
		return ""
	}
	return h.prompts[len(h.prompts)-1]
}

func (h *fakeHead) lastPreamble() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.preambles) == 0 {
		return ""
	}
	return h.preambles[len(h.preambles)-1]
}

// fakeFacts is the runtime's side of a turn that has just ended.
type fakeFacts struct {
	pending string
	outcome TurnOutcome
	err     error
}

func (f *fakeFacts) PendingHumanInput(string) string { return f.pending }

func (f *fakeFacts) TurnOutcome(context.Context, string) (TurnOutcome, error) {
	return f.outcome, f.err
}

// fakeSurface records what it was handed.
type fakeSurface struct {
	name  string
	cards bool
	fail  bool

	mu    sync.Mutex
	items []Item
}

func (s *fakeSurface) Name() string { return s.name }

func (s *fakeSurface) CanShowCards() bool { return s.cards }

func (s *fakeSurface) Deliver(_ context.Context, item Item) error {
	if s.fail {
		return errors.New("closed")
	}
	s.mu.Lock()
	s.items = append(s.items, item)
	s.mu.Unlock()
	return nil
}

func (s *fakeSurface) got() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Item(nil), s.items...)
}

func (s *fakeSurface) countOf(kind ItemKind) int {
	var n int
	for _, item := range s.got() {
		if item.Kind == kind {
			n++
		}
	}
	return n
}

// fakeAllowances answers the allowances verb.
type fakeAllowances struct{ doc usage.Document }

func (a fakeAllowances) Document(context.Context) usage.Document { return a.doc }

// rememberCall is one Remember, kept whole so a test can assert the provenance
// mapping rather than only that something was written.
type rememberCall struct {
	Text       string
	Category   memory.Category
	Provenance Provenance
	ProjectID  string
}

// captureCall is one staged capture.
type captureCall struct {
	ProjectID string
	Text      string
	Source    memory.Source
}

// fakeMemory is the brain without the brain: canned reads, recorded writes.
type fakeMemory struct {
	pinned []Fact
	index  []IndexLine
	// found is what Search answers with. It is deliberately NOT what Pinned
	// answers with, so a test can assert that a body reachable only through
	// recall never reaches the preamble.
	found     []Fact
	searchErr error
	// pinnedErr and indexErr make the store unreadable, which is a third state
	// beside "empty": the preamble must not print one for the other.
	pinnedErr error
	indexErr  error
	// blockReads holds both preamble reads until the context is done, which is
	// what a wedged embedder or a clustering pass over a large corpus looks like
	// from here.
	blockReads bool

	mu         sync.Mutex
	queries    []string
	remembered []rememberCall
	confirmed  []string
	flagged    [][2]string
	captured   []captureCall
}

func (m *fakeMemory) Index(ctx context.Context) ([]IndexLine, error) {
	if err := m.stall(ctx); err != nil {
		return nil, err
	}
	return m.index, m.indexErr
}

func (m *fakeMemory) Pinned(ctx context.Context) ([]Fact, error) {
	if err := m.stall(ctx); err != nil {
		return nil, err
	}
	return m.pinned, m.pinnedErr
}

// stall is the slow read: it answers only when the caller's deadline does.
func (m *fakeMemory) stall(ctx context.Context) error {
	if !m.blockReads {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func (m *fakeMemory) Search(_ context.Context, query string, _ int) ([]Fact, error) {
	m.mu.Lock()
	m.queries = append(m.queries, query)
	m.mu.Unlock()
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.found, nil
}

func (m *fakeMemory) Remember(_ context.Context, text string, category memory.Category,
	provenance Provenance, projectID string,
) (Fact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.remembered = append(m.remembered, rememberCall{
		Text: text, Category: category, Provenance: provenance, ProjectID: projectID,
	})
	return Fact{ID: "fact-1", Text: text, Category: category, Source: provenance.Source()}, nil
}

func (m *fakeMemory) Confirm(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confirmed = append(m.confirmed, id)
	return nil
}

func (m *fakeMemory) Flag(_ context.Context, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flagged = append(m.flagged, [2]string{id, reason})
	return nil
}

func (m *fakeMemory) Capture(_ context.Context, projectID, text string, source memory.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.captured = append(m.captured, captureCall{ProjectID: projectID, Text: text, Source: source})
	return nil
}

func (m *fakeMemory) writes() []rememberCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]rememberCall(nil), m.remembered...)
}

func (m *fakeMemory) captures() []captureCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]captureCall(nil), m.captured...)
}
