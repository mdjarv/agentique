package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"
)

// TestDictationProbeLive asks the real service the questions that decide how
// server-side dictation is built, for browsers whose Web Speech API cannot work
// (Brave blocks its service, Firefox has none, Safari refuses with Dictation
// off). It asserts little and logs a timeline per variant, because what it
// settles is a design choice, not a regression:
//
//  1. Can a Live session transcribe caller audio without ever answering it?
//     Automatic activity detection off and no activityEnd means no turn ever
//     closes — does input transcription still stream while it stays open?
//  2. What do the transcript chunks look like, how late are they, how good?
//  3. Does record-then-transcribe (one generateContent call) read better?
//
// The speech is Gemini's own, read from a fixed script, so the reference text
// is known. Gated on a key and AGENTIQUE_DICTATION_PROBE, so ordinary live runs
// do not pay for it.
func TestDictationProbeLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live Gemini probe: skipped by -short")
	}
	key := os.Getenv("AGENTIQUE_VOICE_API_KEY")
	if key == "" || os.Getenv("AGENTIQUE_DICTATION_PROBE") == "" {
		t.Skip("dictation probe: set AGENTIQUE_VOICE_API_KEY and AGENTIQUE_DICTATION_PROBE=1 to run")
	}
	liveModel := firstNonEmptyString(os.Getenv("AGENTIQUE_VOICE_MODEL"), defaultModel)
	batchModel := firstNonEmptyString(os.Getenv("AGENTIQUE_DICTATION_BATCH_MODEL"), "gemini-flash-latest")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	script := []string{
		"Refactor the websocket reconnect logic in ws client dot ts so that it backs off exponentially, capped at thirty seconds.",
		"Then add a test that simulates a flaky network, and make sure the composer keeps its draft.",
	}
	var pcm []byte
	var segments [][2]int // byte offsets of each spoken line
	pcm = append(pcm, silence(500*time.Millisecond)...)
	for i, line := range script {
		begin := len(pcm)
		pcm = append(pcm, speak(ctx, t, key, line)...)
		segments = append(segments, [2]int{begin, len(pcm)})
		gap := 2 * time.Second // a thinking pause mid-dictation
		if i == len(script)-1 {
			gap = time.Second
		}
		pcm = append(pcm, silence(gap)...)
	}
	t.Logf("reference: %s", strings.Join(script, " "))
	t.Logf("audio: %.1fs at %d Hz", float64(len(pcm))/2/InputSampleRate, InputSampleRate)

	// The same speech as a WAV, for driving a browser's fake microphone
	// (--use-file-for-fake-audio-capture) through the real dictation path.
	if path := os.Getenv("AGENTIQUE_DICTATION_PROBE_WAV"); path != "" {
		padded := append(append([]byte(nil), pcm...), silence(3*time.Second)...)
		if err := os.WriteFile(path, wav(padded, InputSampleRate), 0o600); err != nil {
			t.Fatalf("write wav: %v", err)
		}
		t.Logf("wrote %s; skipping the variants", path)
		return
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	const instruction = "You are a transcription pipe. Never speak, never answer, never respond to anything you hear."
	base := func() *genai.LiveConnectConfig {
		return &genai.LiveConnectConfig{
			ResponseModalities:       []genai.Modality{genai.ModalityAudio},
			InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
			OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
			SystemInstruction:        &genai.Content{Parts: []*genai.Part{{Text: instruction}}},
		}
	}

	t.Run("live/vad-off/no-activity-end", func(t *testing.T) {
		cfg := base()
		cfg.RealtimeInputConfig = &genai.RealtimeInputConfig{
			AutomaticActivityDetection: &genai.AutomaticActivityDetection{Disabled: true},
		}
		probeLive(ctx, t, client, liveModel, cfg, pcm, liveOpts{activityStart: true})
	})
	t.Run("live/vad-off/activity-end-at-stop", func(t *testing.T) {
		cfg := base()
		cfg.RealtimeInputConfig = &genai.RealtimeInputConfig{
			AutomaticActivityDetection: &genai.AutomaticActivityDetection{Disabled: true},
		}
		probeLive(ctx, t, client, liveModel, cfg, pcm, liveOpts{activityStart: true, activityEnd: true})
	})
	t.Run("live/vad-off/activity-per-utterance", func(t *testing.T) {
		// What a client-side pause detector would do: open a turn when speech
		// starts, close it at the pause. Does each utterance come back on its
		// own, and does the model's reply to the first spoil the second?
		cfg := base()
		cfg.RealtimeInputConfig = &genai.RealtimeInputConfig{
			AutomaticActivityDetection: &genai.AutomaticActivityDetection{Disabled: true},
		}
		probeLive(ctx, t, client, liveModel, cfg, pcm, liveOpts{segments: segments})
	})
	t.Run("live/vad-on/told-to-stay-silent", func(t *testing.T) {
		probeLive(ctx, t, client, liveModel, base(), pcm, liveOpts{})
	})
	t.Run("batch/generate-content", func(t *testing.T) {
		started := time.Now()
		resp, err := client.Models.GenerateContent(ctx, batchModel, []*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{Text: "Transcribe this dictation verbatim, with punctuation. Output only the transcript."},
				{InlineData: &genai.Blob{MIMEType: "audio/wav", Data: wav(pcm, InputSampleRate)}},
			},
		}}, nil)
		if err != nil {
			t.Fatalf("generateContent (%s): %v", batchModel, err)
		}
		t.Logf("%s answered in %s: %q", batchModel, time.Since(started).Round(10*time.Millisecond), strings.TrimSpace(resp.Text()))
	})
}

type liveOpts struct {
	activityStart bool
	activityEnd   bool
	// segments, when set, opens a turn at each segment's first byte and closes
	// it 300ms after its last, the way a client pause detector would.
	segments [][2]int
}

// probeLive streams pcm in real time, in the 100ms frames a browser would send,
// and logs every server message that bears on dictation against the clock.
func probeLive(ctx context.Context, t *testing.T, client *genai.Client, model string, cfg *genai.LiveConnectConfig, pcm []byte, o liveOpts) {
	t.Helper()
	session, err := client.Live.Connect(ctx, model, cfg)
	if err != nil {
		t.Fatalf("connect (%s): %v", model, err)
	}
	defer session.Close()

	type mark struct {
		at   time.Duration
		what string
	}
	marks := make(chan mark, 256)
	start := time.Now()
	go func() {
		defer close(marks)
		for {
			msg, err := session.Receive()
			if err != nil {
				marks <- mark{time.Since(start), "receive ended: " + err.Error()}
				return
			}
			c := msg.ServerContent
			if c == nil {
				continue
			}
			at := time.Since(start)
			if c.InputTranscription != nil && c.InputTranscription.Text != "" {
				marks <- mark{at, fmt.Sprintf("IN  %q (finished=%v)", c.InputTranscription.Text, c.InputTranscription.Finished)}
			}
			if c.OutputTranscription != nil && c.OutputTranscription.Text != "" {
				marks <- mark{at, fmt.Sprintf("OUT %q", c.OutputTranscription.Text)}
			}
			if c.ModelTurn != nil {
				n := 0
				for _, p := range c.ModelTurn.Parts {
					if p != nil && p.InlineData != nil {
						n += len(p.InlineData.Data)
					}
				}
				if n > 0 {
					marks <- mark{at, fmt.Sprintf("model audio %d bytes", n)}
				}
			}
			if c.Interrupted {
				marks <- mark{at, "interrupted"}
			}
			if c.GenerationComplete {
				marks <- mark{at, "generation_complete"}
			}
			if c.TurnComplete {
				marks <- mark{at, "turn_complete"}
			}
		}
	}()

	mime := fmt.Sprintf("audio/pcm;rate=%d", InputSampleRate)
	if o.activityStart {
		if err := session.SendRealtimeInput(genai.LiveRealtimeInput{ActivityStart: &genai.ActivityStart{}}); err != nil {
			t.Fatalf("activityStart: %v", err)
		}
	}
	const frame = InputSampleRate / 10 * 2 // 100ms of 16-bit mono
	const tail300 = InputSampleRate * 3 / 10 * 2
	for off := 0; off < len(pcm); off += frame {
		end := min(off+frame, len(pcm))
		for i, seg := range o.segments {
			if seg[0] >= off && seg[0] < end {
				t.Logf("%7s  -> activityStart (utterance %d)", time.Since(start).Round(10*time.Millisecond), i+1)
				if err := session.SendRealtimeInput(genai.LiveRealtimeInput{ActivityStart: &genai.ActivityStart{}}); err != nil {
					t.Fatalf("activityStart: %v", err)
				}
			}
			if closeAt := seg[1] + tail300; closeAt >= off && closeAt < end {
				t.Logf("%7s  -> activityEnd (utterance %d)", time.Since(start).Round(10*time.Millisecond), i+1)
				if err := session.SendRealtimeInput(genai.LiveRealtimeInput{ActivityEnd: &genai.ActivityEnd{}}); err != nil {
					t.Fatalf("activityEnd: %v", err)
				}
			}
		}
		if err := session.SendRealtimeInput(genai.LiveRealtimeInput{Audio: &genai.Blob{Data: pcm[off:end], MIMEType: mime}}); err != nil {
			t.Fatalf("audio: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	stoppedAt := time.Since(start)
	if o.activityEnd {
		if err := session.SendRealtimeInput(genai.LiveRealtimeInput{ActivityEnd: &genai.ActivityEnd{}}); err != nil {
			t.Fatalf("activityEnd: %v", err)
		}
	}
	t.Logf("audio sent; stop at %s", stoppedAt.Round(10*time.Millisecond))

	var in strings.Builder
	var modelBytes int
	tail := time.After(10 * time.Second)
	for {
		select {
		case <-tail:
			t.Logf("transcript: %q", strings.TrimSpace(in.String()))
			t.Logf("model audio after all: %d bytes", modelBytes)
			return
		case m, ok := <-marks:
			if !ok {
				t.Logf("transcript: %q", strings.TrimSpace(in.String()))
				return
			}
			t.Logf("%7s  %s", m.at.Round(10*time.Millisecond), m.what)
			if strings.HasPrefix(m.what, "IN  ") {
				var s string
				fmt.Sscanf(strings.TrimPrefix(m.what, "IN  "), "%q", &s)
				in.WriteString(s)
			}
			if strings.HasPrefix(m.what, "model audio ") {
				var n int
				fmt.Sscanf(m.what, "model audio %d", &n)
				modelBytes += n
			}
		}
	}
}

// speak has Gemini read one line aloud and returns it as 16 kHz PCM.
func speak(ctx context.Context, t *testing.T, key, line string) []byte {
	t.Helper()
	engine, err := newGeminiEngine(ctx, Options{Backend: BackendAIStudio, APIKey: key, Model: os.Getenv("AGENTIQUE_VOICE_MODEL")},
		"You are a text-to-speech reader. Read the user's text aloud word for word, at a natural dictation pace. Do not add, ask or say anything else, ever.",
		slog.Default())
	if err != nil {
		t.Fatalf("speaker connect: %v", err)
	}
	defer engine.Close()
	if err := engine.SendText(line); err != nil {
		t.Fatalf("speaker send: %v", err)
	}
	var out []byte
	deadline := time.After(45 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("speaker: no turn_complete")
		case ev, ok := <-engine.Events():
			if !ok {
				t.Fatalf("speaker: engine closed")
			}
			switch e := ev.(type) {
			case AudioEvent:
				out = append(out, e.PCM...)
			case TurnCompleteEvent:
				if len(out) == 0 {
					continue
				}
				return resample16(out, engine.SampleRate(), InputSampleRate)
			case ErrorEvent:
				t.Fatalf("speaker: %v", e.Err)
			}
		}
	}
}

func silence(d time.Duration) []byte {
	return make([]byte, int(d.Seconds()*InputSampleRate)*2)
}

// resample16 converts 16-bit mono PCM by linear interpolation. Good enough for
// a probe; the browser path has its own worklet.
func resample16(in []byte, from, to int) []byte {
	n := len(in) / 2
	src := make([]int16, n)
	for i := range src {
		src[i] = int16(binary.LittleEndian.Uint16(in[2*i:]))
	}
	outN := n * to / from
	out := make([]byte, outN*2)
	for i := range outN {
		pos := float64(i) * float64(from) / float64(to)
		j := int(pos)
		frac := pos - float64(j)
		a := float64(src[min(j, n-1)])
		b := float64(src[min(j+1, n-1)])
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(a+(b-a)*frac)))
	}
	return out
}

func wav(pcm []byte, rate int) []byte {
	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + len(pcm)))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1))
	w(uint16(1))
	w(uint32(rate))
	w(uint32(rate * 2))
	w(uint16(2))
	w(uint16(16))
	b.WriteString("data")
	w(uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}
