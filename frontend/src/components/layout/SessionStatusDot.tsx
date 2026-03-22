import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";

const stateColors: Record<SessionState, string> = {
  idle: "bg-[#a6e3a1]",
  running: "bg-[#f9e2af]",
  starting: "bg-[#89b4fa]",
  failed: "bg-[#f38ba8]",
  disconnected: "bg-[#6c7086]",
  stopped: "bg-[#585b70]",
  done: "bg-[#585b70]",
};

export function SessionStatusDot({ state }: { state: SessionState }) {
  return (
    <span
      className={cn(
        "inline-block h-2 w-2 rounded-full shrink-0",
        stateColors[state],
        state === "running" && "animate-pulse",
      )}
      title={state}
    />
  );
}
