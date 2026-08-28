package voice

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// speakingEngine is an echo engine that can also be handed text, so a test can
// see what the call would have said out loud.
type speakingEngine struct {
	*EchoEngine

	mu   sync.Mutex
	said []string
}

func newSpeakingEngine() *speakingEngine {
	return &speakingEngine{EchoEngine: NewEchoEngine()}
}

func (e *speakingEngine) SendText(text string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.said = append(e.said, text)
	return nil
}

func (e *speakingEngine) spoken() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.said...)
}

// waitForSpeech gives the injection goroutine a moment to land.
func (e *speakingEngine) waitForSpeech(t *testing.T, want int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := e.spoken()
		if len(got) >= want {
			return got
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newWorldCall(engine Engine) *call {
	return &call{
		engine:    engine,
		registry:  NewRegistry(),
		follows:   make(map[string]*followState),
		offered:   make(map[string]SessionRow),
		summaries: make(map[string]string),
		log:       testLogger(),
		runCtx:    context.Background(),
	}
}

// The snapshot is a client's picture of the world. It is bounded on arrival,
// because everything in it can end up in the speech model's context.
func TestWorldSnapshotIsBounded(t *testing.T) {
	c := newWorldCall(NewEchoEngine())

	rows := make([]wireSessionRow, 0, maxWorldRows+50)
	for i := range maxWorldRows + 50 {
		rows = append(rows, wireSessionRow{SessionID: "sess-" + string(rune('a'+i%26)) + string(rune('0'+i%10))})
	}
	c.setWorld(rows)
	if got := len(c.worldRows()); got != maxWorldRows {
		t.Errorf("kept %d rows, want the %d cap", got, maxWorldRows)
	}

	long := strings.Repeat("é", maxWorldField*3)
	c.setWorld([]wireSessionRow{{SessionID: "sess-1", Name: long, ProjectName: "a\nb\tc"}})
	row := c.worldRows()[0]
	if n := len([]rune(row.Name)); n != maxWorldField {
		t.Errorf("name kept %d runes, want the %d clamp", n, maxWorldField)
	}
	if !strings.Contains(row.Name, "é") {
		t.Error("clamping mangled the text; it must cut on a rune boundary")
	}
	if row.ProjectName != "a b c" {
		t.Errorf("projectName = %q, want newlines flattened — a snapshot field must not "+
			"look like a section heading in the prompt it lands in", row.ProjectName)
	}
}

// A viewing frame is context, never a command. The call says what is on screen
// and stops there; re-aiming on a click would send the next prompt somewhere
// nobody asked for.
func TestViewingNeverMovesTheFocus(t *testing.T) {
	engine := newSpeakingEngine()
	c := newWorldCall(engine)
	c.setFocus("sess-focused")
	c.setWorld([]wireSessionRow{{
		SessionID: "sess-other", Name: "Live Voice Dialog",
		ProjectName: "agentique", MachineName: "workstation",
	}})

	c.noteViewing("sess-other")
	said := engine.waitForSpeech(t, 1)

	if c.currentFocus() != "sess-focused" {
		t.Fatal("a viewing frame moved the call's focus")
	}
	if len(said) != 1 {
		t.Fatalf("said %v, want exactly one note", said)
	}
	note := said[0]
	for _, want := range []string{"Live Voice Dialog", "sess-other", "not an instruction", ToolFocusSession} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q is missing %q", note, want)
		}
	}
	// The id was named by the server, so focusing it is now allowed — the
	// operator only has to ask for it.
	if _, ok := c.offeredRow("sess-other"); !ok {
		t.Error("a session the note named must be focusable by name afterwards")
	}
}

// Clicking around a sidebar is not conversation. Without a floor, a minute of
// browsing is a minute of injected notes competing with the person talking.
func TestViewingNotesAreRateLimited(t *testing.T) {
	engine := newSpeakingEngine()
	c := newWorldCall(engine)

	c.noteViewing("sess-a")
	engine.waitForSpeech(t, 1)
	c.noteViewing("sess-b")
	c.noteViewing("sess-c")
	// Give any wrongly-allowed note time to arrive.
	time.Sleep(50 * time.Millisecond)

	if got := engine.spoken(); len(got) != 1 {
		t.Errorf("said %d notes in a burst, want 1: %v", len(got), got)
	}
}

// Two things are not worth saying: that they are looking at the session you are
// already talking about, and that they left the session view.
func TestViewingSaysNothingWhenThereIsNothingToSay(t *testing.T) {
	engine := newSpeakingEngine()
	c := newWorldCall(engine)
	c.setFocus("sess-focused")

	c.noteViewing("sess-focused")
	c.noteViewing("")
	time.Sleep(50 * time.Millisecond)

	if got := engine.spoken(); len(got) != 0 {
		t.Errorf("said %v, want nothing", got)
	}
}

// The snapshot supplies the sessions this server cannot see; the directory
// supplies the truth about its own. Where both have a row, the local one wins.
func TestMergedRowsPreferTheLocalTruth(t *testing.T) {
	c := newWorldCall(NewEchoEngine())
	c.directory = &fakeDirectory{rows: []SessionRow{
		{ID: "sess-local", Name: "Local Truth", MachineName: "here", LastActivity: "2026-08-26T10:00:00Z"},
	}}
	c.setWorld([]wireSessionRow{
		{SessionID: "sess-local", Name: "Stale Copy", MachineName: "here"},
		{SessionID: "sess-remote", Name: "Remote Work", MachineName: "laptop", LastActivityAt: "2026-08-26T09:00:00Z"},
	})

	rows := c.mergedRows(context.Background(), FilterAll)
	if len(rows) != 2 {
		t.Fatalf("merged %d rows, want 2: %v", len(rows), rows)
	}
	byID := map[string]SessionRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	if byID["sess-local"].Name != "Local Truth" {
		t.Errorf("local row = %q, want the database's copy to win", byID["sess-local"].Name)
	}
	if byID["sess-remote"].Name != "Remote Work" {
		t.Error("the snapshot must supply the sessions this server cannot see")
	}
}

// fakeDirectory answers from a fixed set of rows.
type fakeDirectory struct {
	rows      []SessionRow
	projects  []ProjectRow
	summaries map[string]string
	// families is what a spoken model name may be, standing in for the model
	// catalog. Anything else comes back as an UnknownModelError.
	families []string
	// createErr, when set, is what CreateSession fails with.
	createErr error

	// summarizeCalls counts how many times a summary was asked for, so a test
	// can see that focusing warmed one.
	mu             sync.Mutex
	summarizeCalls int
	created        []createdSession
}

// createdSession is one call to fakeDirectory.CreateSession.
type createdSession struct {
	projectID string
	model     string
}

func (f *fakeDirectory) Orientation(context.Context) string { return "Two sessions, none waiting." }

func (f *fakeDirectory) ListSessions(_ context.Context, filter string) []SessionRow {
	var out []SessionRow
	for _, row := range f.sessions() {
		if matchesFilter(row, filter) {
			out = append(out, row)
		}
	}
	return out
}

func (f *fakeDirectory) SessionBrief(_ context.Context, id string) (SessionRow, bool) {
	for _, row := range f.sessions() {
		if row.ID == id {
			return row, true
		}
	}
	return SessionRow{}, false
}

// sessions is every row the directory knows, including the ones it created —
// the real one reads a database, where a session exists the moment it is made.
func (f *fakeDirectory) sessions() []SessionRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SessionRow(nil), f.rows...)
}

func (f *fakeDirectory) Summarize(_ context.Context, id string, deliver func(string)) {
	f.mu.Lock()
	f.summarizeCalls++
	f.mu.Unlock()
	go deliver(f.summaries[id])
}

func (f *fakeDirectory) ListProjects(context.Context) []ProjectRow { return f.projects }

func (f *fakeDirectory) CreateSession(_ context.Context, projectID, model string) (SessionRow, error) {
	if f.createErr != nil {
		return SessionRow{}, f.createErr
	}

	family := "Opus"
	if model != "" {
		matched := ""
		for _, candidate := range f.families {
			if strings.EqualFold(candidate, model) {
				matched = candidate
				break
			}
		}
		if matched == "" {
			return SessionRow{}, &UnknownModelError{Spoken: model, Families: f.families}
		}
		family = matched
	}

	var project ProjectRow
	for _, row := range f.projects {
		if row.ID == projectID {
			project = row
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, createdSession{projectID: projectID, model: model})
	row := SessionRow{
		ID:          fmt.Sprintf("new-%d", len(f.created)),
		ProjectName: project.Name,
		ProjectSlug: project.Slug,
		MachineName: "workstation",
		State:       "idle",
		Model:       family,
	}
	// A created session is a session: everything that looks one up has to find
	// it, or the dispatch that follows reports it as somebody else's machine.
	f.rows = append(f.rows, row)
	return row, nil
}

func (f *fakeDirectory) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.summarizeCalls
}

// creations is what CreateSession was asked for, in order.
func (f *fakeDirectory) creations() []createdSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]createdSession(nil), f.created...)
}
