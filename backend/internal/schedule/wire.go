package schedule

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// ScheduleInfo is the wire shape of a schedule. Pushed globally (Broadcast)
// as "schedule.updated" — the /schedules page spans projects, matching the
// teams/brain global-page precedent.
type ScheduleInfo struct {
	ID                  string `json:"id"`
	ProjectID           string `json:"projectId"`
	SessionID           string `json:"sessionId"`
	Name                string `json:"name"`
	Prompt              string `json:"prompt"`
	Cron                string `json:"cron"`
	Mode                string `json:"mode"`
	Enabled             bool   `json:"enabled"`
	PauseReason         string `json:"pauseReason"`
	Attention           string `json:"attention"`
	AttentionRunID      string `json:"attentionRunId"`
	NextRunAt           string `json:"nextRunAt"`
	ExpiresAt           string `json:"expiresAt"`
	LastRunAt           string `json:"lastRunAt"`
	LastViewedAt        string `json:"lastViewedAt"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	CreatedBy           string `json:"createdBy"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
}

// ScheduleRunInfo is the wire shape of one run. Pushed as "schedule.run".
type ScheduleRunInfo struct {
	ID            string `json:"id"`
	ScheduleID    string `json:"scheduleId"`
	SessionID     string `json:"sessionId"`
	ScheduledFor  string `json:"scheduledFor"`
	CreatedAt     string `json:"createdAt"`
	FiredAt       string `json:"firedAt"`
	FinishedAt    string `json:"finishedAt"`
	Status        string `json:"status"`
	Overdue       bool   `json:"overdue"`
	Attempts      int    `json:"attempts"`
	NextAttemptAt string `json:"nextAttemptAt"`
	TurnIndex     int    `json:"turnIndex"`
	Summary       string `json:"summary"`
	Reason        string `json:"reason"`
	Error         string `json:"error"`
	ErrorKind     string `json:"errorKind"`
	LateReport    string `json:"lateReport"`
	DurationMS    int64  `json:"durationMs"`
}

// ToScheduleInfo converts a store row to its wire shape.
func ToScheduleInfo(s store.Schedule) ScheduleInfo {
	return ScheduleInfo{
		ID:                  s.ID,
		ProjectID:           s.ProjectID,
		SessionID:           s.SessionID,
		Name:                s.Name,
		Prompt:              s.Prompt,
		Cron:                s.Cron,
		Mode:                s.Mode,
		Enabled:             s.Enabled != 0,
		PauseReason:         s.PauseReason,
		Attention:           s.Attention,
		AttentionRunID:      s.AttentionRunID,
		NextRunAt:           s.NextRunAt,
		ExpiresAt:           s.ExpiresAt,
		LastRunAt:           s.LastRunAt,
		LastViewedAt:        s.LastViewedAt,
		ConsecutiveFailures: int(s.ConsecutiveFailures),
		CreatedBy:           s.CreatedBy,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
	}
}

// ToScheduleRunInfo converts a store run row to its wire shape.
func ToScheduleRunInfo(r store.ScheduleRun) ScheduleRunInfo {
	return ScheduleRunInfo{
		ID:            r.ID,
		ScheduleID:    r.ScheduleID,
		SessionID:     r.SessionID,
		ScheduledFor:  r.ScheduledFor,
		CreatedAt:     r.CreatedAt,
		FiredAt:       r.FiredAt,
		FinishedAt:    r.FinishedAt,
		Status:        r.Status,
		Overdue:       r.Overdue != 0,
		Attempts:      int(r.Attempts),
		NextAttemptAt: r.NextAttemptAt,
		TurnIndex:     int(r.TurnIndex),
		Summary:       r.Summary,
		Reason:        r.Reason,
		Error:         r.Error,
		ErrorKind:     r.ErrorKind,
		LateReport:    r.LateReport,
		DurationMS:    r.DurationMs,
	}
}

// pushSchedule re-reads and broadcasts the schedule's current wire state.
func (s *Scheduler) pushSchedule(ctx context.Context, scheduleID string) {
	sched, err := s.q.GetSchedule(ctx, scheduleID)
	if err != nil {
		slog.Debug("scheduler: push schedule load failed", "schedule_id", scheduleID, "error", err)
		return
	}
	s.broadcast("schedule.updated", ToScheduleInfo(sched))
}

// pushRun re-reads and broadcasts the run's current wire state.
func (s *Scheduler) pushRun(ctx context.Context, runID string) {
	run, err := s.q.GetScheduleRun(ctx, runID)
	if err != nil {
		slog.Debug("scheduler: push run load failed", "run_id", runID, "error", err)
		return
	}
	s.broadcast("schedule.run", ToScheduleRunInfo(run))
}

func newID() string { return uuid.NewString() }
