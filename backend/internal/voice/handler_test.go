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
