import { Hash } from "lucide-react";
import { memo } from "react";
import { cn, relativeTime } from "~/lib/utils";
import type { SessionData } from "~/stores/chat-store";
import { PulseStatus } from "../../session/PulseStatus";
import { type BadgeState, resolveSessionState } from "../../session/SessionBadge";
import { SessionStatusBadge } from "../../session/SessionStatusBadge";
import type { TodoProgress } from "./types";

const RESTING_STATES: ReadonlySet<BadgeState> = new Set(["idle", "done", "stopped"]);

function sessionTime(meta: SessionData["meta"]): string {
  if (meta.completedAt) return relativeTime(meta.completedAt);
  if (meta.lastQueryAt) return relativeTime(meta.lastQueryAt);
  if (meta.updatedAt) return relativeTime(meta.updatedAt);
  return "";
}

function TimeStamp({ meta }: { meta: SessionData["meta"] }) {
  return (
    <span className="text-[10px] tabular-nums text-muted-foreground-faint">
      {sessionTime(meta)}
    </span>
  );
}

function RightIndicator({ data }: { data: SessionData }) {
  return (
    <SessionStatusBadge
      state={data.meta.state}
      connected={data.meta.connected}
      hasUnseenCompletion={data.hasUnseenCompletion}
      hasPendingApproval={!!(data.pendingApproval || data.pendingQuestion)}
      isPlanning={data.planMode}
      gitOperation={data.meta.gitOperation}
      size="sm"
    />
  );
}

/** Shared layout: name | right-aligned metadata or status indicator. */
function SessionRowLayout({
  data,
  nameClassName,
  extraMeta,
}: {
  data: SessionData;
  nameClassName?: string;
  extraMeta?: React.ReactNode;
}) {
  const badgeState = resolveSessionState({
    state: data.meta.state,
    hasPendingApproval: !!(data.pendingApproval || data.pendingQuestion),
    isPlanning: data.planMode,
  });
  const isResting = RESTING_STATES.has(badgeState);

  return (
    <>
      <div className="truncate flex-1 min-w-0">
        <span className={cn("block truncate text-sm", nameClassName)}>
          {data.meta.name || "Untitled"}
        </span>
        {data.meta.state === "running" && <PulseStatus sessionId={data.meta.id} />}
      </div>
      <span className="flex items-center gap-1.5 shrink-0 ml-auto">
        {extraMeta}
        {isResting ? <TimeStamp meta={data.meta} /> : <RightIndicator data={data} />}
      </span>
    </>
  );
}

/** Standard session content — name, time or status, todo progress. */
export const SessionContent = memo(function SessionContent({ data }: { data: SessionData }) {
  const { meta } = data;
  const todoDone = data.todos?.filter((t) => t.status === "completed").length ?? 0;
  const todoTotal = data.todos?.length ?? 0;
  const isTerminal = meta.state === "done" || meta.state === "stopped" || meta.state === "failed";
  const faded = isTerminal && meta.worktreeMerged;
  const todosInProgress = todoTotal > 0 && todoDone < todoTotal;

  return (
    <SessionRowLayout
      data={data}
      nameClassName={cn(
        !meta.name && "italic text-muted-foreground",
        faded && "text-muted-foreground line-through decoration-muted-foreground/50",
        data.hasUnseenCompletion && "font-semibold text-foreground-bright",
      )}
      extraMeta={
        todosInProgress ? (
          <span className="text-[11px] text-muted-foreground tabular-nums">
            {todoDone}/{todoTotal}
          </span>
        ) : undefined
      }
    />
  );
});

/** Lead session content — adds worker count badge. */
export const LeadSessionContent = memo(function LeadSessionContent({
  data,
  workerCount,
}: {
  data: SessionData;
  workerCount: number;
}) {
  return (
    <SessionRowLayout
      data={data}
      nameClassName={cn(
        !data.meta.name && "italic text-muted-foreground",
        data.hasUnseenCompletion && "font-semibold text-foreground-bright",
      )}
      extraMeta={
        <span
          className="inline-flex items-center gap-0.5 text-[10px] text-primary/60"
          title={`Lead of ${workerCount} worker${workerCount !== 1 ? "s" : ""}`}
        >
          <Hash className="size-2.5" />
          {workerCount}
        </span>
      }
    />
  );
});

/** Worker session content — muted name. */
export const WorkerSessionContent = memo(function WorkerSessionContent({
  data,
}: {
  data: SessionData;
}) {
  return (
    <SessionRowLayout
      data={data}
      nameClassName={cn(
        "text-muted-foreground",
        data.hasUnseenCompletion && "font-semibold text-foreground-bright",
      )}
    />
  );
});

/** Extract todo progress data for SidebarRow — pass as `todoProgress` prop. */
export function getTodoProgress(data: SessionData): TodoProgress | undefined {
  const todoTotal = data.todos?.length ?? 0;
  if (todoTotal === 0) return undefined;
  const todoDone = data.todos?.filter((t) => t.status === "completed").length ?? 0;
  return { done: todoDone, total: todoTotal };
}
