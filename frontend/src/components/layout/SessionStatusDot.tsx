import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";

const stateColors: Record<SessionState, string> = {
  idle: "bg-success",
  running: "bg-teal",
  failed: "bg-destructive",
  stopped: "bg-foreground",
  done: "bg-info",
  merging: "bg-primary",
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
  const color = waiting ? "bg-orange" : stateColors[state];
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
