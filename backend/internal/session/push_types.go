package session

import (
	"encoding/json"

	"github.com/mdjarv/agentique/backend/internal/browser"
)

// Typed push event payloads.
//
// Each struct corresponds to a push event type broadcast over WebSocket.
// JSON tags MUST match the frontend schemas in ws-push-schemas.ts.

// PushSessionEvent wraps a wire event for a given session.
//
// Seq is a per-session, monotonic wire-sequence number stamped on every
// session.event broadcast (1-based; 0 means "unsequenced" — used only for the
// rare channel-message push to a session that isn't live). Epoch identifies the
// emitting pipeline's lifetime (a fresh value on every resume/rebuild). The
// frontend uses (epoch, seq) to detect gaps, drop duplicates/out-of-order
// events, and trigger a history resync when the pipeline was rebuilt — see the
// event-seq store. Seq resets to 0 within a new pipeline life, so Epoch is what
// disambiguates "the counter restarted" from "an old duplicate".
//
// Seq and Epoch carry omitempty because the generated Zod schema mirrors these
// tags, and a required field makes the client reject the WHOLE payload from a
// peer that does not send it — every session.event push from a paired machine
// on a pre-seq release silently dropped, its transcript frozen. Absent reads
// as 0 on the client: unsequenced, which is also what a stamped 0 means, so
// nothing is lost by omitting the zero value.
type PushSessionEvent struct {
	SessionID string `json:"sessionId"`
	Event     any    `json:"event"`
	Seq       int64  `json:"seq,omitempty"`
	Epoch     int64  `json:"epoch,omitempty"`
}

// PushTurnStarted signals a new turn has begun. TurnIndex is the persisted
// turn identity (allocated by the event pipeline, stable across reloads) —
// the anchor scheduled-run rows and deep-links key on.
//
// TurnIndex is 1-based (AdvanceTurn increments before returning), so omitempty
// never hides a real index — and it must stay optional on the wire for the
// same reason as PushSessionEvent.Seq: a required field rejects whole payloads
// from peers that predate it.
type PushTurnStarted struct {
	SessionID   string            `json:"sessionId"`
	Prompt      string            `json:"prompt"`
	Attachments []QueryAttachment `json:"attachments,omitempty"`
	TurnIndex   int               `json:"turnIndex,omitempty"`
	Origin      *QueryOrigin      `json:"origin,omitempty"`
}

// PushSessionRenamed signals a session name change.
type PushSessionRenamed struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
}

// PushSessionModelResolved carries the concrete model ID reported by the
// provider for this session.
type PushSessionModelResolved struct {
	SessionID     string `json:"sessionId"`
	ResolvedModel string `json:"resolvedModel"`
}

// PushSessionPinned signals a session pin-state change.
type PushSessionPinned struct {
	SessionID string `json:"sessionId"`
	Pinned    bool   `json:"pinned"`
	PinOrder  int64  `json:"pinOrder"`
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

// PushBrowserProvisioning signals progress while a Chromium is being installed
// on first browser use. State is one of "installing", "ready", "failed".
type PushBrowserProvisioning struct {
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
}

// ActivityItem is a single entry in the project activity feed.
// Covers both channel messages and significant session events.
type ActivityItem struct {
	Kind        string `json:"kind"`                  // "message" or "event"
	ItemID      string `json:"itemId"`                // message UUID or event row ID
	SourceID    string `json:"sourceId"`              // channel_id (message) or session_id (event)
	SourceName  string `json:"sourceName"`            // sender_name or session name
	Content     string `json:"content"`               // message text / tool name / error text
	EventType   string `json:"eventType"`             // message_type or event type (tool_use/result/error)
	Category    string `json:"category,omitempty"`    // tool category for tool_use events
	FilePath    string `json:"filePath,omitempty"`    // extracted file path for tool_use events
	ProjectSlug string `json:"projectSlug,omitempty"` // owning project slug ('' for project-less channels)
	CreatedAt   string `json:"createdAt"`
}

// PushSessionPulse carries a compact activity snapshot for a running session.
// Broadcast as "session.pulse" on a debounced ~2s interval while the session
// is actively processing events. In-memory only — not persisted.
type PushSessionPulse struct {
	SessionID        string `json:"sessionId"`
	LastToolCategory string `json:"lastToolCategory,omitempty"`
	LastFilePath     string `json:"lastFilePath,omitempty"`
	ToolCallCount    int    `json:"toolCallCount"`
	CommitCount      int    `json:"commitCount"`
	ErrorCount       int    `json:"errorCount"`
	TurnStartedAt    int64  `json:"turnStartedAt"` // epoch ms
	TodoTotal        int    `json:"todoTotal"`     // unique tasks seen
	TodoCompleted    int    `json:"todoCompleted"` // tasks with completed status
}
