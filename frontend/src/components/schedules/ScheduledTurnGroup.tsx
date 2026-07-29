import { ChevronDown, ChevronRight, Clock } from "lucide-react";
import { memo, useMemo, useState } from "react";
import { TurnBlock } from "~/components/chat/TurnBlock";
import { agoText } from "~/components/schedules/schedule-format";
import type { Turn } from "~/stores/chat-store";

interface ScheduledTurnGroupProps {
  /** The consecutive schedule-origin member turns (always 2+). */
  turns: Turn[];
  sessionId: string;
  projectId: string;
  sessionState: string;
  projectPath?: string;
  worktreePath?: string;
}

function firstTimestamp(turn: Turn | undefined): number | undefined {
  if (!turn) return undefined;
  for (const e of turn.events) if (e.timestamp != null) return e.timestamp;
  return undefined;
}

function lastTimestamp(turn: Turn | undefined): number | undefined {
  if (!turn) return undefined;
  for (let i = turn.events.length - 1; i >= 0; i--) {
    const ts = turn.events[i]?.timestamp;
    if (ts != null) return ts;
  }
  return undefined;
}

/**
 * Collapsed row for a run of consecutive schedule-origin turns
 * (docs/scheduled-loops.md, "Timeline at real cadences"). Member turns are
 * rendered lazily — mounted only once the group is expanded — so a chatty loop
 * doesn't flood the timeline or the LazyTurn mount latch.
 */
export const ScheduledTurnGroup = memo(function ScheduledTurnGroup({
  turns,
  sessionId,
  projectId,
  sessionState,
  projectPath,
  worktreePath,
}: ScheduledTurnGroupProps) {
  const [expanded, setExpanded] = useState(false);

  const { title, rangeText } = useMemo(() => {
    const names = new Set<string>();
    for (const t of turns) {
      if (t.origin?.scheduleName) names.add(t.origin.scheduleName);
    }
    // Interleaved loops from different schedules share one group — fall back
    // to a neutral title rather than mislabeling the whole run.
    const groupTitle =
      names.size === 1 ? (names.values().next().value ?? "Scheduled runs") : "Scheduled runs";
    const firstTs = firstTimestamp(turns[0]);
    const lastTs = lastTimestamp(turns[turns.length - 1]);
    const first = firstTs != null ? agoText(new Date(firstTs).toISOString()) : "";
    const last = lastTs != null ? agoText(new Date(lastTs).toISOString()) : "";
    const range = first && last ? (first === last ? first : `${first} → ${last}`) : "";
    return { title: groupTitle, rangeText: range };
  }, [turns]);

  return (
    <div>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className="flex w-full items-center gap-2 rounded-lg border border-border/50 bg-muted/30 px-3 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0" />
        )}
        <Clock className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate font-medium text-foreground/80">{title}</span>
        <span className="shrink-0">
          — {turns.length} runs
          {rangeText ? `, ${rangeText}` : ""}
        </span>
      </button>
      {expanded && (
        <div className="mt-4 space-y-8 border-l border-border/40 pl-3 max-md:pl-2">
          {turns.map((turn) => (
            <TurnBlock
              key={turn.id}
              turn={turn}
              isLast={false}
              sessionId={sessionId}
              projectId={projectId}
              sessionState={sessionState}
              projectPath={projectPath}
              worktreePath={worktreePath}
            />
          ))}
        </div>
      )}
    </div>
  );
});
