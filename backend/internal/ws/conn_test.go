package ws

import (
	"context"
	"testing"
	"time"

	"github.com/allbin/agentkit/eventbus"
)

// drainPushes reads everything available on c.sendCh up to a short deadline
// and returns the collected ServerPush messages (other types are ignored).
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

func newTestConn(bus *eventbus.Bus) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{
		ctx:    ctx,
		cancel: cancel,
		bus:    bus,
		sendCh: make(chan any, 32),
	}
	c.sub = bus.SubscribeTopics(nil, &connSubscriber{c: c})
	return c
}

// TestSubscribeProjectFansOutToMultipleProjects guards against the regression
// where a single ws.conn could only hold one project subscription at a time.
// The frontend joins every project on every (re)connect, so dropping older
// subscriptions caused pushes to silently disappear for all but the most
// recently subscribed project.
func TestSubscribeProjectFansOutToMultipleProjects(t *testing.T) {
	bus := eventbus.New()
	c := newTestConn(bus)
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

// TestSubscribeProjectFiltersOutUnjoinedProjects confirms that events for
// projects the conn has not joined are not forwarded.
func TestSubscribeProjectFiltersOutUnjoinedProjects(t *testing.T) {
	bus := eventbus.New()
	c := newTestConn(bus)
	defer c.unsubscribe()

	c.subscribeProject("project-A")

	bus.Publish("project-A", "session.event", "from-A")
	bus.Publish("project-B", "session.event", "from-B") // not joined

	got := drainPushes(c, 2, 150*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 push (B is not joined), got %d: %+v", len(got), got)
	}
	if got[0].Payload != "from-A" {
		t.Fatalf("expected from-A, got %v", got[0].Payload)
	}
}

// TestSubscribeProjectIdempotent verifies that re-joining the same project
// does not cause duplicate deliveries.
func TestSubscribeProjectIdempotent(t *testing.T) {
	bus := eventbus.New()
	c := newTestConn(bus)
	defer c.unsubscribe()

	c.subscribeProject("project-A")
	c.subscribeProject("project-A") // re-join is a no-op

	bus.Publish("project-A", "session.event", "from-A")

	got := drainPushes(c, 2, 150*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 push (no duplicates from re-join), got %d: %+v", len(got), got)
	}
}

// TestBroadcastReachesConnExactlyOnce guards against duplicate delivery of
// global events (eventbus.Broadcast — used for team.*, agent-profile.*,
// persona.interaction) when the conn has joined multiple projects. The
// multi-topic subscription delivers Broadcast exactly once regardless of
// topic-set size.
func TestBroadcastReachesConnExactlyOnce(t *testing.T) {
	bus := eventbus.New()
	c := newTestConn(bus)
	defer c.unsubscribe()

	c.subscribeProject("project-A")
	c.subscribeProject("project-B")
	c.subscribeProject("project-C")

	bus.Broadcast("team.created", "team-payload")

	got := drainPushes(c, 2, 150*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 push from Broadcast, got %d: %+v", len(got), got)
	}
	if got[0].Type != "team.created" {
		t.Fatalf("unexpected push type %q", got[0].Type)
	}
}

// TestUnsubscribeReleasesAllProjects confirms conn teardown stops deliveries
// from every project the conn had joined and from global broadcasts.
func TestUnsubscribeReleasesAllProjects(t *testing.T) {
	bus := eventbus.New()
	c := newTestConn(bus)

	c.subscribeProject("project-A")
	c.subscribeProject("project-B")
	c.unsubscribe()

	bus.Publish("project-A", "session.event", "from-A")
	bus.Publish("project-B", "session.event", "from-B")
	bus.Broadcast("team.created", "team-payload")

	got := drainPushes(c, 1, 100*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("expected no pushes after unsubscribe, got %d: %+v", len(got), got)
	}
}
