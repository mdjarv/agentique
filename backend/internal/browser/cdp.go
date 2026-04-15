package browser

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// CDPClient speaks the Chrome DevTools Protocol over WebSocket.
// It handles the JSON-RPC framing, response routing, and screencast auto-ack.
type CDPClient struct {
	ws     *websocket.Conn
	nextID atomic.Int64

	mu      sync.Mutex
	onFrame func(FrameEvent)
	pending map[int64]chan json.RawMessage
	done    chan struct{}

	wmu sync.Mutex // protects ws.WriteMessage
}

// FrameEvent is emitted for each screencast frame.
type FrameEvent struct {
	Data     string             `json:"data"`     // base64 JPEG
	Metadata ScreencastMetadata `json:"metadata"`
	// SessionID is the CDP screencast session ID used for ack.
	SessionID int `json:"sessionId"`
}

// ScreencastMetadata describes the viewport geometry of a screencast frame.
type ScreencastMetadata struct {
	OffsetTop       float64 `json:"offsetTop"`
	PageScaleFactor float64 `json:"pageScaleFactor"`
	DeviceWidth     float64 `json:"deviceWidth"`
	DeviceHeight    float64 `json:"deviceHeight"`
	ScrollOffsetX   float64 `json:"scrollOffsetX"`
	ScrollOffsetY   float64 `json:"scrollOffsetY"`
	Timestamp       float64 `json:"timestamp"`
}

// cdpMessage is the generic CDP JSON-RPC message structure.
type cdpMessage struct {
	ID     int64            `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *cdpError        `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// screencastFrameParams is the CDP Page.screencastFrame event payload.
type screencastFrameParams struct {
	Data      string             `json:"data"`
	Metadata  ScreencastMetadata `json:"metadata"`
	SessionID int                `json:"sessionId"`
}

// NewCDPClient creates a CDPClient connected to the given CDP WebSocket endpoint.
func NewCDPClient(cdpEndpoint string) (*CDPClient, error) {
	ws, _, err := websocket.DefaultDialer.Dial(cdpEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp connect: %w", err)
	}

	c := &CDPClient{
		ws:      ws,
		pending: make(map[int64]chan json.RawMessage),
		done:    make(chan struct{}),
	}

	go c.readLoop()
	return c, nil
}

// newCDPClientFromConn creates a CDPClient from an existing websocket connection (for testing).
func newCDPClientFromConn(ws *websocket.Conn) *CDPClient {
	c := &CDPClient{
		ws:      ws,
		pending: make(map[int64]chan json.RawMessage),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *CDPClient) readLoop() {
	defer close(c.done)
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}

		var msg cdpMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		if msg.Method == "Page.screencastFrame" {
			c.handleScreencastFrame(msg.Params)
			continue
		}

		// Route method responses to waiting callers.
		if msg.ID > 0 {
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.mu.Unlock()

			if ok {
				if msg.Error != nil {
					ch <- marshalError(msg.Error)
				} else {
					ch <- msg.Result
				}
			}
		}
	}
}

func marshalError(e *cdpError) json.RawMessage {
	// Encode as a JSON object with an __error field so callers can detect it.
	b, _ := json.Marshal(map[string]any{"__error": e.Message, "__code": e.Code})
	return b
}

func (c *CDPClient) handleScreencastFrame(params json.RawMessage) {
	var p screencastFrameParams
	if json.Unmarshal(params, &p) != nil {
		return
	}

	// Auto-ack the frame so Chrome keeps sending.
	c.ackScreencastFrame(p.SessionID)

	c.mu.Lock()
	fn := c.onFrame
	c.mu.Unlock()

	if fn != nil {
		fn(FrameEvent{
			Data:      p.Data,
			Metadata:  p.Metadata,
			SessionID: p.SessionID,
		})
	}
}

func (c *CDPClient) ackScreencastFrame(sessionID int) {
	// Fire-and-forget ack — don't wait for response.
	id := c.nextID.Add(1)
	params, _ := json.Marshal(map[string]any{"sessionId": sessionID})
	msg := cdpMessage{
		ID:     id,
		Method: "Page.screencastFrameAck",
		Params: params,
	}
	data, _ := json.Marshal(msg)
	c.wmu.Lock()
	c.ws.WriteMessage(websocket.TextMessage, data)
	c.wmu.Unlock()
}

// call sends a CDP method and waits for the response.
func (c *CDPClient) call(method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
	}

	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	msg := cdpMessage{
		ID:     id,
		Method: method,
		Params: rawParams,
	}
	data, _ := json.Marshal(msg)

	c.wmu.Lock()
	writeErr := c.ws.WriteMessage(websocket.TextMessage, data)
	c.wmu.Unlock()
	if writeErr != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("cdp write: %w", writeErr)
	}

	select {
	case result := <-ch:
		// Check if result is an error response.
		var errCheck struct {
			Error   string `json:"__error"`
			Code    int    `json:"__code"`
		}
		if json.Unmarshal(result, &errCheck) == nil && errCheck.Error != "" {
			return nil, fmt.Errorf("cdp %s: %s (code %d)", method, errCheck.Error, errCheck.Code)
		}
		return result, nil
	case <-c.done:
		return nil, fmt.Errorf("cdp connection closed")
	}
}

// SetOnFrame sets the callback for screencast frames.
func (c *CDPClient) SetOnFrame(fn func(FrameEvent)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onFrame = fn
}

// SetViewport sets the browser viewport size via Emulation.setDeviceMetricsOverride.
func (c *CDPClient) SetViewport(width, height int) error {
	_, err := c.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             width,
		"height":            height,
		"deviceScaleFactor": 1,
		"mobile":            false,
	})
	return err
}

// StartScreencast begins streaming page screenshots via CDP.
func (c *CDPClient) StartScreencast(quality int, maxWidth, maxHeight int) error {
	_, err := c.call("Page.startScreencast", map[string]any{
		"format":    "jpeg",
		"quality":   quality,
		"maxWidth":  maxWidth,
		"maxHeight":  maxHeight,
	})
	return err
}

// StopScreencast stops the screencast stream.
func (c *CDPClient) StopScreencast() error {
	_, err := c.call("Page.stopScreencast", nil)
	return err
}

// DispatchMouseEvent sends a mouse event to the page.
func (c *CDPClient) DispatchMouseEvent(typ string, x, y float64, button string, clickCount int, modifiers int) error {
	_, err := c.call("Input.dispatchMouseEvent", map[string]any{
		"type":       typ,
		"x":          x,
		"y":          y,
		"button":     button,
		"clickCount": clickCount,
		"modifiers":  modifiers,
	})
	return err
}

// DispatchKeyEvent sends a keyboard event to the page.
func (c *CDPClient) DispatchKeyEvent(typ string, key, code, text string, modifiers int) error {
	params := map[string]any{
		"type":      typ,
		"modifiers": modifiers,
	}
	if key != "" {
		params["key"] = key
	}
	if code != "" {
		params["code"] = code
	}
	if text != "" {
		params["text"] = text
	}
	_, err := c.call("Input.dispatchKeyEvent", params)
	return err
}

// Navigate loads a URL in the browser.
func (c *CDPClient) Navigate(url string) error {
	_, err := c.call("Page.navigate", map[string]any{
		"url": url,
	})
	return err
}

// GetCurrentURL returns the URL of the current page by querying the navigation history.
func (c *CDPClient) GetCurrentURL() string {
	result, err := c.call("Page.getNavigationHistory", nil)
	if err != nil {
		return ""
	}
	var hist struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(result, &hist); err != nil {
		return ""
	}
	if hist.CurrentIndex >= 0 && hist.CurrentIndex < len(hist.Entries) {
		return hist.Entries[hist.CurrentIndex].URL
	}
	return ""
}

// NavigateHistory moves back or forward in browser history.
// delta: -1 for back, 1 for forward.
func (c *CDPClient) NavigateHistory(delta int) error {
	// Get the navigation history to find current index.
	result, err := c.call("Page.getNavigationHistory", nil)
	if err != nil {
		return err
	}
	var hist struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(result, &hist); err != nil {
		return fmt.Errorf("parse navigation history: %w", err)
	}
	target := hist.CurrentIndex + delta
	if target < 0 || target >= len(hist.Entries) {
		return nil // nothing to navigate to
	}
	_, err = c.call("Page.navigateToHistoryEntry", map[string]any{
		"entryId": hist.Entries[target].ID,
	})
	return err
}

// Close shuts down the CDP connection.
func (c *CDPClient) Close() {
	c.ws.Close()
	<-c.done
}
