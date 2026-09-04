package voice

import (
	"context"
	"testing"
	"time"
)

// bareGeminiEngine builds an engine with no client and no session — enough for
// the emit/lifecycle seams, which never touch the network.
func bareGeminiEngine(buffer int) *geminiEngine {
	ctx, cancel := context.WithCancel(context.Background())
	return &geminiEngine{
		log:    testLogger(),
		events: make(chan Event, buffer),
		ctx:    ctx,
		cancel: cancel,
	}
}

// A full buffer must never cost a tool call. The model is paused until every
// tool call is answered, so one dropped here is a call that dies mute — the
// consumer serializes behind browser-socket writes and fills the buffer in
// seconds of ordinary speech.
func TestEmitNeverDropsAToolCallBehindAFullBuffer(t *testing.T) {
	e := bareGeminiEngine(2)
	defer e.cancel()
	e.emit(AudioEvent{PCM: []byte{1}})
	e.emit(AudioEvent{PCM: []byte{2}})

	// Audio stays lossy: a third frame returns immediately rather than blocking
	// the receive loop behind a slow consumer.
	audioDone := make(chan struct{})
	go func() { e.emit(AudioEvent{PCM: []byte{3}}); close(audioDone) }()
	select {
	case <-audioDone:
	case <-time.After(2 * time.Second):
		t.Fatal("audio emit blocked on a full buffer; it must drop")
	}

	delivered := make(chan struct{})
	go func() { e.emit(ToolCallEvent{ID: "t1", Name: ToolRunPrompt}); close(delivered) }()
	select {
	case <-delivered:
		t.Fatal("tool-call emit returned against a full buffer — it was dropped")
	case <-time.After(50 * time.Millisecond):
		// Still waiting for the consumer, which is the point.
	}

	// The consumer drains, and the tool call arrives after the queued audio.
	<-e.events
	<-e.events
	select {
	case ev := <-e.events:
		if tc, ok := ev.(ToolCallEvent); !ok || tc.ID != "t1" {
			t.Fatalf("drained %T, want the queued ToolCallEvent", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tool call never arrived after the buffer drained")
	}
	<-delivered
}

// The receive loop's exit closes the event stream, whatever the exit path, so
// a consumer blocked on it ends with the engine instead of waiting forever on
// a corpse. Close afterwards must stay safe — the close moved out of it.
func TestReceiveLoopExitClosesTheEventStream(t *testing.T) {
	e := bareGeminiEngine(1)
	e.start() // no session: the loop exits at once, which must close events
	select {
	case _, ok := <-e.events:
		if ok {
			t.Fatal("expected a closed channel, got an event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events stayed open after the receive loop exited")
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close after loop exit: %v", err)
	}
}

// A blocked control emit is bounded by the engine's lifetime: a consumer that
// died must not wedge the receive loop forever.
func TestEmitControlUnblocksWhenTheEngineCloses(t *testing.T) {
	e := bareGeminiEngine(1)
	e.emit(TranscriptEvent{Text: "fill", Source: "engine"})

	done := make(chan struct{})
	go func() { e.emit(TurnCompleteEvent{}); close(done) }()
	e.cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("control emit stayed blocked past the engine's own end")
	}
}
