// Package voice carries live spoken dialog between a browser and a realtime
// speech engine.
//
// The socket is deliberately separate from the main /ws control plane. That one
// is JSON in both directions (ReadJSON/WriteJSON), so a binary audio frame
// arriving on it fails to decode and closes the connection for every other
// subscription riding it — sessions, projects, the activity wire. Audio gets its
// own endpoint so a voice fault can never take down the control plane.
//
// The engine is the seam. An [Engine] is anything that accepts caller audio and
// emits [Event]s; [EchoEngine] implements it as a loopback so the browser audio
// path can be verified end to end before any model is involved, and a real
// speech backend is another implementation rather than a change to the
// transport.
package voice

import (
	"context"
	"fmt"
)

const (
	// InputSampleRate is the rate the browser captures and uploads at, in Hz.
	// Fixed by the realtime speech API contract, not a preference.
	InputSampleRate = 16000

	// OutputSampleRate is the rate a speech model returns audio at, in Hz.
	// It differs from the input rate, which is why the browser runs two
	// AudioContexts and why the rate is announced on the wire rather than
	// compiled into the client.
	OutputSampleRate = 24000
)

// Event is what an [Engine] emits. The union is sealed: a type switch over it
// must carry a default case, and a new outcome has to be added here rather than
// arriving as an untyped surprise at a call site.
type Event interface {
	isVoiceEvent()
}

// AudioEvent carries one frame of engine speech, as Int16 PCM, little-endian,
// mono, at the engine's output rate.
type AudioEvent struct {
	PCM []byte
}

// TurnCompleteEvent marks the end of an engine turn. Interrupted distinguishes
// the caller talking over the engine from the engine finishing its sentence.
//
// Both cases must reach the browser, because both mean "stop playing what is
// queued". A barge-in that does not flush the playback queue leaves several
// seconds of stale speech playing over the person who interrupted.
type TurnCompleteEvent struct {
	Interrupted bool
}

// TranscriptEvent carries recognised text. Source is "caller" or "engine";
// Final distinguishes a settled transcript from an in-flight guess.
type TranscriptEvent struct {
	Text   string
	Final  bool
	Source string
}

// ErrorEvent reports an engine failure. A fatal error means the engine is done
// and its channel is about to close.
type ErrorEvent struct {
	Err   error
	Fatal bool
}

func (AudioEvent) isVoiceEvent()        {}
func (TurnCompleteEvent) isVoiceEvent() {}
func (TranscriptEvent) isVoiceEvent()   {}
func (ErrorEvent) isVoiceEvent()        {}

// Engine is a realtime speech backend: caller audio in, [Event]s out.
//
// Implementations own their own lifetime. Events is closed when the engine has
// finished, which is the signal the call loop waits on; Close is idempotent and
// safe to call from another goroutine than the one reading Events.
type Engine interface {
	// Send hands one frame of caller audio to the engine. It must not block on
	// the network for long: the caller is a real-time audio loop, and a stalled
	// Send drops microphone frames rather than queueing them.
	Send(ctx context.Context, pcm []byte) error

	// Events is the engine's output stream, closed when the engine ends.
	Events() <-chan Event

	// SampleRate reports the rate of the audio in [AudioEvent], in Hz.
	SampleRate() int

	// Close releases the engine. Safe to call more than once.
	Close() error
}

// Backend names a speech transport. The two differ in credentials and data
// terms rather than protocol, so the rest of the package is written against
// [Engine] and neither name appears below this file.
type Backend string

const (
	// BackendEcho loops caller audio straight back. No credentials, no network,
	// no model — it exists to prove the browser audio path.
	BackendEcho Backend = "echo"
	// BackendAIStudio authenticates with an API key from Google AI Studio.
	BackendAIStudio Backend = "aistudio"
	// BackendVertex authenticates with a Google Cloud project and application
	// default credentials.
	BackendVertex Backend = "vertex"
)

// ParseBackend resolves a configured backend name. An empty name means
// aistudio, matching the documented [voice] default.
func ParseBackend(name string) (Backend, error) {
	switch Backend(name) {
	case "":
		return BackendAIStudio, nil
	case BackendEcho:
		return BackendEcho, nil
	case BackendAIStudio:
		return BackendAIStudio, nil
	case BackendVertex:
		return BackendVertex, nil
	default:
		return "", fmt.Errorf("unknown voice backend %q: want echo, aistudio or vertex", name)
	}
}
