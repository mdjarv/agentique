package ws

import (
	"context"
	"testing"
	"time"

	"github.com/allbin/agentkit/eventbus"
)

// drainAll reads everything available on c.sendCh up to a short deadline and
// returns the collected messages as ServerPush values (other types are ignored).
func drainPushes(c *conn, want int, timeout time.Duration) []ServerPush {
	out := make([]ServerPush, 0, want)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for len(out) < want {
		select {
		case msg := <-c.sendCh:
			if push, ok := msg.(ServerPush); ok {
				out = append(out, push)
			}
		case <-deadline.C:
			return out
		}
	}
	return out
}

// TestSubscribeProjectFansOutToMultipleProjects guards against a regression
// where a single ws.conn could only hold one project subscription at a time.
// The frontend subscribes to every project on every (re)connect, so dropping
// older subscriptions caused pushes to silently disappear for all but the
// most recently subscribed project.
func TestSubscribeProjectFansOutToMultipleProjects(t *testing.T) {
	bus := eventbus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &conn{
		ctx:    ctx,
		cancel: cancel,
		bus:    bus,
		sendCh: make(chan any, 16),
		subs:   make(map[string]*eventbus.Subscription),
	}
	defer c.unsubscribe()

	c.subscribeProject("project-A")
	c.subscribeProject("project-B")

	bus.Publish("project-A", "session.event", "from-A")
	bus.Publish("project-B", "session.event", "from-B")

	got := drainPushes(c, 2, 200*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("expected 2 pushes (one per project), got %d: %+v", len(got), got)
	}

	seen := map[string]bool{}
	for _, p := range got {
		s, ok := p.Payload.(string)
		if !ok {
			t.Fatalf("unexpected payload type %T", p.Payload)
		}
		seen[s] = true
	}
	if !seen["from-A"] || !seen["from-B"] {
		t.Fatalf("missing pushes — got %v", seen)
	}
}

// TestSubscribeProjectIdempotent verifies that re-subscribing to the same
// project replaces the prior handle (no duplicate deliveries) without
// affecting subscriptions to other projects.
func TestSubscribeProjectIdempotent(t *testing.T) {
	bus := eventbus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &conn{
		ctx:    ctx,
		cancel: cancel,
		bus:    bus,
		sendCh: make(chan any, 16),
		subs:   make(map[string]*eventbus.Subscription),
	}
	defer c.unsubscribe()

	c.subscribeProject("project-A")
	c.subscribeProject("project-B")
	c.subscribeProject("project-A") // re-subscribe replaces, doesn't duplicate

	bus.Publish("project-A", "session.event", "from-A")
	bus.Publish("project-B", "session.event", "from-B")

	got := drainPushes(c, 3, 200*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 pushes (no duplicates), got %d: %+v", len(got), got)
	}
}

// TestUnsubscribeReleasesAllProjects confirms conn teardown stops deliveries
// from every project the conn had subscribed to.
func TestUnsubscribeReleasesAllProjects(t *testing.T) {
	bus := eventbus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &conn{
		ctx:    ctx,
		cancel: cancel,
		bus:    bus,
		sendCh: make(chan any, 16),
		subs:   make(map[string]*eventbus.Subscription),
	}
	c.subscribeProject("project-A")
	c.subscribeProject("project-B")
	c.unsubscribe()

	bus.Publish("project-A", "session.event", "from-A")
	bus.Publish("project-B", "session.event", "from-B")

	got := drainPushes(c, 1, 100*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("expected no pushes after unsubscribe, got %d: %+v", len(got), got)
	}
}
