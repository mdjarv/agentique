package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newWSPair dials a real websocket against an httptest server and returns
// both ends. The close-path tests need a genuine socket: the regression they
// pin is a read loop blocked in ReadJSON that only an actual ws.Close can
// unblock.
func newWSPair(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()
	upgraded := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		upgraded <- c
	}))
	t.Cleanup(srv.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case server = <-upgraded:
	case <-time.After(time.Second):
		t.Fatal("server side never upgraded")
	}
	return server, client
}

func newSocketTestConn(ws *websocket.Conn) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &conn{
		ctx:             ctx,
		cancel:          cancel,
		ws:              ws,
		sendCh:          make(chan any, 1),
		dispatchCh:      make(chan ClientMessage, 4),
		maxMessageBytes: defaultMaxMessageBytes,
	}
}

// A full send buffer must actually close the connection. cancel() alone never
// reached the read loop — it sits blocked in ReadJSON, which no context
// observes — so a zombie conn kept reading and executing RPCs whose responses
// were dropped, for up to pongTimeout. Only ws.Close unblocks it.
func TestSendFullBufferUnblocksReadLoop(t *testing.T) {
	server, _ := newWSPair(t)
	c := newSocketTestConn(server)

	readDone := make(chan struct{})
	go func() {
		c.readLoop()
		close(readDone)
	}()

	// No writeLoop draining: the first send fills the 1-slot buffer, the
	// second overflows and must close the conn.
	c.send("fills the buffer")
	c.send("overflows")

	select {
	case <-c.ctx.Done():
	default:
		t.Fatal("overflow must cancel the conn context")
	}
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("read loop still blocked after overflow close — zombie conn")
	}
}

// A write error must close the conn, not just end the write loop. Returning
// bare left the same zombie shape as send(): a read loop blocked on a socket
// nothing will ever answer on.
func TestWriteErrorClosesConn(t *testing.T) {
	server, _ := newWSPair(t)
	c := newSocketTestConn(server)

	go c.writeLoop()

	// Kill the transport under the websocket so the next write fails
	// deterministically.
	_ = server.UnderlyingConn().Close()
	c.send("write into the void")

	select {
	case <-c.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("write error did not close the conn")
	}
}
