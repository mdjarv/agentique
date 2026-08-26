package voice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The tools that only look. Their whole job is to let the assistant talk about
// sessions accurately — and to refuse, in words, whenever it cannot.

func newToolCall(dir Directory, d Dispatcher, focus string) *call {
	c := newTestCall(d, NewRegistry(), focus)
	c.directory = dir
	if focus != "" {
		c.offer(SessionRow{ID: focus})
	}
	return c
}

// giveSocket hands a call a real websocket and returns the browser's end, so a
// test can read the control frames it sends in the order it sent them.
func giveSocket(t *testing.T, c *call) *websocket.Conn {
	t.Helper()

	var upgrader websocket.Upgrader
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		c.ws = conn
		close(ready)
	}))

	browser, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	// The assignment happens on the server's goroutine; waiting here is what
	// makes reading c.ws from the test goroutine safe.
	<-ready

	t.Cleanup(func() {
		_ = browser.Close()
		_ = c.ws.Close()
		srv.Close()
	})
	return browser
}

// expectNoControl asserts the call said nothing more.
func expectNoControl(t *testing.T, ws *websocket.Conn, within time.Duration) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(within))
	if _, payload, err := ws.ReadMessage(); err == nil {
		t.Errorf("an extra control frame arrived: %s", payload)
	}
}

// waitPending waits for the call's promised answers to settle.
func waitPending(t *testing.T, c *call, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := c.pendingAsync.Load()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pendingAsync = %d, want %d", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func directoryWithTwo() *fakeDirectory {
	return &fakeDirectory{
		rows: []SessionRow{
			{ID: "s1", Name: "Live Voice Dialog", ProjectName: "agentique", MachineName: "workstation",
				State: "running", LastActivity: "2026-08-26T12:00:00Z"},
			{ID: "s2", Name: "Reconnect Drops", ProjectName: "agentique", MachineName: "workstation",
				State: "idle", Attention: AttentionApproval, LastActivity: "2026-08-26T11:00:00Z"},
		},
		projects: []ProjectRow{
			{ID: "p1", Name: "agentique", Slug: "agentique", LastActivity: "2026-08-26T12:00:00Z"},
			{ID: "p2", Name: "webtickets", Slug: "webtickets", LastActivity: "2026-08-25T09:00:00Z"},
		},
		families:  []string{"Haiku", "Sonnet", "Opus", "Fable"},
		summaries: map[string]string{"s1": "It has been wiring the voice socket."},
	}
}

// Every tool answers. The model is paused until it does, and an unanswered call
// is indistinguishable from the call having died.
func TestEveryDirectoryToolAnswers(t *testing.T) {
	tests := []struct {
		name string
		dir  Directory
		ev   ToolCallEvent
	}{
		{"list with no directory", nil, ToolCallEvent{Name: ToolListSessions, Args: map[string]any{"filter": "all"}}},
		{"find with no directory", nil, ToolCallEvent{Name: ToolFindSession, Args: map[string]any{"query": "voice"}}},
		{"focus with no directory", nil, ToolCallEvent{Name: ToolFocusSession, Args: map[string]any{"session_id": "s1"}}},
		{"summarize with no directory", nil, ToolCallEvent{Name: ToolSummarizeSession, Args: nil}},
		{"list with a nonsense filter", directoryWithTwo(), ToolCallEvent{Name: ToolListSessions, Args: map[string]any{"filter": 42}}},
		{"find with nothing to find", directoryWithTwo(), ToolCallEvent{Name: ToolFindSession, Args: map[string]any{"query": "  "}}},
		{"focus on an id nobody offered", directoryWithTwo(), ToolCallEvent{Name: ToolFocusSession, Args: map[string]any{"session_id": "made-up"}}},
		{"summarize with no arguments at all", directoryWithTwo(), ToolCallEvent{Name: ToolSummarizeSession}},
		{"projects with no directory", nil, ToolCallEvent{Name: ToolListProjects}},
		{"create with no directory", nil, ToolCallEvent{Name: ToolCreateSession, Args: map[string]any{"project_id": "p1"}}},
		{"projects nothing matches", directoryWithTwo(), ToolCallEvent{Name: ToolListProjects, Args: map[string]any{"query": "kubernetes"}}},
		{"create with no project at all", directoryWithTwo(), ToolCallEvent{Name: ToolCreateSession}},
		{"create in a project nobody offered", directoryWithTwo(), ToolCallEvent{Name: ToolCreateSession, Args: map[string]any{"project_id": "p9"}}},
		{"a tool this build does not have", directoryWithTwo(), ToolCallEvent{Name: "teleport"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newToolCall(tt.dir, &recordingDispatcher{}, "")
			if got := c.runTool(tt.ev); len(got) == 0 {
				t.Fatal("no response payload — the model would stay paused forever")
			}
		})
	}
}

// A list is for the assistant to summarise, and the rows it names become
// focusable. Nothing else is.
func TestListSessionsOffersWhatItNames(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")

	got := c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})
	rows, _ := got["sessions"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("listed %v, want both sessions", got)
	}
	for _, id := range []string{"s1", "s2"} {
		if _, ok := c.offeredRow(id); !ok {
			t.Errorf("%q was listed but is not focusable", id)
		}
	}

	// needs_attention is a real filter, not a relabelling of "everything".
	only := c.toolListSessions(context.Background(), map[string]any{"filter": FilterNeedsAttention})
	rows, _ = only["sessions"].([]map[string]any)
	if len(rows) != 1 || rows[0]["session_id"] != "s2" {
		t.Errorf("needs_attention gave %v, want only the session waiting on approval", only)
	}
}

// The snapshot is what lets a call talk about a session this server cannot see
// at all.
func TestListSessionsIncludesRemoteRowsFromTheSnapshot(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")
	c.setWorld([]wireSessionRow{{
		SessionID: "s9", Name: "Remote Work", MachineName: "laptop", State: "running",
	}})

	got := c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})
	rows, _ := got["sessions"].([]map[string]any)
	var found bool
	for _, row := range rows {
		if row["session_id"] == "s9" && row["machine"] == "laptop" {
			found = true
		}
	}
	if !found {
		t.Errorf("listed %v, want the remote session the snapshot supplied", got)
	}
}

// Finding is not choosing. The result says whether the top one is obvious; the
// assistant still says the name out loud before acting on it.
func TestFindSessionNeverPicks(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")

	got := c.toolFindSession(context.Background(), map[string]any{"query": "live voice dialogue"})
	candidates, _ := got["candidates"].([]map[string]any)
	if len(candidates) == 0 || candidates[0]["session_id"] != "s1" {
		t.Fatalf("find gave %v, want the mangled name to reach Live Voice Dialog", got)
	}
	if clear, _ := got["top_is_clear"].(bool); !clear {
		t.Error("one strong match should be reported as clear")
	}
	if c.currentFocus() != "" {
		t.Error("find_session moved the focus — finding is not choosing")
	}

	miss := c.toolFindSession(context.Background(), map[string]any{"query": "kubernetes upgrade"})
	if clear, _ := miss["top_is_clear"].(bool); clear {
		t.Error("nothing matched, so nothing can be clear")
	}
	if note, _ := miss["note"].(string); !strings.Contains(note, "another way") {
		t.Errorf("a miss should ask them to describe it differently, got %q", note)
	}
}

// Focus is the one gesture that re-aims the call, and it only accepts a session
// the server itself named — not an id assembled out of a transcript.
func TestFocusSessionRequiresAnOfferedID(t *testing.T) {
	dir := directoryWithTwo()
	c := newToolCall(dir, &recordingDispatcher{}, "")

	refused := c.toolFocusSession(context.Background(), map[string]any{"session_id": "8f1c-invented"})
	if _, bad := refused["error"]; !bad {
		t.Fatalf("focus accepted an id nobody offered: %v", refused)
	}
	if c.currentFocus() != "" {
		t.Error("a refused focus still moved the call")
	}

	c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})
	got := c.toolFocusSession(context.Background(), map[string]any{"session_id": "s1"})
	if _, bad := got["error"]; bad {
		t.Fatalf("focus refused a listed session: %v", got)
	}
	if c.currentFocus() != "s1" {
		t.Errorf("focus = %q, want s1", c.currentFocus())
	}
	if got["name"] != "Live Voice Dialog" {
		t.Errorf("focus returned %v, want the brief to name the session", got)
	}
	if can, _ := got["can_start_work"].(bool); !can {
		t.Error("a local session in full auto can be worked in")
	}

	// The next question is usually "what has it been doing?", so the answer is
	// warmed here rather than waited for there.
	deadline := time.Now().Add(2 * time.Second)
	for dir.calls() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if dir.calls() == 0 {
		t.Error("focusing a local session did not warm its summary")
	}
}

// A session on another machine can be looked at and talked about. Work cannot
// be started there, and the refusal has to say which machine it is on.
func TestRemoteSessionsCanBeSeenButNotWorkedIn(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")
	c.setWorld([]wireSessionRow{{SessionID: "s9", Name: "Remote Work", MachineName: "laptop"}})
	c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})

	focused := c.toolFocusSession(context.Background(), map[string]any{"session_id": "s9"})
	if _, bad := focused["error"]; bad {
		t.Fatalf("focusing a remote session should work: %v", focused)
	}
	if can, _ := focused["can_start_work"].(bool); can {
		t.Error("a remote session must not claim work can start there")
	}
	if note, _ := focused["note"].(string); !strings.Contains(note, "laptop") {
		t.Errorf("note %q does not say which machine it runs on", note)
	}

	run := c.runTool(ToolCallEvent{Name: ToolRunPrompt, Args: map[string]any{
		"prompt": "fix the tests", "stay_on_line": true,
	}})
	msg, _ := run["error"].(string)
	if msg == "" {
		t.Fatalf("dispatch to a remote session was allowed: %v", run)
	}
	if !strings.Contains(msg, "laptop") {
		t.Errorf("refusal = %q, want it to name the machine", msg)
	}

	summary := c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s9"})
	msg, _ = summary["error"].(string)
	if !strings.Contains(msg, "laptop") {
		t.Errorf("summary refusal = %q, want it to say the transcript is on another machine", msg)
	}
}

// A project list is a menu to choose from, and only what the server named is
// somewhere a session may be created.
func TestListProjectsOffersWhatItNames(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")

	got := c.toolListProjects(context.Background(), nil)
	rows, _ := got["projects"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("listed %v, want both projects", got)
	}
	for _, id := range []string{"p1", "p2"} {
		if _, ok := c.offeredProject(id); !ok {
			t.Errorf("%q was listed but cannot be created in", id)
		}
	}

	// A spoken name arrives mangled, and the same matcher find_session uses is
	// what has to reach it.
	narrowed := c.toolListProjects(context.Background(), map[string]any{"query": "web tickets"})
	rows, _ = narrowed["projects"].([]map[string]any)
	if len(rows) != 1 || rows[0]["project_id"] != "p2" {
		t.Errorf("narrowed to %v, want only webtickets", narrowed)
	}

	// A miss is not an empty machine, and must not be answered as one.
	miss := c.toolListProjects(context.Background(), map[string]any{"query": "kubernetes"})
	if note, _ := miss["note"].(string); !strings.Contains(note, "another way") {
		t.Errorf("a miss said %q, want it to ask them to say it differently", note)
	}
}

// Creation aims at a project the server listed, never an id assembled out of a
// transcript.
func TestCreateSessionRequiresAnOfferedProject(t *testing.T) {
	dir := directoryWithTwo()
	c := newToolCall(dir, &recordingDispatcher{}, "")

	refused := c.toolCreateSession(context.Background(), map[string]any{"project_id": "8f1c-invented"})
	msg, _ := refused["error"].(string)
	if msg == "" {
		t.Fatalf("created in a project nobody offered: %v", refused)
	}
	if !strings.Contains(msg, ToolListProjects) {
		t.Errorf("refusal = %q, want it to say how to get a real id", msg)
	}
	if len(dir.creations()) != 0 {
		t.Error("a refused create still reached the directory")
	}
	if c.currentFocus() != "" {
		t.Error("a refused create still moved the call")
	}
}

// Creating focuses: the screen lands on the new session, and everything that
// acts on "the session" now acts on it.
func TestCreateSessionFocusesTheNewSession(t *testing.T) {
	dir := directoryWithTwo()
	c := newToolCall(dir, &recordingDispatcher{}, "")
	ws := giveSocket(t, c)
	c.toolListProjects(context.Background(), nil)

	got := c.toolCreateSession(context.Background(), map[string]any{"project_id": "p2", "model": "fable"})
	if _, bad := got["error"]; bad {
		t.Fatalf("create refused an offered project: %v", got)
	}

	created := dir.creations()
	if len(created) != 1 || created[0].projectID != "p2" || created[0].model != "fable" {
		t.Fatalf("directory saw %v, want one create in p2 on fable", created)
	}

	sessionID, _ := got["session_id"].(string)
	if sessionID == "" {
		t.Fatal("the brief does not name the session it made")
	}
	if c.currentFocus() != sessionID {
		t.Errorf("focus = %q, want the session just created", c.currentFocus())
	}
	if _, offered := c.offeredRow(sessionID); !offered {
		t.Error("the new session is not focusable")
	}
	if frame := readControl(t, ws); frame.Type != msgFocus || frame.SessionID != sessionID {
		t.Fatalf("frame = %+v, want the screen to follow onto the new session", frame)
	}

	// The brief has to carry what was said out loud: where it is, and what it
	// runs — the read-back named both.
	if got["project"] != "webtickets" {
		t.Errorf("brief said project %v, want webtickets", got["project"])
	}
	if got["model"] != "Fable" {
		t.Errorf("brief said model %v, want the resolved family", got["model"])
	}
	// Create and send are one gesture, so the result has to say so.
	note, _ := got["note"].(string)
	if !strings.Contains(note, ToolRunPrompt) {
		t.Errorf("note = %q, want it to send the agreed prompt straight away", note)
	}
}

// An empty model is the default, and it is the composer's default rather than
// one this package invents.
func TestCreateSessionWithNoModelTakesTheDefault(t *testing.T) {
	dir := directoryWithTwo()
	c := newToolCall(dir, &recordingDispatcher{}, "")
	c.toolListProjects(context.Background(), nil)

	got := c.toolCreateSession(context.Background(), map[string]any{"project_id": "p1"})
	if _, bad := got["error"]; bad {
		t.Fatalf("create refused the default model: %v", got)
	}
	if created := dir.creations(); len(created) != 1 || created[0].model != "" {
		t.Errorf("directory saw %v, want the model left to the service", created)
	}
}

// A model nobody has is a spoken question, not a substitution — and nothing is
// created while it is asked.
func TestCreateSessionRefusesAnUnknownModelByNamingTheRealOnes(t *testing.T) {
	dir := directoryWithTwo()
	c := newToolCall(dir, &recordingDispatcher{}, "")
	c.toolListProjects(context.Background(), nil)

	got := c.toolCreateSession(context.Background(), map[string]any{"project_id": "p1", "model": "grok"})
	msg, _ := got["error"].(string)
	if msg == "" {
		t.Fatalf("an unknown model was accepted: %v", got)
	}
	if !strings.Contains(msg, "grok") {
		t.Errorf("refusal = %q, want it to repeat what was asked for", msg)
	}
	for _, family := range []string{"Haiku", "Sonnet", "Opus", "Fable"} {
		if !strings.Contains(msg, family) {
			t.Errorf("refusal = %q, want it to name %q as an option", msg, family)
		}
	}
	if c.currentFocus() != "" {
		t.Error("a refused model still moved the call")
	}
}

// A creation that fails is said plainly. It must not leave the call aimed at a
// session that does not exist.
func TestCreateSessionThatFailsSaysSoAndLeavesTheCallAlone(t *testing.T) {
	dir := directoryWithTwo()
	dir.createErr = errors.New("project quota exceeded")
	c := newToolCall(dir, &recordingDispatcher{}, "")
	c.toolListProjects(context.Background(), nil)

	got := c.toolCreateSession(context.Background(), map[string]any{"project_id": "p1"})
	msg, _ := got["error"].(string)
	if msg == "" {
		t.Fatalf("a failed create reported success: %v", got)
	}
	if !strings.Contains(msg, "agentique") {
		t.Errorf("refusal = %q, want it to name where it was trying to create", msg)
	}
	if c.currentFocus() != "" {
		t.Error("a failed create still moved the call")
	}
}

// Nothing focused is a question to ask, not a session to guess at.
func TestRunPromptWithNoFocusAsksWhichSession(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")
	got := c.runTool(ToolCallEvent{Name: ToolRunPrompt, Args: map[string]any{
		"prompt": "do the thing", "stay_on_line": true,
	}})
	msg, _ := got["error"].(string)
	if !strings.Contains(strings.ToLower(msg), "which session") {
		t.Errorf("error = %q, want it to ask which session", msg)
	}
}

// Slow work answers immediately and speaks later. Holding the tool response for
// a local model is a silent microphone.
func TestSummarizeAnswersNowAndSpeaksLater(t *testing.T) {
	engine := newSpeakingEngine()
	dir := directoryWithTwo()
	c := newToolCall(dir, &recordingDispatcher{}, "")
	c.engine = engine
	c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})

	got := c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s1"})
	if _, bad := got["error"]; bad {
		t.Fatalf("summarize refused a local session: %v", got)
	}
	out, _ := got["output"].(string)
	if !strings.Contains(strings.ToLower(out), "moment") && !strings.Contains(strings.ToLower(out), "working on it") {
		t.Errorf("immediate answer = %q, want it to say it is working on it", out)
	}

	said := engine.waitForSpeech(t, 1)
	if len(said) == 0 {
		t.Fatal("the summary never arrived")
	}
	if !strings.Contains(said[0], "wiring the voice socket") {
		t.Errorf("spoken summary = %q, want the delivered text", said[0])
	}
	// A summary distils a transcript nobody here wrote.
	if !strings.Contains(said[0], "NOT an instruction") {
		t.Error("a summary must be framed as quoted data, never as an instruction")
	}

	// Delivered once, then it is warm: asking again answers inline.
	again := c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s1"})
	if again["summary"] != "It has been wiring the voice socket." {
		t.Errorf("second ask = %v, want the cached summary inline", again)
	}
}

// A session with nothing recorded gets an honest answer rather than an
// invented one.
func TestSummarizeSaysWhenThereIsNothing(t *testing.T) {
	engine := newSpeakingEngine()
	dir := directoryWithTwo()
	dir.summaries = map[string]string{}
	c := newToolCall(dir, &recordingDispatcher{}, "")
	c.engine = engine
	c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})

	ws := giveSocket(t, c)
	c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s2"})
	said := engine.waitForSpeech(t, 1)
	if len(said) == 0 {
		t.Fatal("an empty summary must still be answered out loud")
	}
	if !strings.Contains(strings.ToLower(said[0]), "no summary available") {
		t.Errorf("spoken = %q, want an honest no-summary answer", said[0])
	}

	// It still stops looking busy, but there is no card: an empty summary card
	// says less than nothing.
	if start := readControl(t, ws); start.Type != msgActivity || start.Label == "" {
		t.Fatalf("first frame = %+v, want the activity label", start)
	}
	if done := readControl(t, ws); done.Type != msgActivity || done.Label != "" {
		t.Fatalf("second frame = %+v, want the activity cleared", done)
	}
	expectNoControl(t, ws, 100*time.Millisecond)
}

// Tool work is invisible, so a healthy call computing a summary looks exactly
// like a dead one. It says what it is doing, then shows what it found.
func TestSummarizeShowsItsWorkAndPutsTheAnswerOnScreen(t *testing.T) {
	engine := newSpeakingEngine()
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")
	c.engine = engine
	ws := giveSocket(t, c)
	c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})

	c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s1"})

	start := readControl(t, ws)
	if start.Type != msgActivity {
		t.Fatalf("first frame = %+v, want %q", start, msgActivity)
	}
	if !strings.Contains(start.Label, "Live Voice Dialog") || !strings.HasPrefix(start.Label, "Summarizing") {
		t.Errorf("activity label = %q, want it to name what is being summarised", start.Label)
	}

	done := readControl(t, ws)
	if done.Type != msgActivity || done.Label != "" {
		t.Fatalf("second frame = %+v, want an empty label to clear the activity", done)
	}

	summary := readControl(t, ws)
	if summary.Type != msgSummary {
		t.Fatalf("third frame = %+v, want %q", summary, msgSummary)
	}
	if summary.SessionID != "s1" {
		t.Errorf("summary sessionId = %q, want the session it describes", summary.SessionID)
	}
	if summary.Headline != "It has been wiring the voice socket." {
		t.Errorf("summary headline = %q, want the delivered text", summary.Headline)
	}

	// A warm answer is still an answer they asked for, so it lands in the log
	// the same way rather than depending on what warmed the cache.
	c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s1"})
	if again := readControl(t, ws); again.Type != msgSummary || again.Headline != summary.Headline {
		t.Errorf("cached ask sent %+v, want the same summary on screen", again)
	}
}

// The screen copy does not depend on the engine having a voice. A loopback call
// still shows what the summary said.
func TestSummaryReachesTheScreenWithoutAVoice(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")
	ws := giveSocket(t, c)
	c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})

	c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s1"})
	readControl(t, ws) // activity
	readControl(t, ws) // activity cleared
	if summary := readControl(t, ws); summary.Type != msgSummary || summary.Headline == "" {
		t.Errorf("frame = %+v, want the summary on screen even with nothing to speak it", summary)
	}
}

// Warming a summary is not something the operator asked for, and a progress
// line for work nobody requested is indistinguishable from a bug.
func TestWarmingASummaryIsInvisible(t *testing.T) {
	dir := directoryWithTwo()
	c := newToolCall(dir, &recordingDispatcher{}, "")
	ws := giveSocket(t, c)
	c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})

	c.toolFocusSession(context.Background(), map[string]any{"session_id": "s1"})
	if focus := readControl(t, ws); focus.Type != msgFocus {
		t.Fatalf("first frame = %+v, want the screen to follow the voice", focus)
	}
	// The warm-up still holds the line while it runs — it is the same promise.
	waitPending(t, c, 0)
	if dir.calls() == 0 {
		t.Fatal("focusing did not warm a summary")
	}
	expectNoControl(t, ws, 100*time.Millisecond)
}

// A summary that lands after the call is gone has nowhere to go, and must not
// take the process with it.
func TestDeliveryAfterTheCallClosesIsHarmless(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")
	c.engine = newSpeakingEngine()
	ws := giveSocket(t, c)
	c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})

	// Tear the call down the way run() does, then answer into the wreckage.
	c.unfollowAll()
	if err := c.engine.Close(); err != nil {
		t.Fatalf("engine close: %v", err)
	}
	_ = c.ws.Close()
	_ = ws.Close()

	c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s1"})
	waitPending(t, c, 0)
}
