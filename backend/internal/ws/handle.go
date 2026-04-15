package ws

import (
	"context"
	"encoding/json"
	"log/slog"
)

type Validatable interface {
	Validate() error
}

// requestIDKey is the context key for the WS request ID.
type requestIDKey struct{}

// RequestIDFromContext returns the WS request ID from context, or "".
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

func handleRequest[P any, R any](c *conn, msg ClientMessage, fn func(context.Context, P) (R, error)) {
	doHandleRequest(c, msg, fn, slog.LevelWarn)
}

// handleRequestQuiet is like handleRequest but logs errors at debug level.
// Use for high-frequency endpoints where transient errors are expected (e.g. browser input).
func handleRequestQuiet[P any, R any](c *conn, msg ClientMessage, fn func(context.Context, P) (R, error)) {
	doHandleRequest(c, msg, fn, slog.LevelDebug)
}

func doHandleRequest[P any, R any](c *conn, msg ClientMessage, fn func(context.Context, P) (R, error), errLevel slog.Level) {
	ctx := context.WithValue(c.ctx, requestIDKey{}, msg.ID)
	logger := slog.With("type", msg.Type, "requestID", msg.ID)

	var payload P
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		logger.Log(ctx, errLevel, "ws request failed", "error", "invalid payload: "+err.Error())
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if v, ok := any(&payload).(Validatable); ok {
		if err := v.Validate(); err != nil {
			logger.Log(ctx, errLevel, "ws request failed", "error", err)
			c.respond(msg.ID, nil, err.Error())
			return
		}
	}
	result, err := fn(ctx, payload)
	if err != nil {
		logger.Log(ctx, errLevel, "ws request failed", "error", err)
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, result, "")
}
