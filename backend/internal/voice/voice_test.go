package voice

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestParseBackend(t *testing.T) {
	tests := []struct {
		name    string
		want    Backend
		wantErr bool
	}{
		{name: "", want: BackendAIStudio},
		{name: "echo", want: BackendEcho},
		{name: "aistudio", want: BackendAIStudio},
		{name: "vertex", want: BackendVertex},
		{name: "gemini", wantErr: true},
		{name: "AIStudio", wantErr: true}, // case-sensitive on purpose
	}

	for _, tt := range tests {
		got, err := ParseBackend(tt.name)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseBackend(%q) = %q, want an error", tt.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseBackend(%q) returned %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseBackend(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// A backend that needs a credential must not produce a handler without one —
// otherwise the failure surfaces as a dead call rather than a startup error.
func TestNewHandlerRequiresCredentials(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{name: "echo needs nothing", opts: Options{Backend: BackendEcho}},
		{name: "aistudio without a key", opts: Options{Backend: BackendAIStudio}, wantErr: true},
		{name: "aistudio with a key", opts: Options{Backend: BackendAIStudio, APIKey: "k"}},
		{name: "vertex without a project", opts: Options{Backend: BackendVertex}, wantErr: true},
		{name: "vertex with a project", opts: Options{Backend: BackendVertex, Project: "p"}},
		{name: "unknown backend", opts: Options{Backend: Backend("nope")}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHandler(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewHandler() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// pcmFrame builds one frame of Int16 little-endian mono PCM.
func pcmFrame(samples ...int16) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	return buf
}

func dialVoice(t *testing.T, opts Options) (*websocket.Conn, func()) {
	t.Helper()
	h, err := NewHandler(opts)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	ws, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v (resp %v)", err, resp)
	}
	return ws, func() { _ = ws.Close(); srv.Close() }
}

func readControl(t *testing.T, ws *websocket.Conn) serverMessage {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	msgType, payload, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read control: %v", err)
	}
	if msgType != websocket.TextMessage {
		t.Fatalf("read control: got frame type %d, want text", msgType)
	}
	var msg serverMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode control: %v", err)
	}
	return msg
}

// The client must learn its playback rate from the wire. The echo engine
// returns audio at the input rate and a speech model at a different one, so a
// client that hardcodes either plays one of them at the wrong speed.
func TestReadyAnnouncesSampleRates(t *testing.T) {
	ws, cleanup := dialVoice(t, Options{Backend: BackendEcho})
	defer cleanup()

	ready := readControl(t, ws)
	if ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}
	if ready.InputSampleRate != InputSampleRate {
		t.Errorf("inputSampleRate = %d, want %d", ready.InputSampleRate, InputSampleRate)
	}
	if ready.OutputSampleRate != InputSampleRate {
		t.Errorf("echo outputSampleRate = %d, want %d (a loopback does not resample)", ready.OutputSampleRate, InputSampleRate)
	}
}

func TestEchoRoundTrip(t *testing.T) {
	ws, cleanup := dialVoice(t, Options{Backend: BackendEcho})
	defer cleanup()

	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	sent := pcmFrame(0, 1000, -1000, 32767, -32768)
	if err := ws.WriteMessage(websocket.BinaryMessage, sent); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	msgType, got, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read audio: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Fatalf("echo frame type = %d, want binary — audio must never ride a JSON frame", msgType)
	}
	if string(got) != string(sent) {
		t.Errorf("echo returned %v, want %v", got, sent)
	}
}

func TestClientStopClosesTheCall(t *testing.T) {
	ws, cleanup := dialVoice(t, Options{Backend: BackendEcho})
	defer cleanup()

	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	if err := ws.WriteJSON(clientMessage{Type: msgStop}); err != nil {
		t.Fatalf("write stop: %v", err)
	}

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			return // the server closed the socket, which is the point
		}
	}
}

// An unknown control type is a newer client talking to an older server. It must
// be ignored, not treated as a reason to hang up.
func TestUnknownControlIsIgnored(t *testing.T) {
	ws, cleanup := dialVoice(t, Options{Backend: BackendEcho})
	defer cleanup()

	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	if err := ws.WriteJSON(clientMessage{Type: "some_future_thing"}); err != nil {
		t.Fatalf("write control: %v", err)
	}

	sent := pcmFrame(42, -42)
	if err := ws.WriteMessage(websocket.BinaryMessage, sent); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	msgType, got, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("the call should still be open: %v", err)
	}
	if msgType != websocket.BinaryMessage || string(got) != string(sent) {
		t.Errorf("after an unknown control frame, echo = (%d, %v), want (binary, %v)", msgType, got, sent)
	}
}

// The whole reporting path over a real socket: the client binds the call to a
// session, the worker's report reaches the registry, and it arrives as a text
// control frame — never as a binary one, which is audio's channel.
func TestFollowDeliversReportsOverTheSocket(t *testing.T) {
	registry := NewRegistry()
	h, err := NewHandler(Options{Backend: BackendEcho, Registry: registry})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	if err := ws.WriteJSON(clientMessage{Type: msgFollow, SessionID: "sess-1"}); err != nil {
		t.Fatalf("write follow: %v", err)
	}

	// Follow is processed asynchronously by the read loop.
	deadline := time.Now().Add(3 * time.Second)
	for !registry.Listening("sess-1") {
		if time.Now().After(deadline) {
			t.Fatal("the call never registered as following the session")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := registry.Report("sess-1", "surprise", "the auth tests were already failing"); err != nil {
		t.Fatalf("Report: %v", err)
	}

	got := readControl(t, ws)
	if got.Type != msgReport {
		t.Fatalf("frame type = %q, want %q", got.Type, msgReport)
	}
	if got.Kind != string(ReportSurprise) {
		t.Errorf("kind = %q, want %q", got.Kind, ReportSurprise)
	}
	if got.Headline != "the auth tests were already failing" {
		t.Errorf("headline = %q", got.Headline)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("sessionId = %q, want the followed session", got.SessionID)
	}
}

// Following a session suspends the conversational idle rule, and a run ending
// restores it. Without this a call hangs up in the middle of every real task.
func TestFollowingSuspendsTheIdleRuleUntilTheRunEnds(t *testing.T) {
	registry := NewRegistry()
	h, err := NewHandler(Options{Backend: BackendEcho, Registry: registry})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()
	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q", ready.Type)
	}

	if err := ws.WriteJSON(clientMessage{Type: msgFollow, SessionID: "sess-3"}); err != nil {
		t.Fatalf("write follow: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !registry.Listening("sess-3") {
		if time.Now().After(deadline) {
			t.Fatal("never started following")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Blocked still holds the process, so it must not end the working phase.
	registry.Notice("sess-3", Notice{Kind: NoticeBlocked, Headline: "needs approval"})
	blocked := readControl(t, ws)
	if blocked.Type != msgNotice || blocked.Kind != string(NoticeBlocked) {
		t.Fatalf("frame = %q/%q, want a blocked notice", blocked.Type, blocked.Kind)
	}

	registry.Notice("sess-3", Notice{Kind: NoticeFinished, Headline: "all tests pass"})
	done := readControl(t, ws)
	if done.Type != msgNotice {
		t.Fatalf("frame type = %q, want %q", done.Type, msgNotice)
	}
	if done.Kind != string(NoticeFinished) {
		t.Errorf("kind = %q, want %q", done.Kind, NoticeFinished)
	}
	if done.Headline != "all tests pass" {
		t.Errorf("headline = %q", done.Headline)
	}
	if done.SessionID != "sess-3" {
		t.Errorf("sessionId = %q, want the followed session", done.SessionID)
	}

	// The binding survives the run ending — the call is still following, it is
	// just no longer working.
	if !registry.Listening("sess-3") {
		t.Error("a finished run must not unfollow the session")
	}
}

// A closed call must release its binding, or the registry keeps writing into a
// dead socket and the session looks like it still has a listener.
func TestClosingACallReleasesTheBinding(t *testing.T) {
	registry := NewRegistry()
	h, err := NewHandler(Options{Backend: BackendEcho, Registry: registry})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q", ready.Type)
	}
	if err := ws.WriteJSON(clientMessage{Type: msgFollow, SessionID: "sess-2"}); err != nil {
		t.Fatalf("write follow: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !registry.Listening("sess-2") {
		if time.Now().After(deadline) {
			t.Fatal("never started following")
		}
		time.Sleep(5 * time.Millisecond)
	}

	_ = ws.Close()

	deadline = time.Now().Add(3 * time.Second)
	for registry.Listening("sess-2") {
		if time.Now().After(deadline) {
			t.Fatal("the binding outlived the call")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A full world snapshot must not close the call. The read limit is applied
// before the frame type is known, so a limit sized for audio would end a call
// over a sidebar refresh.
func TestAFullWorldSnapshotDoesNotCloseTheCall(t *testing.T) {
	ws, cleanup := dialVoice(t, Options{Backend: BackendEcho})
	defer cleanup()

	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	rows := make([]wireSessionRow, 0, maxWorldRows)
	for i := range maxWorldRows {
		rows = append(rows, wireSessionRow{
			SessionID:      fmt.Sprintf("8f1c0000-0000-4000-8000-%012d", i),
			Name:           strings.Repeat("session name ", 4),
			ProjectSlug:    "agentique-backend",
			ProjectName:    "agentique backend",
			MachineID:      fmt.Sprintf("9a2d0000-0000-4000-8000-%012d", i),
			MachineName:    "workstation-in-the-office",
			State:          "running",
			Branch:         "feature/some-reasonably-long-branch-name",
			LastActivityAt: "2026-08-26T12:00:00Z",
		})
	}
	if err := ws.WriteJSON(clientMessage{Type: msgWorld, Sessions: rows}); err != nil {
		t.Fatalf("write world: %v", err)
	}

	// The call is still up if it still echoes.
	sent := pcmFrame(7, -7)
	if err := ws.WriteMessage(websocket.BinaryMessage, sent); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	msgType, got, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("the call should have survived a full snapshot: %v", err)
	}
	if msgType != websocket.BinaryMessage || string(got) != string(sent) {
		t.Errorf("after a world snapshot, echo = (%d, %v), want (binary, %v)", msgType, got, sent)
	}
}

func TestMaxCallsIsEnforced(t *testing.T) {
	h, err := NewHandler(Options{Backend: BackendEcho, MaxCalls: 1})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	first, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()

	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("second dial succeeded, want refusal at the call limit")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("second dial status = %v, want %d", resp, http.StatusServiceUnavailable)
	}
}

// Send must not block when nobody is draining the engine, because the caller is
// a microphone that will not wait.
func TestEchoEngineDropsRatherThanBlocking(t *testing.T) {
	e := NewEchoEngine()
	defer e.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range echoBufferFrames * 4 {
			if err := e.Send(context.Background(), pcmFrame(1, 2, 3)); err != nil {
				t.Errorf("Send: %v", err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked once the buffer filled; it must drop frames instead")
	}
}

func TestEchoEngineCloseIsIdempotent(t *testing.T) {
	e := NewEchoEngine()
	if err := e.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	// Sending after close is a no-op, not a panic on a closed channel.
	if err := e.Send(context.Background(), pcmFrame(1)); err != nil {
		t.Fatalf("Send after Close: %v", err)
	}
}
