package voice

import (
	"context"
	"encoding/binary"
	"encoding/json"
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
