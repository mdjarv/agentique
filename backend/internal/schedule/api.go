package schedule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// ErrValidation marks caller mistakes (bad cron, cadence below the floor,
// unknown mode) as opposed to infrastructure failures.
var ErrValidation = errors.New("invalid schedule")

// CreateParams describes a new schedule. Exactly one of Cron (recurring) or
// At (once) must be set; Mode is derived. Dynamic mode arrives with M2's
// ScheduleNext tool and is not creatable yet.
type CreateParams struct {
	ProjectID string
	SessionID string
	Name      string
	Prompt    string
	Cron      string
	// At is the one-shot fire time (RFC3339) for reminders.
	At        string
	ExpiresAt string
	// CreatedBy is "user" (UI form) or "agent" (ScheduleCreate MCP tool).
	CreatedBy string
	// Paused creates the schedule disabled with the given pause reason —
	// the agent-created pending-approval flow.
	Paused      bool
	PauseReason string
}

// Create validates and persists a schedule. Recurring schedules get
// next_run_at = the cron's next occurrence; once-mode uses At verbatim.
func (s *Scheduler) Create(ctx context.Context, p CreateParams) (ScheduleInfo, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.Prompt = strings.TrimSpace(p.Prompt)
	if p.Name == "" || p.Prompt == "" {
		return ScheduleInfo{}, fmt.Errorf("%w: name and prompt are required", ErrValidation)
	}
	if p.ProjectID == "" || p.SessionID == "" {
		return ScheduleInfo{}, fmt.Errorf("%w: project and session are required", ErrValidation)
	}
	if (p.Cron == "") == (p.At == "") {
		return ScheduleInfo{}, fmt.Errorf("%w: exactly one of cron or at is required", ErrValidation)
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "user"
	}

	now := s.opts.Now().UTC()
	mode := ModeRecurring
	var nextRun string
	if p.Cron != "" {
		next, err := s.validateCron(p.Cron, now)
		if err != nil {
			return ScheduleInfo{}, err
		}
		nextRun = formatTime(next)
	} else {
		mode = ModeOnce
		at, ok := parseTime(p.At)
		if !ok {
			return ScheduleInfo{}, fmt.Errorf("%w: at must be RFC3339", ErrValidation)
		}
		if !at.After(now) {
			return ScheduleInfo{}, fmt.Errorf("%w: at must be in the future", ErrValidation)
		}
		nextRun = formatTime(at)
	}
	if p.ExpiresAt != "" {
		if _, ok := parseTime(p.ExpiresAt); !ok {
			return ScheduleInfo{}, fmt.Errorf("%w: expires-at must be RFC3339", ErrValidation)
		}
	}

	enabled := int64(1)
	pauseReason := ""
	if p.Paused {
		enabled = 0
		pauseReason = p.PauseReason
		if pauseReason == "" {
			pauseReason = PausePendingApproval
		}
	}

	row, err := s.q.CreateSchedule(ctx, store.CreateScheduleParams{
		ID:          newID(),
		ProjectID:   p.ProjectID,
		SessionID:   p.SessionID,
		Name:        p.Name,
		Prompt:      p.Prompt,
		Cron:        p.Cron,
		Mode:        mode,
		Enabled:     enabled,
		PauseReason: pauseReason,
		NextRunAt:   nextRun,
		ExpiresAt:   normalizeTime(p.ExpiresAt),
		CreatedBy:   p.CreatedBy,
		CreatedAt:   formatTime(now),
		UpdatedAt:   formatTime(now),
	})
	if err != nil {
		return ScheduleInfo{}, fmt.Errorf("create schedule: %w", err)
	}
	info := ToScheduleInfo(row)
	s.broadcast("schedule.updated", info)
	return info, nil
}

// UpdateParams edits an existing schedule (name/prompt/cron/expiry).
type UpdateParams struct {
	ID        string
	Name      string
	Prompt    string
	Cron      string
	ExpiresAt string
}

// Update edits a schedule. A cron change recomputes next_run_at (when the
// schedule is enabled); editing clears a failed attention — it is an explicit
// human act on the loop.
func (s *Scheduler) Update(ctx context.Context, p UpdateParams) (ScheduleInfo, error) {
	sched, err := s.q.GetSchedule(ctx, p.ID)
	if err != nil {
		return ScheduleInfo{}, fmt.Errorf("schedule not found: %w", err)
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Prompt = strings.TrimSpace(p.Prompt)
	if p.Name == "" || p.Prompt == "" {
		return ScheduleInfo{}, fmt.Errorf("%w: name and prompt are required", ErrValidation)
	}
	now := s.opts.Now().UTC()

	if sched.Mode == ModeRecurring && p.Cron != sched.Cron {
		next, err := s.validateCron(p.Cron, now)
		if err != nil {
			return ScheduleInfo{}, err
		}
		if sched.Enabled != 0 {
			if err := s.q.UpdateScheduleNextRun(ctx, store.UpdateScheduleNextRunParams{
				NextRunAt: formatTime(next),
				LastRunAt: sched.LastRunAt,
				UpdatedAt: formatTime(now),
				ID:        p.ID,
			}); err != nil {
				return ScheduleInfo{}, fmt.Errorf("update next run: %w", err)
			}
		}
	} else if sched.Mode != ModeRecurring {
		p.Cron = sched.Cron
	}
	if p.ExpiresAt != "" {
		if _, ok := parseTime(p.ExpiresAt); !ok {
			return ScheduleInfo{}, fmt.Errorf("%w: expires-at must be RFC3339", ErrValidation)
		}
	}

	if err := s.q.UpdateSchedule(ctx, store.UpdateScheduleParams{
		Name:      p.Name,
		Prompt:    p.Prompt,
		Cron:      p.Cron,
		Mode:      sched.Mode,
		ExpiresAt: normalizeTime(p.ExpiresAt),
		UpdatedAt: formatTime(now),
		ID:        p.ID,
	}); err != nil {
		return ScheduleInfo{}, fmt.Errorf("update schedule: %w", err)
	}
	s.clearAttention(ctx, p.ID)
	return s.reload(ctx, p.ID)
}

// Pause disables a schedule by user intent, resolving queued runs.
func (s *Scheduler) Pause(ctx context.Context, id string) (ScheduleInfo, error) {
	if err := s.pauseSchedule(ctx, id, PauseUser, ""); err != nil {
		return ScheduleInfo{}, err
	}
	return s.reload(ctx, id)
}

// Resume re-enables a paused schedule and re-arms next_run_at. It is the
// explicit act that clears a failed attention and resets the failure counter.
// Completed one-shots cannot resume.
func (s *Scheduler) Resume(ctx context.Context, id string) (ScheduleInfo, error) {
	sched, err := s.q.GetSchedule(ctx, id)
	if err != nil {
		return ScheduleInfo{}, fmt.Errorf("schedule not found: %w", err)
	}
	if sched.Enabled != 0 {
		return ToScheduleInfo(sched), nil
	}
	now := s.opts.Now().UTC()

	var nextRun string
	switch sched.Mode {
	case ModeOnce:
		if sched.PauseReason == PauseCompleted {
			return ScheduleInfo{}, fmt.Errorf("%w: a completed one-shot cannot resume", ErrValidation)
		}
		// Pending-approval once-schedules keep their original fire time when
		// still in the future; a past one is refused rather than fired late.
		at, ok := parseTime(sched.NextRunAt)
		if !ok || !at.After(now) {
			return ScheduleInfo{}, fmt.Errorf("%w: the one-shot fire time has passed; recreate it", ErrValidation)
		}
		nextRun = sched.NextRunAt
	case ModeDynamic:
		nextRun = formatTime(now) // dynamic resumes fire immediately
	default:
		spec, err := ParseSpec(sched.Cron)
		if err != nil {
			return ScheduleInfo{}, fmt.Errorf("%w: %v", ErrValidation, err)
		}
		next := spec.Next(now, s.opts.Loc)
		if next.IsZero() {
			return ScheduleInfo{}, fmt.Errorf("%w: cron expression has no future occurrence", ErrValidation)
		}
		nextRun = formatTime(next)
	}

	if err := s.q.SetScheduleEnabled(ctx, store.SetScheduleEnabledParams{
		Enabled:     1,
		PauseReason: "",
		NextRunAt:   nextRun,
		UpdatedAt:   formatTime(now),
		ID:          id,
	}); err != nil {
		return ScheduleInfo{}, fmt.Errorf("resume schedule: %w", err)
	}
	if err := s.q.SetScheduleFailures(ctx, store.SetScheduleFailuresParams{
		ConsecutiveFailures: 0,
		UpdatedAt:           formatTime(now),
		ID:                  id,
	}); err != nil {
		return ScheduleInfo{}, fmt.Errorf("reset failures: %w", err)
	}
	s.clearAttention(ctx, id)
	return s.reload(ctx, id)
}

// Delete removes a schedule and (via FK cascade) its runs.
func (s *Scheduler) Delete(ctx context.Context, id string) error {
	sched, err := s.q.GetSchedule(ctx, id)
	if err != nil {
		return fmt.Errorf("schedule not found: %w", err)
	}
	if err := s.q.DeleteSchedule(ctx, id); err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	info := ToScheduleInfo(sched)
	info.Enabled = false
	s.broadcast("schedule.deleted", info)
	return nil
}

// RunNow fires a schedule immediately: an ad-hoc run outside the cadence,
// through the same claim/delivery path as a due fire. Allowed while paused
// (it does not re-enable the schedule).
func (s *Scheduler) RunNow(ctx context.Context, id string) (ScheduleRunInfo, error) {
	sched, err := s.q.GetSchedule(ctx, id)
	if err != nil {
		return ScheduleRunInfo{}, fmt.Errorf("schedule not found: %w", err)
	}
	now := s.opts.Now().UTC()
	runID := newID()
	rows, err := s.q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
		ID:           runID,
		ScheduleID:   id,
		SessionID:    sched.SessionID,
		ScheduledFor: formatTime(now),
		CreatedAt:    formatTime(now),
		Status:       RunQueued,
		Reason:       "run now",
	})
	if err != nil {
		return ScheduleRunInfo{}, fmt.Errorf("create run: %w", err)
	}
	if rows == 0 {
		return ScheduleRunInfo{}, fmt.Errorf("%w: a run for this second already exists", ErrValidation)
	}
	s.pushRun(ctx, runID)
	s.pruneRuns(ctx, id)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.deliverRunNow(context.Background(), runID)
	}()
	run, err := s.q.GetScheduleRun(ctx, runID)
	if err != nil {
		return ScheduleRunInfo{}, err
	}
	return ToScheduleRunInfo(run), nil
}

// deliverRunNow is attemptDelivery minus the enabled gate — run-now is valid
// on a paused schedule.
func (s *Scheduler) deliverRunNow(ctx context.Context, runID string) {
	run, err := s.q.GetScheduleRun(ctx, runID)
	if err != nil || run.Status != RunQueued {
		return
	}
	sched, err := s.q.GetSchedule(ctx, run.ScheduleID)
	if err != nil {
		return
	}
	s.deliverClaimed(ctx, run, sched)
}

// MarkViewed records the user looking at a schedule's runs (the
// "since you last looked" divider) and clears a view-clearable attention.
func (s *Scheduler) MarkViewed(ctx context.Context, id string) error {
	now := formatTime(s.opts.Now().UTC())
	if err := s.q.MarkScheduleViewed(ctx, store.MarkScheduleViewedParams{
		LastViewedAt: now,
		UpdatedAt:    now,
		ID:           id,
	}); err != nil {
		return fmt.Errorf("mark viewed: %w", err)
	}
	if err := s.q.ClearScheduleActionAttention(ctx, store.ClearScheduleActionAttentionParams{
		UpdatedAt: now,
		ID:        id,
	}); err != nil {
		return fmt.Errorf("clear attention: %w", err)
	}
	s.pushSchedule(ctx, id)
	return nil
}

// List returns all schedules (wire shape), newest first.
func (s *Scheduler) List(ctx context.Context) ([]ScheduleInfo, error) {
	rows, err := s.q.ListSchedules(ctx)
	if err != nil {
		return nil, err
	}
	infos := make([]ScheduleInfo, 0, len(rows))
	for _, r := range rows {
		infos = append(infos, ToScheduleInfo(r))
	}
	return infos, nil
}

// Runs returns a page of a schedule's run history, newest first.
func (s *Scheduler) Runs(ctx context.Context, scheduleID string, limit, offset int) ([]ScheduleRunInfo, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.q.ListScheduleRuns(ctx, store.ListScheduleRunsParams{
		ScheduleID: scheduleID,
		Limit:      int64(limit),
		Offset:     int64(offset),
	})
	if err != nil {
		return nil, err
	}
	infos := make([]ScheduleRunInfo, 0, len(rows))
	for _, r := range rows {
		infos = append(infos, ToScheduleRunInfo(r))
	}
	return infos, nil
}

// Approve enables an agent-created pending-approval schedule; Deny deletes it.
func (s *Scheduler) Approve(ctx context.Context, id string) (ScheduleInfo, error) {
	sched, err := s.q.GetSchedule(ctx, id)
	if err != nil {
		return ScheduleInfo{}, fmt.Errorf("schedule not found: %w", err)
	}
	if sched.PauseReason != PausePendingApproval {
		return ScheduleInfo{}, fmt.Errorf("%w: schedule is not awaiting approval", ErrValidation)
	}
	return s.Resume(ctx, id)
}

// validateCron parses the expression and enforces the cadence floor by
// checking the gap between the next two occurrences.
func (s *Scheduler) validateCron(expr string, now time.Time) (time.Time, error) {
	spec, err := ParseSpec(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	first := spec.Next(now, s.opts.Loc)
	if first.IsZero() {
		return time.Time{}, fmt.Errorf("%w: cron expression has no future occurrence", ErrValidation)
	}
	if second := spec.Next(first, s.opts.Loc); !second.IsZero() {
		if gap := second.Sub(first); gap < s.opts.MinInterval {
			return time.Time{}, fmt.Errorf("%w: cadence %s is below the %s floor", ErrValidation, gap, s.opts.MinInterval)
		}
	}
	return first, nil
}

func (s *Scheduler) clearAttention(ctx context.Context, id string) {
	if err := s.q.SetScheduleAttention(ctx, store.SetScheduleAttentionParams{
		Attention:      "",
		AttentionRunID: "",
		UpdatedAt:      formatTime(s.opts.Now().UTC()),
		ID:             id,
	}); err != nil {
		return
	}
}

func (s *Scheduler) reload(ctx context.Context, id string) (ScheduleInfo, error) {
	sched, err := s.q.GetSchedule(ctx, id)
	if err != nil {
		return ScheduleInfo{}, err
	}
	info := ToScheduleInfo(sched)
	s.broadcast("schedule.updated", info)
	return info, nil
}

func normalizeTime(v string) string {
	t, ok := parseTime(v)
	if !ok {
		return ""
	}
	return formatTime(t)
}
