import { memo } from "react";
import { useShallow } from "zustand/react/shallow";
import { type PulseData, usePulseStore } from "~/stores/pulse-store";

/** Format a file path to just the basename for compact display. */
function basename(path: string): string {
  const i = path.lastIndexOf("/");
  return i >= 0 ? path.slice(i + 1) : path;
}

/** Human-readable label for tool categories. */
const CATEGORY_LABELS: Record<string, string> = {
  command: "running command",
  file_write: "editing",
  file_read: "reading",
  web: "searching web",
  agent: "delegating",
  task: "managing tasks",
  plan: "planning",
  meta: "configuring",
  question: "asking",
  mcp: "using tool",
  other: "working",
};

function formatPulse(pulse: PulseData): string {
  const parts: string[] = [];

  // Activity description
  if (pulse.lastFilePath) {
    const label = CATEGORY_LABELS[pulse.lastToolCategory] ?? "working on";
    parts.push(`${label} ${basename(pulse.lastFilePath)}`);
  } else if (pulse.lastToolCategory) {
    parts.push(CATEGORY_LABELS[pulse.lastToolCategory] ?? "working");
  }

  // Commit count
  if (pulse.commitCount > 0) {
    parts.push(`${pulse.commitCount} commit${pulse.commitCount !== 1 ? "s" : ""}`);
  }

  // Tool call count
  if (pulse.toolCallCount > 0) {
    parts.push(`${pulse.toolCallCount} tool call${pulse.toolCallCount !== 1 ? "s" : ""}`);
  }

  return parts.join(" \u00b7 ");
}

interface PulseStatusProps {
  sessionId: string;
}

export const PulseStatus = memo(function PulseStatus({ sessionId }: PulseStatusProps) {
  const pulse = usePulseStore(useShallow((s) => s.pulses[sessionId]));
  if (!pulse) return null;

  const text = formatPulse(pulse);
  const taskBadge = pulse.todoTotal > 0 ? `${pulse.todoCompleted}/${pulse.todoTotal} tasks` : "";

  if (!text && !taskBadge) return null;

  const parts = [text, taskBadge].filter(Boolean).join(" · ");

  return (
    <span className="block truncate text-[10px] text-muted-foreground-faint" title={parts}>
      {text && <span>{text}</span>}
      {text && taskBadge && <span> · </span>}
      {taskBadge && <span className="tabular-nums text-muted-foreground">{taskBadge}</span>}
    </span>
  );
});
