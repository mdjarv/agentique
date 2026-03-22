import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";

const stateColors: Record<SessionState, string> = {
  draft: "bg-[#9399b2]",
  idle: "bg-[#a6e3a1]",
  running: "bg-[#f9e2af]",
  starting: "bg-[#89b4fa]",
  failed: "bg-[#f38ba8]",
  disconnected: "bg-[#6c7086]",
  stopped: "bg-[#585b70]",
  done: "bg-[#585b70]",
};

interface SessionStatusDotProps {
  state: SessionState;
  hasUnseenCompletion?: boolean;
}

export function SessionStatusDot({ state, hasUnseenCompletion }: SessionStatusDotProps) {
  const showAttention = hasUnseenCompletion && state === "idle";
  return (
    <span
      className={cn(
        "inline-block h-2 w-2 rounded-full shrink-0",
        showAttention ? "bg-[#40a02b]" : stateColors[state],
        (state === "running" || showAttention) && "animate-pulse",
      )}
      title={showAttention ? "completed" : state}
    />
  );
}
