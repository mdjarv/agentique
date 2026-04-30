package ws

import "github.com/allbin/agentkit/eventbus"

// connSubscriber adapts a *conn to the eventbus.Subscriber interface.
//
// OnEvent is non-blocking: it forwards the event onto the conn's send buffer
// via push(). If the buffer is full, push() closes the connection — the same
// back-pressure behavior the previous ws.Hub had.
//
// OnEvent may be invoked concurrently from multiple goroutines. The conn's
// send() guards its own state, so this is safe.
type connSubscriber struct{ c *conn }

func (s *connSubscriber) OnEvent(e eventbus.Event) {
	s.c.push(e.Type, e.Payload)
}
