import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";

const stateColors: Record<SessionState, string> = {
  idle: "bg-[#9ece6a]",
  running: "bg-[#73daca]",
  failed: "bg-[#f7768e]",
  stopped: "bg-[#a9b1d6]",
  done: "bg-[#7dcfff]",
  merging: "bg-[#7aa2f7]",
};

interface SessionStatusDotProps {
  state: SessionState;
  connected?: boolean;
  hasPendingApproval?: boolean;
}

export function SessionStatusDot({
  state,
  connected = true,
  hasPendingApproval,
}: SessionStatusDotProps) {
  const waiting = hasPendingApproval;
  const color = waiting ? "bg-[#ff9e64]" : stateColors[state];
  const pulse = waiting;
  const title = waiting ? "waiting for approval" : state;
  return (
    <span
      className={cn(
        "inline-block h-2 w-2 rounded-full shrink-0",
        color,
        pulse && "animate-pulse",
        !connected && "opacity-40",
      )}
      title={title}
    />
  );
}
