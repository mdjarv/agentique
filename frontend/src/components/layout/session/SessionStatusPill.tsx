import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";
import { BadgeIcon, getBadgeConfig, resolveSessionState } from "./SessionBadge";

interface SessionStatusPillProps {
  state: SessionState;
  connected?: boolean;
  hasPendingApproval?: boolean;
  isPlanning?: boolean;
  gitOperation?: string;
  compact?: boolean;
}

export function SessionStatusPill(props: SessionStatusPillProps) {
  const state = resolveSessionState(props);
  const cfg = getBadgeConfig(state);
  const dim = !props.hasPendingApproval && props.connected === false;

  let label = cfg.label;
  if (props.state === "idle" && props.connected === false) label = "Disconnected";
  if (props.state === "merging" && props.gitOperation === "rebasing") label = "Rebasing";
  if (props.state === "merging" && props.gitOperation === "creating_pr") label = "Creating PR";

  const isPulse = !!cfg.pulseRing;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full py-0.5 text-xs font-medium shrink-0",
        props.compact ? "px-1.5" : "px-2",
        cfg.bg,
        cfg.text,
        dim && "opacity-40",
      )}
      title={label}
    >
      {isPulse ? (
        <span className="relative flex items-center justify-center size-3 shrink-0">
          <span className="absolute inset-0 rounded-full animate-pulse ring-1 ring-current/30" />
          <BadgeIcon state={state} />
        </span>
      ) : (
        <span className="shrink-0">
          <BadgeIcon state={state} gitOperation={props.gitOperation} />
        </span>
      )}
      {!props.compact && label}
    </span>
  );
}
