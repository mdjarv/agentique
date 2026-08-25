package voice

import (
	"context"
	"sync"
)

// echoBufferFrames bounds how many caller frames may be in flight back to the
// browser. At the batching the client uses (~32ms per frame) this is roughly a
// second of audio, which is enough to ride out a scheduling hiccup and short
// enough that a stalled reader is dropped rather than accumulating latency
// nobody can hear past.
const echoBufferFrames = 32

// EchoEngine returns caller audio unchanged.
//
// It is the P3 verification engine: with it, the socket exercises capture,
// batching, framing, upload, download and playback scheduling without any
// credentials or any model. If audio is wrong here it is wrong in the browser,
// which is a much smaller place to look than a live model session.
type EchoEngine struct {
	events chan Event

	mu     sync.Mutex
	closed bool
}

// NewEchoEngine returns a started loopback engine.
func NewEchoEngine() *EchoEngine {
	return &EchoEngine{events: make(chan Event, echoBufferFrames)}
}

// Send echoes one frame back on the event stream.
//
// A full buffer drops the frame rather than blocking. Dropping is correct for
// real-time audio: the caller is a microphone that will not wait, so a blocked
// Send would stall capture and turn a transient reader stall into permanent
// added latency.
func (e *EchoEngine) Send(ctx context.Context, pcm []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}

	// Copy: the read loop reuses its frame buffer once Send returns.
	frame := make([]byte, len(pcm))
	copy(frame, pcm)

	select {
	case e.events <- AudioEvent{PCM: frame}:
	default:
	}
	return nil
}

// Events implements [Engine].
func (e *EchoEngine) Events() <-chan Event { return e.events }

// SampleRate reports the input rate, since a loopback does not resample.
// Announcing it on the wire rather than assuming the model's output rate is
// what lets the same client code play both.
func (e *EchoEngine) SampleRate() int { return InputSampleRate }

// Close implements [Engine]. Idempotent.
func (e *EchoEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	close(e.events)
	return nil
}
