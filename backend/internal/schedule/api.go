package schedule

import (
	"context"
	"errors"
	"fmt"
	"github.com/allbin/agentkit/sqliteops"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// ErrValidation marks caller mistakes (bad cron, cadence below the floor,
// unknown mode) as opposed to infrastructure failures.
var ErrValidation = errors.New("invalid schedule")

// Input bounds. Name lands in the sidebar and every push; prompt is
// re-broadcast on ScheduleInfo with each run transition and re-sent to the
// CLI each fire; cron is re-parsed each fire. Reject, never silently truncate.
const (
	maxNameLen   = 120
	maxPromptLen = 64 * 1024
	maxCronLen   = 256
	// maxPendingProposals bounds agent-created pending-approval schedules per
	// session — ScheduleCreate is auto-allowed, so an over-eager (or
	// prompt-injected) agent must not be able to flood the approval queue.
	maxPendingProposals = 3
)

func validateLengths(name, prompt, cron string) error {
	if len(name) > maxNameLen {
		return fmt.Errorf("%w: name exceeds %d characters", ErrValidation, maxNameLen)
	}
	if len(prompt) > maxPromptLen {
		return fmt.Errorf("%w: prompt exceeds %d bytes", ErrValidation, maxPromptLen)
	}
	if len(cron) > maxCronLen {
		return fmt.Errorf("%w: cron expression exceeds %d characters", ErrValidation, maxCronLen)
	}
	return nil
}

// CreateParams describes a new schedule. Exactly one of Cron (recurring),
// At (once), or Dynamic must be set; Mode is derived.
type CreateParams struct {
	ProjectID string
	SessionID string
	Name      string
	Prompt    string
	Cron      string
	// At is the one-shot fire time (RFC3339) for reminders.
	At string
	// Dynamic creates a self-paced loop: the first fire is immediate and the
	// agent chooses each subsequent delay via ScheduleNext (fallback cadence
	// when it forgets; dynamic-ended parking when it stops entirely).
	Dynamic   bool
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
	if err := validateLengths(p.Name, p.Prompt, p.Cron); err != nil {
		return ScheduleInfo{}, err
	}
	if p.SessionID == "" {
		return ScheduleInfo{}, fmt.Errorf("%w: session is required", ErrValidation)
	}
	cadences := 0
	for _, set := range []bool{p.Cron != "", p.At != "", p.Dynamic} {
		if set {
			cadences++
		}
	}
	if cadences != 1 {
		return ScheduleInfo{}, fmt.Errorf("%w: exactly one of cron, at, or dynamic is required", ErrValidation)
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "user"
	}
	// The session row is the authority for the owning project — a
	// client-supplied mismatched pair would file the schedule under a project
	// whose deletion cascades away a schedule targeting a live session.
	sess, err := s.q.GetSession(ctx, p.SessionID)
	if err != nil {
		return ScheduleInfo{}, fmt.Errorf("%w: session not found", ErrValidation)
	}
	p.ProjectID = sess.ProjectID

	now := s.opts.Now().UTC()
	mode := ModeRecurring
	var nextRun string
	switch {
	case p.Cron != "":
		next, err := s.validateCron(p.Cron, now)
		if err != nil {
			return ScheduleInfo{}, err
		}
		nextRun = formatTime(next)
	case p.Dynamic:
		mode = ModeDynamic
		nextRun = formatTime(now) // first fire immediate, then agent-paced
	default:
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
	if err := validateLengths(p.Name, p.Prompt, p.Cron); err != nil {
		return ScheduleInfo{}, err
	}
	// Absent-means-keep semantics: an omitted cron/expiry preserves the
	// stored value (clearing an expiry is not supported over this RPC).
	if p.Cron == "" {
		p.Cron = sched.Cron
	}
	if p.ExpiresAt == "" {
		p.ExpiresAt = sched.ExpiresAt
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
		Reason:       runNowReason,
	})
	if err != nil {
		return ScheduleRunInfo{}, fmt.Errorf("create run: %w", err)
	}
	if rows == 0 {
		return ScheduleRunInfo{}, fmt.Errorf("%w: a run for this second already exists", ErrValidation)
	}
	s.pushRun(ctx, runID)
	s.pruneRuns(ctx, id)
	// Read the response row BEFORE spawning delivery: an instant resolution
	// (e.g. skipped on a finished session) could otherwise broadcast the
	// terminal state and then lose to this stale queued snapshot client-side.
	run, err := s.q.GetScheduleRun(ctx, runID)
	if err != nil {
		return ScheduleRunInfo{}, err
	}
	s.spawn(func() { s.deliverRunNow(context.Background(), runID) })
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

// AgentCreate is the ScheduleCreate MCP tool backend: a self-targeting,
// **pending-approval** schedule. Non-blocking by design — the CLI's MCP
// client times out per call (~60s) and agentkit's POST-only transport cannot
// extend it with progress, so the handler must never park waiting for a
// human. The schedule is created paused; the approval banner enables it.
func (s *Scheduler) AgentCreate(ctx context.Context, sessionID, name, prompt, cron, at string, dynamic bool) (string, error) {
	sess, err := s.q.GetSession(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("session not found: %w", err)
	}
	// Flood guard: ScheduleCreate is auto-allowed, so bound how many
	// unapproved proposals one session can stack up.
	existing, err := s.q.ListSchedulesBySession(ctx, sessionID)
	if err == nil {
		pending := 0
		for _, sc := range existing {
			if sc.PauseReason == PausePendingApproval {
				pending++
			}
		}
		if pending >= maxPendingProposals {
			return "", fmt.Errorf("%w: %d schedule proposals are already awaiting approval — ask the user to approve or deny them first", ErrValidation, pending)
		}
	}
	info, err := s.Create(ctx, CreateParams{
		ProjectID: sess.ProjectID,
		SessionID: sessionID,
		Name:      name,
		Prompt:    prompt,
		Cron:      cron,
		At:        at,
		Dynamic:   dynamic,
		CreatedBy: "agent",
		Paused:    true,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Schedule %q created and awaiting the user's approval in the UI (id %s). It will not fire until approved — do not wait for it; mention it to the user and continue.", info.Name, info.ID), nil
}

// agentReport is a ScheduleReport outcome held until the run's turn resolves.
type agentReport struct {
	status  string // RunOK | RunActionNeeded | RunError
	summary string
}

// agentPace is a ScheduleNext call held until the run resolves; next_run_at
// is written immediately, the description is stamped on the run row.
type agentPace struct {
	delay  time.Duration
	reason string
	stop   bool
}

func (p agentPace) describe() string {
	if p.stop {
		return "stopped by agent"
	}
	d := p.delay.Round(time.Second)
	if p.reason == "" {
		return fmt.Sprintf("next in %s", d)
	}
	return fmt.Sprintf("next in %s — %s", d, p.reason)
}

// takePace pops the pending pace for a run, if any.
func (s *Scheduler) takePace(runID string) (agentPace, bool) {
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	p, ok := s.pendingPaces[runID]
	if ok {
		delete(s.pendingPaces, runID)
	}
	return p, ok
}

// AgentPace is the ScheduleNext MCP tool backend: the agent of a dynamic
// loop chooses its own next fire (clamped to [min-interval,
// dynamic-max-delay]) with a human-readable reason, or stops the loop.
// runId is mandatory and must belong to an in-flight run fired into the
// calling session on a dynamic-mode schedule. next_run_at is written
// immediately (enabled-guarded) so the UI shows the new countdown right
// away; the reason lands on the run row at resolution.
func (s *Scheduler) AgentPace(ctx context.Context, sessionID, runID string, delaySeconds int, reason string, stop bool) (string, error) {
	if runID == "" {
		return "", fmt.Errorf("%w: runId is required", ErrValidation)
	}
	run, err := s.q.GetScheduleRun(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("%w: run not found", ErrValidation)
	}
	if run.SessionID != sessionID {
		return "", fmt.Errorf("%w: run %s does not belong to this session", ErrValidation, runID)
	}
	switch run.Status {
	case RunQueued, RunFiring, RunRunning:
	default:
		return "", fmt.Errorf("%w: the run is already resolved — pacing applies to the run in flight", ErrValidation)
	}
	sched, err := s.q.GetSchedule(ctx, run.ScheduleID)
	if err != nil {
		return "", fmt.Errorf("%w: schedule not found", ErrValidation)
	}
	if sched.Mode != ModeDynamic {
		return "", fmt.Errorf("%w: schedule %q is %s-mode, not dynamic — its cadence is fixed", ErrValidation, sched.Name, sched.Mode)
	}

	now := s.opts.Now().UTC()
	if stop {
		s.reportMu.Lock()
		s.pendingPaces[runID] = agentPace{stop: true}
		s.reportMu.Unlock()
		if err := s.pauseSchedule(ctx, run.ScheduleID, PauseDynamicEnded, ""); err != nil {
			return "", fmt.Errorf("stop loop: %w", err)
		}
		return "Loop stopped; the schedule is parked (resumable from the UI).", nil
	}

	delay := time.Duration(delaySeconds) * time.Second
	clamped := ""
	if delay < s.opts.MinInterval {
		delay = s.opts.MinInterval
		clamped = fmt.Sprintf(" (clamped up to the %s floor)", s.opts.MinInterval)
	}
	if delay > s.opts.DynamicMaxDelay {
		delay = s.opts.DynamicMaxDelay
		clamped = fmt.Sprintf(" (clamped down to the %s ceiling)", s.opts.DynamicMaxDelay)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 200 {
		reason = reason[:200]
	}

	if _, err := s.q.AdvanceScheduleNextRunIfEnabled(ctx, store.AdvanceScheduleNextRunIfEnabledParams{
		NextRunAt: formatTime(now.Add(delay)),
		LastRunAt: sched.LastRunAt,
		UpdatedAt: formatTime(now),
		ID:        run.ScheduleID,
	}); err != nil {
		return "", fmt.Errorf("apply pacing: %w", err)
	}
	s.reportMu.Lock()
	s.pendingPaces[runID] = agentPace{delay: delay, reason: reason}
	s.reportMu.Unlock()
	s.pushSchedule(ctx, run.ScheduleID)
	return fmt.Sprintf("Next fire in %s%s.", delay.Round(time.Second), clamped), nil
}

// takeReport pops the pending report for a run, if any.
func (s *Scheduler) takeReport(runID string) (agentReport, bool) {
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	rep, ok := s.pendingReports[runID]
	if ok {
		delete(s.pendingReports, runID)
	}
	return rep, ok
}

// AgentReport is the ScheduleReport MCP tool backend: an agent-volunteered
// outcome for one scheduled run. runId is mandatory and must belong to a run
// fired into the calling session — with several schedules (and humans) on one
// session, per-session attribution would misfile outcomes. Reports for
// in-flight runs are consumed at turn resolution (they win over the
// final-text fallback); reports for already-terminal runs land in
// late_report and never rewrite the terminal status.
func (s *Scheduler) AgentReport(ctx context.Context, sessionID, runID, status, summary string) (string, error) {
	var runStatus string
	switch status {
	case "ok":
		runStatus = RunOK
	case "action-needed", "action_needed":
		runStatus = RunActionNeeded
	case "failed":
		runStatus = RunError
	default:
		return "", fmt.Errorf("%w: status must be ok, action-needed, or failed", ErrValidation)
	}
	summary = strings.TrimSpace(summary)
	if runID == "" || summary == "" {
		return "", fmt.Errorf("%w: runId and summary are required", ErrValidation)
	}
	if len(summary) > 500 {
		summary = summary[:500]
	}

	run, err := s.q.GetScheduleRun(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("%w: run not found", ErrValidation)
	}
	if run.SessionID != sessionID {
		return "", fmt.Errorf("%w: run %s does not belong to this session", ErrValidation, runID)
	}

	switch run.Status {
	case RunQueued, RunFiring, RunRunning:
		s.reportMu.Lock()
		s.pendingReports[runID] = agentReport{status: runStatus, summary: summary}
		s.reportMu.Unlock()
		return "Report recorded; it will resolve this run when the turn completes.", nil
	default:
		// Terminal already (overdue → action_needed, boot sweep, …): annotate,
		// never rewrite. Counters are untouched by design.
		late := fmt.Sprintf("agent report (%s): %s", status, summary)
		if err := sqliteops.RetryWrite(func() error {
			return s.q.AppendScheduleRunLateReport(ctx, store.AppendScheduleRunLateReportParams{LateReport: late, ID: runID})
		}); err != nil {
			return "", fmt.Errorf("record late report: %w", err)
		}
		s.pushRun(ctx, runID)
		return "The run was already resolved; your report was recorded as a late annotation.", nil
	}
}

// validateCron parses the expression and enforces the cadence floor over
// several successive gaps — a single sample can miss clustered specs (e.g.
// comma lists) whose minimum gap is far below their average.
func (s *Scheduler) validateCron(expr string, now time.Time) (time.Time, error) {
	spec, err := ParseSpec(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	first := spec.Next(now, s.opts.Loc)
	if first.IsZero() {
		return time.Time{}, fmt.Errorf("%w: cron expression has no future occurrence", ErrValidation)
	}
	prev := first
	for i := 0; i < 8; i++ {
		next := spec.Next(prev, s.opts.Loc)
		if next.IsZero() {
			break
		}
		if gap := next.Sub(prev); gap < s.opts.MinInterval {
			return time.Time{}, fmt.Errorf("%w: cadence %s is below the %s floor", ErrValidation, gap, s.opts.MinInterval)
		}
		prev = next
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
