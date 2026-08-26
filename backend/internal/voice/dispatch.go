package voice

import "context"

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

// Spoken renders a delivery as something to say out loud.
func (d Delivery) Spoken() string {
	switch d {
	case DeliveryMidTurn:
		return "Added to what it is already doing."
	case DeliveryQueued:
		return "Queued — it will start that when the current work finishes."
	case DeliveryTurn:
		return "Started."
	default:
		return "Sent."
	}
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
