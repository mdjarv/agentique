package ws

import (
	"encoding/json"
	"log"
)

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

	project, err := c.queries.GetProject(c.ctx, payload.ProjectID)
	if err != nil {
		c.respond(msg.ID, nil, "project not found")
		return
	}

	sess, err := c.mgr.Create(c.ctx, project.Path,
		func(sessionID string, event any) {
			c.push("session.event", SessionEventPayload{
				SessionID: sessionID,
				Event:     event,
			})
		},
		func(sessionID string, state string) {
			c.push("session.state", SessionStatePayload{
				SessionID: sessionID,
				State:     state,
			})
		},
	)
	if err != nil {
		log.Printf("session create error: %v", err)
		c.respond(msg.ID, nil, "failed to create session: "+err.Error())
		return
	}

	c.respond(msg.ID, SessionCreateResult{SessionID: sess.ID}, "")
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

	sess := c.mgr.Get(payload.SessionID)
	if sess == nil {
		c.respond(msg.ID, nil, "session not found")
		return
	}

	if err := sess.Query(c.ctx, payload.Prompt); err != nil {
		c.respond(msg.ID, nil, "query failed: "+err.Error())
		return
	}

	c.respond(msg.ID, struct{}{}, "")
}
