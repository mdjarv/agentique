import { Bot, Check, Loader2 } from "lucide-react";
import { memo } from "react";
import { ToolIcon } from "~/components/chat/ToolIcons";
import type { ChatEvent } from "~/stores/chat-store";

interface SubagentActivityProps {
  taskEvents: ChatEvent[];
}

function formatDuration(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${ms}ms`;
}

export const SubagentActivity = memo(function SubagentActivity({
  taskEvents,
}: SubagentActivityProps) {
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
          <span className="truncate text-muted-foreground/70">{description}</span>
          {statusLine && (
            <span className="ml-auto text-muted-foreground/50 shrink-0">{statusLine}</span>
          )}
        </div>

        {!isCompleted && lastTool && (
          <div className="flex items-center gap-2 px-2 pb-1.5 text-muted-foreground/50 min-w-0">
            <span className="w-3 shrink-0" />
            <ToolIcon name={lastTool} />
            <span className="truncate">{lastTool}</span>
          </div>
        )}

        {isCompleted && notification?.taskSummary && (
          <div className="border-t px-2 py-1.5 text-muted-foreground/70 whitespace-pre-wrap">
            {notification.taskSummary}
          </div>
        )}
      </div>
    </div>
  );
});
