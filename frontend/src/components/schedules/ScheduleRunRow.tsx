import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import { agoText, formatRunDuration, runStatusMeta } from "~/components/schedules/schedule-format";
import type { ScheduleRunInfo } from "~/lib/schedule-actions";
import { cn } from "~/lib/utils";

interface ScheduleRunRowProps {
  run: ScheduleRunInfo;
}

/** Compact, expandable row for one schedule run (Loops tab + /schedules page). */
export function ScheduleRunRow({ run }: ScheduleRunRowProps) {
  const [expanded, setExpanded] = useState(false);
  const meta = runStatusMeta(run);

  // Skipped slots are noise, not outcomes — dimmed one-liner, no expansion.
  if (run.status === "skipped") {
    return (
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 rounded px-2 py-1 text-xs text-muted-foreground/60">
        <span className={cn("size-1.5 rounded-full shrink-0", meta.dotClass)} />
        <span>skipped</span>
        {run.scheduledFor && <span>slot {agoText(run.scheduledFor)}</span>}
        {run.reason && <span className="min-w-0 break-words">{run.reason}</span>}
      </div>
    );
  }

  const when = run.firedAt || run.scheduledFor;
  const summaryLine = run.summary || run.reason;
  const duration = formatRunDuration(run.durationMs);

  return (
    <button
      type="button"
      className="w-full text-left rounded border bg-card/30 px-2 py-1.5 space-y-1 hover:bg-muted/30 transition-colors"
      onClick={() => setExpanded((v) => !v)}
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
        <span className={cn("size-2 rounded-full shrink-0", meta.dotClass)} />
        <span className={cn("font-medium", meta.textClass)}>{meta.label}</span>
        {when && <span className="text-muted-foreground/70">{agoText(when)}</span>}
        {duration && <span className="text-muted-foreground/70 tabular-nums">{duration}</span>}
        {expanded ? (
          <ChevronDown className="size-3 text-muted-foreground/50 ml-auto shrink-0" />
        ) : (
          <ChevronRight className="size-3 text-muted-foreground/50 ml-auto shrink-0" />
        )}
      </div>

      {!expanded && (summaryLine || run.error) && (
        <p
          className={cn(
            "text-[11px] truncate",
            run.error ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {run.error || summaryLine}
        </p>
      )}

      {expanded && (
        <div className="space-y-1.5 text-[11px]">
          {run.summary && (
            <div className="rounded bg-muted/50 p-1.5 whitespace-pre-wrap break-words">
              {run.summary}
            </div>
          )}
          {!run.summary && run.reason && (
            <p className="text-muted-foreground break-words">{run.reason}</p>
          )}
          {run.error && (
            <p className="text-destructive break-words">
              {run.error}
              {run.errorKind && <span className="text-destructive/70"> ({run.errorKind})</span>}
            </p>
          )}
          {run.lateReport && (
            <p className="text-muted-foreground break-words">
              after this run was resolved: {run.lateReport}
            </p>
          )}
          <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-muted-foreground/60">
            {run.scheduledFor && <span>slot {agoText(run.scheduledFor)}</span>}
            {run.firedAt && <span>fired {agoText(run.firedAt)}</span>}
            {run.attempts > 1 && <span>{run.attempts} attempts</span>}
            {run.overdue && <span className="text-amber-500">ran past the overdue limit</span>}
          </div>
        </div>
      )}
    </button>
  );
}
