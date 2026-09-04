package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/allbin/agentkit/eventbus"
)

func newDispatchTestConn() *conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{
		ctx:        ctx,
		cancel:     cancel,
		sendCh:     make(chan any, 32),
		dispatchCh: make(chan ClientMessage, 4),
	}
	c.sub = eventbus.New().SubscribeTopics(nil, &connSubscriber{c: c})
	return c
}

// awaitResponse reads sendCh until a ServerResponse arrives or the timeout
// elapses (nil on timeout).
func awaitResponse(c *conn, timeout time.Duration) *ServerResponse {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case msg := <-c.sendCh:
			if resp, ok := msg.(ServerResponse); ok {
				return &resp
			}
		case <-deadline.C:
			return nil
		}
	}
}

// Handlers must run off the read loop: while one ran inline, pongs sat
// unread and the read deadline went stale, so a handler slower than
// pongTimeout made the server tear down its own healthy socket. The
// dispatch loop is the seam — this pins that it executes what the read
// loop enqueues.
func TestDispatchLoopExecutesEnqueuedMessages(t *testing.T) {
	c := newDispatchTestConn()
	defer c.close()
	go c.dispatchLoop()

	if !c.enqueueDispatch(ClientMessage{ID: "1", Type: "ping"}) {
		t.Fatal("enqueue refused with room in the queue")
	}
	if !c.enqueueDispatch(ClientMessage{ID: "2", Type: "ping"}) {
		t.Fatal("enqueue refused with room in the queue")
	}

	first := awaitResponse(c, time.Second)
	if first == nil || first.ID != "1" {
		t.Fatalf("first response = %+v, want ID 1", first)
	}
	second := awaitResponse(c, time.Second)
	if second == nil || second.ID != "2" {
		t.Fatalf("second response = %+v, want ID 2 (arrival order preserved)", second)
	}
}

// A full dispatch queue closes the connection — the same contract send()
// applies — rather than blocking the read loop (which recreates the
// stale-deadline fault) or dropping the message (an RPC that silently never
// answers wedges its caller).
func TestEnqueueDispatchFullQueueClosesConn(t *testing.T) {
	c := newDispatchTestConn() // no dispatchLoop draining
	for i := 0; i < cap(c.dispatchCh); i++ {
		if !c.enqueueDispatch(ClientMessage{Type: "ping"}) {
			t.Fatalf("enqueue %d refused before the queue was full", i)
		}
	}

	if c.enqueueDispatch(ClientMessage{Type: "ping"}) {
		t.Fatal("enqueue on a full queue must tell the read loop to stop")
	}
	select {
	case <-c.ctx.Done():
	default:
		t.Fatal("a full dispatch queue must close the connection")
	}
}

// The msggen family blocks for tens of seconds (provider CLI call with retry
// sleeps); on the dispatch loop that stalled every RPC behind it — a stop, an
// approval answer. handleRequestAsync must return to its caller immediately
// and still deliver the response when the handler finishes.
func TestHandleRequestAsyncDoesNotBlockCaller(t *testing.T) {
	c := newDispatchTestConn()
	defer c.close()

	release := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		handleRequestAsync(c, ClientMessage{ID: "slow", Type: "test", Payload: json.RawMessage("{}")},
			func(_ context.Context, _ struct{}) (string, error) {
				<-release
				return "done", nil
			})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("handleRequestAsync blocked its caller while the handler ran")
	}

	close(release)
	resp := awaitResponse(c, time.Second)
	if resp == nil || resp.ID != "slow" || resp.Error != nil {
		t.Fatalf("response = %+v, want successful ID slow", resp)
	}
}

// A panic in an async handler must answer the request and spare the process —
// a synchronous handler gets that guard from net/http for free.
func TestHandleRequestAsyncRecoversPanic(t *testing.T) {
	c := newDispatchTestConn()
	defer c.close()

	handleRequestAsync(c, ClientMessage{ID: "boom", Type: "test", Payload: json.RawMessage("{}")},
		func(_ context.Context, _ struct{}) (string, error) {
			panic("handler exploded")
		})

	resp := awaitResponse(c, time.Second)
	if resp == nil || resp.ID != "boom" {
		t.Fatalf("response = %+v, want an answer for ID boom", resp)
	}
	if resp.Error == nil {
		t.Fatal("a panicking handler must answer with an error")
	}
}
