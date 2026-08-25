import { ChevronRight } from "lucide-react";
import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";
import { BadgeIcon, getBadgeConfig, resolveSessionState, resolveStatusLabel } from "./SessionBadge";

interface SessionStatusPillProps {
  state: SessionState;
  connected?: boolean;
  hasPendingApproval?: boolean;
  isPlanning?: boolean;
  gitOperation?: string;
  compact?: boolean;
  /**
   * Makes the pill a control that takes the user to whatever it is reporting.
   *
   * Pass it only when there is somewhere to go: a pill that reads "Needs
   * approval" from a tab where the buttons are not is a dead end, and a
   * control that does nothing when clicked is worse than plain text. Callers
   * therefore omit this once the user is already looking at the thing.
   */
  onActivate?: () => void;
  /** What activating does, for the tooltip and the screen reader. */
  activateHint?: string;
}

export function SessionStatusPill(props: SessionStatusPillProps) {
  const state = resolveSessionState(props);
  const cfg = getBadgeConfig(state);
  const dim = !props.hasPendingApproval && props.connected === false;
  const label = resolveStatusLabel({
    state: props.state,
    badgeState: state,
    connected: props.connected,
    gitOperation: props.gitOperation,
  });
  const isPulse = !!cfg.pulseRing;
  const hint = props.activateHint;

  const body = (
    <>
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
    </>
  );

  const shape = cn(
    "inline-flex items-center gap-1 rounded-full py-0.5 text-xs font-medium shrink-0",
    props.compact ? "px-1.5" : "px-2",
    cfg.bg,
    cfg.text,
    dim && "opacity-40",
  );

  if (!props.onActivate) {
    return (
      <span className={shape} title={label}>
        {body}
      </span>
    );
  }

  return (
    <button
      type="button"
      onClick={props.onActivate}
      title={hint ? `${label} — ${hint}` : label}
      aria-label={hint ? `${label}, ${hint}` : label}
      className={cn(
        shape,
        "cursor-pointer transition-[filter,gap] hover:brightness-125 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-current",
      )}
    >
      {body}
      <ChevronRight className="-mr-0.5 size-3 shrink-0 opacity-70" />
    </button>
  );
}
