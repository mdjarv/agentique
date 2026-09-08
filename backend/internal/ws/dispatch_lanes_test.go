package ws

import (
	"testing"
	"time"
)

// installTestOp registers a handler under a throwaway op name on the given
// lane, and returns the cleanup. Tests in this package run serially, so the
// package-level registries can be edited in place.
func installTestOp(t *testing.T, name string, concurrent bool, h handlerFunc) {
	t.Helper()
	handlerRegistry[name] = h
	if concurrent {
		concurrentOps[name] = true
	}
	t.Cleanup(func() {
		delete(handlerRegistry, name)
		delete(concurrentOps, name)
	})
}

func newLaneTestConn() *conn {
	c := newDispatchTestConn()
	c.readSlots = make(chan struct{}, maxConcurrentReads)
	// The shared test conn keeps a four-deep queue to make overflow easy to
	// hit; a lane test enqueues a flood and needs the production depth.
	c.dispatchCh = make(chan ClientMessage, dispatchBufSize)
	return c
}

// A read on the read lane must not hold up a mutation queued behind it.
// This is the whole point of the lane: a project.fetch waiting on the
// network held a session history for 1.6s on the serial loop.
func TestReadLaneDoesNotBlockTheSerialLane(t *testing.T) {
	release := make(chan struct{})
	installTestOp(t, "test.slow-read", true, func(c *conn, msg ClientMessage) {
		<-release
		c.respond(msg.ID, "slow", "")
	})

	c := newLaneTestConn()
	defer c.close()
	go c.dispatchLoop()

	c.enqueueDispatch(ClientMessage{ID: "read", Type: "test.slow-read"})
	c.enqueueDispatch(ClientMessage{ID: "mutation", Type: "ping"})

	first := awaitResponse(c, time.Second)
	if first == nil || first.ID != "mutation" {
		t.Fatalf("first response = %+v, want the ping that was queued behind the blocked read", first)
	}
	close(release)
	second := awaitResponse(c, time.Second)
	if second == nil || second.ID != "read" {
		t.Fatalf("second response = %+v, want the released read", second)
	}
}

// Mutations keep arrival order among themselves: a slow one still holds the
// next, exactly as before the read lane existed.
func TestSerialLaneKeepsArrivalOrder(t *testing.T) {
	release := make(chan struct{})
	installTestOp(t, "test.slow-mutation", false, func(c *conn, msg ClientMessage) {
		<-release
		c.respond(msg.ID, "slow", "")
	})

	c := newLaneTestConn()
	defer c.close()
	go c.dispatchLoop()

	c.enqueueDispatch(ClientMessage{ID: "first", Type: "test.slow-mutation"})
	c.enqueueDispatch(ClientMessage{ID: "second", Type: "ping"})

	if early := awaitResponse(c, 100*time.Millisecond); early != nil {
		t.Fatalf("ping answered before the mutation ahead of it: %+v", early)
	}
	close(release)
	first := awaitResponse(c, time.Second)
	second := awaitResponse(c, time.Second)
	if first == nil || second == nil || first.ID != "first" || second.ID != "second" {
		t.Fatalf("order = %v, %v; want first, second", first, second)
	}
}

// The read lane is bounded: once every slot is held, the loop waits for one
// rather than spawning without limit — so a flood of reads is absorbed by the
// queue, and a mutation behind it waits for one slot, not the whole flood.
func TestReadLaneIsBounded(t *testing.T) {
	release := make(chan struct{})
	installTestOp(t, "test.slow-read", true, func(c *conn, msg ClientMessage) {
		<-release
		c.respond(msg.ID, "slow", "")
	})

	c := newLaneTestConn()
	defer c.close()
	go c.dispatchLoop()

	for i := 0; i < maxConcurrentReads+1; i++ {
		c.enqueueDispatch(ClientMessage{ID: "read", Type: "test.slow-read"})
	}
	c.enqueueDispatch(ClientMessage{ID: "mutation", Type: "ping"})

	if early := awaitResponse(c, 100*time.Millisecond); early != nil {
		t.Fatalf("answered while every read slot was held: %+v", early)
	}
	close(release)
	got := map[string]int{}
	for i := 0; i < maxConcurrentReads+2; i++ {
		resp := awaitResponse(c, time.Second)
		if resp == nil {
			t.Fatalf("only %d of %d responses arrived after release", i, maxConcurrentReads+2)
		}
		got[resp.ID]++
	}
	if got["read"] != maxConcurrentReads+1 || got["mutation"] != 1 {
		t.Fatalf("responses = %v", got)
	}
}

// A conn built without read slots (older tests, or a bare construction)
// runs read-lane handlers inline rather than deadlocking on a nil channel.
func TestReadLaneWithoutSlotsRunsInline(t *testing.T) {
	installTestOp(t, "test.read", true, func(c *conn, msg ClientMessage) {
		c.respond(msg.ID, "ok", "")
	})
	c := newDispatchTestConn() // no readSlots
	defer c.close()
	go c.dispatchLoop()

	c.enqueueDispatch(ClientMessage{ID: "1", Type: "test.read"})
	if resp := awaitResponse(c, time.Second); resp == nil || resp.ID != "1" {
		t.Fatalf("response = %+v", resp)
	}
}

// Every op on the read lane must be a registered handler; a typo here would
// silently put nothing on the lane.
func TestConcurrentOpsAreRegistered(t *testing.T) {
	for op := range concurrentOps {
		if _, ok := handlerRegistry[op]; !ok {
			t.Errorf("concurrentOps names %q, which has no handler", op)
		}
	}
}
