package ws

import (
	"encoding/json"
	"log/slog"
)

type Validatable interface {
	Validate() error
}

func handleRequest[P any, R any](c *conn, msg ClientMessage, fn func(P) (R, error)) {
	var payload P
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		slog.Warn("ws request failed", "type", msg.Type, "id", msg.ID, "error", "invalid payload: "+err.Error())
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if v, ok := any(&payload).(Validatable); ok {
		if err := v.Validate(); err != nil {
			slog.Warn("ws request failed", "type", msg.Type, "id", msg.ID, "error", err)
			c.respond(msg.ID, nil, err.Error())
			return
		}
	}
	result, err := fn(payload)
	if err != nil {
		slog.Warn("ws request failed", "type", msg.Type, "id", msg.ID, "error", err)
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, result, "")
}
