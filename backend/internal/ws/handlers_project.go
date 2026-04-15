package ws

import (
	"context"
	"fmt"

	"github.com/mdjarv/agentique/backend/internal/msggen"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/store"
)

func (c *conn) handleProjectSubscribe(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p ProjectSubscribePayload) (struct{}, error) {
		c.hub.Subscribe(p.ProjectID, c)
		return struct{}{}, nil
	})
}

func (c *conn) handleProjectGitStatus(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectGitStatusPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.Status(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectFetch(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectFetchPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.Fetch(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectPush(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectPushPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.Push(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectCommit(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectCommitPayload) (project.CommitResult, error) {
		return c.projectGitSvc.Commit(ctx, p.ProjectID, p.Message)
	})
}

func (c *conn) handleProjectListBranches(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectListBranchesPayload) (project.BranchListResult, error) {
		return c.projectGitSvc.ListBranches(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectCheckout(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectCheckoutPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.Checkout(ctx, p.ProjectID, p.Branch)
	})
}

func (c *conn) handleProjectPull(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectPullPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.Pull(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectTrackedFiles(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectTrackedFilesPayload) (project.TrackedFilesResult, error) {
		return c.projectGitSvc.TrackedFiles(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectCommands(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectCommandsPayload) (project.CommandsResult, error) {
		return c.projectGitSvc.Commands(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectUncommittedFiles(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectUncommittedFilesPayload) (project.UncommittedFilesResult, error) {
		return c.projectGitSvc.UncommittedFiles(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectGenerateCommitMsg(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectGenerateCommitMsgPayload) (msggen.CommitMessageResult, error) {
		return c.projectGitSvc.GenerateCommitMessage(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectDiscard(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectDiscardPayload) (project.ProjectGitStatus, error) {
		return c.projectGitSvc.DiscardChanges(ctx, p.ProjectID)
	})
}

func (c *conn) handleProjectReorder(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectReorderPayload) (struct{}, error) {
		for i, id := range p.ProjectIDs {
			err := c.queries.UpdateProjectSortOrder(ctx, store.UpdateProjectSortOrderParams{
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

func (c *conn) handleProjectSetFavorite(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ProjectSetFavoritePayload) (store.Project, error) {
		var fav int64
		if p.Favorite {
			fav = 1
		}
		proj, err := c.queries.UpdateProjectFavorite(ctx, store.UpdateProjectFavoriteParams{
			Favorite: fav,
			ID:       p.ProjectID,
		})
		if err != nil {
			return store.Project{}, fmt.Errorf("update favorite: %w", err)
		}
		c.hub.Broadcast(p.ProjectID, "project.updated", proj)
		return proj, nil
	})
}
