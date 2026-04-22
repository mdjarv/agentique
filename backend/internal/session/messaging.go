package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/google/uuid"
)

// ChannelMCPServerName is the legacy stdio MCP server name for channel messaging.
// Used as a fallback when the HTTP MCP transport is not wired.
const ChannelMCPServerName = "agentique-channel"

// AgentiqueMCPServerName is the unified HTTP MCP server name. Tools live under
// the "mcp__agentique__" prefix when this transport is active.
const AgentiqueMCPServerName = "agentique"

// ChannelSendMessageTool is the full tool name for SendMessage on the legacy
// stdio transport. Permission interceptor keys on this string.
const ChannelSendMessageTool = "mcp__" + ChannelMCPServerName + "__SendMessage"

// AgentiqueSendMessageTool is the SendMessage tool name on the HTTP MCP transport.
const AgentiqueSendMessageTool = "mcp__" + AgentiqueMCPServerName + "__SendMessage"

// AgentiqueAcquireDevURLTool / Release / List — auto-allowed tools served via HTTP MCP.
const (
	AgentiqueAcquireDevURLTool = "mcp__" + AgentiqueMCPServerName + "__AcquireDevUrl"
	AgentiqueReleaseDevURLTool = "mcp__" + AgentiqueMCPServerName + "__ReleaseDevUrl"
	AgentiqueListDevURLsTool   = "mcp__" + AgentiqueMCPServerName + "__ListDevUrls"
)

// ChannelMCPConfig returns the MCP config JSON that starts the legacy stdio
// mcp-channel server. Used as fallback when HTTP MCP is not wired.
func ChannelMCPConfig() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "agentique" // fallback
	} else {
		exe, _ = filepath.EvalSymlinks(exe)
	}
	return fmt.Sprintf(`{"mcpServers":{%q:{"command":%q,"args":["mcp-channel"]}}}`,
		ChannelMCPServerName, exe)
}

// AgentiqueMCPHTTPConfig returns the MCP config JSON that connects Claude to
// the in-process /mcp endpoint over HTTP, authenticated with a per-session
// bearer token.
func AgentiqueMCPHTTPConfig(internalURL, token string) string {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			AgentiqueMCPServerName: map[string]any{
				"type": "http",
				"url":  internalURL,
				"headers": map[string]any{
					"Authorization": "Bearer " + token,
				},
			},
		},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		// Should not happen; return an empty config as last resort.
		return `{"mcpServers":{}}`
	}
	return string(b)
}

// SpawnWorkersRequest is the parsed body from SendMessage({to: "@spawn", ...}).
//
// If ChannelID is non-empty, the workers are added to that existing channel
// (sender must already be a lead there). If ChannelID is empty, a new channel
// is created using ChannelName (or a default derived from the sender's name).
type SpawnWorkersRequest struct {
	ChannelID   string             `json:"channelId,omitempty"`
	ChannelName string             `json:"channelName"`
	Workers     []SpawnWorkerEntry `json:"workers"`
}

// SpawnWorkerEntry describes a single worker to spawn.
type SpawnWorkerEntry struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Prompt string `json:"prompt"`
}

// SpawnDecision is the result of authorizing a SendMessage({to:"@spawn",...}).
type SpawnDecision int

const (
	// SpawnDecisionPrompt means the regular UI approval flow should run.
	SpawnDecisionPrompt SpawnDecision = iota
	// SpawnDecisionAuto means the sender is trusted (channel lead) — bypass UI
	// approval and execute the spawn directly.
	SpawnDecisionAuto
	// SpawnDecisionReject means the spawn is not permitted for this sender
	// (e.g. a worker asking to spawn). The intercepted SendMessage must be
	// denied with Reason as the message.
	SpawnDecisionReject
)

// SetAgentMessageCallback sets the callback for routing SendMessage tool invocations
// within a specific channel. Multiple channels can have callbacks simultaneously.
func (s *Session) SetAgentMessageCallback(channelID string, cb func(senderID, targetName, content, msgType string) error) {
	s.mu.Lock()
	if s.channel.agentMessageCallbacks == nil {
		s.channel.agentMessageCallbacks = make(map[string]func(string, string, string, string) error)
	}
	s.channel.agentMessageCallbacks[channelID] = cb
	s.mu.Unlock()
}

// RemoveAgentMessageCallback removes the callback for a specific channel.
func (s *Session) RemoveAgentMessageCallback(channelID string) {
	s.mu.Lock()
	delete(s.channel.agentMessageCallbacks, channelID)
	s.mu.Unlock()
}

// ClearAllAgentMessageCallbacks removes all channel callbacks.
func (s *Session) ClearAllAgentMessageCallbacks() {
	s.mu.Lock()
	s.channel.agentMessageCallbacks = nil
	s.mu.Unlock()
}

// SetPersonaContext configures persona query support for AskTeammate interception.
// Called after session creation when the session is bound to an agent profile in a team.
func (s *Session) SetPersonaContext(querier PersonaQuerier, profileID, profileName string, teammates map[string]teammateRef) {
	s.mu.Lock()
	s.persona.personaQuerier = querier
	s.persona.teamContext = &sessionTeamContext{
		agentProfileID:   profileID,
		agentProfileName: profileName,
		teammates:        teammates,
	}
	s.mu.Unlock()
}

func (s *Session) interceptAskTeammate(input json.RawMessage) (*claudecli.PermissionResponse, error) {
	var parsed struct {
		Name     string `json:"name"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("Failed to parse AskTeammate input: %v", err),
		}, nil
	}

	s.mu.Lock()
	querier := s.persona.personaQuerier
	tc := s.persona.teamContext
	s.mu.Unlock()

	if querier == nil || tc == nil {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: "AskTeammate is not available: session is not bound to a team.",
		}, nil
	}

	ref, ok := tc.teammates[strings.ToLower(parsed.Name)]
	if !ok {
		slog.Info("AskTeammate target not found",
			"session_id", s.ID, "asker", tc.agentProfileName, "target", parsed.Name)
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("No teammate named %q found in your team.", parsed.Name),
		}, nil
	}

	slog.Info("AskTeammate intercepted",
		"session_id", s.ID, "asker", tc.agentProfileName, "target", parsed.Name,
		"question_len", len(parsed.Question))

	response, err := querier.QueryForSession(s.ctx, parsed.Name, ref.teamID, tc.agentProfileID, tc.agentProfileName, parsed.Question)
	if err != nil {
		slog.Warn("AskTeammate persona query failed", "session_id", s.ID, "target", parsed.Name, "error", err)
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("Persona query failed for %q: %v", parsed.Name, err),
		}, nil
	}

	return &claudecli.PermissionResponse{
		Allow:       false,
		DenyMessage: response,
	}, nil
}

// SetSpawnWorkersCallback sets the callback for handling worker spawn requests.
func (s *Session) SetSpawnWorkersCallback(cb func(senderID string, req SpawnWorkersRequest) error) {
	s.mu.Lock()
	s.channel.onSpawnWorkers = cb
	s.mu.Unlock()
}

// SetSpawnAuthCallback sets the authorization callback for @spawn. The
// callback runs before any UI approval: it decides whether to auto-approve
// (channel lead), reject (worker), or fall back to the regular prompt flow
// (session not in any channel).
func (s *Session) SetSpawnAuthCallback(cb func(senderID string, req SpawnWorkersRequest) (SpawnDecision, string)) {
	s.mu.Lock()
	s.channel.onAuthorizeSpawn = cb
	s.mu.Unlock()
}

// SetDissolveChannelCallback sets the callback for handling channel dissolution requests.
func (s *Session) SetDissolveChannelCallback(cb func(senderID string) error) {
	s.mu.Lock()
	s.channel.onDissolveChannel = cb
	s.mu.Unlock()
}

// parseSendMessageInput extracts the target and body from a SendMessage tool
// call. It accepts both "content" (our preamble examples) and "message" (the
// actual tool schema), and handles "message" being either a JSON string or a
// raw JSON object (for @spawn payloads).
func parseSendMessageInput(input json.RawMessage) (to, body, msgType string, err error) {
	var parsed struct {
		To      string          `json:"to"`
		Content string          `json:"content"`
		Message json.RawMessage `json:"message"`
		Type    string          `json:"type"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return "", "", "", err
	}

	// Resolve the message body: prefer "content", fall back to "message".
	body = parsed.Content
	if body == "" && len(parsed.Message) > 0 {
		// "message" may be a JSON string or an object. Try to unquote as
		// string first; if that fails, use the raw JSON bytes directly.
		var msgStr string
		if json.Unmarshal(parsed.Message, &msgStr) == nil {
			body = msgStr
		} else {
			body = string(parsed.Message)
		}
	}

	// Unwrap JSON envelope: Claude sometimes wraps messages in a JSON object
	// like {"type":"plan","text":"actual message"} or {"content":"actual message"}.
	// Extract the text payload so the content renders as prose, not raw JSON.
	body = unwrapJSONEnvelope(body)

	return parsed.To, body, normalizeMessageType(parsed.Type), nil
}

// unwrapJSONEnvelope detects when a message body is a JSON object with a
// "text" or "content" field and extracts the actual text. This handles the
// common case where Claude embeds the message type inside the body:
//
//	{"type":"plan","text":"Here's my plan..."}
//
// Returns the original body unchanged if it's not a JSON envelope.
func unwrapJSONEnvelope(body string) string {
	if len(body) < 2 || body[0] != '{' {
		return body
	}
	var envelope struct {
		Text    string `json:"text"`
		Content string `json:"content"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &envelope) != nil {
		return body
	}
	if envelope.Text != "" {
		return envelope.Text
	}
	if envelope.Content != "" {
		return envelope.Content
	}
	if envelope.Message != "" {
		return envelope.Message
	}
	return body
}

// normalizeMessageType validates and lowercases a message type from tool input.
// Unknown types fall back to "message".
func normalizeMessageType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "plan":
		return "plan"
	case "progress":
		return "progress"
	case "done":
		return "done"
	case "message", "":
		return "message"
	default:
		slog.Warn("unknown message type, defaulting to message", "raw_type", raw)
		return "message"
	}
}

// interceptSendMessage handles the SendMessage tool by routing it through the
// channel messaging system. Returns a deny response with a success-like message
// so Claude thinks the message was delivered (v1 hack — proper tool result
// interception requires claudecli-go changes).
// Also intercepts "@spawn" target for worker delegation.
func (s *Session) interceptSendMessage(input json.RawMessage) (*claudecli.PermissionResponse, error) {
	to, body, _, err := parseSendMessageInput(input)
	if err != nil {
		slog.Warn("SendMessage parse failed", "session_id", s.ID, "error", err, "raw_input", string(input))
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("Failed to parse SendMessage input: %v", err),
		}, nil
	}

	slog.Debug("SendMessage intercepted", "session_id", s.ID, "to", to, "body_len", len(body))

	// Intercept @spawn for worker delegation.
	if to == "@spawn" {
		return s.interceptSpawnWorkers(body)
	}

	// Intercept @dissolve for channel dissolution.
	if to == "@dissolve" {
		return s.interceptDissolveChannel()
	}

	// Regular messages: deny here (to prevent CLI from processing internally)
	// but don't route — the EventPipeline's OnSendMessage callback handles
	// actual routing. This avoids double-delivery when both can_use_tool and
	// the pipeline fire for the same SendMessage.
	return &claudecli.PermissionResponse{
		Allow:       false,
		DenyMessage: fmt.Sprintf("Message delivered to %q successfully.", to),
	}, nil
}

// interceptSpawnWorkers handles SendMessage to "@spawn" — routes through the
// standard approval flow so the user can approve/deny worker creation.
func (s *Session) interceptSpawnWorkers(content string) (*claudecli.PermissionResponse, error) {
	var req SpawnWorkersRequest
	if err := json.Unmarshal([]byte(content), &req); err != nil {
		slog.Warn("spawn request parse failed",
			"session_id", s.ID,
			"error", err,
			"content_len", len(content),
			"content_preview", truncate(content, 200),
		)
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("Failed to parse spawn request: %v. Expected JSON with channelName and workers array.", err),
		}, nil
	}
	if len(req.Workers) == 0 {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: "Spawn request must include at least one worker.",
		}, nil
	}

	s.mu.Lock()
	cb := s.channel.onSpawnWorkers
	authCb := s.channel.onAuthorizeSpawn
	s.mu.Unlock()

	if cb == nil {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: "Worker spawning is not available for this session.",
		}, nil
	}

	// Ask the service whether this spawn can bypass UI approval, must be
	// rejected outright, or should fall through to the human prompt flow.
	decision := SpawnDecisionPrompt
	var rejectReason string
	if authCb != nil {
		decision, rejectReason = authCb(s.ID, req)
	}
	switch decision {
	case SpawnDecisionReject:
		slog.Info("spawn rejected by authorizer", "session_id", s.ID, "reason", rejectReason)
		msg := rejectReason
		if msg == "" {
			msg = "Worker spawning is not available for this session."
		}
		return &claudecli.PermissionResponse{Allow: false, DenyMessage: msg}, nil
	case SpawnDecisionAuto:
		slog.Info("spawn auto-approved (channel lead)", "session_id", s.ID, "workers", len(req.Workers))
		if err := cb(s.ID, req); err != nil {
			return &claudecli.PermissionResponse{
				Allow:       false,
				DenyMessage: fmt.Sprintf("Worker creation failed: %v", err),
			}, nil
		}
		var names []string
		for _, w := range req.Workers {
			names = append(names, w.Name)
		}
		return &claudecli.PermissionResponse{
			Allow: false,
			DenyMessage: fmt.Sprintf(
				"Auto-approved: spawned %d workers: %s. They are working in separate worktrees. "+
					"Each worker will message you shortly with their plan before starting work. "+
					"Wait for all workers to check in before proceeding.",
				len(req.Workers), strings.Join(names, ", ")),
		}, nil
	}

	// SpawnDecisionPrompt — existing UI approval flow.
	// Marshal the request as the tool input for the approval UI.
	inputJSON, _ := json.Marshal(req)

	approvalID := uuid.New().String()
	ch := make(chan *claudecli.PermissionResponse, 1)

	pa := &pendingApproval{
		id:       approvalID,
		toolName: "SpawnWorkers",
		input:    inputJSON,
		ch:       ch,
	}

	s.mu.Lock()
	s.pendingApprovals[approvalID] = pa
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pendingApprovals, approvalID)
		s.mu.Unlock()
	}()

	slog.Debug("spawn workers requested", "session_id", s.ID, "workers", len(req.Workers), "approval_id", approvalID)

	s.broadcast("session.tool-permission", PushToolPermission{
		SessionID: s.ID, ApprovalID: approvalID, ToolName: "SpawnWorkers", Input: json.RawMessage(inputJSON),
	})

	select {
	case resp := <-ch:
		if !resp.Allow {
			return &claudecli.PermissionResponse{
				Allow:       false,
				DenyMessage: "User denied worker creation.",
			}, nil
		}
		// User approved — create the workers.
		if err := cb(s.ID, req); err != nil {
			return &claudecli.PermissionResponse{
				Allow:       false,
				DenyMessage: fmt.Sprintf("Worker creation failed: %v", err),
			}, nil
		}
		var names []string
		for _, w := range req.Workers {
			names = append(names, w.Name)
		}
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf(
				"Successfully spawned %d workers: %s. They are working in separate worktrees. "+
					"Each worker will message you shortly with their plan before starting work. "+
					"Wait for all workers to check in before proceeding.",
				len(req.Workers), strings.Join(names, ", ")),
		}, nil
	case <-s.ctx.Done():
		return &claudecli.PermissionResponse{Allow: false, DenyMessage: "session closed"}, nil
	}
}

// interceptDissolveChannel handles SendMessage to "@dissolve" — dissolves the
// channel this leader session belongs to, cleaning up all workers.
func (s *Session) interceptDissolveChannel() (*claudecli.PermissionResponse, error) {
	s.mu.Lock()
	cb := s.channel.onDissolveChannel
	s.mu.Unlock()

	if cb == nil {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: "Channel dissolution is not available for this session.",
		}, nil
	}

	if err := cb(s.ID); err != nil {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("Channel dissolution failed: %v", err),
		}, nil
	}

	return &claudecli.PermissionResponse{
		Allow:       false,
		DenyMessage: "Channel dissolved successfully. All worker sessions have been stopped and cleaned up. You are no longer part of a channel.",
	}, nil
}
