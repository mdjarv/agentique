import { Bot, Check, ChevronDown, ChevronRight, Loader2 } from "lucide-react";
import { memo, useState } from "react";
import { SubagentSteps, subagentSteps } from "~/components/chat/SubagentSteps";
import { ToolIcon } from "~/components/chat/ToolIcons";
import type { ChatEvent, TaskEvent } from "~/stores/chat-store";

interface SubagentActivityProps {
  taskEvents: TaskEvent[];
  /**
   * The subagent's own text/thinking/tool events, forwarded by the CLI when
   * [claude] forward-subagent-text is on. Collapsed by default: the point of
   * the card is the task's status, and a chatty subagent would otherwise bury
   * the parent turn.
   */
  subagentEvents?: ChatEvent[];
}

function formatDuration(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${ms}ms`;
}

export const SubagentActivity = memo(function SubagentActivity({
  taskEvents,
  subagentEvents,
}: SubagentActivityProps) {
  const [expanded, setExpanded] = useState(false);
  const started = taskEvents.find((e) => e.taskSubtype === "task_started");
  const progress = taskEvents.findLast((e) => e.taskSubtype === "task_progress");
  const notification = taskEvents.find((e) => e.taskSubtype === "task_notification");

  if (!started) return null;

  const isCompleted = notification?.taskStatus === "completed";
  const description = started.taskDescription ?? "";
  const taskType = started.taskType ?? "";

  const latest = notification ?? progress;
  const toolCount = latest?.toolUses ?? 0;
  const duration = latest?.durationMs ?? 0;
  const lastTool = progress?.lastToolName;

  const statusParts: string[] = [];
  if (toolCount > 0) statusParts.push(`${toolCount} tool${toolCount !== 1 ? "s" : ""}`);
  if (duration > 0) statusParts.push(formatDuration(duration));
  const statusLine = statusParts.join(", ");
  const nested = subagentSteps(subagentEvents);

  return (
    <div className="ml-5 border-l-2 border-agent/20 pl-2.5">
      <div className="border rounded-md bg-muted/20 overflow-hidden text-xs">
        <div className="flex items-center gap-2 px-2 py-1.5 text-muted-foreground min-w-0">
          {isCompleted ? (
            <Check className="h-3 w-3 text-success shrink-0" />
          ) : (
            <Loader2 className="h-3 w-3 animate-spin shrink-0" />
          )}
          <Bot className="h-3 w-3 text-agent/70 shrink-0" />
          {taskType && <span className="font-medium text-agent/70">[{taskType}]</span>}
          <span className="truncate text-muted-foreground-dim">{description}</span>
          {statusLine && (
            <span className="ml-auto text-muted-foreground-faint shrink-0">{statusLine}</span>
          )}
        </div>

        {!isCompleted && lastTool && (
          <div className="flex items-center gap-2 px-2 pb-1.5 text-muted-foreground-faint min-w-0">
            <span className="w-3 shrink-0" />
            <ToolIcon name={lastTool} />
            <span className="truncate">{lastTool}</span>
          </div>
        )}

        {isCompleted && notification?.taskSummary && (
          <div className="border-t px-2 py-1.5 text-muted-foreground-dim whitespace-pre-wrap">
            {notification.taskSummary}
          </div>
        )}

        {nested.length > 0 && (
          <>
            <button
              type="button"
              onClick={() => setExpanded((v) => !v)}
              className="flex items-center gap-1.5 w-full border-t px-2 py-1.5 text-muted-foreground-faint hover:text-muted-foreground hover:bg-muted/30 transition-colors cursor-pointer"
            >
              {expanded ? (
                <ChevronDown className="h-3 w-3 shrink-0" />
              ) : (
                <ChevronRight className="h-3 w-3 shrink-0" />
              )}
              <span>
                {expanded ? "Hide" : "Show"} {nested.length} subagent{" "}
                {nested.length === 1 ? "step" : "steps"}
              </span>
            </button>
            {expanded && (
              <div className="border-t px-2 py-1.5">
                <SubagentSteps steps={nested} />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
});
