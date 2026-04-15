package server

import "github.com/mdjarv/agentique/backend/internal/session"

// sseListener adapts hub broadcasts into SSEEvent channel writes.
type sseListener struct {
	ch chan<- session.SSEEvent
}

func (l *sseListener) OnEvent(projectID, pushType string, payload any) {
	select {
	case l.ch <- session.SSEEvent{ProjectID: projectID, Type: pushType, Payload: payload}:
	default:
		// Drop event if channel is full (slow SSE client).
	}
}
