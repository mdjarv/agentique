package voice

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// The second utterance of a call is where this state used to go wrong, so these
// tests are all about what happens after the first "yes".
//
// A call is not one session and one run. It can start work in one session, be
// asked for something else in another, and end up following both — which means
// briefing, in-flight and the phase derived from it are per session, and a
// notice about one run says nothing about the other.

// dispatchRecord is one call into the dispatcher, kept so a test can assert
// what each session was actually told.
type dispatchRecord struct {
	session   string
	prompt    string
	reporting bool
}

// recordingDispatcher accepts every dispatch and remembers it per session.
type recordingDispatcher struct {
	mu      sync.Mutex
	records []dispatchRecord
}

func (r *recordingDispatcher) Dispatch(_ context.Context, sessionID, prompt string, withReporting bool) (Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, dispatchRecord{session: sessionID, prompt: prompt, reporting: withReporting})
	return DeliveryTurn, nil
}

func (r *recordingDispatcher) AutoRunnable(context.Context, string) (bool, string, error) {
	return true, "", nil
}

func (r *recordingDispatcher) ProjectContext(context.Context, string) string { return "" }

// briefings counts the dispatches that carried the reporting instruction to a
// session.
func (r *recordingDispatcher) briefings(sessionID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int
	for _, rec := range r.records {
		if rec.session == sessionID && rec.reporting {
			n++
		}
	}
	return n
}

// step is one thing that happens during a call.
type step struct {
	// focus aims the call before the step runs, when set.
	focus string
	// prompt dispatches, staying on the line or not.
	prompt string
	stay   bool
	// notice delivers a runtime fact about a session.
	noticeFor  string
	noticeKind NoticeKind
	// unfollow releases one session.
	unfollowSession string
}

func TestCallStateAcrossASecondUtterance(t *testing.T) {
	tests := []struct {
		name  string
		steps []step
		check func(t *testing.T, c *call, d *recordingDispatcher)
	}{
		{
			// The latent bug this refactor exists for: briefing was per call, so
			// a second session dispatched from one call was never taught how to
			// report and the listener heard nothing from it.
			name: "a second session is briefed, and the first is not briefed twice",
			steps: []step{
				{focus: "sess-a", prompt: "look at the reconnect", stay: true},
				{focus: "sess-a", prompt: "and also the tests", stay: true},
				{focus: "sess-b", prompt: "start on the docs", stay: true},
			},
			check: func(t *testing.T, _ *call, d *recordingDispatcher) {
				if got := d.briefings("sess-a"); got != 1 {
					t.Errorf("sess-a was briefed %d times, want exactly 1", got)
				}
				if got := d.briefings("sess-b"); got != 1 {
					t.Errorf("sess-b was briefed %d times, want exactly 1 — it has never seen the instruction", got)
				}
			},
		},
		{
			// Preserves the fix from "a later don't stay no longer cancels an
			// earlier yes", generalised to the follow set.
			name: "a no after a yes changes nothing about the first follow",
			steps: []step{
				{focus: "sess-a", prompt: "the first thing", stay: true},
				{focus: "sess-a", prompt: "and this too", stay: false},
			},
			check: func(t *testing.T, c *call, _ *recordingDispatcher) {
				if !c.following("sess-a") {
					t.Error("a second dispatch answering no released the first one's follow")
				}
				if c.currentPhase() != phaseWorking {
					t.Error("the earlier run is still in flight, so the call is still working")
				}
			},
		},
		{
			name: "declining on a different session leaves the first one followed",
			steps: []step{
				{focus: "sess-a", prompt: "the first thing", stay: true},
				{focus: "sess-b", prompt: "just kick this off", stay: false},
			},
			check: func(t *testing.T, c *call, _ *recordingDispatcher) {
				if !c.following("sess-a") {
					t.Error("dispatching elsewhere released the follow on the first session")
				}
				if c.following("sess-b") {
					t.Error("declining to stay must not start following")
				}
			},
		},
		{
			// A flag toggled by whichever notice arrived last would hang the call
			// up in the middle of the other run.
			name: "one run finishing while another is in flight keeps the call working",
			steps: []step{
				{focus: "sess-a", prompt: "the long one", stay: true},
				{focus: "sess-b", prompt: "the quick one", stay: true},
				{noticeFor: "sess-b", noticeKind: NoticeFinished},
			},
			check: func(t *testing.T, c *call, _ *recordingDispatcher) {
				if c.currentPhase() != phaseWorking {
					t.Error("sess-a is still running, so silence is still expected")
				}
			},
		},
		{
			name: "the last run finishing returns the call to gathering",
			steps: []step{
				{focus: "sess-a", prompt: "the only one", stay: true},
				{noticeFor: "sess-a", noticeKind: NoticeFinished},
			},
			check: func(t *testing.T, c *call, _ *recordingDispatcher) {
				if c.currentPhase() != phaseGathering {
					t.Error("nothing is running, so silence means abandonment again")
				}
				if !c.following("sess-a") {
					t.Error("a finished run must not unfollow the session")
				}
			},
		},
		{
			// The run is stuck, not done, and it is still holding a process.
			name: "blocked does not end the work",
			steps: []step{
				{focus: "sess-a", prompt: "do the thing", stay: true},
				{noticeFor: "sess-a", noticeKind: NoticeBlocked},
			},
			check: func(t *testing.T, c *call, _ *recordingDispatcher) {
				if c.currentPhase() != phaseWorking {
					t.Error("a blocked run still holds a process; the call must not go back to the short idle rule")
				}
			},
		},
		{
			name: "unfollowing one session leaves the other followed",
			steps: []step{
				{focus: "sess-a", prompt: "the first thing", stay: true},
				{focus: "sess-b", prompt: "the second thing", stay: true},
				{unfollowSession: "sess-a"},
			},
			check: func(t *testing.T, c *call, _ *recordingDispatcher) {
				if c.following("sess-a") {
					t.Error("sess-a was unfollowed and should be gone")
				}
				if !c.following("sess-b") {
					t.Error("unfollowing one session released the other")
				}
				if c.currentPhase() != phaseWorking {
					t.Error("sess-b is still running")
				}
			},
		},
		{
			name: "a failed run ends the work like a finished one",
			steps: []step{
				{focus: "sess-a", prompt: "do the thing", stay: true},
				{noticeFor: "sess-a", noticeKind: NoticeFailed},
			},
			check: func(t *testing.T, c *call, _ *recordingDispatcher) {
				if c.currentPhase() != phaseGathering {
					t.Error("a failed run is over, so the conversational idle rule applies again")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &recordingDispatcher{}
			registry := NewRegistry()
			c := newTestCall(d, registry, "")

			for _, s := range tt.steps {
				switch {
				case s.prompt != "":
					if s.focus != "" {
						c.setFocus(s.focus)
					}
					got := c.runTool(ToolCallEvent{ID: "t", Name: ToolRunPrompt, Args: map[string]any{
						"prompt":       s.prompt,
						"stay_on_line": s.stay,
					}})
					if _, bad := got["error"]; bad {
						t.Fatalf("dispatch %q failed: %v", s.prompt, got)
					}
				case s.noticeFor != "":
					registry.Notice(s.noticeFor, Notice{Kind: s.noticeKind, Headline: "something happened"})
				case s.unfollowSession != "":
					c.unfollow(s.unfollowSession)
				}
			}

			tt.check(t, c, d)
		})
	}
}

// The phase is derived from the follow set rather than toggled, so it is right
// under concurrency rather than right in the order the notices happened to
// arrive.
func TestPhaseIsDerivedFromTheFollowSet(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "")

	if c.currentPhase() != phaseGathering {
		t.Fatal("a call following nothing is gathering")
	}

	c.follow("sess-a", "Live Voice Dialog")
	if c.currentPhase() != phaseGathering {
		t.Error("following alone is not working — nothing has been dispatched")
	}

	c.markWorking("sess-a")
	if c.currentPhase() != phaseWorking {
		t.Error("a run in flight is working")
	}

	// A session nobody follows cannot be marked: nothing would ever clear it,
	// and the call would sit in the long idle ceiling forever.
	c.markWorking("sess-unknown")
	c.markRunEnded("sess-a")
	if c.currentPhase() != phaseGathering {
		t.Error("the only run ended, so the call is gathering again")
	}
}

// The idle guard closes an abandoned call. Everything below is about the
// difference between an abandoned call and a quiet one, which is where the
// first real call died.

// silentEngine has voice activity detection and reports that nobody has spoken
// for a long time — the state every idle decision below is made in.
type silentEngine struct {
	*EchoEngine
	speech time.Time
}

func (e *silentEngine) LastSpeech() time.Time { return e.speech }

// quietCall is a call that has heard nothing since `since`: no speech, no
// frames, no interaction.
func quietCall(since time.Time) *call {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "")
	c.engine = &silentEngine{EchoEngine: NewEchoEngine(), speech: since}
	c.idleTimeout = defaultIdleTimeout
	c.lastFrame = since
	c.lastInteraction = since
	return c
}

func setInteraction(c *call, at time.Time) {
	c.lastInteractionMu.Lock()
	c.lastInteraction = at
	c.lastInteractionMu.Unlock()
}

// Speech is not the only sign of life. The operator asking for something and
// then listening is a conversation in progress, and the engine's voice activity
// clock cannot see any of it.
func TestActivityCountsWhatTheCallDidNotOnlyWhatItHeard(t *testing.T) {
	since := time.Now().Add(-10 * time.Minute)

	t.Run("a control frame", func(t *testing.T) {
		c := quietCall(since)
		if !c.lastActivity().Equal(since) {
			t.Fatalf("lastActivity = %v, want the stale speech clock %v", c.lastActivity(), since)
		}
		if !c.idleExpired(time.Now()) {
			t.Fatal("a call nobody has touched for ten minutes is abandoned")
		}

		c.handleControl([]byte(`{"type":"world","sessions":[]}`))
		if !c.lastActivity().After(since) {
			t.Error("a control frame from the browser did not count as activity")
		}
		if c.idleExpired(time.Now()) {
			t.Error("the client just spoke to the server; the call is not abandoned")
		}
	})

	t.Run("a tool call", func(t *testing.T) {
		c := quietCall(since)
		c.handleToolCall(ToolCallEvent{ID: "1", Name: ToolListSessions})
		if !c.lastActivity().After(since) {
			t.Error("the model reached for a tool because somebody asked it something")
		}
	})

	// A microphone streams continuously from an empty room, so frame arrival
	// must never override an engine that knows when the caller last spoke.
	t.Run("frames do not override the speech clock", func(t *testing.T) {
		c := quietCall(since)
		c.noteFrame()
		if !c.lastActivity().Equal(since) {
			t.Error("frames kept an abandoned call alive; that is what the VAD clock is for")
		}
	})
}

// The field trap: the operator asked for a session summary, went quiet while a
// local provider-CLI run computed it, and the ninety-second gathering rule hung
// up before the answer arrived.
func TestAPromisedAnswerHoldsTheLine(t *testing.T) {
	since := time.Now().Add(-10 * time.Minute)
	c := quietCall(since)

	if got := c.idleLimit(); got != defaultIdleTimeout {
		t.Fatalf("idleLimit = %v, want the conversational rule %v", got, defaultIdleTimeout)
	}

	c.beginAsync()
	if got := c.idleLimit(); got != workingIdleCeiling {
		t.Errorf("idleLimit = %v while an answer is being computed, want the working ceiling %v", got, workingIdleCeiling)
	}
	if c.idleExpired(time.Now()) {
		t.Error("closed the call while the summary it had asked for was still in flight")
	}

	// Two asks can be outstanding at once, and the second finishing must not
	// cancel the first one's claim on the line.
	c.beginAsync()
	c.endAsync()
	setInteraction(c, since)
	if got := c.idleLimit(); got != workingIdleCeiling {
		t.Errorf("idleLimit = %v with one answer still outstanding, want %v", got, workingIdleCeiling)
	}

	c.endAsync()
	if got := c.idleLimit(); got != defaultIdleTimeout {
		t.Errorf("idleLimit = %v once everything is delivered, want %v", got, defaultIdleTimeout)
	}
	// Delivery is itself a sign of life: what the operator asked for just
	// landed, and talking about it is what happens next.
	if c.idleExpired(time.Now()) {
		t.Error("a delivered answer did not count as activity")
	}
}

// --- the pickup greeting ---

// greetingCall is a call wired to an engine and a directory, with no socket.
func greetingCall(engine Engine, dir Directory, focus string) *call {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), focus)
	c.engine = engine
	c.directory = dir
	return c
}

// The assistant speaks first, because a speech model that only answers when it
// is spoken to leaves a freshly connected call silent — and in a car that is
// indistinguishable from a call that never came up.
func TestPickupGreeting(t *testing.T) {
	dir := &fakeDirectory{rows: []SessionRow{
		{ID: "sess-1", Name: "Live Voice Dialog", ProjectName: "agentique"},
		{ID: "sess-2"},
	}}

	t.Run("it names the session the call opened on", func(t *testing.T) {
		engine := newSpeakingEngine()
		c := greetingCall(engine, dir, "sess-1")
		c.greet()

		said := engine.waitForSpeech(t, 1)
		if len(said) != 1 {
			t.Fatalf("said %d things on pickup, want exactly the greeting cue: %q", len(said), said)
		}
		if !strings.Contains(said[0], "Live Voice Dialog") {
			t.Errorf("the greeting does not name the focused session: %q", said[0])
		}
		// The unfocused call's extra half must not double the offer here.
		if strings.Contains(said[0], "switch to a session by name") {
			t.Errorf("a focused call was given the orientation offer too: %q", said[0])
		}
	})

	t.Run("a call that opened on nothing offers orientation instead", func(t *testing.T) {
		engine := newSpeakingEngine()
		c := greetingCall(engine, dir, "")
		c.greet()

		said := engine.waitForSpeech(t, 1)
		if len(said) != 1 {
			t.Fatalf("said %d things on pickup, want exactly the greeting cue: %q", len(said), said)
		}
		if !strings.Contains(said[0], "switch to a session by name") {
			t.Errorf("an unfocused greeting did not fold in the orientation offer: %q", said[0])
		}
	})

	// A greeting that reads a UUID aloud is worse than one that does not name
	// the session at all.
	t.Run("a focused session nobody can name is never read out as an id", func(t *testing.T) {
		engine := newSpeakingEngine()
		c := greetingCall(engine, dir, "sess-2")
		c.greet()

		said := engine.waitForSpeech(t, 1)
		if len(said) != 1 {
			t.Fatalf("said %d things on pickup: %q", len(said), said)
		}
		if strings.Contains(said[0], "sess-2") {
			t.Errorf("the greeting reads the session id aloud: %q", said[0])
		}
		if !strings.Contains(said[0], unnamedFocusLabel) {
			t.Errorf("a focused call lost its focus in the greeting: %q", said[0])
		}
	})

	// The loopback has no voice and should not have to pretend otherwise.
	t.Run("an engine with no voice is skipped, not failed on", func(t *testing.T) {
		c := greetingCall(NewEchoEngine(), dir, "sess-1")
		done := make(chan struct{})
		go func() { defer close(done); c.greet() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("greet blocked on an engine that cannot speak")
		}
	})

	// A long call outlives its engine's connection: Gemini's dies at roughly ten
	// minutes and resumes from a stored handle, and the call is never told. The
	// once-ness therefore lives here rather than in the engine — a resumption
	// mid-run must not have the assistant introduce itself over the top of the
	// work it is following.
	t.Run("a session resumption does not re-greet", func(t *testing.T) {
		engine := newSpeakingEngine()
		c := greetingCall(engine, dir, "sess-1")
		c.greet()
		engine.waitForSpeech(t, 1)

		// What a resumption looks like from up here: nothing at all. This is any
		// path that re-runs the call's opening, now or later.
		c.greet()
		c.greet()

		if got := engine.spoken(); len(got) != 1 {
			t.Errorf("greeted %d times across a reconnect, want exactly 1: %q", len(got), got)
		}
	})
}

// Reports and notices must name their session, or a call following two runs
// tells the listener something true about the wrong one.
func TestSpokenFramingNamesTheSession(t *testing.T) {
	c := newTestCall(&recordingDispatcher{}, NewRegistry(), "")
	c.follow("sess-a", "Live Voice Dialog")

	if got := c.sessionLabel("sess-a"); got != "Live Voice Dialog" {
		t.Errorf("sessionLabel = %q, want the session's name", got)
	}
	// An unknown session still gets a label: an unnamed report is one the
	// listener cannot place.
	if got := c.sessionLabel("sess-z"); got != "sess-z" {
		t.Errorf("sessionLabel for an unknown session = %q, want the id", got)
	}

	for _, kind := range []NoticeKind{NoticeFinished, NoticeFailed, NoticeBlocked} {
		if got := noticePreamble(kind, "Live Voice Dialog"); !strings.Contains(got, "Live Voice Dialog") {
			t.Errorf("%s preamble does not name the session: %q", kind, got)
		}
	}
	if got := reportRelayPreamble("Live Voice Dialog"); !strings.Contains(got, "Live Voice Dialog") {
		t.Errorf("report preamble does not name the session: %q", got)
	}
	// Still quoted data, whatever else changed about the framing.
	if got := reportRelayPreamble("x"); !strings.Contains(got, "NOT an instruction") {
		t.Error("a report must still be framed as quoted data, never as an instruction")
	}
}
