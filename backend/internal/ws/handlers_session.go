package ws

import (
	"context"

	"github.com/allbin/agentkit/worktree"
	"github.com/mdjarv/agentique/backend/internal/msggen"
	"github.com/mdjarv/agentique/backend/internal/session"
)

func (c *conn) handleSessionCreate(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionCreatePayload) (session.CreateSessionResult, error) {
		return c.svc.CreateSession(ctx, session.CreateSessionParams{
			ProjectID:       p.ProjectID,
			Name:            p.Name,
			Worktree:        p.Worktree,
			Branch:          p.Branch,
			Model:           p.Model,
			PlanMode:        p.PlanMode,
			AutoApproveMode: p.AutoApproveMode,
			RequestID:       msg.ID,
			Effort:          p.Effort,
			MaxBudget:       p.MaxBudget,
			MaxTurns:        p.MaxTurns,
			BehaviorPresets: p.BehaviorPresets,
			AgentProfileID:  p.AgentProfileID,
			IdempotencyKey:  p.IdempotencyKey,
		})
	})
}

func (c *conn) handleSessionQuery(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionQueryPayload) (struct{}, error) {
		return struct{}{}, c.svc.QuerySession(ctx, p.SessionID, p.Prompt, p.Attachments)
	})
}

func (c *conn) handleSessionList(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionListPayload) (session.ListSessionsResult, error) {
		return c.svc.ListSessions(ctx, p.ProjectID)
	})
}

func (c *conn) handleSessionStop(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionStopPayload) (struct{}, error) {
		return struct{}{}, c.svc.StopSession(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionResetConversation(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionResetConversationPayload) (struct{}, error) {
		return struct{}{}, c.svc.ResetConversation(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionResume(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionResumePayload) (session.SessionInfo, error) {
		return c.svc.ResumeSession(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionDiff(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionDiffPayload) (worktree.DiffResult, error) {
		return c.gitSvc.Diff(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionInterrupt(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p SessionInterruptPayload) (struct{}, error) {
		return struct{}{}, c.svc.InterruptSession(p.SessionID)
	})
}

func (c *conn) handleSessionMerge(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionMergePayload) (session.MergeResult, error) {
		return c.gitSvc.Merge(ctx, p.SessionID, p.Mode)
	})
}

func (c *conn) handleSessionCreatePR(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionCreatePRPayload) (session.CreatePRResult, error) {
		return c.gitSvc.CreatePR(ctx, session.CreatePRParams{
			SessionID: p.SessionID,
			Title:     p.Title,
			Body:      p.Body,
		})
	})
}

func (c *conn) handleSessionDeleteBulk(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionDeleteBulkPayload) (SessionDeleteBulkResult, error) {
		results := make([]SessionDeleteBulkResultItem, 0, len(p.SessionIDs))
		for _, sid := range p.SessionIDs {
			item := SessionDeleteBulkResultItem{SessionID: sid, Success: true}
			if err := c.svc.DeleteSession(ctx, sid); err != nil {
				item.Success = false
				item.Error = err.Error()
			}
			results = append(results, item)
		}
		return SessionDeleteBulkResult{Results: results}, nil
	})
}

func (c *conn) handleSessionDelete(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionDeletePayload) (struct{}, error) {
		return struct{}{}, c.svc.DeleteSession(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionRename(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionRenamePayload) (struct{}, error) {
		return struct{}{}, c.svc.RenameSession(ctx, p.SessionID, p.Name)
	})
}

func (c *conn) handleSessionSetModel(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionSetModelPayload) (struct{}, error) {
		return struct{}{}, c.svc.SetSessionModel(ctx, p.SessionID, p.Model)
	})
}

func (c *conn) handleSessionSetPermission(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p SessionSetPermissionPayload) (struct{}, error) {
		return struct{}{}, c.svc.SetPermissionMode(p.SessionID, p.Mode)
	})
}

func (c *conn) handleSessionSetAutoApprove(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p SessionSetAutoApprovePayload) (struct{}, error) {
		return struct{}{}, c.svc.SetAutoApproveMode(p.SessionID, p.Mode)
	})
}

func (c *conn) handleSessionResolveApproval(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p SessionResolveApprovalPayload) (struct{}, error) {
		return struct{}{}, c.svc.ResolveApproval(p.SessionID, p.ApprovalID, p.Allow, p.Message)
	})
}

func (c *conn) handleSessionResolveQuestion(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p SessionResolveQuestionPayload) (struct{}, error) {
		return struct{}{}, c.svc.ResolveQuestion(p.SessionID, p.QuestionID, p.Answers)
	})
}

func (c *conn) handleSessionCommit(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionCommitPayload) (session.CommitResult, error) {
		return c.gitSvc.Commit(ctx, p.SessionID, p.Message)
	})
}

func (c *conn) handleSessionRebase(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionRebasePayload) (session.RebaseResult, error) {
		return c.gitSvc.Rebase(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionHistory(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionHistoryPayload) (session.HistoryResult, error) {
		return c.svc.GetHistory(ctx, p.SessionID, p.Limit)
	})
}

func (c *conn) handleSessionGeneratePRDesc(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionGeneratePRDescPayload) (msggen.PRDescriptionResult, error) {
		return c.gitSvc.GeneratePRDescription(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionMarkDone(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionMarkDonePayload) (struct{}, error) {
		return struct{}{}, c.svc.MarkSessionDone(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionClean(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionCleanPayload) (session.CleanResult, error) {
		return c.gitSvc.Clean(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionRefreshGit(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionRefreshGitPayload) (session.GitSnapshot, error) {
		return c.gitSvc.RefreshGitStatus(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionCommitLog(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionCommitLogPayload) (session.CommitLogResult, error) {
		return c.gitSvc.CommitLog(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionPRStatus(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionPRStatusPayload) (session.PRStatusResult, error) {
		return c.gitSvc.PRStatus(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionUncommittedFiles(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionUncommittedFilesPayload) (session.UncommittedFilesResult, error) {
		return c.gitSvc.UncommittedFiles(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionUncommittedDiff(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionUncommittedDiffPayload) (worktree.DiffResult, error) {
		return c.gitSvc.UncommittedDiff(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionGenerateCommitMsg(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionGenerateCommitMsgPayload) (msggen.CommitMessageResult, error) {
		return c.gitSvc.GenerateCommitMessage(ctx, p.SessionID)
	})
}

func (c *conn) handleSessionGenerateName(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p SessionGenerateNamePayload) (SessionGenerateNameResult, error) {
		name, err := c.svc.GenerateSessionName(ctx, p.SessionID)
		if err != nil {
			return SessionGenerateNameResult{}, err
		}
		return SessionGenerateNameResult{Name: name}, nil
	})
}

// --- Message injection handler ---

func (c *conn) handleSessionEnqueue(msg ClientMessage) {
	// Reuse SessionQueryPayload — same shape (sessionId, prompt, attachments).
	handleRequest(c, msg, func(ctx context.Context, p SessionQueryPayload) (struct{}, error) {
		return struct{}{}, c.svc.EnqueueMessage(ctx, p.SessionID, p.Prompt, p.Attachments)
	})
}
