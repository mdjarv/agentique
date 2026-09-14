package voice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"google.golang.org/genai"
)

// dictationInstruction is the whole of what the dictation session's model is
// told. It does not stop the model replying — the probe showed it answers
// anyway (TestDictationProbeLive) — so the transcriber drops every reply, and
// this only keeps those replies short.
const dictationInstruction = "You are a transcription pipe. Never speak, never answer, never respond to anything you hear."

// geminiTranscriber is dictation over a Gemini Live session.
//
// Automatic activity detection is off and the client marks each utterance,
// because that is the only configuration the probe found that is both reliable
// and prompt: with no utterance marks nothing is ever transcribed, and with the
// server's own detection the second sentence of a dictation was sometimes never
// returned. Each utterance's transcript arrives about 0.3s after it is closed.
//
// There is no resumption. A Live connection ends at about ten minutes, which is
// a long dictation; the session reports that as a fatal error and the client
// says so, rather than carrying reconnect machinery for a case a person can
// answer by pressing the mic again.
type geminiTranscriber struct {
	events chan Event
	log    *slog.Logger

	sendMu  sync.Mutex
	session *genai.Session

	closeOnce sync.Once
	closing   chan struct{}
}

func newGeminiTranscriber(ctx context.Context, opts Options, log *slog.Logger) (*geminiTranscriber, error) {
	client, err := newGenaiClient(ctx, opts)
	if err != nil {
		return nil, err
	}
	model := firstNonEmptyString(opts.Model, defaultModel)
	session, err := client.Live.Connect(ctx, model, dictationConfig())
	if err != nil {
		return nil, fmt.Errorf("gemini live connect: %w", err)
	}
	tr := &geminiTranscriber{
		events:  make(chan Event, geminiEventBuffer),
		log:     log,
		session: session,
		closing: make(chan struct{}),
	}
	go tr.receiveLoop()
	return tr, nil
}

// dictationConfig is the session a dictation needs: the caller's words back as
// text, and nothing a call has — no tools, no voice, no transcript of replies.
func dictationConfig() *genai.LiveConnectConfig {
	return &genai.LiveConnectConfig{
		// Native-audio Live models accept no other modality; the audio they
		// answer with is discarded in handle.
		ResponseModalities:      []genai.Modality{genai.ModalityAudio},
		InputAudioTranscription: &genai.AudioTranscriptionConfig{},
		RealtimeInputConfig: &genai.RealtimeInputConfig{
			AutomaticActivityDetection: &genai.AutomaticActivityDetection{Disabled: true},
		},
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: dictationInstruction}}},
	}
}

func (g *geminiTranscriber) Send(ctx context.Context, pcm []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	frame := make([]byte, len(pcm))
	copy(frame, pcm)
	return g.send(genai.LiveRealtimeInput{Audio: &genai.Blob{
		Data:     frame,
		MIMEType: fmt.Sprintf("audio/pcm;rate=%d", InputSampleRate),
	}})
}

func (g *geminiTranscriber) BeginUtterance() error {
	return g.send(genai.LiveRealtimeInput{ActivityStart: &genai.ActivityStart{}})
}

func (g *geminiTranscriber) EndUtterance() error {
	return g.send(genai.LiveRealtimeInput{ActivityEnd: &genai.ActivityEnd{}})
}

// send serialises writes: the transport underneath rejects concurrent ones.
func (g *geminiTranscriber) send(input genai.LiveRealtimeInput) error {
	g.sendMu.Lock()
	defer g.sendMu.Unlock()
	select {
	case <-g.closing:
		return nil // closing; a dropped frame is the correct outcome
	default:
	}
	if err := g.session.SendRealtimeInput(input); err != nil {
		return fmt.Errorf("gemini send: %w", err)
	}
	return nil
}

func (g *geminiTranscriber) Events() <-chan Event { return g.events }

func (g *geminiTranscriber) Close() error {
	var err error
	g.closeOnce.Do(func() {
		close(g.closing)
		g.sendMu.Lock()
		err = g.session.Close()
		g.sendMu.Unlock()
	})
	return err
}

// receiveLoop owns the event channel's close, so every exit ends the reader.
func (g *geminiTranscriber) receiveLoop() {
	defer close(g.events)
	for {
		msg, err := g.session.Receive()
		if err != nil {
			select {
			case <-g.closing:
			default:
				g.emit(ErrorEvent{Err: fmt.Errorf("gemini receive: %w", err), Fatal: true})
			}
			return
		}
		if msg == nil {
			continue
		}
		if msg.GoAway != nil {
			g.emit(ErrorEvent{Err: errDictationSessionLimit, Fatal: true})
			_ = g.Close()
			return
		}
		if c := msg.ServerContent; c != nil && c.InputTranscription != nil && c.InputTranscription.Text != "" {
			g.emit(TranscriptEvent{Text: c.InputTranscription.Text, Source: transcriptSourceCaller, Final: true})
		}
		// Everything else — the reply audio the instruction does not prevent,
		// turn and interruption markers — is not dictation.
	}
}

// emit never drops a transcript: losing words silently is the one failure a
// dictation cannot have. The buffer is far deeper than a person speaks.
func (g *geminiTranscriber) emit(ev Event) {
	select {
	case g.events <- ev:
	case <-g.closing:
	}
}

// errDictationSessionLimit is the service ending a dictation that has run as
// long as one connection may.
var errDictationSessionLimit = errors.New("the dictation session reached its time limit")
