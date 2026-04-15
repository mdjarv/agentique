package browser

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCDP is a test helper that acts as a CDP WebSocket server.
type mockCDP struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	conn    *websocket.Conn
	received []cdpMessage
}

func newMockCDP(t *testing.T) *mockCDP {
	t.Helper()
	m := &mockCDP{t: t}

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		m.mu.Lock()
		m.conn = ws
		m.mu.Unlock()
	}))

	return m
}

func (m *mockCDP) wsURL() string {
	return "ws" + strings.TrimPrefix(m.srv.URL, "http")
}

func (m *mockCDP) close() {
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
	}
	m.mu.Unlock()
	m.srv.Close()
}

// waitConn waits for a client to connect.
func (m *mockCDP) waitConn(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.mu.Lock()
		c := m.conn
		m.mu.Unlock()
		if c != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for WS connection")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readMessage reads one CDP message from the client.
func (m *mockCDP) readMessage(t *testing.T) cdpMessage {
	t.Helper()
	m.mu.Lock()
	c := m.conn
	m.mu.Unlock()

	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := c.ReadMessage()
	require.NoError(t, err)

	var msg cdpMessage
	require.NoError(t, json.Unmarshal(data, &msg))
	return msg
}

// sendResult sends a method response for a given request ID.
func (m *mockCDP) sendResult(id int64, result any) {
	m.mu.Lock()
	c := m.conn
	m.mu.Unlock()

	var raw json.RawMessage
	if result != nil {
		raw, _ = json.Marshal(result)
	} else {
		raw = json.RawMessage(`{}`)
	}

	msg := cdpMessage{ID: id, Result: raw}
	data, _ := json.Marshal(msg)
	c.WriteMessage(websocket.TextMessage, data)
}

// sendEvent sends a CDP event (no ID).
func (m *mockCDP) sendEvent(method string, params any) {
	m.mu.Lock()
	c := m.conn
	m.mu.Unlock()

	raw, _ := json.Marshal(params)
	msg := cdpMessage{Method: method, Params: raw}
	data, _ := json.Marshal(msg)
	c.WriteMessage(websocket.TextMessage, data)
}

func TestCDPClient_CallAndResponse(t *testing.T) {
	mock := newMockCDP(t)
	defer mock.close()

	client, err := NewCDPClient(mock.wsURL())
	require.NoError(t, err)
	defer client.Close()

	mock.waitConn(t)

	// Call Navigate in background.
	var navigateErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		navigateErr = client.Navigate("https://example.com")
	})

	// Read the request and respond.
	req := mock.readMessage(t)
	assert.Equal(t, "Page.navigate", req.Method)

	var params map[string]string
	json.Unmarshal(req.Params, &params)
	assert.Equal(t, "https://example.com", params["url"])

	mock.sendResult(req.ID, map[string]string{"frameId": "abc"})

	wg.Wait()
	assert.NoError(t, navigateErr)
}

func TestCDPClient_StartScreencast(t *testing.T) {
	mock := newMockCDP(t)
	defer mock.close()

	client, err := NewCDPClient(mock.wsURL())
	require.NoError(t, err)
	defer client.Close()

	mock.waitConn(t)

	var startErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		startErr = client.StartScreencast(80, 1280, 720)
	})

	req := mock.readMessage(t)
	assert.Equal(t, "Page.startScreencast", req.Method)

	var params map[string]any
	json.Unmarshal(req.Params, &params)
	assert.Equal(t, "jpeg", params["format"])
	assert.Equal(t, float64(80), params["quality"])
	assert.Equal(t, float64(1280), params["maxWidth"])
	assert.Equal(t, float64(720), params["maxHeight"])

	mock.sendResult(req.ID, nil)

	wg.Wait()
	assert.NoError(t, startErr)
}

func TestCDPClient_ScreencastFrameAndAutoAck(t *testing.T) {
	mock := newMockCDP(t)
	defer mock.close()

	client, err := NewCDPClient(mock.wsURL())
	require.NoError(t, err)
	defer client.Close()

	mock.waitConn(t)

	frameCh := make(chan FrameEvent, 1)
	client.SetOnFrame(func(e FrameEvent) {
		frameCh <- e
	})

	// Send a screencast frame event from the mock server.
	mock.sendEvent("Page.screencastFrame", screencastFrameParams{
		Data: "base64data",
		Metadata: ScreencastMetadata{
			DeviceWidth:  1280,
			DeviceHeight: 720,
		},
		SessionID: 42,
	})

	// Should receive the frame.
	select {
	case frame := <-frameCh:
		assert.Equal(t, "base64data", frame.Data)
		assert.Equal(t, float64(1280), frame.Metadata.DeviceWidth)
		assert.Equal(t, 42, frame.SessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for frame")
	}

	// Should receive an auto-ack from the client.
	ack := mock.readMessage(t)
	assert.Equal(t, "Page.screencastFrameAck", ack.Method)

	var ackParams map[string]any
	json.Unmarshal(ack.Params, &ackParams)
	assert.Equal(t, float64(42), ackParams["sessionId"])
}

func TestCDPClient_DispatchMouseEvent(t *testing.T) {
	mock := newMockCDP(t)
	defer mock.close()

	client, err := NewCDPClient(mock.wsURL())
	require.NoError(t, err)
	defer client.Close()

	mock.waitConn(t)

	var dispatchErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		dispatchErr = client.DispatchMouseEvent("mousePressed", 100.5, 200.5, "left", 1, 0)
	})

	req := mock.readMessage(t)
	assert.Equal(t, "Input.dispatchMouseEvent", req.Method)

	var params map[string]any
	json.Unmarshal(req.Params, &params)
	assert.Equal(t, "mousePressed", params["type"])
	assert.Equal(t, 100.5, params["x"])
	assert.Equal(t, 200.5, params["y"])
	assert.Equal(t, "left", params["button"])
	assert.Equal(t, float64(1), params["clickCount"])

	mock.sendResult(req.ID, nil)
	wg.Wait()
	assert.NoError(t, dispatchErr)
}

func TestCDPClient_DispatchKeyEvent(t *testing.T) {
	mock := newMockCDP(t)
	defer mock.close()

	client, err := NewCDPClient(mock.wsURL())
	require.NoError(t, err)
	defer client.Close()

	mock.waitConn(t)

	var dispatchErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		dispatchErr = client.DispatchKeyEvent("keyDown", "Enter", "Enter", "\r", 0)
	})

	req := mock.readMessage(t)
	assert.Equal(t, "Input.dispatchKeyEvent", req.Method)

	var params map[string]any
	json.Unmarshal(req.Params, &params)
	assert.Equal(t, "keyDown", params["type"])
	assert.Equal(t, "Enter", params["key"])
	assert.Equal(t, "Enter", params["code"])
	assert.Equal(t, "\r", params["text"])

	mock.sendResult(req.ID, nil)
	wg.Wait()
	assert.NoError(t, dispatchErr)
}

func TestCDPClient_DispatchKeyEvent_OmitsEmptyFields(t *testing.T) {
	mock := newMockCDP(t)
	defer mock.close()

	client, err := NewCDPClient(mock.wsURL())
	require.NoError(t, err)
	defer client.Close()

	mock.waitConn(t)

	var wg sync.WaitGroup
	wg.Go(func() {
		client.DispatchKeyEvent("keyDown", "Shift", "", "", 0)
	})

	req := mock.readMessage(t)
	var params map[string]any
	json.Unmarshal(req.Params, &params)
	assert.Equal(t, "Shift", params["key"])
	_, hasCode := params["code"]
	_, hasText := params["text"]
	assert.False(t, hasCode, "empty code should be omitted")
	assert.False(t, hasText, "empty text should be omitted")

	mock.sendResult(req.ID, nil)
	wg.Wait()
}

func TestCDPClient_ConnectionClosed(t *testing.T) {
	mock := newMockCDP(t)

	client, err := NewCDPClient(mock.wsURL())
	require.NoError(t, err)

	mock.waitConn(t)

	// Close the server-side connection.
	mock.close()

	// call should return an error.
	err = client.Navigate("https://example.com")
	assert.Error(t, err)

	client.Close()
}

func TestCDPClient_ErrorResponse(t *testing.T) {
	mock := newMockCDP(t)
	defer mock.close()

	client, err := NewCDPClient(mock.wsURL())
	require.NoError(t, err)
	defer client.Close()

	mock.waitConn(t)

	var navigateErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		navigateErr = client.Navigate("not-a-url")
	})

	req := mock.readMessage(t)

	// Send an error response.
	errResp := cdpMessage{
		ID: req.ID,
		Error: &cdpError{
			Code:    -32000,
			Message: "Cannot navigate to invalid URL",
		},
	}
	data, _ := json.Marshal(errResp)
	mock.mu.Lock()
	mock.conn.WriteMessage(websocket.TextMessage, data)
	mock.mu.Unlock()

	wg.Wait()
	assert.Error(t, navigateErr)
	assert.Contains(t, navigateErr.Error(), "Cannot navigate to invalid URL")
}
