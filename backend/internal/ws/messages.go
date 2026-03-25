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
	ProjectID   string  `json:"projectId"`
	Name        string  `json:"name"`
	Worktree    bool    `json:"worktree"`
	Branch      string  `json:"branch"`
	Model       string  `json:"model"`
	PlanMode    bool    `json:"planMode"`
	AutoApprove bool    `json:"autoApprove"`
	Effort      string  `json:"effort"`
	MaxBudget   float64 `json:"maxBudget"`
	MaxTurns    int     `json:"maxTurns"`
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

type SessionDiffPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionInterruptPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionMergePayload struct {
	SessionID string `json:"sessionId"`
	Cleanup   bool   `json:"cleanup"`
}

type SessionCreatePRPayload struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

type SessionDeletePayload struct {
	SessionID string `json:"sessionId"`
}

type SessionSetModelPayload struct {
	SessionID string `json:"sessionId"`
	Model     string `json:"model"`
}

type SessionSetPermissionPayload struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"`
}

type SessionResolveApprovalPayload struct {
	SessionID  string `json:"sessionId"`
	ApprovalID string `json:"approvalId"`
	Allow      bool   `json:"allow"`
	Message    string `json:"message"`
}

type SessionSetAutoApprovePayload struct {
	SessionID string `json:"sessionId"`
	Enabled   bool   `json:"enabled"`
}

type SessionResolveQuestionPayload struct {
	SessionID  string            `json:"sessionId"`
	QuestionID string            `json:"questionId"`
	Answers    map[string]string `json:"answers"`
}

type SessionRenamePayload struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
}

type SessionCommitPayload struct {
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
}

type SessionRebasePayload struct {
	SessionID string `json:"sessionId"`
}

type SessionGeneratePRDescPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionGenerateCommitMsgPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionMarkDonePayload struct {
	SessionID string `json:"sessionId"`
}

type SessionCleanPayload struct {
	SessionID string `json:"sessionId"`
}
