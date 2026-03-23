package ws

import (
	"encoding/json"

	"github.com/allbin/agentique/backend/internal/session"
)

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

type ProjectSubscribePayload struct {
	ProjectID string `json:"projectId"`
}

type SessionCreatePayload struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Worktree  bool   `json:"worktree"`
	Branch    string `json:"branch"`
}

type SessionQueryPayload struct {
	SessionID   string                    `json:"sessionId"`
	Prompt      string                    `json:"prompt"`
	Attachments []session.QueryAttachment `json:"attachments,omitempty"`
}

type SessionListPayload struct {
	ProjectID string `json:"projectId"`
}

type SessionStopPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionHistoryPayload struct {
	SessionID string `json:"sessionId"`
}
