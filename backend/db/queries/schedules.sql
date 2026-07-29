-- Scheduled loops (docs/scheduled-loops.md). All timestamps are UTC RFC3339
-- seconds precision ("2006-01-02T15:04:05Z"); '' means unset.

-- name: CreateSchedule :one
INSERT INTO schedules (
    id, project_id, session_id, name, prompt, cron, mode, enabled,
    pause_reason, next_run_at, expires_at, created_by, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSchedule :one
SELECT * FROM schedules WHERE id = ?;

-- name: ListSchedules :many
SELECT * FROM schedules ORDER BY created_at DESC;

-- name: ListSchedulesBySession :many
SELECT * FROM schedules WHERE session_id = ? ORDER BY created_at DESC;

-- name: ListEnabledSchedulesBySession :many
SELECT * FROM schedules WHERE session_id = ? AND enabled = 1;

-- name: ListDueSchedules :many
SELECT * FROM schedules
WHERE enabled = 1
  AND next_run_at != ''
  AND next_run_at <= ?
ORDER BY next_run_at;

-- name: UpdateSchedule :exec
UPDATE schedules
SET name = ?, prompt = ?, cron = ?, mode = ?, expires_at = ?, updated_at = ?
WHERE id = ?;

-- name: UpdateScheduleNextRun :exec
UPDATE schedules SET next_run_at = ?, last_run_at = ?, updated_at = ? WHERE id = ?;

-- name: SetScheduleEnabled :exec
UPDATE schedules SET enabled = ?, pause_reason = ?, next_run_at = ?, updated_at = ? WHERE id = ?;

-- name: SetScheduleAttention :exec
UPDATE schedules SET attention = ?, attention_run_id = ?, updated_at = ? WHERE id = ?;

-- name: ClearScheduleActionAttention :exec
UPDATE schedules
SET attention = '', attention_run_id = '', updated_at = ?
WHERE id = ? AND attention = 'action_needed';

-- name: SetScheduleFailures :exec
UPDATE schedules SET consecutive_failures = ?, updated_at = ? WHERE id = ?;

-- name: MarkScheduleViewed :exec
UPDATE schedules SET last_viewed_at = ?, updated_at = ? WHERE id = ?;

-- name: DeleteSchedule :exec
DELETE FROM schedules WHERE id = ?;

-- name: CreateScheduleRun :execrows
INSERT INTO schedule_runs (
    id, schedule_id, session_id, scheduled_for, created_at, status, reason
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (schedule_id, scheduled_for) DO NOTHING;

-- name: GetScheduleRun :one
SELECT * FROM schedule_runs WHERE id = ?;

-- name: ListScheduleRuns :many
SELECT * FROM schedule_runs
WHERE schedule_id = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListRunsSince :one
SELECT COUNT(*) AS total,
       COALESCE(SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END), 0) AS ok_count
FROM schedule_runs
WHERE schedule_id = ? AND created_at > ?;

-- name: ListUnfinishedScheduleRuns :many
SELECT * FROM schedule_runs WHERE status IN ('queued', 'firing', 'running');

-- name: ListQueuedRunsBySession :many
SELECT * FROM schedule_runs WHERE session_id = ? AND status = 'queued';

-- name: ListUnfinishedRunsForSchedule :many
SELECT * FROM schedule_runs
WHERE schedule_id = ? AND status IN ('queued', 'firing', 'running');

-- name: ClaimScheduleRun :execrows
UPDATE schedule_runs SET status = 'firing' WHERE id = ? AND status = 'queued';

-- name: RequeueScheduleRun :exec
UPDATE schedule_runs SET status = 'queued', attempts = ?, next_attempt_at = ? WHERE id = ?;

-- name: MarkScheduleRunFired :exec
UPDATE schedule_runs
SET status = 'running', fired_at = ?, turn_index = ?, attempts = ?
WHERE id = ?;

-- name: ResolveScheduleRun :execrows
UPDATE schedule_runs
SET status = ?, finished_at = ?, summary = ?, reason = ?, error = ?, error_kind = ?, duration_ms = ?
WHERE id = ? AND status IN ('queued', 'firing', 'running');

-- name: SetScheduleRunOverdue :exec
UPDATE schedule_runs SET overdue = 1 WHERE id = ? AND status = 'running';

-- name: AppendScheduleRunLateReport :exec
UPDATE schedule_runs SET late_report = ? WHERE id = ?;

-- name: PruneScheduleRuns :exec
DELETE FROM schedule_runs
WHERE schedule_runs.schedule_id = ?
  AND schedule_runs.id NOT IN (
    SELECT keep.id FROM schedule_runs AS keep
    WHERE keep.schedule_id = ?
    ORDER BY keep.created_at DESC
    LIMIT ?
  );
