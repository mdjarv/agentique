package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/allbin/agentkit/eventbus"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// newRunTestConn builds a conn with everything run() touches: a real socket, a
// real subscription, and the read lane's slots.
func newRunTestConn(ws *websocket.Conn, bus *eventbus.Bus) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{
		ctx:             ctx,
		cancel:          cancel,
		ws:              ws,
		bus:             bus,
		sendCh:          make(chan any, sendBufSize),
		dispatchCh:      make(chan ClientMessage, dispatchBufSize),
		readSlots:       make(chan struct{}, maxConcurrentReads),
		maxMessageBytes: defaultMaxMessageBytes,
	}
	c.sub = bus.SubscribeTopics(nil, &connSubscriber{c: c})
	c.sub.AddTopic("")
	return c
}

func subscribeMsg(t *testing.T, id string) ClientMessage {
	t.Helper()
	payload, err := json.Marshal(ProjectSubscribePayload{ProjectID: uuid.NewString()})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return ClientMessage{ID: id, Type: "project.subscribe", Payload: payload}
}

// A request that reaches a handler after teardown has begun must find a conn it
// can still use. `unsubscribe` used to nil the subscription, and it ran before
// the context was cancelled, so a queued `project.subscribe` dereferenced the
// nil and took the process with it.
//
// The library already does the work the nil was pretending to do: AddTopic
// no-ops once the subscription is closed.
func TestSubscribeProjectAfterUnsubscribe(t *testing.T) {
	bus := eventbus.New()
	c := newTestConn(bus)

	c.unsubscribe()
	c.subscribeProject(uuid.NewString())

	// Nothing to observe beyond surviving the call — the topic is not joined,
	// which is the correct outcome for a conn on its way out.
	c.unsubscribe() // idempotent, and the second call must not panic either
}

// The crash in the field: a client dropped mid boot-burst with two hundred
// requests still queued, and teardown dismantled the conn while the dispatch
// loop was draining them.
func TestRunTearsDownWithRequestsStillQueued(t *testing.T) {
	server, client := newWSPair(t)
	c := newRunTestConn(server, eventbus.New())

	// Fill the queue the way a boot burst does, before anything drains it.
	for i := range 200 {
		if !c.enqueueDispatch(subscribeMsg(t, string(rune('a'+i%26)))) {
			t.Fatalf("enqueue %d refused with room in the queue", i)
		}
	}

	// The client is gone: the read loop ends at once and run() goes straight
	// into teardown with the backlog still there.
	_ = client.Close()

	done := make(chan struct{})
	go func() {
		c.run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after the client went away")
	}
}

// run() must not return while a handler it started is still working. The read
// lane spawns goroutines that outlive the dispatch loop, so waiting on the loop
// alone would leave them running against a conn that had finished tearing down.
func TestRunWaitsForReadLaneHandler(t *testing.T) {
	server, client := newWSPair(t)
	c := newRunTestConn(server, eventbus.New())

	// "wire.list" is a read-lane op (concurrentOps), so this runs on its own
	// goroutine off the dispatch loop — the one the teardown used to race.
	started := make(chan struct{})
	finished := make(chan struct{})
	restore := handlerRegistry["wire.list"]
	handlerRegistry["wire.list"] = func(*conn, ClientMessage) {
		close(started)
		time.Sleep(200 * time.Millisecond)
		close(finished)
	}
	t.Cleanup(func() { handlerRegistry["wire.list"] = restore })

	if !c.enqueueDispatch(ClientMessage{ID: "1", Type: "wire.list"}) {
		t.Fatal("enqueue refused with room in the queue")
	}

	done := make(chan struct{})
	go func() {
		c.run()
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("read-lane handler never ran")
	}

	// Drop the client while the handler is mid-flight.
	_ = client.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after the client went away")
	}
	select {
	case <-finished:
	default:
		t.Fatal("run returned while a read-lane handler was still running")
	}
}
