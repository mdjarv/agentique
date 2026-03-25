package ws

import "encoding/json"

type Validatable interface {
	Validate() error
}

func handleRequest[P any, R any](c *conn, msg ClientMessage, fn func(P) (R, error)) {
	var payload P
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if v, ok := any(&payload).(Validatable); ok {
		if err := v.Validate(); err != nil {
			c.respond(msg.ID, nil, err.Error())
			return
		}
	}
	result, err := fn(payload)
	if err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, result, "")
}
