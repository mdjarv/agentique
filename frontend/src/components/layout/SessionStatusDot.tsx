import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";

const stateColors: Record<SessionState, string> = {
  idle: "bg-[#9ece6a]",
  running: "bg-[#e0af68]",
  starting: "bg-[#7aa2f7]",
  failed: "bg-[#f7768e]",
  disconnected: "bg-[#ff9e64]",
  stopped: "bg-[#a9b1d6]",
  done: "bg-[#7dcfff]",
};

interface SessionStatusDotProps {
  state: SessionState;
  hasUnseenCompletion?: boolean;
  hasPendingApproval?: boolean;
}

export function SessionStatusDot({
  state,
  hasUnseenCompletion,
  hasPendingApproval,
}: SessionStatusDotProps) {
  const showAttention = hasUnseenCompletion && state === "idle";
  const waiting = hasPendingApproval;
  const color = waiting ? "bg-[#bb9af7]" : showAttention ? "bg-[#73daca]" : stateColors[state];
  const pulse = waiting || state === "running" || showAttention;
  const title = waiting ? "waiting for approval" : showAttention ? "completed" : state;
  return (
    <span
      className={cn("inline-block h-2 w-2 rounded-full shrink-0", color, pulse && "animate-pulse")}
      title={title}
    />
  );
}
