import type { SessionState } from "~/stores/chat-store";
import { type BadgeSize, getBadgeConfig, resolveSessionState, SessionBadge } from "./SessionBadge";

interface SessionStatusBadgeProps {
  state: SessionState;
  connected?: boolean;
  hasUnseenCompletion?: boolean;
  hasPendingApproval?: boolean;
  isPlanning?: boolean;
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
  gitOperation,
  size,
  className,
}: SessionStatusBadgeProps) {
  const badgeState = resolveSessionState({ state, hasPendingApproval, isPlanning });

  let title: string | undefined;
  if (state === "idle" && !connected) title = "Idle (disconnected)";
  if (state === "merging" && gitOperation === "rebasing") title = "Rebasing";
  if (state === "merging" && gitOperation === "creating_pr") title = "Creating PR";

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
