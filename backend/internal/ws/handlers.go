package ws

import (
	"fmt"

	"github.com/allbin/agentique/backend/internal/gitops"
	"github.com/allbin/agentique/backend/internal/msggen"
	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
)

func (c *conn) handleProjectSubscribe(msg ClientMessage) {
	handleRequest(c, msg, func(p ProjectSubscribePayload) (struct{}, error) {
		c.hub.Subscribe(p.ProjectID, c)
		return struct{}{}, nil
	})
}

func (c *conn) handleSessionCreate(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionCreatePayload) (session.CreateSessionResult, error) {
		return c.svc.CreateSession(c.ctx, session.CreateSessionParams{
			ProjectID:       p.ProjectID,
			Name:            p.Name,
			Worktree:        p.Worktree,
			Branch:          p.Branch,
			Model:           p.Model,
			PlanMode:        p.PlanMode,
			AutoApprove:     p.AutoApprove,
			RequestID:       msg.ID,
			Effort:          p.Effort,
			MaxBudget:       p.MaxBudget,
			MaxTurns:        p.MaxTurns,
			BehaviorPresets: p.BehaviorPresets,
		})
	})
}

func (c *conn) handleSessionQuery(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionQueryPayload) (struct{}, error) {
		return struct{}{}, c.svc.QuerySession(c.ctx, p.SessionID, p.Prompt, p.Attachments)
	})
}

func (c *conn) handleSessionList(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionListPayload) (session.ListSessionsResult, error) {
		return c.svc.ListSessions(c.ctx, p.ProjectID)
	})
}

func (c *conn) handleSessionStop(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionStopPayload) (struct{}, error) {
		return struct{}{}, c.svc.StopSession(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionResume(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionResumePayload) (session.SessionInfo, error) {
		return c.svc.ResumeSession(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionDiff(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionDiffPayload) (gitops.DiffResult, error) {
		return c.gitSvc.Diff(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionInterrupt(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionInterruptPayload) (struct{}, error) {
		return struct{}{}, c.svc.InterruptSession(p.SessionID)
	})
}

func (c *conn) handleSessionMerge(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionMergePayload) (session.MergeResult, error) {
		return c.gitSvc.Merge(c.ctx, p.SessionID, p.Cleanup)
	})
}

func (c *conn) handleSessionCreatePR(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionCreatePRPayload) (session.CreatePRResult, error) {
		return c.gitSvc.CreatePR(c.ctx, session.CreatePRParams{
			SessionID: p.SessionID,
			Title:     p.Title,
			Body:      p.Body,
		})
	})
}

func (c *conn) handleSessionDeleteBulk(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionDeleteBulkPayload) (SessionDeleteBulkResult, error) {
		results := make([]SessionDeleteBulkResultItem, 0, len(p.SessionIDs))
		for _, sid := range p.SessionIDs {
			item := SessionDeleteBulkResultItem{SessionID: sid, Success: true}
			if err := c.svc.DeleteSession(c.ctx, sid); err != nil {
				item.Success = false
				item.Error = err.Error()
			}
			results = append(results, item)
		}
		return SessionDeleteBulkResult{Results: results}, nil
	})
}

func (c *conn) handleSessionDelete(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionDeletePayload) (struct{}, error) {
		return struct{}{}, c.svc.DeleteSession(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionRename(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionRenamePayload) (struct{}, error) {
		return struct{}{}, c.svc.RenameSession(c.ctx, p.SessionID, p.Name)
	})
}

func (c *conn) handleSessionSetModel(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionSetModelPayload) (struct{}, error) {
		return struct{}{}, c.svc.SetSessionModel(c.ctx, p.SessionID, p.Model)
	})
}

func (c *conn) handleSessionSetPermission(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionSetPermissionPayload) (struct{}, error) {
		return struct{}{}, c.svc.SetPermissionMode(p.SessionID, p.Mode)
	})
}

func (c *conn) handleSessionSetAutoApprove(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionSetAutoApprovePayload) (struct{}, error) {
		return struct{}{}, c.svc.SetAutoApprove(p.SessionID, p.Enabled)
	})
}

func (c *conn) handleSessionResolveApproval(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionResolveApprovalPayload) (struct{}, error) {
		return struct{}{}, c.svc.ResolveApproval(p.SessionID, p.ApprovalID, p.Allow, p.Message)
	})
}

func (c *conn) handleSessionResolveQuestion(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionResolveQuestionPayload) (struct{}, error) {
		return struct{}{}, c.svc.ResolveQuestion(p.SessionID, p.QuestionID, p.Answers)
	})
}

func (c *conn) handleSessionCommit(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionCommitPayload) (session.CommitResult, error) {
		return c.gitSvc.Commit(c.ctx, p.SessionID, p.Message)
	})
}

func (c *conn) handleSessionRebase(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionRebasePayload) (session.RebaseResult, error) {
		return c.gitSvc.Rebase(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionHistory(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionHistoryPayload) (session.HistoryResult, error) {
		return c.svc.GetHistory(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionGeneratePRDesc(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionGeneratePRDescPayload) (msggen.PRDescriptionResult, error) {
		return c.gitSvc.GeneratePRDescription(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionMarkDone(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionMarkDonePayload) (struct{}, error) {
		return struct{}{}, c.svc.MarkSessionDone(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionClean(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionCleanPayload) (session.CleanResult, error) {
		return c.gitSvc.Clean(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionRefreshGit(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionRefreshGitPayload) (session.GitSnapshot, error) {
		return c.gitSvc.RefreshGitStatus(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionUncommittedFiles(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionUncommittedFilesPayload) (session.UncommittedFilesResult, error) {
		return c.gitSvc.UncommittedFiles(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionUncommittedDiff(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionUncommittedDiffPayload) (gitops.DiffResult, error) {
		return c.gitSvc.UncommittedDiff(c.ctx, p.SessionID)
	})
}

func (c *conn) handleSessionGenerateCommitMsg(msg ClientMessage) {
	handleRequest(c, msg, func(p SessionGenerateCommitMsgPayload) (msggen.CommitMessageResult, error) {
		return c.gitSvc.GenerateCommitMessage(c.ctx, p.SessionID)
	})
}

// --- Project git handlers ---

func (c *conn) handleProjectGitStatus(msg ClientMessage) {
	handleRequest(c, msg, func(p ProjectGitStatusPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.Status(c.ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectFetch(msg ClientMessage) {
	handleRequest(c, msg, func(p ProjectFetchPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.Fetch(c.ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectPush(msg ClientMessage) {
	handleRequest(c, msg, func(p ProjectPushPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.Push(c.ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectCommit(msg ClientMessage) {
	handleRequest(c, msg, func(p ProjectCommitPayload) (project.CommitResult, error) {
		return c.projectGitSvc.Commit(c.ctx, p.ProjectID, p.Message)
	})
}

func (c *conn) handleProjectTrackedFiles(msg ClientMessage) {
	handleRequest(c, msg, func(p ProjectTrackedFilesPayload) (project.TrackedFilesResult, error) {
		return c.projectGitSvc.TrackedFiles(c.ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectCommands(msg ClientMessage) {
	handleRequest(c, msg, func(p ProjectCommandsPayload) (project.CommandsResult, error) {
		return c.projectGitSvc.Commands(c.ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectReorder(msg ClientMessage) {
	handleRequest(c, msg, func(p ProjectReorderPayload) (struct{}, error) {
		for i, id := range p.ProjectIDs {
			err := c.queries.UpdateProjectSortOrder(c.ctx, store.UpdateProjectSortOrderParams{
				SortOrder: int64(i + 1),
				ID:        id,
			})
			if err != nil {
				return struct{}{}, fmt.Errorf("update sort order: %w", err)
			}
		}
		return struct{}{}, nil
	})
}
