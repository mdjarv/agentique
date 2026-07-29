package ws

import (
	"context"
	"errors"

	"github.com/mdjarv/agentique/backend/internal/schedule"
)

// errSchedulerDisabled is returned by every schedule RPC when the scheduler
// is disabled via [scheduler] disabled.
var errSchedulerDisabled = errors.New("the scheduler is disabled on this server")

func (c *conn) requireScheduler() (*schedule.Scheduler, error) {
	if c.scheduleSvc == nil {
		return nil, errSchedulerDisabled
	}
	return c.scheduleSvc, nil
}

func (c *conn) handleScheduleCreate(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleCreatePayload) (schedule.ScheduleInfo, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return schedule.ScheduleInfo{}, err
		}
		return svc.Create(ctx, schedule.CreateParams{
			ProjectID: p.ProjectID,
			SessionID: p.SessionID,
			Name:      p.Name,
			Prompt:    p.Prompt,
			Cron:      p.Cron,
			At:        p.At,
			ExpiresAt: p.ExpiresAt,
			CreatedBy: "user",
		})
	})
}

func (c *conn) handleScheduleList(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, _ struct{}) ([]schedule.ScheduleInfo, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return nil, err
		}
		return svc.List(ctx)
	})
}

func (c *conn) handleScheduleUpdate(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleUpdatePayload) (schedule.ScheduleInfo, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return schedule.ScheduleInfo{}, err
		}
		return svc.Update(ctx, schedule.UpdateParams{
			ID:        p.ID,
			Name:      p.Name,
			Prompt:    p.Prompt,
			Cron:      p.Cron,
			ExpiresAt: p.ExpiresAt,
		})
	})
}

func (c *conn) handleScheduleDelete(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleIDPayload) (struct{}, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, svc.Delete(ctx, p.ID)
	})
}

func (c *conn) handleSchedulePause(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleIDPayload) (schedule.ScheduleInfo, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return schedule.ScheduleInfo{}, err
		}
		return svc.Pause(ctx, p.ID)
	})
}

func (c *conn) handleScheduleResume(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleIDPayload) (schedule.ScheduleInfo, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return schedule.ScheduleInfo{}, err
		}
		return svc.Resume(ctx, p.ID)
	})
}

func (c *conn) handleScheduleApprove(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleIDPayload) (schedule.ScheduleInfo, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return schedule.ScheduleInfo{}, err
		}
		return svc.Approve(ctx, p.ID)
	})
}

func (c *conn) handleScheduleRunNow(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleIDPayload) (schedule.ScheduleRunInfo, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return schedule.ScheduleRunInfo{}, err
		}
		return svc.RunNow(ctx, p.ID)
	})
}

func (c *conn) handleScheduleRuns(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleRunsPayload) ([]schedule.ScheduleRunInfo, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return nil, err
		}
		return svc.Runs(ctx, p.ScheduleID, p.Limit, p.Offset)
	})
}

func (c *conn) handleScheduleMarkViewed(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ScheduleIDPayload) (struct{}, error) {
		svc, err := c.requireScheduler()
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, svc.MarkViewed(ctx, p.ID)
	})
}
