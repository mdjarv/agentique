package ws

import "github.com/allbin/agentkit/eventbus"

// connSubscriber adapts a *conn to the eventbus.Subscriber interface.
//
// The conn holds a multi-topic Subscription whose membership is mutated via
// AddTopic as the client joins projects, so the eventbus filters topical
// deliveries for us — OnEvent only fires for events on a joined topic or a
// Broadcast. Forwarding onto the conn's send buffer via push() is
// non-blocking — if the buffer is full, push() closes the connection (the
// same back-pressure behavior the previous ws.Hub had).
//
// OnEvent may be invoked concurrently from multiple goroutines. The conn's
// send() guards its own state, so this is safe.
type connSubscriber struct{ c *conn }

func (s *connSubscriber) OnEvent(e eventbus.Event) {
	s.c.push(e.Type, e.Payload)
}
