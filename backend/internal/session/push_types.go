package session

import (
	"encoding/json"

	"github.com/allbin/agentique/backend/internal/browser"
)

// Typed push event payloads.
//
// Each struct corresponds to a push event type broadcast over WebSocket.
// JSON tags MUST match the frontend schemas in ws-push-schemas.ts.

// PushSessionEvent wraps a wire event for a given session.
type PushSessionEvent struct {
	SessionID string `json:"sessionId"`
	Event     any    `json:"event"`
}

// PushTurnStarted signals a new turn has begun.
type PushTurnStarted struct {
	SessionID   string            `json:"sessionId"`
	Prompt      string            `json:"prompt"`
	Attachments []QueryAttachment `json:"attachments,omitempty"`
}

// PushSessionRenamed signals a session name change.
type PushSessionRenamed struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
}

// PushSessionDeleted signals a session was deleted.
type PushSessionDeleted struct {
	SessionID string `json:"sessionId"`
}

// PushPRUpdated signals a PR URL change.
type PushPRUpdated struct {
	SessionID string `json:"sessionId"`
	PrUrl     string `json:"prUrl"`
}

// PushToolPermission requests user approval for a tool invocation.
type PushToolPermission struct {
	SessionID  string          `json:"sessionId"`
	ApprovalID string          `json:"approvalId"`
	ToolName   string          `json:"toolName"`
	Input      json.RawMessage `json:"input"`
}

// PushApprovalResolved signals a tool approval was resolved.
type PushApprovalResolved struct {
	SessionID  string `json:"sessionId"`
	ApprovalID string `json:"approvalId"`
}

// PushPermissionModeChanged signals a permission mode transition.
type PushPermissionModeChanged struct {
	SessionID      string `json:"sessionId"`
	PermissionMode string `json:"permissionMode"`
}

// PushUserQuestion requests user input for an AskUserQuestion.
type PushUserQuestion struct {
	SessionID  string         `json:"sessionId"`
	QuestionID string         `json:"questionId"`
	Questions  []WireQuestion `json:"questions"`
}

// PushQuestionResolved signals a user question was answered.
type PushQuestionResolved struct {
	SessionID  string `json:"sessionId"`
	QuestionID string `json:"questionId"`
}

// PushProjectGitStatus broadcasts project-level git status.
type PushProjectGitStatus struct {
	ProjectID        string `json:"projectId"`
	Branch           string `json:"branch"`
	UncommittedCount int    `json:"uncommittedCount"`
	HasRemote        bool   `json:"hasRemote"`
	AheadRemote      int    `json:"aheadRemote"`
	BehindRemote     int    `json:"behindRemote"`
}

// PushChannelDeleted signals a channel was deleted or dissolved.
type PushChannelDeleted struct {
	ChannelID string `json:"channelId"`
}

// PushChannelMemberJoined signals a session joined a channel.
type PushChannelMemberJoined struct {
	ChannelID string        `json:"channelId"`
	Member    ChannelMember `json:"member"`
	Channel   *ChannelInfo  `json:"channel,omitempty"`
}

// PushChannelMemberLeft signals a session left a channel.
type PushChannelMemberLeft struct {
	ChannelID string `json:"channelId"`
	SessionID string `json:"sessionId"`
}

// PushIDOnly is a generic payload carrying a single ID field.
type PushIDOnly struct {
	ID string `json:"id"`
}

// PushBrowserFrame delivers a screencast frame.
type PushBrowserFrame struct {
	SessionID string                     `json:"sessionId"`
	Data      string                     `json:"data"`
	Metadata  browser.ScreencastMetadata `json:"metadata"`
}

// PushBrowserStopped signals the browser was stopped.
type PushBrowserStopped struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}
