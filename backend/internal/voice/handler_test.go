package voice

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// handshakeDialer gives up rather than waiting forever, so an upgrade that is
// blocked behind slow work fails this file's tests instead of hanging them.
var handshakeDialer = &websocket.Dialer{HandshakeTimeout: 3 * time.Second}

// blockingDirectory holds Orientation until it is released, standing in for the
// real one on a machine where a directory read is slow.
type blockingDirectory struct {
	fakeDirectory
	entered chan struct{}
	release chan struct{}
}

func (b *blockingDirectory) Orientation(ctx context.Context) string {
	close(b.entered)
	select {
	case <-b.release:
		return "Two sessions, none waiting."
	case <-ctx.Done():
		return ""
	}
}

// The socket opens before anything slow is attempted.
//
// The briefing runs a directory read and, behind it, a provider-CLI summary; the
// engine handshake is a network round trip. Building all of that before the
// upgrade meant the browser waited seconds for a socket and sometimes gave up
// entirely — a call that transcribed the operator's speech and never answered.
func TestUpgradeHappensBeforeTheBriefing(t *testing.T) {
	dir := &blockingDirectory{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	h, err := NewHandler(Options{Backend: BackendEcho, Directory: dir})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v — the socket must be up before the briefing is gathered", err)
	}
	defer ws.Close()

	// The upgrade completed, so the briefing must still be where the test left
	// it. Anything else means the handler gathered first and upgraded after.
	select {
	case <-dir.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the briefing was never gathered")
	}

	close(dir.release)
	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}
}

// A briefing that never answers costs the drafter its context, never the call.
func TestBriefingBudgetDoesNotHoldTheCall(t *testing.T) {
	dir := &blockingDirectory{
		entered: make(chan struct{}),
		release: make(chan struct{}), // never released
	}
	defer close(dir.release)

	h, err := NewHandler(Options{Backend: BackendEcho, Directory: dir})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	// The real budget is measured in seconds; a test should not wait one out.
	h.briefingBudget = 100 * time.Millisecond
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	<-dir.entered
	// The budget expires and the call opens without an orientation.
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	msgType, payload, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("ready never arrived behind a stuck briefing: %v", err)
	}
	if msgType != websocket.TextMessage || !strings.Contains(string(payload), msgReady) {
		t.Fatalf("first frame = (%d, %s), want a ready control frame", msgType, payload)
	}
}

// The engine is built after the upgrade, so its failure has to be reported on
// the socket. An HTTP status written into a hijacked connection is read by
// nobody, and a socket that opens and then goes silent is the exact fault the
// ordering exists to fix.
func TestEngineFailureAfterUpgradeIsSaidOnTheSocket(t *testing.T) {
	h, err := NewHandler(Options{Backend: BackendEcho})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	h.newEngine = func(context.Context, string, Persona) (Engine, error) {
		return nil, errors.New("credentials rejected by the speech vendor")
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v — the socket opens before the engine, so it must not fail here", err)
	}
	defer ws.Close()

	msg := readControl(t, ws)
	if msg.Type != msgError {
		t.Fatalf("control frame = %q, want %q", msg.Type, msgError)
	}
	if msg.Message == "" {
		t.Error("the refusal carried no reason; the reader is left with a mute call")
	}
	if strings.Contains(msg.Message, "credentials rejected") {
		t.Errorf("the failure detail reached the browser: %q — it belongs in the log", msg.Message)
	}

	// And it hangs up rather than leaving a socket nothing will ever speak on.
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Error("the socket stayed open after the refusal")
	}
}

// A backend that never answers is refused, rather than held.
//
// Without a budget of ours the dial ran until the browser gave up, holding one
// of very few call slots — and since the client rings while it connects, the
// operator would hear a call ringing forever at a backend that was never going
// to answer.
func TestEngineDialTimeoutIsRefusedOnTheSocket(t *testing.T) {
	h, err := NewHandler(Options{Backend: BackendEcho})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	dialing := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	h.newEngine = func(context.Context, string, Persona) (Engine, error) {
		close(dialing)
		<-release
		return NewEchoEngine(), nil
	}
	// The real budget is measured in seconds; a test should not wait one out.
	h.engineDialBudget = 100 * time.Millisecond
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v — the socket opens before the engine", err)
	}
	defer ws.Close()

	<-dialing
	msg := readControl(t, ws)
	if msg.Type != msgError {
		t.Fatalf("control frame = %q, want %q — a stuck dial must be said, not waited out", msg.Type, msgError)
	}
	if msg.Message == "" {
		t.Error("the refusal carried no reason; the reader is left with a mute call")
	}

	// And it hangs up, which is what stops the client ringing.
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Error("the socket stayed open after the refusal")
	}
}

// An engine that turns up after the budget is closed, not abandoned: it is a
// live session on somebody's paid backend with nobody listening to it.
func TestLateEngineIsClosedRatherThanLeaked(t *testing.T) {
	h, err := NewHandler(Options{Backend: BackendEcho})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	engine := &closeCountingEngine{Engine: NewEchoEngine(), closed: make(chan struct{})}
	release := make(chan struct{})
	h.newEngine = func(context.Context, string, Persona) (Engine, error) {
		<-release
		return engine, nil
	}
	h.engineDialBudget = 50 * time.Millisecond
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	if msg := readControl(t, ws); msg.Type != msgError {
		t.Fatalf("control frame = %q, want %q", msg.Type, msgError)
	}
	close(release)

	select {
	case <-engine.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the late engine was never closed")
	}
}

// The greeting goes out when the call goes live, and once.
//
// Its second job is the downlink's proof of life: the client's audio-health
// watchdog can only report "the assistant replied and no audio arrived" once
// the assistant has replied to something, so greeting on pickup makes that
// check happen seconds after connecting rather than after the first exchange.
func TestPickupGreetingGoesOutWhenTheCallGoesLive(t *testing.T) {
	engine := newSpeakingEngine()
	h, err := NewHandler(Options{
		Backend:   BackendEcho,
		Directory: &fakeDirectory{rows: []SessionRow{{ID: "sess-1", Name: "Live Voice Dialog"}}},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	h.newEngine = func(context.Context, string, Persona) (Engine, error) { return engine, nil }
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"?sessionId=sess-1", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	said := engine.waitForSpeech(t, 1)
	if len(said) == 0 {
		t.Fatal("nothing was injected on pickup — the call would sit silent until the operator spoke")
	}
	if !strings.Contains(said[0], "Live Voice Dialog") {
		t.Errorf("the greeting does not name the session the call opened on: %q", said[0])
	}

	// Ordinary traffic must not produce a second one.
	if err := ws.WriteJSON(clientMessage{Type: msgWorld}); err != nil {
		t.Fatalf("write world: %v", err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, pcmFrame(1, 2, 3)); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err != nil {
		t.Fatalf("the call should still be up: %v", err)
	}
	if got := engine.spoken(); len(got) != 1 {
		t.Errorf("greeted %d times in one call, want exactly 1: %q", len(got), got)
	}
}

// closeCountingEngine reports when it is released.
type closeCountingEngine struct {
	Engine
	once   sync.Once
	closed chan struct{}
}

func (e *closeCountingEngine) Close() error {
	e.once.Do(func() { close(e.closed) })
	return e.Engine.Close()
}

// corpseEngine emits one fatal error and then nothing, its event stream left
// open — modelling a receive loop that died without closing it.
type corpseEngine struct{ events chan Event }

func newCorpseEngine() *corpseEngine {
	c := &corpseEngine{events: make(chan Event, 1)}
	c.events <- ErrorEvent{Err: errors.New("engine gone"), Fatal: true}
	return c
}

func (c *corpseEngine) Send(context.Context, []byte) error { return nil }
func (c *corpseEngine) Events() <-chan Event               { return c.events }
func (c *corpseEngine) SampleRate() int                    { return InputSampleRate }
func (c *corpseEngine) Close() error                       { return nil }

// A fatal engine error ends the call. Before this, it produced one error frame
// and the call then sat on the idle ceiling — up to thirty minutes of the
// operator streaming microphone audio into a corpse.
func TestFatalEngineErrorEndsTheCall(t *testing.T) {
	h, err := NewHandler(Options{Backend: BackendEcho})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	h.newEngine = func(context.Context, string, Persona) (Engine, error) {
		return newCorpseEngine(), nil
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	// The error is said first — it is what stops the client's ringback — and
	// the one closed frame follows it.
	sawError := false
	for {
		msg := readControl(t, ws)
		switch msg.Type {
		case msgError:
			sawError = true
		case msgClosed:
			if !sawError {
				t.Error("closed arrived with no error frame before it — the failure was never said")
			}
			if msg.Reason != "engine-error" {
				t.Errorf("close reason = %q, want engine-error", msg.Reason)
			}
			return
		}
	}
}

// trackingRecorder is a SessionTracker that records what the handler gave it.
type trackingRecorder struct {
	mu        sync.Mutex
	refuse    error
	closeCall func()
	untracked bool
}

func (f *trackingRecorder) TrackWebSocket(_ *store.GetAuthSessionRow, closeConn func()) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return nil, f.refuse
	}
	f.closeCall = closeConn
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.untracked = true
	}, nil
}

func (f *trackingRecorder) revoke() {
	f.mu.Lock()
	closeCall := f.closeCall
	f.mu.Unlock()
	if closeCall != nil {
		closeCall()
	}
}

func (f *trackingRecorder) wasUntracked() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.untracked
}

// Revoking the auth session that opened a call hangs the call up. The voice
// socket is an authenticated WebSocket with a dispatcher behind it; before it
// registered with the tracker, revocation and expiry closed /ws subscriptions
// while a live microphone — and run_prompt — kept running on the dead
// credential (docs/multi-machine.md: established sockets close on revocation).
func TestRevokedAuthSessionHangsUpTheCall(t *testing.T) {
	h, err := NewHandler(Options{Backend: BackendEcho})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	tracker := &trackingRecorder{}
	h.SetSessionTracker(tracker)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()
	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	tracker.revoke()

	// The client is told, on the call's own exactly-once teardown — a raw
	// socket close alone would race the call's writer and end without a word.
	if closed := readControl(t, ws); closed.Type != msgClosed {
		t.Fatalf("control frame after revocation = %q, want %q", closed.Type, msgClosed)
	}

	// And the close is enforced server-side rather than left to the client.
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			break
		}
	}

	// And the registration is released once the call is gone.
	deadline := time.Now().Add(5 * time.Second)
	for !tracker.wasUntracked() {
		if time.Now().After(deadline) {
			t.Fatal("the tracker registration was never released after the call ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A session the tracker refuses — expired, or revoked during setup — never
// gets a call. Reported on the socket, because the upgrade already happened.
func TestUntrackableAuthSessionIsRefused(t *testing.T) {
	h, err := NewHandler(Options{Backend: BackendEcho})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	h.SetSessionTracker(&trackingRecorder{refuse: errors.New("auth session expired")})
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v — refusal happens after the upgrade, on the socket", err)
	}
	defer ws.Close()

	msg := readControl(t, ws)
	if msg.Type != msgError {
		t.Fatalf("control frame = %q, want %q", msg.Type, msgError)
	}
	if strings.Contains(msg.Message, "expired") {
		t.Errorf("the tracker's detail reached the browser: %q — it belongs in the log", msg.Message)
	}

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Error("the socket stayed open after the refusal")
	}
}

// A call that ends normally releases its tracker registration, or the auth
// service accumulates an entry per finished call for the session's lifetime.
func TestFinishedCallReleasesItsTracking(t *testing.T) {
	h, err := NewHandler(Options{Backend: BackendEcho})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	tracker := &trackingRecorder{}
	h.SetSessionTracker(tracker)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	_ = ws.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !tracker.wasUntracked() {
		if time.Now().After(deadline) {
			t.Fatal("hanging up did not release the tracker registration")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
