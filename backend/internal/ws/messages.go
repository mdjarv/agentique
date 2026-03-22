package ws

import "encoding/json"

// ClientMessage is the envelope for all client -> server messages.
type ClientMessage struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ServerResponse is sent back to the client correlated by ID.
type ServerResponse struct {
	ID      string     `json:"id"`
	Type    string     `json:"type"` // always "response"
	Payload any        `json:"payload,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

// ServerPush is a fire-and-forget event from server to client.
type ServerPush struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// ErrorBody is the error field in a ServerResponse.
type ErrorBody struct {
	Message string `json:"message"`
}

// --- Method payloads ---

type SessionCreatePayload struct {
	ProjectID string `json:"projectId"`
}

type SessionCreateResult struct {
	SessionID string `json:"sessionId"`
}

type SessionQueryPayload struct {
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
}

// --- Push event payloads ---

type SessionEventPayload struct {
	SessionID string `json:"sessionId"`
	Event     any    `json:"event"`
}

type SessionStatePayload struct {
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
}
