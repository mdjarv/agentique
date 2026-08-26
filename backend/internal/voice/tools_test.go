package voice

import (
	"context"
	"strings"
	"testing"
	"time"
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

func directoryWithTwo() *fakeDirectory {
	return &fakeDirectory{
		rows: []SessionRow{
			{ID: "s1", Name: "Live Voice Dialog", ProjectName: "agentique", MachineName: "workstation",
				State: "running", LastActivity: "2026-08-26T12:00:00Z"},
			{ID: "s2", Name: "Reconnect Drops", ProjectName: "agentique", MachineName: "workstation",
				State: "idle", Attention: AttentionApproval, LastActivity: "2026-08-26T11:00:00Z"},
		},
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

	c.toolSummarizeSession(context.Background(), map[string]any{"session_id": "s2"})
	said := engine.waitForSpeech(t, 1)
	if len(said) == 0 {
		t.Fatal("an empty summary must still be answered out loud")
	}
	if !strings.Contains(strings.ToLower(said[0]), "no summary available") {
		t.Errorf("spoken = %q, want an honest no-summary answer", said[0])
	}
}
