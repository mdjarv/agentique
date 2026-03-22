package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	claudecli "github.com/allbin/claudecli-go"
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

	sess, err := c.mgr.Create(c.ctx, params)
	if err != nil {
		log.Printf("session create error: %v", err)
		if worktreePath != "" {
			session.RemoveWorktree(project.Path, worktreePath)
		}
		c.respond(msg.ID, nil, "failed to create session: "+err.Error())
		return
	}

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

	// Lazy resume: if session not live, try to resume from Claude session ID.
	if sess == nil {
		var err error
		sess, err = c.resumeSession(payload.SessionID)
		if err != nil {
			c.respond(msg.ID, nil, "session not found: "+err.Error())
			return
		}
	}

	if err := sess.Query(c.ctx, payload.Prompt, payload.Attachments); err != nil {
		c.respond(msg.ID, nil, "query failed: "+err.Error())
		return
	}

	c.respond(msg.ID, struct{}{}, "")

	// Auto-name session from the first prompt using Haiku.
	if sess.QueryCount() == 1 {
		sessionID := payload.SessionID
		projectID := sess.ProjectID
		prompt := payload.Prompt
		go func() {
			name := generateSessionName(prompt)
			if name == "" {
				return
			}
			if err := c.queries.UpdateSessionName(context.Background(), store.UpdateSessionNameParams{
				Name: name,
				ID:   sessionID,
			}); err != nil {
				log.Printf("auto-rename db update error: %v", err)
				return
			}
			c.hub.Broadcast(projectID, "session.renamed", SessionRenamedPayload{
				SessionID: sessionID,
				Name:      name,
			})
		}()
	}
}

// resumeSession attempts to resume a non-live session from its Claude session ID.
func (c *conn) resumeSession(sessionID string) (*session.Session, error) {
	dbSess, err := c.queries.GetSession(c.ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not in DB")
	}
	if !dbSess.ClaudeSessionID.Valid || dbSess.ClaudeSessionID.String == "" {
		return nil, fmt.Errorf("session has no Claude session ID")
	}

	workDir := dbSess.WorkDir
	if _, statErr := os.Stat(workDir); statErr != nil {
		project, projErr := c.queries.GetProject(c.ctx, dbSess.ProjectID)
		if projErr != nil {
			return nil, fmt.Errorf("project not found")
		}
		if dbSess.WorktreeBranch.Valid && dbSess.WorktreeBranch.String != "" {
			if err := session.RestoreWorktree(project.Path, dbSess.WorktreeBranch.String, dbSess.WorktreePath.String); err != nil {
				log.Printf("session %s: worktree restore failed, falling back to project root: %v", sessionID, err)
				workDir = project.Path
			}
		} else {
			workDir = project.Path
		}
	}

	return c.mgr.Resume(c.ctx, sessionID, dbSess.ClaudeSessionID.String, dbSess.ProjectID, workDir)
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

	turns, err := session.HistoryFromDB(c.ctx, c.queries, payload.SessionID)
	if err != nil {
		c.respond(msg.ID, nil, "failed to load history: "+err.Error())
		return
	}

	c.respond(msg.ID, SessionHistoryResult{Turns: turns}, "")
}

// generateSessionName calls Haiku to generate a short title from a prompt.
func generateSessionName(prompt string) string {
	p := prompt
	if len(p) > 500 {
		p = p[:500]
	}
	namePrompt := "Generate a short 2-4 word title for this coding task. " +
		"Respond with ONLY the title, no quotes or punctuation:\n\n" + p

	client := claudecli.New()
	result, err := client.RunBlocking(context.Background(), namePrompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
	)
	if err != nil {
		log.Printf("auto-rename haiku error: %v", err)
		return ""
	}

	name := strings.TrimSpace(result.Text)
	name = strings.Trim(name, "\"'")
	if name == "" {
		return ""
	}
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}
