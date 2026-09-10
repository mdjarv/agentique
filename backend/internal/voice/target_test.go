package voice

import (
	"context"
	"strings"
	"testing"
)

// Where a prompt lands. Everything here exists because it went wrong once: a
// call opened on a session in one project, the operator said a prompt naming a
// different project out loud, and the work went to the session the call
// happened to be pointing at.

// theIncident is that call, with the names it actually had.
func theIncident() (focus SessionRow, known []SessionRow, projects func() []ProjectRow) {
	focus = SessionRow{
		ID: "riff-1", Name: "Live Melodikrysset Sessions",
		ProjectName: "riff", ProjectSlug: "riff", MachineName: "workstation",
	}
	known = []SessionRow{
		focus,
		{ID: "ag-1", Name: "Voice Reliability", ProjectName: "Agentique", ProjectSlug: "agentique"},
	}
	projects = func() []ProjectRow {
		return []ProjectRow{
			{ID: "p1", Name: "riff", Slug: "riff"},
			{ID: "p2", Name: "Agentique", Slug: "agentique"},
		}
	}
	return focus, known, projects
}

func TestJudgeTargetAcceptsWhatTheAssistantActuallyNamed(t *testing.T) {
	focus, known, projects := theIncident()

	// Accepting is generous on purpose: refusing a good send costs a sentence,
	// sending to the wrong session costs the work.
	accepted := []string{
		"Live Melodikrysset Sessions",
		"the Melodikrysset one",
		"live melodikrysset sessions in riff",
		"riff",
		"the riff session",
		"melodikrysset",
	}
	for _, spoken := range accepted {
		t.Run(spoken, func(t *testing.T) {
			if got := judgeTarget(spoken, focus, known, projects); !got.OK {
				t.Errorf("refused %q as %s: %s", spoken, got.Reason, got.Say)
			}
		})
	}
}

func TestJudgeTargetRefusesAProjectTheFocusIsNotIn(t *testing.T) {
	focus, known, projects := theIncident()

	// The exact shape of the incident: the words name a repository, and it is
	// not this one's.
	got := judgeTarget("the Agentique session", focus, nil, projects)
	if got.OK {
		t.Fatal("a prompt aimed at another project was accepted")
	}
	if got.Reason != targetOtherProject && got.Reason != targetElsewhere {
		t.Errorf("reason = %q, want it to say the target was somewhere else", got.Reason)
	}
	// The refusal has to be sayable, and has to name both ends: a listener who
	// cannot see the screen learns where it was pointed only from this.
	if !strings.Contains(got.Say, "riff") {
		t.Errorf("refusal %q does not say where the call is aimed", got.Say)
	}

	// With the other session known too, the refusal should name it rather than
	// the project — it is the more useful half.
	elsewhere := judgeTarget("the Agentique session", focus, known, projects)
	if elsewhere.OK {
		t.Fatal("a prompt aimed at another session was accepted")
	}
	if !strings.Contains(elsewhere.Say, "Agentique") {
		t.Errorf("refusal %q does not name what the assistant said", elsewhere.Say)
	}
}

func TestJudgeTargetRefusesANearMissInsideOneProject(t *testing.T) {
	focus, known, projects := theIncident()
	// Same project, different session: naming alone would let this through,
	// which is why a clear better match anywhere else is checked first.
	focus.ProjectName, focus.ProjectSlug = "Agentique", "agentique"
	focus.Name = "Storage Reclaim"
	known = append(known, focus)

	got := judgeTarget("Voice Reliability", focus, known, projects)
	if got.OK {
		t.Fatal("a prompt naming a sibling session was accepted")
	}
	if got.Reason != targetElsewhere {
		t.Errorf("reason = %q, want %q", got.Reason, targetElsewhere)
	}
	if !strings.Contains(got.Say, ToolFocusSession) {
		t.Errorf("refusal %q does not say how to put it right", got.Say)
	}
}

func TestJudgeTargetRefusesWhenNothingWasNamed(t *testing.T) {
	focus, known, projects := theIncident()

	got := judgeTarget("   ", focus, known, projects)
	if got.OK {
		t.Fatal("a send with no stated target was accepted")
	}
	if got.Reason != targetMissing {
		t.Errorf("reason = %q, want %q", got.Reason, targetMissing)
	}
	// Recoverable in one more call, and silently: the operator has already said
	// yes and is waiting, so this is not something to narrate at them.
	if !strings.Contains(got.Say, "target") {
		t.Errorf("refusal %q does not say what to pass", got.Say)
	}
}

func TestJudgeTargetRefusesSomethingNothingMatches(t *testing.T) {
	focus, known, projects := theIncident()

	got := judgeTarget("the deployment pipeline", focus, known, projects)
	if got.OK {
		t.Fatal("an unrecognised target was accepted")
	}
	if got.Reason != targetUnrecognised {
		t.Errorf("reason = %q, want %q", got.Reason, targetUnrecognised)
	}
	if !strings.Contains(got.Say, "Live Melodikrysset Sessions in riff") {
		t.Errorf("refusal %q does not name where the call is aimed", got.Say)
	}
}

// A check that cannot be performed must not refuse. A call wired to no
// directory knows one session and can reach no other, so demanding a target it
// has no way to verify would only break the single-session call.
func TestJudgeTargetAcceptsWhenTheFocusCannotBeDescribed(t *testing.T) {
	bare := SessionRow{ID: "sess-1"}
	for _, spoken := range []string{"", "anything at all"} {
		if got := judgeTarget(spoken, bare, nil, nil); !got.OK {
			t.Errorf("refused %q against an unnameable focus: %s", spoken, got.Say)
		}
	}
}

// The project branch needs a project list, and getting one is a database read.
// It is a thunk so only the refusing path pays for it, and a nil one has to
// degrade to a vaguer sentence rather than a panic.
func TestJudgeTargetSurvivesWithoutAProjectList(t *testing.T) {
	focus, _, _ := theIncident()
	got := judgeTarget("the Agentique session", focus, nil, nil)
	if got.OK {
		t.Fatal("a target naming another project was accepted with no project list")
	}
	if got.Reason != targetUnrecognised {
		t.Errorf("reason = %q, want it to fall back to unrecognised", got.Reason)
	}
}

// Naming is not describing. Two projects that could both be it get a question,
// never a pick — the rule the rest of this package already follows.
func TestJudgeTargetDoesNotPickBetweenProjects(t *testing.T) {
	focus, _, _ := theIncident()
	projects := func() []ProjectRow {
		return []ProjectRow{
			{ID: "p1", Name: "riff", Slug: "riff"},
			{ID: "p2", Name: "Agentique", Slug: "agentique"},
			{ID: "p3", Name: "Agentique UI", Slug: "agentique-ui"},
		}
	}
	got := judgeTarget("agentique", focus, nil, projects)
	if got.OK {
		t.Fatal("an ambiguous project target was accepted")
	}
	if got.Reason != targetUnrecognised {
		t.Errorf("reason = %q, want the ambiguous case to fall through rather than pick", got.Reason)
	}
}

// The whole thing, through the tool: the incident replayed, and nothing sent.
func TestRunPromptRefusesAPromptAimedAtAnotherProject(t *testing.T) {
	dir := &fakeDirectory{
		rows: []SessionRow{
			{ID: "riff-1", Name: "Live Melodikrysset Sessions", ProjectName: "riff",
				ProjectSlug: "riff", MachineName: "workstation", State: "idle"},
			{ID: "ag-1", Name: "Voice Reliability", ProjectName: "Agentique",
				ProjectSlug: "agentique", MachineName: "workstation", State: "idle"},
		},
		projects: []ProjectRow{
			{ID: "p1", Name: "riff", Slug: "riff"},
			{ID: "p2", Name: "Agentique", Slug: "agentique"},
		},
	}
	disp := &recordingDispatcher{}
	c := newToolCall(dir, disp, "riff-1")

	got := c.runTool(ToolCallEvent{ID: "1", Name: ToolRunPrompt, Args: map[string]any{
		"prompt": "Debug the live voice calls: the wrong codec is picked in the car.",
		"target": "the Agentique session",
	}})

	if _, refused := got["error"]; !refused {
		t.Fatalf("the prompt was sent to the wrong project: %v", got)
	}
	if sent := disp.dispatches(); len(sent) != 0 {
		t.Fatalf("dispatcher saw %v, want nothing to have been sent", sent)
	}
	// The refusal is what the listener hears, so it has to say where the call
	// is actually pointed.
	if msg, _ := got["error"].(string); !strings.Contains(msg, "riff") {
		t.Errorf("refusal %q does not name the session the call is on", msg)
	}
}

// Saying the right thing still sends. The check is a guard, not a gate.
func TestRunPromptSendsWhenTheTargetAgrees(t *testing.T) {
	disp := &recordingDispatcher{}
	c := newToolCall(directoryWithTwo(), disp, "s1")

	got := c.runTool(ToolCallEvent{ID: "1", Name: ToolRunPrompt, Args: map[string]any{
		"prompt": "add a retry around the reconnect",
		"target": "Live Voice Dialog",
	}})

	if _, refused := got["error"]; refused {
		t.Fatalf("a correctly named target was refused: %v", got)
	}
	sent := disp.dispatches()
	if len(sent) != 1 || sent[0].session != "s1" {
		t.Fatalf("dispatches = %v, want one to s1", sent)
	}
}

// Every answer says where the call is aimed. The assistant's picture of that
// lives only in its own transcript, and drifts there in silence.
func TestEveryToolAnswerNamesTheFocus(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "s1")

	for _, ev := range []ToolCallEvent{
		{ID: "1", Name: ToolListSessions, Args: map[string]any{"filter": FilterAll}},
		{ID: "2", Name: ToolFindSession, Args: map[string]any{"query": "voice"}},
		{ID: "3", Name: ToolListProjects},
	} {
		got := c.runTool(ev)
		focused, _ := got["focused_on"].(string)
		if !strings.Contains(focused, "Live Voice Dialog") {
			t.Errorf("%s answered focused_on = %q, want the session the call is aimed at", ev.Name, focused)
		}
	}
}

// The call opens knowing its focus by id alone, because the operator chose it
// by pressing a button rather than from a list. Anything that asks fills it in,
// or the most common dispatch target on any call is the one the server can say
// least about — which would also refuse every target named against it.
func TestTheOpeningFocusIsFilledInFromTheDatabase(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "s1")

	if row, _ := c.offeredRow("s1"); row.Name != "" {
		t.Fatalf("the opening focus started out named %q; this test proves nothing", row.Name)
	}

	if got := displayFor(c.focusRow(context.Background())); got != "Live Voice Dialog in agentique" {
		t.Errorf("focusRow = %q, want the row from the database", got)
	}
	if row, _ := c.offeredRow("s1"); row.Name != "Live Voice Dialog" {
		t.Error("what was looked up was not kept, so the next lookup pays again")
	}
}

// A refusal reaches the log with a token to grep, and the model with a sentence
// to say — and never the other way round.
func TestRefusalsAreLoggedAndTheReasonNeverReachesTheModel(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &recordingDispatcher{}, "")

	result := c.runTool(ToolCallEvent{ID: "1", Name: ToolFocusSession,
		Args: map[string]any{"session_id": "nobody-offered-this"}})
	if _, ok := result[reasonKey]; !ok {
		t.Fatal("the refusal carries no reason for the log")
	}

	c.recordRefusal(ToolFocusSession, result)
	if _, leaked := result[reasonKey]; leaked {
		t.Error("the log's reason leaked into the model's answer")
	}
	if _, said := result["error"]; !said {
		t.Error("recording a refusal removed the sentence the listener hears")
	}
}
