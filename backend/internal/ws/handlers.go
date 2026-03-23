package ws

import (
	"encoding/json"

	"github.com/allbin/agentique/backend/internal/session"
)

func (c *conn) handleProjectSubscribe(msg ClientMessage) {
	var payload ProjectSubscribePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.ProjectID == "" {
		c.respond(msg.ID, nil, "projectId is required")
		return
	}

	c.hub.Subscribe(payload.ProjectID, c)
	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionCreate(msg ClientMessage) {
	var payload SessionCreatePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.ProjectID == "" {
		c.respond(msg.ID, nil, "projectId is required")
		return
	}

	result, err := c.svc.CreateSession(c.ctx, session.CreateSessionParams{
		ProjectID: payload.ProjectID,
		Name:      payload.Name,
		Worktree:  payload.Worktree,
		Branch:    payload.Branch,
		RequestID: msg.ID,
	})
	if err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}

	c.respond(msg.ID, result, "")
}

func (c *conn) handleSessionQuery(msg ClientMessage) {
	var payload SessionQueryPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.SessionID == "" || payload.Prompt == "" {
		c.respond(msg.ID, nil, "sessionId and prompt are required")
		return
	}

	if err := c.svc.QuerySession(c.ctx, payload.SessionID, payload.Prompt, payload.Attachments); err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}

	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionList(msg ClientMessage) {
	var payload SessionListPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.ProjectID == "" {
		c.respond(msg.ID, nil, "projectId is required")
		return
	}

	result, err := c.svc.ListSessions(c.ctx, payload.ProjectID)
	if err != nil {
		c.respond(msg.ID, nil, "failed to list sessions: "+err.Error())
		return
	}

	c.respond(msg.ID, result, "")
}

func (c *conn) handleSessionStop(msg ClientMessage) {
	var payload SessionStopPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}

	if err := c.svc.StopSession(c.ctx, payload.SessionID); err != nil {
		c.respond(msg.ID, nil, "failed to stop session: "+err.Error())
		return
	}

	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionHistory(msg ClientMessage) {
	var payload SessionHistoryPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}

	result, err := c.svc.GetHistory(c.ctx, payload.SessionID)
	if err != nil {
		c.respond(msg.ID, nil, "failed to load history: "+err.Error())
		return
	}

	c.respond(msg.ID, result, "")
}
