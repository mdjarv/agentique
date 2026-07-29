import { Clock, Pause, Play, Trash2, Zap } from "lucide-react";
import { Fragment, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { ScheduleRunRow } from "~/components/schedules/ScheduleRunRow";
import {
  formatRunDuration,
  humanCadence,
  pauseReasonText,
  untilText,
} from "~/components/schedules/schedule-format";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { ScheduleInfo, ScheduleRunInfo } from "~/lib/schedule-actions";
import {
  deleteSchedule,
  listScheduleRuns,
  markScheduleViewed,
  pauseSchedule,
  resumeSchedule,
  runScheduleNow,
} from "~/lib/schedule-actions";
import { getErrorMessage } from "~/lib/utils";
import { EMPTY_RUNS, useScheduleStore } from "~/stores/schedule-store";

interface LoopsPanelProps {
  sessionId: string;
}

/** Per-session Loops tab: schedules targeting this session with run history. */
export function LoopsPanel({ sessionId }: LoopsPanelProps) {
  const schedules = useScheduleStore(
    useShallow((s) => {
      const list = Object.values(s.schedules).filter((sc) => sc.sessionId === sessionId);
      list.sort((a, b) => a.createdAt.localeCompare(b.createdAt));
      return list;
    }),
  );

  if (schedules.length === 0) {
    return (
      <div className="flex-1 overflow-y-auto flex items-center justify-center p-6">
        <div className="text-center space-y-2 max-w-sm">
          <Clock className="size-8 mx-auto text-muted-foreground/40" />
          <p className="text-sm text-muted-foreground">No loops on this session</p>
          <p className="text-xs text-muted-foreground/70">
            Ask the agent in chat (&ldquo;schedule a check every 30m&rdquo;) or create one from the
            /schedules page.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="p-4 space-y-4">
        {schedules.map((schedule) => (
          <ScheduleCard key={schedule.id} schedule={schedule} />
        ))}
      </div>
    </div>
  );
}

/** Terminal outcome statuses that count toward the ok-rate. */
const OUTCOME_STATUSES = new Set(["ok", "action_needed", "error", "interrupted"]);

function computeStats(runs: ScheduleRunInfo[], now: number) {
  const dayAgo = now - 24 * 60 * 60 * 1000;
  let fires24h = 0;
  let outcomes24h = 0;
  let ok24h = 0;
  const durations: number[] = [];
  for (const run of runs) {
    if (run.durationMs > 0) durations.push(run.durationMs);
    if (!run.firedAt) continue;
    const fired = new Date(run.firedAt).getTime();
    if (Number.isNaN(fired) || fired < dayAgo) continue;
    fires24h++;
    if (OUTCOME_STATUSES.has(run.status)) {
      outcomes24h++;
      if (run.status === "ok") ok24h++;
    }
  }
  durations.sort((a, b) => a - b);
  const p50 = durations[Math.floor((durations.length - 1) / 2)] ?? 0;
  const okRate = outcomes24h > 0 ? Math.round((ok24h / outcomes24h) * 100) : null;
  return { fires24h, okRate, p50 };
}

function ScheduleCard({ schedule }: { schedule: ScheduleInfo }) {
  const ws = useWebSocket();
  const runs = useScheduleStore((s) => s.runs[schedule.id] ?? EMPTY_RUNS);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [busy, setBusy] = useState(false);
  // Divider baseline: captured once at mount, BEFORE mark-viewed re-stamps
  // lastViewedAt — otherwise the "since you last looked" line would vanish
  // the instant the server acknowledges the view.
  const [viewBaseline] = useState(schedule.lastViewedAt);

  useEffect(() => {
    listScheduleRuns(ws, { scheduleId: schedule.id, limit: 50 })
      .then((page) => useScheduleStore.getState().setRuns(schedule.id, page))
      .catch((err) => console.error("listScheduleRuns failed", err));
    // Viewing the Loops tab clears view-clearable attention and re-stamps the
    // divider baseline server-side (the local baseline above stays pre-view).
    markScheduleViewed(ws, { id: schedule.id }).catch((err) =>
      console.error("markScheduleViewed failed", err),
    );
  }, [ws, schedule.id]);

  const stats = useMemo(() => computeStats(runs, Date.now()), [runs]);

  // Index of the first run at-or-before the baseline; runs are newest-first,
  // so the divider goes right above it. Skip when the baseline is empty,
  // nothing is newer (index 0), or everything is newer (no boundary in view).
  const dividerIndex = useMemo(() => {
    if (!viewBaseline) return -1;
    const baseline = new Date(viewBaseline).getTime();
    if (Number.isNaN(baseline)) return -1;
    const idx = runs.findIndex((r) => {
      const t = new Date(r.firedAt || r.scheduledFor || r.createdAt).getTime();
      return !Number.isNaN(t) && t <= baseline;
    });
    return idx > 0 ? idx : -1;
  }, [runs, viewBaseline]);

  const nextFire = schedule.enabled
    ? untilText(schedule.nextRunAt) || "—"
    : pauseReasonText(schedule.pauseReason);

  const handleRunNow = async () => {
    setBusy(true);
    try {
      const run = await runScheduleNow(ws, { id: schedule.id });
      useScheduleStore.getState().upsertRun(run);
      toast.success("Run queued");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to queue run"));
    } finally {
      setBusy(false);
    }
  };

  const handlePauseResume = async () => {
    setBusy(true);
    try {
      const updated = schedule.enabled
        ? await pauseSchedule(ws, { id: schedule.id })
        : await resumeSchedule(ws, { id: schedule.id });
      useScheduleStore.getState().upsertSchedule(updated);
    } catch (err) {
      toast.error(getErrorMessage(err, schedule.enabled ? "Failed to pause" : "Failed to resume"));
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async () => {
    setBusy(true);
    try {
      await deleteSchedule(ws, { id: schedule.id });
      useScheduleStore.getState().removeSchedule(schedule.id);
      toast.success("Loop deleted");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to delete loop"));
    } finally {
      setBusy(false);
      setConfirmDelete(false);
    }
  };

  return (
    <div className="rounded-lg border bg-card/40">
      {/* Header row */}
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 py-2 border-b">
        <Clock className="size-3.5 text-muted-foreground shrink-0" />
        <span className="text-sm font-medium min-w-0 truncate">{schedule.name}</span>
        <span className="text-xs text-muted-foreground">{humanCadence(schedule)}</span>
        <span className="text-xs text-muted-foreground/70">
          {schedule.enabled && schedule.nextRunAt ? `next ${nextFire}` : nextFire}
        </span>
        {schedule.attention !== "" && (
          <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-500">
            {schedule.attention === "failed" ? "loop paused" : "needs you"}
          </span>
        )}
        {schedule.consecutiveFailures > 0 && (
          <span className="rounded bg-destructive/10 px-1.5 py-0.5 text-[10px] text-destructive tabular-nums">
            {schedule.consecutiveFailures} consecutive failure
            {schedule.consecutiveFailures !== 1 ? "s" : ""}
          </span>
        )}
        <div className="flex items-center gap-1 ml-auto shrink-0">
          <Button size="xs" variant="ghost" disabled={busy} onClick={handleRunNow} title="Run now">
            <Zap className="size-3" />
            Run now
          </Button>
          <Button
            size="icon-xs"
            variant="ghost"
            disabled={busy}
            onClick={handlePauseResume}
            title={schedule.enabled ? "Pause" : "Resume"}
          >
            {schedule.enabled ? <Pause className="size-3" /> : <Play className="size-3" />}
          </Button>
          <Button
            size="icon-xs"
            variant="ghost"
            className="text-muted-foreground hover:text-destructive"
            disabled={busy}
            onClick={() => setConfirmDelete(true)}
            title="Delete"
          >
            <Trash2 className="size-3" />
          </Button>
        </div>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-px bg-border/50 border-b">
        <Stat label="Fires 24h" value={String(stats.fires24h)} />
        <Stat label="Ok rate 24h" value={stats.okRate === null ? "—" : `${stats.okRate}%`} />
        <Stat label="P50 duration" value={stats.p50 > 0 ? formatRunDuration(stats.p50) : "—"} />
        <Stat label="Next fire" value={nextFire} />
      </div>

      {/* Run history */}
      <div className="p-2 space-y-1">
        {runs.length === 0 && (
          <p className="px-1 py-2 text-xs text-muted-foreground/60">No runs yet.</p>
        )}
        {runs.map((run, i) => (
          <Fragment key={run.id}>
            {i === dividerIndex && (
              <div className="flex items-center gap-2 px-1 py-0.5">
                <div className="h-px flex-1 bg-primary/30" />
                <span className="text-[10px] text-muted-foreground/60 shrink-0">
                  since you last looked
                </span>
                <div className="h-px flex-1 bg-primary/30" />
              </div>
            )}
            <ScheduleRunRow run={run} />
          </Fragment>
        ))}
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete loop</AlertDialogTitle>
            <AlertDialogDescription>
              Delete &quot;{schedule.name}&quot;? This removes the schedule and its run history.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={busy}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-card/60 px-3 py-1.5 min-w-0">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className="text-sm font-medium tabular-nums truncate">{value}</div>
    </div>
  );
}
