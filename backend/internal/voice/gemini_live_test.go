package voice

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestGeminiEngineLive talks to the real Live API.
//
// It is skipped by -short and without a key, matching how this repo gates its
// other test that needs a live provider. It exists because the parts most
// likely to be wrong here — the API version, the model id, the audio MIME type,
// the shape of a server message — cannot be verified by reasoning, only by
// asking the service.
func TestGeminiEngineLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live Gemini test: skipped by -short")
	}
	key := os.Getenv("AGENTIQUE_VOICE_API_KEY")
	if key == "" {
		t.Skip("live Gemini test: set AGENTIQUE_VOICE_API_KEY to run")
	}

	opts := Options{
		Backend:           BackendAIStudio,
		APIKey:            key,
		Model:             os.Getenv("AGENTIQUE_VOICE_MODEL"),
		SystemInstruction: "You are terse. Answer in one short sentence.",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	engine, err := newGeminiEngine(ctx, opts, slog.Default())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer engine.Close()

	if got := engine.SampleRate(); got != OutputSampleRate {
		t.Errorf("SampleRate() = %d, want %d", got, OutputSampleRate)
	}

	if err := engine.SendText("Say the word hello and nothing else."); err != nil {
		t.Fatalf("send text: %v", err)
	}

	var (
		audioFrames int
		audioBytes  int
		transcript  string
		turnDone    bool
	)
	deadline := time.After(45 * time.Second)

collect:
	for {
		select {
		case <-deadline:
			break collect
		case ev, ok := <-engine.Events():
			if !ok {
				break collect
			}
			switch e := ev.(type) {
			case AudioEvent:
				audioFrames++
				audioBytes += len(e.PCM)
			case TranscriptEvent:
				if e.Source == "engine" {
					transcript += e.Text
				}
			case TurnCompleteEvent:
				turnDone = true
				break collect
			case ErrorEvent:
				t.Fatalf("engine error: %v (fatal=%v)", e.Err, e.Fatal)
			default:
				t.Fatalf("unexpected event %T", ev)
			}
		}
	}

	if audioFrames == 0 {
		t.Error("no audio came back — the response modality or MIME type is wrong")
	}
	if !turnDone {
		t.Error("no turn_complete — the browser would never flush its playback queue")
	}
	t.Logf("audio: %d frames / %d bytes; transcript: %q", audioFrames, audioBytes, transcript)
}
