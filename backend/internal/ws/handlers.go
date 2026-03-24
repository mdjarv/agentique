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
		ProjectID:   payload.ProjectID,
		Name:        payload.Name,
		Worktree:    payload.Worktree,
		Branch:      payload.Branch,
		Model:       payload.Model,
		PlanMode:    payload.PlanMode,
		AutoApprove: payload.AutoApprove,
		RequestID:   msg.ID,
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

func (c *conn) handleSessionDiff(msg ClientMessage) {
	var payload SessionDiffPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}

	result, err := c.gitSvc.Diff(c.ctx, payload.SessionID)
	if err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}

	c.respond(msg.ID, result, "")
}

func (c *conn) handleSessionInterrupt(msg ClientMessage) {
	var payload SessionInterruptPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}

	if err := c.svc.InterruptSession(payload.SessionID); err != nil {
		c.respond(msg.ID, nil, "interrupt failed: "+err.Error())
		return
	}

	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionMerge(msg ClientMessage) {
	var payload SessionMergePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}
	result, err := c.gitSvc.Merge(c.ctx, payload.SessionID, payload.Cleanup)
	if err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, result, "")
}

func (c *conn) handleSessionCreatePR(msg ClientMessage) {
	var payload SessionCreatePRPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}
	result, err := c.gitSvc.CreatePR(c.ctx, session.CreatePRParams{
		SessionID: payload.SessionID,
		Title:     payload.Title,
		Body:      payload.Body,
	})
	if err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, result, "")
}

func (c *conn) handleSessionDelete(msg ClientMessage) {
	var payload SessionDeletePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}
	if err := c.svc.DeleteSession(c.ctx, payload.SessionID); err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionRename(msg ClientMessage) {
	var payload SessionRenamePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" || payload.Name == "" {
		c.respond(msg.ID, nil, "sessionId and name are required")
		return
	}
	if err := c.svc.RenameSession(c.ctx, payload.SessionID, payload.Name); err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionSetModel(msg ClientMessage) {
	var payload SessionSetModelPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" || payload.Model == "" {
		c.respond(msg.ID, nil, "sessionId and model are required")
		return
	}
	if err := c.svc.SetSessionModel(c.ctx, payload.SessionID, payload.Model); err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionSetPermission(msg ClientMessage) {
	var payload SessionSetPermissionPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" || payload.Mode == "" {
		c.respond(msg.ID, nil, "sessionId and mode are required")
		return
	}
	if err := c.svc.SetPermissionMode(payload.SessionID, payload.Mode); err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionSetAutoApprove(msg ClientMessage) {
	var payload SessionSetAutoApprovePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}
	if err := c.svc.SetAutoApprove(payload.SessionID, payload.Enabled); err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionResolveApproval(msg ClientMessage) {
	var payload SessionResolveApprovalPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" || payload.ApprovalID == "" {
		c.respond(msg.ID, nil, "sessionId and approvalId are required")
		return
	}
	if err := c.svc.ResolveApproval(payload.SessionID, payload.ApprovalID, payload.Allow, payload.Message); err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionResolveQuestion(msg ClientMessage) {
	var payload SessionResolveQuestionPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" || payload.QuestionID == "" {
		c.respond(msg.ID, nil, "sessionId and questionId are required")
		return
	}
	if err := c.svc.ResolveQuestion(payload.SessionID, payload.QuestionID, payload.Answers); err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionCommit(msg ClientMessage) {
	var payload SessionCommitPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" || payload.Message == "" {
		c.respond(msg.ID, nil, "sessionId and message are required")
		return
	}
	result, err := c.gitSvc.Commit(c.ctx, payload.SessionID, payload.Message)
	if err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, result, "")
}

func (c *conn) handleSessionRebase(msg ClientMessage) {
	var payload SessionRebasePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}
	result, err := c.gitSvc.Rebase(c.ctx, payload.SessionID)
	if err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, result, "")
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

func (c *conn) handleSessionGeneratePRDesc(msg ClientMessage) {
	var payload SessionGeneratePRDescPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}
	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}
	result, err := c.gitSvc.GeneratePRDescription(c.ctx, payload.SessionID)
	if err != nil {
		c.respond(msg.ID, nil, err.Error())
		return
	}
	c.respond(msg.ID, result, "")
}
