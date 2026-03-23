import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";

const stateConfig: Record<SessionState, { label: string; classes: string }> = {
  draft: { label: "Draft", classes: "text-[#565f89] bg-[#565f89]/20" },
  idle: { label: "Idle", classes: "text-[#9ece6a] bg-[#9ece6a]/20" },
  running: { label: "Run", classes: "text-[#e0af68] bg-[#e0af68]/20" },
  starting: { label: "Init", classes: "text-[#7aa2f7] bg-[#7aa2f7]/20" },
  failed: { label: "Fail", classes: "text-[#f7768e] bg-[#f7768e]/20" },
  disconnected: { label: "Lost", classes: "text-[#414868] bg-[#414868]/30" },
  stopped: { label: "Stop", classes: "text-[#3b4261] bg-[#3b4261]/30" },
  done: { label: "Done", classes: "text-[#3b4261] bg-[#3b4261]/30" },
};

const attentionConfig = { label: "New", classes: "text-[#73daca] bg-[#73daca]/20" };
const waitingConfig = { label: "Wait", classes: "text-[#bb9af7] bg-[#bb9af7]/20" };

const planningConfig = { label: "Plan", classes: "text-[#e0af68] bg-[#e0af68]/20" };

interface SessionStatusBadgeProps {
  state: SessionState;
  hasUnseenCompletion?: boolean;
  hasPendingApproval?: boolean;
  isPlanning?: boolean;
}

export function SessionStatusBadge({ state, hasUnseenCompletion, hasPendingApproval, isPlanning }: SessionStatusBadgeProps) {
  const showAttention = hasUnseenCompletion && state === "idle";
  const config = hasPendingApproval
    ? waitingConfig
    : isPlanning && state === "running"
      ? planningConfig
      : showAttention
        ? attentionConfig
        : stateConfig[state];

  return (
    <span
      className={cn(
        "inline-flex items-center justify-center rounded px-1.5 text-[10px] font-semibold leading-4 shrink-0 uppercase tracking-wide",
        config.classes,
        (hasPendingApproval || state === "running" || showAttention) && "animate-pulse",
      )}
    >
      {config.label}
    </span>
  );
}
