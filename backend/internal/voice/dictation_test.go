package voice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeTranscriber records what dictation asked of it and emits what the test
// scripts.
type fakeTranscriber struct {
	mu     sync.Mutex
	calls  []string
	frames int
	events chan Event
	once   sync.Once
}

func newFakeTranscriber() *fakeTranscriber {
	return &fakeTranscriber{events: make(chan Event, 16)}
}

func (f *fakeTranscriber) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeTranscriber) Send(context.Context, []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames++
	return nil
}
func (f *fakeTranscriber) BeginUtterance() error { f.record("begin"); return nil }
func (f *fakeTranscriber) EndUtterance() error   { f.record("end"); return nil }
func (f *fakeTranscriber) Events() <-chan Event  { return f.events }
func (f *fakeTranscriber) Close() error {
	f.once.Do(func() { close(f.events) })
	return nil
}

func (f *fakeTranscriber) snapshot() ([]string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...), f.frames
}

func dialDictation(t *testing.T, h *DictationHandler) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func readDictation(t *testing.T, ws *websocket.Conn) dictationMessage {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg dictationMessage
	if err := ws.ReadJSON(&msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	return msg
}

func sendControl(t *testing.T, ws *websocket.Conn, typ string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"type": typ})
	if err := ws.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write %s: %v", typ, err)
	}
}

func fakeDictationHandler(t *testing.T, tr Transcriber) *DictationHandler {
	t.Helper()
	h, err := NewDictationHandler(Options{Backend: BackendAIStudio, APIKey: "k"})
	if err != nil {
		t.Fatalf("NewDictationHandler: %v", err)
	}
	h.newTranscriber = func(context.Context) (Transcriber, error) { return tr, nil }
	return h
}

// eventually polls until cond holds, because the socket delivers asynchronously.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

func TestDictationIsNotMountedWithoutRealSpeech(t *testing.T) {
	for _, opts := range []Options{
		{Backend: BackendEcho},
		{Backend: BackendAIStudio},
		{Backend: BackendVertex},
	} {
		if _, err := NewDictationHandler(opts); !errors.Is(err, ErrDictationUnavailable) {
			t.Errorf("%+v: err = %v, want ErrDictationUnavailable", opts, err)
		}
	}
}

// Audio only means something inside an utterance: the service folds stray
// frames into whichever utterance comes next.
func TestDictationForwardsAudioOnlyInsideAnUtterance(t *testing.T) {
	tr := newFakeTranscriber()
	ws := dialDictation(t, fakeDictationHandler(t, tr))
	if ready := readDictation(t, ws); ready.Type != msgReady || ready.InputSampleRate != InputSampleRate {
		t.Fatalf("first frame = %+v, want ready at %d", ready, InputSampleRate)
	}

	frame := make([]byte, 1024)
	_ = ws.WriteMessage(websocket.BinaryMessage, frame) // before: dropped
	sendControl(t, ws, msgUtteranceStart)
	_ = ws.WriteMessage(websocket.BinaryMessage, frame)
	_ = ws.WriteMessage(websocket.BinaryMessage, frame)
	sendControl(t, ws, msgUtteranceEnd)
	_ = ws.WriteMessage(websocket.BinaryMessage, frame) // after: dropped
	sendControl(t, ws, msgUtteranceEnd)                 // a repeat is not a second end

	eventually(t, "begin, two frames, one end", func() bool {
		calls, frames := tr.snapshot()
		return frames == 2 && strings.Join(calls, ",") == "begin,end"
	})
}

// The client joins chunks by this flag: a space before a new utterance, the
// service's own spacing inside one.
func TestDictationMarksTheFirstChunkOfEachUtterance(t *testing.T) {
	tr := newFakeTranscriber()
	ws := dialDictation(t, fakeDictationHandler(t, tr))
	readDictation(t, ws)

	sendControl(t, ws, msgUtteranceStart)
	sendControl(t, ws, msgUtteranceEnd)
	eventually(t, "first utterance closed", func() bool { c, _ := tr.snapshot(); return len(c) == 2 })
	tr.events <- TranscriptEvent{Text: "Refactor the", Source: transcriptSourceCaller}
	tr.events <- TranscriptEvent{Text: " reconnect logic.", Source: transcriptSourceCaller}

	first, second := readDictation(t, ws), readDictation(t, ws)
	if !first.NewUtterance || second.NewUtterance {
		t.Fatalf("flags = %v, %v; want true, false", first.NewUtterance, second.NewUtterance)
	}

	sendControl(t, ws, msgUtteranceStart)
	sendControl(t, ws, msgUtteranceEnd)
	eventually(t, "second utterance closed", func() bool { c, _ := tr.snapshot(); return len(c) == 4 })
	tr.events <- TranscriptEvent{Text: "Then add a test.", Source: transcriptSourceCaller}
	if third := readDictation(t, ws); third.Type != msgTranscript || !third.NewUtterance || third.Text != "Then add a test." {
		t.Fatalf("third = %+v, want a new utterance", third)
	}
}

// Stopping mid-utterance still closes it, so the last words come back.
func TestDictationStopClosesTheOpenUtterance(t *testing.T) {
	tr := newFakeTranscriber()
	ws := dialDictation(t, fakeDictationHandler(t, tr))
	readDictation(t, ws)

	sendControl(t, ws, msgUtteranceStart)
	sendControl(t, ws, msgStop)
	eventually(t, "stop ends the utterance", func() bool {
		c, _ := tr.snapshot()
		return strings.Join(c, ",") == "begin,end"
	})
	tr.events <- TranscriptEvent{Text: "last words", Source: transcriptSourceCaller}
	if msg := readDictation(t, ws); msg.Text != "last words" {
		t.Fatalf("after stop = %+v, want the last words", msg)
	}
}

// A service that ends says why, then closes.
func TestDictationReportsTheServiceEnding(t *testing.T) {
	tr := newFakeTranscriber()
	ws := dialDictation(t, fakeDictationHandler(t, tr))
	readDictation(t, ws)

	tr.events <- ErrorEvent{Err: errDictationSessionLimit, Fatal: true}
	if msg := readDictation(t, ws); msg.Type != msgError || !strings.Contains(msg.Message, "time limit") {
		t.Fatalf("got %+v, want the time limit named", msg)
	}
	if msg := readDictation(t, ws); msg.Type != msgClosed {
		t.Fatalf("got %+v, want closed", msg)
	}
}

func TestDictationRefusesOnTheSocketWhenTheServiceWillNotAnswer(t *testing.T) {
	h := fakeDictationHandler(t, nil)
	h.newTranscriber = func(context.Context) (Transcriber, error) { return nil, errors.New("dial refused") }
	ws := dialDictation(t, h)
	if msg := readDictation(t, ws); msg.Type != msgError || msg.Message != "the speech service is unavailable" {
		t.Fatalf("got %+v, want a fixed refusal", msg)
	}
}

func TestDictationClosesWhenNobodySpeaks(t *testing.T) {
	tr := newFakeTranscriber()
	h := fakeDictationHandler(t, tr)
	h.idleTimeout = 80 * time.Millisecond
	ws := dialDictation(t, h)
	readDictation(t, ws)
	if msg := readDictation(t, ws); msg.Type != msgClosed || msg.Reason != "idle" {
		t.Fatalf("got %+v, want closed for idle", msg)
	}
}
