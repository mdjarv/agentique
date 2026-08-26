package voice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/genai"
)

// Live session tuning. These numbers are load-bearing rather than taste.
const (
	// prefixPaddingMs is how much detected speech must accumulate before the
	// model treats it as the caller starting to talk. Short, because this is
	// what barge-in latency is made of.
	prefixPaddingMs int32 = 50
	// silenceDurationMs is how long a gap must be before the caller's turn is
	// considered over. Long enough to think mid-sentence without being cut off.
	silenceDurationMs int32 = 800

	// Context window compression keeps a call alive past the point an
	// uncompressed audio session dies (about 15 minutes).
	compressionTrigger int64 = 104857
	compressionTarget  int64 = 52428

	// geminiEventBuffer is how many outbound events may queue before frames are
	// dropped. Same reasoning as the echo engine: the listener is real time.
	geminiEventBuffer = 64
)

// defaultModel is used when [voice] model is unset.
//
// It is a fallback, not a pin. The model id belongs in config because a new
// upstream release must not require an agentique release, and because the two
// backends do not carry identical ids.
const defaultModel = "models/gemini-3.1-flash-live-preview"

// geminiEngine is a live speech session against Gemini.
//
// Two things shape the implementation. The connection drops at roughly ten
// minutes regardless of session length, so resumption is not an optimisation —
// a call that spans a coding run hits that limit every time, and the browser
// must not notice. And gorilla/websocket underneath rejects concurrent writes,
// so every send is serialised behind sendMu, which also guards the session
// pointer because reconnect swaps it.
type geminiEngine struct {
	client *genai.Client
	model  string
	config *genai.LiveConnectConfig
	log    *slog.Logger

	events chan Event

	ctx    context.Context
	cancel context.CancelFunc

	// sendMu guards session, both because the transport rejects concurrent
	// writes and because reconnect replaces the pointer under it.
	sendMu  sync.Mutex
	session *genai.Session

	// handleMu guards the resumption handle, written by the receive loop and
	// read by reconnect.
	handleMu     sync.Mutex
	resumeHandle string

	// lastSpeechMu guards the VAD clock backing [SpeechIdler].
	lastSpeechMu sync.Mutex
	lastSpeech   time.Time

	closeOnce sync.Once
	wg        sync.WaitGroup
}

// newGeminiEngine connects a live session and starts its receive loop.
func newGeminiEngine(ctx context.Context, opts Options, systemInstruction string, log *slog.Logger) (*geminiEngine, error) {
	clientCfg := &genai.ClientConfig{
		// v1alpha, because session resumption is absent from v1beta — and
		// without resumption a call cannot outlive ten minutes.
		HTTPOptions: genai.HTTPOptions{APIVersion: "v1alpha"},
	}
	switch opts.Backend {
	case BackendAIStudio:
		clientCfg.Backend = genai.BackendGeminiAPI
		clientCfg.APIKey = opts.APIKey
	case BackendVertex:
		clientCfg.Backend = genai.BackendVertexAI
		clientCfg.Project = opts.Project
		clientCfg.Location = opts.Location
	default:
		return nil, fmt.Errorf("backend %q is not a speech backend", opts.Backend)
	}

	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("gemini client: %w", err)
	}

	// The persona's model wins over config: it is the more specific choice, and
	// it is the one somebody just made in the settings page.
	model := firstNonEmptyString(opts.Persona.Model, opts.Model, defaultModel)

	engineCtx, cancel := context.WithCancel(ctx)
	e := &geminiEngine{
		client: client,
		model:  model,
		config: liveConfig(systemInstruction, opts.Persona),
		log:    log,
		events: make(chan Event, geminiEventBuffer),
		ctx:    engineCtx,
		cancel: cancel,
	}

	session, err := client.Live.Connect(engineCtx, model, e.config)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gemini live connect: %w", err)
	}
	e.session = session

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.receiveLoop()
	}()
	return e, nil
}

// liveConfig builds the session configuration.
func liveConfig(systemInstruction string, persona Persona) *genai.LiveConnectConfig {
	trigger, target := compressionTrigger, compressionTarget
	cfg := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},

		// Transcription both ways costs nothing extra and is what lets the UI
		// show what was heard and said.
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},

		RealtimeInputConfig: &genai.RealtimeInputConfig{
			AutomaticActivityDetection: &genai.AutomaticActivityDetection{
				// Low sensitivity both ways: a car is a noisy room, and a false
				// start-of-speech interrupts the agent mid-sentence.
				StartOfSpeechSensitivity: genai.StartSensitivityLow,
				EndOfSpeechSensitivity:   genai.EndSensitivityLow,
				PrefixPaddingMs:          &[]int32{prefixPaddingMs}[0],
				SilenceDurationMs:        &[]int32{silenceDurationMs}[0],
			},
			ActivityHandling: genai.ActivityHandlingStartOfActivityInterrupts,
		},

		// Without this a call dies at about fifteen minutes of audio.
		ContextWindowCompression: &genai.ContextWindowCompressionConfig{
			TriggerTokens: &trigger,
			SlidingWindow: &genai.SlidingWindow{TargetTokens: &target},
		},

		// An empty handle asks the server to start issuing them; a handle is
		// supplied on reconnect.
		SessionResumption: &genai.SessionResumptionConfig{},

		// The assistant's tools. Four of them look — list, find, focus,
		// summarise — and exactly one starts work, down the same path the
		// composer's send button uses. The speech model never runs anything
		// itself.
		Tools: []*genai.Tool{{FunctionDeclarations: toolDeclarations()}},
	}
	// The chosen voice is the audible half of the persona; the instruction is
	// the other. An empty name leaves the backend's default rather than
	// guessing at a value that may not exist upstream.
	if persona.VoiceName != "" {
		cfg.SpeechConfig = &genai.SpeechConfig{
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: persona.VoiceName},
			},
		}
	}
	if systemInstruction != "" {
		cfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: systemInstruction}},
		}
	}
	return cfg
}

// Send forwards one frame of caller audio.
func (e *geminiEngine) Send(ctx context.Context, pcm []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.ctx.Err(); err != nil {
		return nil // engine closing; dropping the frame is correct
	}

	frame := make([]byte, len(pcm))
	copy(frame, pcm)

	e.sendMu.Lock()
	defer e.sendMu.Unlock()
	if e.session == nil {
		// Mid-reconnect, or closing. Dropping is correct: audio queued across a
		// reconnect would play late, and late audio is worse than none.
		return nil
	}
	return e.session.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			Data: frame,
			// The rate is part of the MIME type here, not a separate field.
			MIMEType: fmt.Sprintf("audio/pcm;rate=%d", InputSampleRate),
		},
	})
}

// SendText injects text into the conversation as if the caller had said it.
//
// This is how a progress report reaches the listener: it is relayed as content
// for the model to speak, never as an instruction for it to follow.
func (e *geminiEngine) SendText(text string) error {
	e.sendMu.Lock()
	defer e.sendMu.Unlock()
	if e.session == nil {
		return errors.New("voice session is not connected")
	}
	return e.session.SendRealtimeInput(genai.LiveRealtimeInput{Text: text})
}

// RespondTool implements [ToolResponder].
//
// The model is paused until this arrives, so every tool call must be answered.
// Taking a second is survivable; never answering is dead air followed by a
// cancelled call.
func (e *geminiEngine) RespondTool(id, name string, response map[string]any) error {
	e.sendMu.Lock()
	defer e.sendMu.Unlock()
	if e.session == nil {
		return errors.New("voice session is not connected")
	}
	return e.session.SendToolResponse(genai.LiveToolResponseInput{
		FunctionResponses: []*genai.FunctionResponse{{
			ID:       id,
			Name:     name,
			Response: response,
		}},
	})
}

// Events implements [Engine].
func (e *geminiEngine) Events() <-chan Event { return e.events }

// SampleRate implements [Engine]. Gemini returns audio at 24 kHz regardless of
// the 16 kHz it is fed, which is why the rate is announced on the wire.
func (e *geminiEngine) SampleRate() int { return OutputSampleRate }

// LastSpeech implements [SpeechIdler] using the model's own voice activity
// detection, which is a far better idle signal than frame arrival: the
// microphone streams continuously, so frames keep coming from an empty room.
func (e *geminiEngine) LastSpeech() time.Time {
	e.lastSpeechMu.Lock()
	defer e.lastSpeechMu.Unlock()
	return e.lastSpeech
}

func (e *geminiEngine) noteSpeech() {
	e.lastSpeechMu.Lock()
	e.lastSpeech = time.Now()
	e.lastSpeechMu.Unlock()
}

// Close implements [Engine]. Idempotent.
func (e *geminiEngine) Close() error {
	var err error
	e.closeOnce.Do(func() {
		e.cancel()
		e.sendMu.Lock()
		if e.session != nil {
			err = e.session.Close()
			e.session = nil
		}
		e.sendMu.Unlock()
		e.wg.Wait()
		close(e.events)
	})
	return err
}

// emit queues an event, dropping it if the consumer has fallen behind.
func (e *geminiEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.ctx.Done():
	default:
	}
}

// receiveLoop pumps the live session, reconnecting across the connection
// lifetime limit until the engine's context ends.
func (e *geminiEngine) receiveLoop() {
	for {
		if err := e.ctx.Err(); err != nil {
			return
		}

		e.sendMu.Lock()
		session := e.session
		e.sendMu.Unlock()
		if session == nil {
			return
		}

		msg, err := session.Receive()
		if err != nil {
			if e.ctx.Err() != nil {
				return
			}
			// EOF and transport errors both mean this connection is finished.
			// The session is not: reconnect with the stored handle and the
			// browser never learns anything happened.
			if !errors.Is(err, io.EOF) {
				e.log.Warn("voice receive failed, reconnecting", "error", err)
			}
			if !e.reconnect() {
				e.emit(ErrorEvent{Err: err, Fatal: true})
				return
			}
			continue
		}
		if msg == nil {
			continue
		}
		e.handle(msg)
	}
}

// handle dispatches one server message.
func (e *geminiEngine) handle(msg *genai.LiveServerMessage) {
	if u := msg.SessionResumptionUpdate; u != nil && u.Resumable && u.NewHandle != "" {
		e.handleMu.Lock()
		e.resumeHandle = u.NewHandle
		e.handleMu.Unlock()
	}

	if msg.GoAway != nil {
		// The server is about to close. Closing our side now makes Receive
		// return promptly so the reconnect happens on our schedule rather than
		// mid-sentence.
		e.log.Info("voice session goaway", "timeLeft", msg.GoAway.TimeLeft)
		e.sendMu.Lock()
		if e.session != nil {
			_ = e.session.Close()
		}
		e.sendMu.Unlock()
		return
	}

	if call := msg.ToolCall; call != nil {
		for _, fc := range call.FunctionCalls {
			if fc == nil {
				continue
			}
			e.emit(ToolCallEvent{ID: fc.ID, Name: fc.Name, Args: fc.Args})
		}
		return
	}

	content := msg.ServerContent
	if content == nil {
		return
	}

	if t := content.InputTranscription; t != nil && t.Text != "" {
		e.noteSpeech()
		e.emit(TranscriptEvent{Text: t.Text, Source: "caller", Final: content.TurnComplete})
	}
	if t := content.OutputTranscription; t != nil && t.Text != "" {
		e.emit(TranscriptEvent{Text: t.Text, Source: "engine", Final: content.TurnComplete})
	}

	if content.ModelTurn != nil {
		for _, part := range content.ModelTurn.Parts {
			if part == nil || part.InlineData == nil || len(part.InlineData.Data) == 0 {
				continue
			}
			e.emit(AudioEvent{PCM: part.InlineData.Data})
		}
	}

	// Both outcomes must reach the browser, because both mean "flush what is
	// queued". An interruption that leaves queued speech playing talks over the
	// person who interrupted.
	if content.Interrupted {
		e.noteSpeech()
		e.emit(TurnCompleteEvent{Interrupted: true})
		return
	}
	if content.TurnComplete {
		e.emit(TurnCompleteEvent{})
	}
}

// reconnect re-establishes the live session using the stored resumption
// handle. It reports whether the call can continue.
func (e *geminiEngine) reconnect() bool {
	e.handleMu.Lock()
	handle := e.resumeHandle
	e.handleMu.Unlock()

	cfg := *e.config
	cfg.SessionResumption = &genai.SessionResumptionConfig{Handle: handle}

	session, err := e.client.Live.Connect(e.ctx, e.model, &cfg)
	if err != nil {
		e.log.Error("voice reconnect failed", "error", err)
		return false
	}

	e.sendMu.Lock()
	previous := e.session
	e.session = session
	e.sendMu.Unlock()

	if previous != nil {
		_ = previous.Close()
	}
	e.log.Info("voice session resumed", "resumed", handle != "")
	return true
}

// firstNonEmptyString returns the first value that is set.
func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
