package voice

import (
	"strings"
	"testing"
	"time"
)

// The fault this path was built for: the assistant said it was hanging up, and
// the call stayed open. Arming has to be a state the call actually holds, not a
// sentence it said.
func TestHangUpArmsTheCallAndAsksForAGoodbye(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "sess-1")
	if c.askedToHangUp() {
		t.Fatal("a fresh call is not hanging up")
	}

	got := c.runTool(ToolCallEvent{ID: "1", Name: ToolHangUp})
	if _, bad := got["error"]; bad {
		t.Fatalf("hang_up refused: %v", got)
	}
	if !c.askedToHangUp() {
		t.Error("hang_up did not arm the call, so nothing would ever close it")
	}
	if c.ended() {
		t.Error("the call ended before the goodbye was spoken")
	}

	// The tool answer is what the listener hears, and it is the last thing they
	// will hear — so it has to be a farewell and a full stop.
	out, _ := got["output"].(string)
	lower := strings.ToLower(out)
	for _, want := range []string{"say goodbye", "ends the moment you stop speaking", "keeps going"} {
		if !strings.Contains(lower, want) {
			t.Errorf("hang_up answer is missing %q: %s", want, out)
		}
	}
}

// The goodbye is spoken on the turn after the tool answers, and that turn
// completing is what ends the call.
func TestTheGoodbyeTurnEndsTheCall(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "sess-1")

	// An ordinary turn on an unarmed call must not close anything.
	if err := c.forward(TurnCompleteEvent{}); err == nil {
		t.Fatal("a socketless call should have failed to send turn_complete")
	}
	if c.ended() {
		t.Fatal("a turn completing ended a call nobody asked to end")
	}

	c.runTool(ToolCallEvent{ID: "1", Name: ToolHangUp})
	_ = c.forward(TurnCompleteEvent{})
	if !c.ended() {
		t.Error("the goodbye finished and the call stayed open — the original fault")
	}
}

// Talking over the farewell is not a retraction. They asked to hang up; an
// interrupted goodbye still ends the call, or barging in wedges it open.
func TestAnInterruptedGoodbyeStillEndsTheCall(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "sess-1")
	c.runTool(ToolCallEvent{ID: "1", Name: ToolHangUp})

	_ = c.forward(TurnCompleteEvent{Interrupted: true})
	if !c.ended() {
		t.Error("an interrupted goodbye left the call open")
	}
}

// "Close when the turn completes" is a promise about an engine, and an engine
// that is wedged, reconnecting or voiceless never completes one. The grace is
// what stops an explicit hangup from falling back to the idle rule — which, on
// a call following a run, is half an hour.
func TestTheGoodbyeGraceClosesACallThatNeverSpeaks(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "sess-1")
	now := time.Now()

	if c.hangupOverdue(now) {
		t.Fatal("a call nobody asked to end is never overdue")
	}

	c.armHangup(now)
	if c.hangupOverdue(now.Add(goodbyeGrace - time.Second)) {
		t.Error("the grace expired before the goodbye had a chance to be said")
	}
	if !c.hangupOverdue(now.Add(goodbyeGrace + time.Second)) {
		t.Error("an engine that never completed a turn would hold the call open forever")
	}
}

// A second ask must not extend the first one's deadline, or an assistant that
// keeps saying farewell keeps the line open as long as it keeps talking.
func TestAskingTwiceDoesNotExtendTheGrace(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "sess-1")
	start := time.Now()

	c.armHangup(start)
	first := c.hangupGrace.Load()
	c.armHangup(start.Add(30 * time.Second))
	if c.hangupGrace.Load() != first {
		t.Error("a second hang_up pushed the deadline out")
	}
}

// The browser is told once. Two closing frames would have it sound the hangup
// tone twice, and the two paths that end an armed call genuinely race.
func TestTheClosingFrameGoesOutExactlyOnce(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "sess-1")

	if !c.endCall(hangupReason) {
		t.Fatal("the first close did not report itself as the one that closed")
	}
	if c.endCall("idle") {
		t.Error("a second close was reported as an ending of its own")
	}
	if !c.ended() {
		t.Error("the call does not know it has been closed")
	}
}

// Hanging up is the operator's gesture. A call that ends itself because a run
// finished, or because nobody said anything, is a call that hung up on someone
// who was still there.
func TestNothingButTheToolArmsAHangup(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "sess-1")

	c.runTool(ToolCallEvent{ID: "1", Name: ToolRunPrompt, Args: map[string]any{
		"prompt": "do the thing",
	}})
	c.markRunEnded("sess-1")
	_ = c.forward(TurnCompleteEvent{})

	if c.askedToHangUp() || c.ended() {
		t.Error("the call hung up without being asked to")
	}
}
