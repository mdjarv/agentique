import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";

const stateColors: Record<SessionState, string> = {
  draft: "bg-[#565f89]",
  idle: "bg-[#9ece6a]",
  running: "bg-[#e0af68]",
  starting: "bg-[#7aa2f7]",
  failed: "bg-[#f7768e]",
  disconnected: "bg-[#414868]",
  stopped: "bg-[#3b4261]",
  done: "bg-[#3b4261]",
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
        showAttention ? "bg-[#73daca]" : stateColors[state],
        (state === "running" || showAttention) && "animate-pulse",
      )}
      title={showAttention ? "completed" : state}
    />
  );
}
