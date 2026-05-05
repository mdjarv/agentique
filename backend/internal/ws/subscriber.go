package ws

import "github.com/allbin/agentkit/eventbus"

// connSubscriber adapts a *conn to the eventbus.Subscriber interface.
//
// The conn holds a single bus.SubscribeAll handle, so OnEvent receives every
// published event. Topical events (non-empty Topic) are forwarded only when
// the conn has joined that project; global broadcasts (empty Topic) always
// pass through. Forwarding any matched event onto the conn's send buffer via
// push() is non-blocking — if the buffer is full, push() closes the
// connection (the same back-pressure behavior the previous ws.Hub had).
//
// OnEvent may be invoked concurrently from multiple goroutines. The conn's
// send() guards its own state, so this is safe.
type connSubscriber struct{ c *conn }

func (s *connSubscriber) OnEvent(e eventbus.Event) {
	if e.Topic != "" && !s.c.hasProject(e.Topic) {
		return
	}
	s.c.push(e.Type, e.Payload)
}
