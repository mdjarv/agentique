import type { SessionState } from "~/stores/chat-store";
import {
  type BadgeSize,
  getBadgeConfig,
  resolveSessionState,
  resolveStatusLabel,
  SessionBadge,
} from "./SessionBadge";

interface SessionStatusBadgeProps {
  state: SessionState;
  connected?: boolean;
  hasUnseenCompletion?: boolean;
  hasPendingApproval?: boolean;
  isPlanning?: boolean;
  /** Worst-of schedule attention for the session ('' when none). */
  scheduleAttention?: "" | "action_needed" | "failed";
  gitOperation?: string;
  size?: BadgeSize;
  className?: string;
}

export function SessionStatusBadge({
  state,
  connected = true,
  hasUnseenCompletion,
  hasPendingApproval,
  isPlanning,
  scheduleAttention,
  gitOperation,
  size,
  className,
}: SessionStatusBadgeProps) {
  const badgeState = resolveSessionState({
    state,
    hasPendingApproval,
    isPlanning,
    scheduleAttention,
  });
  const title =
    state === "idle" && !connected
      ? "Idle (disconnected)"
      : state === "merging" && (gitOperation === "rebasing" || gitOperation === "creating_pr")
        ? resolveStatusLabel({ state, badgeState, connected, gitOperation })
        : undefined;

  return (
    <SessionBadge
      state={badgeState}
      size={size}
      dim={!connected && !hasPendingApproval}
      ring={!!hasUnseenCompletion}
      pulse={!!getBadgeConfig(badgeState).pulseRing}
      gitOperation={gitOperation}
      title={title}
      className={className}
    />
  );
}
