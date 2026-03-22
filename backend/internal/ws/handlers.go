package ws

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/allbin/agentique/backend/internal/session"
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

	workDir := project.Path
	var worktreePath, worktreeBranch string

	if payload.Worktree {
		branch := payload.Branch
		if branch == "" {
			// Auto-generate branch name using a short prefix.
			branch = "session-" + msg.ID
			if len(branch) > 30 {
				branch = branch[:30]
			}
		}
		worktreeBranch = branch
		worktreePath = session.WorktreePath(project.Name, branch)
		if err := session.CreateWorktree(project.Path, branch, worktreePath); err != nil {
			log.Printf("worktree create error: %v", err)
			c.respond(msg.ID, nil, "failed to create worktree: "+err.Error())
			return
		}
		workDir = worktreePath
	}

	// Auto-generate name if empty.
	name := payload.Name
	if name == "" {
		sessions, listErr := c.mgr.ListByProject(c.ctx, payload.ProjectID)
		count := 0
		if listErr == nil {
			count = len(sessions)
		}
		name = fmt.Sprintf("Session %d", count+1)
	}

	params := session.CreateParams{
		ProjectID:      payload.ProjectID,
		Name:           name,
		WorkDir:        workDir,
		WorktreePath:   worktreePath,
		WorktreeBranch: worktreeBranch,
	}

	sess, err := c.mgr.Create(c.ctx, params,
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
		// If we created a worktree but session creation failed, clean up.
		if worktreePath != "" {
			session.RemoveWorktree(project.Path, worktreePath)
		}
		c.respond(msg.ID, nil, "failed to create session: "+err.Error())
		return
	}

	// Read the DB record to get the created_at timestamp.
	dbSess, dbErr := c.queries.GetSession(c.ctx, sess.ID)
	createdAt := ""
	if dbErr == nil {
		createdAt = dbSess.CreatedAt
	}

	c.respond(msg.ID, SessionCreateResult{
		SessionID:      sess.ID,
		Name:           name,
		State:          sess.State(),
		WorktreePath:   worktreePath,
		WorktreeBranch: worktreeBranch,
		CreatedAt:      createdAt,
	}, "")
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

	sessions, err := c.mgr.ListByProject(c.ctx, payload.ProjectID)
	if err != nil {
		c.respond(msg.ID, nil, "failed to list sessions: "+err.Error())
		return
	}

	infos := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		info := SessionInfo{
			ID:        s.ID,
			Name:      s.Name,
			State:     s.State,
			CreatedAt: s.CreatedAt,
		}
		if s.WorktreePath.Valid {
			info.WorktreePath = s.WorktreePath.String
		}
		if s.WorktreeBranch.Valid {
			info.WorktreeBranch = s.WorktreeBranch.String
		}
		infos = append(infos, info)
	}

	c.respond(msg.ID, SessionListResult{Sessions: infos}, "")
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

	if err := c.mgr.Stop(c.ctx, payload.SessionID); err != nil {
		c.respond(msg.ID, nil, "failed to stop session: "+err.Error())
		return
	}

	c.respond(msg.ID, struct{}{}, "")
}

func (c *conn) handleSessionSubscribe(msg ClientMessage) {
	var payload SessionSubscribePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.respond(msg.ID, nil, "invalid payload")
		return
	}

	if payload.SessionID == "" {
		c.respond(msg.ID, nil, "sessionId is required")
		return
	}

	sess := c.mgr.Get(payload.SessionID)
	if sess == nil {
		c.respond(msg.ID, nil, "session not active")
		return
	}

	// Adopt this session's callbacks to this connection.
	sess.SetCallbacks(
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

	// Push the current state immediately so the frontend is in sync.
	c.push("session.state", SessionStatePayload{
		SessionID: payload.SessionID,
		State:     sess.State(),
	})

	c.respond(msg.ID, struct{}{}, "")
}
