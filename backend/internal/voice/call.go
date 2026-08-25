package voice

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeTimeout = 10 * time.Second
	pongTimeout  = 60 * time.Second
	pingInterval = 25 * time.Second

	// maxFrameBytes bounds one inbound frame. Caller audio arrives in ~32ms
	// batches (about 1KB at 16kHz mono s16le), so this is generous for a real
	// frame and still refuses anything that is not audio.
	maxFrameBytes = 64 << 10

	// defaultIdleTimeout closes a call whose caller has gone quiet. It exists
	// because a live speech session bills for wall-clock time with the
	// microphone open: unlike every other cost in agentique, an abandoned tab
	// keeps spending until something closes it.
	defaultIdleTimeout = 90 * time.Second

	// idleCheckInterval is how often the idle rule is evaluated. Coarse on
	// purpose — this is a billing guard, not a UI affordance.
	idleCheckInterval = 5 * time.Second
)

// SpeechIdler is an optional [Engine] capability. An engine that performs voice
// activity detection knows when the caller last actually spoke, which is a far
// better idle signal than frame arrival: the microphone streams continuously,
// so frames keep coming from an empty room.
//
// An engine without it falls back to frame arrival, which still catches a
// closed laptop or a dropped connection.
type SpeechIdler interface {
	// LastSpeech reports when caller speech was last detected. A zero time
	// means no speech has been detected yet in this call.
	LastSpeech() time.Time
}

// call is one live conversation: a browser socket on one side, an [Engine] on
// the other.
//
// Framing carries no envelope — the WebSocket frame type is the discriminator.
// Binary frames are Int16 PCM (little-endian, mono); text frames are JSON
// control messages. That keeps audio out of a JSON encoder, which matters at
// 30-odd frames a second.
type call struct {
	ws     *websocket.Conn
	engine Engine
	log    *slog.Logger

	idleTimeout time.Duration

	// writeMu serialises writes. gorilla/websocket rejects concurrent writers,
	// and audio, control frames and pings all originate on different
	// goroutines.
	writeMu sync.Mutex

	// lastFrame is the fallback idle signal for an engine without VAD.
	lastFrameMu sync.Mutex
	lastFrame   time.Time
}

func newCall(ws *websocket.Conn, engine Engine, idleTimeout time.Duration, log *slog.Logger) *call {
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleTimeout
	}
	return &call{
		ws:          ws,
		engine:      engine,
		log:         log,
		idleTimeout: idleTimeout,
		lastFrame:   time.Now(),
	}
}

// run drives the call until the socket closes, the engine ends, or ctx is done.
func (c *call) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if err := c.engine.Close(); err != nil {
			c.log.Warn("voice engine close failed", "error", err)
		}
		_ = c.ws.Close()
	}()

	if err := c.sendControl(serverMessage{
		Type:             msgReady,
		InputSampleRate:  InputSampleRate,
		OutputSampleRate: c.engine.SampleRate(),
	}); err != nil {
		c.log.Warn("voice ready send failed", "error", err)
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); defer cancel(); c.pumpEngine(ctx) }()
	go func() { defer wg.Done(); defer cancel(); c.pumpKeepalive(ctx) }()

	// The read loop owns this goroutine: it is the one that must observe the
	// socket closing, and cancelling on its return is what stops the others.
	c.readLoop(ctx)
	cancel()
	wg.Wait()
}

// readLoop forwards caller audio to the engine and handles control frames.
func (c *call) readLoop(ctx context.Context) {
	c.ws.SetReadLimit(maxFrameBytes)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	})

	for {
		msgType, payload, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.log.Warn("voice read error", "error", err)
			}
			return
		}

		switch msgType {
		case websocket.BinaryMessage:
			c.noteFrame()
			if err := c.engine.Send(ctx, payload); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				c.log.Warn("voice engine send failed", "error", err)
			}

		case websocket.TextMessage:
			if stop := c.handleControl(payload); stop {
				return
			}

		default:
			// Ping/pong/close are handled by the library.
		}
	}
}

// pumpEngine forwards engine output to the browser until the engine ends.
func (c *call) pumpEngine(ctx context.Context) {
	events := c.engine.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := c.forward(ev); err != nil {
				c.log.Warn("voice forward failed", "error", err)
				return
			}
		}
	}
}

// forward writes one engine event to the browser.
func (c *call) forward(ev Event) error {
	switch e := ev.(type) {
	case AudioEvent:
		return c.sendAudio(e.PCM)

	case TurnCompleteEvent:
		// Both the natural end of a turn and an interruption must reach the
		// client, because both mean "flush whatever is queued".
		return c.sendControl(serverMessage{Type: msgTurnComplete, Interrupted: e.Interrupted})

	case TranscriptEvent:
		return c.sendControl(serverMessage{
			Type:   msgTranscript,
			Text:   e.Text,
			Final:  e.Final,
			Source: e.Source,
		})

	case ErrorEvent:
		c.log.Warn("voice engine error", "error", e.Err, "fatal", e.Fatal)
		// The message is deliberately generic: the detail goes to the log, not
		// to a browser that cannot act on it.
		return c.sendControl(serverMessage{Type: msgError, Message: "the voice engine reported a problem"})

	default:
		c.log.Warn("voice unknown engine event", "type", ev)
		return nil
	}
}

// pumpKeepalive pings the browser and enforces the idle rule.
func (c *call) pumpKeepalive(ctx context.Context) {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	idle := time.NewTicker(idleCheckInterval)
	defer idle.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ping.C:
			c.writeMu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
			err := c.ws.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				return
			}

		case <-idle.C:
			if time.Since(c.lastActivity()) < c.idleTimeout {
				continue
			}
			c.log.Info("voice call idle, closing", "timeout", c.idleTimeout)
			_ = c.sendControl(serverMessage{Type: msgClosed, Reason: "idle"})
			return
		}
	}
}

// lastActivity is the engine's speech clock when it has one, and frame arrival
// otherwise. See [SpeechIdler] for why the distinction matters.
func (c *call) lastActivity() time.Time {
	if idler, ok := c.engine.(SpeechIdler); ok {
		if t := idler.LastSpeech(); !t.IsZero() {
			return t
		}
	}
	c.lastFrameMu.Lock()
	defer c.lastFrameMu.Unlock()
	return c.lastFrame
}

func (c *call) noteFrame() {
	c.lastFrameMu.Lock()
	c.lastFrame = time.Now()
	c.lastFrameMu.Unlock()
}

func (c *call) sendAudio(pcm []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.ws.WriteMessage(websocket.BinaryMessage, pcm)
}

func (c *call) sendControl(msg serverMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.ws.WriteJSON(msg)
}
