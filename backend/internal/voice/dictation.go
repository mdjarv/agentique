package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/httpsecurity"
)

// Dictation is speech to composer text for browsers whose Web Speech API cannot
// do it: Brave blocks the service behind it, Firefox has none, and Safari
// refuses while macOS Dictation is off. It is not a call. Nothing is spoken,
// nothing is dispatched, no persona or tool exists — the caller's words come
// back as text and that is the whole contract, which is why it has its own
// socket rather than a mode on the call's.
//
// The protocol is the call's shape cut down. Binary frames are 16 kHz PCM and
// only mean anything inside an utterance; text frames are JSON control:
//
//	client → utterance_start | utterance_end | stop
//	server → ready | transcript{text, newUtterance} | error{message} | closed{reason}
//
// The client decides where utterances begin and end, from its own microphone
// level. That is forced by the service: with automatic activity detection a
// Live session sometimes never returned the second sentence of a dictation,
// and with detection off nothing is transcribed until an utterance is closed.
// See TestDictationProbeLive and docs/voice.md.
const (
	msgUtteranceStart = "utterance_start"
	msgUtteranceEnd   = "utterance_end"
)

const (
	// defaultMaxDictations bounds concurrent dictations. Each holds a live
	// speech session open.
	defaultMaxDictations int64 = 4

	// dictationIdleTimeout ends a dictation nobody has spoken into. The client
	// stops on its own long before this; it is the guard for a tab that did not.
	dictationIdleTimeout = 2 * time.Minute

	// dictationMaxFrameBytes bounds one inbound frame. A 32 ms audio batch is
	// about 1 KB and a control frame is tens of bytes.
	dictationMaxFrameBytes = 16 << 10
)

// Transcriber is dictation's seam: caller audio in, the caller's words out.
//
// Send is honoured only between BeginUtterance and EndUtterance. Events carries
// [TranscriptEvent] and [ErrorEvent] and is closed when the transcriber ends.
type Transcriber interface {
	Send(ctx context.Context, pcm []byte) error
	BeginUtterance() error
	EndUtterance() error
	Events() <-chan Event
	Close() error
}

// ErrDictationUnavailable is returned for a backend dictation cannot use. The
// echo engine has no words to give back, so a server without real speech
// credentials does not mount dictation at all.
var ErrDictationUnavailable = errors.New("dictation needs a speech backend with credentials")

// DictationHandler serves the dictation socket. Route it under /api/, for the
// reason [Handler] gives.
type DictationHandler struct {
	opts    Options
	tracker SessionTracker

	// newTranscriber is a field so a test can supply a fake; production gets a
	// Gemini Live session.
	newTranscriber func(ctx context.Context) (Transcriber, error)

	dialBudget  time.Duration
	idleTimeout time.Duration
	active      atomic.Int64
}

// NewDictationHandler returns a dictation handler for a real speech backend.
func NewDictationHandler(opts Options) (*DictationHandler, error) {
	switch opts.Backend {
	case BackendAIStudio:
		if opts.APIKey == "" {
			return nil, ErrDictationUnavailable
		}
	case BackendVertex:
		if opts.Project == "" {
			return nil, ErrDictationUnavailable
		}
	default:
		return nil, ErrDictationUnavailable
	}
	h := &DictationHandler{opts: opts, dialBudget: engineDialBudget, idleTimeout: dictationIdleTimeout}
	h.newTranscriber = func(ctx context.Context) (Transcriber, error) {
		return newGeminiTranscriber(ctx, h.opts, slog.With("subsystem", "dictation"))
	}
	return h, nil
}

// SetSessionTracker binds each dictation to the auth session that opened it,
// so revoking that session closes the microphone too.
func (h *DictationHandler) SetSessionTracker(t SessionTracker) { h.tracker = t }

func (h *DictationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	limit := h.opts.MaxCalls
	if limit <= 0 {
		limit = defaultMaxDictations
	}
	if n := h.active.Add(1); n > limit {
		h.active.Add(-1)
		http.Error(w, "too many dictations", http.StatusServiceUnavailable)
		return
	}
	defer h.active.Add(-1)

	// Upgrade first, for the call's reason: the dial is a network round trip,
	// and a failure after this point can be said on the socket.
	u := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return httpsecurity.WebSocketOriginAllowed(r, h.opts.AllowedOrigins, h.opts.AllowTicketOrigin)
	}}
	ws, err := u.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("dictation upgrade failed", "error", err, "remote", r.RemoteAddr)
		return
	}
	defer ws.Close()
	log := slog.With("subsystem", "dictation", "backend", h.opts.Backend)

	d := &dictation{ws: ws, log: log, idleTimeout: h.idleTimeout}
	d.newUtterance.Store(true)

	if h.tracker != nil {
		untrack, err := h.tracker.TrackWebSocket(auth.UserFromContext(r.Context()), func() { d.end("revoked") })
		if err != nil {
			log.Warn("dictation auth session tracking failed", "error", err)
			refuseCall(ws, "not authorized")
			return
		}
		defer untrack()
	}

	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()

	tr, err := dialWithin(ctx, h.dialBudget, h.newTranscriber)
	if err != nil {
		log.Error("dictation transcriber start failed", "error", err)
		refuseCall(ws, "the speech service is unavailable")
		return
	}
	defer tr.Close()
	d.tr = tr

	d.run(ctx, cancel)
}

// dialWithin waits at most budget for dial, closing a late arrival rather than
// abandoning it: a speech session nobody holds still bills.
func dialWithin[T interface{ Close() error }](ctx context.Context, budget time.Duration, dial func(context.Context) (T, error)) (T, error) {
	type dialed struct {
		v   T
		err error
	}
	// Buffered: the dial must be able to finish and exit even after the wait
	// has been given up on, or a slow backend leaks a goroutine per attempt.
	done := make(chan dialed, 1)
	go func() {
		v, err := dial(ctx)
		done <- dialed{v, err}
	}()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case d := <-done:
		return d.v, d.err
	case <-timer.C:
		go func() {
			if d := <-done; d.err == nil {
				_ = d.v.Close()
			}
		}()
		var zero T
		return zero, fmt.Errorf("the speech backend did not answer within %s", budget)
	}
}

// dictationMessage is a control frame on the dictation socket. Every field past
// Type is optional, the rule every voice frame follows.
type dictationMessage struct {
	Type string `json:"type"`

	// ready
	InputSampleRate int `json:"inputSampleRate,omitempty"`

	// transcript. NewUtterance marks the first text of an utterance, where the
	// client puts a space; every later chunk of the same utterance carries the
	// service's own spacing and is appended as it is, since a chunk boundary can
	// fall inside a word.
	Text         string `json:"text,omitempty"`
	NewUtterance bool   `json:"newUtterance,omitempty"`

	// error
	Message string `json:"message,omitempty"`
	// closed
	Reason string `json:"reason,omitempty"`
}

// dictation is one open dictation socket.
type dictation struct {
	ws          *websocket.Conn
	tr          Transcriber
	log         *slog.Logger
	idleTimeout time.Duration

	writeMu sync.Mutex

	// inUtterance gates audio: frames outside an utterance are dropped, since
	// the service would fold them into whichever utterance comes next.
	inUtterance atomic.Bool
	// newUtterance is set when an utterance is closed and consumed by the first
	// transcript chunk after it. The transcript of an utterance arrives well
	// before the next can be spoken and closed, so the order holds.
	newUtterance atomic.Bool
	// lastSpeech is the unix-nano time of the last utterance mark.
	lastSpeech atomic.Int64

	endOnce sync.Once
}

func (d *dictation) run(ctx context.Context, cancel context.CancelFunc) {
	d.lastSpeech.Store(time.Now().UnixNano())
	if err := d.write(dictationMessage{Type: msgReady, InputSampleRate: InputSampleRate}); err != nil {
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		d.pumpTranscripts(ctx)
	}()
	go func() {
		defer wg.Done()
		d.keepalive(ctx)
	}()

	d.readLoop(ctx)
	cancel()
	// Unblock the transcript pump, which may be waiting on the service.
	_ = d.tr.Close()
	wg.Wait()
}

func (d *dictation) readLoop(ctx context.Context) {
	d.ws.SetReadLimit(dictationMaxFrameBytes)
	_ = d.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	d.ws.SetPongHandler(func(string) error {
		return d.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	})
	for {
		msgType, payload, err := d.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
				d.log.Warn("dictation read error", "error", err)
			}
			return
		}
		switch msgType {
		case websocket.BinaryMessage:
			if !d.inUtterance.Load() {
				continue
			}
			if err := d.tr.Send(ctx, payload); err != nil && !errors.Is(err, context.Canceled) {
				d.log.Warn("dictation send failed", "error", err)
			}
		case websocket.TextMessage:
			if stop := d.handleControl(payload); stop {
				return
			}
		}
	}
}

// handleControl applies one client control frame; true ends the dictation.
func (d *dictation) handleControl(payload []byte) bool {
	var msg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return false
	}
	switch msg.Type {
	case msgUtteranceStart:
		if d.inUtterance.Swap(true) {
			return false
		}
		d.lastSpeech.Store(time.Now().UnixNano())
		if err := d.tr.BeginUtterance(); err != nil {
			d.log.Warn("dictation utterance start failed", "error", err)
		}
	case msgUtteranceEnd:
		if !d.inUtterance.Swap(false) {
			return false
		}
		d.lastSpeech.Store(time.Now().UnixNano())
		d.newUtterance.Store(true)
		if err := d.tr.EndUtterance(); err != nil {
			d.log.Warn("dictation utterance end failed", "error", err)
		}
	case msgStop:
		// Close an open utterance first, so its words still come back before
		// the client stops listening for them.
		if d.inUtterance.Swap(false) {
			d.newUtterance.Store(true)
			_ = d.tr.EndUtterance()
		}
		return false
	}
	return false
}

// pumpTranscripts forwards the caller's words and reports a service that ends.
func (d *dictation) pumpTranscripts(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-d.tr.Events():
			if !ok {
				d.end("ended")
				return
			}
			switch e := ev.(type) {
			case TranscriptEvent:
				if e.Text == "" {
					continue
				}
				_ = d.write(dictationMessage{
					Type:         msgTranscript,
					Text:         e.Text,
					NewUtterance: d.newUtterance.Swap(false),
				})
			case ErrorEvent:
				d.log.Warn("dictation transcriber error", "error", e.Err, "fatal", e.Fatal)
				if !e.Fatal {
					continue
				}
				reason := "the speech service ended the dictation"
				if errors.Is(e.Err, errDictationSessionLimit) {
					reason = "the dictation reached its time limit"
				}
				_ = d.write(dictationMessage{Type: msgError, Message: reason})
				d.end("ended")
				return
			default:
			}
		}
	}
}

func (d *dictation) keepalive(ctx context.Context) {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	check := time.NewTicker(d.idleTimeout / 4)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			d.writeMu.Lock()
			_ = d.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
			err := d.ws.WriteMessage(websocket.PingMessage, nil)
			d.writeMu.Unlock()
			if err != nil {
				return
			}
		case <-check.C:
			if d.inUtterance.Load() {
				continue
			}
			if time.Since(time.Unix(0, d.lastSpeech.Load())) > d.idleTimeout {
				d.log.Info("dictation idle, closing", "timeout", d.idleTimeout)
				d.end("idle")
				return
			}
		}
	}
}

// end says why the dictation is over, once, and closes the socket, which ends
// the read loop and with it everything else.
func (d *dictation) end(reason string) {
	d.endOnce.Do(func() {
		_ = d.write(dictationMessage{Type: msgClosed, Reason: reason})
		d.writeMu.Lock()
		_ = d.ws.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(writeTimeout))
		d.writeMu.Unlock()
		_ = d.ws.Close()
	})
}

func (d *dictation) write(msg dictationMessage) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_ = d.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := d.ws.WriteJSON(msg); err != nil {
		return fmt.Errorf("dictation write: %w", err)
	}
	return nil
}
