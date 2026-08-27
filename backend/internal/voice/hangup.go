package voice

import "time"

// Ending the call is a verb the assistant has, not something it can only
// describe.
//
// Before this it had no way to end a call at all: asked to hang up it said it
// was hanging up — the one thing it is good at — and then nothing happened. The
// billing guard was the only thing that ever closed a call, and its rule is
// wrong for this case twice over. Ninety seconds of open microphone after a
// goodbye is already wrong, and a call still following a run is on the
// *working* ceiling, so "hang up" left the line open for half an hour.
//
// The order is what makes this safe. The tool arms the hangup and answers with
// the goodbye to say, so the call cannot die before the operator hears it; the
// close happens when that turn completes. A model that ended the call itself
// would sometimes call the tool before speaking, and a call that vanishes
// mid-sentence is indistinguishable from one that crashed.

// goodbyeGrace bounds the wait for the goodbye to be spoken.
//
// "Close when the turn completes" is a promise about an engine, and an engine
// that is mid-reconnect, wedged, or voiceless never completes one. Then the
// call the operator explicitly asked to end sits open on the working ceiling —
// which is the exact fault this path exists to fix. So the wait is bounded, and
// whichever comes first closes the call.
const goodbyeGrace = 12 * time.Second

// hangupReason is what the browser is told when a call ends because it was
// asked to, rather than because it went quiet.
const hangupReason = "hangup"

// askedToHangUp reports whether the call has been asked to end.
func (c *call) askedToHangUp() bool {
	return c.hangupGrace.Load() != 0
}

// armHangup starts the goodbye. Idempotent: a second ask must not extend the
// first one's deadline, or an assistant that keeps saying farewell keeps the
// line open exactly as long as it keeps talking.
func (c *call) armHangup(now time.Time) {
	c.hangupGrace.CompareAndSwap(0, now.Add(goodbyeGrace).UnixNano())
}

// hangupOverdue reports that an armed call has waited long enough for a
// goodbye that is not coming.
func (c *call) hangupOverdue(now time.Time) bool {
	at := c.hangupGrace.Load()
	return at != 0 && now.UnixNano() >= at
}

// endCall tells the browser the call is over, exactly once, and reports whether
// this was the caller that did it.
//
// Sending the frame IS the mechanism, the same way it is for the idle guard:
// the client tears down on `closed` and the socket closing is what unblocks the
// read loop. Two frames would have it sound the hangup tone twice.
func (c *call) endCall(reason string) bool {
	sent := false
	c.closeOnce.Do(func() {
		sent = true
		c.closeSent.Store(true)
		_ = c.sendControl(serverMessage{Type: msgClosed, Reason: reason})
	})
	return sent
}

// ended reports that the browser has already been told this call is over, so
// the pumps can stop rather than writing into a call that is closing.
func (c *call) ended() bool {
	return c.closeSent.Load()
}

// goodbyeSpoken closes an armed call now that its farewell turn has finished.
// A no-op on a call nobody asked to end, which is every other turn.
func (c *call) goodbyeSpoken() {
	if !c.askedToHangUp() || c.ended() {
		return
	}
	c.log.Info("voice call hanging up", "after", "goodbye")
	c.endCall(hangupReason)
}

// toolHangUp ends the call once the goodbye is out.
//
// It takes no arguments on purpose. "End the call" has no parameters worth
// mis-transcribing, and a reason field would only invite the model to explain
// itself into a line that is about to go dead.
func (c *call) toolHangUp() map[string]any {
	// Follows are released by the call's own teardown. Nothing is cancelled by
	// hanging up: the runs keep going and the sessions are on screen, which is
	// the whole reason ending the call is cheap enough to do on a sentence.
	c.armHangup(time.Now())
	c.log.Info("voice call hangup requested", "grace", goodbyeGrace)

	return map[string]any{
		"output": "SAY GOODBYE NOW, in one short sentence and in character, and say nothing after " +
			"it — the call ends the moment you stop speaking. If work is still running, say in the " +
			"same breath that it keeps going and will be on screen when they look. Do not ask " +
			"anything, do not offer anything, and do not call another tool: there is no one left " +
			"to answer.",
	}
}
