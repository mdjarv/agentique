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
	got := strings.ToLower(SystemInstruction("", Persona{}))
	for _, want := range []string{
		"never answer the question yourself", // the likeliest failure
		"silence is not consent",             // the safety contract
		ToolRunPrompt,                        // it must know the tool's name
		"read the prompt back",               // the hands-free readback
	} {
		if !strings.Contains(got, strings.ToLower(want)) {
			t.Errorf("system instruction is missing %q", want)
		}
	}
}

func TestSystemInstructionIncludesProjectContext(t *testing.T) {
	got := SystemInstruction("The repo is a Go backend with a React frontend.", Persona{})
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

// toolCall exercises runTool against a call wired to the given dispatcher.
func toolCall(t *testing.T, d Dispatcher, target string, args map[string]any) map[string]any {
	t.Helper()
	c := &call{
		engine:        NewEchoEngine(),
		registry:      NewRegistry(),
		dispatcher:    d,
		targetSession: target,
		log:           testLogger(),
		runCtx:        context.Background(),
		// sendControl writes to a nil socket; runTool tolerates that failing.
	}
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
			c := &call{
				engine:        NewEchoEngine(),
				registry:      NewRegistry(),
				dispatcher:    tc.d,
				targetSession: "sess-1",
				log:           testLogger(),
				runCtx:        context.Background(),
			}
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
	c := &call{
		engine:        NewEchoEngine(),
		registry:      NewRegistry(),
		dispatcher:    d,
		targetSession: "sess-1",
		log:           testLogger(),
		runCtx:        context.Background(),
	}
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
	c := &call{
		engine:        NewEchoEngine(),
		registry:      registry,
		dispatcher:    d,
		targetSession: "sess-1",
		log:           testLogger(),
		runCtx:        context.Background(),
	}

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
