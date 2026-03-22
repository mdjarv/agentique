package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	claudecli "github.com/allbin/claudecli-go"
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

	// Auto-name session from the first prompt using Haiku.
	if sess.QueryCount() == 1 {
		sessionID := payload.SessionID
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
			c.push("session.renamed", SessionRenamedPayload{
				SessionID: sessionID,
				Name:      name,
			})
		}()
	}
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
		// Not live -- try to resume if it has a Claude session ID.
		dbSess, dbErr := c.queries.GetSession(c.ctx, payload.SessionID)
		if dbErr != nil || !dbSess.ClaudeSessionID.Valid || dbSess.ClaudeSessionID.String == "" {
			c.respond(msg.ID, nil, "session not active")
			return
		}

		var err error
		sess, err = c.mgr.Resume(c.ctx, payload.SessionID, dbSess.ClaudeSessionID.String, dbSess.WorkDir,
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
			log.Printf("session resume error: %v", err)
			c.respond(msg.ID, nil, "failed to resume session: "+err.Error())
			return
		}
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

	sess := c.mgr.Get(payload.SessionID)
	if sess == nil {
		// Session not live -- return empty history.
		c.respond(msg.ID, SessionHistoryResult{Turns: []session.HistoryTurn{}}, "")
		return
	}

	turns := sess.History()
	if turns == nil {
		turns = []session.HistoryTurn{}
	}
	c.respond(msg.ID, SessionHistoryResult{Turns: turns}, "")
}

// generateSessionName calls Haiku to generate a short title from a prompt.
func generateSessionName(prompt string) string {
	// Truncate long prompts to keep the naming call cheap.
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
	// Strip surrounding quotes if present.
	name = strings.Trim(name, "\"'")
	if name == "" {
		return ""
	}
	// Cap at 50 chars.
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}
