package voice

import (
	"context"
	"fmt"
	"strings"
)

// Delivery is how a dispatched prompt reached its session.
//
// It mirrors session.MessageDelivery rather than importing it, so this package
// stays independent of the session pipeline. The distinction is worth carrying
// all the way to speech: only the server knows which of the three happened, and
// "added that to what it's doing now" is a different sentence from "that'll go
// in after this finishes".
type Delivery string

const (
	// DeliveryTurn — the session was idle and the prompt opened a new turn.
	DeliveryTurn Delivery = "turn"
	// DeliveryMidTurn — injected into the turn already running.
	DeliveryMidTurn Delivery = "mid_turn"
	// DeliveryQueued — buffered, and starts as a fresh turn at the next idle
	// boundary.
	DeliveryQueued Delivery = "queued"
)

// Spoken renders a delivery as something to say out loud. A whole sentence,
// because it is sometimes the whole of what there is to say.
func (d Delivery) Spoken() string {
	switch d {
	case DeliveryMidTurn:
		return "Added to what it is already doing."
	case DeliveryQueued:
		return "Queued — it will start that when the current work finishes."
	case DeliveryTurn:
		return "Started — it is working on it now."
	default:
		return "Sent."
	}
}

// Clause is the same outcome as a fragment, for a sentence that has already
// named the session and the work. Present tense and definite: the operator is
// being told what *is* happening, not what may have been arranged.
func (d Delivery) Clause() string {
	switch d {
	case DeliveryMidTurn:
		return "it has been added to the work already running there and is being picked up now"
	case DeliveryQueued:
		return "it is queued and starts the moment the current work finishes"
	case DeliveryTurn:
		return "it has started and is running now"
	default:
		return "it has been sent"
	}
}

// Confirmation is what the assistant must say the instant a prompt lands.
//
// The read-back before the send is a question, and a question answered with
// silence is indistinguishable from one that was never heard. Everything past
// the operator's yes is invisible to someone in a car: the dispatch card lands
// on a screen they are not looking at, and a run makes no sound of its own. The
// "silence is fine" rule covers the minutes *while* work runs; it must not
// swallow the second after a yes, which is the one moment the listener has no
// way to tell a send from a misheard sentence.
//
// So the tool answers with a sentence to say rather than a status to interpret,
// and it is a statement, not another question — the consent gate is behind us
// and asking again here would read as the send not having happened.
//
// session is what to call the target out loud; it is never an id ([displayFor]
// guarantees that), and an empty one degrades to the focus rather than being
// read as a blank.
func (d Delivery) Confirmation(session string) string {
	target := "the session you are focused on"
	if named := strings.TrimSpace(session); named != "" {
		target = named
	}
	return fmt.Sprintf("SAY THIS OUT LOUD NOW, before anything else and as one sentence: that it "+
		"has gone to %s, what you sent in a few words of your own, and that %s. State it, do not "+
		"ask it — they already said yes, and asking again sounds like it did not go. Do not go "+
		"quiet here: they cannot see the screen, so an unconfirmed send is indistinguishable from "+
		"a request you never heard. Then stop and wait.", target, d.Clause())
}

// Dispatcher hands a drafted prompt to the session that does the work.
//
// The voice agent never runs anything itself: it produces text and this sends
// it down the same path the composer's send button uses. One route into the
// session pipeline, whether the gesture was a click or a sentence.
type Dispatcher interface {
	// Dispatch delivers prompt to sessionID and reports which of the three
	// outcomes happened. It must return promptly — the speech model is paused
	// waiting on the tool response, and a long stall is audible as dead air.
	//
	// withReporting appends the instruction that teaches the worker to report
	// progress back. It is true only when someone is actually staying on the
	// line: a run nobody is listening to should carry no reporting overhead at
	// all, which is the whole reason the handoff asks.
	Dispatch(ctx context.Context, sessionID, prompt string, withReporting bool) (Delivery, error)

	// ProjectContext is what the drafter should know about the work before it
	// starts asking questions: enough to name files and ask sharp questions,
	// and no more.
	//
	// It is deliberately a summary rather than the file tree and full history.
	// Everything handed over goes to the speech vendor on every call, and a
	// drafter that knows the repository's shape asks better questions than one
	// that has been given all of it.
	//
	// Returning "" is valid and yields a generic but correctly-shaped drafter.
	ProjectContext(ctx context.Context, sessionID string) string

	// AutoRunnable reports whether sessionID runs without stopping for
	// approval, and if not, why.
	//
	// Live voice has no spoken approval — approving a command you cannot see,
	// through a transcription, is not consent — so a session that would stop
	// and ask is refused at the handoff rather than silently stalling with the
	// call sounding fine.
	AutoRunnable(ctx context.Context, sessionID string) (bool, string, error)
}
