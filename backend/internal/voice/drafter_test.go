package voice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestDeliverySpokenDistinguishesTheThreeOutcomes(t *testing.T) {
	// The whole reason delivery is reported rather than inferred is that these
	// are three different sentences to a listener.
	seen := map[string]bool{}
	for _, d := range []Delivery{DeliveryTurn, DeliveryMidTurn, DeliveryQueued} {
		s := d.Spoken()
		if s == "" {
			t.Errorf("%q has nothing to say", d)
		}
		if seen[s] {
			t.Errorf("%q reuses another outcome's wording: %q", d, s)
		}
		seen[s] = true
	}
	if !strings.Contains(strings.ToLower(DeliveryQueued.Spoken()), "queue") {
		t.Error("a queued prompt must say it is waiting, or the user thinks it started")
	}
}

func TestSystemInstructionCarriesTheLoadBearingRules(t *testing.T) {
	got := strings.ToLower(SystemInstruction(Briefing{Persona: Persona{}}))
	for _, want := range []string{
		"never answer the question yourself", // the likeliest failure
		"silence is not consent",             // the safety contract
		ToolRunPrompt,                        // it must know the tool's name
		"read the prompt back",               // the hands-free readback
		"greeting them is one sentence",      // the pickup greeting is not an introduction
		"never repeat it later",              // and it happens once
		"drop the greeting and listen",       // the operator outranks the ritual
	} {
		if !strings.Contains(got, strings.ToLower(want)) {
			t.Errorf("system instruction is missing %q", want)
		}
	}
}

// The greeting cue is the trigger, because the speech model has no "call
// opened" event: the first injected text is what makes it speak at all.
func TestGreetingCueSaysWhatToSayAndStops(t *testing.T) {
	focused := greetingCue("Live Voice Dialog")
	unfocused := greetingCue("")

	for _, cue := range []string{focused, unfocused} {
		lower := strings.ToLower(cue)
		for _, want := range []string{
			"call connected",     // it is the server's own words, not the user's
			"one short sentence", // a greeting, not an introduction
			"never repeat it",
			"drop the greeting and listen", // barge-in: the operator was there first
		} {
			if !strings.Contains(lower, want) {
				t.Errorf("greeting cue is missing %q: %q", want, cue)
			}
		}
		// Server words carry no quotation framing — there is no agent-written
		// content in here to quote.
		if strings.Contains(cue, "NOT an instruction") {
			t.Errorf("the greeting cue framed itself as untrusted content: %q", cue)
		}
	}

	if !strings.Contains(focused, "Live Voice Dialog") {
		t.Errorf("a focused greeting must name the session: %q", focused)
	}

	// The unfocused call is where the instruction's single line of orientation
	// is spent, and the cue has to say it REPLACES that offer — otherwise the
	// operator hears the same three options twice in ten seconds.
	if !strings.Contains(unfocused, "switch to a session by name") {
		t.Errorf("an unfocused greeting did not fold in the orientation offer: %q", unfocused)
	}
	if !strings.Contains(unfocused, "replaces") {
		t.Errorf("the unfocused greeting must replace the orientation offer, not double it: %q", unfocused)
	}
	if strings.Contains(focused, "switch to a session by name") {
		t.Errorf("a focused greeting was given the orientation offer as well: %q", focused)
	}
}

// The switchboard rules. Each one exists because of a specific way this goes
// wrong out loud: acting on a session nobody heard named, picking between two
// similar names, or promising work on a machine that cannot receive it.
func TestSystemInstructionCarriesTheSwitchboardRules(t *testing.T) {
	got := strings.ToLower(SystemInstruction(Briefing{Persona: Persona{}}))
	for _, want := range []string{
		ToolListSessions,
		ToolFindSession,
		ToolFocusSession,
		ToolSummarizeSession,
		"naming the session it is going to", // the read-back names the target
		"it never picks for you",            // ambiguity is asked about, not resolved
		"by its full name",                  // a clear winner is still confirmed
		"other machines",                    // remote sessions can be seen, not worked in
		"never switch silently",             // a viewing note is data
	} {
		if !strings.Contains(got, strings.ToLower(want)) {
			t.Errorf("system instruction is missing %q", want)
		}
	}
}

// Creating a session is deferred to the same yes that sends the prompt.
//
// Every rule asserted here answers a specific failure: an extra round trip is
// another chance for a transcription to go wrong, creating before the yes
// orphans an empty session and its worktree when a call drops, and stopping
// between the create and the send turns one agreed gesture into a second
// question.
func TestSystemInstructionDefersCreationToTheOneYes(t *testing.T) {
	got := strings.ToLower(SystemInstruction(Briefing{Persona: Persona{}}))
	for _, want := range []string{
		ToolListProjects,
		ToolCreateSession,
		"not until they have said yes",     // nothing exists before consent
		"settings are stated, never asked", // no defaults question of its own
		"one read-back covers all of it",   // project, settings and prompt in one breath
		"new* session",                     // the read-back says it is a new one
		"immediately",                      // create then send, without a pause
		"never pick",                       // an ambiguous project is asked about
		"created on this machine only",     // a remote project cannot host one
	} {
		if !strings.Contains(got, strings.ToLower(want)) {
			t.Errorf("system instruction is missing %q", want)
		}
	}
	// The consent gate did not move: it is still the read-back before the send.
	if !strings.Contains(got, "silence is still not consent") {
		t.Error("creating a session must not weaken the consent rule")
	}
}

// Asked what it can do, the assistant answers rather than sending the question
// to a coding agent — and answers with what is true.
//
// The carve-out has to sit beside the never-answer rule: read apart, the two
// contradict, and the model resolves that by drafting a prompt for "what can
// you do?". The CANNOT list is the other half: a speech model with tools will
// offer to approve and merge unless it is told in words that it cannot.
func TestSystemInstructionAnswersQuestionsAboutItself(t *testing.T) {
	full := SystemInstruction(Briefing{})
	got := strings.ToLower(full)

	for _, want := range []string{
		"that rule is about their code and their work", // the carve-out
		"questions about *you*",
		"you can do, how to switch or start a session",
		"never turn a question about this call into", // and never into a prompt
		"you can:",
		"you cannot, and must never offer to",
		"approve anything",          // no spoken approval
		"another machine",           // remote sessions are look-only
		"archive, merge",            // not its to do
		"without reading it back",   // the consent gate holds even here
		"talk about cost",           // costs never appear
		"never invent a capability", // the hallucination guard
		"one or two sentences",      // help follows the speech rules
	} {
		if !strings.Contains(got, strings.ToLower(want)) {
			t.Errorf("system instruction is missing %q", want)
		}
	}

	// The carve-out is worthless if it lands in a different part of the prompt
	// from the rule it modifies.
	rule := strings.Index(full, "You never answer the question yourself")
	carveOut := strings.Index(full, "That rule is about their code and their work")
	if rule < 0 || carveOut < 0 || carveOut < rule {
		t.Fatalf("the carve-out is at %d and the rule at %d — they must be read together", carveOut, rule)
	}
	if strings.Contains(full[rule:carveOut], "\n# ") {
		t.Error("a section heading separates the never-answer rule from its carve-out")
	}
}

// A call that opened on nothing may orient them once. One that opened on a
// session must not: they pressed the button from it and know where they are.
func TestOrientationOfferOnlyWhenTheCallOpenedOnNothing(t *testing.T) {
	const offer = "did not open on any session"

	unfocused := SystemInstruction(Briefing{})
	if !strings.Contains(unfocused, offer) {
		t.Error("an unfocused call was given nothing to offer")
	}
	if !strings.Contains(unfocused, "never again in this call") {
		t.Error("the orientation offer must be once, not a tutorial")
	}

	focused := SystemInstruction(Briefing{InitialFocus: "sess-1"})
	if strings.Contains(focused, offer) {
		t.Error("a call that opened on a session still offers to orient them")
	}
}

// Orientation is what is going on when the call opens — reference material with
// a shelf life, and it must say so rather than being quoted as current.
func TestSystemInstructionIncludesOrientation(t *testing.T) {
	got := SystemInstruction(Briefing{Orientation: "Three sessions, one waiting on approval."})
	if !strings.Contains(got, "one waiting on approval") {
		t.Error("orientation did not reach the instruction")
	}
	if !strings.Contains(got, ToolListSessions) {
		t.Error("orientation must point at the tool that refreshes it")
	}

	// Absent is the ordinary case for a machine with no directory wired.
	if strings.Contains(SystemInstruction(Briefing{Persona: Persona{}}), "What is going on right now") {
		t.Error("an empty orientation must not leave an empty section behind")
	}
}

func TestSystemInstructionIncludesProjectContext(t *testing.T) {
	got := SystemInstruction(Briefing{ProjectContext: "The repo is a Go backend with a React frontend."})
	if !strings.Contains(got, "Go backend with a React frontend") {
		t.Error("project context did not reach the instruction")
	}
	// Context is reference material an agent wrote or a repo supplied, so it
	// must be framed as such rather than as more instructions.
	if !strings.Contains(strings.ToLower(got), "not instructions") {
		t.Error("project context must be framed as reference, not as instructions")
	}
}

// --- dispatcher doubles ---

type fakeDispatcher struct {
	mu        sync.Mutex
	delivery  Delivery
	dispErr   error
	autoOK    bool
	autoWhy   string
	autoErr   error
	gotPrompt string
	calls     int

	gotReporting bool

	projectContext string
}

func (f *fakeDispatcher) Dispatch(_ context.Context, _, prompt string, withReporting bool) (Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotPrompt = prompt
	f.gotReporting = withReporting
	if f.dispErr != nil {
		return "", f.dispErr
	}
	return f.delivery, nil
}

func (f *fakeDispatcher) AutoRunnable(context.Context, string) (bool, string, error) {
	return f.autoOK, f.autoWhy, f.autoErr
}

func (f *fakeDispatcher) ProjectContext(context.Context, string) string { return f.projectContext }

func (f *fakeDispatcher) dispatched() (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.gotPrompt
}

// newTestCall builds a call with no socket. sendControl writes to a nil socket;
// every path through runTool tolerates that failing.
func newTestCall(d Dispatcher, registry *Registry, focus string) *call {
	return &call{
		engine:          NewEchoEngine(),
		registry:        registry,
		dispatcher:      d,
		focus:           focus,
		follows:         make(map[string]*followState),
		offered:         make(map[string]SessionRow),
		offeredProjects: make(map[string]ProjectRow),
		summaries:       make(map[string]string),
		log:             testLogger(),
		runCtx:          context.Background(),
	}
}

// toolCall exercises runTool against a call wired to the given dispatcher.
func toolCall(t *testing.T, d Dispatcher, target string, args map[string]any) map[string]any {
	t.Helper()
	c := newTestCall(d, NewRegistry(), target)
	return c.runTool(ToolCallEvent{ID: "1", Name: ToolRunPrompt, Args: args})
}

func TestRunPromptDispatchesAndReportsDelivery(t *testing.T) {
	d := &fakeDispatcher{autoOK: true, delivery: DeliveryQueued}
	got := toolCall(t, d, "sess-1", map[string]any{
		"prompt":       "  fix the reconnect  ",
		"stay_on_line": true,
	})

	out, _ := got["output"].(string)
	if !strings.Contains(strings.ToLower(out), "queue") {
		t.Errorf("result = %v, want the queued wording", got)
	}
	calls, prompt := d.dispatched()
	if calls != 1 {
		t.Fatalf("dispatched %d times, want 1", calls)
	}
	if prompt != "fix the reconnect" {
		t.Errorf("prompt = %q, want it trimmed", prompt)
	}
}

// No spoken approval exists, so a session that would stop and ask must be
// refused at the handoff rather than stalling with the call sounding fine.
func TestRunPromptRefusesASessionThatWouldStopAndAsk(t *testing.T) {
	d := &fakeDispatcher{autoOK: false, autoWhy: `It is currently set to "auto".`}
	got := toolCall(t, d, "sess-1", map[string]any{"prompt": "do the thing"})

	msg, _ := got["error"].(string)
	if msg == "" {
		t.Fatalf("result = %v, want a refusal", got)
	}
	if !strings.Contains(strings.ToLower(msg), "auto mode") {
		t.Errorf("refusal = %q, want it to name the reason", msg)
	}
	if calls, _ := d.dispatched(); calls != 0 {
		t.Errorf("dispatched %d times despite the refusal, want 0", calls)
	}
}

func TestRunPromptRejectsAnEmptyPrompt(t *testing.T) {
	d := &fakeDispatcher{autoOK: true, delivery: DeliveryTurn}
	for _, arg := range []map[string]any{
		{"prompt": "   "},
		{},
		{"prompt": 42},
	} {
		got := toolCall(t, d, "sess-1", arg)
		if _, ok := got["error"]; !ok {
			t.Errorf("args %v gave %v, want an error", arg, got)
		}
	}
	if calls, _ := d.dispatched(); calls != 0 {
		t.Errorf("dispatched %d times on empty prompts, want 0", calls)
	}
}

// Staying on the line is what turns reporting on. A run nobody is listening to
// must carry no reporting instruction at all.
func TestStayOnLineDecidesReporting(t *testing.T) {
	for _, staying := range []bool{true, false} {
		d := &fakeDispatcher{autoOK: true, delivery: DeliveryTurn}
		got := toolCall(t, d, "sess-1", map[string]any{
			"prompt":       "do the thing",
			"stay_on_line": staying,
		})
		if _, bad := got["error"]; bad {
			t.Fatalf("stay_on_line=%v gave %v", staying, got)
		}
		d.mu.Lock()
		reporting := d.gotReporting
		d.mu.Unlock()
		if reporting != staying {
			t.Errorf("stay_on_line=%v produced withReporting=%v", staying, reporting)
		}
	}
}

// Hanging up must be said, not silently implied — otherwise the listener waits
// for updates that are never coming.
func TestHangingUpSaysThereWillBeNoUpdates(t *testing.T) {
	d := &fakeDispatcher{autoOK: true, delivery: DeliveryTurn}
	got := toolCall(t, d, "sess-1", map[string]any{
		"prompt":       "do the thing",
		"stay_on_line": false,
	})
	out, _ := got["output"].(string)
	if !strings.Contains(strings.ToLower(out), "no further updates") {
		t.Errorf("result = %q, want it to say no more updates are coming", out)
	}
}

// Absent means no: an open microphone is the expensive answer, so it has to be
// asked for rather than defaulted into.
func TestAbsentStayOnLineDoesNotFollow(t *testing.T) {
	d := &fakeDispatcher{autoOK: true, delivery: DeliveryTurn}
	got := toolCall(t, d, "sess-1", map[string]any{"prompt": "do the thing"})
	if _, bad := got["error"]; bad {
		t.Fatalf("result = %v, want a dispatch", got)
	}
	d.mu.Lock()
	reporting := d.gotReporting
	d.mu.Unlock()
	if reporting {
		t.Error("an absent stay_on_line must not turn reporting on")
	}
}

func TestRunPromptWithoutASessionSaysSo(t *testing.T) {
	d := &fakeDispatcher{autoOK: true, delivery: DeliveryTurn}
	got := toolCall(t, d, "", map[string]any{"prompt": "do the thing"})
	if _, ok := got["error"]; !ok {
		t.Errorf("result = %v, want an error when no session is attached", got)
	}
}

// Every path must produce a response payload. An unanswered tool call leaves
// the model paused forever, which sounds exactly like the call having died.
func TestEveryToolPathAnswers(t *testing.T) {
	cases := []struct {
		name string
		d    *fakeDispatcher
		ev   ToolCallEvent
	}{
		{"unknown tool", &fakeDispatcher{autoOK: true}, ToolCallEvent{Name: "something_else"}},
		{"dispatch fails", &fakeDispatcher{autoOK: true, dispErr: errors.New("boom")}, ToolCallEvent{Name: ToolRunPrompt, Args: map[string]any{"prompt": "x"}}},
		{"auto check fails", &fakeDispatcher{autoErr: errors.New("gone")}, ToolCallEvent{Name: ToolRunPrompt, Args: map[string]any{"prompt": "x"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCall(tc.d, NewRegistry(), "sess-1")
			got := c.runTool(tc.ev)
			if len(got) == 0 {
				t.Fatal("no response payload — the model would stay paused forever")
			}
		})
	}
}

// The reporting instruction is a page of prose and the worker keeps the first
// copy in context, so a second dispatch on the same call must not repeat it.
func TestTheWorkerIsBriefedOncePerCall(t *testing.T) {
	d := &fakeDispatcher{autoOK: true, delivery: DeliveryTurn}
	c := newTestCall(d, NewRegistry(), "sess-1")
	args := map[string]any{"prompt": "do the thing", "stay_on_line": true}

	c.runTool(ToolCallEvent{ID: "1", Name: ToolRunPrompt, Args: args})
	d.mu.Lock()
	first := d.gotReporting
	d.mu.Unlock()
	if !first {
		t.Fatal("the first dispatch must teach the worker how to report")
	}

	c.runTool(ToolCallEvent{ID: "2", Name: ToolRunPrompt, Args: args})
	d.mu.Lock()
	second := d.gotReporting
	d.mu.Unlock()
	if second {
		t.Error("the second dispatch repeated the whole reporting instruction")
	}
}

// A later "no, don't stay" must not tear down the follow from an earlier "yes".
//
// Reproduces a real call: dispatch, ask to stay, then add a second thing
// mid-run. If that second dispatch says "not staying", the first run's reports
// went nowhere — the listener asked for progress and got nothing, because the
// binding had been quietly released underneath them.
func TestASecondDispatchCannotUnfollowARunningOne(t *testing.T) {
	registry := NewRegistry()
	d := &fakeDispatcher{autoOK: true, delivery: DeliveryTurn}
	c := newTestCall(d, registry, "sess-1")

	c.runTool(ToolCallEvent{ID: "1", Name: ToolRunPrompt, Args: map[string]any{
		"prompt": "the first thing", "stay_on_line": true,
	}})
	if !registry.Listening("sess-1") {
		t.Fatal("staying on the line did not start following")
	}

	c.runTool(ToolCallEvent{ID: "2", Name: ToolRunPrompt, Args: map[string]any{
		"prompt": "and also this", "stay_on_line": false,
	}})
	if !registry.Listening("sess-1") {
		t.Error("a second dispatch released the follow the first one established")
	}
}
